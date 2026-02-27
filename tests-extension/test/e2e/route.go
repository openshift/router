package router

import (
	"github.com/openshift/router-tests-extension/test/e2e/testdata"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
	clusterinfra "github.com/openshift/openshift-tests-private/test/extended/util/clusterinfra"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("routes", exutil.KubeConfigPath())

	// incorporate OCP-10024, OCP-11883 and OCP-12122 into one
	// Test case creater: zzhao@redhat.com - OCP-10024 Route could NOT be updated after created
	// Test case creater: zzhao@redhat.com - OCP-11883 Be able to add more alias for service
	// Test case creater: zzhao@redhat.com - OCP-12122 Alias will be invalid after removing it
	g.It("Author:mjoseph-Critical-10024-Route could NOT be updated after created", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp2         = filepath.Join(buildPruningBaseDir, "subdomain-routes/route.yaml")
			unSecSvcName        = "service-unsecure"
			aliasRoute          = "service-unsecure2"
			edgeRoute           = "ocp10024-unsecure"
			rut                 = routeDescription{
				namespace: "",
				domain:    "",
				subDomain: "ocp10024",
				template:  customTemp2,
			}
		)

		exutil.By("1: Create an edge route using the route yaml file")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		rut.domain = "apps" + "." + baseDomain
		rut.namespace = ns
		rut.create(oc)
		getRoutes(oc, ns)
		ensureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")

		exutil.By("2: Try to update the hostname for route using a test user and confirm it is not possible")
		patchOutput, _ := oc.WithoutNamespace().Run("patch").Args("route/"+edgeRoute, "-p", "{\"spec\":{\"host\":\"www.changeroute.com\"}}", "--type=merge", "-n", ns).Output()
		o.Expect(patchOutput).To(o.ContainSubstring(`spec.host: Invalid value: "www.changeroute.com"`))

		// OCP-11883: Be able to add more alias for service
		exutil.By("3: Create a server and its service")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("4: Create a http route using the service-unsecure service")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		createRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, unSecSvcName, "default")
		backendName1 := "be_http:" + ns + ":service-unsecure"
		ensureHaproxyBlockConfigContains(oc, routerpod, backendName1, []string{"service-unsecure:http"})

		exutil.By("5: Create another http route (alias) using the same service")
		createRoute(oc, ns, "http", aliasRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, aliasRoute, "default")
		getRoutes(oc, ns)
		backendName2 := "be_http:" + ns + ":service-unsecure2"
		ensureHaproxyBlockConfigContains(oc, routerpod, backendName2, []string{"service-unsecure:http"})

		// OCP-12122 Alias will be invalid after removing it
		exutil.By("6: Delete the alias route and verify that route is not accessible")
		err := oc.AsAdmin().Run("delete").Args("-n", ns, "route", aliasRoute).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", ns, aliasRoute, "-ojsonpath={.status.ingress[?(@.routerName==\"default\")].conditions[*].status}").Output()
		o.Expect(routeOutput).To(o.ContainSubstring(`routes.route.openshift.io "service-unsecure2" not found`))

		exutil.By("7: Confirming the alias route got removed from haproxy")
		waitErr := wait.PollImmediate(3*time.Second, 60*time.Second, func() (bool, error) {
			noBackendConfig := readRouterPodData(oc, routerpod, "cat haproxy.config", "be_http:"+ns)
			if !strings.Contains(noBackendConfig, aliasRoute) {
				return true, nil
			}
			e2e.Logf("Still waiting for the alias route to get removed from the haproxy")
			return false, nil
		})
		o.Expect(waitErr).NotTo(o.HaveOccurred(), "The alias route'service-unsecure2' never get removed")
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-10043-Set balance leastconn for passthrough routes", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			svcName             = "service-secure"
		)

		exutil.By("1.0 Create a server pod and its services")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("2.0 Create a passthrough route")
		createRoute(oc, ns, "passthrough", "route-pass", svcName, []string{"--hostname=" + "passth10043" + ".apps." + getBaseDomain(oc)})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-pass", "default")

		exutil.By(`3.0 Add the balance=leastconn annotation to the routes`)
		setAnnotation(oc, ns, "route/route-pass", "haproxy.router.openshift.io/balance=leastconn")

		exutil.By(`4.0 Check the balance leastconn configuration in haproxy`)
		routerpod := getOneRouterPodNameByIC(oc, "default")
		backendStart := fmt.Sprintf("backend be_tcp:%s:%s", ns, "route-pass")
		ensureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{"balance leastconn"})
	})

	// bugzilla: 1368525
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Medium-10207-NetworkEdge Should use the same cookies for secure and insecure access when insecureEdgeTerminationPolicy set to allow for edge/reencrypt route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
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
			ingctrl             = ingressControllerDescription{
				name:      "ocp10207",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
		)

		exutil.By("1.0: Prepare file folder and file for testing")
		defer os.RemoveAll(fileDir)
		err := os.MkdirAll(fileDir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		updateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		exutil.By("2.0: Create a client pod, two server pods and the service")
		ns := oc.Namespace()
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		// create the cookie folder in the client pod
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, clientPodName, "--", "mkdir", podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		exutil.By("3.0: Create a custom ingresscontroller and an edge route with insecure_policy Allow")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "edge10207" + "." + ingctrl.domain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)
		createRoute(oc, ns, "edge", "route-edge10207", unSecSvcName, []string{"--hostname=" + routehost, "--insecure-policy=Allow"})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge10207", "default")

		exutil.By("4.0: Curl the edge route for two times, one with saving the cookie for the second server")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput := []string{"Hello-OpenShift " + srvPodList[0] + " http-8080"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-c", podFileDir + "/cookie-10207", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		exutil.By("5.0: Open the cookie file and check the contents")
		// access the cookie file and confirm that the output contains false and false
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, clientPodName+":"+podFileDir+"/cookie-10207", fileDir+"/cookie-10207").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		checkCookieFile(fileDir+"/cookie-10207", "FALSE\t/\tFALSE")

		exutil.By("6.0: Curl the edge route with the cookie, expect forwarding to the second server")
		curlCmdWithCookie := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-b", podFileDir + "/cookie-10207", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result := repeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[1]).To(o.Equal(6))

		exutil.By("7.0: Patch the edge route with Redirect tls insecureEdgeTerminationPolicy, then curl the edge route with the cookie, expect forwarding to the second server")
		patchResourceAsAdmin(oc, ns, "route/route-edge10207", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Redirect"}}}`)
		toDst2 := routehost + ":80:" + podIP
		curlCmdWithCookie = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-ksSL", "-b", podFileDir + "/cookie-10207", "--resolve", toDst, "--resolve", toDst2, "--connect-timeout", "10"}
		_, result = repeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[1]).To(o.Equal(6))

		exutil.By("8.0: Create a reencrypt route with Allow policy")
		reenhost := "reen10207" + "." + ingctrl.domain
		toDst = reenhost + ":443:" + podIP
		toDst2 = reenhost + ":80:" + podIP
		createRoute(oc, ns, "reencrypt", "route-reen10207", secSvcName, []string{"--hostname=" + reenhost, "--insecure-policy=Allow"})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen10207", "default")

		exutil.By("9.0: Curl the route and generate a cookie file")
		curlCmdWithCookie = []string{"-n", ns, clientPodName, "--", "curl", "http://" + reenhost, "-ks", "-c", podFileDir + "/reen-cookie", "--resolve", toDst, "--resolve", toDst2, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " https-8443"}
		repeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 60, 1)

		exutil.By("10.0: Open the cookie file and check the contents")
		// access the cookie file and confirm that the output contains false and false
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, clientPodName+":"+podFileDir+"/reen-cookie", fileDir+"/reen-cookie").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		checkCookieFile(fileDir+"/reen-cookie", "FALSE\t/\tFALSE")
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-10660-Service endpoint can be work well if the mapping pod ip is updated", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			serverName          = "web-server-deploy"
		)

		exutil.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		exutil.By("2. Check the service endpoints")
		epJsonPath := "{.subsets[0].addresses[0].ip}:{.subsets[0].ports[0].port}"
		epIPregExp := "([0-9]+.[0-9]+.[0-9]+.[0-9]+|[0-9a-zA-Z]+:[0-9a-zA-Z:]+)"
		epSearchOutput := waitForOutputMatchRegexp(oc, ns, "endpoints/"+unSecSvcName, epJsonPath, epIPregExp)
		o.Expect(epSearchOutput).NotTo(o.ContainSubstring("NotMatch"))
		ep := getByJsonPath(oc, ns, "endpoints/"+unSecSvcName, epJsonPath)

		exutil.By("3. Delete the server pod and check the endpoint")
		scaleDeploy(oc, ns, serverName, 0)
		// there will not an ip assigned for the EP after the pod is removed
		noneEP := getByJsonPath(oc, ns, "endpoints/"+unSecSvcName, epJsonPath)
		o.Expect(noneEP).To(o.ContainSubstring(":"))

		exutil.By("4. Create the pod again and recheck the service endpoints")
		scaleDeploy(oc, ns, serverName, 1)
		epSearchOutput = waitForOutputMatchRegexp(oc, ns, "endpoints/"+unSecSvcName, epJsonPath, epIPregExp)
		o.Expect(epSearchOutput).NotTo(o.ContainSubstring("NotMatch"))
		newEP := getByJsonPath(oc, ns, "endpoints/"+unSecSvcName, epJsonPath)
		// the new IP assigned will be different from the old one
		o.Expect(newEP).NotTo(o.Equal(ep))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Low-10943-NetworkEdge Set invalid timeout server for route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create single pod and the service")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		exutil.By("2.0: Create an unsecure route")

		createRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		exutil.By("3.0: Annotate unsecure route")
		setAnnotation(oc, ns, "route/"+unSecSvcName, "haproxy.router.openshift.io/timeout=-2s")
		findAnnotation := getAnnotation(oc, ns, "route", unSecSvcName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"-2s`))

		exutil.By("4.0: Check HAProxy file for timeout tunnel")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		ensureHaproxyBlockConfigNotContains(oc, routerpod, ns, []string{"timeout server  -2s"})
	})

	// author: iamin@redhat.com
	// combine OCP-9651, OCP-9717
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-11036-NetworkEdge Set insecureEdgeTerminationPolicy to Redirect for passthrough/edge/reencrypt route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			SvcName             = "service-secure"
			unSecSvc            = "service-unsecure"
		)

		exutil.By("1.0: Create single pod, service and a passthrough/edge/reencrypt route")
		ns := oc.Namespace()
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvc), o.ContainSubstring(SvcName)))
		createRoute(oc, ns, "passthrough", "passthrough-route", SvcName, []string{})
		createRoute(oc, ns, "reencrypt", "reen-route", SvcName, []string{})
		createRoute(oc, ns, "edge", "edge-route", unSecSvc, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring("passthrough-route"), o.ContainSubstring("reen-route"), o.ContainSubstring("edge-route")))

		exutil.By("2.0: Add Redirect in tls")
		patchResourceAsAdmin(oc, ns, "route/passthrough-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Redirect"}}}`)
		output, err = oc.Run("get").Args("route/passthrough-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		exutil.By("3.0: Test Route Http request is redirected to https")
		routehost := "passthrough-route-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("http://"+routehost, "-I -k", "ocation: https://"+routehost)
		waitForOutsideCurlContains("http://"+routehost, "-L -k", "Hello-OpenShift "+srvPodList[0]+" https-8443")

		exutil.By("4.0: Attempt to update route policy to Allow")
		result, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/passthrough-route", "-p", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Allow"}}}`, "-n", ns).Output()
		o.Expect(result).To(o.ContainSubstring("invalid value for InsecureEdgeTerminationPolicy option, acceptable values are None, Redirect, or empty"))

		exutil.By("5.0: Add Redirect in reencrypt tls")
		patchResourceAsAdmin(oc, ns, "route/reen-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Redirect"}}}`)
		output, err = oc.Run("get").Args("route/reen-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		exutil.By("6.0: Test Route Http request is redirected to https")
		reenhost := "reen-route-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("http://"+reenhost, "-I -k", "ocation: https://"+reenhost)
		waitForOutsideCurlContains("http://"+reenhost, "-L -k", "Hello-OpenShift "+srvPodList[0]+" https-8443")

		exutil.By("7.0: Add Redirect in edge tls")
		patchResourceAsAdmin(oc, ns, "route/edge-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Redirect"}}}`)
		output, err = oc.Run("get").Args("route/edge-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		exutil.By("8.0: Test Route Http request is redirected to https")
		edgehost := "edge-route-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("http://"+edgehost, "-I -k", "ocation: https://"+edgehost)
		waitForOutsideCurlContains("http://"+edgehost, "-L -k", "Hello-OpenShift "+srvPodList[0]+" http-8080")

		exutil.By("9.0: Attempt to update route policy to invalid value")
		result, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/edge-route", "-p", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Abc"}}}`, "-n", ns).Output()
		o.Expect(result).To(o.ContainSubstring("invalid value for InsecureEdgeTerminationPolicy option, acceptable values are None, Allow, Redirect, or empty"))

	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Medium-11067-NetworkEdge oc help information should contain option wildcard-policy", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			svcName             = "service-secure"
		)

		exutil.By("1.0: Create single pod, service")
		ns := oc.Namespace()
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		exutil.By("2.0: Check help section for expose service")
		output, err := oc.Run("expose").Args("service", svcName, "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("--wildcard-policy="))

		exutil.By("3.0: Check help section for edge route creation")
		output, err = oc.Run("create").Args("route", "edge", "route-edge", "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("--wildcard-policy="))

		exutil.By("4.0: Check help section for passthrough route creation")
		output, err = oc.Run("create").Args("route", "passthrough", "route-pass", "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("--wildcard-policy="))

		exutil.By("5.0: Check help section for reencrypt route creation")
		output, err = oc.Run("create").Args("route", "reencrypt", "route-reen", "--help").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("--wildcard-policy="))
	})

	// merge OCP-11042(NetworkEdge NetworkEdge Disable haproxy hash based sticky session for edge termination routes) to OCP-11130
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-11130-NetworkEdge Enable/Disable haproxy cookies based sticky session for edge termination routes", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			fileDir             = "/data/OCP-11130-cookie"
			ingctrl             = ingressControllerDescription{
				name:      "ocp11130",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
		)

		exutil.By("1.0: Updated replicas in the web-server-deploy file for testing")
		updateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		exutil.By("2.0: Create a client pod, two server pods and the service")
		ns := oc.Namespace()
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		// create the cookie folder in the client pod
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, clientPodName, "--", "mkdir", fileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		exutil.By("3.0: Create an edge route")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "edge11130" + "." + ingctrl.domain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)
		createRoute(oc, ns, "edge", "route-edge11130", unSecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge11130", "default")

		exutil.By("4.0: Curl the edge route, make sure saving the cookie for server 1")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-c", fileDir + "/cookie-11130", "--resolve", toDst, "--connect-timeout", "10"}

		expectOutput := []string{"Hello-OpenShift " + srvPodList[0] + " http-8080"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		exutil.By("5.0: Curl the edge route, make sure could get response from server 2")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		exutil.By("6.0: Curl the edge route with the cookie, expect all are forwarded to the server 1")
		curlCmdWithCookie := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-b", fileDir + "/cookie-11130", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result := repeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[0]).To(o.Equal(6))

		// Disable haproxy hash based sticky session for edge termination routes
		exutil.By("7.0: Annotate the edge route with haproxy.router.openshift.io/disable_cookies=true")
		_, err = oc.Run("annotate").WithoutNamespace().Args("-n", ns, "route/route-edge11130", "haproxy.router.openshift.io/disable_cookies=true", "--overwrite").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("8.0: Curl the edge route, and save the cookie for the backend server")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-c", fileDir + "/cookie-11130", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		exutil.By("9.0: Curl the edge route with the cookie, expect forwarding to the two server")
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result = repeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 150, 15)
		o.Expect(result[0] > 0).To(o.BeTrue())
		o.Expect(result[1] > 0).To(o.BeTrue())
		o.Expect(result[0] + result[1]).To(o.Equal(15))
	})

	// incorporate OCP-11619, OCP-10914 and OCP-11325 into one
	// Test case creater: bmeng@redhat.com - OCP-11619-Limit the number of TCP connection per IP in specified time period
	// Test case creater: yadu@redhat.com - OCP-10914: Protect from ddos by limiting TCP concurrent connection for route
	// Test case creater: hongli@redhat.com - OCP-11325: Limit the number of http request per ip
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-11619-Limit the number of TCP connection per IP in specified time period", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		)

		exutil.By("1. Create a server")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("2. Create a passthrough route in the namespace")
		createRoute(oc, ns, "passthrough", "mypass", "service-secure", []string{})
		output := getRoutes(oc, ns)
		o.Expect(output).To(o.ContainSubstring("mypass"))

		exutil.By("3. Check the reachability of the passthrough route")
		routehost := getRouteHost(oc, ns, "mypass")
		curlCmd := fmt.Sprintf(`curl -k https://%s --connect-timeout 10`, routehost)
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		exutil.By("4. Annotate the route to limit the TCP nums per ip and verify")
		setAnnotation(oc, ns, "route/mypass", "haproxy.router.openshift.io/rate-limit-connections=true")
		setAnnotation(oc, ns, "route/mypass", "haproxy.router.openshift.io/rate-limit-connections.rate-tcp=2")
		findAnnotation := getAnnotation(oc, ns, "route", "mypass")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections: "true"`))
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections.rate-tcp: "2"`))

		exutil.By("5. Verify the haproxy configuration to ensure the tcp rate limit is configured")
		podName := getOneRouterPodNameByIC(oc, "default")
		backendName := "be_tcp:" + ns + ":mypass"
		ensureHaproxyBlockConfigContains(oc, podName, backendName, []string{"src_conn_rate", "tcp-request content reject if { src_conn_rate ge 2 }"})

		// OCP-10914: Protect from ddos by limiting TCP concurrent connection for route
		exutil.By("6. Expose a service in the namespace")
		createRoute(oc, ns, "http", "service-unsecure", "service-unsecure", []string{})
		output = getRoutes(oc, ns)
		o.Expect(output).To(o.ContainSubstring("service-unsecure"))

		exutil.By("7. Check the reachability of the http route")
		routehost = getRouteHost(oc, ns, "service-unsecure")
		curlCmd = fmt.Sprintf(`curl http://%s --connect-timeout 10`, routehost)
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 30, 1)

		exutil.By("8. Annotate the route to limit the concurrent TCP connections rate and verify")
		setAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/rate-limit-connections=true")
		setAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/rate-limit-connections.concurrent-tcp=2")
		findAnnotation = getAnnotation(oc, ns, "route", "service-unsecure")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections: "true"`))
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections.concurrent-tcp: "2"`))

		exutil.By("9. Verify the haproxy configuration to ensure the tcp rate limit is configured")
		backendName1 := "be_http:" + ns + ":service-unsecure"
		ensureHaproxyBlockConfigContains(oc, podName, backendName1, []string{"src_conn_cur", "tcp-request content reject if { src_conn_cur ge  2 }"})

		// OCP-11325: Limit the number of http request per ip
		exutil.By("10. Annotate the route to limit the http request nums per ip and verify")
		setAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/rate-limit-connections.concurrent-tcp-")
		setAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/rate-limit-connections.rate-http=3")
		findAnnotation = getAnnotation(oc, ns, "route", "service-unsecure")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections: "true"`))
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`haproxy.router.openshift.io/rate-limit-connections.rate-http: "3"`))

		exutil.By("11. Verify the haproxy configuration to ensure the http rate limit is configured")
		ensureHaproxyBlockConfigContains(oc, podName, backendName1, []string{"src_http_req_rate", "tcp-request content reject if { src_http_req_rate ge 3 }"})
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-11635-NetworkEdge Set timeout server for passthough route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			secureSvcName       = "httpbin-svc-secure"
		)

		exutil.By("1.0: Create single pod and the service")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		exutil.By("2.0: Create a passthrough route")
		routeName := "route-passthrough11635"
		routehost := routeName + "-" + ns + ".apps." + getBaseDomain(oc)

		createRoute(oc, ns, "passthrough", routeName, secureSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")

		exutil.By("3.0: Annotate passthrough route")
		setAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=3s")
		findAnnotation := getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"3s`))

		exutil.By("4.0: Curl the edge route for two times, one with normal delay and other above timeout delay")
		waitForOutsideCurlContains("https://"+routehost+"/delay/2", "-kI", `200 OK`)
		waitForOutsideCurlContains("https://"+routehost+"/delay/5", "-kI", `exit status`)

		exutil.By("5.0: Check HAProxy file for timeout tunnel")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns, []string{routeName, "timeout tunnel  3s"})
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Medium-11728-haproxy hash based sticky session for tcp mode passthrough routes", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			secSvcName          = "service-secure"
			routeName           = "route-pass11728"
			ingctrl             = ingressControllerDescription{
				name:      "ocp11728",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("1.0: Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2.0: Updated replicas in the web-server-deploy file for testing")
		updateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		exutil.By("3.0: Create a client pod, two server pods and the service")
		ns := oc.Namespace()
		err := oc.WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		exutil.By("4.0: Create a passthrough route")
		routehost := routeName + "." + ingctrl.domain
		createRoute(oc, ns, "passthrough", routeName, secSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")

		exutil.By("5.0: Check the passthrough route configuration in haproxy")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		backendStart := fmt.Sprintf(`backend be_tcp:%s:%s`, ns, routeName)
		ensureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{routeName, "balance source", "hash-type consistent"})

		exutil.By("6.0: Curl the passthrough route, and save the output")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-sk", "--resolve", toDst, "--connect-timeout", "10"}
		outputWithOneServer, _ := repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		exutil.By("7.0: Curl the passthrough route for 6 times, all are forwarded to the expected server")
		expectOutput := []string{"Hello-OpenShift " + srvPodList[0], "Hello-OpenShift " + srvPodList[1]}
		output, matchedList := repeatCmdOnClient(oc, curlCmd, expectOutput, 90, 6)
		o.Expect(output).To(o.ContainSubstring(outputWithOneServer))
		o.Expect(matchedList[0] + matchedList[1]).To(o.Equal(6))
		o.Expect(matchedList[0] * matchedList[1]).To(o.Equal(0))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-11982-NetworkEdge Set timeout server for http route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			insecureSvcName     = "httpbin-svc-insecure"
		)

		exutil.By("1.0: Create single pod and the service")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=httpbin-pod")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(insecureSvcName))

		exutil.By("2.0: Create an http route")
		routeName := "route-http11982"
		routehost := routeName + "-" + ns + ".apps." + getBaseDomain(oc)

		createRoute(oc, ns, "http", routeName, insecureSvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(routeName))

		exutil.By("3.0: Annotate http route")
		setAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=2s")
		findAnnotation := getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"2s`))

		exutil.By("4.0: Curl the http route for two times, one with normal delay and other above timeout delay")
		waitForOutsideCurlContains("http://"+routehost+"/delay/1", "-I", `200 OK`)
		// some proxies return "Gateway Timeout" but some return "Gateway Time-out"
		waitForOutsideCurlContains("http://"+routehost+"/delay/5", "-I", `504 Gateway Time`)

		exutil.By("5.0: Check HAProxy file for timeout tunnel")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns, []string{routeName, "timeout server  2s"})
	})

	// bug: 1374772
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-12091-haproxy config information should be clean when changing the service to another route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")
			webServerDeploy1    = webServerDeployDescription{
				deployName:      "web-server-deploy1",
				svcSecureName:   "service-secure1",
				svcUnsecureName: "service-unsecure1",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy2 = webServerDeployDescription{
				deployName:      "web-server-deploy2",
				svcSecureName:   "service-secure2",
				svcUnsecureName: "service-unsecure2",
				template:        webServerTemplate,
				namespace:       "",
			}
			deploy1Label      = "name=" + webServerDeploy1.deployName
			deploy2Label      = "name=" + webServerDeploy2.deployName
			unsecureRouteName = "unsecure12091"

			ingctrl = ingressControllerDescription{
				name:      "12091",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("1.0 Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2.0: Create a client pod and deploy two sets of web-server and services")
		ns := oc.Namespace()
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		webServerDeploy1.namespace = ns
		webServerDeploy2.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.create(oc)
		ensurePodWithLabelReady(oc, ns, deploy1Label)
		ensurePodWithLabelReady(oc, ns, deploy2Label)
		pod1Name := getPodListByLabel(oc, ns, deploy1Label)[0]
		pod2Name := getPodListByLabel(oc, ns, deploy2Label)[0]

		exutil.By("3.0: Create a unsecure route")
		routehost := unsecureRouteName + "." + ingctrl.domain
		createRoute(oc, ns, "http", unsecureRouteName, webServerDeploy1.svcUnsecureName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, unsecureRouteName, ingctrl.name)

		exutil.By("4.0: Add the balance=roundrobin annotation to the route, then check it in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		backendStart := fmt.Sprintf(`backend be_http:%s:%s`, ns, unsecureRouteName)
		setAnnotation(oc, ns, "route/"+unsecureRouteName, "haproxy.router.openshift.io/balance=roundrobin")
		ensureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{"balance roundrobin"})

		exutil.By("5.0: Curl the http route, make sure the first server is hit")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-s", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput := []string{"Hello-OpenShift " + pod1Name + " http-8080"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		exutil.By("6.0: Patch the http route with spec to another service")
		toAnotherService := fmt.Sprintf(`{"spec":{"to":{"name": "%s"}}}`, webServerDeploy2.svcUnsecureName)
		patchResourceAsAdmin(oc, ns, "route/"+unsecureRouteName, toAnotherService)

		exutil.By("7.0: Check the route configuration in haproxy, make sure the first service disappeared and the second service present")
		pod1IP := getByJsonPath(oc, ns, "pod/"+pod1Name, `{.status.podIP}`)
		ensureHaproxyBlockConfigNotContains(oc, routerpod, backendStart, []string{webServerDeploy1.svcUnsecureName, pod1IP})
		ensureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{webServerDeploy2.svcUnsecureName})

		exutil.By("8.0: Curl the route for 10 times, all are forwarded to the second server")
		expectOutput = []string{"Hello-OpenShift " + pod1Name + " http-8080", "Hello-OpenShift " + pod2Name + " http-8080"}
		_, result := repeatCmdOnClient(oc, curlCmd, expectOutput, 180, 10)
		o.Expect(result[1]).To(o.Equal(10))
	})

	// incorporate OCP-12506 OCP-15115 OCP-16368 into one
	// Test case creater: hongli@redhat.com - OCP-12506: Hostname of componentRoutes should be RFC compliant
	// Test case creater: zzhao@redhat.com - OCP-15115: Harden haproxy to prevent the PROXY header from being passed for reencrypt route
	g.It("Author:mjoseph-High-12506-reencrypt route with no cert if a router is configured with a default wildcard cert", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		caCert := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")

		exutil.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		defaultContPod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		exutil.By("2. Create a reen route")
		createRoute(oc, ns, "reencrypt", "12506-no-cert", "service-secure", []string{"--dest-ca-cert=" + caCert})
		getRoutes(oc, ns)

		exutil.By("3. Confirm whether the destination certificate is present")
		waitForOutputContains(oc, ns, "route/12506-no-cert", "{.spec.tls}", "destinationCACertificate")

		exutil.By("4. Check the router pod and ensure the routes are loaded in haproxy.config of default controller")
		ensureHaproxyBlockConfigContains(oc, defaultContPod, ns, []string{"backend be_secure:" + ns + ":12506-no-cert"})

		exutil.By("5. Check the reachability of the host in the default controller")
		reenHost := "12506-no-cert-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("https://"+reenHost, "-k", `Hello-OpenShift web-server-deploy`)

		// OCP-15115: Harden haproxy to prevent the PROXY header from being passed for reencrypt route
		exutil.By("6. Access the route with 'proxy' header and confirm the proxy is carried with it")
		result := waitForOutsideCurlContains("--head -H proxy:10.10.10.10 https://"+reenHost, "-k", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`proxy:10.10.10.10`))
	})

	// incorporate OCP-12562 and OCP-12575 into one
	// Test case creater: hongli@redhat.com - OCP-12562 The path specified in route can work well for edge terminated
	// Test case creater: hongli@redhat.com - OCP-12575 The path specified in route can work well for unsecure
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-12562-The path specified in route can work well for edge/unsecure termination", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		unSecSvc := "service-unsecure"
		edgeRoute := "12562-edge"
		httpRoute := "12562-http"

		exutil.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		exutil.By("2. Create a edge route with path")
		edgeHost := edgeRoute + "-" + ns + ".apps." + baseDomain
		createRoute(oc, ns, "edge", edgeRoute, unSecSvc, []string{"--path=/test"})
		ensureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")

		exutil.By("3. Curl the edge routes with and without path")
		waitForOutsideCurlContains("https://"+edgeHost+"/test/", "-k", "Hello-OpenShift-Path-Test")
		waitForOutsideCurlContains("https://"+edgeHost, "-k", "Application is not available")

		exutil.By("4. Remove path and check the reachability of the edge route without path")
		patchResourceAsAdmin(oc, ns, "route/"+edgeRoute, `{"spec":{"path": ""}}`)
		ensureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")
		waitForOutsideCurlContains("https://"+edgeHost, "-k", "Hello-OpenShift")

		exutil.By("5. Re-add the path and again check the reachability of the edge route with path")
		patchResourceAsAdmin(oc, ns, "route/"+edgeRoute, `{"spec":{"path": "/test"}}`)
		ensureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")
		output := getByJsonPath(oc, ns, "route/"+edgeRoute, `{.items[*].status.ingress[?(@.routerName=="default")].conditions[*].reason}`)
		o.Expect(output).NotTo(o.ContainSubstring("HostAlreadyClaimed"))
		waitForOutsideCurlContains("https://"+edgeHost+"/test/", "-k", "Hello-OpenShift-Path-Test")

		// OCP-12575: The path specified in route can work well for unsecure
		exutil.By("6. Create a http route")
		httpHost := httpRoute + "-" + ns + ".apps." + baseDomain
		createRoute(oc, ns, "http", httpRoute, unSecSvc, []string{"--path=/test"})
		ensureRouteIsAdmittedByIngressController(oc, ns, httpRoute, "default")

		exutil.By("7. Curl the edge route and with and without path")
		waitForOutsideCurlContains("http://"+httpHost+"/test/", "-k", "Hello-OpenShift-Path-Test")
		waitForOutsideCurlContains("http://"+httpHost, "-k", "Application is not available")

		exutil.By("8. Remove path and check the reachability of the http route without path")
		patchResourceAsAdmin(oc, ns, "route/"+httpRoute, `{"spec":{"path": ""}}`)
		ensureRouteIsAdmittedByIngressController(oc, ns, httpRoute, "default")
		waitForOutsideCurlContains("http://"+httpHost, "-k", "Hello-OpenShift")

		exutil.By("9. Re-add the path and again check the reachability of the http route with path")
		patchResourceAsAdmin(oc, ns, "route/"+httpRoute, `{"spec":{"path": "/test"}}`)
		ensureRouteIsAdmittedByIngressController(oc, ns, httpRoute, "default")
		output1 := getByJsonPath(oc, ns, "route/"+httpRoute, `{.items[*].status.ingress[?(@.routerName=="default")].conditions[*].reason}`)
		o.Expect(output1).NotTo(o.ContainSubstring("HostAlreadyClaimed"))
		waitForOutsideCurlContains("http://"+httpHost+"/test/", "-k", "Hello-OpenShift-Path-Test")
	})

	// Test case creater: hongli@redhat.com
	g.It("Author:mjoseph-Critical-12564-The path specified in route can work well for reencrypt terminated", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		caCert := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")

		exutil.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		defaultContPod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		exutil.By("2. Create a reen route")
		createRoute(oc, ns, "reencrypt", "12564-reencrypt", "service-secure", []string{"--dest-ca-cert=" + caCert, "--path=/test"})
		getRoutes(oc, ns)

		exutil.By("3. Confirm whether the destination certificate is present")
		waitForOutputContains(oc, ns, "route/12564-reencrypt", "{.spec.tls}", "destinationCACertificate")

		exutil.By("4. Check the router pod and ensure the routes are loaded in haproxy.config of default controller")
		ensureHaproxyBlockConfigContains(oc, defaultContPod, ns, []string{"backend be_secure:" + ns + ":12564-reencrypt"})

		exutil.By("5. Check the reachability of the  in the specified path")
		reenHostWithPath := "12564-reencrypt-" + ns + ".apps." + getBaseDomain(oc) + "/test/"
		waitForOutsideCurlContains("https://"+reenHostWithPath, "-k", `Hello-OpenShift-Path-Test web-server-deploy`)

		exutil.By("6. Check the reachability of the host in the default controller")
		reenHostWithOutPath := "12564-reencrypt-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("https://"+reenHostWithOutPath, "-kI", "503 Service Unavailable")
	})

	// incorporate OCP-12652, OCP-12556 and OCP-13248 into one
	// Test case creater: zzhao@redhat.com - OCP-12652 The later route should be HostAlreadyClaimed when there is a same host exist
	// Test case creater: zzhao@redhat.com - OCP-12556 Create a route without host named
	// Test case creater: zzhao@redhat.com - OCP-13248 The hostname should be converted to available route when met special character
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-12652-The later route should be HostAlreadyClaimed when there is a same host exist", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			unsecureRoute       = "route.12652"
			httpRoute           = "route.edge"
			reenRoute           = "route.reen"
			passthroughRoute    = "route.pass"
			e2eTestNamespace2   = "e2e-ne-ocp22652-" + getRandomString()
		)

		exutil.By("1. Create an additional namespace for this scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		e2eTestNamespace1 := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		httpRoutehost1 := "route-12652-" + e2eTestNamespace1 + ".apps." + baseDomain
		httpRoutehost2 := "route-12652-" + e2eTestNamespace2 + ".apps." + baseDomain

		exutil.By("2. Create a server pod and an unsecure service in one ns")
		createResourceFromWebServer(oc, e2eTestNamespace1, testPodSvc, srvrcInfo)

		exutil.By("3: Create a http and edge route")
		// Http route is created without hostname
		createRoute(oc, e2eTestNamespace1, "http", unsecureRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, unsecureRoute, "default")
		createRoute(oc, e2eTestNamespace1, "edge", httpRoute, unSecSvcName, []string{"--hostname=www.route-edge.com"})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, httpRoute, "default")

		exutil.By("4. Create a server pod and an unsecure service in the other ns")
		operateResourceFromFile(oc, "create", e2eTestNamespace2, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		exutil.By("5: Create a http and edge route in the other ns with same host name")
		// Http route is created without hostname
		_, err := oc.AsAdmin().WithoutNamespace().Run("expose").Args("-n", e2eTestNamespace2, "service", unSecSvcName, "--name="+unsecureRoute).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace2, unsecureRoute, "default")
		_, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", e2eTestNamespace2, "route", "edge", httpRoute, "--service="+unSecSvcName, "--hostname=www.route-edge.com").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("6. Confirm the route in the second ns is shown as HostAlreadyClaimed")
		waitForOutputContains(oc, e2eTestNamespace2, "route", `{.items[*].status.ingress[?(@.routerName=="default")].conditions[*].reason}`, "HostAlreadyClaimed")

		// OCP-12556 NetworkEdge Create a route without host named
		exutil.By("7: Check the http routes in both namespace are reachable without explicity configuring hostname")
		waitForOutsideCurlContains("http://"+httpRoutehost1, "", `Hello-OpenShift web-server-deploy`)
		waitForOutsideCurlContains("http://"+httpRoutehost2, "", `Hello-OpenShift web-server-deploy`)

		// OCP-13248 The hostname should be converted to available route when met special character
		exutil.By("8: Create passthrough and reen route in first namespace")
		createRoute(oc, e2eTestNamespace1, "passthrough", passthroughRoute, secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, passthroughRoute, "default")
		createRoute(oc, e2eTestNamespace1, "reencrypt", reenRoute, secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, reenRoute, "default")

		exutil.By("9: Check these routes whose names have '.' decoded to '-'")
		output := getRoutes(oc, e2eTestNamespace1)
		o.Expect(output).To(o.And(o.ContainSubstring("route-reen"), o.ContainSubstring("route-edge"), o.ContainSubstring("route-pass"), o.ContainSubstring("route-12652")))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-13753-NetworkEdge Check the cookie if using secure mode when insecureEdgeTerminationPolicy to Redirect for edge/reencrypt route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			SvcName             = "service-secure"
			fileDir             = "/tmp/OCP-13753-cookie"
		)

		exutil.By("1.0: Prepare file folder and file for testing")
		defer os.RemoveAll(fileDir)
		err := os.MkdirAll(fileDir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("2.0: Create two server pods and the service")
		ns := oc.Namespace()
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		exutil.By("3.0: Create an edge and reencrypt route with insecure_policy Redirect")
		edgehost := "edge-route-" + ns + ".apps." + getBaseDomain(oc)
		reenhost := "reen-route-" + ns + ".apps." + getBaseDomain(oc)
		createRoute(oc, ns, "edge", "edge-route", unSecSvcName, []string{"--insecure-policy=Redirect"})
		ensureRouteIsAdmittedByIngressController(oc, ns, "edge-route", "default")
		output, err := oc.Run("get").Args("route/edge-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		createRoute(oc, ns, "reencrypt", "reen-route", SvcName, []string{"--insecure-policy=Redirect"})
		ensureRouteIsAdmittedByIngressController(oc, ns, "reen-route", "default")
		output, err = oc.Run("get").Args("route/reen-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Redirect"`))

		exutil.By("4.0: Curl the edge route and generate a cookie file")
		waitForOutsideCurlContains("http://"+edgehost, "-v -L -k -c "+fileDir+"/edge-cookie", "Hello-OpenShift "+srvPodList[0]+" http-8080")

		exutil.By("5.0: Open the cookie file and check the contents")
		// access the cookie file and confirm that the output contains false and true
		checkCookieFile(fileDir+"/edge-cookie", "FALSE\t/\tTRUE")

		exutil.By("6.0: Curl the reencrypt route and generate a cookie file")
		waitForOutsideCurlContains("http://"+reenhost, "-v -L -k -c "+fileDir+"/reen-cookie", "Hello-OpenShift "+srvPodList[0]+" https-8443")

		exutil.By("7.0: Open the cookie file and check the contents")
		// access the cookie file and confirm that the output contains false and true
		checkCookieFile(fileDir+"/reen-cookie", "FALSE\t/\tTRUE")

	})

	// author: iamin@redhat.com
	//combine OCP-9650
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-13839-NetworkEdge Set insecureEdgeTerminationPolicy to Allow for reencrypt/edge route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			SvcName             = "service-secure"
			unSecSvc            = "service-unsecure"
		)

		exutil.By("1.0: Create single pod, service and reencrypt and edge route")
		ns := oc.Namespace()
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvc), o.ContainSubstring(SvcName)))
		createRoute(oc, ns, "reencrypt", "reen-route", SvcName, []string{})
		createRoute(oc, ns, "edge", "edge-route", unSecSvc, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring("reen-route"), o.ContainSubstring("edge-route")))

		exutil.By("2.0: Add Allow policy in tls")
		patchResourceAsAdmin(oc, ns, "route/reen-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Allow"}}}`)
		output, err = oc.Run("get").Args("route/reen-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Allow"`))

		exutil.By("3.0: Test Route is accessible using http and https")
		routehost := "reen-route-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("http://"+routehost, "-k", "Hello-OpenShift "+srvPodList[0]+" https-8443 default")
		waitForOutsideCurlContains("https://"+routehost, "-k", "Hello-OpenShift "+srvPodList[0]+" https-8443 default")

		exutil.By("4.0: Add Allow in edge tls")
		patchResourceAsAdmin(oc, ns, "route/edge-route", `{"spec":{"tls": {"insecureEdgeTerminationPolicy":"Allow"}}}`)
		output, err = oc.Run("get").Args("route/edge-route", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"insecureEdgeTerminationPolicy":"Allow"`))

		exutil.By("5.0: Test Route is accessible using http and https")
		edgehost := "edge-route-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("http://"+edgehost, "-k", "Hello-OpenShift "+srvPodList[0]+" http-8080")
		waitForOutsideCurlContains("https://"+edgehost, "-k", "Hello-OpenShift "+srvPodList[0]+" http-8080")

	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-Critical-14678-NetworkEdge Only the host in whitelist could access unsecure/edge/reencrypt/passthrough routes", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			unSecSvcName        = "service-unsecure"
			signedPod           = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		)

		exutil.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := getOneRouterPodNameByIC(oc, "default")
		createResourceFromFile(oc, ns, signedPod)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("2.0: Create an unsecure, edge, reencrypt and passthrough route")
		domain := getIngressctlDomain(oc, "default")
		unsecureRoute := "route-unsecure"
		unsecureHost := unsecureRoute + "-" + ns + "." + domain
		edgeRoute := "route-edge"
		edgeHost := edgeRoute + "-" + ns + "." + domain
		passthroughRoute := "route-passthrough"
		passthroughHost := passthroughRoute + "-" + ns + "." + domain
		reenRoute := "route-reen"
		reenHost := reenRoute + "-" + ns + "." + domain

		createRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")
		createRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		createRoute(oc, ns, "passthrough", passthroughRoute, "service-secure", []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-passthrough", "default")
		createRoute(oc, ns, "reencrypt", reenRoute, "service-secure", []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		exutil.By("3.0: Annotate unsecure, edge, reencrypt and passthrough route")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation := getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))
		setAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation = getAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))
		setAnnotation(oc, ns, "route/"+passthroughRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation = getAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))
		setAnnotation(oc, ns, "route/"+reenRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation = getAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))

		exutil.By("4.0: access the routes using the IP from the whitelist")
		waitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift web-server-deploy`)
		waitForOutsideCurlContains("https://"+edgeHost, "-k", `Hello-OpenShift web-server-deploy`)
		waitForOutsideCurlContains("https://"+passthroughHost, "-k", `Hello-OpenShift web-server-deploy`)
		waitForOutsideCurlContains("https://"+reenHost, "-k", `Hello-OpenShift web-server-deploy`)

		exutil.By("5.0: re-annotate routes with a random IP")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=5.6.7.8`)
		findAnnotation = getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"5.6.7.8`))
		setAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_whitelist=5.6.7.8`)
		findAnnotation = getAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"5.6.7.8`))
		setAnnotation(oc, ns, "route/"+passthroughRoute, `haproxy.router.openshift.io/ip_whitelist=5.6.7.8`)
		findAnnotation = getAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"5.6.7.8`))
		setAnnotation(oc, ns, "route/"+reenRoute, `haproxy.router.openshift.io/ip_whitelist=5.6.7.8`)
		findAnnotation = getAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"5.6.7.8`))

		exutil.By("6.0: attempt to access the routes without an IP in the whitelist")
		cmd := fmt.Sprintf(`curl --connect-timeout 10 -s %s %s 2>&1`, "-I", "http://"+unsecureHost)
		result, _ := exec.Command("bash", "-c", cmd).Output()
		// use -I for 2 different scenarios, squid result has failure bad gateway, otherwise uses exit status
		if strings.Contains(string(result), `squid`) {
			waitForOutsideCurlContains("http://"+unsecureHost, "-I", `Bad Gateway`)
		} else {
			waitForOutsideCurlContains("http://"+unsecureHost, "", `exit status`)
		}
		waitForOutsideCurlContains("https://"+edgeHost, "-k", `exit status`)
		waitForOutsideCurlContains("https://"+passthroughHost, "-k", `exit status`)
		waitForOutsideCurlContains("https://"+reenHost, "-k", `exit status`)

		exutil.By("7.0: Check HaProxy if the IP in the whitelist annotation exists")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src 5.6.7.8"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+edgeRoute, []string{"acl allowlist src 5.6.7.8"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+passthroughRoute, []string{"acl allowlist src 5.6.7.8"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+reenRoute, []string{"acl allowlist src 5.6.7.8"})
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Low-14680-NetworkEdge Add invalid value in annotation whitelist to route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := getOneRouterPodNameByIC(oc, "default")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("2.0: Create an unsecure, route")
		unsecureRoute := "route-unsecure"
		unsecureHost := unsecureRoute + "-" + ns + ".apps." + getBaseDomain(oc)

		createRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")

		exutil.By("3.0: Annotate route with invalid whitelist value")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist='192.abc.123.0'`)
		findAnnotation := getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"'192.abc.123.0'"`))

		exutil.By("4.0: access the route using any host since whitelist is not in effect")
		waitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift web-server-deploy`)

		exutil.By("5.0: re-annotate route with IP that all Hosts can access")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0`)
		findAnnotation = getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0`))

		exutil.By("6.0: all hosts can access the route")
		waitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift web-server-deploy`)

		exutil.By("7.0: Check HaProxy if the IP in the whitelist annotation exists")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src 0.0.0.0/0"})
	})

	// incorporate OCP-15028 OCP-15071 OCP-15072 OCP-15073 into one
	// Test case creater: zzhao@redhat.com - OCP-15028: The router can do a case-insensitive match of a hostname for unsecure route
	// Test case creater: zzhao@redhat.com - OCP-15071: The router can do a case-insensitive match of a hostname for edge route
	// Test case creater: zzhao@redhat.com - OCP-15072: The router can do a case-insensitive match of a hostname for passthrough route
	// Test case creater: zzhao@redhat.com - OCP-15073: The router can do a case-insensitive match of a hostname for reencrypt route
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-15028-router can do a case-insensitive match of a hostname for unsecure/edge/passthrough/reencrypt route", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		caCert := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")
		UnsecureSvcName := "service-unsecure"
		SecureSvcName := "service-secure"

		exutil.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		exutil.By("2. Create a unsecure route and ensure the routes are loaded in haproxy.config of default controlle")
		createRoute(oc, ns, "http", "15028-http", UnsecureSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "15028-http", "default")
		httpHostCapital := "15028-HTTP-" + ns + ".apps." + baseDomain

		exutil.By("3. Create a edge route and ensure the routes are loaded in haproxy.config of default controlle")
		createRoute(oc, ns, "edge", "15028-edge", UnsecureSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "15028-edge", "default")
		edgeHostCapital := "15028-EDGE-" + ns + ".apps." + baseDomain

		exutil.By("4. Create a passthrough route and ensure the routes are loaded in haproxy.config of default controlle")
		createRoute(oc, ns, "passthrough", "15028-pass", SecureSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "15028-pass", "default")
		passHostCapital := "15028-PASS-" + ns + ".apps." + baseDomain

		exutil.By("5. Create a reen route and ensure the routes are loaded in haproxy.config of default controlle")
		createRoute(oc, ns, "reencrypt", "15028-reen", SecureSvcName, []string{"--dest-ca-cert=" + caCert})
		ensureRouteIsAdmittedByIngressController(oc, ns, "15028-reen", "default")
		reenHostCapital := "15028-REEN-" + ns + ".apps." + getBaseDomain(oc)
		getRoutes(oc, ns)

		// OCP-15028: The router can do a case-insensitive match of a hostname for unsecure route
		exutil.By("6. Check the reachability of case-insensitive match of the hostname for the unsecure route")
		waitForOutsideCurlContains("http://"+httpHostCapital, "-k", `Hello-OpenShift web-server-deploy`)

		// OCP-15071: The router can do a case-insensitive match of a hostname for edge route
		exutil.By("7. Check the reachability of case-insensitive match of the hostname for the edge route")
		waitForOutsideCurlContains("https://"+edgeHostCapital, "-k", `Hello-OpenShift web-server-deploy`)

		// OCP-15072: The router can do a case-insensitive match of a hostname for passthrough route
		exutil.By("8. Check the reachability of case-insensitive match of the hostname for the passthrough route")
		waitForOutsideCurlContains("https://"+passHostCapital, "-k", `Hello-OpenShift web-server-deploy`)

		// OCP-15073: The router can do a case-insensitive match of a hostname for reencrypt route
		exutil.By("9. Check the reachability of case-insensitive match of the hostname for the reencrypt route")
		waitForOutsideCurlContains("https://"+reenHostCapital, "-k", `Hello-OpenShift web-server-deploy`)
	})

	// incorporate OCP-10762 OCP-15113 OCP-15114 into one
	// Test case creater: zzhao@redhat.com - OCP-10762: Check the header forward format
	// Test case creater: zzhao@redhat.com - OCP-15113: Harden haproxy to prevent the PROXY header from being passed for unsecure route
	// Test case creater: zzhao@redhat.com - OCP-15114: Harden haproxy to prevent the PROXY header from being passed for edge route
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-15113-Harden haproxy to prevent the PROXY header from being passed for unsecure/edge route", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
		UnsecureSvcName := "httpbin-svc-insecure"

		exutil.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		exutil.By("2. Create a unsecure route")
		createRoute(oc, ns, "http", "15113-http", UnsecureSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "15113-http", "default")
		getRoutes(oc, ns)
		httpHost := "15113-http-" + ns + ".apps." + baseDomain

		exutil.By("3. Create a edge route")
		createRoute(oc, ns, "edge", "15113-edge", UnsecureSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "15113-edge", "default")
		getRoutes(oc, ns)
		edgeHost := "15113-edge-" + ns + ".apps." + baseDomain

		// OCP-10762: Check the header forward format
		exutil.By("4. Access the route and check the header forward format")
		result := waitForOutsideCurlContains("http://"+httpHost+"/headers", "", `proto=http`)
		o.Expect(result).To(o.ContainSubstring(`"Forwarded": "for=`))
		result = waitForOutsideCurlContains("https://"+edgeHost+"/headers", "-k", `proto=https`)
		o.Expect(result).To(o.ContainSubstring(`"Forwarded": "for=`))

		// OCP-15113: Harden haproxy to prevent the PROXY header from being passed for unsecure route
		exutil.By("5. Access the route with 'proxy' header and confirm the proxy is not carried with it")
		result = waitForOutsideCurlContains(httpHost+"/headers", "-H proxy:10.10.10.10", `"Host": "`+httpHost)
		o.Expect(result).NotTo(o.ContainSubstring(`proxy:10.10.10.10`))

		// OCP-15114: Harden haproxy to prevent the PROXY header from being passed for edge route
		exutil.By("6. Access the route with 'proxy' header and confirm the proxy is not carried with it")
		result = waitForOutsideCurlContains("https://"+edgeHost+"/headers", "-k -H proxy:10.10.10.10", `"Host": "`+edgeHost)
		o.Expect(result).NotTo(o.ContainSubstring(`proxy:10.10.10.10`))
	})

	// merge OCP-15874(NetworkEdge can set cookie name for reencrypt routes by annotation) to OCP-15873
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-15873-NetworkEdge can set cookie name for edge/reen routes by annotation", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			fileDir             = "/data/OCP-15873-cookie"
			ingctrl             = ingressControllerDescription{
				name:      "15873",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
		)

		exutil.By("1.0: Updated replicas in the web-server-signed-deploy.yaml for testing")
		updateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")

		exutil.By("2.0: Create a client pod, two server pods and the service")
		ns := oc.Namespace()
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		// create the cookie folder in the client pod
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, clientPodName, "--", "mkdir", fileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPodList := createResourceFromWebServer(oc, ns, testPodSvc, srvrcInfo)

		exutil.By("3.0: Create an edge route")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "edge15873" + "." + ingctrl.domain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)
		createRoute(oc, ns, "edge", "route-edge15873", unSecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge15873", "default")

		exutil.By("4.0: Set the cookie name by route annotation with router.openshift.io/cookie_name=2-edge_cookie")
		_, err = oc.Run("annotate").WithoutNamespace().Args("-n", ns, "route/route-edge15873", "router.openshift.io/cookie_name=2-edge_cookie", "--overwrite").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("5.0: Curl the edge route, and check the Set-Cookie header is set")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-kvs", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput := []string{"set-cookie: 2-edge_cookie=[0-9a-z]+"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		exutil.By("6.0: Curl the edge route, saving the cookie for one server")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-c" + fileDir + "/cookie-15873", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[1] + " http-8080"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		exutil.By("7.0: Curl the edge route with the cookie, expect all are forwarded to the desired server")
		curlCmdWithCookie := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-b", fileDir + "/cookie-15873", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[0] + " http-8080", "Hello-OpenShift " + srvPodList[1] + " http-8080"}
		_, result := repeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[1]).To(o.Equal(6))

		// test for NetworkEdge can set cookie name for reencrypt routes by annotation
		exutil.By("8.0: Create a reencrypt route")
		routehost = "reen15873" + "." + ingctrl.domain
		toDst = routehost + ":443:" + podIP
		createRoute(oc, ns, "reencrypt", "route-reen15873", secSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen15873", "default")

		exutil.By("9.0: Set the cookie name by route annotation with router.openshift.io/cookie_name=_reen-cookie3")
		_, err = oc.Run("annotate").WithoutNamespace().Args("-n", ns, "route/route-reen15873", "router.openshift.io/cookie_name=_reen-cookie3", "--overwrite").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("10.0: Curl the reencrypt route, and check the Set-Cookie header is set")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-kv", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"set-cookie: _reen-cookie3=[0-9a-z]+"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 60, 1)

		exutil.By("11.0: Curl the reen route, saving the cookie for one server")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-k", "-c", fileDir + "/cookie-15873", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift " + srvPodList[1] + " https-8443"}
		repeatCmdOnClient(oc, curlCmd, expectOutput, 120, 1)

		exutil.By("12.0: Curl the reen route with the cookie, expect all are forwarded to the desired server")
		curlCmdWithCookie = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "-b", fileDir + "/cookie-15873", "--resolve", toDst, "--connect-timeout", "10"}
		expectOutput = []string{"Hello-OpenShift +" + srvPodList[0] + " +https-8443", "Hello-OpenShift +" + srvPodList[1] + " +https-8443"}
		_, result = repeatCmdOnClient(oc, curlCmdWithCookie, expectOutput, 120, 6)
		o.Expect(result[1]).To(o.Equal(6))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Medium-16732-NetworkEdge Check haproxy.config when overwriting 'timeout server' which was already specified", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create single pod and the service")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		exutil.By("2.0: Create an unsecure route")
		routeName := unSecSvcName

		createRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		exutil.By("3.0: Annotate unsecure route")
		setAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=5s")
		findAnnotation := getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"5s`))

		exutil.By("4.0: Check HAProxy file for timeout server")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout server  5s"})

		// overwrite annotation with same parameter to check whether haProxy shows the same annotation twice
		exutil.By("5.0: Overwrite route annotation")
		setAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=5s")

		exutil.By("6.0: Check HAProxy file again for timeout server")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns, []string{ns + ":" + routeName})
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-ROSA-OSD_CCS-ARO-Critical-17145-haproxy router support websocket via unsecure route", func() {
		// On AWS (non private) proxy cluster, the url `*.apps.<baseDomain>` is unreachable from a test client pod
		// See also https://issues.redhat.com/browse/OCPQE-30244
		if checkProxy(oc) {
			g.Skip("Skipping on proxy cluster since no proxy ENV in the test client pod")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "websocket-deploy.yaml")
			unsecSvcName        = "ws-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodLabel      = "app=hello-pod"
		)

		exutil.By("1: Create a client pod, a server and its services in a namespace")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		// Client pod with websocket client tool
		updateFilebySedCmd(clientPod,
			"quay.io/openshifttest/nginx-alpine@sha256:cee6930776b92dc1e93b73f9e5965925d49cff3d2e91e1d071c2f0ff72cbca29",
			"quay.io/openshifttest/hello-sdn@sha256:c89445416459e7adea9a5a416b3365ed3d74f2491beb904d61dc8d1eb89a72a4")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		// Server pod with websocket testing capablity
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=hello-websocket")

		exutil.By("2: Create a http route for the testing")
		routehost := "unsecure17145-" + ns + ".apps." + baseDomain
		createRoute(oc, ns, "http", "unsecure17145", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsecure17145", "default")

		exutil.By("3: Curl the http route to ensure server is ready")
		curlCmd := []string{"-n", ns, "hello-pod", "--", "curl", "http://" + routehost + "/echo", "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "not websocket protocol", 60, 1)

		exutil.By("4: Use the unsecure route for confirming the websocket is working")
		cmd := fmt.Sprintf("(echo WebsocketTesting ; sleep 20) | ws ws://%s/echo", routehost)
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, "hello-pod", "--", "bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		// The websocket will echo what ever input we give
		o.Expect(output).To(o.ContainSubstring(`< WebsocketTesting`))
	})

	// author: iamin@redhat.com
	//combining OCP-18482 and OCP-18489 into one test
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-18482-NetworkEdge limits backend pod max concurrent connections for unsecure, edge, reen, passthrough route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
		)

		exutil.By("1.0: Create single pod and the services")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvcName), o.ContainSubstring(secSvcName)))

		exutil.By("2.0: Create an unsecure, edge and reencrypt route")
		unsecureRoute := "route-unsecure"
		edgeRoute := "route-edge"
		reenRoute := "route-reen"
		passthroughRoute := "route-passthrough"

		createRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")
		createRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		createRoute(oc, ns, "reencrypt", reenRoute, secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")
		createRoute(oc, ns, "passthrough", passthroughRoute, secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-passthrough", "default")

		exutil.By("3.0: Annotate the routes with rate-limit annotations")
		setAnnotation(oc, ns, "route/"+unsecureRoute, "haproxy.router.openshift.io/pod-concurrent-connections=1")
		findAnnotation := getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"1`))
		setAnnotation(oc, ns, "route/"+edgeRoute, "haproxy.router.openshift.io/pod-concurrent-connections=2")
		findAnnotation = getAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"2`))
		setAnnotation(oc, ns, "route/"+reenRoute, "haproxy.router.openshift.io/pod-concurrent-connections=3")
		findAnnotation = getAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"3`))
		setAnnotation(oc, ns, "route/"+passthroughRoute, "haproxy.router.openshift.io/pod-concurrent-connections=2")
		findAnnotation = getAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"2`))

		exutil.By("4.0: Check HAProxy file for route rate-limit annotation")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		ensureHaproxyBlockConfigContains(oc, routerpod, unsecureRoute, []string{"maxconn 1"})
		ensureHaproxyBlockConfigContains(oc, routerpod, edgeRoute, []string{"maxconn 2"})
		ensureHaproxyBlockConfigContains(oc, routerpod, reenRoute, []string{"maxconn 3"})
		ensureHaproxyBlockConfigContains(oc, routerpod, passthroughRoute, []string{"maxconn 2"})
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Medium-18490-NetworkEdge limits multiple backend pods max concurrent connections", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create single pod and its services")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		exutil.By("2.0: Scale deployment to have 2 pods")
		output, err = oc.AsAdmin().WithoutNamespace().Run("scale").Args("-n", ns, "deployment/web-server-deploy", "--replicas=2").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("web-server-deploy scaled"))
		waitForOutputEquals(oc, ns, "deployment/web-server-deploy", "{.status.availableReplicas}", "2")

		exutil.By("3.0: Create an edge route")
		edgeRoute := "route-edge"
		createRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")

		exutil.By("4.0: Annotate the edge route with rate-limit annotation")
		setAnnotation(oc, ns, "route/"+edgeRoute, "haproxy.router.openshift.io/pod-concurrent-connections=1")
		findAnnotation := getAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/pod-concurrent-connections":"1`))

		exutil.By("5.0: Check HAProxy file for route rate-limit annotation")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		searchOutput := ensureHaproxyBlockConfigContains(oc, routerpod, edgeRoute, []string{"maxconn 1"})
		count := strings.Count(searchOutput, "maxconn 1")
		o.Expect(count).To(o.Equal(2), "Expected the substring to appear exactly twice")
	})

	// Test case creater: zzhao@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Medium-19804-Unsecure route with path and another tls route with same hostname can work at the same time", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		unSecSvc := "service-unsecure"
		edgeRoute := "19804-edge"
		httpRoute := "19804-http"

		exutil.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		exutil.By("2. Create a http route")
		httpHost := edgeRoute + "-" + ns + ".apps." + baseDomain
		createRoute(oc, ns, "http", httpRoute, unSecSvc, []string{"--hostname=" + httpHost, "--path=/test"})
		ensureRouteIsAdmittedByIngressController(oc, ns, httpRoute, "default")

		exutil.By("3. Create a edge route")
		edgeHost := edgeRoute + "-" + ns + ".apps." + baseDomain
		createRoute(oc, ns, "edge", edgeRoute, unSecSvc, []string{"--insecure-policy=Allow"})
		ensureRouteIsAdmittedByIngressController(oc, ns, edgeRoute, "default")

		exutil.By("4. Access the route without path")
		getRoutes(oc, ns)
		waitForOutsideCurlContains("https://"+httpHost+"/test/", "-k", "Hello-OpenShift-Path-Test")

		exutil.By("5. Access the route with path")
		waitForOutsideCurlContains("https://"+edgeHost, "-k", "Hello-OpenShift")
	})

	// author: iamin@redhat.com
	//combining OCP-34106 and OCP-34168 into one
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-34106-NetworkEdge Routes annotated with 'haproxy.router.openshift.io/rewrite-target=/path' will replace and rewrite http request with specified '/path'", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create single pod, service")
		ns := oc.Namespace()
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		exutil.By("2.0: Expose the service to create http unsecure route")
		createRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "service-unsecure", "default")

		exutil.By("3.0: Annotate unsecure route with path rewrite target")
		setAnnotation(oc, ns, "route/"+unSecSvcName, `haproxy.router.openshift.io/rewrite-target=/path/second/`)
		findAnnotation := getAnnotation(oc, ns, "route", unSecSvcName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/rewrite-target":"/path/second/`))

		exutil.By("4.0: Curl route to see if the route will rewrite to second path")
		domain := getIngressctlDomain(oc, "default")
		unsecureHost := unSecSvcName + "-" + ns + "." + domain
		waitForOutsideCurlContains("http://"+unsecureHost, "", `second-test web-server-deploy`)

		exutil.By("5.0: Annotate unsecure route with rewrite target")
		setAnnotation(oc, ns, "route/"+unSecSvcName, `haproxy.router.openshift.io/rewrite-target=/`)
		findAnnotation = getAnnotation(oc, ns, "route", unSecSvcName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/rewrite-target":"/`))

		exutil.By("6.0: Curl route with different post-fixes in the web-server app")
		waitForOutsideCurlContains("http://"+unsecureHost+"/", "", `Hello-OpenShift web-server-deploy`)
		waitForOutsideCurlContains("http://"+unsecureHost+"/test/", "", `Hello-OpenShift-Path-Test web-server-deploy`)
		waitForOutsideCurlContains("http://"+unsecureHost+"/path/", "", `ocp-test web-server-deploy`)
		waitForOutsideCurlContains("http://"+unsecureHost+"/path/second/", "", `second-test web-server-deploy`)

	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-38671-NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation gets applied alongside 'haproxy.router.openshift.io/timeout' for clear/edge/reencrypt routes", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create single pod and 3 services")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvcName), o.ContainSubstring("service-secure")))

		exutil.By("2.0: Create a clear HTTP, edge and reen route")
		routeName := unSecSvcName

		createRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		createRoute(oc, ns, "edge", "edge-route", unSecSvcName, []string{})
		createRoute(oc, ns, "reencrypt", "reen-route", "service-secure", []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(unSecSvcName), o.ContainSubstring("edge-route"), o.ContainSubstring("reen-route")))

		exutil.By("3.0: Annotate all 3 routes")
		setAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout=15s")
		findAnnotation := getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"15s`))

		setAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/timeout=15s")
		findAnnotation = getAnnotation(oc, ns, "route", "edge-route")
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"15s`))

		setAnnotation(oc, ns, "route/reen-route", "haproxy.router.openshift.io/timeout=15s")
		findAnnotation = getAnnotation(oc, ns, "route", "reen-route")
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"15s`))

		exutil.By("4.0: Check HAProxy file for timeout server on the routes")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout server  15s"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":edge-route", []string{"timeout server  15s"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":reen-route", []string{"timeout server  15s"})

		exutil.By("5.0: Annotate all routes with timeout tunnel")
		setAnnotation(oc, ns, "route/"+routeName, "haproxy.router.openshift.io/timeout-tunnel=5s")
		findAnnotation = getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout-tunnel":"5s`))

		setAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/timeout-tunnel=5s")
		findAnnotation = getAnnotation(oc, ns, "route", "edge-route")
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout-tunnel":"5s`))

		setAnnotation(oc, ns, "route/reen-route", "haproxy.router.openshift.io/timeout-tunnel=5s")
		findAnnotation = getAnnotation(oc, ns, "route", "reen-route")
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout-tunnel":"5s`))

		exutil.By("6.0: Check HAProxy file for timeout tunnel on the routes")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout tunnel  5s"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout tunnel  5s"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+routeName, []string{"timeout tunnel  5s"})
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-38672-NetworkEdge 'haproxy.router.openshift.io/timeout-tunnel' annotation takes precedence over 'haproxy.router.openshift.io/timeout' values for passthrough routes", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			secSvcName          = "service-secure"
		)

		exutil.By("1.0: Create single pod, service")
		ns := oc.Namespace()
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("2.0: Create a passthrough route")
		routeName := "38672-route-passth"
		createRoute(oc, ns, "passthrough", routeName, secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")

		exutil.By("3.0: Annotate passthrough route with two timeout annotations")
		setAnnotation(oc, ns, "route/"+routeName, `haproxy.router.openshift.io/timeout=15s`)
		setAnnotation(oc, ns, "route/"+routeName, `haproxy.router.openshift.io/timeout-tunnel=5s`)
		findAnnotation := getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"15s`))
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout-tunnel":"5s`))

		exutil.By("4.0: Check HaProxy to see if timeout tunnel overrides timeout")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		ensureHaproxyBlockConfigContains(oc, routerpod, routeName, []string{"timeout tunnel  5s"})

		exutil.By("5.0: Remove the timeout tunnel annotation")
		setAnnotation(oc, ns, "route/"+routeName, `haproxy.router.openshift.io/timeout-tunnel-`)

		exutil.By("6.0: Check Haproxy to see if timeout annotation is present")
		ensureHaproxyBlockConfigContains(oc, routerpod, routeName, []string{"timeout tunnel  15s"})
	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-ROSA-OSD_CCS-ARO-Medium-42230-route can be configured to whitelist more than 61 ips/CIDRs", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			output              string
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		)
		exutil.By("Create pod, svc resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("expose a service in the namespace")
		createRoute(oc, ns, "http", "service-unsecure", "service-unsecure", []string{})
		output, err := oc.Run("get").Args("-n", ns, "route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("service-unsecure"))

		exutil.By("annotate the route with haproxy.router.openshift.io/ip_whitelist with 61 CIDR values and verify")
		setAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/ip_whitelist=192.168.0.0/24 192.168.1.0/24 192.168.2.0/24 192.168.3.0/24 192.168.4.0/24 192.168.5.0/24 192.168.6.0/24 192.168.7.0/24 192.168.8.0/24 192.168.9.0/24 192.168.10.0/24 192.168.11.0/24 192.168.12.0/24 192.168.13.0/24 192.168.14.0/24 192.168.15.0/24 192.168.16.0/24 192.168.17.0/24 192.168.18.0/24 192.168.19.0/24 192.168.20.0/24 192.168.21.0/24 192.168.22.0/24 192.168.23.0/24 192.168.24.0/24 192.168.25.0/24 192.168.26.0/24 192.168.27.0/24 192.168.28.0/24 192.168.29.0/24 192.168.30.0/24 192.168.31.0/24 192.168.32.0/24 192.168.33.0/24 192.168.34.0/24 192.168.35.0/24 192.168.36.0/24 192.168.37.0/24 192.168.38.0/24 192.168.39.0/24 192.168.40.0/24 192.168.41.0/24 192.168.42.0/24 192.168.43.0/24 192.168.44.0/24 192.168.45.0/24 192.168.46.0/24 192.168.47.0/24 192.168.48.0/24 192.168.49.0/24 192.168.50.0/24 192.168.51.0/24 192.168.52.0/24 192.168.53.0/24 192.168.54.0/24 192.168.55.0/24 192.168.56.0/24 192.168.57.0/24 192.168.58.0/24 192.168.59.0/24 192.168.60.0/24")
		output, err = oc.Run("get").Args("-n", ns, "route", "service-unsecure", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/ip_whitelist"))

		exutil.By("verify the acl whitelist parameter inside router pod for whitelist with 61 CIDR values")
		podName := getOneRouterPodNameByIC(oc, "default")
		//backendName is the leading context of the route
		backendName := "be_http:" + ns + ":service-unsecure"
		ensureHaproxyBlockConfigContains(oc, podName, backendName, []string{"acl allowlist src 192.168.0.0/24", "tcp-request content reject if !allowlist"})
		ensureHaproxyBlockConfigNotContains(oc, podName, backendName, []string{"acl allowlist src -f /var/lib/haproxy/router/allowlists/"})

		exutil.By("annotate the route with haproxy.router.openshift.io/ip_whitelist with more than 61 CIDR values and verify")
		setAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/ip_whitelist=192.168.0.0/24 192.168.1.0/24 192.168.2.0/24 192.168.3.0/24 192.168.4.0/24 192.168.5.0/24 192.168.6.0/24 192.168.7.0/24 192.168.8.0/24 192.168.9.0/24 192.168.10.0/24 192.168.11.0/24 192.168.12.0/24 192.168.13.0/24 192.168.14.0/24 192.168.15.0/24 192.168.16.0/24 192.168.17.0/24 192.168.18.0/24 192.168.19.0/24 192.168.20.0/24 192.168.21.0/24 192.168.22.0/24 192.168.23.0/24 192.168.24.0/24 192.168.25.0/24 192.168.26.0/24 192.168.27.0/24 192.168.28.0/24 192.168.29.0/24 192.168.30.0/24 192.168.31.0/24 192.168.32.0/24 192.168.33.0/24 192.168.34.0/24 192.168.35.0/24 192.168.36.0/24 192.168.37.0/24 192.168.38.0/24 192.168.39.0/24 192.168.40.0/24 192.168.41.0/24 192.168.42.0/24 192.168.43.0/24 192.168.44.0/24 192.168.45.0/24 192.168.46.0/24 192.168.47.0/24 192.168.48.0/24 192.168.49.0/24 192.168.50.0/24 192.168.51.0/24 192.168.52.0/24 192.168.53.0/24 192.168.54.0/24 192.168.55.0/24 192.168.56.0/24 192.168.57.0/24 192.168.58.0/24 192.168.59.0/24 192.168.60.0/24 192.168.61.0/24")
		output1, err1 := oc.Run("get").Args("-n", ns, "route", "service-unsecure", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err1).NotTo(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("haproxy.router.openshift.io/ip_whitelist"))

		exutil.By("verify the acl whitelist parameter inside router pod for whitelist with 62 CIDR values")
		//backendName is the leading context of the route
		ensureHaproxyBlockConfigContains(oc, podName, backendName, []string{`acl allowlist src -f /var/lib/haproxy/router/allowlists/` + ns + `:service-unsecure.txt`, `tcp-request content reject if !allowlist`})
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-45399-ingress controller continue to function normally with unexpected high timeout value", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			output              string
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		)
		exutil.By("Create pod, svc resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("expose a service in the namespace")
		createRoute(oc, ns, "http", "service-secure", "service-secure", []string{})
		output, err := oc.Run("get").Args("-n", ns, "route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("service-secure"))

		exutil.By("annotate the route with haproxy.router.openshift.io/timeout annotation to high value and verify")
		setAnnotation(oc, ns, "route/service-secure", "haproxy.router.openshift.io/timeout=9999d")
		output, err = oc.Run("get").Args("-n", ns, "route", "service-secure", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`haproxy.router.openshift.io/timeout":"9999d`))

		exutil.By("Verify the haproxy configuration for the set timeout value")
		podName := getOneRouterPodNameByIC(oc, "default")
		ensureHaproxyBlockConfigContains(oc, podName, ns, []string{"timeout server  2147483647ms"})

		exutil.By("Verify the pod logs to see any timer overflow error messages")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", podName, "-c", "router").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(log).NotTo(o.ContainSubstring(`timer overflow`))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-High-45741-ingress canary route redirects http to https", func() {
		var ns = "openshift-ingress-canary"
		exutil.By("get the ingress route host")
		canaryRouteHost := getByJsonPath(oc, ns, "route/canary", "{.status.ingress[0].host}")
		o.Expect(canaryRouteHost).Should(o.ContainSubstring(`canary-openshift-ingress-canary.apps`))

		exutil.By("curl canary route via http and redirects to https")
		waitForOutsideCurlContains("http://"+canaryRouteHost, "-I", "302 Found")
		waitForOutsideCurlContains("http://"+canaryRouteHost, "-kL", "Healthcheck requested")
		waitForOutsideCurlContains("https://"+canaryRouteHost, "-k", "Healthcheck requested")
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonPreRelease-PreChkUpgrade-High-45955-Unidling a route work without user intervention", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			ns                  = "ocp45955"
		)

		networkType := exutil.CheckNetworkType(oc)
		if !(strings.Contains(networkType, "openshiftsdn") || strings.Contains(networkType, "ovn")) {
			g.Skip("Skipping because idling is not supported on the network type")
		}

		exutil.By("1.0: Create a new project and a web-server application")
		oc.CreateSpecifiedNamespaceAsAdmin(ns)
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", testPodSvc, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getOnePodNameByLabel(oc, ns, "name=web-server-deploy")

		exutil.By("2.0: Confirm that the service exists and expose it")
		_, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, "service", "service-unsecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = oc.AsAdmin().WithoutNamespace().Run("expose").Args("service", "service-unsecure", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureRouteIsAdmittedByIngressController(oc, ns, "service-unsecure", "default")

		exutil.By("3.0: Access the exposed route")
		routehost := getRouteHost(oc, ns, "service-unsecure")
		curlCmd := fmt.Sprintf(`curl http://%s --connect-timeout 10`, routehost)
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 120, 1)

		exutil.By("4.0: Idle the service and wait for the pod resource to dissapear")
		err = oc.AsAdmin().WithoutNamespace().Run("idle").Args("service-unsecure", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		errResource := waitForResourceToDisappear(oc, ns, "pod/"+podName)
		o.Expect(errResource).NotTo(o.HaveOccurred())

		exutil.By("5.0: Confirm the service annotation is present")
		findAnnotation := getAnnotation(oc, ns, "service", "service-unsecure")
		o.Expect(findAnnotation).To(o.ContainSubstring(`idling.alpha.openshift.io/unidle-targets":"[{\"kind\":\"Deployment\",\"name\":\"web-server-deploy\",\"group\":\"apps\",\"replicas\":1}]`))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonPreRelease-PstChkUpgrade-High-45955-Unidling a route work without user intervention", func() {
		var (
			ns = "ocp45955"
		)

		networkType := exutil.CheckNetworkType(oc)
		if !(strings.Contains(networkType, "openshiftsdn") || strings.Contains(networkType, "ovn")) {
			g.Skip("Skipping because idling is not supported on the network type")
		}

		exutil.By("1.0: Confirm that the service is still available with the annotation")
		_, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, "service", "service-unsecure").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		findAnnotation := getAnnotation(oc, ns, "service", "service-unsecure")
		o.Expect(findAnnotation).To(o.ContainSubstring(`idling.alpha.openshift.io/unidle-targets":"[{\"kind\":\"Deployment\",\"name\":\"web-server-deploy\",\"group\":\"apps\",\"replicas\":1}]`))

		exutil.By("2.0: Access the route to unidle the service")
		routehost := getRouteHost(oc, ns, "service-unsecure")
		curlCmd := fmt.Sprintf(`curl http://%s --connect-timeout 10`, routehost)
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 120, 1)

		exutil.By("3.0: Check the service to see if the idle annotation is removed")
		findAnnotation = getAnnotation(oc, ns, "service", "service-unsecure")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`idling.alpha.openshift.io/unidle-targets`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-49802-HTTPS redirect happens even if there is a more specific http-only", func() {
		// curling through default controller will not work for proxy cluster.
		if checkProxy(oc) {
			g.Skip("This is proxy cluster, skip the test.")
		}
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "49802-route.yaml")
			rut                 = routeDescription{
				namespace: "",
				template:  customTemp,
			}
		)

		exutil.By("Create a pod")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")
		defaultContPod := getOneRouterPodNameByIC(oc, "default")

		exutil.By("create routes and get the details")
		rut.namespace = ns
		rut.create(oc)
		getRoutes(oc, ns)

		exutil.By("check the reachability of the secure route with redirection")
		waitForCurl(oc, podName[0], baseDomain, "hello-pod-"+ns+".apps.", "HTTP/1.1 302 Found", "")
		waitForCurl(oc, podName[0], baseDomain, "hello-pod-"+ns+".apps.", `location: https://hello-pod-`, "")

		exutil.By("check the reachability of the insecure routes")
		waitForCurl(oc, podName[0], baseDomain+"/test/", "hello-pod-http-"+ns+".apps.", "HTTP/1.1 200 OK", "")

		exutil.By("check the reachability of the secure route")
		curlCmd := fmt.Sprintf("curl -I -k https://hello-pod-%s.apps.%s --connect-timeout 10", ns, baseDomain)
		statsOut, err := exutil.RemoteShPod(oc, ns, podName[0], "sh", "-c", curlCmd)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(statsOut).Should(o.ContainSubstring("HTTP/1.1 200 OK"))

		exutil.By("check the router pod and ensure the routes are loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, defaultContPod, "cat haproxy.config", "hello-pod")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_edge_http:" + ns + ":hello-pod"))
		searchOutput1 := readRouterPodData(oc, defaultContPod, "cat haproxy.config", "hello-pod-http")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_http:" + ns + ":hello-pod-http"))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-53696-Route status should updates accordingly when ingress routes cleaned up [Disruptive]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp53696",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("check the intial canary route status")
		ensureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")

		exutil.By("shard the default ingress controller")
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		defer patchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"type\":null}}}}")
		patchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"type\":\"shard\"}}}}")
		// After patching the default congtroller generation should be +1
		actualGenerationInt, _ := strconv.Atoi(actualGen)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		exutil.By("check whether canary route status is cleared")
		checkRouteDetailsRemoved(oc, "openshift-ingress-canary", "canary", "default")

		exutil.By("patch the controller back to default check the canary route status")
		patchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"type\":null}}}}")
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))
		ensureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")

		exutil.By("Create a shard ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = "shard." + baseDomain
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("patch the shard controller and check the canary route status")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"nodePlacement\":{\"nodeSelector\":{\"matchLabels\":{\"node-role.kubernetes.io/worker\":\"\"}}}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		ensureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")
		ensureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", ingctrl.name)

		exutil.By("delete the shard and check the status")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ingctrl.delete(oc)
		err3 := waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+custContPod)
		exutil.AssertWaitPollNoErr(err3, fmt.Sprintf("Router  %v failed to fully terminate", "pod/"+custContPod))
		ensureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")
		checkRouteDetailsRemoved(oc, "openshift-ingress-canary", "canary", ingctrl.name)
	})

	// bugzilla: 2021446
	// no ingress-operator pod on HyperShift guest cluster so this case is not available
	g.It("Author:mjoseph-NonHyperShiftHOST-High-55895-Ingress should be in degraded status when canary route is not available [Disruptive]", func() {
		exutil.By("Check the intial co/ingress and canary route status")
		ensureClusterOperatorNormal(oc, "ingress", 1, 10)
		ensureRouteIsAdmittedByIngressController(oc, "openshift-ingress-canary", "canary", "default")

		exutil.By("Check the reachability of the canary route")
		baseDomain := getBaseDomain(oc)
		operatorPod := getPodListByLabel(oc, "openshift-ingress-operator", "name=ingress-operator")
		routehost := "canary-openshift-ingress-canary.apps." + baseDomain
		cmdOnPod := []string{operatorPod[0], "-n", "openshift-ingress-operator", "--", "curl", "-k", "https://" + routehost, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, cmdOnPod, "Healthcheck requested", 30, 1)

		exutil.By("Patch the ingress controller and deleting the canary route")
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		defer ensureClusterOperatorNormal(oc, "ingress", 3, 300)
		defer patchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":null}}")
		patchResourceAsAdmin(oc, "openshift-ingress-operator", "ingresscontrollers/default", "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"type\":\"default\"}}}}")
		// Deleting canary route
		err := oc.AsAdmin().Run("delete").Args("-n", "openshift-ingress-canary", "route", "canary").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		// After patching the default congtroller generation should be +1
		actualGenerationInt, _ := strconv.Atoi(actualGen)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		exutil.By("Check whether the canary route status cleared and confirm the route is not accessible")
		checkRouteDetailsRemoved(oc, "openshift-ingress-canary", "canary", "default")
		cmdOnPod = []string{operatorPod[0], "-n", "openshift-ingress-operator", "--", "curl", "-Ik", "https://" + routehost, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, cmdOnPod, "503", 120, 1)

		// Wait may be about 300 seconds
		exutil.By("Check the ingress operator status to confirm it is in degraded state cause by canary route")
		jpath := "{.status.conditions[*].message}"
		waitForOutputContains(oc, "default", "co/ingress", jpath, "The \"default\" ingress controller reports Degraded=True")
		waitForOutputContains(oc, "default", "co/ingress", jpath, "Canary route is not admitted by the default ingress controller")
	})

	// bugzilla: 1934904
	// Jira: OCPBUGS-9274
	// no openshift-machine-api namespace on HyperShift guest cluster so this case is not available
	g.It("Author:mjoseph-NonHyperShiftHOST-NonPreRelease-High-56240-Canary daemonset can schedule pods to both worker and infra nodes [Disruptive]", func() {
		var (
			infrastructureName = clusterinfra.GetInfrastructureName(oc)
			machineSetName     = infrastructureName + "-56240"
		)

		exutil.By("Check the intial machines and canary pod details")
		getResourceName(oc, "openshift-machine-api", "machine")
		getResourceName(oc, "openshift-ingress-canary", "pods")

		exutil.By("Create a new machineset")
		clusterinfra.SkipConditionally(oc)
		ms := clusterinfra.MachineSetDescription{Name: machineSetName, Replicas: 1}
		defer ms.DeleteMachineSet(oc)
		ms.CreateMachineSet(oc)

		exutil.By("Update machineset to schedule infra nodes")
		out, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("machinesets.machine.openshift.io", machineSetName, "-n", "openshift-machine-api", "-p", `{"spec":{"template":{"spec":{"taints":null}}}}`, "--type=merge").Output()
		o.Expect(out).To(o.ContainSubstring("machineset.machine.openshift.io/" + machineSetName + " patched"))
		out, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args("machinesets.machine.openshift.io", machineSetName, "-n", "openshift-machine-api", "-p", `{"spec":{"template":{"spec":{"metadata":{"labels":{"ingress": "true", "node-role.kubernetes.io/infra": ""}}}}}}`, "--type=merge").Output()
		o.Expect(out).To(o.ContainSubstring("machineset.machine.openshift.io/" + machineSetName + " patched"))
		updatedMachineName := clusterinfra.WaitForMachinesRunningByLabel(oc, 1, "machine.openshift.io/cluster-api-machineset="+machineSetName)

		exutil.By("Reschedule the running machineset with infra details")
		clusterinfra.DeleteMachine(oc, updatedMachineName[0])
		updatedMachineName1 := clusterinfra.WaitForMachinesRunningByLabel(oc, 1, "machine.openshift.io/cluster-api-machineset="+machineSetName)

		exutil.By("Check the canary deamonset is scheduled on infra node which is newly created")
		// confirm the new machineset is already created
		updatedMachineSetName := clusterinfra.ListWorkerMachineSetNames(oc)
		checkGivenStringPresentOrNot(true, updatedMachineSetName, machineSetName)
		// confirm infra node presence among the nodes
		infraNode := getByLabelAndJsonPath(oc, "default", "node", "node-role.kubernetes.io/infra", "{.items[*].metadata.name}")
		// confirm a canary pod got scheduled on to the infra node
		searchInDescribeResource(oc, "node", infraNode, "canary")

		exutil.By("Confirming the canary namespace is over-rided with the default node selector")
		annotations, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ns", "openshift-ingress-canary", "-ojsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(annotations).To(o.ContainSubstring(`openshift.io/node-selector":""`))

		exutil.By("Confirming the canary daemonset has the default tolerations included for infra role")
		tolerations := getByJsonPath(oc, "openshift-ingress-canary", "daemonset/ingress-canary", "{.spec.template.spec.tolerations}")
		o.Expect(tolerations).To(o.ContainSubstring(`key":"node-role.kubernetes.io/infra`))

		exutil.By("Tainting the infra nodes with 'NoSchedule' and confirm canary pods continues to remain up and functional on those nodes")
		nodeNameOfMachine := clusterinfra.GetNodeNameFromMachine(oc, updatedMachineName1[0])
		output, err := oc.AsAdmin().WithoutNamespace().Run("adm").Args("taint", "nodes", nodeNameOfMachine, "node-role.kubernetes.io/infra:NoSchedule").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("node/" + nodeNameOfMachine + " tainted"))
		// confirm the canary pod is still present in the infra node
		searchInDescribeResource(oc, "node", infraNode, "canary")

		exutil.By("Tainting the infra nodes with 'NoExecute' and confirm canary pods continues to remain up and functional on those nodes")
		output1, err1 := oc.AsAdmin().WithoutNamespace().Run("adm").Args("taint", "nodes", nodeNameOfMachine, "node-role.kubernetes.io/infra:NoExecute").Output()
		o.Expect(err1).NotTo(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("node/" + nodeNameOfMachine + " tainted"))
		// confirm the canary pod is still present in the infra node
		searchInDescribeResource(oc, "node", infraNode, "canary")
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Medium-63004-Ipv6 addresses are also acceptable for whitelisting", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			output              string
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		)

		exutil.By("Create a server pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("expose a service in the namespace")
		createRoute(oc, ns, "http", "service-unsecure", "service-unsecure", []string{})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("service-unsecure"))

		exutil.By("Annotate the route with Ipv6 subnet and verify it")
		setAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/ip_whitelist=2600:14a0::/40")
		output, err = oc.Run("get").Args("route", "service-unsecure", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`"haproxy.router.openshift.io/ip_whitelist":"2600:14a0::/40"`))

		exutil.By("Verify the acl whitelist parameter inside router pod with Ipv6 address")
		defaultPod := getOneRouterPodNameByIC(oc, "default")
		backendName := "be_http:" + ns + ":service-unsecure"
		ensureHaproxyBlockConfigContains(oc, defaultPod, backendName, []string{"acl allowlist src 2600:14a0::/40"})
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-High-73771-router can load secret", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			requiredRole        = filepath.Join(buildPruningBaseDir, "ocp73771-role.yaml")
			unsecsvcName        = "service-unsecure"
			secsvcName          = "service-secure"
			tmpdir              = "/tmp/OCP-73771-CA/"
			caKey               = tmpdir + "ca.key"
			caCrt               = tmpdir + "ca.crt"
			serverKey           = tmpdir + "server.key"
			serverCsr           = tmpdir + "server.csr"
			serverCrt           = tmpdir + "server.crt"
			multiServerCrt      = tmpdir + "multiserver.crt"
		)
		exutil.By("Create pod, svc resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("Create edge/passthrough/reencrypt routes and all should be reachable")
		extraParas := []string{}
		createRoute(oc, ns, "edge", "myedge", unsecsvcName, extraParas)
		createRoute(oc, ns, "passthrough", "mypass", secsvcName, extraParas)
		createRoute(oc, ns, "reencrypt", "myreen", secsvcName, extraParas)
		edgeRouteHost := getRouteHost(oc, ns, "myedge")
		passRouteHost := getRouteHost(oc, ns, "mypass")
		reenRouteHost := getRouteHost(oc, ns, "myreen")
		waitForOutsideCurlContains("https://"+edgeRouteHost, "-k", "Hello-OpenShift")
		waitForOutsideCurlContains("https://"+passRouteHost, "-k", "Hello-OpenShift")
		waitForOutsideCurlContains("https://"+reenRouteHost, "-k", "Hello-OpenShift")

		exutil.By("should be failed if patch the edge route without required role and secret")
		err1 := "Forbidden: router serviceaccount does not have permission to get this secret"
		err2 := "Forbidden: router serviceaccount does not have permission to watch this secret"
		err3 := "Forbidden: router serviceaccount does not have permission to list this secret"
		err4 := `Not found: "secrets \"mytls\" not found`
		output, err := oc.WithoutNamespace().Run("patch").Args("-n", ns, "route/myedge", "-p", `{"spec":{"tls":{"externalCertificate":{"name":"mytls"}}}}`, "--type=merge").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).Should(o.And(
			o.ContainSubstring(err1),
			o.ContainSubstring(err2),
			o.ContainSubstring(err3),
			o.ContainSubstring(err4)))

		exutil.By("create required role/rolebinding and secret")
		// create required role and rolebinding
		createResourceFromFile(oc, ns, requiredRole)
		// prepare the tmp folder and create self-signed cerfitcate
		defer os.RemoveAll(tmpdir)
		err = os.MkdirAll(tmpdir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslNewCa(caKey, caCrt, "/CN=ne-root-ca")
		opensslNewCsr(serverKey, serverCsr, "/CN=ne-server-cert")
		// san just contains edge route host but not reen route host
		san := "subjectAltName=DNS:" + edgeRouteHost
		opensslSignCsr(san, serverCsr, caCrt, caKey, serverCrt)
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "secret", "tls", "mytls", "--cert="+serverCrt, "--key="+serverKey).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("patch the edge and reen route, but only edge route should be reachable")
		patchResourceAsAdmin(oc, ns, "route/myedge", `{"spec":{"tls":{"externalCertificate":{"name":"mytls"}}}}`)
		patchResourceAsAdmin(oc, ns, "route/myreen", `{"spec":{"tls":{"externalCertificate":{"name":"mytls"}}}}`)
		curlOptions := fmt.Sprintf("--cacert %v", caCrt)
		waitForOutsideCurlContains("https://"+edgeRouteHost, curlOptions, "Hello-OpenShift")
		repeatCmdOnClient(oc, fmt.Sprintf("curl https://%s  %s --connect-timeout 10", reenRouteHost, curlOptions), `exit status (51|60)`, 60, 1)

		exutil.By("renew the server certificate with multi SAN and refresh the secret")
		// multiSan contains both edge and reen route host
		multiSan := san + ", DNS:" + reenRouteHost
		opensslSignCsr(multiSan, serverCsr, caCrt, caKey, multiServerCrt)
		newSecretYaml, err := oc.Run("create").Args("-n", ns, "secret", "tls", "mytls", "--cert="+multiServerCrt, "--key="+serverKey, "--dry-run=client", "-o=yaml").OutputToFile("ocp73771-newsecret.yaml")
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.WithoutNamespace().Run("apply").Args("-f", newSecretYaml).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("with the updated secret, both edge and reen route should be reachable")
		waitForOutsideCurlContains("https://"+edgeRouteHost, curlOptions, "Hello-OpenShift")
		waitForOutsideCurlContains("https://"+reenRouteHost, curlOptions, "Hello-OpenShift")

		exutil.By("should failed to patch passthrough route with externalCertificate")
		output, err = oc.WithoutNamespace().Run("patch").Args("-n", ns, "route/mypass", "-p", `{"spec":{"tls":{"externalCertificate":{"name":"mytls"}}}}`, "--type=merge").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("passthrough termination does not support certificate"))

		exutil.By("edge route reports error after deleting the referenced secret")
		err = oc.Run("delete").Args("-n", ns, "secret", "mytls").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputContains(oc, ns, "route/myedge", `{.status.ingress[?(@.routerName=="default")].conditions[*]}`, "ExternalCertificateValidationFailed")

		// https://issues.redhat.com/browse/OCPBUGS-33958 (4.19+)
		exutil.By("edge and reen route should be recovered after recreating the referenced secret")
		err = oc.WithoutNamespace().Run("apply").Args("-f", newSecretYaml).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureRouteIsAdmittedByIngressController(oc, ns, "myedge", "default")
		waitForOutsideCurlContains("https://"+edgeRouteHost, curlOptions, "Hello-OpenShift")
		waitForOutsideCurlContains("https://"+reenRouteHost, curlOptions, "Hello-OpenShift")
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-77080-NetworkEdge Only host in allowlist can access unsecure/edge/reencrypt/passthrough routes", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			signedPod           = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		)

		exutil.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := getOneRouterPodNameByIC(oc, "default")
		srvPodList := createResourceFromWebServer(oc, ns, signedPod, "web-server-deploy")

		exutil.By("2.0: Create an unsecure, edge, reencrypt and passthrough route")
		domain := getIngressctlDomain(oc, "default")
		unsecureRoute := "route-unsecure"
		unsecureHost := unsecureRoute + "-" + ns + "." + domain
		edgeRoute := "route-edge"
		edgeHost := edgeRoute + "-" + ns + "." + domain
		passthroughRoute := "route-passthrough"
		passthroughHost := passthroughRoute + "-" + ns + "." + domain
		reenRoute := "route-reen"
		reenHost := reenRoute + "-" + ns + "." + domain

		createRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")
		createRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		createRoute(oc, ns, "passthrough", passthroughRoute, secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-passthrough", "default")
		createRoute(oc, ns, "reencrypt", reenRoute, secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		exutil.By("3.0: Annotate unsecure, edge, reencrypt and passthrough route")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation := getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))
		setAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation = getAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))
		setAnnotation(oc, ns, "route/"+passthroughRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation = getAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))
		setAnnotation(oc, ns, "route/"+reenRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation = getAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))

		exutil.By("4.0: access the routes using the IP from the allowlist")
		waitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift `+srvPodList[0]+` http-8080`)
		waitForOutsideCurlContains("https://"+edgeHost, "-k", `Hello-OpenShift `+srvPodList[0]+` http-8080`)
		waitForOutsideCurlContains("https://"+passthroughHost, "-k", `Hello-OpenShift `+srvPodList[0]+` https-8443 default`)
		waitForOutsideCurlContains("https://"+reenHost, "-k", `Hello-OpenShift `+srvPodList[0]+` https-8443 default`)

		exutil.By("5.0: re-annotate routes with a random IP")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=1050::5:600:300c:326b`)
		findAnnotation = getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"1050::5:600:300c:326b`))
		setAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_allowlist=8.8.8.8`)
		findAnnotation = getAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"8.8.8.8`))
		setAnnotation(oc, ns, "route/"+passthroughRoute, `haproxy.router.openshift.io/ip_allowlist=1050::5:600:300c:326b`)
		findAnnotation = getAnnotation(oc, ns, "route", passthroughRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"1050::5:600:300c:326b`))
		setAnnotation(oc, ns, "route/"+reenRoute, `haproxy.router.openshift.io/ip_allowlist=8.8.4.4`)
		findAnnotation = getAnnotation(oc, ns, "route", reenRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"8.8.4.4`))

		exutil.By("6.0: attempt to access the routes without an IP in the allowlist")
		cmd := fmt.Sprintf(`curl --connect-timeout 10 -s %s %s 2>&1`, "-I", "http://"+unsecureHost)
		result, _ := exec.Command("bash", "-c", cmd).Output()
		// use -I for 2 different scenarios, squid result has failure bad gateway, otherwise uses exit status
		if strings.Contains(string(result), `squid`) {
			waitForOutsideCurlContains("http://"+unsecureHost, "-I", `Bad Gateway`)
		} else {
			waitForOutsideCurlContains("http://"+unsecureHost, "", `exit status`)
		}
		waitForOutsideCurlContains("https://"+edgeHost, "-k", `exit status`)
		waitForOutsideCurlContains("https://"+passthroughHost, "-k", `exit status`)
		waitForOutsideCurlContains("https://"+reenHost, "-k", `exit status`)

		exutil.By("7.0: Check HaProxy if the IP in the allowlist annotation exists")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src 1050::5:600:300c:326b", "tcp-request content reject if !allowlist"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+edgeRoute, []string{"acl allowlist src 8.8.8.8", "tcp-request content reject if !allowlist"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+passthroughRoute, []string{"acl allowlist src 1050::5:600:300c:326b", "tcp-request content reject if !allowlist"})
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+reenRoute, []string{"acl allowlist src 8.8.4.4", "tcp-request content reject if !allowlist"})
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-77082-NetworkEdge Route gives allowlist precedence when whitelist and allowlist annotations are both present", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPod             = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := getOneRouterPodNameByIC(oc, "default")
		srvPodList := createResourceFromWebServer(oc, ns, testPod, "web-server-deploy")
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("2.0: Create an unsecure route")
		unsecureRoute := "route-unsecure"
		unsecureHost := unsecureRoute + "-" + ns + ".apps." + getBaseDomain(oc)
		createRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")

		exutil.By("3.0: Annotate unsecure route")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=0.0.0.0/0 ::/0`)
		findAnnotation := getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"0.0.0.0/0 ::/0`))

		exutil.By("4.0: access the route using the IP from the whitelist")
		waitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift `+srvPodList[0]+` http-8080`)

		exutil.By("5.0: add allowlist annotation with non valid host IP")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=1.2.3.4`)
		findAnnotation = getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"1.2.3.4`))

		exutil.By("6.0: attempt to access the routes without an IP in the allowlist")
		cmd := fmt.Sprintf(`curl --connect-timeout 10 -s %s %s 2>&1`, "-I", "http://"+unsecureHost)
		result, _ := exec.Command("bash", "-c", cmd).Output()
		// use -I for 2 different scenarios, squid result has failure bad gateway, otherwise uses exit status
		if strings.Contains(string(result), `squid`) {
			waitForOutsideCurlContains("http://"+unsecureHost, "-I", `Bad Gateway`)
		} else {
			waitForOutsideCurlContains("http://"+unsecureHost, "", `exit status`)
		}

		exutil.By("7.0: annotate route with a valid public client IP in the allowlist and an invalid host IP in the whitelist")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=0.0.0.0/0 ::/0`)
		findAnnotation = getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"0.0.0.0/0 ::/0`))

		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_whitelist=1.2.3.4`)
		findAnnotation1 := getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation1).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_whitelist":"1.2.3.4`))

		waitForOutsideCurlContains("http://"+unsecureHost, "", `Hello-OpenShift `+srvPodList[0]+` http-8080`)

		exutil.By("8.0: Check HaProxy if the allowlist annotation exists and tcp request exist")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src", "tcp-request content reject if !allowlist"})
	})

	// author: iamin@redhat.com
	// Combines OCP-77091 and OCP 77086 tests for allowlist epic NE:1100
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-77091-NetworkEdge Route does not enable allowlist with than 61 CIDRs and if invalid IP annotation is given", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPod             = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		exutil.By("1.0: Create Pod and Services")
		ns := oc.Namespace()
		routerpod := getOneRouterPodNameByIC(oc, "default")
		srvPodList := createResourceFromWebServer(oc, ns, testPod, "web-server-deploy")
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("2.0: Create an edge route")
		edgeRoute := "route-edge"
		edgeHost := edgeRoute + "-" + ns + ".apps." + getBaseDomain(oc)
		createRoute(oc, ns, "edge", edgeRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")

		exutil.By("3.0: annotate route with an invalid IP and try to access route")
		setAnnotation(oc, ns, "route/"+edgeRoute, `haproxy.router.openshift.io/ip_allowlist=192.abc.123.0`)
		findAnnotation := getAnnotation(oc, ns, "route", edgeRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"192.abc.123.0`))

		waitForOutsideCurlContains("https://"+edgeHost, "-k", `Hello-OpenShift `+srvPodList[0]+` http-8080`)

		exutil.By("4.0: Check HaProxy to confirm the allowlist annotation does not occur")
		ensureHaproxyBlockConfigNotContains(oc, routerpod, ns+":"+edgeRoute, []string{"acl allowlist src", "tcp-request content reject if !allowlist"})

		//OCP-77091 route does not enable whitelist with more than 61 CIDRs
		exutil.By("5.0: Create an unsecure route")
		unsecureRoute := "route-unsecure"
		createRoute(oc, ns, "http", unsecureRoute, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-unsecure", "default")

		exutil.By("6.0: Annotate unsecure route with 61 CIDRs")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=192.168.0.0/24 192.168.1.0/24 192.168.2.0/24 192.168.3.0/24 192.168.4.0/24 192.168.5.0/24 192.168.6.0/24 192.168.7.0/24 192.168.8.0/24 192.168.9.0/24 192.168.10.0/24 192.168.11.0/24 192.168.12.0/24 192.168.13.0/24 192.168.14.0/24 192.168.15.0/24 192.168.16.0/24 192.168.17.0/24 192.168.18.0/24 192.168.19.0/24 192.168.20.0/24 192.168.21.0/24 192.168.22.0/24 192.168.23.0/24 192.168.24.0/24 192.168.25.0/24 192.168.26.0/24 192.168.27.0/24 192.168.28.0/24 192.168.29.0/24 192.168.30.0/24 192.168.31.0/24 192.168.32.0/24 192.168.33.0/24 192.168.34.0/24 192.168.35.0/24 192.168.36.0/24 192.168.37.0/24 192.168.38.0/24 192.168.39.0/24 192.168.40.0/24 192.168.41.0/24 192.168.42.0/24 192.168.43.0/24 192.168.44.0/24 192.168.45.0/24 192.168.46.0/24 192.168.47.0/24 192.168.48.0/24 192.168.49.0/24 192.168.50.0/24 192.168.51.0/24 192.168.52.0/24 192.168.53.0/24 192.168.54.0/24 192.168.55.0/24 192.168.56.0/24 192.168.57.0/24 192.168.58.0/24 192.168.59.0/24 192.168.60.0/24`)
		findAnnotation = getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"`))

		exutil.By("7.0: Check HaProxy if the allowlist annotation exists and tcp request exist")
		ensureHaproxyBlockConfigContains(oc, routerpod, ns+":"+unsecureRoute, []string{"acl allowlist src 192.168.0.0/24", "tcp-request content reject if !allowlist"})

		exutil.By("8.0: add allowlist annotation with more than 61 CIDRs")
		setAnnotation(oc, ns, "route/"+unsecureRoute, `haproxy.router.openshift.io/ip_allowlist=192.168.0.0/24 192.168.1.0/24 192.168.2.0/24 192.168.3.0/24 192.168.4.0/24 192.168.5.0/24 192.168.6.0/24 192.168.7.0/24 192.168.8.0/24 192.168.9.0/24 192.168.10.0/24 192.168.11.0/24 192.168.12.0/24 192.168.13.0/24 192.168.14.0/24 192.168.15.0/24 192.168.16.0/24 192.168.17.0/24 192.168.18.0/24 192.168.19.0/24 192.168.20.0/24 192.168.21.0/24 192.168.22.0/24 192.168.23.0/24 192.168.24.0/24 192.168.25.0/24 192.168.26.0/24 192.168.27.0/24 192.168.28.0/24 192.168.29.0/24 192.168.30.0/24 192.168.31.0/24 192.168.32.0/24 192.168.33.0/24 192.168.34.0/24 192.168.35.0/24 192.168.36.0/24 192.168.37.0/24 192.168.38.0/24 192.168.39.0/24 192.168.40.0/24 192.168.41.0/24 192.168.42.0/24 192.168.43.0/24 192.168.44.0/24 192.168.45.0/24 192.168.46.0/24 192.168.47.0/24 192.168.48.0/24 192.168.49.0/24 192.168.50.0/24 192.168.51.0/24 192.168.52.0/24 192.168.53.0/24 192.168.54.0/24 192.168.55.0/24 192.168.56.0/24 192.168.57.0/24 192.168.58.0/24 192.168.59.0/24 192.168.60.0/24 192.168.61.0/24`)
		findAnnotation = getAnnotation(oc, ns, "route", unsecureRoute)
		o.Expect(findAnnotation).To(o.ContainSubstring(`haproxy.router.openshift.io/ip_allowlist":"`))

		exutil.By("9.0: Check HaProxy if the allowlist annotation exists and tcp request exist")
		ensureHaproxyBlockConfigContains(oc, routerpod, "backend be_http:"+ns+":"+unsecureRoute, []string{`acl allowlist src -f /var/lib/haproxy/router/allowlists/` + ns + ":" + unsecureRoute + ".txt", "tcp-request content reject if !allowlist"})
		ensureHaproxyBlockConfigNotContains(oc, routerpod, "backend be_http:"+ns+":"+unsecureRoute, []string{"acl allowlist src 192.168.0.0/24"})
	})

	// OCPBUGS-47773
	// Added the Serial flag in case other cases may cause the ingress co abnormal
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-85274-Route spec path that have specail characters should not cause HaProxy error and ingress degraded [Serial]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			ingressJsonPath     = `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		)

		// skip the test if ingress co is abnormal
		status := getByJsonPath(oc, "default", "co/ingress", ingressJsonPath)
		if status != "TrueFalseFalse" {
			g.Skip("ingress co is abnormal")
		}

		exutil.By("1.0: Create a single pod and the service")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		exutil.By("2.0: Create an unsecure route")
		createRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, unSecSvcName, "default")

		exutil.By(`3.0: Try to patch the route spec.path with "/route-admission-test#2", which includes the # character`)
		specPath := `{"spec": {"path": "/route-admission-test#2"}}`
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unSecSvcName, "-p", specPath, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`cannot contain # or spaces`))

		exutil.By(`4.0: Try to patch the route spec.path with "/route-admission-test 22", which includes the space character`)
		specPath = `{"spec": {"path": "/route-admission-test 22"}}`
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unSecSvcName, "-p", specPath, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`cannot contain # or spaces`))

		exutil.By("5.0: Check the ingress co, make sure it is normal")
		status = getByJsonPath(oc, "default", "co/ingress", ingressJsonPath)
		o.Expect(status).To(o.ContainSubstring("TrueFalseFalse"))
	})
})
