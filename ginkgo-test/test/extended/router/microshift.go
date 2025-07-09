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
	exutil "github.com/openshift/router/ginkgo-test/test/extended/util"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-network-edge] Network_Edge", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLIWithoutNamespace("router-microshift")

	g.It("Author:mjoseph-MicroShiftOnly-High-60136-reencrypt route using Ingress resource for Microshift with destination CA certificate", func() {
		var (
			e2eTestNamespace    = "e2e-ne-ocp60136-" + getRandomString()
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			ingressFile         = filepath.Join(buildPruningBaseDir, "microshift-ingress-destca.yaml")
		)

		exutil.By("create a namespace for the scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)

		exutil.By("create a web-server-deploy pod and its services")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, testPodSvc)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		podName := getPodListByLabel(oc, e2eTestNamespace, "name=web-server-deploy")
		ingressPod := getOneRouterPodNameByIC(oc, "default")

		exutil.By("create ingress using the file and get the route details")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, ingressFile)
		createResourceFromFile(oc, e2eTestNamespace, ingressFile)
		getIngress(oc, e2eTestNamespace)
		getRoutes(oc, e2eTestNamespace)
		routeNames := getResourceName(oc, e2eTestNamespace, "route")

		exutil.By("check whether route details are present")
		waitForOutput(oc, e2eTestNamespace, "route/"+routeNames[0], "{.status.ingress[0].conditions[0].type}", "Admitted")
		waitForOutput(oc, e2eTestNamespace, "route/"+routeNames[0], "{.status.ingress[0].host}", "service-secure-test.example.com")
		waitForOutput(oc, e2eTestNamespace, "route/"+routeNames[0], "{.spec.tls.termination}", "reencrypt")

		exutil.By("check the reachability of the host in test pod")
		routerPodIP := getPodv4Address(oc, ingressPod, "openshift-ingress")
		curlCmd := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "https://service-secure-test.example.com:443", "-k", "-I", "--resolve", "service-secure-test.example.com:443:" + routerPodIP, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		exutil.By("check the router pod and ensure the routes are loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ingress-ms-reen")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_secure:" + e2eTestNamespace + ":" + routeNames[0]))
	})

	g.It("Author:mjoseph-MicroShiftOnly-Critical-60266-creation of edge and passthrough routes for Microshift", func() {
		var (
			e2eTestNamespace    = "e2e-ne-ocp60266-" + getRandomString()
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			edgeRouteHost       = "route-edge-" + e2eTestNamespace + ".apps.example.com"
			passRouteHost       = "route-pass-" + e2eTestNamespace + ".apps.example.com"
		)

		exutil.By("create a namespace for the scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)

		exutil.By("create a web-server-deploy pod and its services")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, testPodSvc)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		podName := getPodListByLabel(oc, e2eTestNamespace, "name=web-server-deploy")
		ingressPod := getOneRouterPodNameByIC(oc, "default")

		exutil.By("create a passthrough route")
		createRoute(oc, e2eTestNamespace, "passthrough", "ms-pass", "service-secure", []string{"--hostname=" + passRouteHost})
		getRoutes(oc, e2eTestNamespace)

		exutil.By("check whether passthrough route details are present")
		waitForOutput(oc, e2eTestNamespace, "route/ms-pass", "{.spec.tls.termination}", "passthrough")
		waitForOutput(oc, e2eTestNamespace, "route/ms-pass", "{.status.ingress[0].host}", passRouteHost)
		waitForOutput(oc, e2eTestNamespace, "route/ms-pass", "{.status.ingress[0].conditions[0].type}", "Admitted")

		exutil.By("check the reachability of the host in test pod for passthrough route")
		routerPodIP := getPodv4Address(oc, ingressPod, "openshift-ingress")
		passRoute := passRouteHost + ":443:" + routerPodIP
		curlCmd := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "https://" + passRouteHost + ":443", "-k", "-I", "--resolve", passRoute, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		exutil.By("check the router pod and ensure the passthrough route is loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ms-pass")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_tcp:" + e2eTestNamespace + ":ms-pass"))

		exutil.By("create a edge route")
		createRoute(oc, e2eTestNamespace, "edge", "ms-edge", "service-unsecure", []string{"--hostname=" + edgeRouteHost})
		getRoutes(oc, e2eTestNamespace)

		exutil.By("check whether edge route details are present")
		waitForOutput(oc, e2eTestNamespace, "route/ms-edge", "{.spec.tls.termination}", "edge")
		waitForOutput(oc, e2eTestNamespace, "route/ms-edge", "{.status.ingress[0].host}", edgeRouteHost)
		waitForOutput(oc, e2eTestNamespace, "route/ms-edge", "{.status.ingress[0].conditions[0].type}", "Admitted")

		exutil.By("check the reachability of the host in test pod for edge route")
		edgeRoute := edgeRouteHost + ":443:" + routerPodIP
		curlCmd1 := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "https://" + edgeRouteHost + ":443", "-k", "-I", "--resolve", edgeRoute, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd1, "200", 30, 1)

		exutil.By("check the router pod and ensure the edge route is loaded in haproxy.config")
		searchOutput1 := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ms-edge")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_edge_http:" + e2eTestNamespace + ":ms-edge"))
	})

	g.It("Author:mjoseph-MicroShiftOnly-Critical-60283-creation of http and re-encrypt routes for Microshift", func() {
		var (
			e2eTestNamespace    = "e2e-ne-ocp60283-" + getRandomString()
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			httpRouteHost       = "route-http-" + e2eTestNamespace + ".apps.example.com"
			reenRouteHost       = "route-reen-" + e2eTestNamespace + ".apps.example.com"
		)

		exutil.By("create a namespace for the scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)

		exutil.By("create a signed web-server-deploy pod and its services")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, testPodSvc)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		podName := getPodListByLabel(oc, e2eTestNamespace, "name=web-server-deploy")
		ingressPod := getOneRouterPodNameByIC(oc, "default")

		exutil.By("create a http route")
		_, err := oc.WithoutNamespace().Run("expose").Args("-n", e2eTestNamespace, "--name=ms-http", "service", "service-unsecure", "--hostname="+httpRouteHost).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		getRoutes(oc, e2eTestNamespace)

		exutil.By("check whether http route details are present")
		waitForOutput(oc, e2eTestNamespace, "route/ms-http", "{.spec.port.targetPort}", "http")
		waitForOutput(oc, e2eTestNamespace, "route/ms-http", "{.status.ingress[0].host}", httpRouteHost)
		waitForOutput(oc, e2eTestNamespace, "route/ms-http", "{.status.ingress[0].conditions[0].type}", "Admitted")

		exutil.By("check the reachability of the host in test pod for http route")
		routerPodIP := getPodv4Address(oc, ingressPod, "openshift-ingress")
		httpRoute := httpRouteHost + ":80:" + routerPodIP
		curlCmd := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "http://" + httpRouteHost + ":80", "-k", "-I", "--resolve", httpRoute, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		exutil.By("check the router pod and ensure the http route is loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ms-http")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_http:" + e2eTestNamespace + ":ms-http"))

		exutil.By("create a reen route")
		createRoute(oc, e2eTestNamespace, "reencrypt", "ms-reen", "service-secure", []string{"--hostname=" + reenRouteHost})
		getRoutes(oc, e2eTestNamespace)

		exutil.By("check whether reen route details are present")
		waitForOutput(oc, e2eTestNamespace, "route/ms-reen", "{.spec.tls.termination}", "reencrypt")
		waitForOutput(oc, e2eTestNamespace, "route/ms-reen", "{.status.ingress[0].host}", reenRouteHost)
		waitForOutput(oc, e2eTestNamespace, "route/ms-reen", "{.status.ingress[0].conditions[0].type}", "Admitted")

		exutil.By("check the reachability of the host in test pod reen route")
		reenRoute := reenRouteHost + ":443:" + routerPodIP
		curlCmd1 := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "https://" + reenRouteHost + ":443", "-k", "-I", "--resolve", reenRoute, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd1, "200", 30, 1)

		exutil.By("check the router pod and ensure the reen route is loaded in haproxy.config")
		searchOutput1 := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ms-reen")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_secure:" + e2eTestNamespace + ":ms-reen"))
	})

	g.It("Author:mjoseph-MicroShiftOnly-Critical-60149-http route using Ingress resource for Microshift", func() {
		var (
			e2eTestNamespace    = "e2e-ne-ocp60149-" + getRandomString()
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			ingressFile         = filepath.Join(buildPruningBaseDir, "microshift-ingress-http.yaml")
			httpRoute           = "service-unsecure-test.example.com"
		)

		exutil.By("create a namespace for the scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)

		exutil.By("create a web-server-deploy pod and its services")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, testPodSvc)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		podName := getPodListByLabel(oc, e2eTestNamespace, "name=web-server-deploy")
		ingressPod := getOneRouterPodNameByIC(oc, "default")

		exutil.By("create ingress using the file and get the route details")
		defer operateResourceFromFile(oc, "delete", e2eTestNamespace, ingressFile)
		createResourceFromFile(oc, e2eTestNamespace, ingressFile)
		getIngress(oc, e2eTestNamespace)
		getRoutes(oc, e2eTestNamespace)
		routeNames := getResourceName(oc, e2eTestNamespace, "route")

		exutil.By("check whether http route details are present")
		waitForOutput(oc, e2eTestNamespace, "route/"+routeNames[0], "{.spec.port.targetPort}", "http")
		waitForOutput(oc, e2eTestNamespace, "route/"+routeNames[0], "{.status.ingress[0].host}", httpRoute)
		waitForOutput(oc, e2eTestNamespace, "route/"+routeNames[0], "{.status.ingress[0].conditions[0].type}", "Admitted")

		exutil.By("check the reachability of the host in test pod for http route")
		routerPodIP := getPodv4Address(oc, ingressPod, "openshift-ingress")
		curlCmd := []string{"-n", e2eTestNamespace, podName[0], "--", "curl", "http://service-unsecure-test.example.com:80", "-k", "-I", "--resolve", "service-unsecure-test.example.com:80:" + routerPodIP, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		exutil.By("check the router pod and ensure the http route is loaded in haproxy.config")
		searchOutput := readRouterPodData(oc, ingressPod, "cat haproxy.config", "ingress-on-microshift")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_http:" + e2eTestNamespace + ":" + routeNames[0]))
	})

	g.It("Author:shudili-MicroShiftOnly-High-72802-make router namespace ownership check configurable for the default microshift configuration", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
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

		exutil.By("1. check the Env ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK of deployment/default-router, which should be true for the default configuration")
		routerPodName := getOneRouterPodNameByIC(oc, "default")
		defaultVal := readRouterPodEnv(oc, routerPodName, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(defaultVal).To(o.ContainSubstring("true"))

		exutil.By("2. prepare two namespaces for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace1)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace1)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace1)
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace2)
		path1 := "/path"
		path2 := "/test"
		httpRoutehost := unSecSvcName + "-" + "ocp72802." + "apps.example.com"
		edgeRoute := "route-edge" + "-" + "ocp72802." + "apps.example.com"
		reenRoute := "route-reen" + "-" + "ocp72802." + "apps.example.com"

		exutil.By("3. create a client pod, a server pod and two services in one ns")
		createResourceFromFile(oc, e2eTestNamespace1, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace1, clientPodLabel)

		createResourceFromFile(oc, e2eTestNamespace1, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace1, "name="+srvrcInfo)

		exutil.By("4. create a server pod and two services in the other ns")
		createResourceFromFile(oc, e2eTestNamespace2, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, clientPodLabel)

		createResourceFromFile(oc, e2eTestNamespace2, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		exutil.By("5. expose an insecure/edge/REEN type routes with path " + path1 + " in the first ns")
		err := oc.Run("expose").Args("service", unSecSvcName, "--hostname="+httpRoutehost, "--path="+path1, "-n", e2eTestNamespace1).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutput(oc, e2eTestNamespace1, "route", "{.items[0].metadata.name}", unSecSvcName)

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

		exutil.By("6. expose an insecure/edge/REEN type routes with path " + path2 + " in the second ns")
		err = oc.Run("expose").Args("service", unSecSvcName, "--hostname="+httpRoutehost, "--path="+path2, "-n", e2eTestNamespace2).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutput(oc, e2eTestNamespace2, "route", "{.items[0].metadata.name}", unSecSvcName)

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

		exutil.By("7.1. check the http route in the first ns should be adimitted")
		jpath := "{.status.ingress[0].conditions[0].status}"
		adtInfo := getByJsonPath(oc, e2eTestNamespace1, "route/"+unSecSvcName, jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		exutil.By("7.2. check the edge route in the first ns should be adimitted")
		adtInfo = getByJsonPath(oc, e2eTestNamespace1, "route/route-edge", jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		exutil.By("7.3. check the REEN route in the first ns should be adimitted")
		adtInfo = getByJsonPath(oc, e2eTestNamespace1, "route/route-reen", jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		exutil.By("8.1. check the http route in the second ns with the same hostname but with different path should be adimitted too")
		adtInfo = getByJsonPath(oc, e2eTestNamespace2, "route/"+unSecSvcName, jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		exutil.By("8.2. check the edge route in the second ns with the same hostname but with different path should be adimitted too")
		adtInfo = getByJsonPath(oc, e2eTestNamespace2, "route/route-edge", jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		exutil.By("8.3. check the REEN route in the second ns with the same hostname but with different path should be adimitted too")
		adtInfo = getByJsonPath(oc, e2eTestNamespace2, "route/route-reen", jpath)
		o.Expect(adtInfo).To(o.Equal("True"))

		exutil.By("9. curl the first HTTP route and check the result")
		srvPodName := getPodListByLabel(oc, e2eTestNamespace1, "name=web-server-deploy")
		routerPodIP := getPodv4Address(oc, routerPodName, "openshift-ingress")
		toDst := httpRoutehost + ":80:" + routerPodIP
		cmdOnPod := []string{"-n", e2eTestNamespace1, clientPodName, "--", "curl", "http://" + httpRoutehost + "/path/index.html", "--resolve", toDst, "--connect-timeout", "10"}
		result, _ := repeatCmdOnClient(oc, cmdOnPod, "http-8080", 30, 1)
		o.Expect(result).To(o.ContainSubstring("http-8080"))
		output, err = oc.Run("exec").Args(cmdOnPod...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("ocp-test " + srvPodName[0] + " http-8080"))

		exutil.By("10. curl the second HTTP route and check the result")
		srvPodName = getPodListByLabel(oc, e2eTestNamespace2, "name=web-server-deploy")
		cmdOnPod = []string{"-n", e2eTestNamespace1, clientPodName, "--", "curl", "http://" + httpRoutehost + "/test/index.html", "--resolve", toDst, "--connect-timeout", "10"}
		result, _ = repeatCmdOnClient(oc, cmdOnPod, "http-8080", 60, 1)
		o.Expect(result).To(o.ContainSubstring("Hello-OpenShift-Path-Test " + srvPodName[0] + " http-8080"))
	})

	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-Longduration-Medium-73621-Disable/Enable namespace ownership support for router [Disruptive]", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			e2eTestNamespace1   = "e2e-ne-73621-" + getRandomString()
			e2eTestNamespace2   = "e2e-ne-73621-" + getRandomString()
		)

		exutil.By("1. prepare two namespaces for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace1)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace1)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace1)
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace2)
		path1 := "/path"
		path2 := "/path/second"
		httpRouteHost := unSecSvcName + "-" + "ocp73621." + "apps.example.com"

		exutil.By("2. debug node to disable namespace ownership support by setting namespaceOwnership to Strict in the config.yaml file")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		creatFileCmdForDisabled := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup73621
else
    touch /etc/microshift/config.yaml.no73621
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    routeAdmissionPolicy:
        namespaceOwnership: Strict
EOF`)

		sedCmd := fmt.Sprintf(`sed -i'' -e 's|Strict|InterNamespaceAllowed|g' /etc/microshift/config.yaml`)
		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no73621; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no73621
elif test -f /etc/microshift/config.yaml.backup73621 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup73621 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup73621
fi
`)

		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace1, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace1, nodeName)
		}()

		_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace1, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", creatFileCmdForDisabled).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace1, nodeName)

		exutil.By("3. check the Env ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK of deployment/default-router, which should be false")
		routerPodName := getOneRouterPodNameByIC(oc, "default")
		// wait some time and make sure the changes are done on the router pod
		time.Sleep(5 * time.Second)
		ownershipVal := readRouterPodEnv(oc, routerPodName, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(ownershipVal).To(o.ContainSubstring("false"))

		exutil.By("4. create a server pod and the services in one ns")
		createResourceFromFile(oc, e2eTestNamespace1, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace1, "name="+srvrcInfo)

		exutil.By("5. create a route with path " + path1 + " in the first ns, which should be admitted")
		extraParas := []string{"--hostname=" + httpRouteHost, "--path=" + path1}
		jpath := "{.status.ingress[0].conditions[0].status}"
		createRoute(oc, e2eTestNamespace1, "http", "route-http", unSecSvcName, extraParas)
		waitForOutput(oc, e2eTestNamespace1, "route", "{.items[0].metadata.name}", "route-http")
		waitForOutput(oc, e2eTestNamespace1, "route/route-http", jpath, "True")

		exutil.By("6. create a server pod and the services in the other ns")
		createResourceFromFile(oc, e2eTestNamespace2, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		exutil.By("7. create a route with path " + path2 + " in the second ns, which should NOT be admitted")
		extraParas = []string{"--hostname=" + httpRouteHost, "--path=" + path2}
		createRoute(oc, e2eTestNamespace2, "http", "route-http", unSecSvcName, extraParas)
		waitForOutput(oc, e2eTestNamespace2, "route", "{.items[0].metadata.name}", "route-http")
		waitForOutput(oc, e2eTestNamespace2, "route/route-http", jpath, "False")

		exutil.By("8. check the two routes with same hostname but with different path for the second time, the first one is adimitted, while the second one isn't")
		adtInfo := getByJsonPath(oc, e2eTestNamespace1, "route/route-http", jpath)
		o.Expect(adtInfo).To(o.ContainSubstring("True"))
		adtInfo = getByJsonPath(oc, e2eTestNamespace2, "route/route-http", jpath)
		o.Expect(adtInfo).To(o.ContainSubstring("False"))

		exutil.By("9. Confirm the second route is shown as HostAlreadyClaimed")
		jpath2 := `{.status.ingress[?(@.routerName=="default")].conditions[*].reason}`
		searchOutput := getByJsonPath(oc, e2eTestNamespace2, "route/route-http", jpath2)
		o.Expect(searchOutput).To(o.ContainSubstring("HostAlreadyClaimed"))

		exutil.By("10. debug node to enable namespace ownership support by setting namespaceOwnership to InterNamespaceAllowed in the config.yaml file")
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace1, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace1, nodeName)

		exutil.By("11. check the two route with same hostname but with different path, both of them should be adimitted")
		waitForOutput(oc, e2eTestNamespace1, "route/route-http", jpath, "True")
		waitForOutput(oc, e2eTestNamespace2, "route/route-http", jpath, "True")

		exutil.By("12. Confirm no route is shown as HostAlreadyClaimed")
		searchOutput1 := getByJsonPath(oc, e2eTestNamespace1, "route/route-http", jpath2)
		searchOutput2 := getByJsonPath(oc, e2eTestNamespace2, "route/route-http", jpath2)
		o.Expect(strings.Count(searchOutput1+searchOutput2, "HostAlreadyClaimed")).To(o.Equal(0))
	})

	g.It("Author:shudili-MicroShiftOnly-High-73152-Expose router as load balancer service type", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "service-unsecure"
			secsvcName          = "service-secure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			e2eTestNamespace    = "e2e-ne-ocp73152-" + getRandomString()
		)

		exutil.By("Check the router-default service is a load balancer and has a load balancer ip")
		svcType := getByJsonPath(oc, "openshift-ingress", "service/router-default", "{.spec.type}")
		o.Expect(svcType).To(o.ContainSubstring("LoadBalancer"))
		lbIPs := getByJsonPath(oc, "openshift-ingress", "service/router-default", "{.status.loadBalancer.ingress[0].ip}")
		o.Expect(len(lbIPs) > 4).To(o.BeTrue())

		exutil.By("Deploy a project with a client pod, a backend pod and its services resources")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")

		exutil.By("Create a HTTP/Edge/Passthrough/REEN route")
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
		waitForOutput(oc, e2eTestNamespace, "route/route-reen", "{.status.ingress[0].conditions[0].status}", "True")
		output := getByJsonPath(oc, e2eTestNamespace, "route", "{.items[*].metadata.name}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring("route-http"),
			o.ContainSubstring("route-edge"),
			o.ContainSubstring("route-passth"),
			o.ContainSubstring("route-reen")))

		exutil.By("Curl the HTTP route")
		routeReq := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + httpRouteHost, "-I", "--resolve", httpRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		exutil.By("Curl the Edge route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRouteHost, "-k", "-I", "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		exutil.By("Curl the Passthrough route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + passThRouteHost, "-k", "-I", "--resolve", passThRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		exutil.By("Curl the REEN route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + reenRouteHost, "-k", "-I", "--resolve", reenRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)
	})

	g.It("Author:shudili-MicroShiftOnly-High-73202-Add configurable listening IP addresses and listening ports", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
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

		exutil.By("create a namespace for testing, then debug node and get the valid host ips")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)
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

		exutil.By("check the default load balancer ips of the router-default service, which should be all node's valid host ips")
		lbIPs := getByJsonPath(oc, "openshift-ingress", "service/router-default", "{.status.loadBalancer.ingress[*].ip}")
		lbIPs = getSortedString(lbIPs)
		hostIPs := getSortedString(hostIPList)
		o.Expect(lbIPs).To(o.Equal(hostIPs))

		exutil.By("check the default load balancer ports of the router-default service, which should be 80 for the unsecure http port and 443 for the seccure https port")
		httpPort := getByJsonPath(oc, "openshift-ingress", "service/router-default", `{.spec.ports[?(@.name=="http")].port}`)
		o.Expect(httpPort).To(o.Equal("80"))
		httpsPort := getByJsonPath(oc, "openshift-ingress", "service/router-default", `{.spec.ports[?(@.name=="https")].port}`)
		o.Expect(httpsPort).To(o.Equal("443"))

		exutil.By("Deploy a backend pod and its services resources in the created ns")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")

		exutil.By("Create a HTTP/Edge/Passthrough/REEN route")
		httpRouteHost := unsecsvcName + "-" + "ocp73202." + "apps.example.com"
		edgeRouteHost := "route-edge" + "-" + "ocp73202." + "apps.example.com"
		passThRouteHost := "route-passth" + "-" + "ocp73202." + "apps.example.com"
		reenRouteHost := "route-reen" + "-" + "ocp73202." + "apps.example.com"
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecsvcName, []string{"--hostname=" + httpRouteHost})
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRouteHost})
		createRoute(oc, e2eTestNamespace, "passthrough", "route-passth", secsvcName, []string{"--hostname=" + passThRouteHost})
		createRoute(oc, e2eTestNamespace, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRouteHost})
		waitForOutput(oc, e2eTestNamespace, "route/route-reen", "{.status.ingress[0].conditions[0].status}", "True")
		output := getByJsonPath(oc, e2eTestNamespace, "route", "{.items[*].metadata.name}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring("route-http"),
			o.ContainSubstring("route-edge"),
			o.ContainSubstring("route-passth"),
			o.ContainSubstring("route-reen")))

		exutil.By("Curl the routes with destination to each load balancer ip")
		for _, lbIP := range strings.Split(lbIPs, " ") {
			// config firewall for ipv6 load balancer
			configFwForLB(oc, e2eTestNamespace, nodeName, lbIP)
			httpRouteDst := httpRouteHost + ":80:" + lbIP
			edgeRouteDst := edgeRouteHost + ":443:" + lbIP
			passThRouteDst := passThRouteHost + ":443:" + lbIP
			reenRouteDst := reenRouteHost + ":443:" + lbIP

			exutil.By("Curl the http route with destination " + lbIP)
			routeReq := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + httpRouteHost, "-I", "--resolve", httpRouteDst, "--connect-timeout", "10"}
			repeatCmdOnClient(oc, routeReq, "200", 150, 1)

			exutil.By("Curl the Edge route with destination " + lbIP)
			routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRouteHost, "-k", "-I", "--resolve", edgeRouteDst, "--connect-timeout", "10"}
			repeatCmdOnClient(oc, routeReq, "200", 60, 1)

			exutil.By("Curl the Pass-through route with destination " + lbIP)
			routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + passThRouteHost, "-k", "-I", "--resolve", passThRouteDst, "--connect-timeout", "10"}
			repeatCmdOnClient(oc, routeReq, "200", 60, 1)

			exutil.By("Curl the REEN route with destination " + lbIP)
			routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + reenRouteHost, "-k", "-I", "--resolve", reenRouteDst, "--connect-timeout", "10"}
			repeatCmdOnClient(oc, routeReq, "200", 60, 1)
		}
	})

	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-Longduration-High-73203-configuring listening IP addresses and listening Ports [Disruptive]", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "service-unsecure"
			secsvcName          = "service-secure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			specifiedAddress    string
			randHostIP          string
			e2eTestNamespace    = "e2e-ne-ocp73203-" + getRandomString()
		)

		exutil.By(`create a namespace for testing, then debug node and get all valid host interfaces and invalid host ips`)
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)
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

		exutil.By(`create the config.yaml under the node with the desired listening IP addresses and listening Ports, if there is the old config.yaml, then make a copy at first`)
		creatFileCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup73203
else
    touch /etc/microshift/config.yaml.no73203
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    listenAddress:
        - %s
    ports:
        http: 10080
        https: 10443
EOF`, specifiedAddress)

		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no73203; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no73203
elif test -f /etc/microshift/config.yaml.backup73203 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup73203 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup73203
fi 
`)

		// restored to default by the defer function before the case finishes running
		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()
		_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", creatFileCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)

		exutil.By("wait the check router-default service is updated and its load balancer ip is as same as configured in default.yaml")
		regExp := "^" + randHostIP + "$"
		searchOutput := waitForRegexpOutput(oc, "openshift-ingress", "service/router-default", "{.status.loadBalancer.ingress[*].ip}", regExp)
		o.Expect(searchOutput).To(o.Equal(randHostIP))

		exutil.By("check service router-default's http port is changed to 10080 and its https port is changed to 10443")
		jpath := `{.spec.ports[?(@.name=="http")].port}`
		httpPort := getByJsonPath(oc, "openshift-ingress", "svc/router-default", jpath)
		o.Expect(httpPort).To(o.Equal("10080"))
		jpath = `{.spec.ports[?(@.name=="https")].port}`
		httpsPort := getByJsonPath(oc, "openshift-ingress", "svc/router-default", jpath)
		o.Expect(httpsPort).To(o.Equal("10443"))

		exutil.By("Deploy a client pod, a backend pod and its services resources")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")

		exutil.By("Create a HTTP/Edge/Passthrough/REEN route")
		httpRouteHost := unsecsvcName + "-" + "ocp73203." + "apps.example.com"
		edgeRouteHost := "route-edge" + "-" + "ocp73203." + "apps.example.com"
		passThRouteHost := "route-passth" + "-" + "ocp73203." + "apps.example.com"
		reenRouteHost := "route-reen" + "-" + "ocp73203." + "apps.example.com"
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecsvcName, []string{"--hostname=" + httpRouteHost})
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRouteHost})
		createRoute(oc, e2eTestNamespace, "passthrough", "route-passth", secsvcName, []string{"--hostname=" + passThRouteHost})
		createRoute(oc, e2eTestNamespace, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRouteHost})
		waitForOutput(oc, e2eTestNamespace, "route/route-reen", "{.status.ingress[0].conditions[0].status}", "True")
		output := getByJsonPath(oc, e2eTestNamespace, "route", "{.items[*].metadata.name}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring("route-http"),
			o.ContainSubstring("route-edge"),
			o.ContainSubstring("route-passth"),
			o.ContainSubstring("route-reen")))

		exutil.By("Curl the routes with destination to the the custom load balancer ip and http/https ports")
		httpRouteDst := httpRouteHost + ":10080:" + randHostIP
		edgeRouteDst := edgeRouteHost + ":10443:" + randHostIP
		passThRouteDst := passThRouteHost + ":10443:" + randHostIP
		reenRouteDst := reenRouteHost + ":10443:" + randHostIP

		exutil.By("Curl the http route")
		// config firewall for ipv6 load balancer
		configFwForLB(oc, e2eTestNamespace, nodeName, randHostIP)

		routeReq := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + httpRouteHost + ":10080", "-I", "--resolve", httpRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 150, 1)

		exutil.By("Curl the Edge route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRouteHost + ":10443", "-k", "-I", "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		exutil.By("Curl the Passthrough route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + passThRouteHost + ":10443", "-k", "-I", "--resolve", passThRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)

		exutil.By("Curl the REEN route")
		routeReq = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + reenRouteHost + ":10443", "-k", "-I", "--resolve", reenRouteDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, routeReq, "200", 60, 1)
	})

	g.It("MicroShiftOnly-Author:shudili-NonPreRelease-Longduration-High-73209-Add enable/disable option for default router [Disruptive]", func() {
		var e2eTestNamespace = "e2e-ne-ocp73209-" + getRandomString()

		exutil.By("create a namespace for testing, then debug node and get the valid host ips")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)

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

		exutil.By("debug node to disable the default router by setting ingress status to Removed")
		creatFileCmdForDisablingRouter := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup73209
else
    touch /etc/microshift/config.yaml.no73209
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    status: Removed
EOF`)

		sedCmd := fmt.Sprintf(`sed -i'' -e 's|Removed|Managed|g' /etc/microshift/config.yaml`)
		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no73209; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no73209
elif test -f /etc/microshift/config.yaml.backup73209 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup73209 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup73209
fi
`)

		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()

		_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", creatFileCmdForDisablingRouter).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)

		exutil.By("check the openshift-ingress namespace will be deleted")
		err = waitForResourceToDisappear(oc, "default", "ns/"+"openshift-ingress")
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "namespace openshift-ingress"))

		exutil.By("debug node to enable the default router by setting ingress status to Managed")
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)

		exutil.By("check router-default load balancer is enabled")
		waitForOutput(oc, "openshift-ingress", "service/router-default", `{.spec.ports[?(@.name=="http")].port}`, "80")
		lbIPs := getByJsonPath(oc, "openshift-ingress", "service/router-default", "{.status.loadBalancer.ingress[*].ip}")
		lbIPs = getSortedString(lbIPs)
		o.Expect(lbIPs).To(o.Equal(hostIPs))
		httpsPort := getByJsonPath(oc, "openshift-ingress", "svc/router-default", `{.spec.ports[?(@.name=="https")].port}`)
		o.Expect(httpsPort).To(o.Equal("443"))
	})

	g.It("Author:shudili-MicroShiftOnly-High-77349-introduce ingress controller customization with microshift config.yaml [Disruptive]", func() {

		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			unsecsvcName        = "service-unsecure"
			e2eTestNamespace    = "e2e-ne-ocp77349-" + getRandomString()
			httpRouteHost       = unsecsvcName + "-" + "ocp77349." + "apps.example.com"

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

		exutil.By("1.0 Deploy a project with a backend pod and its services resources, then create a route")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name=web-server-deploy")
		createRoute(oc, e2eTestNamespace, "http", "route-http", unsecsvcName, []string{"--hostname=" + httpRouteHost})

		exutil.By("2.0 check the router-default deployment that all default ENVs of tested parameters are as expected")
		for _, routerEntry := range allParas {
			jsonPath := fmt.Sprintf(`{.spec.template.spec.containers[0].env[?(@.name=="%s")].value}`, routerEntry[0])
			envValue := getByJsonPath(oc, "openshift-ingress", "deployment/router-default", jsonPath)
			if envValue != routerEntry[1] {
				e2e.Logf("the retrieved default value of env: %s is not as expected: %s", envValue, routerEntry[1])
			}
			o.Expect(envValue == routerEntry[1]).To(o.BeTrue())
		}

		exutil.By("3.0 check the haproxy.config that all default vaules of tested parameters are set as expected")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		for _, routerEntry := range allParas {
			if routerEntry[3] != "skip for none" {
				haCfg := readHaproxyConfig(oc, routerpod, routerEntry[3], "-A0", routerEntry[3])
				if !strings.Contains(haCfg, routerEntry[3]) {
					e2e.Logf("the retrieved default value of haproxy: %s is not as expected: %s", haCfg, routerEntry[3])
				}
				o.Expect(haCfg).To(o.ContainSubstring(routerEntry[3]))
			}
		}

		exutil.By("4.0 debug node to configure the microshift config.yaml")
		configIngressCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup77349
else
    touch /etc/microshift/config.yaml.no77349
fi
cat >> /etc/microshift/config.yaml << EOF
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
        tunnelTimeout: "2h"
EOF`)

		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no77349; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no77349
elif test -f /etc/microshift/config.yaml.backup77349 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup77349 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup77349
fi
`)

		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()

		_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", configIngressCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)

		exutil.By("5.0 check the router-default deployment that all updated ENVs of tested parameters are as expected")
		for _, routerEntry := range allParas {
			jsonPath := fmt.Sprintf(`{.spec.template.spec.containers[0].env[?(@.name=="%s")].value}`, routerEntry[0])
			envValue := getByJsonPath(oc, "openshift-ingress", "deployment/router-default", jsonPath)
			if envValue != routerEntry[2] {
				e2e.Logf("the retrieved updated value of env: %s is not as expected: %s", envValue, routerEntry[2])
			}
			o.Expect(envValue == routerEntry[2]).To(o.BeTrue())
		}

		exutil.By("6.0 check the haproxy.config that all updated vaules of tested parameters are set as expected")
		routerpod = getOneRouterPodNameByIC(oc, "default")
		for _, routerEntry := range allParas {
			if routerEntry[4] != "skip for none" {
				haCfg := readHaproxyConfig(oc, routerpod, routerEntry[4], "-A0", routerEntry[4])
				if !strings.Contains(haCfg, routerEntry[4]) {
					e2e.Logf("the retrieved updated value of haproxy: %s is not as expected: %s", haCfg, routerEntry[4])
				}
				o.Expect(haCfg).To(o.ContainSubstring(routerEntry[4]))
			}
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-High-80508-supporting customerized default certification for Ingress Controller [Disruptive]", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod-withprivilege.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			e2eTestNamespace    = "e2e-ne-80508-" + getRandomString()
			baseDomain          = "apps.example.com"
			dirname             = "/tmp/OCP-80508"
			validity            = 30
			defaultCaSubj       = "/CN=MS-default-CA"
			defaultCaCrt        = dirname + "/80508-ca.crt"
			defaultCaKey        = dirname + "/80508-ca.key"
			defaultCaCsr        = dirname + "/80508-usr.csr"
			defaultUserSubj     = "/CN=example-ne.com"
			defaultUsrCrt       = dirname + "/80508-usr.crt"
			defaultUsrKey       = dirname + "/80508-usr.key"
			defaultUsrCsr       = dirname + "/80508-usr.csr"
			defaultCnf          = dirname + "openssl.cnf"
			edgeRoute           = "route-edge80508." + baseDomain
		)

		exutil.By("1.0: Use openssl to create a certification for the ingress default certification")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("1.1: Create a key for the ingress default certification")
		opensslCmd := fmt.Sprintf(`openssl genrsa -out %s 2048`, defaultCaKey)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("1.2: Create a csr for the ingress default certification")
		opensslCmd = fmt.Sprintf(`openssl req -new -key %s -subj %s  -out %s`, defaultCaKey, defaultCaSubj, defaultCaCsr)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("1.3: Create the extension file, then create the customerized certification for the ingress default certification")
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

		exutil.By("1.4: Create a user CSR and the user key for a route")
		opensslNewCsr(defaultUsrKey, defaultUsrCsr, defaultUserSubj)

		exutil.By("1.5: Sign the user CSR and generate the user certificate for a route")
		san := "subjectAltName = DNS.1:*." + baseDomain + ",DNS.2:" + edgeRoute
		opensslSignCsr(san, defaultUsrCsr, defaultCaCrt, defaultCaKey, defaultUsrCrt)

		exutil.By("2.0: Create the secret in the cluster for the customerized default certification")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", "openshift-ingress", "secret", "custom-cert80508").Output()
		output, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", "openshift-ingress", "secret", "tls", "custom-cert80508", "--cert="+defaultCaCrt, "--key="+defaultCaKey).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("created"))

		exutil.By("3.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)

		exutil.By("4.0: Debug node and configure the config.yaml file to configure the default certification")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		customConfig := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup80508
else
    touch /etc/microshift/config.yaml.no80508
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    certificateSecret: "custom-cert80508"
EOF`)

		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no80508; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no80508
elif test -f /etc/microshift/config.yaml.backup80508 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup80508 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup80508
fi
`)

		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()

		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)

		exutil.By("5.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", e2eTestNamespace, dirname, e2eTestNamespace+"/"+clientPodName+":"+dirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		exutil.By("6.0: Create an edge route for the testing")
		jsonPath := "{.status.ingress[0].conditions[?(@.type==\"Admitted\")].status}"
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute})
		waitForOutput(oc, e2eTestNamespace, "route/route-edge", jsonPath, "True")

		exutil.By("7.0: Check the router-default deployment that the volume of default certificate is updated to the custom")
		output = getByJsonPath(oc, "openshift-ingress", "deployment/router-default", "{..volumes[?(@.name==\"default-certificate\")].secret.secretName}")
		o.Expect(output).To(o.ContainSubstring("custom-cert80508"))

		exutil.By("8.0: check the customed default certification in a router pod")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		output, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "openssl x509 -noout -in /etc/pki/tls/private/tls.crt -text").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Issuer: CN = MS-default-CA"))

		exutil.By("9.0: Curl the edge route with the user certification, issued by MS-default-CA")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := edgeRoute + ":443:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-sI", "--cacert", defaultCaCrt, "--cert", defaultUsrCrt, "--key", defaultUsrKey, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		exutil.By("10.0: Curl the edge route again without any certification")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-skI", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
	})

	// author: shudili@redhat.com
	// incorporate OCP-80510 and OCP-80513 into one
	g.It("Author:shudili-MicroShiftOnly-High-80510-supporting Old tlsSecurityProfile for the ingress controller [Disruptive]", func() {
		var e2eTestNamespace = "e2e-ne-123456-" + getRandomString()

		exutil.By("1.0: prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		actualGenerationInt, _ := strconv.Atoi(actualGen)

		exutil.By("2.0: debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		backupConfig := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup80510
else
    touch /etc/microshift/config.yaml.no80510
fi
`)
		_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", backupConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no80510; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no80510
elif test -f /etc/microshift/config.yaml.backup80510 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup80510 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup80510
fi
`)
		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()

		// OCP-80513 - [MicroShift] supporting Intermediate tlsSecurityProfile for the ingress controller
		exutil.By("3.0: Check default TLS env in a router pod that the SSL_MIN_VERSION, ROUTER_CIPHER and ROUTER_CIPHERS should be as same as Intermediate profile defined")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		env := readRouterPodEnv(oc, routerpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, routerpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384`))

		// OCP-80510 - [MicroShift] supporting Old tlsSecurityProfile for the ingress controller
		exutil.By("4.0: configure the config.yaml file with the Old tls profile")
		customConfig := fmt.Sprintf(`
cat >> /etc/microshift/config.yaml << EOF
ingress:
    tlsSecurityProfile:
        old: {}
        type: Old
EOF`)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		exutil.By("5.0: check the TLS env in a router pod that the SSL_MIN_VERSION, ROUTER_CIPHER and ROUTER_CIPHERS should be as same as Old profile defined")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.1`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA256:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA`))

		// OCP-80513 - [MicroShift] supporting Intermediate tlsSecurityProfile for the ingress controller
		exutil.By("6.0: Configure the config.yaml file with the Intermidiate tls profile")
		customConfig = fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.backup80510 ; then
    rm /etc/microshift/config.yaml -f
    cp /etc/microshift/config.yaml.backup80510 /etc/microshift/config.yaml
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    tlsSecurityProfile:
        intermediate: {}
        type: Intermediate
EOF`)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		exutil.By("7.0: Check TLS env in a router pod that the SSL_MIN_VERSION, ROUTER_CIPHER and ROUTER_CIPHERS should be as same as the default defined by the Intermediate profile")
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
		var e2eTestNamespace = "e2e-ne-80514-" + getRandomString()

		exutil.By("1.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		actualGenerationInt, _ := strconv.Atoi(actualGen)

		exutil.By("2.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		backupConfig := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup80514
else
    touch /etc/microshift/config.yaml.no80514
fi
`)
		_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", backupConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no80514; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no80514
elif test -f /etc/microshift/config.yaml.backup80514 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup80514 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup80514
fi
`)
		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()

		// OCP-80514 - [MicroShift] supporting Modern tlsSecurityProfile for the ingress controller
		exutil.By("3.0: Configure the config.yaml file with the Modern profile")
		customConfig := fmt.Sprintf(`
rm /etc/microshift/config.yaml -f
if test -f /etc/microshift/config.yaml.backup80514 ; then
    cp /etc/microshift/config.yaml.backup80514 /etc/microshift/config.yaml
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    tlsSecurityProfile:
        modern: {}
        type: Modern
EOF`)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		exutil.By("4.0: Check TLS env in a router pod that the SSL_MIN_VERSION and ROUTER_CIPHERSUITES should be as same as the Modern profile defined")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		env := readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.3`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHERSUITES")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))

		exutil.By("5.0: Check the haproxy config on the router pod to ensure the ssl version TLSv1.3 is reflected")
		tlsVersion := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "ssl-min-ver")
		o.Expect(tlsVersion).To(o.ContainSubstring(`ssl-default-bind-options ssl-min-ver TLSv1.3`))

		// OCP-80516 - [MicroShift] supporting Custom tlsSecurityProfile for the ingress controller
		exutil.By("6.0: Configure the config.yaml file with the Custom profile")
		customConfig = fmt.Sprintf(`
rm /etc/microshift/config.yaml -f
if test -f /etc/microshift/config.yaml.backup80514 ; then
    cp /etc/microshift/config.yaml.backup80514 /etc/microshift/config.yaml
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    tlsSecurityProfile:
        custom:
          ciphers:
            - DHE-RSA-AES256-GCM-SHA384
            - ECDHE-ECDSA-AES256-GCM-SHA384
          minTLSVersion: VersionTLS12
        type: Custom
EOF`)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		exutil.By("7.0: Check TLS env in a router pod that the SSL_MIN_VERSION and ROUTER_CIPHER should be as same as the Custom profile defined")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES256-GCM-SHA384`))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-High-80518-mTLS supporting client certificate with the subject filter [Disruptive]", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod-withprivilege.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			e2eTestNamespace    = "e2e-ne-80518-" + getRandomString()
			baseDomain          = "apps.example.com"
			dirname             = "/tmp/OCP-80518-ca"
			caSubj              = "/CN=MS-Test-Root-CA"
			caCrt               = dirname + "/80518-ca.crt"
			caKey               = dirname + "/80518-ca.key"
			userSubj            = "/CN=example-test.com"
			usrCrt              = dirname + "/80518-usr.crt"
			usrKey              = dirname + "/80518-usr.key"
			usrCsr              = dirname + "/80518-usr.csr"
			userSubj2           = "/CN=example-test2.com"
			usrCrt2             = dirname + "/80518-usr2.crt"
			usrKey2             = dirname + "/80518-usr2.key"
			usrCsr2             = dirname + "/80518-usr2.csr"
			cmName              = "ocp80518"
			filter              = userSubj
			edgeRoute           = "route-edge80518." + baseDomain
			edgeRoute2          = "route2-edge80518." + baseDomain
		)

		exutil.By("1.0: Use openssl to create custom client certification, create a new self-signed CA including the ca certification and ca key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslNewCa(caKey, caCrt, caSubj)

		exutil.By("1.1: Create a user CSR and the user key for a client")
		opensslNewCsr(usrKey, usrCsr, userSubj)

		exutil.By("1.2: Sign the user CSR and generate the user certificate")
		san := "subjectAltName = DNS.1:*." + baseDomain + ",DNS.2:" + edgeRoute
		opensslSignCsr(san, usrCsr, caCrt, caKey, usrCrt)

		exutil.By("1.3: Create another user CSR and the user key for the client")
		opensslNewCsr(usrKey2, usrCsr2, userSubj2)

		exutil.By("1.4: Sign the another user CSR and generate the user certificate")
		san = "subjectAltName = DNS.1:*." + baseDomain + ",DNS.2:" + edgeRoute2
		opensslSignCsr(san, usrCsr2, caCrt, caKey, usrCrt2)

		exutil.By("2.0: Create a cm with date ca certification")
		defer deleteConfigMap(oc, "openshift-ingress", cmName)
		createConfigMapFromFile(oc, "openshift-ingress", cmName, "ca-bundle.pem="+caCrt)

		exutil.By("3.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		actualGenerationInt, _ := strconv.Atoi(actualGen)

		exutil.By("4.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		backupConfig := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup80518
else
    touch /etc/microshift/config.yaml.no80518
fi
`)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", backupConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no80518; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no80518
elif test -f /etc/microshift/config.yaml.backup80518 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup80518 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup80518
fi
`)
		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()

		exutil.By("5.0: Debug node and configure clientTLS with allowedSubjectPatterns in the config.yaml for permitting the first route")
		customConfig := fmt.Sprintf(`
rm /etc/microshift/config.yaml -f
if test -f /etc/microshift/config.yaml.backup80514 ; then
    cp /etc/microshift/config.yaml.backup80517 /etc/microshift/config.yaml
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    clientTLS:
        allowedSubjectPatterns: ["%s"]
        clientCA:
            name: "%s"
        clientCertificatePolicy: "Required"
EOF`, filter, cmName)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		exutil.By("6.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", e2eTestNamespace, dirname, e2eTestNamespace+"/"+clientPodName+":"+dirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		exutil.By("7.0: Create two edge route for the testing, one mathing allowedSubjectPatterns of the clientTLS")
		jsonPath := "{.status.ingress[0].conditions[?(@.type==\"Admitted\")].status}"
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute, "--cert=" + usrCrt, "--key=" + usrKey})
		createRoute(oc, e2eTestNamespace, "edge", "route-edge2", unSecSvcName, []string{"--hostname=" + edgeRoute2, "--cert=" + usrCrt2, "--key=" + usrKey2})
		waitForOutput(oc, e2eTestNamespace, "route/route-edge", jsonPath, "True")
		waitForOutput(oc, e2eTestNamespace, "route/route-edge2", jsonPath, "True")

		exutil.By("8.0: Check the ROUTER_MUTUAL_TLS_AUTH env in a router pod")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		env := readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH_FILTER")
		o.Expect(env).To(o.ContainSubstring(filter))

		exutil.By("9.0: Curl the first edge route with the user certification, expect to get 200 OK for mathing the allowedSubjectPatterns of the clientTLS")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := edgeRoute + ":443:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-sI", "--cacert", caCrt, "--cert", usrCrt, "--key", usrKey, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		exutil.By("10.0: Curl the second edge route with the user2 certification, expect to get 403 Forbidden for not mathing the allowedSubjectPatterns of the clientTLS")
		toDst = edgeRoute2 + ":443:" + podIP
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute2, "-sI", "--cacert", caCrt, "--cert", usrCrt2, "--key", usrKey2, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "403 Forbidden", 60, 1)
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-High-80520-supporting wildcard routeAdmissionPolicy for the Ingress Controller [Disruptive]", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod-withprivilege.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			e2eTestNamespace    = "e2e-ne-80520-" + getRandomString()
			baseDomain          = "apps.example.com"
		)

		exutil.By("1.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)

		exutil.By("2.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		exutil.By("3.0: For the default WildcardsDisallowed wildcardPolicy of routeAdmission, check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be false")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		namespaceOwnershipEnv := readRouterPodEnv(oc, routerpod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=false"))

		exutil.By("4.0: Create a route with wildcard-policy Subdomain, which should Not be Admitted")
		routehost := "wildcard." + baseDomain
		anyhost := "any." + baseDomain
		jsonPath := "{.status.ingress[0].conditions[?(@.type==\"Admitted\")].status}"
		createRoute(oc, e2eTestNamespace, "http", "unsecure80520", unsecSvcName, []string{"--wildcard-policy=Subdomain", "--hostname=" + routehost})
		waitForOutput(oc, e2eTestNamespace, "route/unsecure80520", jsonPath, "False")

		exutil.By("5.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		backupConfig := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup80520
else
    touch /etc/microshift/config.yaml.no80520
fi
`)
		_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", backupConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no80520; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no80520
elif test -f /etc/microshift/config.yaml.backup80520 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup80520 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup80520
fi
`)
		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()

		exutil.By("6.0: Debug node to set WildcardsAllowed wildcardPolicy in the config.yaml file")
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		actualGenerationInt, _ := strconv.Atoi(actualGen)
		customConfig := fmt.Sprintf(`
cat >> /etc/microshift/config.yaml << EOF
ingress:
    routeAdmissionPolicy:
        wildcardPolicy: "WildcardsAllowed"
EOF`)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		exutil.By("7.0. Check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be true")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		namespaceOwnershipEnv = readRouterPodEnv(oc, routerpod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=true"))

		exutil.By("8.0: Curl the route with the two hostnames again, both should get 200 ok reponse")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + routehost, "-sI", "--resolve", toDst, "--connect-timeout", "10"}
		waitForOutput(oc, e2eTestNamespace, "route/unsecure80520", jsonPath, "True")
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		toDst = anyhost + ":80:" + podIP
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "http://" + anyhost, "-sI", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		exutil.By("9.0: Debug node to set WildcardsDisabllowed wildcardPolicy in the config.yaml file")
		customConfig = fmt.Sprintf(`
rm /etc/microshift/config.yaml -f
if test -f /etc/microshift/config.yaml.backup80520 ; then
    cp /etc/microshift/config.yaml.backup80520 /etc/microshift/config.yaml
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    routeAdmissionPolicy:
        wildcardPolicy: "WildcardsDisallowed"
EOF`)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		exutil.By("10.0: For the configured WildcardsDisallowed wildcardPolicy of routeAdmission, check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be false")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		namespaceOwnershipEnv = readRouterPodEnv(oc, routerpod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=false"))

		exutil.By(`11.0: Check the route's status, which should Not be Admitted`)
		waitForOutput(oc, e2eTestNamespace, "route/unsecure80520", jsonPath, "False")
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-MicroShiftOnly-NonPreRelease-High-80517-mTLS supporting client certificate with Optional or Required policy [Disruptive]", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod-withprivilege.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			e2eTestNamespace    = "e2e-ne-80517-" + getRandomString()
			baseDomain          = "apps.example.com"
			dirname             = "/tmp/OCP-80517-ca"
			caSubj              = "/CN=MS-Test-Root-CA"
			caCrt               = dirname + "/80517-ca.crt"
			caKey               = dirname + "/80517-ca.key"
			userSubj            = "/CN=example-test.com"
			usrCrt              = dirname + "/80517-usr.crt"
			usrKey              = dirname + "/80517-usr.key"
			usrCsr              = dirname + "/80517-usr.csr"
			cmName              = "ocp80517"
			edgeRoute           = "route-edge80517." + baseDomain
		)

		exutil.By("1.0: Use openssl to create custom client certification, create a new self-signed CA including the ca certification and ca key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslNewCa(caKey, caCrt, caSubj)

		exutil.By("1.1: Create a user CSR and the user key for a client")
		opensslNewCsr(usrKey, usrCsr, userSubj)

		exutil.By("1.2: Sign the user CSR and generate the user certificate")
		san := "subjectAltName = DNS.1:*." + baseDomain + ",DNS.2:" + edgeRoute
		opensslSignCsr(san, usrCsr, caCrt, caKey, usrCrt)

		exutil.By("2.0: Create a cm with date ca certification")
		defer deleteConfigMap(oc, "openshift-ingress", cmName)
		createConfigMapFromFile(oc, "openshift-ingress", cmName, "ca-bundle.pem="+caCrt)

		exutil.By("3.0: Prepare a namespace for the following testing")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace)
		exutil.SetNamespacePrivileged(oc, e2eTestNamespace)
		actualGen, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment/router-default", "-n", "openshift-ingress", "-o=jsonpath={.metadata.generation}").Output()
		actualGenerationInt, _ := strconv.Atoi(actualGen)

		exutil.By("4.0: Debug node to backup the config.yaml, and restore it before the test finishes running")
		nodeName := getByJsonPath(oc, "default", "nodes", "{.items[0].metadata.name}")
		backupConfig := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml ; then
    cp /etc/microshift/config.yaml /etc/microshift/config.yaml.backup80517
else
    touch /etc/microshift/config.yaml.no80517
fi
`)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", backupConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		recoverCmd := fmt.Sprintf(`
if test -f /etc/microshift/config.yaml.no80517; then
    rm -f /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.no80517
elif test -f /etc/microshift/config.yaml.backup80517 ; then
    rm -f /etc/microshift/config.yaml
    cp /etc/microshift/config.yaml.backup80517 /etc/microshift/config.yaml
    rm -f /etc/microshift/config.yaml.backup80517
fi
`)
		defer func() {
			_, err := oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", recoverCmd).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		}()

		exutil.By("5.0: Check the ROUTER_MUTUAL_TLS_AUTH env in a router pod which should be empty for the default clientCertificatePolicy")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		env := readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`NotFound`))

		exutil.By("6.0: Debug node and configure clientTLS with clientCertificatePolicy Required in the config.yaml")
		customConfig := fmt.Sprintf(`
rm /etc/microshift/config.yaml -f
if test -f /etc/microshift/config.yaml.backup80514 ; then
    cp /etc/microshift/config.yaml.backup80517 /etc/microshift/config.yaml
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    clientTLS:
        clientCA:
            name: "%s"
        clientCertificatePolicy: "Required"
EOF`, cmName)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+1))

		exutil.By("7.0: Create a client pod, a deployment and the services")
		createResourceFromFile(oc, e2eTestNamespace, clientPod)
		ensurePodWithLabelReady(oc, e2eTestNamespace, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", e2eTestNamespace, dirname, e2eTestNamespace+"/"+clientPodName+":"+dirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, e2eTestNamespace, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace, "name="+srvrcInfo)

		exutil.By("8.0: Create an edge route for the testing")
		jsonPath := "{.status.ingress[0].conditions[?(@.type==\"Admitted\")].status}"
		createRoute(oc, e2eTestNamespace, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute, "--cert=" + usrCrt, "--key=" + usrKey})
		waitForOutput(oc, e2eTestNamespace, "route/route-edge", jsonPath, "True")

		exutil.By("9.0: Check the ROUTER_MUTUAL_TLS_AUTH and ROUTER_MUTUAL_TLS_AUTH_CA envs in a router pod for the Required clientCertificatePolicy")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH=required`))
		env = readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH_CA")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH_CA=/etc/pki/tls/client-ca/ca-bundle.pem`))

		exutil.By("10.0: Curl the edge route with the user certification, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := edgeRoute + ":443:" + podIP
		curlCmd := []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-sI", "--cacert", caCrt, "--cert", usrCrt, "--key", usrKey, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		exutil.By("11.0: Curl the edge route without any certifications, expect to get SSL_read error")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-skv", "--resolve", toDst, "--connect-timeout", "10"}
		waitForErrorOccur(oc, curlCmd, "SSL_read: error", 60)

		exutil.By("12.0: Debug node and configure clientTLS with clientCertificatePolicy Optional in the config.yaml")
		customConfig = fmt.Sprintf(`
rm /etc/microshift/config.yaml -f
if test -f /etc/microshift/config.yaml.backup80514 ; then
    cp /etc/microshift/config.yaml.backup80517 /etc/microshift/config.yaml
fi
cat >> /etc/microshift/config.yaml << EOF
ingress:
    clientTLS:
        clientCA:
            name: "%s"
        clientCertificatePolicy: "Optional"
EOF`, cmName)
		_, err = oc.AsAdmin().WithoutNamespace().Run("debug").Args("-n", e2eTestNamespace, "--quiet=true", "node/"+nodeName, "--", "chroot", "/host", "bash", "-c", customConfig).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		restartMicroshiftService(oc, e2eTestNamespace, nodeName)
		ensureRouterDeployGenerationIs(oc, "default", strconv.Itoa(actualGenerationInt+2))

		exutil.By("13.0: Check the ROUTER_MUTUAL_TLS_AUTH env in a router pod for the Optional clientCertificatePolicy")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, "default")
		env = readRouterPodEnv(oc, routerpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH=optional`))

		exutil.By("14.0: Curl the edge route with the user certification, expect to get 200 OK")
		podIP = getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = edgeRoute + ":443:" + podIP
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-sI", "--cacert", caCrt, "--cert", usrCrt, "--key", usrKey, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)

		exutil.By("15.0: Curl the edge route without any certifications, expect to get 200 OK")
		curlCmd = []string{"-n", e2eTestNamespace, clientPodName, "--", "curl", "https://" + edgeRoute, "-skI", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
	})
})
