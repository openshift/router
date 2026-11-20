package qe

import (
	"fmt"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	util "github.com/openshift/router-tests-extension/test"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Cookies", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("route-cookies", compat_otp.KubeConfigPath())

	// Includes OCP-11679: Disable haproxy hash based sticky session for unsecure routes.
	//Author:hongli@redhat.com
	g.It("Enable/Disable haproxy cookies based sticky session for unsecure routes", g.Label("OCP-11903"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			cookieDir           = "/data/OCP-11903-cookie"
			cookie              = cookieDir + "/cookie-11903"

			ingctrl = util.IngressControllerDescription{
				Name:      "11903",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		defer func() {
			o.Expect(ingctrl.Delete(oc)).NotTo(o.HaveOccurred())
		}()
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.0: Prepare a private copy of the web-server fixture for testing")
		testPodSvc = util.CopyFixture(testPodSvc)
		util.UpdateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		compat_otp.By("3.0: Create a client pod and two server pods and the service")
		ns := oc.Namespace()
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", util.MirrorFixtureImages(oc, ns, clientPod)).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, clientPodName, "--", "mkdir", "-p", cookieDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("4.0: Create a plain HTTP route")
		routehost := "unsecure11903." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", unsecSvcName, unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unsecSvcName, ingctrl.Name)

		compat_otp.By("5.0: Curl the http route, make sure the second server is hit")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-s", "-c", cookie, "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput := []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		compat_otp.By("6.0: Curl the http route again, make sure the first server is hit")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-s", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		compat_otp.By("7.0: Curl the http route with the specified cookie for 10 times, expect all are forwarded to the second server")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-s", "-b", cookie, "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result := util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 120, 10)
		o.Expect(result[1]).To(o.Equal(10))

		compat_otp.By(`8.0: Disable haproxy hash based sticky session for the route by adding 'disable cookies' annotation to it`)
		util.SetAnnotation(oc, ns, "route/"+unsecSvcName, "haproxy.router.openshift.io/disable_cookies=true")

		compat_otp.By("9.0: Check the disable cookies configuration in haproxy")
		backendStart := fmt.Sprintf("backend be_http:%s:%s", ns, unsecSvcName)
		util.EnsureHaproxyBlockConfigNotMatchRegexp(oc, routerpod, backendStart, []string{`cookie .+httponly`})

		compat_otp.By("10.0: Curl the http route with the specified cookie for 10 times again, expect the requests are forwarded to the two servers")
		_, result = util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 120, 10)
		o.Expect(result[0] + result[1]).To(o.Equal(10))
		o.Expect(result[0]).To(o.BeNumerically(">", 0))
		o.Expect(result[1]).To(o.BeNumerically(">", 0))
	})

	//Author:hongli@redhat.com
	g.It("Cookie name should not use openshift prefix", g.Label("OCP-12566"), func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !util.IsCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			routeName           = "unsecureroute2"
		)

		compat_otp.By("1.0: Prepare a private cookie directory and a private copy of the web-server fixture")
		fileDir, err := os.MkdirTemp("", "ocp-12566-cookie")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private cookie directory for OCP-12566")
		defer os.RemoveAll(fileDir)
		cookie := filepath.Join(fileDir, "cookie12566")
		testPodSvc = util.CopyFixture(testPodSvc)
		util.UpdateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		compat_otp.By("2.0: Create two server pods and the service")
		ns := oc.Namespace()
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("3.0: Create a plain HTTP route")
		routehost := "unsecure12566.apps." + util.GetBaseDomain(oc)
		util.CreateRoute(oc, ns, "http", routeName, unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")

		compat_otp.By("4.0: Curl the http route, make sure one server is hit")
		curlCmd := fmt.Sprintf(`curl http://%s -s -c %s --connect-timeout 10`, routehost, cookie)
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("5.0: Check the cookies which should not contain a OPENSHIFT prefix or an IP-address hash")
		cookiesOutput, err := os.ReadFile(cookie)
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to read the cookie file for OCP-12566")
		o.Expect(string(cookiesOutput)).NotTo(o.ContainSubstring("OPENSHIFT"))
		o.Expect(string(cookiesOutput)).NotTo(o.MatchRegexp(`\d+\.\d+\.\d+\.\d+`))
	})

	//Author:hongli@redhat.com
	g.It("Can set cookie name for unsecure routes by annotation", g.Label("OCP-15872"), func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !util.IsCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create two server pods and the service")
		testPodSvc = util.CopyFixture(testPodSvc)
		util.UpdateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")
		ns := oc.Namespace()
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("2.0: Create a plain HTTP route")
		routehost := "unsecure15872.apps." + util.GetBaseDomain(oc)
		util.CreateRoute(oc, ns, "http", unsecSvcName, unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unsecSvcName, "default")

		compat_otp.By("3.0: Set a cookie name to the route by the annotation, and then ensure the change in haproxy")
		util.SetAnnotation(oc, ns, "route/"+unsecSvcName, "router.openshift.io/cookie_name=unsecure-cookie_1")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, "be_http:"+ns+":"+unsecSvcName, []string{"unsecure-cookie_1"})

		compat_otp.By("4.0: Curl the http route, make sure the second server is hit and the cookie is set in the client side")
		curlCmd := fmt.Sprintf(`curl http://%s -sv --connect-timeout 10`, routehost)
		expectOutput := []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		output, _ := util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(output).To(o.MatchRegexp(`(?i)set-cookie: unsecure-cookie_1=[0-9a-zA-Z]+`))
	})

	//Author:mjoseph@redhat.com
	g.It("router.openshift.io/cookie-same-site route annotation accepts None Lax or Strict attribute for edge routes", g.Label("OCP-35547"), func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !util.IsCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create a server pod and the service")
		ns := oc.Namespace()
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("2.0: Create an edge route")
		routehost := "edge35547.apps." + util.GetBaseDomain(oc)
		util.CreateRoute(oc, ns, "edge", "edge35547", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "edge35547", "default")

		compat_otp.By("3.0: Curl the edge route, and check set-cookie header, expect getting SameSite=None")
		curlCmd := fmt.Sprintf(`curl https://%s -sSkI --connect-timeout 10`, routehost)
		result, _ := util.RepeatCmdOnClient(oc, curlCmd, "set-cookie:", 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=None`))

		compat_otp.By("4.0: Add Strict annotation to the edge route, and then ensure the change in haproxy")
		util.SetAnnotation(oc, ns, "route/edge35547", "router.openshift.io/cookie-same-site=Strict")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, "be_edge_http:"+ns+":edge35547", []string{"SameSite=Strict"})

		compat_otp.By("5.0: Curl the edge route again, and check set-cookie header, expect getting SameSite=Strict")
		result, _ = util.RepeatCmdOnClient(oc, curlCmd, "set-cookie:", 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=Strict`))
	})

	//Author:mjoseph@redhat.com
	g.It("router.openshift.io/cookie-same-site route annotation accepts None Lax or Strict attribute for Reencrypt routes", g.Label("OCP-35548"), func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !util.IsCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			secsvcName          = "service-secure"
		)

		compat_otp.By("1.0: Create a server pod and the service")
		ns := oc.Namespace()
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("2.0: Create a reencrypt route")
		routehost := "reen35548.apps." + util.GetBaseDomain(oc)
		util.CreateRoute(oc, ns, "reencrypt", "reen35548", secsvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "reen35548", "default")

		compat_otp.By("3.0: Curl the reencrypt route, and check set-cookie header, expect getting SameSite=None")
		curlCmd := fmt.Sprintf(`curl https://%s -sSkI --connect-timeout 10`, routehost)
		result, _ := util.RepeatCmdOnClient(oc, curlCmd, "set-cookie:", 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=None`))

		compat_otp.By("4.0: Add Lax annotation to the reencrypt route, and then ensure the change in haproxy")
		util.SetAnnotation(oc, ns, "route/reen35548", "router.openshift.io/cookie-same-site=Lax")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, "be_secure:"+ns+":reen35548", []string{"SameSite=Lax"})

		compat_otp.By("5.0: Curl the reencrypt route again, and check set-cookie header, expect getting SameSite=Lax")
		result, _ = util.RepeatCmdOnClient(oc, curlCmd, "set-cookie:", 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=Lax`))

		compat_otp.By("6.0: Add Strict annotation to the reencrypt route, and then ensure the change in haproxy")
		util.SetAnnotation(oc, ns, "route/reen35548", "router.openshift.io/cookie-same-site=Strict")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, "be_secure:"+ns+":reen35548", []string{"SameSite=Strict"})

		compat_otp.By("7.0: Curl the reencrypt route for the 3rd time, and check set-cookie header, expect getting SameSite=Strict")
		result, _ = util.RepeatCmdOnClient(oc, curlCmd, "set-cookie:", 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=Strict`))
	})
})
