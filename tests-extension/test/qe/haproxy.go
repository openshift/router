package qe

import (
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
	util "github.com/openshift/router-tests-extension/test"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-env", compat_otp.KubeConfigPath())

	// incorporate OCP-15044, OCP-15049, OCP-15050 and OCP-15051 into one
	// Test case creater: hongli@redhat.com - OCP-15044 The backend health check interval of unsecure route can be set by annotation
	// Test case creater: hongli@redhat.com - OCP-15049 The backend health check interval of edge route can be set by annotation
	// Test case creater: hongli@redhat.com - OCP-15050 The backend health check interval of passthrough route can be set by annotation
	// Test case creater: hongli@redhat.com - OCP-15051 The backend health check interval of reencrypt route can be set by annotation
	// port=yes - 99.4% pass rate (542 runs 60 days to Sep 1)
	g.It("The backend health check interval of unsecure/edge/passthrough/reencrypt route can be set by annotation", g.Label("OCP-15044"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
		)

		compat_otp.By("1. Create a server pod and its service")
		ns := oc.Namespace()
		defaultContPod := util.GetOneRouterPodNameByIC(oc, "default")
		// need two replicas of the server to test this scenario
		util.UpdateFilebySedCmd(testPodSvc, "replicas: 1", "replicas: 2")
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2.0: Create an unsecure, edge, reencrypt and passthrough route")
		util.CreateRoute(oc, ns, "http", "route-http", unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http", "default")
		util.CreateRoute(oc, ns, "edge", "route-edge", unSecSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		util.CreateRoute(oc, ns, "passthrough", "route-pass", secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-pass", "default")
		util.CreateRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		compat_otp.By("3.0: Annotate unsecure, edge, reencrypt and passthrough route")
		util.SetAnnotation(oc, ns, "route/route-http", `router.openshift.io/haproxy.health.check.interval=200ms`)
		util.SetAnnotation(oc, ns, "route/route-edge", `router.openshift.io/haproxy.health.check.interval=300ms`)
		util.SetAnnotation(oc, ns, "route/route-pass", `router.openshift.io/haproxy.health.check.interval=400ms`)
		util.SetAnnotation(oc, ns, "route/route-reen", `router.openshift.io/haproxy.health.check.interval=500ms`)

		compat_otp.By("4. Check the router pod and ensure the routes are loaded in haproxy.config of default controller")
		util.EnsureHaproxyBlockConfigContains(oc, defaultContPod, ns+":"+"route-http", []string{"check inter 200ms"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultContPod, ns+":"+"route-edge", []string{"check inter 300ms"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultContPod, ns+":"+"route-pass", []string{"check inter 400ms"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultContPod, ns+":"+"route-reen", []string{"check inter 500ms"})
	})

	// author: hongli@redhat.com
	// port=yes - 99.6% pass rate (542 runs 60 days to Sep 1)
	g.It("No health check when there is only one endpoint for a route", g.Label("OCP-16870"), func() {
		// skip the test if featureSet is set there
		if compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("Skip for not supporting DynamicConfigurationManager")
		}

		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			deploymentName      = "web-server-deploy"
			unSecSvcName        = "service-unsecure"
		)

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

		compat_otp.By("3.0: Check the haproxy.config that the health check should not exist in the backend server slots")
		jsonPath := fmt.Sprintf(`{.items[?(@.metadata.generateName=="%s-")].metadata.name}`, unSecSvcName)
		epSliceName := util.GetByJsonPath(oc, ns, "EndpointSlice", jsonPath)
		jsonPath = "{.endpoints[0].addresses[0]}"
		epIP := util.GetByJsonPath(oc, ns, "EndpointSlice/"+epSliceName, jsonPath)
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		backendConfig := util.EnsureHaproxyBlockConfigContains(oc, routerpod, "be_http:"+ns+":"+unSecSvcName, []string{epIP})
		o.Expect(backendConfig).NotTo(o.ContainSubstring("check inter"))

		compat_otp.By("4.0: Scale up the deployment with replicas 2, then check the haproxy.config that the health check should exist in the backend server slots")
		util.ScaleDeploy(oc, ns, deploymentName, 2)
		backendConfig = util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerpod, "be_http:"+ns+":"+unSecSvcName, []string{epIP + ".+check inter"})
		o.Expect(strings.Count(backendConfig, "check inter") == 2).To(o.BeTrue())
	})

	// Test case creater: zzhao@redhat.com
	// port=yes - 99.8% pass rate (542 runs 60 days to Sep 1)
	g.It("Health check when there are multi service and each service has one backend", g.Label("OCP-16872"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
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
			unsecureRouteName = "16872"
		)

		compat_otp.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.Namespace = ns
		webServerDeploy2.Namespace = ns
		webServerDeploy1.Create(oc)
		webServerDeploy2.Create(oc)
		util.EnsurePodWithLabelReady(oc, ns, deploy1Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy2Label)
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")

		compat_otp.By("1. Create unsecure route and set route-backends with multi serivces")
		util.CreateRoute(oc, ns, "http", unsecureRouteName, webServerDeploy1.SvcUnsecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unsecureRouteName, "default")
		// Confirm there is no check inter in the haproxy.config before setting route-backends with multi services.
		httpSearchOutput := util.EnsureHaproxyBlockConfigContains(oc, routerPod, ns+":"+unsecureRouteName, []string{unsecureRouteName})
		o.Expect(httpSearchOutput).NotTo(o.ContainSubstring("check inter 500ms"))
		// Note: the "balance roundrobin" is used for the route once set route-backends, no need to annotate the route"
		err := oc.Run("set").Args("route-backends", unsecureRouteName, "service-unsecure1=20", "service-unsecure2=80").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("2. Check unsecure route health check interval in haproxy.config")
		util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, "be_http:"+ns+":"+unsecureRouteName, []string{"server pod:" + webServerDeploy1.DeployName + ".+check inter 5000ms", "server pod:" + webServerDeploy2.DeployName + ".+check inter 5000ms"})
	})

	// author: shudili@redhat.com
	// Includes OCP-21766: Integrate router metrics with the monitoring component
	//          OCP-10903: The router pod should have default resource limits
	// port=no - 0.0% pass rate (542 runs 60 days to Sep 1)
	g.It("misc tests for haproxy router", g.Label("OCP-21766"), func() {
		var (
			namespace          = "openshift-ingress"
			servicemonitorName = "router-default"
			rolebindingName    = "prometheus-k8s"
		)
		compat_otp.By(fmt.Sprintf("Check whether servicemonitor %s exists or not", servicemonitorName))
		jsonPath := "{.items[*].metadata.name}"
		servicemonitorList := util.GetByJsonPath(oc, namespace, "servicemonitor", jsonPath)
		o.Expect(servicemonitorList).To(o.ContainSubstring(servicemonitorName))

		compat_otp.By(fmt.Sprintf("Check whether rolebinding prometheus-k8s exists or not", rolebindingName))
		rolebindingList := util.GetByJsonPath(oc, namespace, "rolebinding", jsonPath)
		o.Expect(rolebindingList).To(o.ContainSubstring(rolebindingName))

		compat_otp.By(fmt.Sprintf("check the openshift.io/cluster-monitoring label of the namespace %s, which should be true", namespace))
		jsonPath = `{.metadata.labels.openshift\.io/cluster-monitoring}`
		value := util.GetByJsonPath(oc, "default", "namespace/"+namespace, jsonPath)
		o.Expect(value).To(o.ContainSubstring("true"))

		// OCP-10903
		compat_otp.By("check the default resources limits of the router container in the router-default deployment")
		jsonPath = `{.spec.template.spec.containers[?(@.name=="router")].resources.requests.cpu}{.spec.template.spec.containers[?(@.name=="router")].resources.requests.memory}`
		resources := util.GetByJsonPath(oc, "openshift-ingress", "deployments/router-default", jsonPath)
		o.Expect(resources).To(o.ContainSubstring("25m64Mi"))
	})

	// author: shudili@redhat.com
	// port=yes - 99.3% pass rate (542 runs 60 days to Sep 1)
	g.It("router support HTTP2 for passthrough route and reencrypt route with custom certs", g.Label("OCP-27628"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
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
		customTemp := util.AddExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp27628",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0 Get the domain info for testing")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "reen27628" + "." + ingctrl.Domain

		compat_otp.By("2.0: Start to use openssl to create ca certification&key and user certification&key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("2.1: Create a new self-signed CA including the ca certification and ca key")
		util.OpenSSLNewCA(caKey, caCrt, caSubj)

		compat_otp.By("2.2: Create a user CSR and the user key for the reen route")
		util.OpenSSLNewCSR(usrKey, usrCsr, userSubj)

		compat_otp.By("2.3: Sign the user CSR and generate the certificate for the reen route")
		san := "subjectAltName = DNS:*." + ingctrl.Domain
		util.OpenSSLSignCSR(san, usrCsr, caCrt, caKey, usrCrt)

		compat_otp.By("3.0: create a cm with date ca certification, then create the custom ingresscontroller")
		defer util.DeleteConfigMap(oc, "openshift-config", cmName)
		util.CreateConfigMapFromFile(oc, "openshift-config", cmName, "ca-bundle.pem="+caCrt)

		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		compat_otp.By("4.0: enable http2 on the custom ingresscontroller by the annotation if env ROUTER_DISABLE_HTTP2 is true")
		jsonPath := "{.spec.template.spec.containers[0].env[?(@.name==\"ROUTER_DISABLE_HTTP2\")].value}"
		envValue := util.GetByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.Name, jsonPath)
		if envValue == "true" {
			util.SetAnnotationAsAdmin(oc, ingctrl.Namespace, "ingresscontroller/"+ingctrl.Name, `ingress.operator.openshift.io/default-enable-http2=true`)
			util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		}

		compat_otp.By("5.0 Create a deployment and a client pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, dirname, ns+"/"+clientPodName+":"+podDirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("6.0 Create a reencrypt route and a passthrough route inside the namespace")
		util.CreateRoute(oc, ns, "reencrypt", "route-reen", svcName, []string{"--hostname=" + routehost, "--ca-cert=" + caCrt, "--cert=" + usrCrt, "--key=" + usrKey})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")
		routehost2 := "pass27628" + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "passthrough", "route-pass", svcName, []string{"--hostname=" + routehost2})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-pass", "default")

		compat_otp.By("7.0 Check the cert_config.map for the reencypt route with custom cert")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "cat cert_config.map").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`route-reen.pem [alpn h2,http/1.1] ` + routehost))

		compat_otp.By("8.0 Curl the reencrypt route with specified protocol http2")
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--http2", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "HTTP/2 200", 60, 1)

		compat_otp.By("9.0 Curl the pass route with specified protocol http2")
		toDst = routehost2 + ":443:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost2, "-skI", "--http2", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "HTTP/2 200", 60, 1)
	})

	// author: hongli@redhat.com
	// port=yes - 98.5% pass rate (542 runs 60 days to Sep 1)
	g.It("Ingresscontroller routes traffic only to ready pods/backends", g.Label("OCP-37714"), func() {
		// skip the test if featureSet is set there
		if compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("Skip for DCM enabled, the haproxy has the dynamic server slot configration for the only one endpoint, not the static")
		}

		// if the ingress canary route isn't accessable from outside, skip it
		if !util.IsCanaryRouteAvailable(oc) {
			g.Skip("Skip for the ingress canary route could not be available to the outside")
		}

		var (
			buildPruningBaseDir = TestdataDir()
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
		updatedDeployFile := util.AddContentToFileWithMatchedOrder(testPodSvc, "        - name: nginx", extraParas, 1)
		defer os.Remove(updatedDeployFile)

		compat_otp.By("2.0 Create a deployment and a client pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, updatedDeployFile)
		util.WaitForOutputEquals(oc, ns, "deployment/web-server-deploy", "{.status.replicas}", "1", 180*time.Second)
		serverPod := util.GetPodListByLabel(oc, ns, "name=web-server-deploy")[0]
		util.WaitForOutputEquals(oc, ns, "pod/"+serverPod, "{.status.phase}", "Running")
		util.WaitForOutputEquals(oc, ns, "pod/"+serverPod, "{.status.conditions[?(@.type==\"Ready\")].status}", "False")

		compat_otp.By("3.0 Create a http route")
		routehost := "unsecure37714" + ".apps." + util.GetBaseDomain(oc)
		util.CreateRoute(oc, ns, "http", unSecSvcName, unSecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unSecSvcName, "default")

		compat_otp.By("4.0 Check haproxy.config which should not have the server slot")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		serverSlot := "pod:" + serverPod + ":" + unSecSvcName + ":http"
		backendConfig := util.EnsureHaproxyBlockConfigContains(oc, routerpod, "be_http:"+ns+":"+unSecSvcName, []string{unSecSvcName})
		o.Expect(backendConfig).NotTo(o.ContainSubstring(serverSlot))

		compat_otp.By("5.0 Curl the http route, expect to get 503 for the server pod is not ready")
		curlCmd := fmt.Sprintf(`curl http://%s -sI --connect-timeout 10`, routehost)
		util.RepeatCmdOnClient(oc, curlCmd, "503", 60, 1)

		compat_otp.By("6.0 Create the /data/ready under the server pod")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, serverPod, "--", "touch", "/data/ready").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.WaitForOutputEquals(oc, ns, "pod/"+serverPod, "{.status.conditions[?(@.type==\"Ready\")].status}", "True")

		compat_otp.By("7.0 Check haproxy.config which should have the server slot")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "be_http:"+ns+":"+unSecSvcName, []string{serverSlot})

		compat_otp.By("8.0 Curl the http route again, expect to get 200 ok for the server pod is ready")
		util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
	})

	// author: aiyengar@redhat.com
	// port=unknown - no data in BigQuery 60 days to Sep 1
	g.It("Ingresscontroller with endpointPublishingStrategy of hostNetwork allows PROXY protocol for source forwarding", g.Label("OCP-40675"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-hn-PROXY.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp40675",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("check whether there are more than two worker nodes present for testing hostnetwork")
		workerNodeCount, _ := util.ExactNodeDetails(oc)
		if workerNodeCount <= 2 {
			g.Skip("Skipping as we need more than two worker nodes")
		}

		compat_otp.By("Create a hostNetwork ingresscontroller with PROXY protocol set")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Check the router env to verify the PROXY variable is applied")
		routername := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.PollReadPodData(oc, "openshift-ingress", routername, "/usr/bin/env", `ROUTER_USE_PROXY_PROTOCOL=true`)
	})

	// author: aiyengar@redhat.com
	// port=yes - 99.4% pass rate (508 runs 60 days to Sep 1)
	g.It("Ingresscontroller with endpointPublishingStrategy of nodePort allows PROXY protocol for source forwarding", g.Label("OCP-40677"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np-PROXY.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp40677",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a NP ingresscontroller with PROXY protocol set")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Check the router env to verify the PROXY variable is applied")
		podname := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		dssearch := util.ReadRouterPodEnv(oc, podname, "ROUTER_USE_PROXY_PROTOCOL")
		o.Expect(dssearch).To(o.ContainSubstring(`ROUTER_USE_PROXY_PROTOCOL=true`))
	})

	// author: shudili@redhat.com
	// port=yes - 98.8% pass rate (508 runs 60 days to Sep 1)
	g.It("The endpointPublishingStrategy parameter allows TCP, PROXY, and empty definitions for HostNetwork", g.Label("OCP-40679"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hn-PROXY.yaml")
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp40679",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
		)

		compat_otp.By("check whether there are more than two worker nodes present for testing hostnetwork")
		workerNodeCount, _ := util.ExactNodeDetails(oc)
		if workerNodeCount <= 2 {
			g.Skip("Skipping as we need more than two worker nodes")
		}

		compat_otp.By("Create a hostNetwork ingresscontroller with protocol PROXY set by the template")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Check the router env to verify the PROXY variable is applied")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.PollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_USE_PROXY_PROTOCOL=true`)

		compat_otp.By("Patch the hostNetwork ingresscontroller with protocol TCP")
		patchPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"protocol\": \"TCP\"}}}}"
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("Check the configuration and router env for protocol TCP")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		cmd := fmt.Sprintf("/usr/bin/env | grep %s", `ROUTER_USE_PROXY_PROTOCOL`)
		jsonPath := "{.spec.endpointPublishingStrategy.hostNetwork.protocol}"
		output := util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jsonPath)
		o.Expect(output).To(o.ContainSubstring("TCP"))
		err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", cmd).Execute()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("Patch the hostNetwork ingresscontroller with protocol empty")
		patchPath = `{"spec":{"endpointPublishingStrategy":{"hostNetwork":{"protocol": ""}}}}`
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")

		compat_otp.By("Check the configuration and router env for protocol empty")
		output = util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jsonPath)
		o.Expect(output).To(o.BeEmpty())
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", cmd).Execute()
		o.Expect(err).To(o.HaveOccurred())
	})

	// port=yes - 99.5% pass rate (780 runs 60 days to Sep 1)
	g.It("The Power-of-two balancing features defaults to random LB algorithm instead of leastconn for REEN/Edge/insecure routes", g.Label("OCP-41042"), func() {
		var (
			baseDomain   = util.GetBaseDomain(oc)
			defaultPod   = util.GetOneRouterPodNameByIC(oc, "default")
			unsecsvcName = "service-unsecure"
			secsvcName   = "service-secure"
		)
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		addSvc := filepath.Join(buildPruningBaseDir, "svc-additional-backend.yaml")

		compat_otp.By("Create pods and service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.CreateResourceFromFile(oc, ns, addSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a edge/insecure/REEN/passthrough type routes via the services inside the namespace")
		edgeRoute := "route-edge" + "-" + ns + "." + baseDomain
		reenRoute := "route-reen" + "-" + ns + "." + baseDomain
		util.CreateRoute(oc, ns, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRoute})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))
		util.CreateRoute(oc, ns, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRoute})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-reen"))
		util.CreateRoute(oc, ns, "http", unsecsvcName, unsecsvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("Check the default loadbalance algorithm inside proxy pod")
		edgeBackend := "be_edge_http:" + ns + ":route-edge"
		reenBackend := "be_secure:" + ns + ":route-reen"
		insecBackend := "be_http:" + ns + ":service-unsecure"
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, edgeBackend, []string{"balance random"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, reenBackend, []string{"balance random"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, insecBackend, []string{"balance random"})
	})

	// port=yes - 99.4% pass rate (542 runs 60 days to Sep 1)
	g.It("The Power-of-two balancing features switches to roundrobin mode for REEN/Edge/insecure/passthrough routes with multiple backends configured with weights", g.Label("OCP-41186"), func() {
		var (
			baseDomain   = util.GetBaseDomain(oc)
			defaultPod   = util.GetOneRouterPodNameByIC(oc, "default")
			unsecsvcName = "service-unsecure"
			secsvcName   = "service-secure"
		)
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		addSvc := filepath.Join(buildPruningBaseDir, "svc-additional-backend.yaml")

		compat_otp.By("Create pods and service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.CreateResourceFromFile(oc, ns, addSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a edge/insecure/REEN/passthrough type routes via the services inside the namespace")
		edgeRoute := "route-edge" + "-" + ns + "." + baseDomain
		reenRoute := "route-reen" + "-" + ns + "." + baseDomain
		passthRoute := "route-passth" + "-" + ns + "." + baseDomain
		util.CreateRoute(oc, ns, "edge", "route-edge", unsecsvcName, []string{"--hostname=" + edgeRoute})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))
		util.CreateRoute(oc, ns, "reencrypt", "route-reen", secsvcName, []string{"--hostname=" + reenRoute})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-reen"))
		util.CreateRoute(oc, ns, "passthrough", "route-passth", unsecsvcName, []string{"--hostname=" + passthRoute})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-passth"))
		util.CreateRoute(oc, ns, "http", unsecsvcName, unsecsvcName, []string{})
		output, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("Check the default loadbalance algorithm inside proxy pod")
		edgeBackend := "be_edge_http:" + ns + ":route-edge"
		reenBackend := "be_secure:" + ns + ":route-reen"
		insecBackend := "be_http:" + ns + ":service-unsecure"
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, edgeBackend, []string{"balance random"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, reenBackend, []string{"balance random"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, insecBackend, []string{"balance random"})

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
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, edgeBackend, []string{"balance roundrobin"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, reenBackend, []string{"balance roundrobin"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, insecBackend, []string{"balance roundrobin"})
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, passthBackend, []string{"balance roundrobin"})
	})

	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("The Power of two balancing  honours the per route balancing algorithm defined via haproxy.router.openshift.io/balance annotation", g.Label("OCP-41187"), func() {
		var (
			defaultPod   = util.GetOneRouterPodNameByIC(oc, "default")
			unsecsvcName = "service-unsecure"
		)
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")

		compat_otp.By("Create pods and service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a route from the namespace and set route LB annotation")
		util.CreateRoute(oc, ns, "http", unsecsvcName, unsecsvcName, []string{})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(unsecsvcName))
		util.SetAnnotation(oc, ns, "route/service-unsecure", "haproxy.router.openshift.io/balance=leastconn")
		findAnnotation, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", unsecsvcName, "-n", ns, "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		getAlgoValue := gjson.Get(string(findAnnotation), "haproxy\\.router\\.openshift\\.io/balance").String()
		o.Expect(getAlgoValue).To(o.ContainSubstring("leastconn"))

		compat_otp.By("Check the default loadbalance algorithm inside proxy pod and check the default LB variable to confirm power-of-two is active")
		insecBackend := "be_http:" + ns + ":service-unsecure"
		rtrParamCheck := util.ReadPodEnv(oc, defaultPod, "openshift-ingress", "ROUTER_LOAD_BALANCE_ALGORITHM")
		o.Expect(rtrParamCheck).To(o.ContainSubstring("random"))
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, insecBackend, []string{"balance leastconn"})
	})

	// port=yes - 99.4% pass rate (508 runs 60 days to Sep 1)
	g.It("Power-of-two feature allows unsupportedConfigOverrides ingress operator option to enable leastconn balancing algorithm", g.Label("OCP-41206"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "41206",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Patch ingresscontroller with unsupportedConfigOverrides option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"unsupportedConfigOverrides\":{\"loadBalancingAlgorithm\":\"leastconn\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		newrouterpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("verify ROUTER_LOAD_BALANCE_ALGORITHM variable of the deployed router pod")
		checkenv := util.ReadRouterPodEnv(oc, newrouterpod, "ROUTER_LOAD_BALANCE_ALGORITHM")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_LOAD_BALANCE_ALGORITHM=leastconn`))

		compat_otp.By("deploy pod resource and expose a route via the ingresscontroller")
		ns := oc.Namespace()
		edgeRoute := "route-edge" + "-" + ns + "." + ingctrl.Domain
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		util.CreateRoute(oc, ns, "edge", "route-edge", "service-unsecure", []string{"--hostname=" + edgeRoute})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Check the router config for the default LB algorithm set at the backend")
		edgeBackend := "be_edge_http:" + ns + ":route-edge"
		util.EnsureHaproxyBlockConfigContains(oc, newrouterpod, edgeBackend, []string{"balance leastconn"})
	})

	// port=yes - 99.0% pass rate (508 runs 60 days to Sep 1)
	g.It("router continues to function normally with the service selector of exposed route gets removed/deleted", g.Label("OCP-41929"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp41929",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")
		custContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("Deploy a backend pod and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "service-unsecure-" + ns + "." + ingctrl.Domain
		SrvErr := oc.Run("expose").Args("svc/service-unsecure", "--hostname="+routehost).Execute()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		routeOutput := util.GetRoutes(oc, ns)
		o.Expect(routeOutput).To(o.ContainSubstring("service-unsecure"))

		compat_otp.By("Cross check the selector value of the 'service-unsecure' service")
		jpath := "{.spec.selector}"
		output := util.GetByJsonPath(oc, ns, "svc/service-unsecure", jpath)
		o.Expect(output).To(o.ContainSubstring(`"name":"web-server-deploy"`))

		compat_otp.By("Delete the service selector for the 'service-unsecure' service")
		patchPath := `{"spec":{"selector":null}}`
		util.PatchResourceAsAdmin(oc, ns, "svc/service-unsecure", patchPath)

		compat_otp.By("Check the service config to confirm the value of the selector is empty")
		output = util.GetByJsonPath(oc, ns, "svc/service-unsecure", jpath)
		o.Expect(output).To(o.BeEmpty())

		compat_otp.By("Check the router pod logs and confirm there is no reload error message")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", custContPod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Contains(log, "error reloading router") {
			e2e.Failf("Router reloaded after removing service selector")
		}
	})

	// author: shudili@redhat.com
	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("Errorfile stanzas and dummy default html files have been added to the router", g.Label("OCP-42878"), func() {
		compat_otp.By("Get pod (router) in openshift-ingress namespace")
		podname := util.GetOneRouterPodNameByIC(oc, "default")

		compat_otp.By("Check if there are default 404 and 503 error pages on the router")
		searchOutput := util.ReadRouterPodData(oc, podname, "ls -l", "error-page")
		o.Expect(searchOutput).To(o.ContainSubstring(`error-page-404.http`))
		o.Expect(searchOutput).To(o.ContainSubstring(`error-page-503.http`))

		compat_otp.By("Check if errorfile stanzas have been added into haproxy-config.template")
		searchOutput = util.ReadRouterPodData(oc, podname, "cat haproxy-config.template", "errorfile")
		o.Expect(searchOutput).To(o.ContainSubstring(`ROUTER_ERRORFILE_404`))
		o.Expect(searchOutput).To(o.ContainSubstring(`ROUTER_ERRORFILE_503`))
	})

	// author: shudili@redhat.com
	// port=yes - 98.8% pass rate (508 runs 60 days to Sep 1)
	g.It("User can customize HAProxy 2.0 Error Page", g.Label("OCP-42940"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
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
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp42940",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.Name
		)

		compat_otp.By("Create a ConfigMap with custom 404 and 503 error pages")
		cmCrtErr := oc.AsAdmin().WithoutNamespace().Run("create").Args("configmap", cmName, "--from-file="+http404page, "--from-file="+http503page, "-n", "openshift-config").Execute()
		o.Expect(cmCrtErr).NotTo(o.HaveOccurred())
		defer util.DeleteConfigMap(oc, "openshift-config", cmName)
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
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("patch the custom ingresscontroller with the http error code pages")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchHTTPErrorPage)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("get one custom ingress-controller router pod's IP")
		podname := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, podname, "openshift-ingress")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		compat_otp.By("create a client pod")
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		compat_otp.By("create an unsecure service and its backend pod")
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("Expose an route with the unsecure service inside the namespace")
		routehost := srvName + "-" + ns + "." + ingctrl.Domain
		srvErr := oc.Run("expose").Args("service", srvName, "--hostname="+routehost).Execute()
		o.Expect(srvErr).NotTo(o.HaveOccurred())
		util.WaitForOutputEquals(oc, ns, "route", "{.items[0].metadata.name}", srvName)

		compat_otp.By("curl a normal route from the client pod")
		routestring := srvName + "-" + ns + "." + ingctrl.Name + "."
		util.WaitForCurl(oc, clientPodName, baseDomain, routestring, "200 OK", podIP)

		compat_otp.By("curl a non-existing route, expect to get custom http 404 Not Found error")
		notExistRoute := "notexistroute" + "-" + ns + "." + ingctrl.Domain
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
		err = util.WaitForResourceToDisappear(oc, ns, "pod/"+podname)
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+podname))
		output, err = oc.Run("exec").Args(clientPodName, "--", "curl", "-v", "http://"+routehost, "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("503 Service Unavailable"))
		o.Expect(output).To(o.ContainSubstring("Custom error page:The requested application is not available"))
	})

	// author: jechen@redhat.com
	// port=yes - 99.0% pass rate (508 runs 60 days to Sep 1)
	g.It("Configmap mounted on router volume after ingresscontroller has spec field HttpErrorCodePage populated with configmap name", g.Label("OCP-43115"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp43115",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1. create a custom ingresscontroller, and get its router name")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.  Configure a customized error page configmap from files in openshift-config namespace")
		configmapName := "custom-43115-error-code-pages"
		cmFile1 := filepath.Join(buildPruningBaseDir, "error-page-503.http")
		cmFile2 := filepath.Join(buildPruningBaseDir, "error-page-404.http")
		_, error := oc.AsAdmin().WithoutNamespace().Run("create").Args("configmap", configmapName, "--from-file="+cmFile1, "--from-file="+cmFile2, "-n", "openshift-config").Output()
		o.Expect(error).NotTo(o.HaveOccurred())
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("configmap", configmapName, "-n", "openshift-config").Output()

		compat_otp.By("3. Check if configmap is successfully configured in openshift-config namesapce")
		err := util.CheckConfigMap(oc, "openshift-config", configmapName)
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("cm %v not found", configmapName))

		compat_otp.By("4. Patch the configmap created above to the custom ingresscontroller in openshift-ingress namespace")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"httpErrorCodePages\":{\"name\":\"custom-43115-error-code-pages\"}}}")

		compat_otp.By("5. Check if configmap is successfully patched into openshift-ingress namesapce, configmap with name ingctrl.Name-errorpages should be created")
		expectedCmName := ingctrl.Name + `-errorpages`
		err = util.CheckConfigMap(oc, "openshift-ingress", expectedCmName)
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("cm %v not found", expectedCmName))

		compat_otp.By("6. Obtain new router pod created, and check if error_code_pages directory is created on it")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		newrouterpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("Check /var/lib/haproxy/conf directory to see if error_code_pages subdirectory is created on the router")
		searchOutput := util.ReadRouterPodData(oc, newrouterpod, "ls -al /var/lib/haproxy/conf", "error_code_pages")
		o.Expect(searchOutput).To(o.ContainSubstring(`error_code_pages`))

		compat_otp.By("7. Check if custom error code pages have been mounted")
		searchOutput = util.ReadRouterPodData(oc, newrouterpod, "ls -al /var/lib/haproxy/conf/error_code_pages", "error")
		o.Expect(searchOutput).To(o.ContainSubstring(`error-page-503.http -> ..data/error-page-503.http`))
		o.Expect(searchOutput).To(o.ContainSubstring(`error-page-404.http -> ..data/error-page-404.http`))

		searchOutput = util.ReadRouterPodData(oc, newrouterpod, "cat /var/lib/haproxy/conf/error_code_pages/error-page-503.http", "Unavailable")
		o.Expect(searchOutput).To(o.ContainSubstring(`HTTP/1.0 503 Service Unavailable`))
		o.Expect(searchOutput).To(o.ContainSubstring(`Custom:Application Unavailable`))

		searchOutput = util.ReadRouterPodData(oc, newrouterpod, "cat /var/lib/haproxy/conf/error_code_pages/error-page-404.http", "Not Found")
		o.Expect(searchOutput).To(o.ContainSubstring(`HTTP/1.0 404 Not Found`))
		o.Expect(searchOutput).To(o.ContainSubstring(`Custom:Not Found`))

	})

	// author: shudili@redhat.com
	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("User can delete configmap and update configmap with new custom error page", g.Label("OCP-43292"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
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
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp43292",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.Name
		)

		compat_otp.By("Create a ConfigMap with custom 404 and 503 error pages")
		defer util.DeleteConfigMap(oc, "openshift-config", cmName)
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
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("patch the custom ingresscontroller with the http error code pages")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchHTTPErrorPage)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("get one custom ingress-controller router pod's IP")
		podname := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, podname, "openshift-ingress")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		compat_otp.By("create a client pod")
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		compat_otp.By("create an unsecure service and its backend pod")
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("Expose an route with the unsecure service inside the namespace")
		routehost := srvName + "-" + ns + "." + ingctrl.Domain
		toDst := routehost + ":80:" + podIP
		output, SrvErr := oc.Run("expose").Args("service", srvName, "--hostname="+routehost).Output()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(srvName))
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-I", "http://" + routehost, "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, cmdOnPod, "200", 60, 1)

		compat_otp.By("curl a non-existing route, expect to get custom http 404 Not Found error")
		notExistRoute := "notexistroute" + "-" + ns + "." + ingctrl.Domain
		toDst = notExistRoute + ":80:" + podIP
		output, err := oc.Run("exec").Args("-n", ns, clientPodName, "--", "curl", "-v", "http://"+notExistRoute, "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).Should(o.And(
			o.ContainSubstring("404 Not Found"),
			o.ContainSubstring("Custom error page:The requested document was not found")))

		compat_otp.By("remove the custom error page from the ingress-controller")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, rmHTTPErrorPage)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")
		util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

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
	// port=yes - 98.6% pass rate (508 runs 60 days to Sep 1)
	g.It("The logEmptyRequests ingresscontroller parameter set to Ignore add the dontlognull option in the haproxy configuration", g.Label("OCP-43414"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "43414",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Patch ingresscontroller with logEmptyRequests set to Ignore option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"type\":\"Container\"},\"logEmptyRequests\":\"Ignore\"}}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		newrouterpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("verify the Dontlog variable inside the  router pod")
		checkenv := util.ReadRouterPodEnv(oc, newrouterpod, "ROUTER_DONT_LOG_NULL")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_DONT_LOG_NULL=true`))

		compat_otp.By("Verify the parameter set in the haproxy configuration of the router pod")
		checkoutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newrouterpod, "--", "bash", "-c", `cat haproxy.config | grep -w "dontlognull"`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(checkoutput).To(o.ContainSubstring(`option dontlognull`))

	})

	// author: aiyengar@redhat.com
	// port=yes - 99.2% pass rate (508 runs 60 days to Sep 1)
	g.It("httpEmptyRequestsPolicy ingresscontroller parameter set to ignore adds the http-ignore-probes option in the haproxy configuration", g.Label("OCP-43416"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "43416",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Patch ingresscontroller with logEmptyRequests set to Ignore option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"httpEmptyRequestsPolicy\":\"Ignore\"}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		newrouterpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		compat_otp.By("verify the Dontlog variable inside the  router pod")
		checkenv := util.ReadRouterPodEnv(oc, newrouterpod, "ROUTER_HTTP_IGNORE_PROBES")
		o.Expect(checkenv).To(o.ContainSubstring(`ROUTER_HTTP_IGNORE_PROBES=true`))

		compat_otp.By("Verify the parameter set in the haproxy configuration of the router pod")
		checkoutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newrouterpod, "--", "bash", "-c", `cat haproxy.config | grep -w "http-ignore-probes"`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(checkoutput).To(o.ContainSubstring(`option http-ignore-probes`))
	})

	// author: shudili@redhat.com
	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("The logEmptyRequests option only gets applied when the access logging is configured for the ingresscontroller", g.Label("OCP-43454"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = util.IngressControllerDescription{
				Name:      "43454",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.Name
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("check the default .spec.logging")
		jpath := "{.spec.logging}"
		logging := util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jpath)
		o.Expect(logging).To(o.BeEmpty())

		compat_otp.By("patch the custom ingresscontroller with .spec.logging.access.destination.container")
		patchPath := `{"spec":{"logging":{"access":{"destination":{"type":"Container"}}}}}`
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("check the .spec.logging")
		logging = util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jpath)
		expLogStr := "\"logEmptyRequests\":\"Log\""
		o.Expect(logging).To(o.ContainSubstring(expLogStr))
	})

	// author: mjoseph@redhat.com
	// port=yes - 99.4% pass rate (508 runs 60 days to Sep 1)
	g.It("Setting ROUTER_ENABLE_COMPRESSION and ROUTER_COMPRESSION_MIME in HAProxy", g.Label("OCP-46571"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "46571",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Patch ingresscontroller with httpCompression option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"httpCompression\":{\"mimeTypes\":[\"text/html\",\"text/css; charset=utf-8\",\"application/json\"]}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		newrouterpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("check the env variable of the router pod")
		checkenv1 := util.ReadRouterPodEnv(oc, newrouterpod, "ROUTER_ENABLE_COMPRESSION")
		o.Expect(checkenv1).To(o.ContainSubstring(`ROUTER_ENABLE_COMPRESSION=true`))
		checkenv2 := util.ReadRouterPodEnv(oc, newrouterpod, "ROUTER_COMPRESSION_MIME")
		o.Expect(checkenv2).To(o.ContainSubstring(`ROUTER_COMPRESSION_MIME=text/html "text/css; charset=utf-8" application/json`))

		compat_otp.By("check the haproxy config on the router pod for compression algorithm")
		algo := util.ReadRouterPodData(oc, newrouterpod, "cat haproxy.config", "compression")
		o.Expect(algo).To(o.ContainSubstring(`compression algo gzip`))
		o.Expect(algo).To(o.ContainSubstring(`compression type text/html "text/css; charset=utf-8" application/json`))
	})

	// author: mjoseph@redhat.com
	// port=yes - 100.0% pass rate (508 runs 60 days to Sep 1)
	g.It("Setting wrong data in ROUTER_ENABLE_COMPRESSION and ROUTER_COMPRESSION_MIME in HAProxy", g.Label("OCP-46898"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "46898",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller, and get its router name")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("Patch ingresscontroller with wrong httpCompression data and check whether it is configurable")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/46898", "-p", "{\"spec\":{\"httpCompression\":{\"mimeTypes\":[\"text/\",\"text/css; charset=utf-8\",\"//\"]}}}", "--type=merge", "-n", ingctrl.Namespace).Output()
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
	// port=yes - 99.6% pass rate (746 runs 60 days to Sep 1)
	g.It("check haproxy router v4v6 mode", g.Label("OCP-47344"), func() {
		compat_otp.By("Get ROUTER_IP_V4_V6_MODE env, if NotFound then v4 is using by default")
		defaultRouterPod := util.GetOneRouterPodNameByIC(oc, "default")
		checkEnv := util.ReadRouterPodEnv(oc, defaultRouterPod, "ROUTER_IP_V4_V6_MODE")
		ipStackType := util.CheckIPStackType(oc)
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
	// port=no - 28.5% pass rate (508 runs 60 days to Sep 1)
	g.It("check haproxy's version and router base image", g.Label("OCP-49131"), func() {
		var expVersion = "3.2.22"
		compat_otp.By("Try to get HAProxy's version from the container running haproxy")
		haproxyVer := util.GetHAProxySockVersion(oc)
		compat_otp.By("show haproxy version(" + haproxyVer + "), and check if it is updated successfully")
		o.Expect(haproxyVer).To(o.ContainSubstring(expVersion))
		// in 4.16, OCP-73373 - Bump openshift-router image to RHEL9"
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", "-c", "haproxy", "deploy/router-default", "--", "bash", "-c", "cat /etc/redhat-release").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Red Hat Enterprise Linux release 9"))
	})

	// author: shudili@redhat.com
	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("Allow Ingress to be modified on the settings of livenessProbe and readinessProbe", g.Label("OCP-50074"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		timeout5 := "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"router\",\"livenessProbe\":{\"timeoutSeconds\":5},\"readinessProbe\":{\"timeoutSeconds\":5}}]}}}}"
		timeoutmax := "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"router\",\"livenessProbe\":{\"timeoutSeconds\":2147483647},\"readinessProbe\":{\"timeoutSeconds\":2147483647}}]}}}}"
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp50074",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")
		util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("check the default liveness probe and readiness probe parameters in the json outut of the router deployment")
		routerDeploymentName := "router-" + ingctrl.Name
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..livenessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":1"))
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..readinessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":1"))

		compat_otp.By("patch livenessProbe and readinessProbe with 5s to the router deployment")
		_, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("deployment", routerDeploymentName, "--type=strategic", "--patch="+timeout5, "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

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
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")

		compat_otp.By("check liveness probe and readiness probe max 2147483647s in the json output of the router deployment")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..livenessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":2147483647"))
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", routerDeploymentName, "-o=jsonpath={..readinessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":2147483647"))

		compat_otp.By("check liveness probe and readiness probe max 2147483647s in the json output of the router pod")
		podname := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podname, "-o=jsonpath={..livenessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":2147483647"))
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", podname, "-o=jsonpath={..readinessProbe}", "-n", "openshift-ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("\"timeoutSeconds\":2147483647"))
	})

	// author: shudili@redhat.com
	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("Negative test of allow Ingress to be modified on the settings of livenessProbe and readinessProbe", g.Label("OCP-50075"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		timeoutMinus := "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"router\",\"livenessProbe\":{\"timeoutSeconds\":-1},\"readinessProbe\":{\"timeoutSeconds\":-1}}]}}}}"
		timeoutString := "{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"router\",\"livenessProbe\":{\"timeoutSeconds\":\"abc\"},\"readinessProbe\":{\"timeoutSeconds\":\"abc\"}}]}}}}"
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp50075",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("try to patch livenessProbe and readinessProbe with a minus number -1 to the router deployment")
		routerDeploymentName := "router-" + ingctrl.Name
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("deployment", routerDeploymentName, "--type=strategic", "--patch="+timeoutMinus, "-n", "openshift-ingress").Output()
		o.Expect(output).To(o.ContainSubstring("spec.template.spec.containers[0].livenessProbe.timeoutSeconds: Invalid value: -1: must be greater than or equal to 0"))
		o.Expect(output).To(o.ContainSubstring("spec.template.spec.containers[0].readinessProbe.timeoutSeconds: Invalid value: -1: must be greater than or equal to 0"))

		compat_otp.By("try to patch livenessProbe and readinessProbe with string type of value to the router deployment")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args("deployment", routerDeploymentName, "--type=strategic", "--patch="+timeoutString, "-n", "openshift-ingress").Output()
		o.Expect(output).To(o.ContainSubstring("The request is invalid: patch: Invalid value: \"map[spec:map[template:map[spec:map[containers:[map[livenessProbe:map[timeoutSeconds:abc] name:router readinessProbe:map[timeoutSeconds:abc]]]]]]]\": unrecognized type: int32"))
	})

	// port=yes - 99.4% pass rate (508 runs 60 days to Sep 1)
	g.It("Multiple routers with hostnetwork endpoint strategy can be deployed on same worker node with different http/https/stat port numbers", g.Label("OCP-50405"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")
			ingctrlhp1          = util.IngressControllerHostPortDescription{
				Name:      "ocp50405one",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				HTTPPort:  10080,
				HTTPSPort: 10443,
				StatsPort: 10936,
				Template:  customTemp,
			}

			ingctrlhp2 = util.IngressControllerHostPortDescription{
				Name:      "ocp50405two",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				HTTPPort:  11080,
				HTTPSPort: 11443,
				StatsPort: 11936,
				Template:  customTemp,
			}
			ingctrlResource1 = "ingresscontrollers/" + ingctrlhp1.Name
			ingctrlResource2 = "ingresscontrollers/" + ingctrlhp2.Name
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
		workerNodeCount, _ := util.ExactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we at least need one worker node")
		}

		compat_otp.By("Collect nodename of one of the default haproxy pods")
		defRouterPod := util.GetOneRouterPodNameByIC(oc, "default")
		defNodeName := util.GetNodeNameByPod(oc, ns, defRouterPod)

		compat_otp.By("Create two custom ingresscontrollers")
		baseDomain := util.GetBaseDomain(oc)
		ingctrlhp1.Domain = ingctrlhp1.Name + "." + baseDomain
		ingctrlhp2.Domain = ingctrlhp2.Name + "." + baseDomain

		defer ingctrlhp1.Delete(oc)
		ingctrlhp1.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrlhp1.Name, "1")

		defer ingctrlhp2.Delete(oc)
		ingctrlhp2.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrlhp2.Name, "1")

		compat_otp.By("Patch the two custom ingress-controllers with nodePlacement")
		patchSelectNode := "{\"spec\":{\"nodePlacement\":{\"nodeSelector\":{\"matchLabels\":{\"kubernetes.io/hostname\": \"" + defNodeName + "\"}}}}}"
		err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource1, "-p", patchSelectNode, "--type=merge", "-n", ingctrlhp1.Namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource2, "-p", patchSelectNode, "--type=merge", "-n", ingctrlhp2.Namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsureRouterDeployGenerationIs(oc, ingctrlhp1.Name, "2")
		util.EnsureRouterDeployGenerationIs(oc, ingctrlhp2.Name, "2")

		compat_otp.By("Check the node names on which the route pods of the custom ingress-controllers reside on")
		routerPod1 := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrlhp1.Name)
		routerPod2 := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrlhp2.Name)
		routerNodeName1 := util.GetNodeNameByPod(oc, ns, routerPod1)
		routerNodeName2 := util.GetNodeNameByPod(oc, ns, routerPod2)
		o.Expect(defNodeName).Should(o.And(
			o.ContainSubstring(routerNodeName1),
			o.ContainSubstring(routerNodeName2)))

		compat_otp.By("Verify the http/https/statsport of the custom proxy pod")
		checkPodEnv := util.DescribePodResource(oc, routerPod1, "openshift-ingress")
		o.Expect(checkPodEnv).Should(o.And(
			o.ContainSubstring("ROUTER_SERVICE_HTTPS_PORT:                 10443"),
			o.ContainSubstring("ROUTER_SERVICE_HTTP_PORT:                  10080"),
			o.ContainSubstring("STATS_PORT:                                10936")))

	})

	// author: shudili@redhat.com
	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("The http/https/stat port field in the ingresscontroller does not accept negative values during configuration", g.Label("OCP-50406"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")
			ingctrlhp           = util.IngressControllerHostPortDescription{
				Name:      "ocp50406",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				HTTPPort:  10080,
				HTTPSPort: 10443,
				StatsPort: 10936,
				Template:  customTemp,
			}

			ingctrlResource = "ingresscontrollers/" + ingctrlhp.Name
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
		workerNodeCount, _ := util.ExactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we atleast need  one worker node")
		}

		compat_otp.By("Create a custom ingresscontrollers")
		baseDomain := util.GetBaseDomain(oc)
		ingctrlhp.Domain = ingctrlhp.Name + "." + baseDomain
		defer ingctrlhp.Delete(oc)
		ingctrlhp.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrlhp.Name, "1")

		compat_otp.By("Patch the the custom ingress-controllers with invalid hostNetwork configutations")
		jsonPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"httpPort\": -10090}}}}"
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", jsonPath, "--type=merge", "-n", ingctrlhp.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: -10090"))

		jsonPath = "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"httpsPort\": -11443}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", jsonPath, "--type=merge", "-n", ingctrlhp.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: -11443"))

		jsonPath = "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"statsPort\": -12936}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", jsonPath, "--type=merge", "-n", ingctrlhp.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: -12936"))
	})

	// author: shudili@redhat.com
	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("Routers with hostnetwork endpoint strategy with same http/https/stat port numbers cannot be deployed on the same worker node", g.Label("OCP-50819"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")
			ingctrlhp1          = util.IngressControllerHostPortDescription{
				Name:      "ocp50819one",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				HTTPPort:  10080,
				HTTPSPort: 10443,
				StatsPort: 10936,
				Template:  customTemp,
			}

			ingctrlhp2 = util.IngressControllerHostPortDescription{
				Name:      "ocp50819two",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				HTTPPort:  10080,
				HTTPSPort: 10433,
				StatsPort: 10936,
				Template:  customTemp,
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
		workerNodeCount, _ := util.ExactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we atleast need  one worker node")
		}

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrlhp1.Domain = ingctrlhp1.Name + "." + baseDomain
		ingctrlhp2.Domain = ingctrlhp2.Name + "." + baseDomain

		defer ingctrlhp1.Delete(oc)
		ingctrlhp1.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrlhp1.Name, "1")

		compat_otp.By("Patch the first custom IC with max replicas, so each node has a custom router pod ")
		jpath := "{.status.readyReplicas}"
		if workerNodeCount > 1 {
			ingctrl1Resource := "ingresscontrollers/" + ingctrlhp1.Name
			util.PatchResourceAsAdmin(oc, ingctrlhp1.Namespace, ingctrl1Resource, "{\"spec\":{\"replicas\":"+strconv.Itoa(workerNodeCount)+"}}")
			util.EnsureRouterDeployGenerationIs(oc, ingctrlhp1.Name, "2")
			util.WaitForOutputEquals(oc, "openshift-ingress", "deployment/router-"+ingctrlhp1.Name, jpath, strconv.Itoa(workerNodeCount))
		}

		compat_otp.By("Try to create another custom IC with the same http/https/stat port numbers as the first custom IC")
		defer ingctrlhp2.Delete(oc)
		ingctrlhp2.Create(oc)
		err := util.WaitForPodWithLabelAppear(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller=ocp50819two")
		compat_otp.AssertWaitPollNoErr(err, "router pod of the second custom IC does not appear  within allowed time!")
		customICRouterPod := util.GetPodListByLabel(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller=ocp50819two")
		checkPodMsg := util.GetByJsonPath(oc, "openshift-ingress", "pod/"+customICRouterPod[0], "{.status..message}")
		o.Expect(checkPodMsg).To(o.ContainSubstring("node(s) didn't have free ports for the requested pod ports"))
	})

	// port=yes - 99.6% pass rate (542 runs 60 days to Sep 1)
	g.It("The Power-of-two balancing features switches to source algorithm for passthrough routes", g.Label("OCP-52738"), func() {
		var (
			baseDomain = util.GetBaseDomain(oc)
			defaultPod = util.GetOneRouterPodNameByIC(oc, "default")
		)
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")

		compat_otp.By("Create pods and service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Expose a passthrough type routes via the services inside the namespace")
		passthRoute := "route-passth" + "-" + ns + "." + baseDomain
		util.CreateRoute(oc, ns, "passthrough", "route-passth", "service-secure", []string{"--hostname=" + passthRoute})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-passth"))

		compat_otp.By("Check the default loadbalance algorithm inside proxy pod and check the default LB variable to confirm power-of-two is active")
		rtrParamCheck := util.ReadPodEnv(oc, defaultPod, "openshift-ingress", "ROUTER_LOAD_BALANCE_ALGORITHM")
		o.Expect(rtrParamCheck).To(o.ContainSubstring("random"))
		passthBackend := "be_tcp:" + ns + ":route-passth"
		util.EnsureHaproxyBlockConfigContains(oc, defaultPod, passthBackend, []string{"balance source"})
	})

	// author: shudili@redhat.com
	// port=yes - 99.2% pass rate (508 runs 60 days to Sep 1)
	g.It("Ingresscontroller with private endpoint publishing strategy supports PROXY protocol", g.Label("OCP-53048"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-private.yaml")
			ingctrl             = util.IngressControllerDescription{
				Name:      "53048",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.Name
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("check the default value of .status.endpointPublishingStrategy.private.protocol, which should be TCP")
		jpath := "{.status.endpointPublishingStrategy.private.protocol}"
		protocol := util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring("TCP"))

		compat_otp.By("patch the custom ingresscontroller with protocol proxy")
		patchPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"private\":{\"protocol\":\"PROXY\"}}}}"
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("check the changed value of .endpointPublishingStrategy.private.protocol, which should be PROXY")
		jpath = "{.spec.endpointPublishingStrategy.private.protocol}{.status.endpointPublishingStrategy.private.protocol}"
		protocol = util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring("PROXYPROXY"))

		compat_otp.By("check the custom ingresscontroller's status, which should indicate that PROXY protocol is enabled")
		jsonPath := "{.status.endpointPublishingStrategy}"
		status := util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jsonPath)
		o.Expect(status).To(o.ContainSubstring(`{"private":{"protocol":"PROXY"},"type":"Private"}`))

		compat_otp.By("check the private deployment, which should have PROXY protocol enabled")
		jsonPath = `{.spec.template.spec.containers[0].env[?(@.name=="ROUTER_USE_PROXY_PROTOCOL")]}`
		proxyProtocol := util.GetByJsonPath(oc, "openshift-ingress", "deployments/router-"+ingctrl.Name, jsonPath)
		o.Expect(proxyProtocol).To(o.ContainSubstring(`{"name":"ROUTER_USE_PROXY_PROTOCOL","value":"true"}`))

		compat_otp.By("check the ROUTER_USE_PROXY_PROTOCOL env, which should be true")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		proxyEnv := util.ReadRouterPodEnv(oc, routerpod, "ROUTER_USE_PROXY_PROTOCOL")
		o.Expect(proxyEnv).To(o.ContainSubstring("ROUTER_USE_PROXY_PROTOCOL=true"))

		compat_otp.By("check the accept-proxy in haproxy.config of a router pod")
		bindCfg, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "cat haproxy.config | grep \"bind :\"").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		IPStackType := util.CheckIPStackType(oc)
		if IPStackType == "ipv4single" {
			o.Expect(strings.Count(bindCfg, "bind :")).To(o.Equal(2))
			util.EnsureHaproxyBlockConfigContains(oc, routerpod, "bind :80 accept-proxy", []string{"bind :80 accept-proxy"})
			util.EnsureHaproxyBlockConfigContains(oc, routerpod, "bind :443 accept-proxy", []string{"bind :443 accept-proxy"})
		} else if IPStackType == "ipv6single" {
			o.Expect(strings.Count(bindCfg, "bind :")).To(o.Equal(2))
			util.EnsureHaproxyBlockConfigContains(oc, routerpod, "bind :::80 v6only accept-proxy", []string{"bind :::80 v6only accept-proxy"})
			util.EnsureHaproxyBlockConfigContains(oc, routerpod, "bind :::443 v6only accept-proxy", []string{"bind :::443 v6only accept-proxy"})
		} else if IPStackType == "dualstack" {
			o.Expect(strings.Count(bindCfg, "bind :")).To(o.Equal(4))
			util.EnsureHaproxyBlockConfigContains(oc, routerpod, "bind :80 accept-proxy", []string{"bind :80 accept-proxy"})
			util.EnsureHaproxyBlockConfigContains(oc, routerpod, "bind :443 accept-proxy", []string{"bind :443 accept-proxy"})
			util.EnsureHaproxyBlockConfigContains(oc, routerpod, "bind :::80 v6only accept-proxy", []string{"bind :::80 v6only accept-proxy"})
			util.EnsureHaproxyBlockConfigContains(oc, routerpod, "bind :::443 v6only accept-proxy", []string{"bind :::443 v6only accept-proxy"})
		}
	})

	// Bug: 2044682
	// port=yes - 99.1% pass rate (542 runs 60 days to Sep 1)
	g.It("Set Cookie2 by an application in a route should not kill all router pods", g.Label("OCP-54998"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = util.IngressControllerDescription{
				Name:      "54998",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("get one custom ingress-controller router pod's IP")
		podname := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, podname, "openshift-ingress")

		compat_otp.By("create an unsecure service and its backend pod")
		ns := oc.Namespace()
		sedCmd := fmt.Sprintf(`sed -i'' -e 's/8080/10081/g' %s`, testPodSvc)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("start the service on the backend server port 10081 by socat command")
		jsonPath := "{.items[0].metadata.name}"
		srvPodName := util.GetByJsonPath(oc, ns, "pods", jsonPath)
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
		routehost := srvName + "-" + ns + "." + ingctrl.Domain
		output, SrvErr := oc.Run("expose").Args("service", srvName, "--hostname="+routehost).Output()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(srvName))

		compat_otp.By("create a client pod to send traffic")
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("curl the route from the client pod")
		toDst := routehost + ":80:" + podIP
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-I", "http://" + routehost, "--resolve", toDst, "--connect-timeout", "10"}
		result, _ := util.RepeatCmdOnClient(oc, cmdOnPod, "Set-Cookie2 X=Y", 60, 1)
		o.Expect(result).To(o.ContainSubstring("Set-Cookie2 X=Y"))
	})

	// Bug: 1967228
	// port=yes - 99.4% pass rate (508 runs 60 days to Sep 1)
	g.It("the 503 Error page should not contain license for a vulnerable release of Bootstrap", g.Label("OCP-55825"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = util.IngressControllerDescription{
				Name:      "55825",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Create a client pod used to send traffic")
		ns := oc.Namespace()
		compat_otp.By("create a client pod")
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("curl a non-existing route, and then check that Bootstrap portion of the license is removed")
		podname := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, podname, "openshift-ingress")
		notExistRoute := "notexistroute" + "-" + ns + "." + ingctrl.Domain
		toDst := notExistRoute + ":80:" + podIP
		output, err2 := oc.Run("exec").Args(clientPodName, "--", "curl", "-sS", "-i", "http://"+notExistRoute, "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("503"))
		o.Expect(output).ShouldNot(o.Or(
			o.ContainSubstring("Bootstrap"),
			o.ContainSubstring("Copyright 2011-2015 Twitter"),
			o.ContainSubstring("Licensed under MIT"),
			o.ContainSubstring("normalize.css v3.0.3")))

	})

	// author: mjoseph@redhat.com
	// port=yes - 99.4% pass rate (542 runs 60 days to Sep 1)
	g.It("Accessing the route should wake up the idled resources", g.Label("OCP-56898"), func() {
		// This test case works only on OVN or SDN network type
		networkType := compat_otp.CheckNetworkType(oc)
		if !(strings.Contains(networkType, "openshiftsdn") || strings.Contains(networkType, "ovn")) {
			g.Skip(fmt.Sprintf("Skipping because idling is not supported on the '%s' network type", networkType))
		}
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp56898",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")
		custContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		custContIP := util.GetPodv4Address(oc, custContPod, "openshift-ingress")

		compat_otp.By("Deploy a backend pod and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("Create a client pod")
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "service-unsecure-" + ns + "." + ingctrl.Domain
		SrvErr := oc.Run("expose").Args("svc/service-unsecure", "--hostname="+routehost).Execute()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		routeOutput := util.GetRoutes(oc, ns)
		o.Expect(routeOutput).To(o.ContainSubstring("service-unsecure"))

		compat_otp.By("Check the router pod and ensure the routes are loaded in haproxy.config")
		haproxyOutput := util.PollReadPodData(oc, "openshift-ingress", custContPod, "cat haproxy.config", "service-unsecure")
		o.Expect(haproxyOutput).To(o.ContainSubstring("backend be_http:" + ns + ":service-unsecure"))

		compat_otp.By("Check the reachability of the insecure route")
		util.WaitForCurl(oc, clientPodName, baseDomain, "service-unsecure-"+ns+"."+"ocp56898.", "HTTP/1.1 200 OK", custContIP)

		compat_otp.By("Idle the insecure service")
		idleOutput, err := oc.AsAdmin().WithoutNamespace().Run("idle").Args("service-unsecure", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(idleOutput).To(o.ContainSubstring("The service \"" + ns + "/service-unsecure\" has been marked as idled"))

		compat_otp.By("Verify the Idle annotation")
		findAnnotation := util.GetAnnotation(oc, ns, "svc", "service-unsecure")
		o.Expect(findAnnotation).To(o.ContainSubstring("idling.alpha.openshift.io/idled-at"))
		o.Expect(findAnnotation).To(o.ContainSubstring(`idling.alpha.openshift.io/unidle-targets":"[{\"kind\":\"Deployment\",\"name\":\"web-server-deploy\",\"group\":\"apps\",\"replicas\":1}]`))

		compat_otp.By("Wake the Idle resource by accessing its route")
		util.WaitForCurl(oc, clientPodName, baseDomain, "service-unsecure-"+ns+"."+"ocp56898.", "HTTP/1.1 200 OK", custContIP)

		compat_otp.By("Confirm the Idle annotation got removed")
		findAnnotation = util.GetAnnotation(oc, ns, "svc", "service-unsecure")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring("idling.alpha.openshift.io/idled-at"))
	})

	// bug: 1826225
	// port=yes - 99.8% pass rate (508 runs 60 days to Sep 1)
	g.It("edge terminated h2 (gRPC) connections need a haproxy template change to work correctly", g.Label("OCP-57001"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			srvPodSvc           = filepath.Join(buildPruningBaseDir, "bug1826225-proh2-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			svcName             = "service-h2c-57001"
			routeName           = "myedge1"
		)

		compat_otp.By("Create a backend pod and its service resources")
		ns := oc.Namespace()
		compat_otp.By("create a h2c service and its backend pod")
		util.CreateResourceFromFile(oc, ns, srvPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("Create an edge route with the h2c service inside the namespace")
		output, routeErr := oc.AsAdmin().WithoutNamespace().Run("create").Args("route", "edge", routeName, "--service="+svcName, "-n", ns).Output()
		o.Expect(routeErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(routeName))

		compat_otp.By("Check the Haproxy backend configuration and make sure proto h2 is added for the route")
		podname := util.GetOneRouterPodNameByIC(oc, "default")
		backendConfig := util.PollReadPodData(oc, "openshift-ingress", podname, "cat haproxy.config", svcName)
		o.Expect(backendConfig).To(o.ContainSubstring("proto h2"))
	})

	// bugzilla: 1976894
	// port=yes - 99.4% pass rate (508 runs 60 days to Sep 1)
	g.It("Idling StatefulSet is not supported", g.Label("OCP-57404"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "ocp57404-stateful-set.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp57404",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")
		custContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		custContIP := util.GetPodv4Address(oc, custContPod, "openshift-ingress")

		compat_otp.By("Deploy the statefulset and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "app=echoenv-sts")

		compat_otp.By("Create a client pod")
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, "app=hello-pod")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "echoenv-statefulset-service-" + ns + "." + ingctrl.Domain
		SrvErr := oc.Run("expose").Args("svc/echoenv-statefulset-service", "--hostname="+routehost).Execute()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		routeOutput := util.GetRoutes(oc, ns)
		o.Expect(routeOutput).To(o.ContainSubstring("echoenv-statefulset-service"))

		compat_otp.By("Check the reachability of the insecure route")
		util.WaitForCurl(oc, "hello-pod", baseDomain, "echoenv-statefulset-service-"+ns+"."+"ocp57404.", "HTTP/1.1 200 OK", custContIP)

		compat_otp.By("Trying to idle the statefulset-service")
		idleOutput, _ := oc.AsAdmin().WithoutNamespace().Run("idle").Args("echoenv-statefulset-service", "-n", ns).Output()
		o.Expect(idleOutput).To(o.ContainSubstring("idling StatefulSet is not supported yet"))

		compat_otp.By("Verify the Idle annotation is not present")
		findAnnotation := util.GetAnnotation(oc, ns, "svc", "echoenv-statefulset-service")
		o.Expect(findAnnotation).NotTo(o.ContainSubstring("idling.alpha.openshift.io/idled-at"))

		compat_otp.By("Recheck the reachability of the insecure route")
		util.WaitForCurl(oc, "hello-pod", baseDomain, "echoenv-statefulset-service-"+ns+"."+"ocp57404.", "HTTP/1.1 200 OK", custContIP)
	})

	// bugzilla: 1941592 1859134
	// port=yes - 99.6% pass rate (508 runs 60 days to Sep 1)
	g.It("HAProxyDown message only for pods and No reaper messages for zombie processes", g.Label("OCP-57406"), func() {
		compat_otp.By("Verify there will be precise message pointing to the  router nod, when HAProxy is down")
		output := util.GetByJsonPath(oc, "openshift-ingress-operator", "PrometheusRule", `{.items[0].spec.groups[0].rules[?(@.alert=="HAProxyDown")].annotations.message}`)
		o.Expect(output).To(o.ContainSubstring(`HAProxy metrics are reporting that HAProxy is down on pod {{ $labels.namespace }} / {{ $labels.pod }}`))

		compat_otp.By("Check the router pod logs and confirm there is no periodic reper error message  for zombie process")
		podname := util.GetOneRouterPodNameByIC(oc, "default")
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", podname).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Contains(log, "waitid: no child processes") {
			e2e.Failf("waitid: no child processes generated")
		}
	})

	// author: mjoseph@redhat.com
	// port=yes - 100.0% pass rate (508 runs 60 days to Sep 1)
	g.It("IngressController option to use PROXY protocol with IBM Cloud load-balancers - TCP, PROXY and omitted", g.Label("OCP-59951"), func() {
		// This test case is only for IBM cluster
		compat_otp.SkipIfPlatformTypeNot(oc, "IBMCloud")
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-IBMproxy.yaml")
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp59951",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.Name
		)

		compat_otp.By("create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("check the value of .status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol, which should be PROXY")
		jpath := "{.status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol}"
		protocol := util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring("PROXY"))

		compat_otp.By("check the ROUTER_USE_PROXY_PROTOCOL env, which should be true")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.PollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_USE_PROXY_PROTOCOL=true`)

		compat_otp.By("Ensure the proxy-protocol annotation is added to the LB service")
		findAnnotation, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("svc", "router-ocp59951", "-n", "openshift-ingress", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(findAnnotation).To(o.ContainSubstring(`"service.kubernetes.io/ibm-load-balancer-cloud-provider-enable-features":"proxy-protocol"`))

		compat_otp.By("patch the custom ingresscontroller with protocol option TCP")
		patchPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"providerParameters\":{\"ibm\":{\"protocol\":\"TCP\"}}}}}}"
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)

		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		compat_otp.By("check the value of .status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol, which should be TCP")
		jpath = "{.status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol}"
		protocol = util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring("TCP"))

		compat_otp.By("check the ROUTER_USE_PROXY_PROTOCOL env, which should not present")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		proxyEnv, _ := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "/usr/bin/env | grep ROUTER_USE_PROXY_PROTOCOL").Output()
		o.Expect(proxyEnv).NotTo(o.ContainSubstring("ROUTER_USE_PROXY_PROTOCOL"))

		compat_otp.By("patch the custom ingresscontroller with protocol option omitted")
		patchPath = `{"spec":{"endpointPublishingStrategy":{"loadBalancer":{"providerParameters":{"ibm":{"protocol":""}}}}}}`
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)

		compat_otp.By(`check the value of .status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm.protocol, which should be ""`)
		jpath = "{.status.endpointPublishingStrategy.loadBalancer.providerParameters.ibm}"
		protocol = util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, jpath)
		o.Expect(protocol).To(o.ContainSubstring(`{}`))
	})

	// OCPBUGS-4573
	// port=yes - 99.8% pass rate (542 runs 60 days to Sep 1)
	g.It("Ingress controller stats port is not set according to endpointPublishingStrategy", g.Label("OCP-62926"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")
			ingctrlhp           = util.IngressControllerHostPortDescription{
				Name:      "ocp62926",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				HTTPPort:  16080,
				HTTPSPort: 16443,
				StatsPort: 16936,
				Template:  customTemp,
			}

			ingctrlResource = "ingresscontrollers/" + ingctrlhp.Name
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
		workerNodeCount, _ := util.ExactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we atleast need one worker node")
		}

		compat_otp.By("Create a custom ingress-controller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrlhp.Domain = ingctrlhp.Name + "." + baseDomain
		defer ingctrlhp.Delete(oc)
		ingctrlhp.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrlhp.Name, "1")

		compat_otp.By("Patch the the custom ingress-controller with httpPort 17080, httpsPort 17443 and statsPort 17936")
		jsonPath := "{\"spec\":{\"endpointPublishingStrategy\":{\"hostNetwork\":{\"httpPort\":17080, \"httpsPort\":17443, \"statsPort\":17936}}}}"
		util.PatchResourceAsAdmin(oc, ingctrlhp.Namespace, ingctrlResource, jsonPath)
		util.EnsureRouterDeployGenerationIs(oc, ingctrlhp.Name, "2")

		compat_otp.By("Check STATS_PORT env under a custom router pod, which should be 17936")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrlhp.Name)
		jsonPath = `{.spec.containers[].env[?(@.name=="STATS_PORT")].value}`
		output := util.GetByJsonPath(oc, "openshift-ingress", "pod/"+routerpod, jsonPath)
		o.Expect(output).To(o.ContainSubstring("17936"))

		compat_otp.By("Check http/https/metrics ports under a custom router pod, which should be 17080/17443/17936")
		jsonPath = `{.spec.containers[].ports[?(@.name=="http")].hostPort}-{.spec.containers[].ports[?(@.name=="https")].hostPort}-{.spec.containers[].ports[?(@.name=="metrics")].hostPort}`
		output = util.GetByJsonPath(oc, "openshift-ingress", "pod/"+routerpod, jsonPath)
		o.Expect(output).To(o.ContainSubstring("17080-17443-17936"))

		compat_otp.By("Check the custom router-internal service, make sure the targetPort of the metrics port is changed to metrics instead of port number 1936")
		jsonPath = `{.spec.ports[?(@.name=="metrics")].targetPort}`
		output = util.GetByJsonPath(oc, "openshift-ingress", "service/router-internal-"+ingctrlhp.Name, jsonPath)
		o.Expect(output).To(o.ContainSubstring("metrics"))

		compat_otp.By("Check http/https/metrics ports under the router endpoints, which should be 17080/17443/17936")
		jsonPath = `{.subsets[].ports[?(@.name=="http")].port}-{.subsets[].ports[?(@.name=="https")].port}-{.subsets[].ports[?(@.name=="metrics")].port}`
		output = util.GetByJsonPath(oc, "openshift-ingress", "endpoints/router-internal-"+ingctrlhp.Name, jsonPath)
		o.Expect(output).To(o.ContainSubstring("17080-17443-17936"))
	})

	// author: hongli@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43745 and https://issues.redhat.com/browse/OCPBUGS-43811
	// port=yes - 99.1% pass rate (542 runs 60 days to Sep 1)
	g.It("option idle-close-on-response is configurable", g.Label("OCP-79514"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp79514",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.Name
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		routerPod := util.GetOneRouterPodNameByIC(oc, ingctrl.Name)

		compat_otp.By("Verify default spec.idleConnectionTerminationPolicy is Immediate in 4.19+")
		output := util.GetByJsonPath(oc, ingctrl.Namespace, ingctrlResource, `{.spec.idleConnectionTerminationPolicy}`)
		o.Expect(output).To(o.ContainSubstring("Immediate"))

		compat_otp.By("Verify no variable ROUTER_IDLE_CLOSE_ON_RESPONSE in deployed router pod")
		checkEnv := util.ReadRouterPodEnv(oc, routerPod, "ROUTER_IDLE_CLOSE_ON_RESPONSE")
		o.Expect(checkEnv).To(o.ContainSubstring("NotFound"))

		compat_otp.By("Verify no option idle-close-on-response in haproxy.config")
		_, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerPod, "--", "grep", "idle-close-on-response", "haproxy.config").Output()
		o.Expect(err).To(o.HaveOccurred())

		compat_otp.By("Patch custom ingresscontroller spec.idleConnectionTerminationPolicy with Deferred")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, `{"spec":{"idleConnectionTerminationPolicy":"Deferred"}}`)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		routerPod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("Verify variable ROUTER_IDLE_CLOSE_ON_RESPONSE of the deployed router pod")
		checkEnv = util.ReadRouterPodEnv(oc, routerPod, "ROUTER_IDLE_CLOSE_ON_RESPONSE")
		o.Expect(checkEnv).To(o.ContainSubstring(`ROUTER_IDLE_CLOSE_ON_RESPONSE=true`))

		compat_otp.By("Check the router haproxy.config for option idle-close-on-response")
		output = util.ReadRouterPodData(oc, routerPod, "cat haproxy.config", "idle-close-on-response")
		o.Expect(strings.Count(output, "option idle-close-on-response")).To(o.Equal(3))
	})

	// author: shudili@redhat.com
	// port=yes - 99.3% pass rate (542 runs 60 days to Sep 1)
	g.It("Support Prometheus monitoring metric for router max connections", g.Label("OCP-88325"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp88325",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.Name
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		routerPod := util.GetOneRouterPodNameByIC(oc, ingctrl.Name)

		compat_otp.By("2.0: Check the default haproxy_max_connections metrics in Prometheus")
		searchReg := fmt.Sprintf(`pod.+%s.+service.+router-internal-ocp88325`, routerPod)
		expReg := fmt.Sprintf(`pod.+%s.+service.+router-internal-ocp88325.+value.+%s`, routerPod, "50000")
		token, err := util.GetSAToken(oc, "prometheus-k8s", "openshift-monitoring")
		o.Expect(err).NotTo(o.HaveOccurred())
		metric := util.WaitForPrometheusMetricsMatchRegexp(oc, token, "haproxy_max_connections", searchReg)
		o.Expect(metric).To(o.MatchRegexp(expReg))

		compat_otp.By("3.0: Patch tuningOptions/maxConnections with 2000 to IC ocp88325")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\": {\"tuningOptions\": {\"maxConnections\": 2000}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		routerPod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("4.0: Check the haproxy_max_connections metrics in Prometheus, the stat of the router should be 2000")
		searchReg = fmt.Sprintf(`pod.+%s.+service.+router-internal-ocp88325`, routerPod)
		expReg = fmt.Sprintf(`pod.+%s.+service.+router-internal-ocp88325.+value.+%s`, routerPod, "2000")
		metric = util.WaitForPrometheusMetricsMatchRegexp(oc, token, "haproxy_max_connections", searchReg)
		o.Expect(metric).To(o.MatchRegexp(expReg))
	})
})
