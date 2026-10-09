package qe

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	util "github.com/openshift/router-tests-extension/test"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/origin/test/extended/util"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	clusterinfra "github.com/openshift/origin/test/extended/util/compat_otp/clusterinfra"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	oc := exutil.NewCLI("routes")

	// Incorporate OCP-10024, OCP-11883 and OCP-12122 into one
	// Test case creater: zzhao@redhat.com - OCP-10024 Route could NOT be updated after created
	// Test case creater: zzhao@redhat.com - OCP-11883 Be able to add more alias for service
	// Test case creater: zzhao@redhat.com - OCP-12122 Alias will be invalid after removing it
	g.It("Author:mjoseph-Critical-10024-Route could NOT be updated after created", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp2         = filepath.Join(buildPruningBaseDir, "subdomain-routes/route.yaml")
			unSecSvcName        = "service-unsecure"
			aliasRoute          = "service-unsecure2"
			edgeRoute           = "ocp10024-unsecure"
			rut                 = util.RouteDescription{
				Namespace: "",
				Domain:    "",
				SubDomain: "ocp10024",
				Template:  customTemp2,
			}
		)

		compat_otp.By("1: Create an edge route using the route yaml file")
		ns := oc.Namespace()
		baseDomain := util.GetBaseDomain(oc)
		rut.Domain = "apps" + "." + baseDomain
		rut.Namespace = ns
		rut.Create(oc)
		util.GetRoutes(oc, ns)
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")

		compat_otp.By("2: Try to update the hostname for route using a test user and confirm it is not possible")
		patchOutput, err := oc.WithoutNamespace().Run("patch").Args("route/"+edgeRoute, "-p", "{\"spec\":{\"host\":\"www.changeroute.com\"}}", "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(patchOutput).To(o.ContainSubstring(`spec.host: Invalid value: "www.changeroute.com"`))

		// OCP-11883: Be able to add more alias for service
		compat_otp.By("3: Create a server and its service")
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("4: Create a http route using the service-unsecure service")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.CreateRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unSecSvcName, "default")
		backendName1 := "be_http:" + ns + ":service-unsecure"
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backendName1, []string{"service-unsecure:http"})

		compat_otp.By("5: Create another http route (alias) using the same service")
		util.CreateRoute(oc, ns, "http", aliasRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, aliasRoute, "default")
		util.GetRoutes(oc, ns)
		backendName2 := "be_http:" + ns + ":service-unsecure2"
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backendName2, []string{"service-unsecure:http"})

		// OCP-12122 Alias will be invalid after removing it
		compat_otp.By("6: Delete the alias route and verify that route is not accessible")
		err = oc.AsAdmin().Run("delete").Args("-n", ns, "route", aliasRoute).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", ns, aliasRoute, "-ojsonpath={.status.ingress[?(@.routerName==\"default\")].conditions[*].status}").Output()
		o.Expect(routeOutput).To(o.ContainSubstring(`routes.route.openshift.io "service-unsecure2" not found`))

		compat_otp.By("7: Confirming the alias route got removed from haproxy")
		waitErr := wait.PollImmediate(3*time.Second, 60*time.Second, func() (bool, error) {
			noBackendConfig := util.ReadRouterPodData(oc, routerpod, "cat haproxy.config", "be_http:"+ns)
			if !strings.Contains(noBackendConfig, aliasRoute) {
				return true, nil
			}
			e2e.Logf("Still waiting for the alias route to get removed from the haproxy")
			return false, nil
		})
		o.Expect(waitErr).NotTo(o.HaveOccurred(), "The alias route %s never get removed", aliasRoute)
	})

	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-10043-Set balance leastconn for passthrough routes", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			svcName             = "service-secure"
		)

		compat_otp.By("1.0 Create a server pod and its services")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2.0 Create a passthrough route")
		util.CreateRoute(oc, ns, "passthrough", "route-pass", svcName, []string{"--hostname=" + "passth10043" + ".apps." + util.GetBaseDomain(oc)})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-pass", "default")

		compat_otp.By(`3.0 Add the balance=leastconn annotation to the routes`)
		util.SetAnnotation(oc, ns, "route/route-pass", "haproxy.router.openshift.io/balance=leastconn")

		compat_otp.By(`4.0 Check the balance leastconn configuration in haproxy`)
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		backendStart := fmt.Sprintf("backend be_tcp:%s:%s", ns, "route-pass")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{"balance leastconn"})
	})

	// Bugzilla: 1368525
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Medium-10207-NetworkEdge Should use the same cookies for secure and insecure access when insecureEdgeTerminationPolicy set to allow for edge/reencrypt route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			podFileDir          = "/data/OCP-10207-cookie"
			fileDir             = "/tmp/OCP-10207-cookie"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp10207",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Prepare file folder and file for testing")
		defer os.RemoveAll(fileDir)
		err := os.MkdirAll(fileDir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		util.UpdateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		compat_otp.By("2.0: Create a client pod, two server pods and the service")
		ns := oc.Namespace()
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		// create the cookie folder in the client pod
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, clientPodName, "--", "mkdir", podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("3.0: Create a custom ingresscontroller and an edge route with insecure_policy Allow")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "edge10207" + "." + ingctrl.Domain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		util.CreateRoute(oc, ns, "edge", "route-edge10207", unSecSvcName, []string{"--hostname=" + routehost, "--insecure-policy=Allow"})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge10207", "default")

		compat_otp.By("4.0: Curl the edge route for two times, one with saving the cookie for the second server")
		routerpod := util.GetOneRouterPodNameByIC(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput := []string{"Hello-OpenShift " + srvPodList[0] + " http-8080"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-c", podFileDir + "/cookie-10207", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		compat_otp.By("5.0: Open the cookie file and check the contents")
		// access the cookie file and confirm that the output contains false and false
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, clientPodName+":"+podFileDir+"/cookie-10207", fileDir+"/cookie-10207").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.CheckCookieFile(fileDir+"/cookie-10207", "FALSE\t/\tFALSE")

		compat_otp.By("6.0: Curl the edge route with the cookie, expect forwarding to the second server")
		curlCmdWithCookie := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-b", podFileDir + "/cookie-10207", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result := util.RepeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[1]).To(o.Equal(6))

		compat_otp.By("7.0: Patch the edge route with Redirect tls insecureEdgeTerminationPolicy, then curl the edge route with the cookie, expect forwarding to the second server")
		util.PatchResourceAsAdmin(oc, ns, "route/route-edge10207", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Redirect"}}}`)
		toDst2 := routehost + ":80:" + podIP
		curlCmdWithCookie = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-ksSL", "-b", podFileDir + "/cookie-10207", "--resolve", toDst, "--resolve", toDst2, "--connect-timeout", "10"}
		_, result = util.RepeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[1]).To(o.Equal(6))

		compat_otp.By("8.0: Create a reencrypt route with Allow policy")
		reenhost := "reen10207" + "." + ingctrl.Domain
		toDst = reenhost + ":443:" + podIP
		toDst2 = reenhost + ":80:" + podIP
		util.CreateRoute(oc, ns, "reencrypt", "route-reen10207", secSvcName, []string{"--hostname=" + reenhost, "--insecure-policy=Allow"})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen10207", "default")

		compat_otp.By("9.0: Curl the route and generate a cookie file")
		curlCmdWithCookie = []string{"-n", ns, clientPodName, "--", "curl", "http://" + reenhost, "-ks", "-c", podFileDir + "/reen-cookie", "--resolve", toDst, "--resolve", toDst2, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " https-8443"}
		util.RepeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 60, 1)

		compat_otp.By("10.0: Open the cookie file and check the contents")
		// access the cookie file and confirm that the output contains false and false
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, clientPodName+":"+podFileDir+"/reen-cookie", fileDir+"/reen-cookie").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.CheckCookieFile(fileDir+"/reen-cookie", "FALSE\t/\tFALSE")
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-10660-Service endpoint can be work well if the mapping pod ip is updated", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			serverName          = "web-server-deploy"
		)

		compat_otp.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Check the service endpoints")
		epJsonPath := "{.subsets[0].addresses[0].ip}:{.subsets[0].ports[0].port}"
		epIPregExp := "([0-9]+.[0-9]+.[0-9]+.[0-9]+|[0-9a-zA-Z]+:[0-9a-zA-Z:]+)"
		epSearchOutput := util.WaitForOutputMatchRegexp(oc, ns, "endpoints/"+unSecSvcName, epJsonPath, epIPregExp)
		o.Expect(epSearchOutput).NotTo(o.ContainSubstring("NotMatch"))
		ep := util.GetByJsonPath(oc, ns, "endpoints/"+unSecSvcName, epJsonPath)

		compat_otp.By("3. Delete the server pod and check the endpoint")
		util.ScaleDeploy(oc, ns, serverName, 0)
		// there will not an ip assigned for the EP after the pod is removed
		noneEP := util.GetByJsonPath(oc, ns, "endpoints/"+unSecSvcName, epJsonPath)
		o.Expect(noneEP).To(o.ContainSubstring(":"))

		compat_otp.By("4. Create the pod again and recheck the service endpoints")
		util.ScaleDeploy(oc, ns, serverName, 1)
		epSearchOutput = util.WaitForOutputMatchRegexp(oc, ns, "endpoints/"+unSecSvcName, epJsonPath, epIPregExp)
		o.Expect(epSearchOutput).NotTo(o.ContainSubstring("NotMatch"))
		newEP := util.GetByJsonPath(oc, ns, "endpoints/"+unSecSvcName, epJsonPath)
		// the new IP assigned will be different from the old one
		o.Expect(newEP).NotTo(o.Equal(ep))
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Low-10943-NetworkEdge Set invalid timeout server for route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create single pod and the service")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		compat_otp.By("2.0: Create an unsecure route")

		util.CreateRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		compat_otp.By("3.0: Annotate unsecure route")
		util.SetAnnotation(oc, ns, "route/"+unSecSvcName, "haproxy.router.openshift.io/timeout=-2s")
		findAnnotation := util.GetAnnotation(oc, ns, "route", unSecSvcName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"-2s`))

		compat_otp.By("4.0: Check HAProxy file for timeout tunnel")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigNotContains(oc, routerpod, ns, []string{"timeout server  -2s"})
	})

	// Combine OCP-9651, OCP-9717
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-11036-NetworkEdge Set insecureEdgeTerminationPolicy to Redirect for passthrough/edge/reencrypt route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			SvcName             = "service-secure"
			unSecSvc            = "service-unsecure"
		)

		compat_otp.By("1.0: Create single pod, service and a passthrough/edge/reencrypt route")
		ns := oc.Namespace()
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvc), o.ContainSubstring(SvcName)))
		util.CreateRoute(oc, ns, "passthrough", "passthrough-route", SvcName, []string{})
		util.CreateRoute(oc, ns, "reencrypt", "reen-route", SvcName, []string{})
		util.CreateRoute(oc, ns, "edge", "edge-route", unSecSvc, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring("passthrough-route"), o.ContainSubstring("reen-route"), o.ContainSubstring("edge-route")))

		compat_otp.By("2.0: Add Redirect in tls")
		util.PatchResourceAsAdmin(oc, ns, "route/passthrough-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Redirect"}}}`)
		output, err = oc.Run("get").Args("route/passthrough-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		compat_otp.By("3.0: Test Route Http request is redirected to https")
		routehost := "passthrough-route-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.WaitForOutsideCurlContains("http://"+routehost, "-I -k", "location: https://"+routehost)
		util.WaitForOutsideCurlContains("http://"+routehost, "-L -k", "Hello-OpenShift "+srvPodList[0]+" https-8443")

		compat_otp.By("4.0: Attempt to update route policy to Allow")
		result, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/passthrough-route", "-p", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Allow"}}}`, "-n", ns).Output()
		o.Expect(result).To(o.ContainSubstring("invalid value for InsecureEdgeTerminationPolicy option, acceptable values are None, Redirect, or empty"))

		compat_otp.By("5.0: Add Redirect in reencrypt tls")
		util.PatchResourceAsAdmin(oc, ns, "route/reen-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Redirect"}}}`)
		output, err = oc.Run("get").Args("route/reen-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		compat_otp.By("6.0: Test Route Http request is redirected to https")
		reenhost := "reen-route-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.WaitForOutsideCurlContains("http://"+reenhost, "-I -k", "location: https://"+reenhost)
		util.WaitForOutsideCurlContains("http://"+reenhost, "-L -k", "Hello-OpenShift "+srvPodList[0]+" https-8443")

		compat_otp.By("7.0: Add Redirect in edge tls")
		util.PatchResourceAsAdmin(oc, ns, "route/edge-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Redirect"}}}`)
		output, err = oc.Run("get").Args("route/edge-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		compat_otp.By("8.0: Test Route Http request is redirected to https")
		edgehost := "edge-route-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.WaitForOutsideCurlContains("http://"+edgehost, "-I -k", "location: https://"+edgehost)
		util.WaitForOutsideCurlContains("http://"+edgehost, "-L -k", "Hello-OpenShift "+srvPodList[0]+" http-8080")

		compat_otp.By("9.0: Attempt to update route policy to invalid value")
		result, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/edge-route", "-p", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Abc"}}}`, "-n", ns).Output()
		o.Expect(result).To(o.ContainSubstring("invalid value for InsecureEdgeTerminationPolicy option, acceptable values are None, Allow, Redirect, or empty"))

	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Medium-11067-NetworkEdge oc help information should contain option wildcard-policy", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			svcName             = "service-secure"
		)

		compat_otp.By("1.0: Create single pod, service")
		ns := oc.Namespace()
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2.0: Check help section for expose service")
		output, err := oc.Run("expose").Args("service", svcName, "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("--wildcard-policy="))

		compat_otp.By("3.0: Check help section for edge route creation")
		output, err = oc.Run("create").Args("route", "edge", "route-edge", "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("--wildcard-policy="))

		compat_otp.By("4.0: Check help section for passthrough route creation")
		output, err = oc.Run("create").Args("route", "passthrough", "route-pass", "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("--wildcard-policy="))

		compat_otp.By("5.0: Check help section for reencrypt route creation")
		output, err = oc.Run("create").Args("route", "reencrypt", "route-reen", "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("--wildcard-policy="))
	})

	// Merges OCP-11042 to OCP-11130
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-11130-NetworkEdge Enable/Disable haproxy cookies based sticky session for edge termination routes", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			fileDir             = "/data/OCP-11130-cookie"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp11130",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Updated replicas in the web-server-deploy file for testing")
		util.UpdateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		compat_otp.By("2.0: Create a client pod, two server pods and the service")
		ns := oc.Namespace()
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		// create the cookie folder in the client pod
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, clientPodName, "--", "mkdir", fileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("3.0: Create an edge route")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "edge11130" + "." + ingctrl.Domain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		util.CreateRoute(oc, ns, "edge", "route-edge11130", unSecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge11130", "default")

		compat_otp.By("4.0: Curl the edge route, make sure saving the cookie for server 1")
		routerpod := util.GetOneRouterPodNameByIC(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-c", fileDir + "/cookie-11130", "--resolve", toDst, "--connect-timeout", "10"}

		expectOutput := []string{"Hello-OpenShift " + srvPodList[0] + " http-8080"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		compat_otp.By("5.0: Curl the edge route, make sure could get response from server 2")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		compat_otp.By("6.0: Curl the edge route with the cookie, expect all are forwarded to the server 1")
		curlCmdWithCookie := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-b", fileDir + "/cookie-11130", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result := util.RepeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[0]).To(o.Equal(6))

		// Disable haproxy hash based sticky session for edge termination routes
		compat_otp.By("7.0: Annotate the edge route with haproxy.router.openshift.io/disable_cookies=true")
		util.SetAnnotation(oc, ns, "route/route-edge11130", "haproxy.router.openshift.io/disable_cookies=true")

		compat_otp.By("8.0: Curl the edge route, and save the cookie for the backend server")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-c", fileDir + "/cookie-11130", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		compat_otp.By("9.0: Curl the edge route with the cookie, expect forwarding to the two server")
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result = util.RepeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 150, 15)
		o.Expect(result[0] > 0).To(o.BeTrue())
		o.Expect(result[1] > 0).To(o.BeTrue())
		o.Expect(result[0] + result[1]).To(o.Equal(15))
	})

	// Incorporate OCP-11619, OCP-10914 and OCP-11325 into one
	// Test case creater: bmeng@redhat.com - OCP-11619-Limit the number of TCP connection per IP in specified time period
	// Test case creater: yadu@redhat.com - OCP-10914: Protect from ddos by limiting TCP concurrent connection for route
	// Test case creater: hongli@redhat.com - OCP-11325: Limit the number of http request per ip
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-11619-Limit the number of TCP connection per IP in specified time period", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		)

		compat_otp.By("1. Create a server")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2. Create a passthrough route in the namespace")
		util.CreateRoute(oc, ns, "passthrough", "mypass", "service-secure", []string{})
		output := util.GetRoutes(oc, ns)
		o.Expect(output).To(o.ContainSubstring("mypass"))

		compat_otp.By("3. Check the reachability of the passthrough route")
		routehost := util.GetRouteHost(oc, ns, "mypass")
		curlCmd := fmt.Sprintf(`curl -k https://%s --connect-timeout 10`, routehost)
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4. Annotate the route to limit the TCP nums per ip and verify")
		util.SetAnnotation(oc, ns, "route/mypass", "haproxy.router.openshift.io/rate-limit-connections=true")
		util.SetAnnotation(oc, ns, "route/mypass", "haproxy.router.openshift.io/rate-limit-connections.rate-tcp=2")
		findAnnotation := util.GetAnnotation(oc, ns, "route", "mypass")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections: "true"`))
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections.rate-tcp: "2"`))

		compat_otp.By("5. Verify the haproxy configuration to ensure the tcp rate limit is configured")
		podName := util.GetOneRouterPodNameByIC(oc, "default")
		backendName := "be_tcp:" + ns + ":mypass"
		util.EnsureHaproxyBlockConfigContains(oc, podName, backendName, []string{"src_conn_rate", "tcp-request content reject if { src_conn_rate ge 2 }"})

		// OCP-10914: Protect from ddos by limiting TCP concurrent connection for route
		compat_otp.By("6. Expose a service in the namespace")
		util.CreateRoute(oc, ns, "http", "service-unsecure", "service-unsecure", []string{})
		output = util.GetRoutes(oc, ns)
		o.Expect(output).To(o.ContainSubstring("service-unsecure"))

		compat_otp.By("7. Check the reachability of the http route")
		routehost = util.GetRouteHost(oc, ns, "service-unsecure")
		curlCmd = fmt.Sprintf(`curl http://%s --connect-timeout 10`, routehost)
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 30, 1)

		compat_otp.By("8. Annotate the route to limit the concurrent TCP connections rate and verify")
		util.SetAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/rate-limit-connections=true")
		util.SetAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/rate-limit-connections.concurrent-tcp=2")
		findAnnotation = util.GetAnnotation(oc, ns, "route", "service-unsecure")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections: "true"`))
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections.concurrent-tcp: "2"`))

		compat_otp.By("9. Verify the haproxy configuration to ensure the tcp rate limit is configured")
		backendName1 := "be_http:" + ns + ":service-unsecure"
		util.EnsureHaproxyBlockConfigContains(oc, podName, backendName1, []string{"src_conn_cur", "tcp-request content reject if { src_conn_cur ge  2 }"})

		// OCP-11325: Limit the number of http request per ip
		compat_otp.By("10. Annotate the route to limit the http request nums per ip and verify")
		util.SetAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/rate-limit-connections.concurrent-tcp-")
		util.SetAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/rate-limit-connections.rate-http=3")
		findAnnotation = util.GetAnnotation(oc, ns, "route", "service-unsecure")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections: "true"`))
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections.rate-http: "3"`))

		compat_otp.By("11. Verify the haproxy configuration to ensure the http rate limit is configured")
		util.EnsureHaproxyBlockConfigContains(oc, podName, backendName1, []string{"src_http_req_rate", "tcp-request content reject if { src_http_req_rate ge 3 }"})
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-11635-NetworkEdge Set timeout server for passthough route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			secureSvcName       = "httpbin-svc-secure"
		)

		compat_otp.By("1.0: Create single pod and the service")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("2.0: Create a passthrough route")
		routeName := "route-passthrough11635"
		routehost := routeName + "-" + ns + ".apps." + util.GetBaseDomain(oc)

		util.CreateRoute(oc, ns, "passthrough", routeName, secureSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")

		compat_otp.By("3.0: Annotate passthrough route")
		util.SetAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=3s")
		findAnnotation := util.GetAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"3s`))

		compat_otp.By("4.0: Curl the edge route for two times, one with normal delay and other above timeout delay")
		util.WaitForOutsideCurlContains("https://"+routehost+"/delay/2", "-kI", `200 OK`)
		util.WaitForOutsideCurlContains("https://"+routehost+"/delay/5", "-kI", `exit status`)

		compat_otp.By("5.0: Check HAProxy file for timeout tunnel")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns, []string{routeName, "timeout tunnel  3s"})
	})

	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Medium-11728-haproxy hash based sticky session for tcp mode passthrough routes", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			secSvcName          = "service-secure"
			routeName           = "route-pass11728"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp11728",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0: Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.0: Updated replicas in the web-server-deploy file for testing")
		util.UpdateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		compat_otp.By("3.0: Create a client pod, two server pods and the service")
		ns := oc.Namespace()
		err := oc.WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("4.0: Create a passthrough route")
		routehost := routeName + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "passthrough", routeName, secSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")

		compat_otp.By("5.0: Check the passthrough route configuration in haproxy")
		routerpod := util.GetOneRouterPodNameByIC(oc, ingctrl.Name)
		backendStart := fmt.Sprintf(`backend be_tcp:%s:%s`, ns, routeName)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{routeName, "balance source", "hash-type consistent"})

		compat_otp.By("6.0: Curl the passthrough route, and save the output")
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-sk", "--resolve", toDst, "--connect-timeout", "10"}
		outputWithOneServer, _ := util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("7.0: Curl the passthrough route for 6 times, all are forwarded to the expected server")
		expectOutput := []string{"Hello-OpenShift " + srvPodList[0], "Hello-OpenShift " + srvPodList[1]}
		output, matchedList := util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 90, 6)
		o.Expect(output).To(o.ContainSubstring(outputWithOneServer))
		o.Expect(matchedList[0] + matchedList[1]).To(o.Equal(6))
		o.Expect(matchedList[0] * matchedList[1]).To(o.Equal(0))
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-11982-NetworkEdge Set timeout server for http route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			insecureSvcName     = "httpbin-svc-insecure"
		)

		compat_otp.By("1.0: Create single pod and the service")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(insecureSvcName))

		compat_otp.By("2.0: Create an http route")
		routeName := "route-http11982"
		routehost := routeName + "-" + ns + ".apps." + util.GetBaseDomain(oc)

		util.CreateRoute(oc, ns, "http", routeName, insecureSvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(routeName))

		compat_otp.By("3.0: Annotate http route")
		util.SetAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=2s")
		findAnnotation := util.GetAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"2s`))

		compat_otp.By("4.0: Curl the http route for two times, one with normal delay and other above timeout delay")
		util.WaitForOutsideCurlContains("http://"+routehost+"/delay/1", "-I", `200 OK`)
		// some proxies return "Gateway Timeout" but some return "Gateway Time-out"
		util.WaitForOutsideCurlContains("http://"+routehost+"/delay/5", "-I", `504 Gateway Time`)

		compat_otp.By("5.0: Check HAProxy file for timeout tunnel")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns, []string{routeName, "timeout server  2s"})
	})

	// Bug: 1374772
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-12091-haproxy config information should be clean when changing the service to another route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")
			webServerDeploy1    = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy1",
				SvcSecureName:   "service-secure1",
				SvcUnsecureName: "service-unsecure1",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy2 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy2",
				SvcSecureName:   "service-secure2",
				SvcUnsecureName: "service-unsecure2",
				Template:        webServerTemplate,
				Namespace:       "",
			}
			deploy1Label      = "name=" + webServerDeploy1.DeployName
			deploy2Label      = "name=" + webServerDeploy2.DeployName
			unsecureRouteName = "unsecure12091"

			ingctrl = util.IngressControllerDescription{
				Name:      "12091",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.0: Create a client pod and deploy two sets of web-server and services")
		ns := oc.Namespace()
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		webServerDeploy1.Namespace = ns
		webServerDeploy2.Namespace = ns
		webServerDeploy1.Create(oc)
		webServerDeploy2.Create(oc)
		util.EnsurePodWithLabelReady(oc, ns, deploy1Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy2Label)
		pod1Name := util.GetPodListByLabel(oc, ns, deploy1Label)[0]
		pod2Name := util.GetPodListByLabel(oc, ns, deploy2Label)[0]

		compat_otp.By("3.0: Create a unsecure route")
		routehost := unsecureRouteName + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", unsecureRouteName, webServerDeploy1.SvcUnsecureName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unsecureRouteName, ingctrl.Name)

		compat_otp.By("4.0: Add the balance=roundrobin annotation to the route, then check it in haproxy")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		backendStart := fmt.Sprintf(`backend be_http:%s:%s`, ns, unsecureRouteName)
		util.SetAnnotation(oc, ns, "route/"+unsecureRouteName, "haproxy.router.openshift.io/balance=roundrobin")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{"balance roundrobin"})

		compat_otp.By("5.0: Curl the http route, make sure the first server is hit")
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-s", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput := []string{"Hello-OpenShift " + pod1Name + " http-8080"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		compat_otp.By("6.0: Patch the http route with spec to another service")
		toAnotherService := fmt.Sprintf(`{"spec":{"to":{"name": "%s"}}}`, webServerDeploy2.SvcUnsecureName)
		util.PatchResourceAsAdmin(oc, ns, "route/"+unsecureRouteName, toAnotherService)

		compat_otp.By("7.0: Check the route configuration in haproxy, make sure the first service disappeared and the second service present")
		pod1IP := util.GetByJsonPath(oc, ns, "pod/"+pod1Name, `{.status.podIP}`)
		util.EnsureHaproxyBlockConfigNotContains(oc, routerpod, backendStart, []string{webServerDeploy1.SvcUnsecureName, pod1IP})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{webServerDeploy2.SvcUnsecureName})

		compat_otp.By("8.0: Curl the route for 10 times, all are forwarded to the second server")
		expectOutput = []string{"Hello-OpenShift " + pod1Name + " http-8080", "Hello-OpenShift " + pod2Name + " http-8080"}
		_, result := util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 180, 10)
		o.Expect(result[1]).To(o.Equal(10))
	})

	// Incorporate OCP-12506, OCP-15115 and OCP-16368 into one
	// Test case creater: hongli@redhat.com - OCP-12506: Hostname of componentRoutes should be RFC compliant
	// Test case creater: zzhao@redhat.com - OCP-15115: Harden haproxy to prevent the PROXY header from being passed for reencrypt route
	g.It("Author:mjoseph-High-12506-reencrypt route with no cert if a router is configured with a default wildcard cert", func() {
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		caCert := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")

		compat_otp.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		defaultContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, "default")
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Create a reen route")
		util.CreateRoute(oc, ns, "reencrypt", "12506-no-cert", "service-secure", []string{"--dest-ca-cert=" + caCert})
		util.GetRoutes(oc, ns)

		compat_otp.By("3. Confirm whether the destination certificate is present")
		util.WaitForOutputContains(oc, ns, "route/12506-no-cert", "{.spec.tls}", "destinationCACertificate")

		compat_otp.By("4. Check the router pod and ensure the routes are loaded in haproxy.config of default controller")
		util.EnsureHaproxyBlockConfigContains(oc, defaultContPod, ns, []string{"backend be_secure:" + ns + ":12506-no-cert"})

		compat_otp.By("5. Check the reachability of the host in the default controller")
		reenHost := "12506-no-cert-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.WaitForOutsideCurlContains("https://"+reenHost, "-k", `Hello-OpenShift web-server-deploy`)

		// OCP-15115: Harden haproxy to prevent the PROXY header from being passed for reencrypt route
		compat_otp.By("6. Access the route with 'proxy' header and confirm the proxy is carried with it")
		result := util.WaitForOutsideCurlContains("--head -H proxy:10.10.10.10 https://"+reenHost, "-k", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`proxy:10.10.10.10`))
	})

	// Incorporate OCP-12562 and OCP-12575 into one
	// Test case creater: hongli@redhat.com - OCP-12562 The path specified in route can work well for edge terminated
	// Test case creater: hongli@redhat.com - OCP-12575 The path specified in route can work well for unsecure
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-12562-The path specified in route can work well for edge/unsecure termination", func() {
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		unSecSvc := "service-unsecure"
		edgeRoute := "12562-edge"
		httpRoute := "12562-http"

		compat_otp.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		baseDomain := util.GetBaseDomain(oc)
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Create a edge route with path")
		edgeHost := edgeRoute + "-" + ns + ".apps." + baseDomain
		util.CreateRoute(oc, ns, "edge", edgeRoute, unSecSvc, []string{"--path=/test"})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")

		compat_otp.By("3. Curl the edge routes with and without path")
		util.WaitForOutsideCurlContains("https://"+edgeHost+"/test/", "-k", "Hello-OpenShift-Path-Test")
		util.WaitForOutsideCurlContains("https://"+edgeHost, "-k", "Application is not available")

		compat_otp.By("4. Remove path and check the reachability of the edge route without path")
		util.PatchResourceAsAdmin(oc, ns, "route/"+edgeRoute, `{"spec":{"path": ""}}`)
		pathValue := util.GetByJsonPath(oc, ns, "route/"+edgeRoute, `{.spec.path}`)
		o.Expect(pathValue).To(o.BeEmpty(), "expected path to be removed from edge route")
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")
		util.WaitForOutsideCurlContains("https://"+edgeHost, "-k", "Hello-OpenShift")

		compat_otp.By("5. Re-add the path and again check the reachability of the edge route with path")
		util.PatchResourceAsAdmin(oc, ns, "route/"+edgeRoute, `{"spec":{"path": "/test"}}`)
		pathValue = util.GetByJsonPath(oc, ns, "route/"+edgeRoute, `{.spec.path}`)
		o.Expect(pathValue).To(o.Equal("/test"), "expected path /test to be applied to edge route")
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")
		output := util.GetByJsonPath(oc, ns, "route/"+edgeRoute, `{.items[*].status.ingress[?(@.routerName=="default")].conditions[*].reason}`)
		o.Expect(output).NotTo(o.ContainSubstring("HostAlreadyClaimed"))
		util.WaitForOutsideCurlContains("https://"+edgeHost+"/test/", "-k", "Hello-OpenShift-Path-Test")

		// OCP-12575: The path specified in route can work well for unsecure
		compat_otp.By("6. Create a http route")
		httpHost := httpRoute + "-" + ns + ".apps." + baseDomain
		util.CreateRoute(oc, ns, "http", httpRoute, unSecSvc, []string{"--path=/test"})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, httpRoute, "default")

		compat_otp.By("7. Curl the edge route and with and without path")
		util.WaitForOutsideCurlContains("http://"+httpHost+"/test/", "-k", "Hello-OpenShift-Path-Test")
		util.WaitForOutsideCurlContains("http://"+httpHost, "-k", "Application is not available")

		compat_otp.By("8. Remove path and check the reachability of the http route without path")
		util.PatchResourceAsAdmin(oc, ns, "route/"+httpRoute, `{"spec":{"path": ""}}`)
		pathValue = util.GetByJsonPath(oc, ns, "route/"+httpRoute, `{.spec.path}`)
		o.Expect(pathValue).To(o.BeEmpty(), "expected path to be removed from http route")
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, httpRoute, "default")
		util.WaitForOutsideCurlContains("http://"+httpHost, "-k", "Hello-OpenShift")

		compat_otp.By("9. Re-add the path and again check the reachability of the http route with path")
		util.PatchResourceAsAdmin(oc, ns, "route/"+httpRoute, `{"spec":{"path": "/test"}}`)
		pathValue = util.GetByJsonPath(oc, ns, "route/"+httpRoute, `{.spec.path}`)
		o.Expect(pathValue).To(o.Equal("/test"), "expected path /test to be applied to http route")
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, httpRoute, "default")
		output1 := util.GetByJsonPath(oc, ns, "route/"+httpRoute, `{.items[*].status.ingress[?(@.routerName=="default")].conditions[*].reason}`)
		o.Expect(output1).NotTo(o.ContainSubstring("HostAlreadyClaimed"))
		util.WaitForOutsideCurlContains("http://"+httpHost+"/test/", "-k", "Hello-OpenShift-Path-Test")
	})

	// Test case creater: hongli@redhat.com
	g.It("Author:mjoseph-Critical-12564-The path specified in route can work well for reencrypt terminated", func() {
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		caCert := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")

		compat_otp.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		defaultContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, "default")
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Create a reen route")
		util.CreateRoute(oc, ns, "reencrypt", "12564-reencrypt", "service-secure", []string{"--dest-ca-cert=" + caCert, "--path=/test"})
		util.GetRoutes(oc, ns)

		compat_otp.By("3. Confirm whether the destination certificate is present")
		util.WaitForOutputContains(oc, ns, "route/12564-reencrypt", "{.spec.tls}", "destinationCACertificate")

		compat_otp.By("4. Check the router pod and ensure the routes are loaded in haproxy.config of default controller")
		util.EnsureHaproxyBlockConfigContains(oc, defaultContPod, ns, []string{"backend be_secure:" + ns + ":12564-reencrypt"})

		compat_otp.By("5. Check the reachability of the host in the specified path")
		reenHostWithPath := "12564-reencrypt-" + ns + ".apps." + util.GetBaseDomain(oc) + "/test/"
		util.WaitForOutsideCurlContains("https://"+reenHostWithPath, "-k", `Hello-OpenShift-Path-Test web-server-deploy`)

		compat_otp.By("6. Check the reachability of the host in the default controller")
		reenHostWithOutPath := "12564-reencrypt-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.WaitForOutsideCurlContains("https://"+reenHostWithOutPath, "-kI", "503 Service Unavailable")
	})

	// Incorporate OCP-12652, OCP-12556 and OCP-13248 into one
	// Test case creater: zzhao@redhat.com - OCP-12652 The later route should be HostAlreadyClaimed when there is a same host exist
	// Test case creater: zzhao@redhat.com - OCP-12556 Create a route without host named
	// Test case creater: zzhao@redhat.com - OCP-13248 The hostname should be converted to available route when met special character
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-12652-The later route should be HostAlreadyClaimed when there is a same host exist", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			unsecureRoute       = "route.12652"
			httpRoute           = "route.edge"
			reenRoute           = "route.reen"
			passthroughRoute    = "route.pass"
			e2eTestNamespace2   = "e2e-ne-ocp22652-" + util.GetRandomString()
		)

		compat_otp.By("1. Create an additional namespace for this scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		e2eTestNamespace1 := oc.Namespace()
		baseDomain := util.GetBaseDomain(oc)
		httpRoutehost1 := "route-12652-" + e2eTestNamespace1 + ".apps." + baseDomain
		httpRoutehost2 := "route-12652-" + e2eTestNamespace2 + ".apps." + baseDomain

		compat_otp.By("2. Create a server pod and an unsecure service in one ns")
		util.CreateResourceFromWebServer(oc, e2eTestNamespace1, testPodSvc, srvrcInfo)

		compat_otp.By("3: Create a http and edge route")
		// Http route is created without hostname
		util.CreateRoute(oc, e2eTestNamespace1, "http", unsecureRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, unsecureRoute, "default")
		util.CreateRoute(oc, e2eTestNamespace1, "edge", httpRoute, unSecSvcName, []string{"--hostname=www.route-edge.com"})
		util.EnsureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, httpRoute, "default")

		compat_otp.By("4. Create a server pod and an unsecure service in the other ns")
		util.OperateResourceFromFile(oc, "create", e2eTestNamespace2, testPodSvc)
		util.EnsurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		compat_otp.By("5: Create a http and edge route in the other ns with same host name")
		// Http route is created without hostname
		_, err := oc.AsAdmin().WithoutNamespace().Run("expose").Args("-n", e2eTestNamespace2, "service", unSecSvcName, "--name="+unsecureRoute).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsureRouteIsAdmittedByIngressController(oc, e2eTestNamespace2, unsecureRoute, "default")
		_, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", e2eTestNamespace2, "route", "edge", httpRoute, "--service="+unSecSvcName, "--hostname=www.route-edge.com").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("6. Confirm the route in the second ns is shown as HostAlreadyClaimed")
		util.WaitForOutputContains(oc, e2eTestNamespace2, "route", `{.items[*].status.ingress[?(@.routerName=="default")].conditions[*].reason}`, "HostAlreadyClaimed")

		// OCP-12556 NetworkEdge Create a route without host named
		compat_otp.By("7: Check the http routes in both namespace are reachable without explicity configuring hostname")
		util.WaitForOutsideCurlContains("http://"+httpRoutehost1, "", `Hello-OpenShift web-server-deploy`)
		util.WaitForOutsideCurlContains("http://"+httpRoutehost2, "", `Hello-OpenShift web-server-deploy`)

		// OCP-13248 The hostname should be converted to available route when met special character
		compat_otp.By("8: Create passthrough and reen route in first namespace")
		util.CreateRoute(oc, e2eTestNamespace1, "passthrough", passthroughRoute, secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, passthroughRoute, "default")
		util.CreateRoute(oc, e2eTestNamespace1, "reencrypt", reenRoute, secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, reenRoute, "default")

		compat_otp.By("9: Check these routes whose names have '.' decoded to '-'")
		output := util.GetRoutes(oc, e2eTestNamespace1)
		o.Expect(output).To(o.And(o.ContainSubstring("route-reen"), o.ContainSubstring("route-edge"), o.ContainSubstring("route-pass"), o.ContainSubstring("route-12652")))
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-13753-NetworkEdge Check the cookie if using secure mode when insecureEdgeTerminationPolicy to Redirect for edge/reencrypt route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			SvcName             = "service-secure"
			fileDir             = "/tmp/OCP-13753-cookie"
		)

		compat_otp.By("1.0: Prepare file folder and file for testing")
		defer os.RemoveAll(fileDir)
		err := os.MkdirAll(fileDir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("2.0: Create two server pods and the service")
		ns := oc.Namespace()
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("3.0: Create an edge and reencrypt route with insecure_policy Redirect")
		edgehost := "edge-route-" + ns + ".apps." + util.GetBaseDomain(oc)
		reenhost := "reen-route-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.CreateRoute(oc, ns, "edge", "edge-route", unSecSvcName, []string{"--insecure-policy=Redirect"})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "edge-route", "default")
		output, err := oc.Run("get").Args("route/edge-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		util.CreateRoute(oc, ns, "reencrypt", "reen-route", SvcName, []string{"--insecure-policy=Redirect"})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "reen-route", "default")
		output, err = oc.Run("get").Args("route/reen-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		compat_otp.By("4.0: Curl the edge route and generate a cookie file")
		util.WaitForOutsideCurlContains("http://"+edgehost, "-v -L -k -c "+fileDir+"/edge-cookie", "Hello-OpenShift "+srvPodList[0]+" http-8080")

		compat_otp.By("5.0: Open the cookie file and check the contents")
		// access the cookie file and confirm that the output contains false and true
		util.CheckCookieFile(fileDir+"/edge-cookie", "FALSE\t/\tTRUE")

		compat_otp.By("6.0: Curl the reencrypt route and generate a cookie file")
		util.WaitForOutsideCurlContains("http://"+reenhost, "-v -L -k -c "+fileDir+"/reen-cookie", "Hello-OpenShift "+srvPodList[0]+" https-8443")

		compat_otp.By("7.0: Open the cookie file and check the contents")
		// access the cookie file and confirm that the output contains false and true
		util.CheckCookieFile(fileDir+"/reen-cookie", "FALSE\t/\tTRUE")

	})

	// Combine OCP-9650
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-13839-NetworkEdge Set insecureEdgeTerminationPolicy to Allow for reencrypt/edge route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			SvcName             = "service-secure"
			unSecSvc            = "service-unsecure"
		)

		compat_otp.By("1.0: Create single pod, service and reencrypt and edge route")
		ns := oc.Namespace()
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvc), o.ContainSubstring(SvcName)))
		util.CreateRoute(oc, ns, "reencrypt", "reen-route", SvcName, []string{})
		util.CreateRoute(oc, ns, "edge", "edge-route", unSecSvc, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring("reen-route"), o.ContainSubstring("edge-route")))

		compat_otp.By("2.0: Add Allow policy in tls")
		util.PatchResourceAsAdmin(oc, ns, "route/reen-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Allow"}}}`)
		o.Expect(util.GetByJsonPath(oc, ns, "route/reen-route", `{.spec.tls.insecureEdgeTerminationPolicy}`)).To(o.Equal("Allow"))

		compat_otp.By("3.0: Test Route is accessible using http and https")
		routehost := "reen-route-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.WaitForOutsideCurlContains("http://"+routehost, "-k", "Hello-OpenShift "+srvPodList[0]+" https-8443 default")
		util.WaitForOutsideCurlContains("https://"+routehost, "-k", "Hello-OpenShift "+srvPodList[0]+" https-8443 default")

		compat_otp.By("4.0: Add Allow in edge tls")
		util.PatchResourceAsAdmin(oc, ns, "route/edge-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Allow"}}}`)
		o.Expect(util.GetByJsonPath(oc, ns, "route/edge-route", `{.spec.tls.insecureEdgeTerminationPolicy}`)).To(o.Equal("Allow"))

		compat_otp.By("5.0: Test Route is accessible using http and https")
		edgehost := "edge-route-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.WaitForOutsideCurlContains("http://"+edgehost, "-k", "Hello-OpenShift "+srvPodList[0]+" http-8080")
		util.WaitForOutsideCurlContains("https://"+edgehost, "-k", "Hello-OpenShift "+srvPodList[0]+" http-8080")

	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-14678-NetworkEdge Only the host in whitelist could access unsecure/edge/reencrypt/passthrough routes", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			unSecSvcName        = "service-unsecure"
			signedPod           = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		)

		compat_otp.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.CreateResourceFromFile(oc, ns, signedPod)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2.0: Create an unsecure, edge, reencrypt and passthrough route")
		domain := util.GetIngressctlDomain(oc, "default")
		unsecureRoute := "route-unsecure"
		unsecureHost := unsecureRoute + "-" + ns + "." + domain
		edgeRoute := "route-edge"
		edgeHost := edgeRoute + "-" + ns + "." + domain
		passthroughRoute := "route-passthrough"
		passthroughHost := passthroughRoute + "-" + ns + "." + domain
		reenRoute := "route-reen"
		reenHost := reenRoute + "-" + ns + "." + domain

		util.CreateRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")
		util.CreateRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		util.CreateRoute(oc, ns, "passthrough", passthroughRoute, "service-secure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-passthrough", "default")
		util.CreateRoute(oc, ns, "reencrypt", reenRoute, "service-secure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		compat_otp.By("3.0: Annotate unsecure, edge, reencrypt and passthrough route")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation := util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))
		util.SetAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))
		util.SetAnnotation(oc, ns, "route/"+passthroughRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))
		util.SetAnnotation(oc, ns, "route/"+reenRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))

		compat_otp.By("4.0: access the routes using the IP from the whitelist")
		util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift web-server-deploy`)
		util.WaitForOutsideCurlContains("https://"+edgeHost, "-k", `Hello-OpenShift web-server-deploy`)
		util.WaitForOutsideCurlContains("https://"+passthroughHost, "-k", `Hello-OpenShift web-server-deploy`)
		util.WaitForOutsideCurlContains("https://"+reenHost, "-k", `Hello-OpenShift web-server-deploy`)

		compat_otp.By("5.0: re-annotate routes with a random IP")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=5.6.7.8`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"5.6.7.8`))
		util.SetAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_whitelist=5.6.7.8`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"5.6.7.8`))
		util.SetAnnotation(oc, ns, "route/"+passthroughRoute, `haproxy.router.openshift.io/ip_whitelist=5.6.7.8`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"5.6.7.8`))
		util.SetAnnotation(oc, ns, "route/"+reenRoute, `haproxy.router.openshift.io/ip_whitelist=5.6.7.8`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"5.6.7.8`))

		compat_otp.By("6.0: attempt to access the routes without an IP in the whitelist")
		cmd := fmt.Sprintf(`curl --connect-timeout 10 -s %s %s 2>&1`, "-I", "http://"+unsecureHost)
		result, _ := exec.Command("bash", "-c", cmd).Output()
		// use -I for 2 different scenarios, squid result has failure bad gateway, otherwise uses exit status
		if strings.Contains(string(result), `squid`) {
			util.WaitForOutsideCurlContains("http://"+unsecureHost, "-I", `Bad Gateway`)
		} else {
			util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `exit status`)
		}
		util.WaitForOutsideCurlContains("https://"+edgeHost, "-k", `exit status`)
		util.WaitForOutsideCurlContains("https://"+passthroughHost, "-k", `exit status`)
		util.WaitForOutsideCurlContains("https://"+reenHost, "-k", `exit status`)

		compat_otp.By("7.0: Check HaProxy if the IP in the whitelist annotation exists")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src 5.6.7.8"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+edgeRoute, []string{"acl allowlist src 5.6.7.8"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+passthroughRoute, []string{"acl allowlist src 5.6.7.8"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+reenRoute, []string{"acl allowlist src 5.6.7.8"})
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Low-14680-NetworkEdge Add invalid value in annotation whitelist to route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2.0: Create an unsecure, route")
		unsecureRoute := "route-unsecure"
		unsecureHost := unsecureRoute + "-" + ns + ".apps." + util.GetBaseDomain(oc)

		util.CreateRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")

		compat_otp.By("3.0: Annotate route with invalid whitelist value")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist='192.abc.123.0'`)
		findAnnotation := util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"'192.abc.123.0'"`))

		compat_otp.By("4.0: access the route using any host since whitelist is not in effect")
		util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift web-server-deploy`)

		compat_otp.By("5.0: re-annotate route with IP that all Hosts can access")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0`))

		compat_otp.By("6.0: all hosts can access the route")
		util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift web-server-deploy`)

		compat_otp.By("7.0: Check HaProxy if the IP in the whitelist annotation exists")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src 0.0.0.0/0"})
	})

	// Incorporate OCP-15028 OCP-15071 OCP-15072 OCP-15073 into one
	// Test case creater: zzhao@redhat.com - OCP-15028: The router can do a case-insensitive match of a hostname for unsecure route
	// Test case creater: zzhao@redhat.com - OCP-15071: The router can do a case-insensitive match of a hostname for edge route
	// Test case creater: zzhao@redhat.com - OCP-15072: The router can do a case-insensitive match of a hostname for passthrough route
	// Test case creater: zzhao@redhat.com - OCP-15073: The router can do a case-insensitive match of a hostname for reencrypt route
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-15028-router can do a case-insensitive match of a hostname for unsecure/edge/passthrough/reencrypt route", func() {
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		caCert := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")
		UnsecureSvcName := "service-unsecure"
		SecureSvcName := "service-secure"

		compat_otp.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		baseDomain := util.GetBaseDomain(oc)
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Create a unsecure route and ensure the routes are loaded in haproxy.config of default controller")
		util.CreateRoute(oc, ns, "http", "15028-http", UnsecureSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "15028-http", "default")
		httpHostCapital := "15028-HTTP-" + ns + ".apps." + baseDomain

		compat_otp.By("3. Create a edge route and ensure the routes are loaded in haproxy.config of default controller")
		util.CreateRoute(oc, ns, "edge", "15028-edge", UnsecureSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "15028-edge", "default")
		edgeHostCapital := "15028-EDGE-" + ns + ".apps." + baseDomain

		compat_otp.By("4. Create a passthrough route and ensure the routes are loaded in haproxy.config of default controller")
		util.CreateRoute(oc, ns, "passthrough", "15028-pass", SecureSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "15028-pass", "default")
		passHostCapital := "15028-PASS-" + ns + ".apps." + baseDomain

		compat_otp.By("5. Create a reen route and ensure the routes are loaded in haproxy.config of default controller")
		util.CreateRoute(oc, ns, "reencrypt", "15028-reen", SecureSvcName, []string{"--dest-ca-cert=" + caCert})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "15028-reen", "default")
		reenHostCapital := "15028-REEN-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.GetRoutes(oc, ns)

		// OCP-15028: The router can do a case-insensitive match of a hostname for unsecure route
		compat_otp.By("6. Check the reachability of case-insensitive match of the hostname for the unsecure route")
		util.WaitForOutsideCurlContains("http://"+httpHostCapital, "-k", `Hello-OpenShift web-server-deploy`)

		// OCP-15071: The router can do a case-insensitive match of a hostname for edge route
		compat_otp.By("7. Check the reachability of case-insensitive match of the hostname for the edge route")
		util.WaitForOutsideCurlContains("https://"+edgeHostCapital, "-k", `Hello-OpenShift web-server-deploy`)

		// OCP-15072: The router can do a case-insensitive match of a hostname for passthrough route
		compat_otp.By("8. Check the reachability of case-insensitive match of the hostname for the passthrough route")
		util.WaitForOutsideCurlContains("https://"+passHostCapital, "-k", `Hello-OpenShift web-server-deploy`)

		// OCP-15073: The router can do a case-insensitive match of a hostname for reencrypt route
		compat_otp.By("9. Check the reachability of case-insensitive match of the hostname for the reencrypt route")
		util.WaitForOutsideCurlContains("https://"+reenHostCapital, "-k", `Hello-OpenShift web-server-deploy`)
	})

	// Merges OCP-15874 to OCP-15873
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-15873-NetworkEdge can set cookie name for edge/reen routes by annotation", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			fileDir             = "/data/OCP-15873-cookie"
			ingctrl             = util.IngressControllerDescription{
				Name:      "15873",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Updated replicas in the web-server-signed-deploy.yaml for testing")
		util.UpdateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		compat_otp.By("2.0: Create a client pod, two server pods and the service")
		ns := oc.Namespace()
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		// create the cookie folder in the client pod
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, clientPodName, "--", "mkdir", fileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		compat_otp.By("3.0: Create an edge route")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "edge15873" + "." + ingctrl.Domain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		util.CreateRoute(oc, ns, "edge", "route-edge15873", unSecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge15873", "default")

		compat_otp.By("4.0: Set the cookie name by route annotation with router.openshift.io/cookie_name=2-edge_cookie")
		util.SetAnnotation(oc, ns, "route/route-edge15873", "router.openshift.io/cookie_name=2-edge_cookie")

		compat_otp.By("5.0: Curl the edge route, and check the Set-Cookie header is set")
		routerpod := util.GetOneRouterPodNameByIC(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-kvs", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput := []string{"set-cookie: 2-edge_cookie=[0-9a-z]+"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		compat_otp.By("6.0: Curl the edge route, saving the cookie for one server")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-c" + fileDir + "/cookie-15873", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		compat_otp.By("7.0: Curl the edge route with the cookie, expect all are forwarded to the desired server")
		curlCmdWithCookie := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-b", fileDir + "/cookie-15873", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result := util.RepeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[1]).To(o.Equal(6))

		// test for NetworkEdge can set cookie name for reencrypt routes by annotation
		compat_otp.By("8.0: Create a reencrypt route")
		routehost = "reen15873" + "." + ingctrl.Domain
		toDst = routehost + ":443:" + podIP
		util.CreateRoute(oc, ns, "reencrypt", "route-reen15873", secSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen15873", "default")

		compat_otp.By("9.0: Set the cookie name by route annotation with router.openshift.io/cookie_name=_reen-cookie3")
		util.SetAnnotation(oc, ns, "route/route-reen15873", "router.openshift.io/cookie_name=_reen-cookie3")

		compat_otp.By("10.0: Curl the reencrypt route, and check the Set-Cookie header is set")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-kv", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"set-cookie: _reen-cookie3=[0-9a-z]+"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		compat_otp.By("11.0: Curl the reen route, saving the cookie for one server")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-k", "-c", fileDir + "/cookie-15873", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[1] + " https-8443"}
		util.RepeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		compat_otp.By("12.0: Curl the reen route with the cookie, expect all are forwarded to the desired server")
		curlCmdWithCookie = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-b", fileDir + "/cookie-15873", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift +" + srvPodList[0] + " +https-8443", "Hello-OpenShift +" + srvPodList[1] + " +https-8443"}
		_, result = util.RepeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[1]).To(o.Equal(6))
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Medium-16732-NetworkEdge Check haproxy.config when overwriting 'timeout server' which was already specified", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create single pod and the service")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		compat_otp.By("2.0: Create an unsecure route")
		routeName := unSecSvcName

		util.CreateRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		compat_otp.By("3.0: Annotate unsecure route")
		util.SetAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=5s")
		findAnnotation := util.GetAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"5s`))

		compat_otp.By("4.0: Check HAProxy file for timeout server")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout server  5s"})

		// overwrite annotation with same parameter to check whether haProxy shows the same annotation twice
		compat_otp.By("5.0: Overwrite route annotation")
		util.SetAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=5s")

		compat_otp.By("6.0: Check HAProxy file again for timeout server and ensure it is not duplicated")
		searchOutput := util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout server  5s"})
		count := strings.Count(searchOutput, "timeout server  5s")
		o.Expect(count).To(o.Equal(1), "Expected 'timeout server  5s' to appear exactly once after overwrite, but found %d", count)
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-ROSA-OSD_CCS-ARO-Critical-17145-haproxy router support websocket via unsecure route", func() {
		// On AWS (non private) proxy cluster, the url `*.apps.<baseDomain>` is unreachable from a test client pod
		// See also https://issues.redhat.com/browse/OCPQE-30244
		if util.CheckProxy(oc) {
			g.Skip("Skipping on proxy cluster since no proxy ENV in the test client pod")
		}

		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "websocket-deploy.yaml")
			unsecSvcName        = "ws-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodLabel      = "app=hello-pod"
		)

		compat_otp.By("1: Create a client pod, a server and its services in a namespace")
		ns := oc.Namespace()
		baseDomain := util.GetBaseDomain(oc)
		// Client pod with websocket client tool
		util.UpdateFilebySedCmd(clientPod,
			"quay.io/openshifttest/nginx-alpine@sha256:cee6930776b92dc1e93b73f9e5965925d49cff3d2e91e1d071c2f0ff72cbca29",
			"quay.io/openshifttest/hello-sdn@sha256:c89445416459e7adea9a5a416b3365ed3d74f2491beb904d61dc8d1eb89a72a4")
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		// Server pod with websocket testing capablity
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=hello-websocket")

		compat_otp.By("2: Create a http route for the testing")
		routehost := "unsecure17145-" + ns + ".apps." + baseDomain
		util.CreateRoute(oc, ns, "http", "unsecure17145", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "unsecure17145", "default")

		compat_otp.By("3: Curl the http route to ensure server is ready")
		curlCmd := []string{"-n", ns, "hello-pod", "--", "curl", "http://" + routehost + "/echo", "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "not websocket protocol", 60, 1)

		compat_otp.By("4: Use the unsecure route for confirming the websocket is working")
		cmd := fmt.Sprintf("(echo WebsocketTesting ; sleep 20) | ws ws://%s/echo", routehost)
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, "hello-pod", "--", "bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		// The websocket will echo what ever input we give
		o.Expect(output).To(o.ContainSubstring(`< WebsocketTesting`))
	})

	// Combining OCP-18482 and OCP-18489 into one test
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-18482-NetworkEdge limits backend pod max concurrent connections for unsecure, edge, reen, passthrough route", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
		)

		compat_otp.By("1.0: Create single pod and the services")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvcName), o.ContainSubstring(secSvcName)))

		compat_otp.By("2.0: Create an unsecure, edge and reencrypt route")
		unsecureRoute := "route-unsecure"
		edgeRoute := "route-edge"
		reenRoute := "route-reen"
		passthroughRoute := "route-passthrough"

		util.CreateRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")
		util.CreateRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		util.CreateRoute(oc, ns, "reencrypt", reenRoute, secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")
		util.CreateRoute(oc, ns, "passthrough", passthroughRoute, secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-passthrough", "default")

		compat_otp.By("3.0: Annotate the routes with rate-limit annotations")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, "haproxy.router.openshift.io/pod-concurrent-connections=1")
		findAnnotation := util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"1`))
		util.SetAnnotation(oc, ns, "route/"+edgeRoute, "haproxy.router.openshift.io/pod-concurrent-connections=2")
		findAnnotation = util.GetAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"2`))
		util.SetAnnotation(oc, ns, "route/"+reenRoute, "haproxy.router.openshift.io/pod-concurrent-connections=3")
		findAnnotation = util.GetAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"3`))
		util.SetAnnotation(oc, ns, "route/"+passthroughRoute, "haproxy.router.openshift.io/pod-concurrent-connections=2")
		findAnnotation = util.GetAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"2`))

		compat_otp.By("4.0: Check HAProxy file for route rate-limit annotation")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, unsecureRoute, []string{"maxconn 1"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, edgeRoute, []string{"maxconn 2"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, reenRoute, []string{"maxconn 3"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, passthroughRoute, []string{"maxconn 2"})
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Medium-18490-NetworkEdge limits multiple backend pods max concurrent connections", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create single pod and its services")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		compat_otp.By("2.0: Scale deployment to have 2 pods")
		output, err = oc.AsAdmin().WithoutNamespace().Run("scale").Args("-n", ns, "deployment/web-server-deploy", "--replicas=2").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("web-server-deploy scaled"))
		util.WaitForOutputEquals(oc, ns, "deployment/web-server-deploy", "{.status.availableReplicas}", "2")

		compat_otp.By("3.0: Create an edge route")
		edgeRoute := "route-edge"
		util.CreateRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")

		compat_otp.By("4.0: Annotate the edge route with rate-limit annotation")
		util.SetAnnotation(oc, ns, "route/"+edgeRoute, "haproxy.router.openshift.io/pod-concurrent-connections=1")
		findAnnotation := util.GetAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"1`))

		compat_otp.By("5.0: Check HAProxy file for route rate-limit annotation")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		searchOutput := util.EnsureHaproxyBlockConfigContains(oc, routerpod, edgeRoute, []string{"maxconn 1"})
		count := strings.Count(searchOutput, "maxconn 1")
		o.Expect(count).To(o.Equal(2), "Expected the substring to appear exactly twice")
	})

	// Test case creater: zzhao@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Medium-19804-Unsecure route with path and another tls route with same hostname can work at the same time", func() {
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		unSecSvc := "service-unsecure"
		edgeRoute := "19804-edge"
		httpRoute := "19804-http"

		compat_otp.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		baseDomain := util.GetBaseDomain(oc)
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Create a http route")
		httpHost := edgeRoute + "-" + ns + ".apps." + baseDomain
		util.CreateRoute(oc, ns, "http", httpRoute, unSecSvc, []string{"--hostname=" + httpHost, "--path=/test"})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, httpRoute, "default")

		compat_otp.By("3. Create a edge route")
		edgeHost := edgeRoute + "-" + ns + ".apps." + baseDomain
		util.CreateRoute(oc, ns, "edge", edgeRoute, unSecSvc, []string{"--insecure-policy=Allow"})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")

		compat_otp.By("4. Access the route without path")
		util.GetRoutes(oc, ns)
		util.WaitForOutsideCurlContains("https://"+httpHost+"/test/", "-k", "Hello-OpenShift-Path-Test")

		compat_otp.By("5. Access the route with path")
		util.WaitForOutsideCurlContains("https://"+edgeHost, "-k", "Hello-OpenShift")
	})

	// Combining OCP-34106 and OCP-34168 into one
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-34106-NetworkEdge Routes annotated with 'haproxy.router.openshift.io/rewrite-target=/path' will replace and rewrite http request with specified '/path'", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create single pod, service")
		ns := oc.Namespace()
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2.0: Expose the service to create http unsecure route")
		util.CreateRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "service-unsecure", "default")

		compat_otp.By("3.0: Annotate unsecure route with path rewrite target")
		util.SetAnnotation(oc, ns, "route/"+unSecSvcName, `haproxy.router.openshift.io/rewrite-target=/path/second/`)
		findAnnotation := util.GetAnnotation(oc, ns, "route", unSecSvcName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/rewrite-target":"/path/second/`))

		compat_otp.By("4.0: Curl route to see if the route will rewrite to second path")
		domain := util.GetIngressctlDomain(oc, "default")
		unsecureHost := unSecSvcName + "-" + ns + "." + domain
		util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `second-test web-server-deploy`)

		compat_otp.By("5.0: Annotate unsecure route with rewrite target")
		util.SetAnnotation(oc, ns, "route/"+unSecSvcName, `haproxy.router.openshift.io/rewrite-target=/`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", unSecSvcName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/rewrite-target":"/`))

		compat_otp.By("6.0: Curl route with different post-fixes in the web-server app")
		util.WaitForOutsideCurlContains("http://"+unsecureHost+"/", "", `Hello-OpenShift web-server-deploy`)
		util.WaitForOutsideCurlContains("http://"+unsecureHost+"/test/", "", `Hello-OpenShift-Path-Test web-server-deploy`)
		util.WaitForOutsideCurlContains("http://"+unsecureHost+"/path/", "", `ocp-test web-server-deploy`)
		util.WaitForOutsideCurlContains("http://"+unsecureHost+"/path/second/", "", `second-test web-server-deploy`)

	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-38671-NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation gets applied alongside 'haproxy.router.openshift.io/timeout' for clear/edge/reencrypt routes", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create single pod and 3 services")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvcName), o.ContainSubstring("service-secure")))

		compat_otp.By("2.0: Create a clear HTTP, edge and reen route")
		routeName := unSecSvcName

		util.CreateRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		util.CreateRoute(oc, ns, "edge", "edge-route", unSecSvcName, []string{})
		util.CreateRoute(oc, ns, "reencrypt", "reen-route", "service-secure", []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvcName), o.ContainSubstring("edge-route"), o.ContainSubstring("reen-route")))

		compat_otp.By("3.0: Annotate all 3 routes")
		util.SetAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=15s")
		findAnnotation := util.GetAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"15s`))

		util.SetAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/timeout=15s")
		findAnnotation = util.GetAnnotation(oc, ns, "route", "edge-route")
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"15s`))

		util.SetAnnotation(oc, ns, "route/reen-route", "haproxy.router.openshift.io/timeout=15s")
		findAnnotation = util.GetAnnotation(oc, ns, "route", "reen-route")
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"15s`))

		compat_otp.By("4.0: Check HAProxy file for timeout server on the routes")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout server  15s"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":edge-route", []string{"timeout server  15s"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":reen-route", []string{"timeout server  15s"})

		compat_otp.By("5.0: Annotate all routes with timeout tunnel")
		util.SetAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout-tunnel=5s")
		findAnnotation = util.GetAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout-tunnel":"5s`))

		util.SetAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/timeout-tunnel=5s")
		findAnnotation = util.GetAnnotation(oc, ns, "route", "edge-route")
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout-tunnel":"5s`))

		util.SetAnnotation(oc, ns, "route/reen-route", "haproxy.router.openshift.io/timeout-tunnel=5s")
		findAnnotation = util.GetAnnotation(oc, ns, "route", "reen-route")
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout-tunnel":"5s`))

		compat_otp.By("6.0: Check HAProxy file for timeout tunnel on the routes")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout tunnel  5s"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout tunnel  5s"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout tunnel  5s"})
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-38672-NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation takes precedence over 'haproxy.router.openshift.io/timeout' values for passthrough routes", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			secSvcName          = "service-secure"
		)

		compat_otp.By("1.0: Create single pod, service")
		ns := oc.Namespace()
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2.0: Create a passthrough route")
		routeName := "38672-route-passth"
		util.CreateRoute(oc, ns, "passthrough", routeName, secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")

		compat_otp.By("3.0: Annotate passthrough route with two timeout annotations")
		util.SetAnnotation(oc, ns, "route/"+routeName, `haproxy.router.openshift.io/timeout=15s`)
		util.SetAnnotation(oc, ns, "route/"+routeName, `haproxy.router.openshift.io/timeout-tunnel=5s`)
		findAnnotation := util.GetAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"15s`))
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout-tunnel":"5s`))

		compat_otp.By("4.0: Check HaProxy to see if timeout tunnel overrides timeout")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeName, []string{"timeout tunnel  5s"})

		compat_otp.By("5.0: Remove the timeout tunnel annotation")
		util.SetAnnotation(oc, ns, "route/"+routeName, `haproxy.router.openshift.io/timeout-tunnel-`)

		compat_otp.By("6.0: Check Haproxy to see if timeout annotation is present")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeName, []string{"timeout tunnel  15s"})
	})

	g.It("Author:aiyengar-ROSA-OSD_CCS-ARO-Medium-42230-route can be configured to whitelist more than 61 ips/CIDRs", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			output              string
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		)
		compat_otp.By("Create pod, svc resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("expose a service in the namespace")
		util.CreateRoute(oc, ns, "http", "service-unsecure", "service-unsecure", []string{})
		output, err := oc.Run("get").Args("-n", ns, "route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("service-unsecure"))

		compat_otp.By("annotate the route with haproxy.router.openshift.io/ip_whitelist with 61 CIDR values and verify")
		util.SetAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/ip_whitelist=192.168.0.0/24 192.168.1.0/24 192.168.2.0/24 192.168.3.0/24 192.168.4.0/24 192.168.5.0/24 192.168.6.0/24 192.168.7.0/24 192.168.8.0/24 192.168.9.0/24 192.168.10.0/24 192.168.11.0/24 192.168.12.0/24 192.168.13.0/24 192.168.14.0/24 192.168.15.0/24 192.168.16.0/24 192.168.17.0/24 192.168.18.0/24 192.168.19.0/24 192.168.20.0/24 192.168.21.0/24 192.168.22.0/24 192.168.23.0/24 192.168.24.0/24 192.168.25.0/24 192.168.26.0/24 192.168.27.0/24 192.168.28.0/24 192.168.29.0/24 192.168.30.0/24 192.168.31.0/24 192.168.32.0/24 192.168.33.0/24 192.168.34.0/24 192.168.35.0/24 192.168.36.0/24 192.168.37.0/24 192.168.38.0/24 192.168.39.0/24 192.168.40.0/24 192.168.41.0/24 192.168.42.0/24 192.168.43.0/24 192.168.44.0/24 192.168.45.0/24 192.168.46.0/24 192.168.47.0/24 192.168.48.0/24 192.168.49.0/24 192.168.50.0/24 192.168.51.0/24 192.168.52.0/24 192.168.53.0/24 192.168.54.0/24 192.168.55.0/24 192.168.56.0/24 192.168.57.0/24 192.168.58.0/24 192.168.59.0/24 192.168.60.0/24")
		output, err = oc.Run("get").Args("-n", ns, "route", "service-unsecure", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/ip_whitelist"))

		compat_otp.By("verify the acl whitelist parameter inside router pod for whitelist with 61 CIDR values")
		podName := util.GetOneRouterPodNameByIC(oc, "default")
		//backendName is the leading context of the route
		backendName := "be_http:" + ns + ":service-unsecure"
		util.EnsureHaproxyBlockConfigContains(oc, podName, backendName, []string{"acl allowlist src 192.168.0.0/24", "tcp-request content reject if !allowlist"})
		util.EnsureHaproxyBlockConfigNotContains(oc, podName, backendName, []string{"acl allowlist src -f /var/lib/haproxy/router/allowlists/"})

		compat_otp.By("annotate the route with haproxy.router.openshift.io/ip_whitelist with more than 61 CIDR values and verify")
		util.SetAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/ip_whitelist=192.168.0.0/24 192.168.1.0/24 192.168.2.0/24 192.168.3.0/24 192.168.4.0/24 192.168.5.0/24 192.168.6.0/24 192.168.7.0/24 192.168.8.0/24 192.168.9.0/24 192.168.10.0/24 192.168.11.0/24 192.168.12.0/24 192.168.13.0/24 192.168.14.0/24 192.168.15.0/24 192.168.16.0/24 192.168.17.0/24 192.168.18.0/24 192.168.19.0/24 192.168.20.0/24 192.168.21.0/24 192.168.22.0/24 192.168.23.0/24 192.168.24.0/24 192.168.25.0/24 192.168.26.0/24 192.168.27.0/24 192.168.28.0/24 192.168.29.0/24 192.168.30.0/24 192.168.31.0/24 192.168.32.0/24 192.168.33.0/24 192.168.34.0/24 192.168.35.0/24 192.168.36.0/24 192.168.37.0/24 192.168.38.0/24 192.168.39.0/24 192.168.40.0/24 192.168.41.0/24 192.168.42.0/24 192.168.43.0/24 192.168.44.0/24 192.168.45.0/24 192.168.46.0/24 192.168.47.0/24 192.168.48.0/24 192.168.49.0/24 192.168.50.0/24 192.168.51.0/24 192.168.52.0/24 192.168.53.0/24 192.168.54.0/24 192.168.55.0/24 192.168.56.0/24 192.168.57.0/24 192.168.58.0/24 192.168.59.0/24 192.168.60.0/24 192.168.61.0/24")
		output1, err1 := oc.Run("get").Args("-n", ns, "route", "service-unsecure", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err1).NotTo(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("haproxy.router.openshift.io/ip_whitelist"))

		compat_otp.By("verify the acl whitelist parameter inside router pod for whitelist with 62 CIDR values")
		//backendName is the leading context of the route
		util.EnsureHaproxyBlockConfigContains(oc, podName, backendName, []string{`acl allowlist src -f /var/lib/haproxy/router/allowlists/` + ns + `:service-unsecure.txt`, `tcp-request content reject if !allowlist`})
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-45399-ingress controller continue to function normally with unexpected high timeout value", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			output              string
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		)
		compat_otp.By("Create pod, svc resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("expose a service in the namespace")
		util.CreateRoute(oc, ns, "http", "service-secure", "service-secure", []string{})
		output, err := oc.Run("get").Args("-n", ns, "route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("service-secure"))

		compat_otp.By("annotate the route with haproxy.router.openshift.io/timeout annotation to high value and verify")
		util.SetAnnotation(oc, ns, "route/service-secure", "haproxy.router.openshift.io/timeout=9999d")
		o.Expect(util.GetAnnotation(oc, ns, "route", "service-secure")).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"9999d`))

		compat_otp.By("Verify the haproxy configuration for the set timeout value")
		podName := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigContains(oc, podName, ns, []string{"timeout server  2147483647ms"})

		compat_otp.By("Verify the pod logs to see any timer overflow error messages")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", podName, "-c", "router").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(log).NotTo(o.ContainSubstring(`timer overflow`))
	})

	g.It("Author:hongli-ROSA-OSD_CCS-ARO-High-45741-ingress canary route redirects http to https", func() {
		var ns = "openshift-ingress-canary"
		compat_otp.By("get the ingress route host")
		canaryRouteHost := util.GetByJsonPath(oc, ns, "route/canary", "{.status.ingress[0].host}")
		o.Expect(canaryRouteHost).Should(o.ContainSubstring(`canary-openshift-ingress-canary.apps`))

		compat_otp.By("curl canary route via http and redirects to https")
		util.WaitForOutsideCurlContains("http://"+canaryRouteHost, "-I", "302 Found")
		util.WaitForOutsideCurlContains("http://"+canaryRouteHost, "-kL", "Healthcheck requested")
		util.WaitForOutsideCurlContains("https://"+canaryRouteHost, "-k", "Healthcheck requested")
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-49802-HTTPS redirect happens even if there is a more specific http-only", func() {
		// curling through default controller will not work for proxy cluster.
		if util.CheckProxy(oc) {
			g.Skip("This is proxy cluster, skip the test.")
		}
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "49802-route.yaml")
			rut                 = util.RouteDescription{
				Namespace: "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a pod")
		baseDomain := util.GetBaseDomain(oc)
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := util.GetPodListByLabel(oc, ns, "name=web-server-deploy")
		defaultContPod := util.GetOneRouterPodNameByIC(oc, "default")

		compat_otp.By("create routes and get the details")
		rut.Namespace = ns
		rut.Create(oc)
		util.GetRoutes(oc, ns)

		compat_otp.By("check the reachability of the secure route with redirection")
		util.WaitForCurl(oc, podName[0], baseDomain, "hello-pod-"+ns+".apps.", "HTTP/1.1 302 Found", "")
		util.WaitForCurl(oc, podName[0], baseDomain, "hello-pod-"+ns+".apps.", `location: https://hello-pod-`, "")

		compat_otp.By("check the reachability of the insecure routes")
		util.WaitForCurl(oc, podName[0], baseDomain+"/test/", "hello-pod-http-"+ns+".apps.", "HTTP/1.1 200 OK", "")

		compat_otp.By("check the reachability of the secure route")
		curlCmd := fmt.Sprintf("curl -I -k https://hello-pod-%s.apps.%s --connect-timeout 10", ns, baseDomain)
		statsOut, err := compat_otp.RemoteShPod(oc, ns, podName[0], "sh", "-c", curlCmd)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(statsOut).Should(o.ContainSubstring("HTTP/1.1 200 OK"))

		compat_otp.By("check the router pod and ensure the routes are loaded in haproxy.config")
		searchOutput := util.ReadRouterPodData(oc, defaultContPod, "cat haproxy.config", "hello-pod")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_edge_http:" + ns + ":hello-pod"))
		searchOutput1 := util.ReadRouterPodData(oc, defaultContPod, "cat haproxy.config", "hello-pod-http")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_http:" + ns + ":hello-pod-http"))
	})

	g.It("Author:mjoseph-Critical-53696-Route status should updates accordingly when ingress routes cleaned up [Disruptive]", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp53696",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("check the intial canary route status")
		util.EnsureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")

		compat_otp.By("shard the default ingress controller")
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		defer util.PatchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"type\":null}}}}")
		util.PatchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"type\":\"shard\"}}}}")
		// After patching the default congtroller generation should be +1
		actualGenerationInt, _ := strconv.Atoi(actualGen)
		util.EnsureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("check whether canary route status is cleared")
		util.CheckRouteDetailsRemoved(oc, "openshift-ingress-canary", "canary", "default")

		compat_otp.By("patch the controller back to default check the canary route status")
		util.PatchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"type\":null}}}}")
		util.EnsureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))
		util.EnsureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")

		compat_otp.By("Create a shard ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = "shard." + baseDomain
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("patch the shard controller and check the canary route status")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"nodePlacement\":{\"nodeSelector\":{\"matchLabels\":{\"node-role.kubernetes.io/worker\":\"\"}}}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		util.EnsureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")
		util.EnsureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", ingctrl.Name)

		compat_otp.By("delete the shard and check the status")
		custContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		ingctrl.Delete(oc)
		err3 := util.WaitForResourceToDisappear(oc, "openshift-ingress", "pod/"+custContPod)
		compat_otp.AssertWaitPollNoErr(err3, fmt.Sprintf("Router  %v failed to fully terminate", "pod/"+custContPod))
		util.EnsureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")
		util.CheckRouteDetailsRemoved(oc, "openshift-ingress-canary", "canary", ingctrl.Name)
	})

	// Cugzilla: 2021446
	// No ingress-operator pod on HyperShift guest cluster so this case is not available
	g.It("Author:mjoseph-NonHyperShiftHOST-High-55895-Ingress should be in degraded status when canary route is not available [Disruptive]", func() {
		compat_otp.By("Check the intial co/ingress and canary route status")
		util.EnsureClusterOperatorNormal(oc, "ingress", 1, 10)
		util.EnsureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")

		compat_otp.By("Check the reachability of the canary route")
		baseDomain := util.GetBaseDomain(oc)
		operatorPod := util.GetPodListByLabel(oc, "openshift-ingress-operator", "name=ingress-operator")
		routehost := "canary-openshift-ingress-canary.apps." + baseDomain
		cmdOnPod := []string{operatorPod[0], "-n", "openshift-ingress-operator", "--", "curl", "-k", "https://" + routehost, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, cmdOnPod, "Healthcheck requested", 30, 1)

		compat_otp.By("Patch the ingress controller and deleting the canary route")
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		defer util.EnsureClusterOperatorNormal(oc, "ingress", 3, 300)
		defer util.PatchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":null}}")
		util.PatchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"type\":\"default\"}}}}")
		// Deleting canary route
		err := oc.AsAdmin().Run("delete").Args("-n", "openshift-ingress-canary", "route", "canary").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		// After patching the default congtroller generation should be +1
		actualGenerationInt, _ := strconv.Atoi(actualGen)
		util.EnsureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("Check whether the canary route status cleared and confirm the route is not accessible")
		util.CheckRouteDetailsRemoved(oc, "openshift-ingress-canary", "canary", "default")
		cmdOnPod = []string{operatorPod[0], "-n", "openshift-ingress-operator", "--", "curl", "-Ik", "https://" + routehost, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, cmdOnPod, "503", 120, 1)

		// Wait may be about 300 seconds
		compat_otp.By("Check the ingress operator status to confirm it is in degraded state cause by canary route")
		jpath := "{.status.conditions[*].message}"
		util.WaitForOutputContains(oc, "default", "co/ingress", jpath, "The \"default\" ingress controller reports Degraded=True")
		util.WaitForOutputContains(oc, "default", "co/ingress", jpath, "Canary route is not admitted by the default ingress controller")
	})

	// Bugzilla: 1934904
	// Jira: OCPBUGS-9274
	// No openshift-machine-api namespace on HyperShift guest cluster so this case is not available
	g.It("Author:mjoseph-NonHyperShiftHOST-NonPreRelease-High-56240-Canary daemonset can schedule pods to both worker and infra nodes [Disruptive]", func() {
		var (
			infrastructureName = clusterinfra.GetInfrastructureName(oc)
			machineSetName     = infrastructureName + "-56240"
		)

		compat_otp.By("Check the intial machines and canary pod details")
		util.GetResourceName(oc, "openshift-machine-api", "machine")
		util.GetResourceName(oc, "openshift-ingress-canary", "pods")

		compat_otp.By("Create a new machineset")
		clusterinfra.SkipConditionally(oc)
		ms := clusterinfra.MachineSetDescription{Name: machineSetName, Replicas: 1}
		defer ms.DeleteMachineSet(oc)
		ms.CreateMachineSet(oc)

		compat_otp.By("Update machineset to schedule infra nodes")
		out, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("machinesets.machine.openshift.io", machineSetName, "-n", "openshift-machine-api", "-p", `{"spec":{"template":{"spec":{"taints":null}}}}`, "--type=merge").Output()
		o.Expect(out).To(o.ContainSubstring("machineset.machine.openshift.io/" + machineSetName + " patched"))
		out, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args("machinesets.machine.openshift.io", machineSetName, "-n", "openshift-machine-api", "-p", `{"spec":{"template":{"spec":{"metadata":{"labels":{"ingress": "true", "node-role.kubernetes.io/infra": ""}}}}}}`, "--type=merge").Output()
		o.Expect(out).To(o.ContainSubstring("machineset.machine.openshift.io/" + machineSetName + " patched"))
		updatedMachineName := clusterinfra.WaitForMachinesRunningByLabel(oc, 1, "machine.openshift.io/cluster-api-machineset="+machineSetName)

		compat_otp.By("Reschedule the running machineset with infra details")
		clusterinfra.DeleteMachine(oc, updatedMachineName[0])
		updatedMachineName1 := clusterinfra.WaitForMachinesRunningByLabel(oc, 1, "machine.openshift.io/cluster-api-machineset="+machineSetName)

		compat_otp.By("Check the canary deamonset is scheduled on infra node which is newly created")
		// confirm the new machineset is already created
		updatedMachineSetName := clusterinfra.ListWorkerMachineSetNames(oc)
		util.CheckGivenStringPresentOrNot(true, updatedMachineSetName, machineSetName)
		// confirm infra node presence among the nodes
		infraNode := util.GetByLabelAndJsonPath(oc, "default", "node", "node-role.kubernetes.io/infra", "{.items[*].metadata.name}")
		// confirm a canary pod got scheduled on to the infra node
		util.SearchInDescribeResource(oc, "node", infraNode, "canary")

		compat_otp.By("Confirming the canary namespace is over-rided with the default node selector")
		o.Expect(util.GetAnnotation(oc, "openshift-ingress-canary", "ns", "openshift-ingress-canary")).To(o.ContainSubstring(`openshift.io/node-selector":""`))

		compat_otp.By("Confirming the canary daemonset has the default tolerations included for infra role")
		tolerations := util.GetByJsonPath(oc, "openshift-ingress-canary", "daemonset/ingress-canary", "{.spec.template.spec.tolerations}")
		o.Expect(tolerations).To(o.ContainSubstring(`key":"node-role.kubernetes.io/infra`))

		compat_otp.By("Tainting the infra nodes with 'NoSchedule' and confirm canary pods continues to remain up and functional on those nodes")
		nodeNameOfMachine := clusterinfra.GetNodeNameFromMachine(oc, updatedMachineName1[0])
		output, err := oc.AsAdmin().WithoutNamespace().Run("adm").Args("taint", "nodes", nodeNameOfMachine, "node-role.kubernetes.io/infra:NoSchedule").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("node/" + nodeNameOfMachine + " tainted"))
		// confirm the canary pod is still present in the infra node
		util.SearchInDescribeResource(oc, "node", infraNode, "canary")

		compat_otp.By("Tainting the infra nodes with 'NoExecute' and confirm canary pods continues to remain up and functional on those nodes")
		output1, err1 := oc.AsAdmin().WithoutNamespace().Run("adm").Args("taint", "nodes", nodeNameOfMachine, "node-role.kubernetes.io/infra:NoExecute").Output()
		o.Expect(err1).NotTo(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("node/" + nodeNameOfMachine + " tainted"))
		// confirm the canary pod is still present in the infra node
		util.SearchInDescribeResource(oc, "node", infraNode, "canary")
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Medium-63004-Ipv6 addresses are also acceptable for whitelisting", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			output              string
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		)

		compat_otp.By("Create a server pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("expose a service in the namespace")
		util.CreateRoute(oc, ns, "http", "service-unsecure", "service-unsecure", []string{})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("service-unsecure"))

		compat_otp.By("Annotate the route with Ipv6 subnet and verify it")
		util.SetAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/ip_whitelist=2600:14a0::/40")
		o.Expect(util.GetAnnotation(oc, ns, "route", "service-unsecure")).To(o.ContainSubstring(`"haproxy.router.openshift.io/ip_whitelist":"2600:14a0::/40"`))

		compat_otp.By("Verify the acl whitelist parameter inside router pod with Ipv6 address")
		defaultPod := util.GetOneRouterPodNameByIC(oc, "default")
		backendName := "be_http:" + ns + ":service-unsecure"
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, backendName, []string{"acl allowlist src 2600:14a0::/40"})
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-77080-NetworkEdge Only host in allowlist can access unsecure/edge/reencrypt/passthrough routes", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			signedPod           = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		)

		compat_otp.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		srvPodList := util.CreateResourceFromWebServer(oc, ns, signedPod, "web-server-deploy")

		compat_otp.By("2.0: Create an unsecure, edge, reencrypt and passthrough route")
		domain := util.GetIngressctlDomain(oc, "default")
		unsecureRoute := "route-unsecure"
		unsecureHost := unsecureRoute + "-" + ns + "." + domain
		edgeRoute := "route-edge"
		edgeHost := edgeRoute + "-" + ns + "." + domain
		passthroughRoute := "route-passthrough"
		passthroughHost := passthroughRoute + "-" + ns + "." + domain
		reenRoute := "route-reen"
		reenHost := reenRoute + "-" + ns + "." + domain

		util.CreateRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")
		util.CreateRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		util.CreateRoute(oc, ns, "passthrough", passthroughRoute, secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-passthrough", "default")
		util.CreateRoute(oc, ns, "reencrypt", reenRoute, secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		compat_otp.By("3.0: Annotate unsecure, edge, reencrypt and passthrough route")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation := util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))
		util.SetAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))
		util.SetAnnotation(oc, ns, "route/"+passthroughRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))
		util.SetAnnotation(oc, ns, "route/"+reenRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))

		compat_otp.By("4.0: access the routes using the IP from the allowlist")
		util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift `+srvPodList[0]+` http-8080`)
		util.WaitForOutsideCurlContains("https://"+edgeHost, "-k", `Hello-OpenShift `+srvPodList[0]+` http-8080`)
		util.WaitForOutsideCurlContains("https://"+passthroughHost, "-k", `Hello-OpenShift `+srvPodList[0]+` https-8443 default`)
		util.WaitForOutsideCurlContains("https://"+reenHost, "-k", `Hello-OpenShift `+srvPodList[0]+` https-8443 default`)

		compat_otp.By("5.0: re-annotate routes with a random IP")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=1050::5:600:300c:326b`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"1050::5:600:300c:326b`))
		util.SetAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_allowlist=8.8.8.8`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"8.8.8.8`))
		util.SetAnnotation(oc, ns, "route/"+passthroughRoute, `haproxy.router.openshift.io/ip_allowlist=1050::5:600:300c:326b`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"1050::5:600:300c:326b`))
		util.SetAnnotation(oc, ns, "route/"+reenRoute, `haproxy.router.openshift.io/ip_allowlist=8.8.4.4`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"8.8.4.4`))

		compat_otp.By("6.0: attempt to access the routes without an IP in the allowlist")
		cmd := fmt.Sprintf(`curl --connect-timeout 10 -s %s %s 2>&1`, "-I", "http://"+unsecureHost)
		result, _ := exec.Command("bash", "-c", cmd).Output()
		// use -I for 2 different scenarios, squid result has failure bad gateway, otherwise uses exit status
		if strings.Contains(string(result), `squid`) {
			util.WaitForOutsideCurlContains("http://"+unsecureHost, "-I", `Bad Gateway`)
		} else {
			util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `exit status`)
		}
		util.WaitForOutsideCurlContains("https://"+edgeHost, "-k", `exit status`)
		util.WaitForOutsideCurlContains("https://"+passthroughHost, "-k", `exit status`)
		util.WaitForOutsideCurlContains("https://"+reenHost, "-k", `exit status`)

		compat_otp.By("7.0: Check HaProxy if the IP in the allowlist annotation exists")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src 1050::5:600:300c:326b", "tcp-request content reject if !allowlist"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+edgeRoute, []string{"acl allowlist src 8.8.8.8", "tcp-request content reject if !allowlist"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+passthroughRoute, []string{"acl allowlist src 1050::5:600:300c:326b", "tcp-request content reject if !allowlist"})
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+reenRoute, []string{"acl allowlist src 8.8.4.4", "tcp-request content reject if !allowlist"})
	})

	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-77082-NetworkEdge Route gives allowlist precedence when whitelist and allowlist annotations are both present", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPod             = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPod, "web-server-deploy")
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2.0: Create an unsecure route")
		unsecureRoute := "route-unsecure"
		unsecureHost := unsecureRoute + "-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.CreateRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")

		compat_otp.By("3.0: Annotate unsecure route")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation := util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))

		compat_otp.By("4.0: access the route using the IP from the whitelist")
		util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift `+srvPodList[0]+` http-8080`)

		compat_otp.By("5.0: add allowlist annotation with non valid host IP")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=1.2.3.4`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"1.2.3.4`))

		compat_otp.By("6.0: attempt to access the routes without an IP in the allowlist")
		cmd := fmt.Sprintf(`curl --connect-timeout 10 -s %s %s 2>&1`, "-I", "http://"+unsecureHost)
		result, _ := exec.Command("bash", "-c", cmd).Output()
		// use -I for 2 different scenarios, squid result has failure bad gateway, otherwise uses exit status
		if strings.Contains(string(result), `squid`) {
			util.WaitForOutsideCurlContains("http://"+unsecureHost, "-I", `Bad Gateway`)
		} else {
			util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `exit status`)
		}

		compat_otp.By("7.0: annotate route with a valid public client IP in the allowlist and an invalid host IP in the whitelist")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))

		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=1.2.3.4`)
		findAnnotation1 := util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation1).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"1.2.3.4`))

		util.WaitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift `+srvPodList[0]+` http-8080`)

		compat_otp.By("8.0: Check HaProxy if the allowlist annotation exists and tcp request exist")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src", "tcp-request content reject if !allowlist"})
	})

	// Combines OCP-77091 and OCP 77086 tests for allowlist epic NE:1100
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-77091-NetworkEdge Route does not enable allowlist with than 61 CIDRs and if invalid IP annotation is given", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPod             = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		srvPodList := util.CreateResourceFromWebServer(oc, ns, testPod, "web-server-deploy")
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2.0: Create an edge route")
		edgeRoute := "route-edge"
		edgeHost := edgeRoute + "-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.CreateRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")

		compat_otp.By("3.0: annotate route with an invalid IP and try to access route")
		util.SetAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_allowlist=192.abc.123.0`)
		findAnnotation := util.GetAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"192.abc.123.0`))

		util.WaitForOutsideCurlContains("https://"+edgeHost, "-k", `Hello-OpenShift `+srvPodList[0]+` http-8080`)

		compat_otp.By("4.0: Check HaProxy to confirm the allowlist annotation does not occur")
		util.EnsureHaproxyBlockConfigNotContains(oc, routerpod, ns+":"+edgeRoute, []string{"acl allowlist src", "tcp-request content reject if !allowlist"})

		//OCP-77091 route does not enable whitelist with more than 61 CIDRs
		compat_otp.By("5.0: Create an unsecure route")
		unsecureRoute := "route-unsecure"
		util.CreateRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")

		compat_otp.By("6.0: Annotate unsecure route with 61 CIDRs")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=192.168.0.0/24 192.168.1.0/24 192.168.2.0/24 192.168.3.0/24 192.168.4.0/24 192.168.5.0/24 192.168.6.0/24 192.168.7.0/24 192.168.8.0/24 192.168.9.0/24 192.168.10.0/24 192.168.11.0/24 192.168.12.0/24 192.168.13.0/24 192.168.14.0/24 192.168.15.0/24 192.168.16.0/24 192.168.17.0/24 192.168.18.0/24 192.168.19.0/24 192.168.20.0/24 192.168.21.0/24 192.168.22.0/24 192.168.23.0/24 192.168.24.0/24 192.168.25.0/24 192.168.26.0/24 192.168.27.0/24 192.168.28.0/24 192.168.29.0/24 192.168.30.0/24 192.168.31.0/24 192.168.32.0/24 192.168.33.0/24 192.168.34.0/24 192.168.35.0/24 192.168.36.0/24 192.168.37.0/24 192.168.38.0/24 192.168.39.0/24 192.168.40.0/24 192.168.41.0/24 192.168.42.0/24 192.168.43.0/24 192.168.44.0/24 192.168.45.0/24 192.168.46.0/24 192.168.47.0/24 192.168.48.0/24 192.168.49.0/24 192.168.50.0/24 192.168.51.0/24 192.168.52.0/24 192.168.53.0/24 192.168.54.0/24 192.168.55.0/24 192.168.56.0/24 192.168.57.0/24 192.168.58.0/24 192.168.59.0/24 192.168.60.0/24`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"`))

		compat_otp.By("7.0: Check HaProxy if the allowlist annotation exists and tcp request exist")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src 192.168.0.0/24", "tcp-request content reject if !allowlist"})

		compat_otp.By("8.0: add allowlist annotation with more than 61 CIDRs")
		util.SetAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=192.168.0.0/24 192.168.1.0/24 192.168.2.0/24 192.168.3.0/24 192.168.4.0/24 192.168.5.0/24 192.168.6.0/24 192.168.7.0/24 192.168.8.0/24 192.168.9.0/24 192.168.10.0/24 192.168.11.0/24 192.168.12.0/24 192.168.13.0/24 192.168.14.0/24 192.168.15.0/24 192.168.16.0/24 192.168.17.0/24 192.168.18.0/24 192.168.19.0/24 192.168.20.0/24 192.168.21.0/24 192.168.22.0/24 192.168.23.0/24 192.168.24.0/24 192.168.25.0/24 192.168.26.0/24 192.168.27.0/24 192.168.28.0/24 192.168.29.0/24 192.168.30.0/24 192.168.31.0/24 192.168.32.0/24 192.168.33.0/24 192.168.34.0/24 192.168.35.0/24 192.168.36.0/24 192.168.37.0/24 192.168.38.0/24 192.168.39.0/24 192.168.40.0/24 192.168.41.0/24 192.168.42.0/24 192.168.43.0/24 192.168.44.0/24 192.168.45.0/24 192.168.46.0/24 192.168.47.0/24 192.168.48.0/24 192.168.49.0/24 192.168.50.0/24 192.168.51.0/24 192.168.52.0/24 192.168.53.0/24 192.168.54.0/24 192.168.55.0/24 192.168.56.0/24 192.168.57.0/24 192.168.58.0/24 192.168.59.0/24 192.168.60.0/24 192.168.61.0/24`)
		findAnnotation = util.GetAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"`))

		compat_otp.By("9.0: Check HaProxy if the allowlist annotation exists and tcp request exist")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "backend be_http:"+ns+":"+unsecureRoute, []string{`acl allowlist src -f /var/lib/haproxy/router/allowlists/` + ns + ":" + unsecureRoute + ".txt", "tcp-request content reject if !allowlist"})
		util.EnsureHaproxyBlockConfigNotContains(oc, routerpod, "backend be_http:"+ns+":"+unsecureRoute, []string{"acl allowlist src 192.168.0.0/24"})
	})

	// OCPBUGS-47773
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-85274-Route spec path that have specail characters should not cause HaProxy error and ingress degraded [Serial]", func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			ingressJsonPath     = `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		)

		// skip the test if ingress co is abnormal
		status := util.GetByJsonPath(oc, "default", "co/ingress", ingressJsonPath)
		if status != "TrueFalseFalse" {
			g.Skip("ingress co is abnormal")
		}

		compat_otp.By("1.0: Create a single pod and the service")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		compat_otp.By("2.0: Create an unsecure route")
		util.CreateRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unSecSvcName, "default")

		compat_otp.By(`3.0: Try to patch the route spec.path with "/route-admission-test#2", which includes the # character`)
		specPath := `{"spec": {"path": "/route-admission-test#2"}}`
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unSecSvcName, "-p", specPath, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`cannot contain # or spaces`))

		compat_otp.By(`4.0: Try to patch the route spec.path with "/route-admission-test 22", which includes the space character`)
		specPath = `{"spec": {"path": "/route-admission-test 22"}}`
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unSecSvcName, "-p", specPath, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`cannot contain # or spaces`))

		compat_otp.By("5.0: Check the ingress co, make sure it is normal")
		status = util.GetByJsonPath(oc, "default", "co/ingress", ingressJsonPath)
		o.Expect(status).To(o.ContainSubstring("TrueFalseFalse"))
	})

	// OCPBUGS-76957
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-88075-UnmanagedRoutes metric should filter ingress by both name and namespace [Serial]", func() {
		var (
			unmanagedNamespace    = "unmanaged-ns-88075"
			ingressName           = "example-ingress"
			managedIngressClass   = "openshift-default"
			unmanagedIngressClass = "fake-ingress-class"
			query                 = "openshift_ingress_to_route_controller_route_with_unmanaged_owner"
			routerFixtureDir      = TestdataDir()
			testPodSvc            = filepath.Join(routerFixtureDir, "web-server-deploy.yaml")
			ingressTemplate       = filepath.Join(routerFixtureDir, "ingress-with-class.yaml")
		)

		compat_otp.By("1.0 Create deployment and service in managed namespace and create unmanaged namespace with deployment and service")
		ns := oc.Namespace()
		baseDomain := util.GetBaseDomain(oc)

		// Create deployment and service in managed namespace
		err := oc.AsAdmin().WithoutNamespace().Run("apply").Args("-n", ns, "-f", testPodSvc).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		// Create unmanaged namespace
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("namespace", unmanagedNamespace, "--ignore-not-found").Execute()
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("namespace", unmanagedNamespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		// Create deployment and service in unmanaged namespace
		err = oc.AsAdmin().WithoutNamespace().Run("apply").Args("-n", unmanagedNamespace, "-f", testPodSvc).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, unmanagedNamespace, "name=web-server-deploy")

		compat_otp.By("2.0 Create two ingresses with same name, one managed and one with non-existing ingress class")
		// Create managed ingress using sed to replace ingress name, ingressClassName, and host
		managedHost := fmt.Sprintf("ocp-88075-managed.apps.%s", baseDomain)
		sedCmd := fmt.Sprintf(`sed -i'' -e 's@ingress-with-class@%s@g;s@mytest@%s@g;s@foo.bar.com@%s@g' %s`,
			ingressName, managedIngressClass, managedHost, ingressTemplate)
		_, err = exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.CreateResourceFromFile(oc, ns, ingressTemplate)

		// Create unmanaged ingress with non-existing ingress class
		sedCmd1 := fmt.Sprintf(`sed -i'' -e 's|openshift-default|%s|g;s|ocp-88075-managed|ocp-88075-unmanaged|g' %s`, unmanagedIngressClass, ingressTemplate)
		_, err = exec.Command("bash", "-c", sedCmd1).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", ingressTemplate, "-n", unmanagedNamespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("3.0 Verify two ingresses are created - one managed and one unmanaged")
		managedIngressName := util.GetByJsonPath(oc, ns, "ingress/"+ingressName, "{..metadata.name}")
		o.Expect(managedIngressName).To(o.Equal(ingressName))
		unmanagedIngressName := util.GetByJsonPath(oc, unmanagedNamespace, "ingress/"+ingressName, "{..metadata.name}")
		o.Expect(unmanagedIngressName).To(o.Equal(ingressName))

		compat_otp.By("4.0 Verify managed route is created and unmanaged route is not created")
		httpHost := "ocp-88075-managed.apps." + baseDomain
		output := util.GetRoutes(oc, ns)
		o.Expect(output).To(o.ContainSubstring(httpHost))

		unmanagedRoutes, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", unmanagedNamespace, "-o", "jsonpath={.items[*].metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(unmanagedRoutes).NotTo(o.ContainSubstring(ingressName))

		compat_otp.By("5.0 Check UnmanagedRoutes metric in Prometheus for namespace filtering")
		// Some wait will be there for metrics to be updated and scraped by Prometheus
		var metricResult string
		token, err := util.GetSAToken(oc, "prometheus-k8s", "openshift-monitoring")
		o.Expect(err).NotTo(o.HaveOccurred())
		pollErr := wait.Poll(60*time.Second, 30*time.Second, func() (bool, error) {
			metric, err := util.GetPrometheusMetrics(oc, token, query)
			if err != nil {
				return false, nil
			}

			if !strings.Contains(metric, `"result":[`) || strings.Contains(metric, `"result":[]`) {
				return false, nil
			}

			if strings.Contains(metric, fmt.Sprintf(`"namespace":"%s"`, ns)) {
				metricResult = metric
				return true, nil
			}

			return false, nil
		})
		compat_otp.AssertWaitPollNoErr(pollErr, "Timed out waiting for metric to appear")

		if !strings.Contains(metricResult, fmt.Sprintf(`"namespace":"%s"`, ns)) {
			e2e.Failf("Metric missing for namespace %s", ns)
		}
		// Metric value should be 0 for managed route")
		if strings.Contains(metricResult, `,"1"]`) && strings.Contains(metricResult, fmt.Sprintf(`"namespace":"%s"`, ns)) {
			e2e.Failf("BUG DETECTED: UnmanagedRoutes alert is firing for managed Routes")
		}
	})
})
