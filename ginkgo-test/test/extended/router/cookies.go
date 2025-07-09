package router

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	exutil "github.com/openshift/router/ginkgo-test/test/extended/util"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("route-cookies", exutil.KubeConfigPath())

	// includes: OCP-11903 haproxy cookies based sticky session for unsecure routes
	//           OCP-11679 Disable haproxy hash based sticky session for unsecure routes
	// author: hongli@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-11903-haproxy cookies based sticky session for unsecure routes", func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !isCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			fileDir             = "/tmp/OCP-11903-cookie"
			cookie              = fileDir + "/cookie11903"
		)

		exutil.By("1.0: Prepare file folder and file for testing")
		defer os.RemoveAll(fileDir)
		err := os.MkdirAll(fileDir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		updateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		exutil.By("2.0: Created two server pods and the service")
		ns := oc.Namespace()
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		exutil.By("3.0: Create a plain HTTP route")
		routehost := "unsecure11903" + ".apps." + getBaseDomain(oc)
		createRoute(oc, ns, "http", unsecSvcName, unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, unsecSvcName, "default")

		exutil.By("4.0: Curl the http route, make sure the second server is hit")
		curlCmd := fmt.Sprintf(`curl http://%s -s -c %s --connect-timeout 10`, routehost, cookie)
		expectOutput := []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		exutil.By("5.0: Curl the http route again, make sure the first server is hit")
		curlCmd = fmt.Sprintf(`curl http://%s -s --connect-timeout 10`, routehost)
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		exutil.By("6.0: Curl the http route with the specified cookie for 6 times, expect all are forwarded to the second server")
		curlCmd = fmt.Sprintf(`curl http://%s -s -b %s --connect-timeout 10`, routehost, cookie)
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result := repeatCmdOnClient(oc, curlCmd, expectOutput, 180, 6)
		o.Expect(result[1]).To(o.Equal(6))

		// OCP-11679
		exutil.By(`7.0: Disable haproxy hash based sticky session for the route by adding 'disable cookies' annotation to it`)
		setAnnotation(oc, ns, "route/"+unsecSvcName, "haproxy.router.openshift.io/disable_cookies=true")

		exutil.By("8.0: Check the disable cookies configuration in haproxy")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		backendStart := fmt.Sprintf(`backend be_http:%s:%s`, ns, unsecSvcName)
		waitConfigurationDisappearedInHaproxy(oc, routerpod, backendStart, `cookie .+httponly`)

		exutil.By("9.0: Curl the http route with the specified cookie for 6 times again, expect the requests are forwarded to the two servers")
		_, result = repeatCmdOnClient(oc, curlCmd, expectOutput, 180, 6)
		o.Expect(result[0] + result[1]).To(o.Equal(6))
		o.Expect(result[0]*result[1] > 0).To(o.BeTrue())
	})

	// author: hongli@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-12566-Cookie name should not use openshift prefix", func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !isCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			fileDir             = "/tmp/OCP-12566-cookie"
			cookie              = fileDir + "/cookie12566"
			routeName           = "unsecureroute2"
		)

		exutil.By("1.0: Prepare file folder and file for testing")
		defer os.RemoveAll(fileDir)
		err := os.MkdirAll(fileDir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("2.0: Create two server pods and the service")
		ns := oc.Namespace()
		updateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")
		createResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		exutil.By("3.0: Create a plain HTTP route")
		routehost := "unsecure12566" + ".apps." + getBaseDomain(oc)
		createRoute(oc, ns, "http", routeName, unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")

		exutil.By("4.0: Curl the http route, make sure one server is hit")
		curlCmd := fmt.Sprintf(`curl http://%s -s -c %s --connect-timeout 10`, routehost, cookie)
		expectOutput := []string{"Hello-OpenShift"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		exutil.By("5.0: Check the cookies which should not contain a OPENSHIFT prefix or a md5 hash")
		cmd := fmt.Sprintf(`cat %s`, cookie)
		cookiesOutput, err := exec.Command("bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(string(cookiesOutput)).NotTo(o.ContainSubstring("OPENSHIFT"))
		o.Expect(string(cookiesOutput)).NotTo(o.MatchRegexp(`\d+\.\d+\.\d+\.\d+`))
	})

	// author: hongli@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-15872-can set cookie name for unsecure routes by annotation", func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !isCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create two server pods and the service")
		updateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")
		ns := oc.Namespace()
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		exutil.By("2.0: Create a plain HTTP route")
		routehost := "unsecure15872" + ".apps." + getBaseDomain(oc)
		createRoute(oc, ns, "http", unsecSvcName, unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, unsecSvcName, "default")

		exutil.By("3.0: Set a cookie name to the route by the annotation, and then ensure the change in haproxy")
		setAnnotation(oc, ns, "route/"+unsecSvcName, "router.openshift.io/cookie_name=unsecure-cookie_1")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		readHaproxyConfig(oc, routerpod, "be_http:"+ns+":"+unsecSvcName, "-A20", "unsecure-cookie_1")

		exutil.By("4.0: Curl the http route, make sure the second server is hit and the cookie is set in the client side")
		curlCmd := fmt.Sprintf(`curl http://%s -sv --connect-timeout 10`, routehost)
		expectOutput := []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		output, _ := repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(output).To(o.MatchRegexp(`(s|S)et-(c|C)ookie: unsecure-cookie_1=[0-9a-zA-Z]+`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-35547-router.openshift.io/cookie-same-site route annotation accepts None Lax or Strict attribute for edge routes", func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !isCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Deploy a project with two server pods and the service")
		project1 := oc.Namespace()
		createResourceFromWebServer(oc, project1, testPodSvc, srvrcInfo)

		exutil.By("2.0: Create an edge route")
		routehost := "edge35547" + ".apps." + getBaseDomain(oc)
		createRoute(oc, project1, "edge", "edge35547", unsecSvcName, []string{"--hostname=" + routehost})
		waitForOutput(oc, project1, "route/edge35547", "{.status.ingress[0].conditions[0].status}", "True")

		exutil.By("3.0: Curl the edge route, and check set-cookie header, expect getting SameSite=None")
		curlCmd := fmt.Sprintf(`curl https://%s -sSkI  --connect-timeout 10`, routehost)
		expectOutput := []string{"set-cookie:"}
		result, _ := repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=None`))

		exutil.By("4.0: Add Strict annotation to the edge route, and then ensure the change in haproxy")
		setAnnotation(oc, project1, "route/edge35547", "router.openshift.io/cookie-same-site=Strict")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		readHaproxyConfig(oc, routerpod, "be_edge_http:"+project1+":edge35547", "-A20", "SameSite=Strict")

		exutil.By("5.0: Curl the edge route again, and check set-cookie header, expect getting SameSite=Strict")
		result, _ = repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=Strict`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-35548-router.openshift.io/cookie-same-site route annotation accepts None Lax or Strict attribute for Reencrypt routes", func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !isCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			secsvcName          = "service-secure"
		)

		exutil.By("1.0: Deploy a project with two server pods and the service")
		project1 := oc.Namespace()
		createResourceFromWebServer(oc, project1, testPodSvc, srvrcInfo)

		exutil.By("2.0: Create a reencrypt route")
		routehost := "reen35548" + ".apps." + getBaseDomain(oc)
		createRoute(oc, project1, "reencrypt", "reen35548", secsvcName, []string{"--hostname=" + routehost})
		waitForOutput(oc, project1, "route/reen35548", "{.status.ingress[0].conditions[0].status}", "True")

		exutil.By("3.0: Curl the reencrypt route, and check set-cookie header, expect getting SameSite=None")
		curlCmd := fmt.Sprintf(`curl https://%s -sSkI  --connect-timeout 10`, routehost)
		expectOutput := []string{"set-cookie:"}
		result, _ := repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=None`))

		exutil.By("4.0: Add Lax annotation to the reencrypt route, and then ensure the change in haproxy")
		setAnnotation(oc, project1, "route/reen35548", "router.openshift.io/cookie-same-site=Lax")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		readHaproxyConfig(oc, routerpod, "be_secure:"+project1+":reen35548", "-A20", "SameSite=Lax")

		exutil.By("5.0: Curl the reencrypt route again, and check set-cookie header, expect getting SameSite=Lax")
		result, _ = repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=Lax`))

		exutil.By("6.0: Add Strict annotation to the reencrypt route, and then ensure the change in haproxy")
		setAnnotation(oc, project1, "route/reen35548", "router.openshift.io/cookie-same-site=Strict")
		readHaproxyConfig(oc, routerpod, "be_secure:"+project1+":reen35548", "-A20", "SameSite=Strict")

		exutil.By("7.0: Curl the reencrypt route for the 3rd time, and check set-cookie header, expect getting SameSite=Strict")
		result, _ = repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(result).To(o.ContainSubstring(`Secure; SameSite=Strict`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Low-35549-router.openshift.io/cookie-same-site route annotation does not work with Passthrough routes", func() {
		// if the ingress canary route isn't accessable from outside, skip it
		if !isCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			secsvcName          = "service-secure"
		)

		exutil.By("1.0: Deploy a project with two server pods and the service")
		project1 := oc.Namespace()
		createResourceFromWebServer(oc, project1, testPodSvc, srvrcInfo)

		exutil.By("2.0: Create a passthrough route")
		routehost := "pass35549" + ".apps." + getBaseDomain(oc)
		createRoute(oc, project1, "passthrough", "pass35549", secsvcName, []string{"--hostname=" + routehost})
		waitForOutput(oc, project1, "route/pass35549", "{.status.ingress[0].conditions[0].status}", "True")

		exutil.By("3.0: Curl the passthrough route, and check the response headers, expect NOT getting set-cookie header with SameSite=None")
		curlCmd := fmt.Sprintf(`curl https://%s -sSkI  --connect-timeout 10`, routehost)
		expectOutput := []string{"HTTP.+200"}
		result, _ := repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(result).NotTo(o.ContainSubstring(`Secure; SameSite=None`))

		exutil.By("4.0: Add Lax annotation to the passthrough route, and then check there is NOT the cookie in haproxy")
		setAnnotation(oc, project1, "route/pass35549", "router.openshift.io/cookie-same-site=Lax")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		routeBackendCfg := getBlockConfig(oc, routerpod, "backend be_tcp:"+project1+":pass35549")
		o.Expect(routeBackendCfg).NotTo(o.MatchRegexp(` cookie: [0-9a-zA-Z]+`))

		exutil.By("5.0: Curl the passthrough route again, and check the response headers, expect NOT getting set-cookie header with SameSite=Lax")
		result, _ = repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		o.Expect(result).NotTo(o.ContainSubstring(`Secure; SameSite=Lax`))
	})
})
