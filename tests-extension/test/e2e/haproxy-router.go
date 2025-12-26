package router

import (
	"github.com/openshift/router-tests-extension/test/testdata"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/tidwall/gjson"
	e2e "k8s.io/kubernetes/test/e2e/framework"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-env", compat_otp.KubeConfigPath())

	// incorporate OCP-15044, OCP-15049, OCP-15050 and OCP-15051 into one
	// Test case creater: hongli@redhat.com - OCP-15044 The backend health check interval of unsecure route can be set by annotation
	// Test case creater: hongli@redhat.com - OCP-15049 The backend health check interval of edge route can be set by annotation
	// Test case creater: hongli@redhat.com - OCP-15050 The backend health check interval of passthrough route can be set by annotation
	// Test case creater: hongli@redhat.com - OCP-15051 The backend health check interval of reencrypt route can be set by annotation
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-15044-The backend health check interval of unsecure/edge/passthrough/reencrypt route can be set by annotation", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
		)

		compat_otp.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		defaultContPod := getOneRouterPodNameByIC(oc, "default")
		// need two replicas of the server to test this scenario
		updateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2.0: Create an unsecure, edge, reencrypt and passthrough route")
		createRoute(oc, ns, "http", "route-http", unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", "default")
		createRoute(oc, ns, "edge", "route-edge", unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		createRoute(oc, ns, "passthrough", "route-pass", secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-pass", "default")
		createRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		compat_otp.By("3.0: Annotate unsecure, edge, reencrypt and passthrough route")
		setAnnotation(oc, ns, "route/route-http", `router.openshift.io/haproxy.health.check.interval=200ms`)
		setAnnotation(oc, ns, "route/route-edge", `router.openshift.io/haproxy.health.check.interval=300ms`)
		setAnnotation(oc, ns, "route/route-pass", `router.openshift.io/haproxy.health.check.interval=400ms`)
		setAnnotation(oc, ns, "route/route-reen", `router.openshift.io/haproxy.health.check.interval=500ms`)

		compat_otp.By("4. Check the router pod and ensure the routes are loaded in haproxy.config of default controller")
		ensureHaproxyBlockConfigContains(oc, defaultContPod, ns+":"+"route-http", []string{"check inter 200ms"})
		ensureHaproxyBlockConfigContains(oc, defaultContPod, ns+":"+"route-edge", []string{"check inter 300ms"})
		ensureHaproxyBlockConfigContains(oc, defaultContPod, ns+":"+"route-pass", []string{"check inter 400ms"})
		ensureHaproxyBlockConfigContains(oc, defaultContPod, ns+":"+"route-reen", []string{"check inter 500ms"})
	})

	// author: hongli@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-16870-No health check when there is only one endpoint for a route", func() {
		// skip the test if featureSet is set there
		if compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("Skip for not supporting DynamicConfigurationManager")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			deploymentName      = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: Create a single pod and the service")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		output, err := oc.Run("get").Args("service").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unSecSvcName))

		compat_otp.By("2.0: Create an unsecure route")
		createRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, unSecSvcName, "default")

		compat_otp.By("3.0: Check the haproxy.config that the health check should not exist in the backend server slots")
		jsonPath := fmt.Sprintf(`{.items[?(@.metadata.generateName=="%s-")].metadata.name}`, unSecSvcName)
		epSliceName := getByJsonPath(oc, ns, "EndpointSlice", jsonPath)
		jsonPath = "{.endpoints[0].addresses[0]}"
		epIP := getByJsonPath(oc, ns, "EndpointSlice/"+epSliceName, jsonPath)
		routerpod := getOneRouterPodNameByIC(oc, "default")
		backendConfig := ensureHaproxyBlockConfigContains(oc, routerpod, "be_http:"+ns+":"+unSecSvcName, []string{epIP})
		o.Expect(backendConfig).NotTo(o.ContainSubstring("check inter"))

		compat_otp.By("4.0: Scale up the deployment with replicas 2, then check the haproxy.config that the health check should exist in the backend server slots")
		scaleDeploy(oc, ns, deploymentName, 2)
		backendConfig = ensureHaproxyBlockConfigMatchRegexp(oc, routerpod, "be_http:"+ns+":"+unSecSvcName, []string{epIP + ".+check inter"})
		o.Expect(strings.Count(backendConfig, "check inter") == 2).To(o.BeTrue())
	})

	// Test case creater: zzhao@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-16872-Health check when there are multi service and each service has one backend", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
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
			unsecureRouteName = "16872"
		)

		compat_otp.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.namespace = ns
		webServerDeploy2.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.create(oc)
		ensurePodWithLabelReady(oc, ns, deploy1Label)
		ensurePodWithLabelReady(oc, ns, deploy2Label)
		routerPod := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("1. Create unsecure route and set route-backends with multi serivces")
		createRoute(oc, ns, "http", unsecureRouteName, webServerDeploy1.svcUnsecureName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, unsecureRouteName, "default")
		// Confirm there is no check inter in the haproxy.config before setting route-backends with multi services.
		httpSearchOutput := ensureHaproxyBlockConfigContains(oc, routerPod, ns+":"+unsecureRouteName, []string{unsecureRouteName})
		o.Expect(httpSearchOutput).NotTo(o.ContainSubstring("check inter 500ms"))
		// Note: the "balance roundrobin" is used for the route once set route-backends, no need to annotate the route"
		err := oc.Run("set").Args("route-backends", unsecureRouteName, "service-unsecure1=20", "service-unsecure2=80").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("2. Check unsecure route health check interval in haproxy.config")
		ensureHaproxyBlockConfigMatchRegexp(oc, routerPod, "be_http:"+ns+":"+unsecureRouteName, []string{"server pod:" + webServerDeploy1.deployName + ".+check inter 5000ms", "server pod:" + webServerDeploy2.deployName + ".+check inter 5000ms"})
	})

	// author: shudili@redhat.com
	// Includes OCP-21766: Integrate router metrics with the monitoring component
	//          OCP-10903: The router pod should have default resource limits
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-21766-misc tests for haproxy router", func() {
		var (
			namespace          = "openshift-ingress"
			servicemonitorName = "router-default"
			rolebindingName    = "prometheus-k8s"
		)
		compat_otp.By(fmt.Sprintf("Check whether servicemonitor %s exists or not", servicemonitorName))
		jsonPath := "{.items[*].metadata.name}"
		servicemonitorList := getByJsonPath(oc, namespace, "servicemonitor", jsonPath)
		o.Expect(servicemonitorList).To(o.ContainSubstring(servicemonitorName))

		compat_otp.By(fmt.Sprintf("Check whether rolebinding prometheus-k8s exists or not", rolebindingName))
		rolebindingList := getByJsonPath(oc, namespace, "rolebinding", jsonPath)
		o.Expect(rolebindingList).To(o.ContainSubstring(rolebindingName))

		compat_otp.By(fmt.Sprintf("check the openshift.io/cluster-monitoring label of the namespace %s, which should be true", namespace))
		jsonPath = `{.metadata.labels.openshift\.io/cluster-monitoring}`
		value := getByJsonPath(oc, "default", "namespace/"+namespace, jsonPath)
		o.Expect(value).To(o.ContainSubstring("true"))

		// OCP-10903
		compat_otp.By("check the default resources limits of the router container in the router-default deployment")
		jsonPath = `{.spec.template.spec.containers[?(@.name=="router")].resources.requests.cpu}{.spec.template.spec.containers[?(@.name=="router")].resources.requests.memory}`
		resources := getByJsonPath(oc, "openshift-ingress", "deployments/router-default", jsonPath)
		o.Expect(resources).To(o.ContainSubstring("100m256Mi"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-27628-router support HTTP2 for passthrough route and reencrypt route with custom certs", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			podDirname          = "/data/OCP-27628-ca"
			podCaCrt            = podDirname + "/27628-ca.crt"
			podUsrCrt           = podDirname + "/27628-usr.crt"
			podUsrKey           = podDirname + "/27628-usr.key"
			dirname             = "/tmp/OCP-27628-ca"
			caSubj              = "/CN=NE-Test-Root-CA"
			caCrt               = dirname + "/27628-ca.crt"
			caKey               = dirname + "/27628-ca.key"
			userSubj            = "/CN=example-ne.com"
			usrCrt              = dirname + "/27628-usr.crt"
			usrKey              = dirname + "/27628-usr.key"
			usrCsr              = dirname + "/27628-usr.csr"
			cmName              = "ocp27628"
		)

		// enabled mTLS for http/2 traffic testing, if not, the frontend haproxy will use http/1.1
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    clientTLS:
      clientCA:
        name: %s
      clientCertificatePolicy: Required
`, cmName)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp27628",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Get the domain info for testing")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "reen27628" + "." + ingctrl.domain

		compat_otp.By("2.0: Start to use openssl to create ca certification&key and user certification&key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("2.1: Create a new self-signed CA including the ca certification and ca key")
		opensslNewCa(caKey, caCrt, caSubj)

		compat_otp.By("2.2: Create a user CSR and the user key for the reen route")
		opensslNewCsr(usrKey, usrCsr, userSubj)

		compat_otp.By("2.3: Sign the user CSR and generate the certificate for the reen route")
		san := "subjectAltName = DNS:*." + ingctrl.domain
		opensslSignCsr(san, usrCsr, caCrt, caKey, usrCrt)

		compat_otp.By("3.0: create a cm with date ca certification, then create the custom ingresscontroller")
		defer deleteConfigMap(oc, "openshift-config", cmName)
		createConfigMapFromFile(oc, "openshift-config", cmName, "ca-bundle.pem="+caCrt)

		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("4.0: enable http2 on the custom ingresscontroller by the annotation if env ROUTER_DISABLE_HTTP2 is true")
		jsonPath := "{.spec.template.spec.containers[0].env[?(@.name==\"ROUTER_DISABLE_HTTP2\")].value}"
		envValue := getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.name, jsonPath)
		if envValue == "true" {
			setAnnotationAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `ingress.operator.openshift.io/default-enable-http2=true`)
			ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		}

		compat_otp.By("5.0 Create a deployment and a client pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, dirname, ns+"/"+clientPodName+":"+podDirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("6.0 Create a reencrypt route and a passthrough route inside the namespace")
		createRoute(oc, ns, "reencrypt", "route-reen", svcName, []string{"--hostname=" + routehost, "--ca-cert=" + caCrt, "--cert=" + usrCrt, "--key=" + usrKey})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")
		routehost2 := "pass27628" + "." + ingctrl.domain
		createRoute(oc, ns, "passthrough", "route-pass", svcName, []string{"--hostname=" + routehost2})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-pass", "default")

		compat_otp.By("7.0 Check the cert_config.map for the reencypt route with custom cert")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "cat cert_config.map").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`route-reen.pem [alpn h2,http/1.1] ` + routehost))

		compat_otp.By("8.0 Curl the reencrypt route with specified protocol http2")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--http2", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "HTTP/2 200", 60, 1)

		compat_otp.By("9.0 Curl the pass route with specified protocol http2")
		toDst = routehost2 + ":443:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost2, "-skI", "--http2", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "HTTP/2 200", 60, 1)
	})

	// author: hongli@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-37714-Ingresscontroller routes traffic only to ready pods/backends", func() {
		// skip the test if featureSet is set there
		if compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("Skip for DCM enabled, the haproxy has the dynamic server slot configration for the only one endpoint, not the static")
		}

		// if the ingress canary route isn't accessable from outside, skip it
		if !isCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			unSecSvcName        = "service-unsecure"
		)

		compat_otp.By("1.0: updated the deployment file with readinessProbe")
		extraParas := fmt.Sprintf(`
          readinessProbe:
            exec:
              command:
              - cat
              - /data/ready
            initialDelaySeconds: 5
            periodSeconds: 5
`)
		updatedDeployFile := addContenToFileWithMatchedOrder(testPodSvc, "        - name: nginx", extraParas, 1)
		defer os.Remove(updatedDeployFile)

		compat_otp.By("2.0 Create a deployment and a client pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, updatedDeployFile)
		waitForOutputEquals(oc, ns, "deployment/web-server-deploy", "{.status.replicas}", "1", 180*time.Second)
		serverPod := getPodListByLabel(oc, ns, "name=web-server-deploy")[0]
		waitForOutputEquals(oc, ns, "pod/"+serverPod, "{.status.phase}", "Running")
		waitForOutputEquals(oc, ns, "pod/"+serverPod, "{.status.conditions[?(@.type==\"Ready\")].status}", "False")

		compat_otp.By("3.0 Create a http route")
		routehost := "unsecure37714" + ".apps." + getBaseDomain(oc)
		createRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, unSecSvcName, "default")

		compat_otp.By("4.0 Check haproxy.config which should not have the server slot")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		serverSlot := "pod:" + serverPod + ":" + unSecSvcName + ":http"
		backendConfig := ensureHaproxyBlockConfigContains(oc, routerpod, "be_http:"+ns+":"+unSecSvcName, []string{unSecSvcName})
		o.Expect(backendConfig).NotTo(o.ContainSubstring(serverSlot))

		compat_otp.By("5.0 Curl the http route, expect to get 503 for the server pod is not ready")
		curlCmd := fmt.Sprintf(`curl http://%s -sI --connect-timeout 10`, routehost)
		repeatCmdOnClient(oc, curlCmd, "503", 60, 1)

		compat_otp.By("6.0 Create the /data/ready under the server pod")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, serverPod, "--", "touch", "/data/ready").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, ns, "pod/"+serverPod, "{.status.conditions[?(@.type==\"Ready\")].status}", "True")

		compat_otp.By("7.0 Check haproxy.config which should have the server slot")
		ensureHaproxyBlockConfigContains(oc, routerpod, "be_http:"+ns+":"+unSecSvcName, []string{serverSlot})

		compat_otp.By("8.0 Curl the http route again, expect to get 200 ok for the server pod is ready")
		repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Critical-40675-Ingresscontroller with endpointPublishingStrategy of hostNetwork allows PROXY protocol for source forwarding [Flaky]", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-hn-PROXY.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp40675",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("check whether there are more than two worker nodes present for testing hostnetwork")
		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount <= 2 {
			g.Skip("Skipping as we need more than two worker nodes")
		}

		compat_otp.By("Create a hostNetwork ingresscontroller with PROXY protocol set")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Check the router env to verify the PROXY variable is applied")
		routername := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		pollReadPodData(oc, "openshift-ingress", routername, "/usr/bin/env", `ROUTER_USE_PROXY_PROTOCOL=true`)
	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Critical-40677-Ingresscontroller with endpointPublishingStrategy of nodePort allows PROXY protocol for source forwarding", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np-PROXY.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp40677",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a NP ingresscontroller with PROXY protocol set")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Check the router env to verify the PROXY variable is applied")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		dssearch := readRouterPodEnv(oc, podname, "ROUTER_USE_PROXY_PROTOCOL")
		o.Expect(dssearch).To(o.ContainSubstring(`ROUTER_USE_PROXY_PROTOCOL=true`))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Medium-40679-The endpointPublishingStrategy parameter allow TCP/PROXY/empty definition for HostNetwork or NodePort type strategies", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hn-PROXY.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp40679",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		compat_otp.By("check whether there are more than two worker nodes present for testing hostnetwork")
		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount <= 2 {
			g.Skip("Skipping as we need more than two worker nodes")
		}

		compat_otp.By("Create a hostNetwork ingresscontroller with protocol PROXY set by the template")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Check the router env to verify the PROXY variable is applied")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		pollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_USE_PROXY_PROTOCOL=true`)

		compat_otp.By("Patch the hostNetwork ingresscontroller with protocol TCP")
		patchPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"protocol\": \"TCP\"}}}}"
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Check the configuration and router env for protocol TCP")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		cmd := fmt.Sprintf("/usr/bin/env | grep %s", `ROUTER_USE_PROXY_PROTOCOL`)
		jsonPath := "{.spec.endpointPublishingStrategy.hostNetwork.protocol}"
		output := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jsonPath)
		o.Expect(output).To(o.ContainSubstring("TCP"))
		err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ingctrl.namespace, routerpod, "--", "bash", "-c", cmd).Execute()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("Patch the hostNetwork ingresscontroller with protocol empty")
		patchPath = `{"spec":{"endpointPublishingStrategy":{"hostNetwork":{"protocol": ""}}}}`
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)

		compat_otp.By("Check the configuration and router env for protocol empty")
		output = getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jsonPath)
		o.Expect(output).To(o.BeEmpty())
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ingctrl.namespace, routerpod, "--", "bash", "-c", cmd).Execute()
		o.Expect(err).To(o.HaveOccurred())
	})

	g.It("Author:aiyengar-ROSA-OSD_CCS-ARO-LEVEL0-High-41042-The Power-of-two balancing features defaults to random LB algorithm instead of leastconn for REEN/Edge/insecure routes", func() {
		var (
			baseDomain   = getBaseDomain(oc)
			defaultPod   = getOneRouterPodNameByIC(oc, "default")
			unsecsvcName = "service-unsecure"
			secsvcName   = "service-secure"
		)
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		addSvc := filepath.Join(buildPruningBaseDir, "svc-additional-backend.yaml")

		compat_otp.By("Create pods and service resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		createResourceFromFile(oc, ns, addSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a edge/insecure/REEN/passthrough type routes via the services inside the namespace")
		edgeRoute := "route-edge" + "-" + ns + "." + baseDomain
		reenRoute := "route-reen" + "-" + ns + "." + baseDomain
		createRoute(oc, ns, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRoute})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))
		createRoute(oc, ns, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRoute})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-reen"))
		createRoute(oc, ns, "http", unsecsvcName, unsecsvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("Check the default loadbalance algorithm inside proxy pod")
		edgeBackend := "be_edge_http:" + ns + ":route-edge"
		reenBackend := "be_secure:" + ns + ":route-reen"
		insecBackend := "be_http:" + ns + ":service-unsecure"
		ensureHaproxyBlockConfigContains(oc, defaultPod, edgeBackend, []string{"balance random"})
		ensureHaproxyBlockConfigContains(oc, defaultPod, reenBackend, []string{"balance random"})
		ensureHaproxyBlockConfigContains(oc, defaultPod, insecBackend, []string{"balance random"})
	})

	g.It("Author:aiyengar-ROSA-OSD_CCS-ARO-Critical-41186-The Power-of-two balancing features switches to roundrobin mode for REEN/Edge/insecure/passthrough routes with multiple backends configured with weights", func() {
		var (
			baseDomain   = getBaseDomain(oc)
			defaultPod   = getOneRouterPodNameByIC(oc, "default")
			unsecsvcName = "service-unsecure"
			secsvcName   = "service-secure"
		)
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		addSvc := filepath.Join(buildPruningBaseDir, "svc-additional-backend.yaml")

		compat_otp.By("Create pods and service resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		createResourceFromFile(oc, ns, addSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a edge/insecure/REEN/passthrough type routes via the services inside the namespace")
		edgeRoute := "route-edge" + "-" + ns + "." + baseDomain
		reenRoute := "route-reen" + "-" + ns + "." + baseDomain
		passthRoute := "route-passth" + "-" + ns + "." + baseDomain
		createRoute(oc, ns, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRoute})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))
		createRoute(oc, ns, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRoute})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-reen"))
		createRoute(oc, ns, "passthrough", "route-passth", unsecsvcName, []string{"--hostname=" + passthRoute})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-passth"))
		createRoute(oc, ns, "http", unsecsvcName, unsecsvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("Check the default loadbalance algorithm inside proxy pod")
		edgeBackend := "be_edge_http:" + ns + ":route-edge"
		reenBackend := "be_secure:" + ns + ":route-reen"
		insecBackend := "be_http:" + ns + ":service-unsecure"
		ensureHaproxyBlockConfigContains(oc, defaultPod, edgeBackend, []string{"balance random"})
		ensureHaproxyBlockConfigContains(oc, defaultPod, reenBackend, []string{"balance random"})
		ensureHaproxyBlockConfigContains(oc, defaultPod, insecBackend, []string{"balance random"})

		compat_otp.By("Add service as weighted backend to the routes and check the balancing algorithm value")
		passthBackend := "be_tcp:" + ns + ":route-passth"
		_, edgerr := oc.Run("set").WithoutNamespace().Args("route-backends", "route-edge", "service-unsecure1=100", "service-unsecure2=150").Output()
		o.Expect(edgerr).NotTo(o.HaveOccurred())
		_, reenerr := oc.Run("set").WithoutNamespace().Args("route-backends", "route-reen", "service-secure1=100", "service-secure2=150").Output()
		o.Expect(reenerr).NotTo(o.HaveOccurred())
		_, passtherr := oc.Run("set").WithoutNamespace().Args("route-backends", "route-passth", "service-secure1=100", "service-secure2=150").Output()
		o.Expect(passtherr).NotTo(o.HaveOccurred())
		_, insecerr := oc.Run("set").WithoutNamespace().Args("route-backends", "service-unsecure", "service-unsecure1=100", "service-unsecure2=150").Output()
		o.Expect(insecerr).NotTo(o.HaveOccurred())
		ensureHaproxyBlockConfigContains(oc, defaultPod, edgeBackend, []string{"balance roundrobin"})
		ensureHaproxyBlockConfigContains(oc, defaultPod, reenBackend, []string{"balance roundrobin"})
		ensureHaproxyBlockConfigContains(oc, defaultPod, insecBackend, []string{"balance roundrobin"})
		ensureHaproxyBlockConfigContains(oc, defaultPod, passthBackend, []string{"balance roundrobin"})
	})

	g.It("Author:aiyengar-High-41187-The Power of two balancing  honours the per route balancing algorithm defined via haproxy.router.openshift.io/balance annotation", func() {
		var (
			defaultPod   = getOneRouterPodNameByIC(oc, "default")
			unsecsvcName = "service-unsecure"
		)
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")

		compat_otp.By("Create pods and service resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a route from the namespace and set route LB annotation")
		createRoute(oc, ns, "http", unsecsvcName, unsecsvcName, []string{})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unsecsvcName))
		setAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/balance=leastconn")
		findAnnotation, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", unsecsvcName, "-n", ns, "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		getAlgoValue := gjson.Get(string(findAnnotation), "haproxy\\.router\\.openshift\\.io/balance").String()
		o.Expect(getAlgoValue).To(o.ContainSubstring("leastconn"))

		compat_otp.By("Check the default loadbalance algorithm inside proxy pod and check the default LB variable to confirm power-of-two is active")
		insecBackend := "be_http:" + ns + ":service-unsecure"
		rtrParamCheck := readPodEnv(oc, defaultPod, "openshift-ingress", "ROUTER_LOAD_BALANCE_ALGORITHM")
		o.Expect(rtrParamCheck).To(o.ContainSubstring("random"))
		ensureHaproxyBlockConfigContains(oc, defaultPod, insecBackend, []string{"balance leastconn"})
	})

	g.It("Author:aiyengar-High-41206-Power-of-two feature allows unsupportedConfigOverrides ingress operator option to enable leastconn balancing algorithm", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "41206",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Patch ingresscontroller with unsupportedConfigOverrides option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"unsupportedConfigOverrides\":{\"loadBalancingAlgorithm\":\"leastconn\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("verify ROUTER_LOAD_BALANCE_ALGORITHM variable of the deployed router pod")
		checkenv := readRouterPodEnv(oc, newrouterpod, "ROUTER_LOAD_BALANCE_ALGORITHM")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_LOAD_BALANCE_ALGORITHM=leastconn`))

		compat_otp.By("deploy pod resource and expose a route via the ingresscontroller")
		ns := oc.Namespace()
		edgeRoute := "route-edge" + "-" + ns + "." + ingctrl.domain
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		createRoute(oc, ns, "edge", "route-edge", "service-unsecure", []string{"--hostname=" + edgeRoute})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Check the router config for the default LB algorithm set at the backend")
		edgeBackend := "be_edge_http:" + ns + ":route-edge"
		ensureHaproxyBlockConfigContains(oc, newrouterpod, edgeBackend, []string{"balance leastconn"})
	})

	g.It("Author:mjoseph-High-41929-Haproxy router continues to function normally with the service selector of exposed route gets removed/deleted", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp41929",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("Deploy a backend pod and its service resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "service-unsecure-" + ns + "." + ingctrl.domain
		SrvErr := oc.Run("expose").Args("svc/service-unsecure", "--hostname="+routehost).Execute()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		routeOutput := getRoutes(oc, ns)
		o.Expect(routeOutput).To(o.ContainSubstring("service-unsecure"))

		compat_otp.By("Cross check the selector value of the 'service-unsecure' service")
		jpath := "{.spec.selector}"
		output := getByJsonPath(oc, ns, "svc/service-unsecure", jpath)
		o.Expect(output).To(o.ContainSubstring(`"name":"web-server-deploy"`))

		compat_otp.By("Delete the service selector for the 'service-unsecure' service")
		patchPath := `{"spec":{"selector":null}}`
		patchResourceAsAdmin(oc, ns, "svc/service-unsecure", patchPath)

		compat_otp.By("Check the service config to confirm the value of the selector is empty")
		output = getByJsonPath(oc, ns, "svc/service-unsecure", jpath)
		o.Expect(output).To(o.BeEmpty())

		compat_otp.By("Check the router pod logs and confirm there is no reload error message")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", custContPod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Contains(log, "error reloading router") {
			e2e.Failf("Router reloaded after removing service selector")
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Medium-42878-Errorfile stanzas and dummy default html files have been added to the router", func() {
		compat_otp.By("Get pod (router) in openshift-ingress namespace")
		podname := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("Check if there are default 404 and 503 error pages on the router")
		searchOutput := readRouterPodData(oc, podname, "ls -l", "error-page")
		o.Expect(searchOutput).To(o.ContainSubstring(`error-page-404.http`))
		o.Expect(searchOutput).To(o.ContainSubstring(`error-page-503.http`))

		compat_otp.By("Check if errorfile stanzas have been added into haproxy-config.template")
		searchOutput = readRouterPodData(oc, podname, "cat haproxy-config.template", "errorfile")
		o.Expect(searchOutput).To(o.ContainSubstring(`ROUTER_ERRORFILE_404`))
		o.Expect(searchOutput).To(o.ContainSubstring(`ROUTER_ERRORFILE_503`))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Medium-42940-User can customize HAProxy 2.0 Error Page", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			http404page         = filepath.Join(buildPruningBaseDir, "error-page-404.http")
			http503page         = filepath.Join(buildPruningBaseDir, "error-page-503.http")
			cmName              = "my-custom-error-code-pages-42940"
			patchHTTPErrorPage  = "{\"spec\": {\"httpErrorCodePages\": {\"name\": \"" + cmName + "\"}}}"
			ingctrl             = ingressControllerDescription{
				name:      "ocp42940",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		compat_otp.By("Create a ConfigMap with custom 404 and 503 error pages")
		cmCrtErr := oc.AsAdmin().WithoutNamespace().Run("create").Args("configmap", cmName, "--from-file="+http404page, "--from-file="+http503page, "-n", "openshift-config").Execute()
		o.Expect(cmCrtErr).NotTo(o.HaveOccurred())
		defer deleteConfigMap(oc, "openshift-config", cmName)
		cmOutput, cmErr := oc.WithoutNamespace().AsAdmin().Run("get").Args("configmap", "-n", "openshift-config").Output()
		o.Expect(cmErr).NotTo(o.HaveOccurred())
		o.Expect(cmOutput).To(o.ContainSubstring(cmName))
		cmOutput, cmErr = oc.WithoutNamespace().AsAdmin().Run("get").Args("configmap", cmName, "-o=jsonpath={.data}", "-n", "openshift-config").Output()
		o.Expect(cmErr).NotTo(o.HaveOccurred())
		o.Expect(cmOutput).To(o.ContainSubstring("error-page-404.http"))
		o.Expect(cmOutput).To(o.ContainSubstring("Custom error page:The requested document was not found"))
		o.Expect(cmOutput).To(o.ContainSubstring("error-page-503.http"))
		o.Expect(cmOutput).To(o.ContainSubstring("Custom error page:The requested application is not available"))

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("patch the custom ingresscontroller with the http error code pages")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchHTTPErrorPage)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("get one custom ingress-controller router pod's IP")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP := getPodv4Address(oc, podname, "openshift-ingress")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		compat_otp.By("create a client pod")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		compat_otp.By("create an unsecure service and its backend pod")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("Expose an route with the unsecure service inside the namespace")
		routehost := srvName + "-" + ns + "." + ingctrl.domain
		srvErr := oc.Run("expose").Args("service", srvName, "--hostname="+routehost).Execute()
		o.Expect(srvErr).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, ns, "route", "{.items[0].metadata.name}", srvName)

		compat_otp.By("curl a normal route from the client pod")
		routestring := srvName + "-" + ns + "." + ingctrl.name + "."
		waitForCurl(oc, clientPodName, baseDomain, routestring, "200 OK", podIP)

		compat_otp.By("curl a non-existing route, expect to get custom http 404 Not Found error")
		notExistRoute := "notexistroute" + "-" + ns + "." + ingctrl.domain
		toDst := routehost + ":80:" + podIP
		toDst2 := notExistRoute + ":80:" + podIP
		output, errCurlRoute := oc.Run("exec").Args(clientPodName, "--", "curl", "-v", "http://"+notExistRoute, "--resolve", toDst2, "--connect-timeout", "10").Output()
		o.Expect(errCurlRoute).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("404 Not Found"))
		o.Expect(output).To(o.ContainSubstring("Custom error page:The requested document was not found"))

		compat_otp.By("delete the backend pod and try to curl the route, expect to get custom http 503 Service Unavailable")
		podname, err := oc.Run("get").Args("pods", "-l", "name="+srvrcInfo, "-o=jsonpath={.items[0].metadata.name}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.Run("delete").Args("deployment", srvrcInfo).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, ns, "pod/"+podname)
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+podname))
		output, err = oc.Run("exec").Args(clientPodName, "--", "curl", "-v", "http://"+routehost, "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("503 Service Unavailable"))
		o.Expect(output).To(o.ContainSubstring("Custom error page:The requested application is not available"))
	})

	// author: jechen@redhat.com
	g.It("Author:jechen-High-43115-Configmap mounted on router volume after ingresscontroller has spec field HttpErrorCodePage populated with configmap name", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43115",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("2.  Configure a customized error page configmap from files in openshift-config namespace")
		configmapName := "custom-43115-error-code-pages"
		cmFile1 := filepath.Join(buildPruningBaseDir, "error-page-503.http")
		cmFile2 := filepath.Join(buildPruningBaseDir, "error-page-404.http")
		_, error := oc.AsAdmin().WithoutNamespace().Run("create").Args("configmap", configmapName, "--from-file="+cmFile1, "--from-file="+cmFile2, "-n", "openshift-config").Output()
		o.Expect(error).NotTo(o.HaveOccurred())
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("configmap", configmapName, "-n", "openshift-config").Output()

		compat_otp.By("3. Check if configmap is successfully configured in openshift-config namesapce")
		err := checkConfigMap(oc, "openshift-config", configmapName)
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("cm %v not found", configmapName))

		compat_otp.By("4. Patch the configmap created above to the custom ingresscontroller in openshift-ingress namespace")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"httpErrorCodePages\":{\"name\":\"custom-43115-error-code-pages\"}}}")

		compat_otp.By("5. Check if configmap is successfully patched into openshift-ingress namesapce, configmap with name ingctrl.name-errorpages should be created")
		expectedCmName := ingctrl.name + `-errorpages`
		err = checkConfigMap(oc, "openshift-ingress", expectedCmName)
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("cm %v not found", expectedCmName))

		compat_otp.By("6. Obtain new router pod created, and check if error_code_pages directory is created on it")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("Check /var/lib/haproxy/conf directory to see if error_code_pages subdirectory is created on the router")
		searchOutput := readRouterPodData(oc, newrouterpod, "ls -al /var/lib/haproxy/conf", "error_code_pages")
		o.Expect(searchOutput).To(o.ContainSubstring(`error_code_pages`))

		compat_otp.By("7. Check if custom error code pages have been mounted")
		searchOutput = readRouterPodData(oc, newrouterpod, "ls -al /var/lib/haproxy/conf/error_code_pages", "error")
		o.Expect(searchOutput).To(o.ContainSubstring(`error-page-503.http -> ..data/error-page-503.http`))
		o.Expect(searchOutput).To(o.ContainSubstring(`error-page-404.http -> ..data/error-page-404.http`))

		searchOutput = readRouterPodData(oc, newrouterpod, "cat /var/lib/haproxy/conf/error_code_pages/error-page-503.http", "Unavailable")
		o.Expect(searchOutput).To(o.ContainSubstring(`HTTP/1.0 503 Service Unavailable`))
		o.Expect(searchOutput).To(o.ContainSubstring(`Custom:Application Unavailable`))

		searchOutput = readRouterPodData(oc, newrouterpod, "cat /var/lib/haproxy/conf/error_code_pages/error-page-404.http", "Not Found")
		o.Expect(searchOutput).To(o.ContainSubstring(`HTTP/1.0 404 Not Found`))
		o.Expect(searchOutput).To(o.ContainSubstring(`Custom:Not Found`))

	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Medium-43292-User can delete configmap and update configmap with new custom error page", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			http404page         = filepath.Join(buildPruningBaseDir, "error-page-404.http")
			http503page         = filepath.Join(buildPruningBaseDir, "error-page-503.http")
			http404page2        = filepath.Join(buildPruningBaseDir, "error-page2-404.http")
			http503page2        = filepath.Join(buildPruningBaseDir, "error-page2-503.http")
			cmName              = "my-custom-error-code-pages-43292"
			patchHTTPErrorPage  = "{\"spec\": {\"httpErrorCodePages\": {\"name\": \"" + cmName + "\"}}}"
			rmHTTPErrorPage     = "{\"spec\": {\"httpErrorCodePages\": {\"name\": \"\"}}}"
			ingctrl             = ingressControllerDescription{
				name:      "ocp43292",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		compat_otp.By("Create a ConfigMap with custom 404 and 503 error pages")
		defer deleteConfigMap(oc, "openshift-config", cmName)
		cmCrtErr := oc.AsAdmin().WithoutNamespace().Run("create").Args("configmap", cmName, "--from-file="+http404page, "--from-file="+http503page, "-n", "openshift-config").Execute()
		o.Expect(cmCrtErr).NotTo(o.HaveOccurred())
		cmOutput, cmErr := oc.WithoutNamespace().AsAdmin().Run("get").Args("configmap", cmName, "-o=jsonpath={.data}", "-n", "openshift-config").Output()
		o.Expect(cmErr).NotTo(o.HaveOccurred())
		o.Expect(cmOutput).Should(o.And(
			o.ContainSubstring("error-page-404.http"),
			o.ContainSubstring("Custom error page:The requested document was not found"),
			o.ContainSubstring("error-page-503.http"),
			o.ContainSubstring("Custom error page:The requested application is not available")))

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("patch the custom ingresscontroller with the http error code pages")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchHTTPErrorPage)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("get one custom ingress-controller router pod's IP")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP := getPodv4Address(oc, podname, "openshift-ingress")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		compat_otp.By("create a client pod")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		compat_otp.By("create an unsecure service and its backend pod")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("Expose an route with the unsecure service inside the namespace")
		routehost := srvName + "-" + ns + "." + ingctrl.domain
		toDst := routehost + ":80:" + podIP
		output, SrvErr := oc.Run("expose").Args("service", srvName, "--hostname="+routehost).Output()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(srvName))
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-I", "http://" + routehost, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, cmdOnPod, "200", 60, 1)

		compat_otp.By("curl a non-existing route, expect to get custom http 404 Not Found error")
		notExistRoute := "notexistroute" + "-" + ns + "." + ingctrl.domain
		toDst = notExistRoute + ":80:" + podIP
		output, err := oc.Run("exec").Args("-n", ns, clientPodName, "--", "curl", "-v", "http://"+notExistRoute, "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).Should(o.And(
			o.ContainSubstring("404 Not Found"),
			o.ContainSubstring("Custom error page:The requested document was not found")))

		compat_otp.By("remove the custom error page from the ingress-controller")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, rmHTTPErrorPage)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")
		getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("delete the configmap")
		cmDltErr := oc.AsAdmin().WithoutNamespace().Run("delete").Args("configmap", cmName, "-n", "openshift-config").Execute()
		o.Expect(cmDltErr).NotTo(o.HaveOccurred())

		compat_otp.By("Create the ConfigMap with another 404 and 503 error pages")
		cmCrtErr = oc.AsAdmin().WithoutNamespace().Run("create").Args("configmap", cmName, "--from-file="+http404page2, "--from-file="+http503page2, "-n", "openshift-config").Execute()
		o.Expect(cmCrtErr).NotTo(o.HaveOccurred())
		cmOutput, cmErr = oc.WithoutNamespace().AsAdmin().Run("get").Args("configmap", cmName, "-o=jsonpath={.data}", "-n", "openshift-config").Output()
		o.Expect(cmErr).NotTo(o.HaveOccurred())
		o.Expect(cmOutput).Should(o.And(
			o.ContainSubstring("error-page2-404.http"),
			o.ContainSubstring("Custom error page:THE REQUESTED DOCUMENT WAS NOT FOUND YET!"),
			o.ContainSubstring("error-page2-503.http"),
			o.ContainSubstring("Custom error page:THE REQUESTED APPLICATION IS NOT AVAILABLE YET!")))

		// the following test step will be added after bug 1990020 is fixed(https://bugzilla.redhat.com/show_bug.cgi?id=1990020)
		// compat_otp.By("curl the non-existing route, expect to get the new custom http 404 Not Found error")
		// output, err = oc.Run("exec").Args(clientPodName, "--", "curl", "-v", "http://"+notExistRoute, "--resolve", toDst).Output()
		// o.Expect(err).NotTo(o.HaveOccurred())
		// o.Expect(output).Should(o.And(
		// o.ContainSubstring("404 Not Found"),
		// o.ContainSubstring("Custom error page:Custom error page:THE REQUESTED DOCUMENT WAS NOT FOUND YET!")))

	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Critical-43414-The logEmptyRequests ingresscontroller parameter set to Ignore add the dontlognull option in the haproxy configuration", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "43414",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Patch ingresscontroller with logEmptyRequests set to Ignore option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"type\":\"Container\"},\"logEmptyRequests\":\"Ignore\"}}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("verify the Dontlog variable inside the  router pod")
		checkenv := readRouterPodEnv(oc, newrouterpod, "ROUTER_DONT_LOG_NULL")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_DONT_LOG_NULL=true`))

		compat_otp.By("Verify the parameter set in the haproxy configuration of the router pod")
		checkoutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newrouterpod, "--", "bash", "-c", `cat haproxy.config | grep -w "dontlognull"`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(checkoutput).To(o.ContainSubstring(`option dontlognull`))

	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Critical-43416-httpEmptyRequestsPolicy ingresscontroller parameter set to ignore adds the http-ignore-probes option in the haproxy configuration", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "43416",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Patch ingresscontroller with logEmptyRequests set to Ignore option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"httpEmptyRequestsPolicy\":\"Ignore\"}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		compat_otp.By("verify the Dontlog variable inside the  router pod")
		checkenv := readRouterPodEnv(oc, newrouterpod, "ROUTER_HTTP_IGNORE_PROBES")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_HTTP_IGNORE_PROBES=true`))

		compat_otp.By("Verify the parameter set in the haproxy configuration of the router pod")
		checkoutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newrouterpod, "--", "bash", "-c", `cat haproxy.config | grep -w "http-ignore-probes"`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(checkoutput).To(o.ContainSubstring(`option http-ignore-probes`))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-43454-The logEmptyRequests option only gets applied when the access logging is configured for the ingresscontroller", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "43454",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("check the default .spec.logging")
		jpath := "{.spec.logging}"
		logging := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jpath)
		o.Expect(logging).To(o.ContainSubstring(""))

		compat_otp.By("patch the custom ingresscontroller with .spec.logging.access.destination.container")
		patchPath := `{"spec":{"logging":{"access":{"destination":{"type":"Container"}}}}}`
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check the .spec.logging")
		logging = getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jpath)
		expLogStr := "\"logEmptyRequests\":\"Log\""
		o.Expect(logging).To(o.ContainSubstring(expLogStr))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-High-46571-Setting ROUTER_ENABLE_COMPRESSION and ROUTER_COMPRESSION_MIME in HAProxy", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "46571",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Patch ingresscontroller with httpCompression option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.name
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"httpCompression\":{\"mimeTypes\":[\"text/html\",\"text/css; charset=utf-8\",\"application/json\"]}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("check the env variable of the router pod")
		checkenv1 := readRouterPodEnv(oc, newrouterpod, "ROUTER_ENABLE_COMPRESSION")
		o.Expect(checkenv1).To(o.ContainSubstring(`ROUTER_ENABLE_COMPRESSION=true`))
		checkenv2 := readRouterPodEnv(oc, newrouterpod, "ROUTER_COMPRESSION_MIME")
		o.Expect(checkenv2).To(o.ContainSubstring(`ROUTER_COMPRESSION_MIME=text/html "text/css; charset=utf-8" application/json`))

		compat_otp.By("check the haproxy config on the router pod for compression algorithm")
		algo := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "compression")
		o.Expect(algo).To(o.ContainSubstring(`compression algo gzip`))
		o.Expect(algo).To(o.ContainSubstring(`compression type text/html "text/css; charset=utf-8" application/json`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Low-46898-Setting wrong data in ROUTER_ENABLE_COMPRESSION and ROUTER_COMPRESSION_MIME in HAProxy", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "46898",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("Patch ingresscontroller with wrong httpCompression data and check whether it is configurable")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/46898", "-p", "{\"spec\":{\"httpCompression\":{\"mimeTypes\":[\"text/\",\"text/css; charset=utf-8\",\"//\"]}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"text/\": spec.httpCompression.mimeTypes[0] in body should match"))
		o.Expect(output).To(o.ContainSubstring("application|audio|image|message|multipart|text|video"))

		compat_otp.By("check the env variable of the router pod")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "/usr/bin/env | grep ROUTER_ENABLE_COMPRESSION").Output()
		o.Expect(output1).NotTo(o.ContainSubstring(`ROUTER_ENABLE_COMPRESSION=true`))

		compat_otp.By("check the haproxy config on the router pod for compression algorithm")
		output2, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "cat haproxy.config | grep compression").Output()
		o.Expect(output2).NotTo(o.ContainSubstring(`compression algo gzip`))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-LEVEL0-Critical-47344-check haproxy router v4v6 mode", func() {
		compat_otp.By("Get ROUTER_IP_V4_V6_MODE env, if NotFound then v4 is using by default")
		defaultRouterPod := getOneRouterPodNameByIC(oc, "default")
		checkEnv := readRouterPodEnv(oc, defaultRouterPod, "ROUTER_IP_V4_V6_MODE")
		ipStackType := checkIPStackType(oc)
		e2e.Logf("the cluster IP stack type is: %v", ipStackType)
		if ipStackType == "ipv6single" {
			o.Expect(checkEnv).To(o.ContainSubstring("=v6"))
		} else if ipStackType == "dualstack" {
			o.Expect(checkEnv).To(o.ContainSubstring("=v4v6"))
		} else {
			o.Expect(checkEnv).To(o.ContainSubstring("NotFound"))
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-49131-check haproxy's version and router base image", func() {
		var expVersion = "haproxy28-2.8.10-1.rhaos4.21.el9"
		compat_otp.By("Try to get HAProxy's version in a default router pod")
		haproxyVer := getHAProxyRPMVersion(oc)
		compat_otp.By("show haproxy version(" + haproxyVer + "), and check if it is updated successfully")
		o.Expect(haproxyVer).To(o.ContainSubstring(expVersion))
		// in 4.16, OCP-73373 - Bump openshift-router image to RHEL9"
		routerpod := getOneRouterPodNameByIC(oc, "default")
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "cat /etc/redhat-release").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Red Hat Enterprise Linux release 9"))
		// added OCP-75905([OCPBUGS-33900] [OCPBUGS-32369] HAProxy shouldn't consume high cpu usage) in 4.14+
		output2, err2 := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "rpm -qa haproxy28 --changelog | grep -A2 OCPBUGS-32369").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
		o.Expect(output2).Should(o.And(
			o.ContainSubstring(`Resolve https://issues.redhat.com/browse/OCPBUGS-32369`),
			o.ContainSubstring(`Carry fix for https://github.com/haproxy/haproxy/issues/2537`),
			o.ContainSubstring(`Fix for issue 2537 picked from https://git.haproxy.org/?p=haproxy.git;a=commit;h=4a9e3e102e192b9efd17e3241a6cc659afb7e7dc`)))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-50074-Allow Ingress to be modified on the settings of livenessProbe and readinessProbe", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		timeout5 := "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"router\",\"livenessProbe\":{\"timeoutSeconds\":5},\"readinessProbe\":{\"timeoutSeconds\":5}}]}}}}"
		timeoutmax := "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"router\",\"livenessProbe\":{\"timeoutSeconds\":2147483647},\"readinessProbe\":{\"timeoutSeconds\":2147483647}}]}}}}"
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp50074",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("check the default liveness probe and readiness probe parameters in the json outut of the router deployment")
		routerDeploymentName := "router-" + ingctrl.name
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..livenessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":1"))
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..readinessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":1"))

		compat_otp.By("patch livenessProbe and readinessProbe with 5s to the router deployment")
		_, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("deployment", routerDeploymentName, "--type=strategic", "--patch="+timeout5, "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check liveness probe and readiness probe 5s in the json output of the router deployment")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..livenessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":5"))
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..readinessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":5"))

		compat_otp.By("patch livenessProbe and readinessProbe with max 2147483647s to the router deployment")
		_, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("deployment", routerDeploymentName, "--type=strategic", "--patch="+timeoutmax, "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		compat_otp.By("check liveness probe and readiness probe max 2147483647s in the json output of the router deployment")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..livenessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":2147483647"))
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..readinessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":2147483647"))

		compat_otp.By("check liveness probe and readiness probe max 2147483647s in the json output of the router pod")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podname, "-o=jsonpath={..livenessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":2147483647"))
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podname, "-o=jsonpath={..readinessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":2147483647"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Low-50075-Negative test of allow Ingress to be modified on the settings of livenessProbe and readinessProbe", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		timeoutMinus := "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"router\",\"livenessProbe\":{\"timeoutSeconds\":-1},\"readinessProbe\":{\"timeoutSeconds\":-1}}]}}}}"
		timeoutString := "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"router\",\"livenessProbe\":{\"timeoutSeconds\":\"abc\"},\"readinessProbe\":{\"timeoutSeconds\":\"abc\"}}]}}}}"
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp50075",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("try to patch livenessProbe and readinessProbe with a minus number -1 to the router deployment")
		routerDeploymentName := "router-" + ingctrl.name
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("deployment", routerDeploymentName, "--type=strategic", "--patch="+timeoutMinus, "-n", "openshift-ingress").Output()
		o.Expect(output).To(o.ContainSubstring("spec.template.spec.containers[0].livenessProbe.timeoutSeconds: Invalid value: -1: must be greater than or equal to 0"))
		o.Expect(output).To(o.ContainSubstring("spec.template.spec.containers[0].readinessProbe.timeoutSeconds: Invalid value: -1: must be greater than or equal to 0"))

		compat_otp.By("try to patch livenessProbe and readinessProbe with string type of value to the router deployment")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args("deployment", routerDeploymentName, "--type=strategic", "--patch="+timeoutString, "-n", "openshift-ingress").Output()
		o.Expect(output).To(o.ContainSubstring("The request is invalid: patch: Invalid value: \"map[spec:map[template:map[spec:map[containers:[map[livenessProbe:map[timeoutSeconds:abc] name:router readinessProbe:map[timeoutSeconds:abc]]]]]]]\": unrecognized type: int32"))
	})

	g.It("Author:shudili-High-50405-Multiple routers with hostnetwork endpoint strategy can be deployed on same worker node with different http/https/stat port numbers", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")
			ingctrlhp1          = ingctrlHostPortDescription{
				name:      "ocp50405one",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  10080,
				httpsport: 10443,
				statsport: 10936,
				template:  customTemp,
			}

			ingctrlhp2 = ingctrlHostPortDescription{
				name:      "ocp50405two",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  11080,
				httpsport: 11443,
				statsport: 11936,
				template:  customTemp,
			}
			ingctrlResource1 = "ingresscontrollers/" + ingctrlhp1.name
			ingctrlResource2 = "ingresscontrollers/" + ingctrlhp2.name
			ns               = "openshift-ingress"
		)

		compat_otp.By("Pre-flight check for the platform type and number of worker nodes in the environment")
		platformtype := compat_otp.CheckPlatform(oc)
		platforms := map[string]bool{
			// ‘None’ also for Baremetal
			"none":      true,
			"baremetal": true,
			"vsphere":   true,
			"openstack": true,
			"nutanix":   true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-supported platform")
		}
		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we at least need one worker node")
		}

		compat_otp.By("Collect nodename of one of the default haproxy pods")
		defRouterPod := getOneRouterPodNameByIC(oc, "default")
		defNodeName := getNodeNameByPod(oc, ns, defRouterPod)

		compat_otp.By("Create two custom ingresscontrollers")
		baseDomain := getBaseDomain(oc)
		ingctrlhp1.domain = ingctrlhp1.name + "." + baseDomain
		ingctrlhp2.domain = ingctrlhp2.name + "." + baseDomain

		defer ingctrlhp1.delete(oc)
		ingctrlhp1.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrlhp1.name, "1")

		defer ingctrlhp2.delete(oc)
		ingctrlhp2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrlhp2.name, "1")

		compat_otp.By("Patch the two custom ingress-controllers with nodePlacement")
		patchSelectNode := "{\"spec\":{\"nodePlacement\":{\"nodeSelector\":{\"matchLabels\":{\"kubernetes.io/hostname\": \"" + defNodeName + "\"}}}}}"
		err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource1, "-p", patchSelectNode, "--type=merge", "-n", ingctrlhp1.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource2, "-p", patchSelectNode, "--type=merge", "-n", ingctrlhp2.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureRouterDeployGenerationIs(oc, ingctrlhp1.name, "2")
		ensureRouterDeployGenerationIs(oc, ingctrlhp2.name, "2")

		compat_otp.By("Check the node names on which the route pods of the custom ingress-controllers reside on")
		routerPod1 := getOneNewRouterPodFromRollingUpdate(oc, ingctrlhp1.name)
		routerPod2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrlhp2.name)
		routerNodeName1 := getNodeNameByPod(oc, ns, routerPod1)
		routerNodeName2 := getNodeNameByPod(oc, ns, routerPod2)
		o.Expect(defNodeName).Should(o.And(
			o.ContainSubstring(routerNodeName1),
			o.ContainSubstring(routerNodeName2)))

		compat_otp.By("Verify the http/https/statsport of the custom proxy pod")
		checkPodEnv := describePodResource(oc, routerPod1, "openshift-ingress")
		o.Expect(checkPodEnv).Should(o.And(
			o.ContainSubstring("ROUTER_SERVICE_HTTPS_PORT:                 10443"),
			o.ContainSubstring("ROUTER_SERVICE_HTTP_PORT:                  10080"),
			o.ContainSubstring("STATS_PORT:                                10936")))

	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Low-50406-The http/https/stat port field in the ingresscontroller does not accept negative values during configuration", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")
			ingctrlhp           = ingctrlHostPortDescription{
				name:      "ocp50406",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  10080,
				httpsport: 10443,
				statsport: 10936,
				template:  customTemp,
			}

			ingctrlResource = "ingresscontrollers/" + ingctrlhp.name
		)

		compat_otp.By("Pre-flight check for the platform type and number of worker nodes in the environment")
		platformtype := compat_otp.CheckPlatform(oc)
		platforms := map[string]bool{
			// ‘None’ also for Baremetal
			"none":      true,
			"baremetal": true,
			"vsphere":   true,
			"openstack": true,
			"nutanix":   true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-supported platform")
		}
		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we atleast need  one worker node")
		}

		compat_otp.By("Create a custom ingresscontrollers")
		baseDomain := getBaseDomain(oc)
		ingctrlhp.domain = ingctrlhp.name + "." + baseDomain
		defer ingctrlhp.delete(oc)
		ingctrlhp.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrlhp.name, "1")

		compat_otp.By("Patch the the custom ingress-controllers with invalid hostNetwork configutations")
		jsonPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"httpPort\": -10090}}}}"
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", jsonPath, "--type=merge", "-n", ingctrlhp.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: -10090"))

		jsonPath = "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"httpPort\": -11443}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", jsonPath, "--type=merge", "-n", ingctrlhp.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: -11443"))

		jsonPath = "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"httpPort\": -12936}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", jsonPath, "--type=merge", "-n", ingctrlhp.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: -12936"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-50819-Routers with hostnetwork endpoint strategy with same http/https/stat port numbers cannot be deployed on the same worker node", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")
			ingctrlhp1          = ingctrlHostPortDescription{
				name:      "ocp50819one",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  10080,
				httpsport: 10443,
				statsport: 10936,
				template:  customTemp,
			}

			ingctrlhp2 = ingctrlHostPortDescription{
				name:      "ocp50819two",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  10080,
				httpsport: 10433,
				statsport: 10936,
				template:  customTemp,
			}
		)

		compat_otp.By("Pre-flight check for the platform type and number of worker nodes in the environment")
		platformtype := compat_otp.CheckPlatform(oc)
		platforms := map[string]bool{
			// ‘None’ also for Baremetal
			"none":      true,
			"baremetal": true,
			"vsphere":   true,
			"openstack": true,
			"nutanix":   true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-supported platform")
		}
		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we atleast need  one worker node")
		}

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrlhp1.domain = ingctrlhp1.name + "." + baseDomain
		ingctrlhp2.domain = ingctrlhp2.name + "." + baseDomain

		defer ingctrlhp1.delete(oc)
		ingctrlhp1.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrlhp1.name, "1")

		compat_otp.By("Patch the first custom IC with max replicas, so each node has a custom router pod ")
		jpath := "{.status.readyReplicas}"
		if workerNodeCount > 1 {
			ingctrl1Resource := "ingresscontrollers/" + ingctrlhp1.name
			patchResourceAsAdmin(oc, ingctrlhp1.namespace, ingctrl1Resource, "{\"spec\":{\"replicas\":"+strconv.Itoa(workerNodeCount)+"}}")
			ensureRouterDeployGenerationIs(oc, ingctrlhp1.name, "2")
			waitForOutputEquals(oc, "openshift-ingress", "deployment/router-"+ingctrlhp1.name, jpath, strconv.Itoa(workerNodeCount))
		}

		compat_otp.By("Try to create another custom IC with the same http/https/stat port numbers as the first custom IC")
		defer ingctrlhp2.delete(oc)
		ingctrlhp2.create(oc)
		err := waitForPodWithLabelAppear(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller=ocp50819two")
		compat_otp.AssertWaitPollNoErr(err, "router pod of the second custom IC does not appear  within allowed time!")
		customICRouterPod := getPodListByLabel(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller=ocp50819two")
		checkPodMsg := getByJsonPath(oc, "openshift-ingress", "pod/"+customICRouterPod[0], "{.status..message}")
		o.Expect(checkPodMsg).To(o.ContainSubstring("node(s) didn't have free ports for the requested pod ports"))
	})

	g.It("Author:aiyengar-ROSA-OSD_CCS-ARO-High-52738-The Power-of-two balancing features switches to source algorithm for passthrough routes", func() {
		var (
			baseDomain = getBaseDomain(oc)
			defaultPod = getOneRouterPodNameByIC(oc, "default")
		)
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")

		compat_otp.By("Create pods and service resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a passthrough type routes via the services inside the namespace")
		passthRoute := "route-passth" + "-" + ns + "." + baseDomain
		createRoute(oc, ns, "passthrough", "route-passth", "service-secure", []string{"--hostname=" + passthRoute})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-passth"))

		compat_otp.By("Check the default loadbalance algorithm inside proxy pod and check the default LB variable to confirm power-of-two is active")
		rtrParamCheck := readPodEnv(oc, defaultPod, "openshift-ingress", "ROUTER_LOAD_BALANCE_ALGORITHM")
		o.Expect(rtrParamCheck).To(o.ContainSubstring("random"))
		passthBackend := "be_tcp:" + ns + ":route-passth"
		ensureHaproxyBlockConfigContains(oc, defaultPod, passthBackend, []string{"balance source"})
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Medium-53048-Ingresscontroller with private endpoint publishing strategy supports PROXY protocol", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-private.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "53048",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("check the default value of .status.endpointPublishingStrategy.private.protocol, which should be TCP")
		jpath := "{.status.endpointPublishingStrategy.private.protocol}"
		protocol := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring("TCP"))

		compat_otp.By("patch the custom ingresscontroller with protocol proxy")
		patchPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"private\":{\"protocol\":\"PROXY\"}}}}"
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check the changed value of .endpointPublishingStrategy.private.protocol, which should be PROXY")
		jpath = "{.spec.endpointPublishingStrategy.private.protocol}{.status.endpointPublishingStrategy.private.protocol}"
		protocol = getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring("PROXYPROXY"))

		compat_otp.By("check the custom ingresscontroller's status, which should indicate that PROXY protocol is enabled")
		jsonPath := "{.status.endpointPublishingStrategy}"
		status := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jsonPath)
		o.Expect(status).To(o.ContainSubstring(`{"private":{"protocol":"PROXY"},"type":"Private"}`))

		compat_otp.By("check the private deployment, which should have PROXY protocol enabled")
		jsonPath = `{.spec.template.spec.containers[0].env[?(@.name=="ROUTER_USE_PROXY_PROTOCOL")]}`
		proxyProtocol := getByJsonPath(oc, "openshift-ingress", "deployments/router-"+ingctrl.name, jsonPath)
		o.Expect(proxyProtocol).To(o.ContainSubstring(`{"name":"ROUTER_USE_PROXY_PROTOCOL","value":"true"}`))

		compat_otp.By("check the ROUTER_USE_PROXY_PROTOCOL env, which should be true")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		proxyEnv := readRouterPodEnv(oc, routerpod, "ROUTER_USE_PROXY_PROTOCOL")
		o.Expect(proxyEnv).To(o.ContainSubstring("ROUTER_USE_PROXY_PROTOCOL=true"))

		compat_otp.By("check the accept-proxy in haproxy.config of a router pod")
		bindCfg, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "cat haproxy.config | grep \"bind :\"").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		IPStackType := checkIPStackType(oc)
		if IPStackType == "ipv4single" {
			o.Expect(strings.Count(bindCfg, "bind :")).To(o.Equal(2))
			ensureHaproxyBlockConfigContains(oc, routerpod, "bind :80 accept-proxy", []string{"bind :80 accept-proxy"})
			ensureHaproxyBlockConfigContains(oc, routerpod, "bind :443 accept-proxy", []string{"bind :443 accept-proxy"})
		} else if IPStackType == "ipv6single" {
			o.Expect(strings.Count(bindCfg, "bind :")).To(o.Equal(2))
			ensureHaproxyBlockConfigContains(oc, routerpod, "bind :::80 v6only accept-proxy", []string{"bind :::80 v6only accept-proxy"})
			ensureHaproxyBlockConfigContains(oc, routerpod, "bind :::443 v6only accept-proxy", []string{"bind :::443 v6only accept-proxy"})
		} else if IPStackType == "dualstack" {
			o.Expect(strings.Count(bindCfg, "bind :")).To(o.Equal(4))
			ensureHaproxyBlockConfigContains(oc, routerpod, "bind :80 accept-proxy", []string{"bind :80 accept-proxy"})
			ensureHaproxyBlockConfigContains(oc, routerpod, "bind :443 accept-proxy", []string{"bind :443 accept-proxy"})
			ensureHaproxyBlockConfigContains(oc, routerpod, "bind :::80 v6only accept-proxy", []string{"bind :::80 v6only accept-proxy"})
			ensureHaproxyBlockConfigContains(oc, routerpod, "bind :::443 v6only accept-proxy", []string{"bind :::443 v6only accept-proxy"})
		}
	})

	// Bug: 2044682
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-54998-Set Cookie2 by an application in a route should not kill all router pods", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = ingressControllerDescription{
				name:      "54998",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("get one custom ingress-controller router pod's IP")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP := getPodv4Address(oc, podname, "openshift-ingress")

		compat_otp.By("create an unsecure service and its backend pod")
		ns := oc.Namespace()
		sedCmd := fmt.Sprintf(`sed -i'' -e 's/8080/10081/g' %s`, testPodSvc)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("start the service on the backend server port 10081 by socat command")
		jsonPath := "{.items[0].metadata.name}"
		srvPodName := getByJsonPath(oc, ns, "pods", jsonPath)
		cidr, errCidr := oc.AsAdmin().WithoutNamespace().Run("get").Args("network.config", "cluster", "-o=jsonpath={.spec.clusterNetwork[].cidr}").Output()
		o.Expect(errCidr).NotTo(o.HaveOccurred())
		// set ipv4 socat or ipv6 socat command on the server
		resWithSetCookie2 := `nohup socat -T 1 -6 -d -d tcp-l:10081,reuseaddr,fork,crlf system:'echo -e "\"HTTP/1.0 200 OK\nDocumentType: text/html\nHeader: Set-Cookie2 X=Y;\n\nthis is a test\""'`
		if strings.Contains(cidr, ".") {
			resWithSetCookie2 = `nohup socat -T 1 -d -d tcp-l:10081,reuseaddr,fork,crlf system:'echo -e "\"HTTP/1.0 200 OK\nDocumentType: text/html\nHeader: Set-Cookie2 X=Y;\n\nthis is a test\""'`
		}
		cmd1, _, _, errSetCookie2 := oc.AsAdmin().Run("exec").Args("-n", ns, srvPodName, "--", "bash", "-c", resWithSetCookie2).Background()
		defer cmd1.Process.Kill()
		o.Expect(errSetCookie2).NotTo(o.HaveOccurred())

		compat_otp.By("expose a route with the unsecure service inside the namespace")
		routehost := srvName + "-" + ns + "." + ingctrl.domain
		output, SrvErr := oc.Run("expose").Args("service", srvName, "--hostname="+routehost).Output()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(srvName))

		compat_otp.By("create a client pod to send traffic")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("curl the route from the client pod")
		toDst := routehost + ":80:" + podIP
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-I", "http://" + routehost, "--resolve", toDst, "--connect-timeout", "10"}
		result, _ := repeatCmdOnClient(oc, cmdOnPod, "Set-Cookie2 X=Y", 60, 1)
		o.Expect(result).To(o.ContainSubstring("Set-Cookie2 X=Y"))
	})

	// Bug: 1967228
	g.It("Author:shudili-High-55825-the 503 Error page should not contain license for a vulnerable release of Bootstrap", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = ingressControllerDescription{
				name:      "55825",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Create a client pod used to send traffic")
		ns := oc.Namespace()
		compat_otp.By("create a client pod")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("curl a non-existing route, and then check that Bootstrap portion of the license is removed")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP := getPodv4Address(oc, podname, "openshift-ingress")
		notExistRoute := "notexistroute" + "-" + ns + "." + ingctrl.domain
		toDst := notExistRoute + ":80:" + podIP
		output, err2 := oc.Run("exec").Args(clientPodName, "--", "curl", "-Iv", "http://"+notExistRoute, "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("503"))
		o.Expect(output).ShouldNot(o.And(
			o.ContainSubstring("Bootstrap"),
			o.ContainSubstring("Copyright 2011-2015 Twitter"),
			o.ContainSubstring("Licensed under MIT"),
			o.ContainSubstring("normalize.css v3.0.3")))

	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-56898-Accessing the route should wake up the idled resources", func() {
		// This test case works only on OVN or SDN network type
		networkType := compat_otp.CheckNetworkType(oc)
		if !(strings.Contains(networkType, "openshiftsdn") || strings.Contains(networkType, "ovn")) {
			g.Skip(fmt.Sprintf("Skipping because idling is not supported on the '%s' network type", networkType))
		}
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = ingressControllerDescription{
				name:      "ocp56898",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		custContIP := getPodv4Address(oc, custContPod, "openshift-ingress")

		compat_otp.By("Deploy a backend pod and its service resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Create a client pod")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "service-unsecure-" + ns + "." + ingctrl.domain
		SrvErr := oc.Run("expose").Args("svc/service-unsecure", "--hostname="+routehost).Execute()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		routeOutput := getRoutes(oc, ns)
		o.Expect(routeOutput).To(o.ContainSubstring("service-unsecure"))

		compat_otp.By("Check the router pod and ensure the routes are loaded in haproxy.config")
		haproxyOutput := pollReadPodData(oc, "openshift-ingress", custContPod, "cat haproxy.config", "service-unsecure")
		o.Expect(haproxyOutput).To(o.ContainSubstring("backend be_http:" + ns + ":service-unsecure"))

		compat_otp.By("Check the reachability of the insecure route")
		waitForCurl(oc, clientPodName, baseDomain, "service-unsecure-"+ns+"."+"ocp56898.", "HTTP/1.1 200 OK", custContIP)

		compat_otp.By("Idle the insecure service")
		idleOutput, err := oc.AsAdmin().WithoutNamespace().Run("idle").Args("service-unsecure", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(idleOutput).To(o.ContainSubstring("The service \"" + ns + "/service-unsecure\" has been marked as idled"))

		compat_otp.By("Verify the Idle annotation")
		findAnnotation := getAnnotation(oc, ns, "svc", "service-unsecure")
		o.Expect(findAnnotation).To(o.ContainSubstring("idling.alpha.openshift.io/idled-at"))
		o.Expect(findAnnotation).To(o.ContainSubstring(`idling.alpha.openshift.io/unidle-targets":"[{\"kind\":\"Deployment\",\"name\":\"web-server-deploy\",\"group\":\"apps\",\"replicas\":1}]`))

		compat_otp.By("Wake the Idle resource by accessing its route")
		waitForCurl(oc, clientPodName, baseDomain, "service-unsecure-"+ns+"."+"ocp56898.", "HTTP/1.1 200 OK", custContIP)

		compat_otp.By("Confirm the Idle annotation got removed")
		findAnnotation = getAnnotation(oc, ns, "svc", "service-unsecure")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring("idling.alpha.openshift.io/idled-at"))
	})

	// bug: 1826225
	g.It("Author:shudili-High-57001-edge terminated h2 (gRPC) connections need a haproxy template change to work correctly", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			srvPodSvc           = filepath.Join(buildPruningBaseDir, "bug1826225-proh2-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			svcName             = "service-h2c-57001"
			routeName           = "myedge1"
		)

		compat_otp.By("Create a backend pod and its service resources")
		ns := oc.Namespace()
		compat_otp.By("create a h2c service and its backend pod")
		createResourceFromFile(oc, ns, srvPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("Create an edge route with the h2c service inside the namespace")
		output, routeErr := oc.AsAdmin().WithoutNamespace().Run("create").Args("route", "edge", routeName, "--service="+svcName, "-n", ns).Output()
		o.Expect(routeErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(routeName))

		compat_otp.By("Check the Haproxy backend configuration and make sure proto h2 is added for the route")
		podname := getOneRouterPodNameByIC(oc, "default")
		backendConfig := pollReadPodData(oc, "openshift-ingress", podname, "cat haproxy.config", svcName)
		o.Expect(backendConfig).To(o.ContainSubstring("proto h2"))
	})

	// bugzilla: 1976894
	g.It("Author:mjoseph-Medium-57404-Idling StatefulSet is not supported", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "ocp57404-stateful-set.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp57404",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		custContIP := getPodv4Address(oc, custContPod, "openshift-ingress")

		compat_otp.By("Deploy the statefulset and its service resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "app=echoenv-sts")

		compat_otp.By("Create a client pod")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, "app=hello-pod")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "echoenv-statefulset-service-" + ns + "." + ingctrl.domain
		SrvErr := oc.Run("expose").Args("svc/echoenv-statefulset-service", "--hostname="+routehost).Execute()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		routeOutput := getRoutes(oc, ns)
		o.Expect(routeOutput).To(o.ContainSubstring("echoenv-statefulset-service"))

		compat_otp.By("Check the reachability of the insecure route")
		waitForCurl(oc, "hello-pod", baseDomain, "echoenv-statefulset-service-"+ns+"."+"ocp57404.", "HTTP/1.1 200 OK", custContIP)

		compat_otp.By("Trying to idle the statefulset-service")
		idleOutput, _ := oc.AsAdmin().WithoutNamespace().Run("idle").Args("echoenv-statefulset-service", "-n", ns).Output()
		o.Expect(idleOutput).To(o.ContainSubstring("idling StatefulSet is not supported yet"))

		compat_otp.By("Verify the Idle annotation is not present")
		findAnnotation := getAnnotation(oc, ns, "svc", "echoenv-statefulset-service")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring("idling.alpha.openshift.io/idled-at"))

		compat_otp.By("Recheck the reachability of the insecure route")
		waitForCurl(oc, "hello-pod", baseDomain, "echoenv-statefulset-service-"+ns+"."+"ocp57404.", "HTTP/1.1 200 OK", custContIP)
	})

	// bugzilla: 1941592 1859134
	g.It("Author:mjoseph-Medium-57406-HAProxyDown message only for pods and No reaper messages for zombie processes", func() {
		compat_otp.By("Verify there will be precise message pointing to the  router nod, when HAProxy is down")
		output := getByJsonPath(oc, "openshift-ingress-operator", "PrometheusRule", `{.items[0].spec.groups[0].rules[?(@.alert=="HAProxyDown")].annotations.message}`)
		o.Expect(output).To(o.ContainSubstring(`HAProxy metrics are reporting that HAProxy is down on pod {{ $labels.namespace }} / {{ $labels.pod }}`))

		compat_otp.By("Check the router pod logs and confirm there is no periodic reper error message  for zombie process")
		podname := getOneRouterPodNameByIC(oc, "default")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", podname).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Contains(log, "waitid: no child processes") {
			e2e.Failf("waitid: no child processes generated")
		}
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-59951-IngressController option to use PROXY protocol with IBM Cloud load-balancers - TCP, PROXY and omitted", func() {
		// This test case is only for IBM cluster
		compat_otp.SkipIfPlatformTypeNot(oc, "IBMCloud")
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-IBMproxy.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp59951",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("check the value of .status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol, which should be PROXY")
		jpath := "{.status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol}"
		protocol := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring("PROXY"))

		compat_otp.By("check the ROUTER_USE_PROXY_PROTOCOL env, which should be true")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		pollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_USE_PROXY_PROTOCOL=true`)

		compat_otp.By("Ensure the proxy-protocol annotation is added to the LB service")
		findAnnotation, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("svc", "router-ocp59951", "-n", "openshift-ingress", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(findAnnotation).To(o.ContainSubstring(`"service.kubernetes.io/ibm-load-balancer-cloud-provider-enable-features":"proxy-protocol"`))

		compat_otp.By("patch the custom ingresscontroller with protocol option TCP")
		patchPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"providerParameters\":{\"ibm\":{\"protocol\":\"TCP\"}}}}}}"
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)

		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		compat_otp.By("check the value of .status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol, which should be TCP")
		jpath = "{.status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol}"
		protocol = getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring("TCP"))

		compat_otp.By("check the ROUTER_USE_PROXY_PROTOCOL env, which should not present")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		proxyEnv, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "/usr/bin/env | grep ROUTER_USE_PROXY_PROTOCOL").Output()
		o.Expect(proxyEnv).NotTo(o.ContainSubstring("ROUTER_USE_PROXY_PROTOCOL"))

		compat_otp.By("patch the custom ingresscontroller with protocol option omitted")
		patchPath = `{"spec":{"endpointPublishingStrategy":{"loadBalancer":{"providerParameters":{"ibm":{"protocol":""}}}}}}`
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)

		compat_otp.By(`check the value of .status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol, which should be ""`)
		jpath = "{.status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm}"
		protocol = getByJsonPath(oc, ingctrl.namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring(`{}`))
	})

	// OCPBUGS-4573
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-62926-Ingress controller stats port is not set according to endpointPublishingStrategy", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")
			ingctrlhp           = ingctrlHostPortDescription{
				name:      "ocp62926",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  16080,
				httpsport: 16443,
				statsport: 16936,
				template:  customTemp,
			}

			ingctrlResource = "ingresscontrollers/" + ingctrlhp.name
		)

		compat_otp.By("Pre-flight check for the platform type and number of worker nodes in the environment")
		platformtype := compat_otp.CheckPlatform(oc)
		platforms := map[string]bool{
			// ‘None’ also for Baremetal
			"none":      true,
			"baremetal": true,
			"vsphere":   true,
			"openstack": true,
			"nutanix":   true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-supported platform")
		}
		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we atleast need one worker node")
		}

		compat_otp.By("Create a custom ingress-controller")
		baseDomain := getBaseDomain(oc)
		ingctrlhp.domain = ingctrlhp.name + "." + baseDomain
		defer ingctrlhp.delete(oc)
		ingctrlhp.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrlhp.name, "1")

		compat_otp.By("Patch the the custom ingress-controller with httpPort 17080, httpsPort 17443 and statsPort 17936")
		jsonPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"httpPort\":17080, \"httpsPort\":17443, \"statsPort\":17936}}}}"
		patchResourceAsAdmin(oc, ingctrlhp.namespace, ingctrlResource, jsonPath)
		ensureRouterDeployGenerationIs(oc, ingctrlhp.name, "2")

		compat_otp.By("Check STATS_PORT env under a custom router pod, which should be 17936")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrlhp.name)
		jsonPath = `{.spec.containers[].env[?(@.name=="STATS_PORT")].value}`
		output := getByJsonPath(oc, "openshift-ingress", "pod/"+routerpod, jsonPath)
		o.Expect(output).To(o.ContainSubstring("17936"))

		compat_otp.By("Check http/https/metrics ports under a custom router pod, which should be 17080/17443/17936")
		jsonPath = `{.spec.containers[].ports[?(@.name=="http")].hostPort}-{.spec.containers[].ports[?(@.name=="https")].hostPort}-{.spec.containers[].ports[?(@.name=="metrics")].hostPort}`
		output = getByJsonPath(oc, "openshift-ingress", "pod/"+routerpod, jsonPath)
		o.Expect(output).To(o.ContainSubstring("17080-17443-17936"))

		compat_otp.By("Check the custom router-internal service, make sure the targetPort of the metrics port is changed to metrics instead of port number 1936")
		jsonPath = `{.spec.ports[?(@.name=="metrics")].targetPort}`
		output = getByJsonPath(oc, "openshift-ingress", "service/router-internal-"+ingctrlhp.name, jsonPath)
		o.Expect(output).To(o.ContainSubstring("metrics"))

		compat_otp.By("Check http/https/metrics ports under the router endpoints, which should be 17080/17443/17936")
		jsonPath = `{.subsets[].ports[?(@.name=="http")].port}-{.subsets[].ports[?(@.name=="https")].port}-{.subsets[].ports[?(@.name=="metrics")].port}`
		output = getByJsonPath(oc, "openshift-ingress", "endpoints/router-internal-"+ingctrlhp.name, jsonPath)
		o.Expect(output).To(o.ContainSubstring("17080-17443-17936"))
	})

	// Test case creater: shudili@redhat.com
	g.It("Author:mjoseph-DEPRECATED-ROSA-OSD_CCS-ARO-High-77862-Check whether required ENV varibales are configured after enabling Dynamic Configuration Manager", func() {
		// skip the test if featureSet is not there
		if !compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}

		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		srvrcInfo := "web-server-deploy"
		srvName := "service-unsecure"
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp77862",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. Create a custom ingresscontroller, and get its router name")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		defaultPodname := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("2. Create an unsecure service and its backend pod")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3. Expose a route with the unsecure service inside the namespace")
		createRoute(oc, ns, "http", srvName, srvName, []string{})
		waitForOutputEquals(oc, ns, "route", "{.items[0].metadata.name}", srvName)

		compat_otp.By("4. Check the env variable of the default router pod")
		checkenv := readRouterPodEnv(oc, defaultPodname, "ROUTER")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_BLUEPRINT_ROUTE_POOL_SIZE=0`))
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_MAX_DYNAMIC_SERVERS=1`))
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_HAPROXY_CONFIG_MANAGER=true`))

		compat_otp.By("5. Check the env variable of the custom router pod")
		checkenv2 := readRouterPodEnv(oc, podname, "ROUTER")
		o.Expect(checkenv2).To(o.ContainSubstring(`ROUTER_BLUEPRINT_ROUTE_POOL_SIZE=0`))
		o.Expect(checkenv2).To(o.ContainSubstring(`ROUTER_MAX_DYNAMIC_SERVERS=1`))
		o.Expect(checkenv2).To(o.ContainSubstring(`ROUTER_HAPROXY_CONFIG_MANAGER=true`))

		compat_otp.By("6. Check the haproxy config on the router pod for dynamic pod config")
		insecBackend := "be_http:" + ns + ":service-unsecure"
		ensureHaproxyBlockConfigContains(oc, podname, insecBackend, []string{"dynamic-cookie-key", "server-template _dynamic-pod- 1-1 172.4.0.4:8765 check disabled"})
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-High-77892-Dynamic Configuration Manager for plain HTTP route [Serial]", func() {
		// skip the test if featureSet is not there
		if !compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = ingressControllerDescription{
				name:      "ocp77892",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "unsecure77892" + "." + ingctrl.domain
		defer func() {
			// added debug info, in case the original router pod was terminated
			routerpod2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
			e2e.Logf("Before end of testing, the routerpod is: %s", routerpod2)
			ingctrl.delete(oc)
		}()
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("2.0 Create a deployment, a HTTP route and a client pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		createRoute(oc, ns, "http", "unsecure77892", svcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsecure77892", "default")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Curl the HTTP route")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		e2e.Logf("init routerpod is: %s", routerpod)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-s", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4.0 Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
		backend := "be_http:" + ns + ":unsecure77892"
		checkDcmBackendCfg(oc, routerpod, backend)

		compat_otp.By("5.0 Use debug command to check the dynamic server's state")
		// used the socat command under the router pod to get all the route's endpoints status
		socatCmd := fmt.Sprintf(`echo "show servers state %s" | socat stdio /var/lib/haproxy/run/haproxy.sock | sed 1d | grep -v '^#' | cut -d ' ' -f2-6 | sed -e 's/0$/STOP/' -e 's/1$/STARTING/' -e 's/2$/UP/' -e 's/3$/SOFTSTOP/'`, backend)
		initSrvStates := checkDcmUpEndpoints(oc, routerpod, socatCmd, 1)
		currentSrvStates := ""

		compat_otp.By("6.0 get the initial router reloaded log")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		initReloadedNum := strings.Count(log, `"msg"="router reloaded" "logger"="template" "output"=`)

		compat_otp.By("7.0 keep scaling up the deployment with replicas 1")
		for i := 1; i < desiredReplicas; i++ {
			compat_otp.By("7." + strconv.Itoa(i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i+1))
			scaleDeploy(oc, ns, srvdmInfo, i+1)
			waitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(i+1))

			compat_otp.By("7." + strconv.Itoa(i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("7." + strconv.Itoa(i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i+1)

			compat_otp.By("7." + strconv.Itoa(i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("8.0 keep scaling down the deployment with replicas 1")
		for i := desiredReplicas - 1; i >= 0; i-- {
			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("9.0 keep scaling up the deployment with replicas 2")
		maxReplicas := 0
		for i := 2; i <= desiredReplicas-2; i = i + 2 {
			compat_otp.By("9." + strconv.Itoa((i+2)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
			maxReplicas = i
		}

		compat_otp.By("10.0 keep scaling down the deployment with replicas 2")
		for i := maxReplicas - 2; i >= 0; i = i - 2 {
			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".4: get the router reloaded log")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-High-77906-Enable ALPN for reencrypt routes when DCM is enabled", func() {
		// skip the test if featureSet is not there
		if !compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			podDirname          = "/data/OCP-77906-ca"
			podCaCrt            = podDirname + "/77906-ca.crt"
			podUsrCrt           = podDirname + "/77906-usr.crt"
			podUsrKey           = podDirname + "/77906-usr.key"
			dirname             = "/tmp/OCP-77906-ca"
			caSubj              = "/CN=NE-Test-Root-CA"
			caCrt               = dirname + "/77906-ca.crt"
			caKey               = dirname + "/77906-ca.key"
			userSubj            = "/CN=example-ne.com"
			usrCrt              = dirname + "/77906-usr.crt"
			usrKey              = dirname + "/77906-usr.key"
			usrCsr              = dirname + "/77906-usr.csr"
			cmName              = "ocp77906"
		)

		// enabled mTLS for http/2 traffic testing, if not, the frontend haproxy will use http/1.1
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    clientTLS:
      clientCA:
        name: %s
      clientCertificatePolicy: Required
`, cmName)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp77906",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Get the domain info for testing")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "reen77906" + "." + ingctrl.domain

		compat_otp.By("2.0: Start to use openssl to create ca certification&key and user certification&key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("2.1: Create a new self-signed CA including the ca certification and ca key")
		opensslNewCa(caKey, caCrt, caSubj)

		compat_otp.By("2.2: Create a user CSR and the user key")
		opensslNewCsr(usrKey, usrCsr, userSubj)

		compat_otp.By("2.3: Sign the user CSR and generate the certificate")
		san := "subjectAltName = DNS:*." + ingctrl.domain
		opensslSignCsr(san, usrCsr, caCrt, caKey, usrCrt)

		compat_otp.By("3.0: create a cm with date ca certification, then create the custom ingresscontroller")
		defer deleteConfigMap(oc, "openshift-config", cmName)
		createConfigMapFromFile(oc, "openshift-config", cmName, "ca-bundle.pem="+caCrt)

		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("4.0: enable http2 on the custom ingresscontroller by the annotation if env ROUTER_DISABLE_HTTP2 is true")
		jsonPath := "{.spec.template.spec.containers[0].env[?(@.name==\"ROUTER_DISABLE_HTTP2\")].value}"
		envValue := getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.name, jsonPath)
		if envValue == "true" {
			setAnnotationAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `ingress.operator.openshift.io/default-enable-http2=true`)
			ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		}

		compat_otp.By("5.0 Create a deployment and a client pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, dirname, ns+"/"+clientPodName+":"+podDirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("6.0 Create a reencrypt route inside the namespace")
		createRoute(oc, ns, "reencrypt", "route-reen", svcName, []string{"--hostname=" + routehost, "--ca-cert=" + caCrt, "--cert=" + usrCrt, "--key=" + usrKey})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		compat_otp.By("7.0 Check the haproxy.config, make sure alpn is enabled for the reencrypt route's backend endpoint")
		backend := "be_secure:" + ns + ":route-reen"
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, backend, []string{"ssl alpn h2,http/1.1"})

		compat_otp.By("8.0 Curl the reencrypt route with specified protocol http2")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--http2", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "HTTP/2 200", 60, 1)

		compat_otp.By("9.0 Curl the reencrypt route with specified protocol http1.1")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--http1.1", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "HTTP/1.1 200", 60, 1)
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-High-77973-Dynamic Configuration Manager for edge route [Serial]", func() {
		// skip the test if featureSet is not there
		if !compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = ingressControllerDescription{
				name:      "ocp77973",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "edge77973" + "." + ingctrl.domain
		defer func() {
			// added debug info, in case the original router pod was terminated
			routerpod2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
			e2e.Logf("Before end of testing, the routerpod is: %s", routerpod2)
			ingctrl.delete(oc)
		}()
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("2.0 Create a deployment, an edge route and a client pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		createRoute(oc, ns, "edge", "edge77973", svcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "edge77973", "default")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Curl the edge route")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		e2e.Logf("init routerpod is: %s", routerpod)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4.0 Check the route's backend configuration including server pod, dynamic pool and dynamic cookie")
		backend := "be_edge_http:" + ns + ":edge77973"
		checkDcmBackendCfg(oc, routerpod, backend)

		compat_otp.By("5.0 Use debug command to check the dynamic server's state")
		// used the socat command under the router pod to get all the route's endpoints status
		socatCmd := fmt.Sprintf(`echo "show servers state %s" | socat stdio /var/lib/haproxy/run/haproxy.sock | sed 1d | grep -v '^#' | cut -d ' ' -f2-6 | sed -e 's/0$/STOP/' -e 's/1$/STARTING/' -e 's/2$/UP/' -e 's/3$/SOFTSTOP/'`, backend)
		initSrvStates := checkDcmUpEndpoints(oc, routerpod, socatCmd, 1)
		currentSrvStates := ""

		compat_otp.By("6.0 get the initial router reloaded log")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		initReloadedNum := strings.Count(log, `"msg"="router reloaded" "logger"="template" "output"=`)

		compat_otp.By("7.0 keep scaling up the deployment with replicas 1")
		for i := 1; i < desiredReplicas; i++ {
			compat_otp.By("7." + strconv.Itoa(i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i+1))
			scaleDeploy(oc, ns, srvdmInfo, i+1)
			waitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(i+1))

			compat_otp.By("7." + strconv.Itoa(i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("7." + strconv.Itoa(i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i+1)

			compat_otp.By("7." + strconv.Itoa(i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("8.0 keep scaling down the deployment with replicas 1")
		for i := desiredReplicas - 1; i >= 0; i-- {
			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("9.0 keep scaling up the deployment with replicas 2")
		maxReplicas := 0
		for i := 2; i <= desiredReplicas-2; i = i + 2 {
			compat_otp.By("9." + strconv.Itoa((i+2)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
			maxReplicas = i
		}

		compat_otp.By("10.0 keep scaling down the deployment with replicas 2")
		for i := maxReplicas - 2; i >= 0; i = i - 2 {
			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".4: get the router reloaded log")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-High-77974-Dynamic Configuration Manager for passthrough route [Serial]", func() {
		// skip the test if featureSet is not there
		if !compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = ingressControllerDescription{
				name:      "ocp77974",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "passth77974" + "." + ingctrl.domain
		defer func() {
			// added debug info, in case the original router pod was terminated
			routerpod2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
			e2e.Logf("Before end of testing, the routerpod is: %s", routerpod2)
			ingctrl.delete(oc)
		}()
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("2.0 Create a deployment, a passthrough route and a client pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		createRoute(oc, ns, "passthrough", "passth77974", svcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "passth77974", "default")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Curl the passthrough route")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		e2e.Logf("init routerpod is: %s", routerpod)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4.0 Check the route's backend configuration including server pod and dynamic pool")
		backend := "be_tcp:" + ns + ":passth77974"
		checkDcmBackendCfg(oc, routerpod, backend)

		compat_otp.By("5.0 Use debug command to check the dynamic server's state")
		// used the socat command under the router pod to get all the route's endpoints status
		socatCmd := fmt.Sprintf(`echo "show servers state %s" | socat stdio /var/lib/haproxy/run/haproxy.sock | sed 1d | grep -v '^#' | cut -d ' ' -f2-6 | sed -e 's/0$/STOP/' -e 's/1$/STARTING/' -e 's/2$/UP/' -e 's/3$/SOFTSTOP/'`, backend)
		initSrvStates := checkDcmUpEndpoints(oc, routerpod, socatCmd, 1)
		currentSrvStates := ""

		compat_otp.By("6.0 get the initial router reloaded log")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		initReloadedNum := strings.Count(log, `"msg"="router reloaded" "logger"="template" "output"=`)

		compat_otp.By("7.0 keep scaling up the deployment with replicas 1")
		for i := 1; i < desiredReplicas; i++ {
			compat_otp.By("7." + strconv.Itoa(i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i+1))
			scaleDeploy(oc, ns, srvdmInfo, i+1)
			waitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(i+1))

			compat_otp.By("7." + strconv.Itoa(i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("7." + strconv.Itoa(i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i+1)

			compat_otp.By("7." + strconv.Itoa(i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("8.0 keep scaling down the deployment with replicas 1")
		for i := desiredReplicas - 1; i >= 0; i-- {
			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("9.0 keep scaling up the deployment with replicas 2")
		maxReplicas := 0
		for i := 2; i <= desiredReplicas-2; i = i + 2 {
			compat_otp.By("9." + strconv.Itoa((i+2)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
			maxReplicas = i
		}

		compat_otp.By("10.0 keep scaling down the deployment with replicas 2")
		for i := maxReplicas - 2; i >= 0; i = i - 2 {
			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".4: get the router reloaded log")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-High-77975-Dynamic Configuration Manager for reencrypt route [Serial]", func() {
		// skip the test if featureSet is not there
		if !compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = ingressControllerDescription{
				name:      "ocp77975",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		routehost := "reen77975" + "." + ingctrl.domain
		defer func() {
			// added debug info, in case the original router pod was terminated
			routerpod2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
			e2e.Logf("Before end of testing, the routerpod is: %s", routerpod2)
			ingctrl.delete(oc)
		}()
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("2.0 Create a deployment, a reencrypt route and a client pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		createRoute(oc, ns, "reencrypt", "reen77975", svcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "reen77975", "default")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Curl the reencrypt route")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		e2e.Logf("init routerpod is: %s", routerpod)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4.0 Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
		backend := "be_secure:" + ns + ":reen77975"
		checkDcmBackendCfg(oc, routerpod, backend)

		compat_otp.By("5.0 Use debug command to check the dynamic server's state")
		// used the socat command under the router pod to get all the route's endpoints status
		socatCmd := fmt.Sprintf(`echo "show servers state %s" | socat stdio /var/lib/haproxy/run/haproxy.sock | sed 1d | grep -v '^#' | cut -d ' ' -f2-6 | sed -e 's/0$/STOP/' -e 's/1$/STARTING/' -e 's/2$/UP/' -e 's/3$/SOFTSTOP/'`, backend)
		initSrvStates := checkDcmUpEndpoints(oc, routerpod, socatCmd, 1)
		currentSrvStates := ""

		compat_otp.By("6.0 get the initial router reloaded log")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		initReloadedNum := strings.Count(log, `"msg"="router reloaded" "logger"="template" "output"=`)

		compat_otp.By("7.0 keep scaling up the deployment with replicas 1")
		for i := 1; i < desiredReplicas; i++ {
			compat_otp.By("7." + strconv.Itoa(i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i+1))
			scaleDeploy(oc, ns, srvdmInfo, i+1)
			waitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(i+1))

			compat_otp.By("7." + strconv.Itoa(i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("7." + strconv.Itoa(i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i+1)

			compat_otp.By("7." + strconv.Itoa(i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("8.0 keep scaling down the deployment with replicas 1")
		for i := desiredReplicas - 1; i >= 0; i-- {
			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".2: Check the route's backend configuration including server pod, dynamic pool and dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("9.0 keep scaling up the deployment with replicas 2")
		maxReplicas := 0
		for i := 2; i <= desiredReplicas-2; i = i + 2 {
			compat_otp.By("9." + strconv.Itoa((i+2)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
			maxReplicas = i
		}

		compat_otp.By("10.0 keep scaling down the deployment with replicas 2")
		for i := maxReplicas - 2; i >= 0; i = i - 2 {
			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			scaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			checkDcmBackendCfg(oc, routerpod, backend)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = checkDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".4: get the router reloaded log")
			initReloadedNum = checkRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-Medium-78239-traffic test for dynamic servers", func() {
		// skip the test if featureSet is not there
		if !compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("testdata", "router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = ingressControllerDescription{
				name:      "ocp78239",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		httpRoutehost := "unsecure78239" + "." + ingctrl.domain
		reenRoutehost := "reen78239" + "." + ingctrl.domain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("2.0 Create a deployment and a client pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Create the HTTP route and the reencrypt route")
		createRoute(oc, ns, "http", "unsecure78239", unsecSvcName, []string{"--hostname=" + httpRoutehost})
		createRoute(oc, ns, "reencrypt", "reen78239", secSvcName, []string{"--hostname=" + reenRoutehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsecure78239", "default")
		ensureRouteIsAdmittedByIngressController(oc, ns, "reen78239", "default")

		compat_otp.By("4.0 set lb roundrobin to the routes")
		setAnnotation(oc, ns, "route/unsecure78239", "haproxy.router.openshift.io/balance=roundrobin")
		setAnnotation(oc, ns, "route/reen78239", "haproxy.router.openshift.io/balance=roundrobin")

		compat_otp.By("5.0 Scale up the deployment with the desired replicas strconv.Itoa(desiredReplicas)")
		podList := scaleDeploy(oc, ns, srvdmInfo, desiredReplicas)
		waitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(desiredReplicas))

		compat_otp.By("6.0 Keep curing the http route, make sure all backend endpoints are hit")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := httpRoutehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + httpRoutehost, "-s", "--resolve", toDst, "--connect-timeout", "10"}
		checkDcmServersAccessible(oc, curlCmd, podList, 180, desiredReplicas)

		compat_otp.By("7.0 Keep curing the reencrypt route, make sure all backend endpoints are hit")
		toDst = reenRoutehost + ":443:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + reenRoutehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		checkDcmServersAccessible(oc, curlCmd, podList, 180, desiredReplicas)
	})

	// author: hongli@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43745 and https://issues.redhat.com/browse/OCPBUGS-43811
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Critical-79514-haproxy option idle-close-on-response is configurable", func() {
		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp79514",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)
		routerPod := getOneRouterPodNameByIC(oc, ingctrl.name)

		compat_otp.By("Verify default spec.idleConnectionTerminationPolicy is Immediate in 4.19+")
		output := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, `{.spec.idleConnectionTerminationPolicy}`)
		o.Expect(output).To(o.ContainSubstring("Immediate"))

		compat_otp.By("Verify no variable ROUTER_IDLE_CLOSE_ON_RESPONSE in deployed router pod")
		checkEnv := readRouterPodEnv(oc, routerPod, "ROUTER_IDLE_CLOSE_ON_RESPONSE")
		o.Expect(checkEnv).To(o.ContainSubstring("NotFound"))

		compat_otp.By("Verify no option idle-close-on-response in haproxy.config")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerPod, "--", "grep", "idle-close-on-response", "haproxy.config").Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("Patch custom ingresscontroller spec.idleConnectionTerminationPolicy with Deferred")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, `{"spec":{"idleConnectionTerminationPolicy":"Deferred"}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		routerPod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("Verify variable ROUTER_IDLE_CLOSE_ON_RESPONSE of the deployed router pod")
		checkEnv = readRouterPodEnv(oc, routerPod, "ROUTER_IDLE_CLOSE_ON_RESPONSE")
		o.Expect(checkEnv).To(o.ContainSubstring(`ROUTER_IDLE_CLOSE_ON_RESPONSE=true`))

		compat_otp.By("Check the router haproxy.config for option idle-close-on-response")
		output = readRouterPodData(oc, routerPod, "cat haproxy.config", "idle-close-on-response")
		o.Expect(strings.Count(output, "option idle-close-on-response")).To(o.Equal(3))
	})
})
