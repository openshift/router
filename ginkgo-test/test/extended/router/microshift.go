package router

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-network-edge] Network_Edge", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLIWithoutNamespace("router-microshift")

	g.It("Author:mjoseph-MicroShiftOnly-High-60136-reencrypt route using Ingress resource for Microshift with destination CA certificate", func() {
		var (
			e2eTestNamespace    = "e2e-ne-ocp60136-" + getRandomString()
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			ingressFile         = filepath.Join(buildPruningBaseDir, "microshift-ingress-destca.yaml")
		)

		compat_otp.By("create a namespace for the scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)

		compat_otp.By("create a web-server-deploy pod and its services")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, testPodSvc)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		podName := getPodListByLabel(oc, e2eTestNamespace, "name=web-server-deploy")
		ingressPod := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("create ingress using the file and get the route details")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, ingressFile)
		createResourceFromFile(oc, e2eTestNamespace, ingressFile)
		getIngress(oc, e2eTestNamespace)
		getRoutes(oc, e2eTestNamespace)
		routeNames := getResourceName(oc, e2eTestNamespace, "route")

		compat_otp.By("check whether route details are present")
		waitForOutputEquals(oc, e2eTestNamespace, "route/"+routeNames[0], "{.status.ingress[0].conditions[0].type}", "Admitted")
		waitForOutputEquals(oc, e2eTestNamespace, "route/"+routeNames[0], "{.status.ingress[0].host}", "service-secure-test.example.com")
		waitForOutputEquals(oc, e2eTestNamespace, "route/"+routeNames[0], "{.spec.tls.termination}", "reencrypt")

		compat_otp.By("check the reachability of the host in test pod")
		routerPodIP := getPodv4Address(oc, ingressPod, "openshift-ingress")
		curlCmd := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "https://service-secure-test.example.com:443", "-k", "-I", "--resolve", "service-secure-test.example.com:443:" + routerPodIP, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		compat_otp.By("check the router pod and ensure the routes are loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ingress-ms-reen")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_secure:" + e2eTestNamespace + ":" + routeNames[0]))
	})

	g.It("Author:mjoseph-MicroShiftOnly-Critical-60266-creation of edge and passthrough routes for Microshift", func() {
		var (
			e2eTestNamespace    = "e2e-ne-ocp60266-" + getRandomString()
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			edgeRouteHost       = "route-edge-" + e2eTestNamespace + ".apps.example.com"
			passRouteHost       = "route-pass-" + e2eTestNamespace + ".apps.example.com"
		)

		compat_otp.By("create a namespace for the scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)

		compat_otp.By("create a web-server-deploy pod and its services")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, testPodSvc)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		podName := getPodListByLabel(oc, e2eTestNamespace, "name=web-server-deploy")
		ingressPod := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("create a passthrough route")
		createRoute(oc, e2eTestNamespace, "passthrough", "ms-pass", "service-secure", []string{"--hostname=" + passRouteHost})
		getRoutes(oc, e2eTestNamespace)

		compat_otp.By("check whether passthrough route details are present")
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-pass", "{.spec.tls.termination}", "passthrough")
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-pass", "{.status.ingress[0].host}", passRouteHost)
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-pass", "{.status.ingress[0].conditions[0].type}", "Admitted")

		compat_otp.By("check the reachability of the host in test pod for passthrough route")
		routerPodIP := getPodv4Address(oc, ingressPod, "openshift-ingress")
		passRoute := passRouteHost + ":443:" + routerPodIP
		curlCmd := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "https://" + passRouteHost + ":443", "-k", "-I", "--resolve", passRoute, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		compat_otp.By("check the router pod and ensure the passthrough route is loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ms-pass")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_tcp:" + e2eTestNamespace + ":ms-pass"))

		compat_otp.By("create a edge route")
		createRoute(oc, e2eTestNamespace, "edge", "ms-edge", "service-unsecure", []string{"--hostname=" + edgeRouteHost})
		getRoutes(oc, e2eTestNamespace)

		compat_otp.By("check whether edge route details are present")
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-edge", "{.spec.tls.termination}", "edge")
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-edge", "{.status.ingress[0].host}", edgeRouteHost)
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-edge", "{.status.ingress[0].conditions[0].type}", "Admitted")

		compat_otp.By("check the reachability of the host in test pod for edge route")
		edgeRoute := edgeRouteHost + ":443:" + routerPodIP
		curlCmd1 := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "https://" + edgeRouteHost + ":443", "-k", "-I", "--resolve", edgeRoute, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd1, "200", 30, 1)

		compat_otp.By("check the router pod and ensure the edge route is loaded in haproxy.config")
		searchOutput1 := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ms-edge")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_edge_http:" + e2eTestNamespace + ":ms-edge"))
	})

	g.It("Author:mjoseph-MicroShiftOnly-Critical-60283-creation of http and re-encrypt routes for Microshift", func() {
		var (
			e2eTestNamespace    = "e2e-ne-ocp60283-" + getRandomString()
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			httpRouteHost       = "route-http-" + e2eTestNamespace + ".apps.example.com"
			reenRouteHost       = "route-reen-" + e2eTestNamespace + ".apps.example.com"
		)

		compat_otp.By("create a namespace for the scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)

		compat_otp.By("create a signed web-server-deploy pod and its services")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, testPodSvc)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		podName := getPodListByLabel(oc, e2eTestNamespace, "name=web-server-deploy")
		ingressPod := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("create a http route")
		_, err := oc.WithoutNamespace().Run("expose").Args("-n", e2eTestNamespace, "--name=ms-http", "service", "service-unsecure", "--hostname="+httpRouteHost).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		getRoutes(oc, e2eTestNamespace)

		compat_otp.By("check whether http route details are present")
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-http", "{.spec.port.targetPort}", "http")
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-http", "{.status.ingress[0].host}", httpRouteHost)
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-http", "{.status.ingress[0].conditions[0].type}", "Admitted")

		compat_otp.By("check the reachability of the host in test pod for http route")
		routerPodIP := getPodv4Address(oc, ingressPod, "openshift-ingress")
		httpRoute := httpRouteHost + ":80:" + routerPodIP
		curlCmd := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "http://" + httpRouteHost + ":80", "-k", "-I", "--resolve", httpRoute, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		compat_otp.By("check the router pod and ensure the http route is loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ms-http")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_http:" + e2eTestNamespace + ":ms-http"))

		compat_otp.By("create a reen route")
		createRoute(oc, e2eTestNamespace, "reencrypt", "ms-reen", "service-secure", []string{"--hostname=" + reenRouteHost})
		getRoutes(oc, e2eTestNamespace)

		compat_otp.By("check whether reen route details are present")
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-reen", "{.spec.tls.termination}", "reencrypt")
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-reen", "{.status.ingress[0].host}", reenRouteHost)
		waitForOutputEquals(oc, e2eTestNamespace, "route/ms-reen", "{.status.ingress[0].conditions[0].type}", "Admitted")

		compat_otp.By("check the reachability of the host in test pod reen route")
		reenRoute := reenRouteHost + ":443:" + routerPodIP
		curlCmd1 := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "https://" + reenRouteHost + ":443", "-k", "-I", "--resolve", reenRoute, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd1, "200", 30, 1)

		compat_otp.By("check the router pod and ensure the reen route is loaded in haproxy.config")
		searchOutput1 := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ms-reen")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_secure:" + e2eTestNamespace + ":ms-reen"))
	})

	g.It("Author:mjoseph-MicroShiftOnly-Critical-60149-http route using Ingress resource for Microshift", func() {
		var (
			e2eTestNamespace    = "e2e-ne-ocp60149-" + getRandomString()
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			ingressFile         = filepath.Join(buildPruningBaseDir, "microshift-ingress-http.yaml")
			httpRoute           = "service-unsecure-test.example.com"
		)

		compat_otp.By("create a namespace for the scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)

		compat_otp.By("create a web-server-deploy pod and its services")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, testPodSvc)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		podName := getPodListByLabel(oc, e2eTestNamespace, "name=web-server-deploy")
		ingressPod := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("create ingress using the file and get the route details")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, ingressFile)
		createResourceFromFile(oc, e2eTestNamespace, ingressFile)
		getIngress(oc, e2eTestNamespace)
		getRoutes(oc, e2eTestNamespace)
		routeNames := getResourceName(oc, e2eTestNamespace, "route")

		compat_otp.By("check whether http route details are present")
		waitForOutputEquals(oc, e2eTestNamespace, "route/"+routeNames[0], "{.spec.port.targetPort}", "http")
		waitForOutputEquals(oc, e2eTestNamespace, "route/"+routeNames[0], "{.status.ingress[0].host}", httpRoute)
		waitForOutputEquals(oc, e2eTestNamespace, "route/"+routeNames[0], "{.status.ingress[0].conditions[0].type}", "Admitted")

		compat_otp.By("check the reachability of the host in test pod for http route")
		routerPodIP := getPodv4Address(oc, ingressPod, "openshift-ingress")
		curlCmd := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "http://service-unsecure-test.example.com:80", "-k", "-I", "--resolve", "service-unsecure-test.example.com:80:" + routerPodIP, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		compat_otp.By("check the router pod and ensure the http route is loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ingress-on-microshift")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_http:" + e2eTestNamespace + ":" + routeNames[0]))
	})

	g.It("Author:shudili-MicroShiftOnly-High-72802-make router namespace ownership check configurable for the default microshift configuration", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			e2eTestNamespace1   = "e2e-ne-ocp72802-" + getRandomString()
			e2eTestNamespace2   = "e2e-ne-ocp72802-" + getRandomString()
		)

		compat_otp.By("1. check the Env ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK of deployment/default-router, which should be true for the default configuration")
		routerPodName := getOneRouterPodNameByIC(oc, "default")
		defaultVal := readRouterPodEnv(oc, routerPodName, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(defaultVal).To(o.ContainSubstring("true"))

		compat_otp.By("2. prepare two namespaces for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace1)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace1)
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		path1 := "/path"
		path2 := "/test"
		httpRoutehost := unSecSvcName + "-" + "ocp72802." + "apps.example.com"
		edgeRoute := "route-edge" + "-" + "ocp72802." + "apps.example.com"
		reenRoute := "route-reen" + "-" + "ocp72802." + "apps.example.com"

		compat_otp.By("3. create a client pod, a server pod and two services in one ns")
		createResourceFromFile(oc, e2eTestNamespace1, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace1, clientPodLabel)

		createResourceFromFile(oc, e2eTestNamespace1, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace1, "name="+srvrcInfo)

		compat_otp.By("4. create a server pod and two services in the other ns")
		createResourceFromFile(oc, e2eTestNamespace2, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, clientPodLabel)

		createResourceFromFile(oc, e2eTestNamespace2, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		compat_otp.By("5. expose an insecure/edge/REEN type routes with path " + path1 + " in the first ns")
		err := oc.Run("expose").Args("service", unSecSvcName, "--hostname="+httpRoutehost, "--path="+path1, "-n", e2eTestNamespace1).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, e2eTestNamespace1, "route", "{.items[0].metadata.name}", unSecSvcName)

		_, err = oc.WithoutNamespace().Run("create").Args("route", "edge", "route-edge", "--service="+unSecSvcName, "--hostname="+edgeRoute, "--path="+path1, "-n", e2eTestNamespace1).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		output, err := oc.WithoutNamespace().Run("get").Args("route", "-n", e2eTestNamespace1).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		_, err = oc.WithoutNamespace().Run("create").Args("route", "reencrypt", "route-reen", "--service="+secSvcName, "--hostname="+reenRoute, "--path="+path1, "-n", e2eTestNamespace1).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		output, err = oc.WithoutNamespace().Run("get").Args("route", "-n", e2eTestNamespace1).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-reen"))

		compat_otp.By("6. expose an insecure/edge/REEN type routes with path " + path2 + " in the second ns")
		err = oc.Run("expose").Args("service", unSecSvcName, "--hostname="+httpRoutehost, "--path="+path2, "-n", e2eTestNamespace2).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, e2eTestNamespace2, "route", "{.items[0].metadata.name}", unSecSvcName)

		_, err = oc.WithoutNamespace().Run("create").Args("route", "edge", "route-edge", "--service="+unSecSvcName, "--hostname="+edgeRoute, "--path="+path2, "-n", e2eTestNamespace2).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		output, err = oc.WithoutNamespace().Run("get").Args("route", "-n", e2eTestNamespace2).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		_, err = oc.WithoutNamespace().Run("create").Args("route", "reencrypt", "route-reen", "--service="+secSvcName, "--hostname="+reenRoute, "--path="+path2, "-n", e2eTestNamespace2).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		output, err = oc.WithoutNamespace().Run("get").Args("route", "-n", e2eTestNamespace2).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-reen"))

		compat_otp.By("7.1. check the http route in the first ns should be adimitted")
		jpath := "{.status.ingress[0].conditions[0].status}"
		adtInfo := getByJsonPath(oc, e2eTestNamespace1, "route/"+unSecSvcName, jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		compat_otp.By("7.2. check the edge route in the first ns should be adimitted")
		adtInfo = getByJsonPath(oc, e2eTestNamespace1, "route/route-edge", jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		compat_otp.By("7.3. check the REEN route in the first ns should be adimitted")
		adtInfo = getByJsonPath(oc, e2eTestNamespace1, "route/route-reen", jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		compat_otp.By("8.1. check the http route in the second ns with the same hostname but with different path should be adimitted too")
		adtInfo = getByJsonPath(oc, e2eTestNamespace2, "route/"+unSecSvcName, jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		compat_otp.By("8.2. check the edge route in the second ns with the same hostname but with different path should be adimitted too")
		adtInfo = getByJsonPath(oc, e2eTestNamespace2, "route/route-edge", jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		compat_otp.By("8.3. check the REEN route in the second ns with the same hostname but with different path should be adimitted too")
		adtInfo = getByJsonPath(oc, e2eTestNamespace2, "route/route-reen", jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		compat_otp.By("9. curl the first HTTP route and check the result")
		srvPodName := getPodListByLabel(oc, e2eTestNamespace1, "name=web-server-deploy")
		routerPodIP := getPodv4Address(oc, routerPodName, "openshift-ingress")
		toDst := httpRoutehost + ":80:" + routerPodIP
		cmdOnPod := []string{"-n", e2eTestNamespace1, clientPodName, "--", "curl", "http://" + httpRoutehost + "/path/index.html", "--resolve", toDst, "--connect-timeout", "10"}
		result, _ := repeatCmdOnClient(oc, cmdOnPod, "http-8080", 30, 1)
		o.Expect(result).To(o.ContainSubstring("http-8080"))
		output, err = oc.Run("exec").Args(cmdOnPod...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("ocp-test " + srvPodName[0] + " http-8080"))

		compat_otp.By("10. curl the second HTTP route and check the result")
		srvPodName = getPodListByLabel(oc, e2eTestNamespace2, "name=web-server-deploy")
		cmdOnPod = []string{"-n", e2eTestNamespace1, clientPodName, "--", "curl", "http://" + httpRoutehost + "/test/index.html", "--resolve", toDst, "--connect-timeout", "10"}
		result, _ = repeatCmdOnClient(oc, cmdOnPod, "http-8080", 60, 1)
		o.Expect(result).To(o.ContainSubstring("Hello-OpenShift-Path-Test " + srvPodName[0] + " http-8080"))
	})

	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-Longduration-Medium-73621-Disable/Enable namespace ownership support for router [Disruptive]", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			caseID              = "73621"
			e2eTestNamespace1   = "e2e-ne-" + caseID + "-" + getRandomString()
			e2eTestNamespace2   = "e2e-ne-" + caseID + "-" + getRandomString()
			baseDomain          = "apps.example.com"
			httpRouteHost       = unSecSvcName + "-" + caseID + "." + baseDomain
			path1               = "/path"
			path2               = "/path/second"
		)

		compat_otp.By("1. Prepare two namespaces for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace1)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace1)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace1)
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)

		compat_otp.By("2: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace1, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace1, caseID, nodeName)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By(`3. Debug node to disable namespace ownership support by setting namespaceOwnership to Strict in the config.yaml file"`)
		ingressConfig := fmt.Sprintf(`
ingress:
    routeAdmissionPolicy:
        namespaceOwnership: Strict`)

		appendIngressToConfigYaml(oc, e2eTestNamespace1, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("4. Check the Env ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK of deployment/default-router, which should be false")
		routerPodName := getOneNewRouterPodFromRollingUpdate(oc, "default")
		ownershipVal := readRouterPodEnv(oc, routerPodName, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(ownershipVal).To(o.ContainSubstring("false"))

		compat_otp.By("5. Create a server pod and the services in one ns")
		createResourceFromFile(oc, e2eTestNamespace1, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace1, "name="+srvrcInfo)

		compat_otp.By("6. Create a route with path " + path1 + " in the first ns, which should be admitted")
		extraParas := []string{"--hostname=" + httpRouteHost, "--path=" + path1}
		jpath := "{.status.ingress[0].conditions[0].status}"
		createRoute(oc, e2eTestNamespace1, "http", "route-http", unSecSvcName, extraParas)
		waitForOutputEquals(oc, e2eTestNamespace1, "route", "{.items[0].metadata.name}", "route-http")
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, "route-http", "default")

		compat_otp.By("7. Create a server pod and the services in the other ns")
		createResourceFromFile(oc, e2eTestNamespace2, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		compat_otp.By("8. Create a route with path " + path2 + " in the second ns, which should NOT be admitted")
		extraParas = []string{"--hostname=" + httpRouteHost, "--path=" + path2}
		createRoute(oc, e2eTestNamespace2, "http", "route-http", unSecSvcName, extraParas)
		waitForOutputEquals(oc, e2eTestNamespace2, "route", "{.items[0].metadata.name}", "route-http")
		ensureRouteIsNotAdmittedByIngressController(oc, e2eTestNamespace2, "route-http", "default")

		compat_otp.By("9. Check the two routes with same hostname but with different path for the second time, the first one is adimitted, while the second one isn't")
		adtInfo := getByJsonPath(oc, e2eTestNamespace1, "route/route-http", jpath)
		o.Expect(adtInfo).To(o.ContainSubstring("True"))
		adtInfo = getByJsonPath(oc, e2eTestNamespace2, "route/route-http", jpath)
		o.Expect(adtInfo).To(o.ContainSubstring("False"))

		compat_otp.By("10. Confirm the second route is shown as HostAlreadyClaimed")
		jpath2 := `{.status.ingress[?(@.routerName=="default")].conditions[*].reason}`
		searchOutput := getByJsonPath(oc, e2eTestNamespace2, "route/route-http", jpath2)
		o.Expect(searchOutput).To(o.ContainSubstring("HostAlreadyClaimed"))

		compat_otp.By("11. Debug node to enable namespace ownership support by setting namespaceOwnership to InterNamespaceAllowed in the config.yaml file")
		ingressConfig = fmt.Sprintf(`
ingress:
    routeAdmissionPolicy:
        namespaceOwnership: InterNamespaceAllowed`)

		appendIngressToConfigYaml(oc, e2eTestNamespace1, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		compat_otp.By("12. Check the two route with same hostname but with different path, both of them should be adimitted")
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace1, "route-http", "default")
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace2, "route-http", "default")

		compat_otp.By("13. Confirm no route is shown as HostAlreadyClaimed")
		searchOutput1 := getByJsonPath(oc, e2eTestNamespace1, "route/route-http", jpath2)
		searchOutput2 := getByJsonPath(oc, e2eTestNamespace2, "route/route-http", jpath2)
		o.Expect(strings.Count(searchOutput1+searchOutput2, "HostAlreadyClaimed")).To(o.Equal(0))
	})

	g.It("Author:shudili-MicroShiftOnly-High-73152-Expose router as load balancer service type", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "service-unsecure"
			secsvcName          = "service-secure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			e2eTestNamespace    = "e2e-ne-ocp73152-" + getRandomString()
		)

		compat_otp.By("Check the router-default service is a load balancer and has a load balancer ip")
		svcType := getByJsonPath(oc, "openshift-ingress", "service/router-default", "{.spec.type}")
		o.Expect(svcType).To(o.ContainSubstring("LoadBalancer"))
		lbIPs := getByJsonPath(oc, "openshift-ingress", "service/router-default", "{.status.loadBalancer.ingress[0].ip}")
		o.Expect(len(lbIPs) > 4).To(o.BeTrue())

		compat_otp.By("Deploy a project with a client pod, a backend pod and its services resources")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")

		compat_otp.By("Create a HTTP/Edge/Passthrough/REEN route")
		httpRouteHost := unsecsvcName + "-" + "ocp73152." + "apps.example.com"
		edgeRouteHost := "route-edge" + "-" + "ocp73152." + "apps.example.com"
		passThRouteHost := "route-passth" + "-" + "ocp73152." + "apps.example.com"
		reenRouteHost := "route-reen" + "-" + "ocp73152." + "apps.example.com"
		lbIP := strings.Split(lbIPs, " ")[0]
		httpRouteDst := httpRouteHost + ":80:" + lbIP
		edgeRouteDst := edgeRouteHost + ":443:" + lbIP
		passThRouteDst := passThRouteHost + ":443:" + lbIP
		reenRouteDst := reenRouteHost + ":443:" + lbIP
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecsvcName, []string{"--hostname=" + httpRouteHost})
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRouteHost})
		createRoute(oc, e2eTestNamespace, "passthrough", "route-passth", secsvcName, []string{"--hostname=" + passThRouteHost})
		createRoute(oc, e2eTestNamespace, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRouteHost})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-reen", "default")
		output := getByJsonPath(oc, e2eTestNamespace, "route", "{.items[*].metadata.name}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring("route-http"),
			o.ContainSubstring("route-edge"),
			o.ContainSubstring("route-passth"),
			o.ContainSubstring("route-reen")))

		compat_otp.By("Curl the HTTP route")
		routeReq := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + httpRouteHost, "-I", "--resolve", httpRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		compat_otp.By("Curl the Edge route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRouteHost, "-k", "-I", "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		compat_otp.By("Curl the Passthrough route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + passThRouteHost, "-k", "-I", "--resolve", passThRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		compat_otp.By("Curl the REEN route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + reenRouteHost, "-k", "-I", "--resolve", reenRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)
	})

	g.It("Author:shudili-MicroShiftOnly-High-73202-Add configurable listening IP addresses and listening ports", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "service-unsecure"
			secsvcName          = "service-secure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			findIpCmd           = "ip address | grep \"inet \""
			hostIPList          []string
			e2eTestNamespace    = "e2e-ne-ocp73202-" + getRandomString()
		)

		compat_otp.By("create a namespace for testing, then debug node and get the valid host ips")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		podCIDR := getByJsonPath(oc, "default", "nodes/"+nodeName, "{.spec.podCIDR}")
		if strings.Contains(podCIDR, `:`) {
			findIpCmd = "ip address | grep \"inet6 \" | grep global"
			hostAddresses, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", findIpCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			hostIPList = getValidIPv6Addresses(hostAddresses)
			e2e.Logf("hostIPList is: /n%v", hostIPList)
		} else {
			hostAddresses, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", findIpCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			_, hostIPList = getValidInterfacesAndIPs(hostAddresses)
			e2e.Logf("hostIPList is: /n%v", hostIPList)
		}

		compat_otp.By("check the default load balancer ips of the router-default service, which should be all node's valid host ips")
		lbIPs := getByJsonPath(oc, "openshift-ingress", "service/router-default", "{.status.loadBalancer.ingress[*].ip}")
		lbIPs = getSortedString(lbIPs)
		hostIPs := getSortedString(hostIPList)
		o.Expect(lbIPs).To(o.Equal(hostIPs))

		compat_otp.By("check the default load balancer ports of the router-default service, which should be 80 for the unsecure http port and 443 for the seccure https port")
		httpPort := getByJsonPath(oc, "openshift-ingress", "service/router-default", `{.spec.ports[?(@.name=="http")].port}`)
		o.Expect(httpPort).To(o.Equal("80"))
		httpsPort := getByJsonPath(oc, "openshift-ingress", "service/router-default", `{.spec.ports[?(@.name=="https")].port}`)
		o.Expect(httpsPort).To(o.Equal("443"))

		compat_otp.By("Deploy a backend pod and its services resources in the created ns")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")

		compat_otp.By("Create a HTTP/Edge/Passthrough/REEN route")
		httpRouteHost := unsecsvcName + "-" + "ocp73202." + "apps.example.com"
		edgeRouteHost := "route-edge" + "-" + "ocp73202." + "apps.example.com"
		passThRouteHost := "route-passth" + "-" + "ocp73202." + "apps.example.com"
		reenRouteHost := "route-reen" + "-" + "ocp73202." + "apps.example.com"
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecsvcName, []string{"--hostname=" + httpRouteHost})
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRouteHost})
		createRoute(oc, e2eTestNamespace, "passthrough", "route-passth", secsvcName, []string{"--hostname=" + passThRouteHost})
		createRoute(oc, e2eTestNamespace, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRouteHost})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-reen", "default")
		output := getByJsonPath(oc, e2eTestNamespace, "route", "{.items[*].metadata.name}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring("route-http"),
			o.ContainSubstring("route-edge"),
			o.ContainSubstring("route-passth"),
			o.ContainSubstring("route-reen")))

		compat_otp.By("Curl the routes with destination to each load balancer ip")
		for _, lbIP := range strings.Split(lbIPs, " ") {
			// config firewall for ipv6 load balancer
			configFwForLB(oc, e2eTestNamespace, nodeName, lbIP)
			httpRouteDst := httpRouteHost + ":80:" + lbIP
			edgeRouteDst := edgeRouteHost + ":443:" + lbIP
			passThRouteDst := passThRouteHost + ":443:" + lbIP
			reenRouteDst := reenRouteHost + ":443:" + lbIP

			compat_otp.By("Curl the http route with destination " + lbIP)
			routeReq := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + httpRouteHost, "-I", "--resolve", httpRouteDst, "--connect-timeout", "10"}
			repeatCmdOnClient(oc, routeReq, "200", 150, 1)

			compat_otp.By("Curl the Edge route with destination " + lbIP)
			routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRouteHost, "-k", "-I", "--resolve", edgeRouteDst, "--connect-timeout", "10"}
			repeatCmdOnClient(oc, routeReq, "200", 60, 1)

			compat_otp.By("Curl the Pass-through route with destination " + lbIP)
			routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + passThRouteHost, "-k", "-I", "--resolve", passThRouteDst, "--connect-timeout", "10"}
			repeatCmdOnClient(oc, routeReq, "200", 60, 1)

			compat_otp.By("Curl the REEN route with destination " + lbIP)
			routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + reenRouteHost, "-k", "-I", "--resolve", reenRouteDst, "--connect-timeout", "10"}
			repeatCmdOnClient(oc, routeReq, "200", 60, 1)
		}
	})

	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-Longduration-High-73203-configuring listening IP addresses and listening Ports [Disruptive]", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "service-unsecure"
			secsvcName          = "service-secure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			specifiedAddress    string
			randHostIP          string
			caseID              = "73203"
			e2eTestNamespace    = "e2e-ne-" + caseID + "-" + getRandomString()
		)

		compat_otp.By(`1. Create a namespace for testing, then debug node and get all valid host interfaces and invalid host ips`)
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		podCIDR := getByJsonPath(oc, "default", "nodes/"+nodeName, "{.spec.podCIDR}")
		if !strings.Contains(podCIDR, `:`) {
			hostAddresses, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", "ip address | grep \"inet \"").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			intfaceList, hostIPList := getValidInterfacesAndIPs(hostAddresses)
			seed := rand.New(rand.NewSource(time.Now().UnixNano()))
			index := seed.Intn(len(intfaceList))
			specifiedAddress = intfaceList[index]
			randHostIP = hostIPList[index]
		} else {
			hostAddresses, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", "ip address | grep \"inet6 \" | grep global").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			hostIPList := getValidIPv6Addresses(hostAddresses)
			seed := rand.New(rand.NewSource(time.Now().UnixNano()))
			index := seed.Intn(len(hostIPList))
			randHostIP = hostIPList[index]
			specifiedAddress = randHostIP
		}

		compat_otp.By("2. Debug node to backup the config.yaml, and restore it before the test finishes running")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)

		compat_otp.By(`3. Debug node and configure ingress with the desired listening IP addresses and listening Ports`)
		ingressConfig := fmt.Sprintf(`
ingress:
    listenAddress:
        - %s
    ports:
        http: 10080
        https: 10443`, specifiedAddress)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)

		compat_otp.By("4. Wait the check router-default service is updated and its load balancer ip is as same as configured in default.yaml")
		regExp := "^" + randHostIP + "$"
		searchOutput := waitForOutputMatchRegexp(oc, "openshift-ingress", "service/router-default", "{.status.loadBalancer.ingress[*].ip}", regExp, 240*time.Second)
		o.Expect(searchOutput).To(o.Equal(randHostIP))

		compat_otp.By("5. Check service router-default's http port is changed to 10080 and its https port is changed to 10443")
		jpath := `{.spec.ports[?(@.name=="http")].port}`
		httpPort := getByJsonPath(oc, "openshift-ingress", "svc/router-default", jpath)
		o.Expect(httpPort).To(o.Equal("10080"))
		jpath = `{.spec.ports[?(@.name=="https")].port}`
		httpsPort := getByJsonPath(oc, "openshift-ingress", "svc/router-default", jpath)
		o.Expect(httpsPort).To(o.Equal("10443"))

		compat_otp.By("6. Deploy a client pod, a backend pod and its services resources")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")

		compat_otp.By("7. Create a HTTP/Edge/Passthrough/REEN route")
		httpRouteHost := unsecsvcName + "-" + "ocp73203." + "apps.example.com"
		edgeRouteHost := "route-edge" + "-" + "ocp73203." + "apps.example.com"
		passThRouteHost := "route-passth" + "-" + "ocp73203." + "apps.example.com"
		reenRouteHost := "route-reen" + "-" + "ocp73203." + "apps.example.com"
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecsvcName, []string{"--hostname=" + httpRouteHost})
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRouteHost})
		createRoute(oc, e2eTestNamespace, "passthrough", "route-passth", secsvcName, []string{"--hostname=" + passThRouteHost})
		createRoute(oc, e2eTestNamespace, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRouteHost})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-reen", "default")
		output := getByJsonPath(oc, e2eTestNamespace, "route", "{.items[*].metadata.name}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring("route-http"),
			o.ContainSubstring("route-edge"),
			o.ContainSubstring("route-passth"),
			o.ContainSubstring("route-reen")))

		compat_otp.By("8. Curl the routes with destination to the the custom load balancer ip and http/https ports")
		httpRouteDst := httpRouteHost + ":10080:" + randHostIP
		edgeRouteDst := edgeRouteHost + ":10443:" + randHostIP
		passThRouteDst := passThRouteHost + ":10443:" + randHostIP
		reenRouteDst := reenRouteHost + ":10443:" + randHostIP

		compat_otp.By("9. Curl the http route")
		// config firewall for ipv6 load balancer
		configFwForLB(oc, e2eTestNamespace, nodeName, randHostIP)

		routeReq := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + httpRouteHost + ":10080", "-I", "--resolve", httpRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 150, 1)

		compat_otp.By("10. Curl the Edge route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRouteHost + ":10443", "-k", "-I", "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		compat_otp.By("11. Curl the Passthrough route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + passThRouteHost + ":10443", "-k", "-I", "--resolve", passThRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		compat_otp.By("12. Curl the REEN route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + reenRouteHost + ":10443", "-k", "-I", "--resolve", reenRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)
	})

	g.It("MicroShiftOnly-Author:shudili-NonPreRelease-Longduration-High-73209-Add enable/disable option for default router [Disruptive]", func() {
		var (
			caseID           = "73209"
			e2eTestNamespace = "e2e-ne-" + caseID + "-" + getRandomString()
		)

		compat_otp.By("1. Create a namespace for testing, then debug node and get the valid host ips")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)

		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		podCIDR := getByJsonPath(oc, "default", "nodes/"+nodeName, "{.spec.podCIDR}")
		hostIPs := ""
		if !strings.Contains(podCIDR, `:`) {
			hostAddresses, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", "ip address | grep \"inet \"").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			_, hostIPList := getValidInterfacesAndIPs(hostAddresses)
			hostIPs = getSortedString(hostIPList)
		} else {
			hostAddresses, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", "ip address | grep \"inet6 \" | grep global").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			hostIPList := getValidIPv6Addresses(hostAddresses)
			hostIPs = getSortedString(hostIPList)
		}

		compat_otp.By("2. Debug node to backup the config.yaml, and restore it before the test finishes running")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)

		compat_otp.By("3. Debug node to disable the default router by setting ingress status to Removed")
		ingressConfig := fmt.Sprintf(`
ingress:
    status: Removed`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)

		compat_otp.By("4. Check the openshift-ingress namespace will be deleted")
		err := waitForResourceToDisappear(oc, "default", "ns/"+"openshift-ingress")
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "namespace openshift-ingress"))

		compat_otp.By(`5. Debug node to enable the default router by setting ingress status to Managed`)
		ingressConfig = fmt.Sprintf(`
ingress:
    status: Managed`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)

		compat_otp.By("6. Check router-default load balancer is enabled")
		waitForOutputEquals(oc, "openshift-ingress", "service/router-default", `{.spec.ports[?(@.name=="http")].port}`, "80", 240*time.Second)
		lbIPs := getByJsonPath(oc, "openshift-ingress", "service/router-default", "{.status.loadBalancer.ingress[*].ip}")
		lbIPs = getSortedString(lbIPs)
		o.Expect(lbIPs).To(o.Equal(hostIPs))
		httpsPort := getByJsonPath(oc, "openshift-ingress", "svc/router-default", `{.spec.ports[?(@.name=="https")].port}`)
		o.Expect(httpsPort).To(o.Equal("443"))
	})

	g.It("Author:shudili-MicroShiftOnly-High-77349-introduce ingress controller customization with microshift config.yaml [Disruptive]", func() {

		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			unsecsvcName        = "service-unsecure"
			caseID              = "77349"
			e2eTestNamespace    = "e2e-ne-" + caseID + "-" + getRandomString()
			httpRouteHost       = unsecsvcName + "-" + caseID + ".apps.example.com"

			// prepare the data for test,  for every slice, the first element is the env name, the second is the expected default env value in the deloyment, the third is the expected custom env value in the deloyment,
			// the fourth is the expected default haproxy configuration, the last is the expected custom haproxy configuration
			// https://issues.redhat.com/browse/OCPBUGS-45191 for routerBackendCheckInterval, the haproxy config should be "check inter 5000ms", marked it "skip for none" in the case
			// for routerSetForwardedHeaders, set expected haproxy config with "skip for none" for the haproxy hasn't such an configuration
			// for routerEnableCompression, routerCompressionMime and routerDontLogNull, set expected haproxy config with "skip for none" for the default values couldn't be seen in the haproxy.config
			routerBufSize                 = []string{`ROUTER_BUF_SIZE`, `32768`, `65536`, `tune.bufsize 32768`, `tune.bufsize 65536`}
			routerMaxRewriteSize          = []string{`ROUTER_MAX_REWRITE_SIZE`, `8192`, `16384`, `tune.maxrewrite 8192`, `tune.maxrewrite 16384`}
			routerBackendCheckInterval    = []string{`ROUTER_BACKEND_CHECK_INTERVAL`, `5s`, `10s`, `skip for none`, `skip for none`}
			routerDefaultClientTimeout    = []string{`ROUTER_DEFAULT_CLIENT_TIMEOUT`, `30s`, `1m`, `timeout client 30s`, `timeout client 1m`}
			routerClientFinTimeout        = []string{`ROUTER_CLIENT_FIN_TIMEOUT`, `1s`, `2s`, `timeout client-fin 1s`, `timeout client-fin 2s`}
			routerDefaultServerTimeout    = []string{`ROUTER_DEFAULT_SERVER_TIMEOUT`, `30s`, `1m`, `timeout server 30s`, `timeout server 1m`}
			routerDefaultServerFinTimeout = []string{`ROUTER_DEFAULT_SERVER_FIN_TIMEOUT`, `1s`, `2s`, `timeout server-fin 1s`, `timeout server-fin 2s`}
			routerDefaultTunnelTimeout    = []string{`ROUTER_DEFAULT_TUNNEL_TIMEOUT`, `1h`, `2h`, `timeout tunnel 1h`, `timeout tunnel 2h`}
			routerInspectDelay            = []string{`ROUTER_INSPECT_DELAY`, `5s`, `10s`, `tcp-request inspect-delay 5s`, `tcp-request inspect-delay 10s`}
			routerThreads                 = []string{`ROUTER_THREADS`, `4`, `8`, `nbthread 4`, `nbthread 8`}
			routerMaxConnections          = []string{`ROUTER_MAX_CONNECTIONS`, `50000`, `100000`, `maxconn 50000`, `maxconn 100000`}
			routerEnableCompression       = []string{`ROUTER_ENABLE_COMPRESSION`, `false`, `true`, `skip for none`, `compression algo`}
			routerCompressionMime         = []string{`ROUTER_COMPRESSION_MIME`, ``, `image`, `skip for none`, `compression type image`}
			routerDontLogNull             = []string{`ROUTER_DONT_LOG_NULL`, `false`, `true`, `skip for none`, `option dontlognull`}
			routerSetForwardedHeaders     = []string{`ROUTER_SET_FORWARDED_HEADERS`, `Append`, `Replace`, `skip for none`, `skip for none`}
			allParas                      = [][]string{routerBufSize, routerMaxRewriteSize, routerBackendCheckInterval, routerDefaultClientTimeout, routerClientFinTimeout, routerDefaultServerTimeout, routerDefaultServerFinTimeout, routerDefaultTunnelTimeout, routerInspectDelay, routerThreads, routerMaxConnections, routerEnableCompression, routerCompressionMime, routerDontLogNull, routerSetForwardedHeaders}
		)

		compat_otp.By("1.0 Deploy a project with a backend pod and its services resources, then create a route")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecsvcName, []string{"--hostname=" + httpRouteHost})

		compat_otp.By("2.0 Check the router-default deployment that all default ENVs of tested parameters are as expected")
		for _, routerEntry := range allParas {
			jsonPath := fmt.Sprintf(`{.spec.template.spec.containers[0].env[?(@.name=="%s")].value}`, routerEntry[0])
			envValue := getByJsonPath(oc, "openshift-ingress", "deployment/router-default", jsonPath)
			if envValue != routerEntry[1] {
				e2e.Logf("the retrieved default value of env: %s is not as expected: %s", envValue, routerEntry[1])
			}
			o.Expect(envValue == routerEntry[1]).To(o.BeTrue())
		}

		compat_otp.By("3.0 Check the haproxy.config that all default vaules of tested parameters are set as expected")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		for _, routerEntry := range allParas {
			if routerEntry[3] != "skip for none" {
				haCfg := ensureHaproxyBlockConfigContains(oc, routerpod, routerEntry[3], []string{routerEntry[3]})
				if !strings.Contains(haCfg, routerEntry[3]) {
					e2e.Logf("the retrieved default value of haproxy: %s is not as expected: %s", haCfg, routerEntry[3])
				}
			}
		}

		compat_otp.By("4.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By(`5.0: Debug node to configure the ingress in the config.yaml`)
		ingressConfig := fmt.Sprintf(`
ingress:
    forwardedHeaderPolicy: "Replace"
    httpCompression:
        mimeTypes:
            - "image"
    logEmptyRequests: "Ignore"
    tuningOptions:
        clientFinTimeout: "2s"
        clientTimeout: "60s"
        headerBufferBytes: 65536
        headerBufferMaxRewriteBytes: 16384
        healthCheckInterval: "10s"
        maxConnections: 100000
        serverFinTimeout: "2s"
        serverTimeout: "60s"
        threadCount: 8
        tlsInspectDelay: "10s"
        tunnelTimeout: "2h"`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("6.0 Check the router-default deployment that all updated ENVs of tested parameters are as expected")
		for _, routerEntry := range allParas {
			jsonPath := fmt.Sprintf(`{.spec.template.spec.containers[0].env[?(@.name=="%s")].value}`, routerEntry[0])
			envValue := getByJsonPath(oc, "openshift-ingress", "deployment/router-default", jsonPath)
			if envValue != routerEntry[2] {
				e2e.Logf("the retrieved updated value of env: %s is not as expected: %s", envValue, routerEntry[2])
			}
			o.Expect(envValue == routerEntry[2]).To(o.BeTrue())
		}

		compat_otp.By("7.0 Check the haproxy.config that all updated vaules of tested parameters are set as expected")
		routerpod = getOneRouterPodNameByIC(oc, "default")
		for _, routerEntry := range allParas {
			if routerEntry[4] != "skip for none" {
				haCfg := ensureHaproxyBlockConfigContains(oc, routerpod, routerEntry[4], []string{routerEntry[4]})
				if !strings.Contains(haCfg, routerEntry[4]) {
					e2e.Logf("the retrieved updated value of haproxy: %s is not as expected: %s", haCfg, routerEntry[4])
				}
			}
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-High-80508-supporting customerized default certification for Ingress Controller [Disruptive]", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			caseID              = "80508"
			e2eTestNamespace    = "e2e-ne-" + caseID + "-" + getRandomString()
			baseDomain          = "apps.example.com"
			podDirname          = "/data/OCP-" + caseID
			podDefaultCaCrt     = podDirname + "/" + caseID + "-ca.crt"
			podDefaultUsrCrt    = podDirname + "/" + caseID + "-usr.crt"
			podDefaultUsrKey    = podDirname + "/" + caseID + "-usr.key"
			dirname             = "/tmp/OCP-" + caseID
			validity            = 30
			defaultCaSubj       = "/CN=MS-default-CA"
			defaultCaCrt        = dirname + "/" + caseID + "-ca.crt"
			defaultCaKey        = dirname + "/" + caseID + "-ca.key"
			defaultCaCsr        = dirname + "/" + caseID + "-usr.csr"
			defaultUserSubj     = "/CN=example-ne.com"
			defaultUsrCrt       = dirname + "/" + caseID + "-usr.crt"
			defaultUsrKey       = dirname + "/" + caseID + "-usr.key"
			defaultUsrCsr       = dirname + "/" + caseID + "-usr.csr"
			defaultCnf          = dirname + "openssl.cnf"
			customCert          = "custom-cert" + caseID
			edgeRoute           = "route-edge" + caseID + "." + baseDomain
		)

		compat_otp.By("1.0: Use openssl to create a certification for the ingress default certification")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.1: Create a key for the ingress default certification")
		opensslCmd := fmt.Sprintf(`openssl genrsa -out %s 2048`, defaultCaKey)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.2: Create a csr for the ingress default certification")
		opensslCmd = fmt.Sprintf(`openssl req -new -key %s -subj %s  -out %s`, defaultCaKey, defaultCaSubj, defaultCaCsr)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.3: Create the extension file, then create the customerized certification for the ingress default certification")
		sanCfg := fmt.Sprintf(`
[ v3_req ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = *.%s
`, baseDomain)

		cmd := fmt.Sprintf(`echo "%s" > %s`, sanCfg, defaultCnf)
		_, err = exec.Command("bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile %s -extensions v3_req  -req -in %s -signkey  %s -days %d -sha256 -out %s`, defaultCnf, defaultCaCsr, defaultCaKey, validity, defaultCaCrt)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.4: Create a user CSR and the user key for a route")
		opensslNewCsr(defaultUsrKey, defaultUsrCsr, defaultUserSubj)

		compat_otp.By("1.5: Sign the user CSR and generate the user certificate for a route")
		san := "subjectAltName = DNS.1:*." + baseDomain + ",DNS.2:" + edgeRoute
		opensslSignCsr(san, defaultUsrCsr, defaultCaCrt, defaultCaKey, defaultUsrCrt)

		compat_otp.By("2.0: Create the secret in the cluster for the customerized default certification")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", "openshift-ingress", "secret", customCert).Output()
		output, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", "openshift-ingress", "secret", "tls", customCert, "--cert="+defaultCaCrt, "--key="+defaultCaKey).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("created"))

		compat_otp.By("3.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)

		compat_otp.By("4.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By(`5.0: Debug node and configure the default certification for the ingress`)
		ingressConfig := fmt.Sprintf(`
ingress:
    certificateSecret: "%s"`, customCert)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("6.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", e2eTestNamespace, dirname, e2eTestNamespace+"/"+clientPodName+":"+podDirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		compat_otp.By("7.0: Create an edge route for the testing")
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-edge", "default")

		compat_otp.By("8.0: Check the router-default deployment that the volume of default certificate is updated to the custom")
		output = getByJsonPath(oc, "openshift-ingress", "deployment/router-default", "{..volumes[?(@.name==\"default-certificate\")].secret.secretName}")
		o.Expect(output).To(o.ContainSubstring(customCert))

		compat_otp.By("9.0: Check the customed default certification in a router pod")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		output, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "openssl x509 -noout -in /etc/pki/tls/private/tls.crt -text").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Issuer: CN = MS-default-CA"))

		compat_otp.By("10.0: Curl the edge route with the user certification, issued by MS-default-CA")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := edgeRoute + ":443:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-sI", "--cacert", podDefaultCaCrt, "--cert", podDefaultUsrCrt, "--key", podDefaultUsrKey, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		compat_otp.By("11.0: Curl the edge route again without any certification")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-skI", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
	})

	// author: shudili@redhat.com
	// incorporate OCP-80510 and OCP-80513 into one
	g.It("Author:shudili-MicroShiftOnly-High-80510-supporting Old tlsSecurityProfile for the ingress controller [Disruptive]", func() {
		var (
			caseID           = "80510"
			e2eTestNamespace = "e2e-ne-" + caseID + "-" + getRandomString()
		)

		compat_otp.By("1.0: prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By("2.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)

		// OCP-80513 - [MicroShift] supporting Intermediate tlsSecurityProfile for the ingress controller
		compat_otp.By("3.0: Check default TLS env in a router pod that the SSL_MIN_VERSION, ROUTER_CIPHER and ROUTER_CIPHERS should be as same as Intermediate profile defined")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		env := readRouterPodEnv(oc, routerpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, routerpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384`))

		// OCP-80510 - [MicroShift] supporting Old tlsSecurityProfile for the ingress controller
		compat_otp.By("4.0: Debug node and configure the Old tls profile for the ingress")
		ingressConfig := fmt.Sprintf(`
ingress:
    tlsSecurityProfile:
        old: {}
        type: Old`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("5.0: Check the TLS env in a router pod that the SSL_MIN_VERSION, ROUTER_CIPHER and ROUTER_CIPHERS should be as same as Old profile defined")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.1`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA256:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA`))

		// OCP-80513 - [MicroShift] supporting Intermediate tlsSecurityProfile for the ingress controller
		compat_otp.By("6.0: Debug node and Configure the Intermidiate tls profile for the ingress")
		ingressConfig = fmt.Sprintf(`
ingress:
    tlsSecurityProfile:
        intermediate: {}
        type: Intermediate`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		compat_otp.By("7.0: Check TLS env in a router pod that the SSL_MIN_VERSION, ROUTER_CIPHER and ROUTER_CIPHERS should be as same as the default defined by the Intermediate profile")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384`))
	})

	// author: shudili@redhat.com
	// incorporate OCP-80514 and OCP-80516 into one
	g.It("Author:shudili-MicroShiftOnly-High-80514-supporting Modern tlsSecurityProfile for the ingress controller [Disruptive]", func() {
		var (
			caseID           = "80514"
			e2eTestNamespace = "e2e-ne-" + caseID + "-" + getRandomString()
		)

		compat_otp.By("1.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By("2.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)

		// OCP-80514 - [MicroShift] supporting Modern tlsSecurityProfile for the ingress controller

		compat_otp.By("3.0: Debug node and configure the Modern tls profile for the ingress")
		ingressConfig := fmt.Sprintf(`
ingress:
    tlsSecurityProfile:
        modern: {}
        type: Modern`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("4.0: Check TLS env in a router pod that the SSL_MIN_VERSION and ROUTER_CIPHERSUITES should be as same as the Modern profile defined")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		env := readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.3`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHERSUITES")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))

		compat_otp.By("5.0: Check the haproxy config on the router pod to ensure the ssl version TLSv1.3 is reflected")
		tlsVersion := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "ssl-min-ver")
		o.Expect(tlsVersion).To(o.ContainSubstring(`ssl-default-bind-options ssl-min-ver TLSv1.3`))

		// OCP-80516 - [MicroShift] supporting Custom tlsSecurityProfile for the ingress controller
		compat_otp.By("6.0: Debug node and configure the Custom tls profile for the ingress")
		ingressConfig = fmt.Sprintf(`
ingress:
    tlsSecurityProfile:
        custom:
          ciphers:
            - DHE-RSA-AES256-GCM-SHA384
            - ECDHE-ECDSA-AES256-GCM-SHA384
          minTLSVersion: VersionTLS12
        type: Custom`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		compat_otp.By("7.0: Check TLS env in a router pod that the SSL_MIN_VERSION and ROUTER_CIPHER should be as same as the Custom profile defined")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES256-GCM-SHA384`))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-High-80518-mTLS supporting client certificate with the subject filter [Disruptive]", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			caseID              = "80518"
			e2eTestNamespace    = "e2e-ne-" + caseID + "-" + getRandomString()
			baseDomain          = "apps.example.com"
			podDirname          = "/data/OCP-" + caseID + "-ca"
			podCaCrt            = podDirname + "/" + caseID + "-ca.crt"
			podUsrCrt           = podDirname + "/" + caseID + "-usr.crt"
			podUsrKey           = podDirname + "/" + caseID + "-usr.key"
			podUsrCrt2          = podDirname + "/" + caseID + "-usr2.crt"
			podUsrKey2          = podDirname + "/" + caseID + "-usr2.key"
			dirname             = "/tmp/OCP-" + caseID + "-ca"
			caSubj              = "/CN=MS-Test-Root-CA"
			caCrt               = dirname + "/" + caseID + "-ca.crt"
			caKey               = dirname + "/" + caseID + "-ca.key"
			userSubj            = "/CN=example-test.com"
			usrCrt              = dirname + "/" + caseID + "-usr.crt"
			usrKey              = dirname + "/" + caseID + "-usr.key"
			usrCsr              = dirname + "/" + caseID + "-usr.csr"
			userSubj2           = "/CN=example-test2.com"
			usrCrt2             = dirname + "/" + caseID + "-usr2.crt"
			usrKey2             = dirname + "/" + caseID + "-usr2.key"
			usrCsr2             = dirname + "/" + caseID + "-usr2.csr"
			cmName              = "ocp" + caseID
			filter              = userSubj
			edgeRoute           = "route-edge" + caseID + "." + baseDomain
			edgeRoute2          = "route2-edge" + caseID + "." + baseDomain
		)

		compat_otp.By("1.0: Use openssl to create custom client certification, create a new self-signed CA including the ca certification and ca key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslNewCa(caKey, caCrt, caSubj)

		compat_otp.By("1.1: Create a user CSR and the user key for a client")
		opensslNewCsr(usrKey, usrCsr, userSubj)

		compat_otp.By("1.2: Sign the user CSR and generate the user certificate")
		san := "subjectAltName = DNS.1:*." + baseDomain + ",DNS.2:" + edgeRoute
		opensslSignCsr(san, usrCsr, caCrt, caKey, usrCrt)

		compat_otp.By("1.3: Create another user CSR and the user key for the client")
		opensslNewCsr(usrKey2, usrCsr2, userSubj2)

		compat_otp.By("1.4: Sign the another user CSR and generate the user certificate")
		san = "subjectAltName = DNS.1:*." + baseDomain + ",DNS.2:" + edgeRoute2
		opensslSignCsr(san, usrCsr2, caCrt, caKey, usrCrt2)

		compat_otp.By("2.0: Create a cm with date ca certification")
		defer deleteConfigMap(oc, "openshift-ingress", cmName)
		createConfigMapFromFile(oc, "openshift-ingress", cmName, "ca-bundle.pem="+caCrt)

		compat_otp.By("3.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By("4.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		compat_otp.By("5.0: Debug node and configure clientTLS with allowedSubjectPatterns in the config.yaml for permitting the first route")
		ingressConfig := fmt.Sprintf(`
ingress:
    clientTLS:
        allowedSubjectPatterns: ["%s"]
        clientCA:
            name: "%s"
        clientCertificatePolicy: "Required"`, filter, cmName)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("6.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", e2eTestNamespace, dirname, e2eTestNamespace+"/"+clientPodName+":"+podDirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		compat_otp.By("7.0: Create two edge route for the testing, one mathing allowedSubjectPatterns of the clientTLS")
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute, "--cert=" + usrCrt, "--key=" + usrKey})
		createRoute(oc, e2eTestNamespace, "edge", "route-edge2", unSecSvcName, []string{"--hostname=" + edgeRoute2, "--cert=" + usrCrt2, "--key=" + usrKey2})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-edge", "default")
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-edge2", "default")

		compat_otp.By("8.0: Check the ROUTER_MUTUAL_TLS_AUTH env in a router pod")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		env := readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH_FILTER")
		o.Expect(env).To(o.ContainSubstring(filter))

		compat_otp.By("9.0: Curl the first edge route with the user certification, expect to get 200 OK for mathing the allowedSubjectPatterns of the clientTLS")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := edgeRoute + ":443:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		compat_otp.By("10.0: Curl the second edge route with the user2 certification, expect to get 403 Forbidden for not mathing the allowedSubjectPatterns of the clientTLS")
		toDst = edgeRoute2 + ":443:" + podIP
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute2, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt2, "--key", podUsrKey2, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "403 Forbidden", 60, 1)
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-High-80520-supporting wildcard routeAdmissionPolicy for the Ingress Controller [Disruptive]", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			caseID              = "80520"
			e2eTestNamespace    = "e2e-ne-" + caseID + "-" + getRandomString()
			baseDomain          = "apps.example.com"
		)

		compat_otp.By("1.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)

		compat_otp.By("2.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		compat_otp.By("3.0: For the default WildcardsDisallowed wildcardPolicy of routeAdmission, check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be false")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		namespaceOwnershipEnv := readRouterPodEnv(oc, routerpod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=false"))

		compat_otp.By("4.0: Create a route with wildcard-policy Subdomain, which should Not be Admitted")
		routehost := "wildcard." + baseDomain
		anyhost := "any." + baseDomain
		createRoute(oc, e2eTestNamespace, "http", "unsecure80520", unsecSvcName, []string{"--wildcard-policy=Subdomain", "--hostname=" + routehost})
		ensureRouteIsNotAdmittedByIngressController(oc, e2eTestNamespace, "unsecure80520", "default")

		compat_otp.By("5.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)

		compat_otp.By("6.0: Debug node to set WildcardsAllowed wildcardPolicy in the config.yaml file")
		ingressConfig := fmt.Sprintf(`
ingress:
    routeAdmissionPolicy:
        wildcardPolicy: "WildcardsAllowed"`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("7.0. Check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be true")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		namespaceOwnershipEnv = readRouterPodEnv(oc, routerpod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=true"))

		compat_otp.By("8.0: Curl the route with the two hostnames again, both should get 200 ok reponse")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + routehost, "-sI", "--resolve", toDst, "--connect-timeout", "10"}
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "unsecure80520", "default")
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		toDst = anyhost + ":80:" + podIP
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + anyhost, "-sI", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		compat_otp.By("9.0: Debug node to set WildcardsDisabllowed wildcardPolicy in the config.yaml file")
		ingressConfig = fmt.Sprintf(`
ingress:
    routeAdmissionPolicy:
        wildcardPolicy: "WildcardsDisallowed"`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		compat_otp.By("10.0: For the configured WildcardsDisallowed wildcardPolicy of routeAdmission, check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be false")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		namespaceOwnershipEnv = readRouterPodEnv(oc, routerpod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=false"))

		compat_otp.By(`11.0: Check the route's status, which should Not be Admitted`)
		ensureRouteIsNotAdmittedByIngressController(oc, e2eTestNamespace, "unsecure80520", "default")
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-High-80517-mTLS supporting client certificate with Optional or Required policy [Disruptive]", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			caseID              = "80517"
			e2eTestNamespace    = "e2e-ne-" + caseID + "-" + getRandomString()
			baseDomain          = "apps.example.com"
			podDirname          = "/data/OCP-" + caseID + "-ca"
			podCaCrt            = podDirname + "/" + caseID + "-ca.crt"
			podUsrCrt           = podDirname + "/" + caseID + "-usr.crt"
			podUsrKey           = podDirname + "/" + caseID + "-usr.key"
			dirname             = "/tmp/OCP-" + caseID + "-ca"
			caSubj              = "/CN=MS-Test-Root-CA"
			caCrt               = dirname + "/" + caseID + "-ca.crt"
			caKey               = dirname + "/" + caseID + "-ca.key"
			userSubj            = "/CN=example-test.com"
			usrCrt              = dirname + "/" + caseID + "-usr.crt"
			usrKey              = dirname + "/" + caseID + "-usr.key"
			usrCsr              = dirname + "/" + caseID + "-usr.csr"
			cmName              = "ocp" + caseID
			edgeRoute           = "route-edge" + caseID + "." + baseDomain
		)

		compat_otp.By("1.0: Use openssl to create custom client certification, create a new self-signed CA including the ca certification and ca key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslNewCa(caKey, caCrt, caSubj)

		compat_otp.By("1.1: Create a user CSR and the user key for a client")
		opensslNewCsr(usrKey, usrCsr, userSubj)

		compat_otp.By("1.2: Sign the user CSR and generate the user certificate")
		san := "subjectAltName = DNS.1:*." + baseDomain + ",DNS.2:" + edgeRoute
		opensslSignCsr(san, usrCsr, caCrt, caKey, usrCrt)

		compat_otp.By("2.0: Create a cm with date ca certification")
		defer deleteConfigMap(oc, "openshift-ingress", cmName)
		createConfigMapFromFile(oc, "openshift-ingress", cmName, "ca-bundle.pem="+caCrt)

		compat_otp.By("3.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)

		compat_otp.By("4.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By("5.0: Check the ROUTER_MUTUAL_TLS_AUTH env in a router pod which should be empty for the default clientCertificatePolicy")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		env := readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`NotFound`))

		compat_otp.By("6.0: Debug node and configure clientTLS with clientCertificatePolicy Required in the config.yaml")
		ingressConfig := fmt.Sprintf(`
ingress:
    clientTLS:
        clientCA:
            name: "%s"
        clientCertificatePolicy: "Required"`, cmName)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("7.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", e2eTestNamespace, dirname, e2eTestNamespace+"/"+clientPodName+":"+podDirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		compat_otp.By("8.0: Create an edge route for the testing")
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute, "--cert=" + usrCrt, "--key=" + usrKey})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-edge", "default")

		compat_otp.By("9.0: Check the ROUTER_MUTUAL_TLS_AUTH and ROUTER_MUTUAL_TLS_AUTH_CA envs in a router pod for the Required clientCertificatePolicy")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH=required`))
		env = readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH_CA")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH_CA=/etc/pki/tls/client-ca/ca-bundle.pem`))

		compat_otp.By("10.0: Curl the edge route with the user certification, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := edgeRoute + ":443:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		compat_otp.By("11.0: Curl the edge route without any certifications, expect to get SSL_read error")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-skv", "--resolve", toDst, "--connect-timeout", "10"}
		waitForErrorOccur(oc, curlCmd, "SSL_read: error", 60)

		compat_otp.By("12.0: Debug node and configure clientTLS with clientCertificatePolicy Optional in the config.yaml")
		ingressConfig = fmt.Sprintf(`
ingress:
    clientTLS:
        clientCA:
            name: "%s"
        clientCertificatePolicy: "Optional"`, cmName)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		compat_otp.By("13.0: Check the ROUTER_MUTUAL_TLS_AUTH env in a router pod for the Optional clientCertificatePolicy")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH=optional`))

		compat_otp.By("14.0: Curl the edge route with the user certification, expect to get 200 OK")
		podIP = getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = edgeRoute + ":443:" + podIP
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		compat_otp.By("15.0: Curl the edge route without any certifications, expect to get 200 OK")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-skI", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-High-81996-capture and log http cookies with specific prefixes via httpCaptureCookies option [Disruptive]", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			caseID              = "81996"
			e2eTestNamespace    = "e2e-ne-" + caseID + "-" + getRandomString()
			baseDomain          = "apps.example.com"
			routehost           = "route-unsec" + caseID + "." + baseDomain
		)

		compat_otp.By("1.0: Prepare a namespace for the testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)

		compat_otp.By("2.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By(`3.0: Debug node and configure "capture and log http cookies with specific prefixes via httpCaptureCookies option"`)
		ingressConfig := fmt.Sprintf(`
ingress:
    accessLogging:
        httpCaptureCookies:
            - matchType: Prefix
              maxLength: 100
              namePrefix: foo
        status: Enabled`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("4.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		compat_otp.By("5.0: Create a http route for the testing")
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-http", "default")

		compat_otp.By("6.0: Check httpCaptureCookies configuration in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		// ensureHaproxyBlockConfigContains(oc, routerpod, "defaults", []string{"capture cookie foo len 100"})

		compat_otp.By("7.0: Curl the http route with cookie fo=nobar, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "fo=nobar", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("8.0: Curl the http route with cookie foo=bar, expect to get 200 OK")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo=bar", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("9.0: Check the router logs, which should contain both the cookie foo=bar and the url")
		logs := waitRouterLogsAppear(oc, routerpod, "foo=bar")
		o.Expect(logs).To(o.ContainSubstring("index.html"))

		compat_otp.By("10.0: Check the router logs, which should NOT contain both the cookie fo=nobar")
		logs, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", "-c", "access-logs", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(logs).NotTo(o.ContainSubstring("fo=nobar"))

		compat_otp.By("11.0: Curl the http route with cookie foo22=bar22, expect to get 200 OK")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo22=bar22", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("12.0: Check the router logs, which should contain both the cookie foo22=bar22 and the url")
		logs = waitRouterLogsAppear(oc, routerpod, "foo22=bar22")
		o.Expect(logs).To(o.ContainSubstring("index.html"))
	})

	// includes OCP-81997 and OCP-81998
	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-High-81997-capture and log http cookies with exact match via httpCaptureCookies option [Disruptive]", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			caseID              = "81997"
			e2eTestNamespace    = "e2e-ne-" + caseID + "-" + getRandomString()
			baseDomain          = "apps.example.com"
			routehost           = "route-unsec" + caseID + "." + baseDomain
		)

		compat_otp.By("1.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		compat_otp.SetNamespacePrivileged(oc, e2eTestNamespace)

		compat_otp.By("2.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer restoreConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		backupConfigYaml(oc, e2eTestNamespace, caseID, nodeName)
		actualGenerationInt := getRouterDeploymentGeneration(oc, "router-default")

		compat_otp.By(`3.0: Debug node and configure "capture and log http cookies with specific prefixes via httpCaptureCookies option"`)
		ingressConfig := fmt.Sprintf(`ingress:
    accessLogging:
        httpCaptureCookies:
            - matchType: Exact
              maxLength: 100
              name: foo
        status: Enabled`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		compat_otp.By("4.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		compat_otp.By("5.0: Create a http route for the testing")
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, e2eTestNamespace, "route-http", "default")

		compat_otp.By("6.0: Check httpCaptureCookies configuration in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		// ensureHaproxyBlockConfigContains(oc, routerpod, "defaults", []string{"capture cookie foo= len 100"})

		compat_otp.By("7.0: Curl the http route with cookie fooor=nobar, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "fooor=nobar", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("8.0: Curl the http route with cookie foo=bar, expect to get 200 OK")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo=bar", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("9.0: Check the router logs, which should contain both the cookie foo=bar and the url")
		logs := waitRouterLogsAppear(oc, routerpod, "foo=bar")
		o.Expect(logs).To(o.ContainSubstring("index.html"))

		compat_otp.By("10.0: Check the router logs, which should NOT contain the cookie fooor=nobar")
		logs, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", "-c", "access-logs", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(logs).NotTo(o.ContainSubstring("fooor=nobar"))

		// OCP-81998(The httpCaptureCookies option strictly adheres to the maxlength parameter)
		compat_otp.By("11.0: debug node and configure maxLength for the httpCaptureCookies")
		ingressConfig = fmt.Sprintf(`ingress:
    accessLogging:
        httpCaptureCookies:
            - matchType: Exact
              maxLength: 10
              name: foo
        status: Enabled`)

		appendIngressToConfigYaml(oc, e2eTestNamespace, caseID, nodeName, ingressConfig)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		compat_otp.By("12.0: Curl the http route with cookie foo=bar89abdef, expect to get 200 OK")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		podIP = getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo=bar89abdef", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("13.0: Check the router logs, which should contain the cookie foo=bar89a, NOT foo=bar89abdef")
		logs = waitRouterLogsAppear(oc, routerpod, "foo=bar89a")
		o.Expect(logs).NotTo(o.ContainSubstring("foo=bar89ab"))
	})
})
