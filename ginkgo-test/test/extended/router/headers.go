package router

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	e2e "k8s.io/kubernetes/test/e2e/framework"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-headers", compat_otp.KubeConfigPath())

	// author: shudili@redhat.com
	// incorporate OCP-34157 and OCP-34163 into one
	// OCP-34157 [HAProxy-frontend-capture] capture and log specific http Request header via "httpCaptureHeaders" option
	// OCP-34163 [HAProxy-frontend-capture] capture and log specific http Response headers via "httpCaptureHeaders" option
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-ConnectedOnly-Critical-34157-NetworkEdge capture and log specific http Request header via httpCaptureHeaders option", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
		unsecsvcName := "httpbin-svc-insecure"
		clientPod := filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
		clientPodName := "hello-pod"
		clientPodLabel := "app=hello-pod"
		srv := "gunicorn"

		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    logging:
      access:
        destination:
          type: Container
        httpCaptureHeaders:
          request:
          - name:  Host
            maxLength: 100
          response:
          - name: Server
            maxLength: 100
`)

		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		ingctrl := ingressControllerDescription{
			name:      "ocp34157",
			namespace: "openshift-ingress-operator",
			domain:    "",
			template:  customTemp,
		}

		compat_otp.By("1.0 Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("2.0 Deploy a project with a client pod, a backend pod and its service resources")
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, clientPod)
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("3.0 Create a http route, and then curl the route")
		routehost := unsecsvcName + "34157" + ".apps." + getBaseDomain(oc)
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", project1, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-I", "--resolve", toDst, "--connect-timeout", "10"}
		createRoute(oc, project1, "http", unsecsvcName, unsecsvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, project1, unsecsvcName, "default")
		repeatCmdOnClient(oc, curlCmd, "200", 60, 1)

		// check for OCP-34157
		compat_otp.By("4.0: check the log which should contain the host")
		waitRouterLogsAppear(oc, routerpod, routehost)

		// check for OCP-34163
		compat_otp.By("5.0: check the log which should contain the backend server info")
		waitRouterLogsAppear(oc, routerpod, srv)
	})

	// includes OCP-34231 and OCP-34247
	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-34231-Configure Ingresscontroller to preserve existing header with forwardedHeaderPolicy set to Append", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    httpHeaders:
      forwardedHeaderPolicy: Append
`)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			testPodSvc            = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecSvcName          = "httpbin-svc-insecure"
			clientPod             = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName         = "hello-pod"
			clientPodLabel        = "app=hello-pod"
			exampleXForwardedHost = "www.example-ne.com"
			ingctrl               = ingressControllerDescription{
				name:      "ocp34231",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create an custom IC with forwardedHeaderPolicy Append")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("3.0: Create a http route for testing OCP-34231")
		routehost := "unsecure34231" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		compat_otp.By("4.0: Check the forwardedHeaderPolicy env, which should be append")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		pollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=append`)

		compat_otp.By("5.0: Curl the http route with a specified X-Forwarded-Host, then check the X-Forwarded-Host header value, which should contain both the exampleXForwardedHost and routehost")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ := repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s,%s"`, exampleXForwardedHost, routehost)))

		// OCP-34247(Different Routes can have different policy with haproxy.router.openshift.io/set-forwarded-headers annotations)
		compat_otp.By("6.0: Create two http routes for testing OCP-34247")
		routehostOCP34247a := "unsecure34247a" + "." + ingctrl.domain
		routehostOCP34247b := "unsecure34247b" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http34247a", unsecSvcName, []string{"--hostname=" + routehostOCP34247a})
		createRoute(oc, ns, "http", "route-http34247b", unsecSvcName, []string{"--hostname=" + routehostOCP34247b})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http34247a", ingctrl.name)
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http34247b", ingctrl.name)

		compat_otp.By("7.0 Add the set-forwarded-headers annotations to the two routes, one with replace, the other with never, then check it in haproxy")
		setAnnotation(oc, ns, "route/route-http34247a", "haproxy.router.openshift.io/set-forwarded-headers=replace")
		setAnnotation(oc, ns, "route/route-http34247b", "haproxy.router.openshift.io/set-forwarded-headers=never")
		backend1Start := "backend be_http:" + ns + ":route-http34247a"
		backend2Start := "backend be_http:" + ns + ":route-http34247b"
		ensureHaproxyBlockConfigContains(oc, routerpod, backend1Start, []string{"http-request set-header X-Forwarded-Host %[req.hdr(host)]"})
		backend2Cfg := getBlockConfig(oc, routerpod, backend2Start)
		o.Expect(backend2Cfg).NotTo(o.ContainSubstring(`http-request set-header X-Forwarded-Host`))

		compat_otp.By("8.0: Curl the replace annotation http route with a specified X-Forwarded-Host, then check the header value which should be replaced by routehostOCP34247a")
		toDst = routehostOCP34247a + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehostOCP34247a + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s"`, routehostOCP34247a)))

		compat_otp.By("9.0: Curl the never annotation http route, then check the http headers which should not contain the X-Forwarded-Host header")
		toDst = routehostOCP34247b + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehostOCP34247b + "/headers", "-sv", "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).NotTo(o.ContainSubstring(`X-Forwarded-Host`))
	})

	// includes OCP-34233 and OCP-34246
	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-34233-Configure Ingresscontroller to replace any existing Forwarded header with forwardedHeaderPolicy set to Replace", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    httpHeaders:
      forwardedHeaderPolicy: Replace
`)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			testPodSvc            = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecSvcName          = "httpbin-svc-insecure"
			clientPod             = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName         = "hello-pod"
			clientPodLabel        = "app=hello-pod"
			exampleXForwardedHost = "www.example-ne.com"
			ingctrl               = ingressControllerDescription{
				name:      "ocp34233",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create an custom IC with forwardedHeaderPolicy Replace")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("3.0: Create a http route for testing OCP-34233")
		routehost := "unsecure34233" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		compat_otp.By("4.0: Check the forwardedHeaderPolicy env, which should be replace")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		pollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=replace`)

		compat_otp.By("5.0: Curl the http route, then check the X-Forwarded-Host header value: exampleXForwardedHost should be replaced by routehost")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ := repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s"`, routehost)))

		// OCP-34246(Configure a different header policy for the route with haproxy.router.openshift.io/set-forwarded-headers annotations)
		compat_otp.By("6.0: Create a http route for the testing OCP-34246")
		routehost = "unsecure34246" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http34246", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http34246", ingctrl.name)

		compat_otp.By("7.0 Add set-forwarded-headers=if-none annotation to the route, then check it in haproxy")
		setAnnotation(oc, ns, "route/route-http34246", "haproxy.router.openshift.io/set-forwarded-headers=if-none")
		backendStart := "be_http:" + ns + ":route-http34246"
		ensureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{"option forwardfor if-none"})

		// For if-none, if the http request already has the X-Forwarded-Host header, cluster won't append or replace with its info
		compat_otp.By("8.0: Curl the http route with a specified X-Forwarded-Host, then check the X-Forwarded-Host header value, which should not be replaced by routehost or not be appended by routehost")
		toDst = routehost + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s"`, exampleXForwardedHost)))

		// For if-none, if the http request has not the X-Forwarded-Proto-Version header, cluster will create it
		compat_otp.By("9.0: Curl the http route with a specified X-Forwarded-Proto-Version, then check the header value is added")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Proto-Version: http2", "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Proto-Version": "%s"`, "http2")))
	})

	// includes OCP-34234, OCP-34235 and OCP-34236
	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-34234-Configure Ingresscontroller to set the headers if they are not already set with forwardedHeaderPolicy set to Ifnone", func() {
		// OCP-34236(forwardedHeaderPolicy option defaults to Append if none is defined in the ingresscontroller configuration)
		var (
			buildPruningBaseDir   = compat_otp.FixturePath("testdata", "router")
			baseTemp              = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc            = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecSvcName          = "httpbin-svc-insecure"
			clientPod             = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName         = "hello-pod"
			clientPodLabel        = "app=hello-pod"
			exampleXForwardedHost = "www.example-ne.com"
			ingctrl               = ingressControllerDescription{
				name:      "ocp34236",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		compat_otp.By("1.0 Create an custom IC with default forwardedHeaderPolicy Append")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("3.0: Create a http route for the testing")
		routehost := "unsecure34236" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		compat_otp.By("4.0: Check httpCaptureHeaders configuration in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		pollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=append`)

		compat_otp.By("5.0: Curl the http route with specified X-Forwarded-Host, then check the X-Forwarded-Host header value, which should contain both the exampleXForwardedHost and routehost")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ := repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s,%s"`, exampleXForwardedHost, routehost)))

		// OCP-34234(Configure Ingresscontroller to set the headers if they are not already set with forwardedHeaderPolicy set to Ifnone)
		compat_otp.By("6.0 Patch the httpHeaders with forwardedHeaderPolicy IfNone")
		patchPath := `{"spec":{"httpHeaders":{"forwardedHeaderPolicy":"IfNone"}}}`
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("7.0: Check httpCaptureHeaders configuration in haproxy")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		pollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=if-none`)

		// For if-none, if the http request already has the X-Forwarded-Host header, cluster won't append or replace with its info
		compat_otp.By("8.0: Curl the http route with a specified X-Forwarded-Host, then check the X-Forwarded-Host header value, which should not be replaced by routehost or not be appended by routehost")
		podIP = getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s"`, exampleXForwardedHost)))

		// For if-none, if the http request has not the X-Forwarded-Host header, cluster will create it
		compat_otp.By("9.0: Curl the http route with a specified X-Forwarded-Proto-Version, then check the header value is added")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Proto-Version: http2", "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Proto-Version": "%s"`, "http2")))

		// OCP-34235(Configure Ingresscontroller to never set the headers and preserve existing with forwardedHeaderPolicy set to Never)
		compat_otp.By("10.0 Patch the httpHeaders with forwardedHeaderPolicy Never")
		patchPath = `{"spec":{"httpHeaders":{"forwardedHeaderPolicy":"Never"}}}`
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		compat_otp.By("11.0: Check httpCaptureHeaders configuration in haproxy")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		pollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=never`)

		compat_otp.By("12.0: Curl the http route with a specified X-Forwarded-Host, then check the http headers which should not contain it")
		podIP = getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = repeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).NotTo(o.ContainSubstring(`X-Forwarded-Host`))
	})

	// bug: 1816540 1803001 1816544
	g.It("Author:shudili-High-57012-Forwarded header includes empty quoted proto-version parameter", func() {
		compat_otp.By("Check haproxy-config.template file in a router pod and make sure proto-version is removed from the Forwarded header")
		podname := getOneRouterPodNameByIC(oc, "default")
		templateConfig := readRouterPodData(oc, podname, "cat haproxy-config.template", "http-request add-header Forwarded")
		o.Expect(templateConfig).To(o.ContainSubstring("proto"))
		o.Expect(templateConfig).NotTo(o.ContainSubstring("proto-version"))

		compat_otp.By("Check proto-version is also removed from the haproxy.config file in a router pod")
		haproxyConfig := readRouterPodData(oc, podname, "cat haproxy.config", "proto")
		o.Expect(haproxyConfig).NotTo(o.ContainSubstring("proto-version"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-ConnectedOnly-High-62528-adding/deleting http headers to an edge route by a router owner", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			ingctrl             = ingressControllerDescription{
				name:      "ocp62528",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
			podFileDir      = "/data/OCP-62528-ca"
			podCaCert       = podFileDir + "/62528-ca.pem"
			podCustomKey    = podFileDir + "/user62528.key"
			podCustomCert   = podFileDir + "/user62528.pem"
			fileDir         = "/tmp/OCP-62528-ca"
			dirname         = "/tmp/OCP-62528-ca/"
			name            = dirname + "62528"
			validity        = 30
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = dirname + "user62528"
			customKey       = userCert + ".key"
			customCert      = userCert + ".pem"
		)
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain

		compat_otp.By("Try to create custom key and custom certification by openssl, create a new self-signed CA at first, creating the CA key")
		opensslCmd := fmt.Sprintf(`openssl genrsa -out %s-ca.key 2048`, name)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create the CA certificate")
		opensslCmd = fmt.Sprintf(`openssl req -x509 -new -nodes -key %s-ca.key -sha256 -days %d -out %s-ca.pem  -subj %s`, name, validity, name, caSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a new user certificate, crearing the user CSR with the private user key")
		userSubj := "/CN=example-ne.com"
		opensslCmd = fmt.Sprintf(`openssl req -nodes -newkey rsa:2048 -keyout %s.key -subj %s -out %s.csr`, userCert, userSubj, userCert)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Sign the user CSR and generate the certificate")
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.`+ingctrl.domain+`") -req -in %s.csr -CA %s-ca.pem -CAkey %s-ca.key -CAcreateserial -out %s.pem -days %d -sha256`, userCert, name, name, userCert, validity)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a custom ingresscontroller")
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		cmFile := "ca-bundle.pem=" + name + "-ca.pem"
		defer deleteConfigMap(oc, "openshift-config", "client-ca-"+ingctrl.name)
		createConfigMapFromFile(oc, "openshift-config", "client-ca-"+ingctrl.name, cmFile)

		compat_otp.By("patch the ingresscontroller to enable client certificate with required policy")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-"+ingctrl.name+"\"},\"clientCertificatePolicy\":\"Required\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Deploy a project with a client pod, a backend pod and its service resources")
		project1 := oc.Namespace()
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", project1, fileDir, project1+"/"+clientPodName+":"+podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("create an edge route")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		edgeRouteHost := "r3-edge62528." + ingctrl.domain
		lowHostEdge := strings.ToLower(edgeRouteHost)
		base64HostEdge := base64.StdEncoding.EncodeToString([]byte(edgeRouteHost))
		edgeRouteDst := edgeRouteHost + ":443:" + podIP
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "route", "edge", "r3-edge", "--service="+unsecsvcName, "--cert="+customCert, "--key="+customKey, "--ca-cert="+name+"-ca.pem", "--hostname="+edgeRouteHost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Patch the edge route with added/deleted http request/response headers under the spec")
		patchHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [" +
			"{\"name\": \"X-SSL-Client-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-Target\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),base64]\"}}}," +
			"{\"name\": \"reqTestHost3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(Host)]\"}}}," +
			"{\"name\": \"X-Forwarded-For\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"11.22.33.44\"}}}," +
			"{\"name\": \"x-forwarded-client-cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"reqTestHeader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"bbb\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"x-ssl-client-der\", \"action\": {\"type\": \"Delete\"}}" +
			"]," +
			"\"response\": [" +
			"{\"name\": \"X-SSL-Server-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-XSS-Protection\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"1; mode=block\"}}}," +
			"{\"name\": \"X-Content-Type-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"nosniff`\"}}}," +
			"{\"name\": \"X-Frame-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"SAMEORIGIN\"}}}," +
			"{\"name\": \"resTestServer1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),lower]\"}}}," +
			"{\"name\": \"resTestServer2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),base64]\"}}}," +
			"{\"name\": \"resTestServer3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server)]\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"server\", \"action\": {\"type\": \"Delete\"}}" +
			"]}}}}"

		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/r3-edge", "-p", patchHeaders, "--type=merge", "-n", project1).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("patched"))

		compat_otp.By("check backend edge route in haproxy that headers to be set or deleted")
		routeBackendCfg := ensureHaproxyBlockConfigContains(oc, routerpod, "be_edge_http:"+project1+":r3-edge", []string{"X-SSL-Client-Cert"})
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-SSL-Client-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-Target' '%[req.hdr(host),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHost1' '%[req.hdr(host),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHost2' '%[req.hdr(host),base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-Forwarded-For' '11.22.33.44'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'x-forwarded-client-cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHeader' 'bbb'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'cache-control' 'private'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request del-header 'x-ssl-client-der'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request del-header 'x-ssl-client-der'")).To(o.BeTrue())

		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-SSL-Server-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-XSS-Protection' '1; mode=block'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-Content-Type-Options' 'nosniff`'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-Frame-Options' 'SAMEORIGIN'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer1' '%[res.hdr(server),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer2' '%[res.hdr(server),base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer3' '%[res.hdr(server)]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'cache-control' 'private'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response del-header 'server'")).To(o.BeTrue())

		compat_otp.By("send traffic to the edge route, then check http headers in the request or response message")
		curlEdgeRouteReq := []string{"-n", project1, clientPodName, "--", "curl", "https://" + edgeRouteHost + "/headers", "-v", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		curlEdgeRouteRes := []string{"-n", project1, clientPodName, "--", "curl", "https://" + edgeRouteHost + "/headers", "-I", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		repeatCmdOnClient(oc, curlEdgeRouteRes, "200", 60, 1)
		reqHeaders, _ := oc.AsAdmin().Run("exec").Args(curlEdgeRouteReq...).Output()
		e2e.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(len(regexp.MustCompile("\"X-Ssl-Client-Cert\": \"([0-9a-zA-Z]+)").FindStringSubmatch(reqHeaders)) > 0).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"X-Target\": \""+edgeRouteHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost1\": \""+lowHostEdge+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost2\": \""+base64HostEdge+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost3\": \""+edgeRouteHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtestheader\": \"bbb\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Cache-Control\": \"private\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, _ := oc.AsAdmin().Run("exec").Args(curlEdgeRouteRes...).Output()
		e2e.Logf("resHeaders is: %v", resHeaders)
		o.Expect(len(regexp.MustCompile("x-ssl-server-cert: ([0-9a-zA-Z]+)").FindStringSubmatch(resHeaders)) > 0).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-xss-protection: 1; mode=block")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-content-type-options: nosniff")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-frame-options: SAMEORIGIN")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver1: "+lowSrv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver2: "+base64Srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver3: "+srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "cache-control: private")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "server:")).NotTo(o.BeTrue())
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-ConnectedOnly-High-66560-adding/deleting http headers to a http route by a router owner", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			ingctrl             = ingressControllerDescription{
				name:      "ocp66560",
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
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("Deploy a project with a client pod, a backend pod and its service resources")
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, clientPod)
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the project")
		routeHost := "service-unsecure66560" + "." + ingctrl.domain
		lowHost := strings.ToLower(routeHost)
		base64Host := base64.StdEncoding.EncodeToString([]byte(routeHost))
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routeHost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := getRoutes(oc, project1)
		o.Expect(routeOutput).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("Patch the route with added/deleted http request/response headers under the spec")
		patchHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [" +
			"{\"name\": \"X-SSL-Client-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-Target\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),base64]\"}}}," +
			"{\"name\": \"reqTestHost3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(Host)]\"}}}," +
			"{\"name\": \"X-Forwarded-For\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"11.22.33.44\"}}}," +
			"{\"name\": \"x-forwarded-client-cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"reqTestHeader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"bbb\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"Referer\", \"action\": {\"type\": \"Delete\"}}" +
			"]," +
			"\"response\": [" +
			"{\"name\": \"X-SSL-Server-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-XSS-Protection\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"1; mode=block\"}}}," +
			"{\"name\": \"X-Content-Type-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"nosniff`\"}}}," +
			"{\"name\": \"X-Frame-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"SAMEORIGIN\"}}}," +
			"{\"name\": \"resTestServer1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),lower]\"}}}," +
			"{\"name\": \"resTestServer2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),base64]\"}}}," +
			"{\"name\": \"resTestServer3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server)]\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"server\", \"action\": {\"type\": \"Delete\"}}" +
			"]}}}}"

		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", patchHeaders, "--type=merge", "-n", project1).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("patched"))

		compat_otp.By("check backend edge route in haproxy that headers to be set or deleted")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		routeBackendCfg := ensureHaproxyBlockConfigContains(oc, routerpod, "be_http:"+project1+":"+unsecsvcName, []string{"X-SSL-Client-Cert"})
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-SSL-Client-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-Target' '%[req.hdr(host),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHost1' '%[req.hdr(host),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHost2' '%[req.hdr(host),base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-Forwarded-For' '11.22.33.44'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'x-forwarded-client-cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHeader' 'bbb'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'cache-control' 'private'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request del-header 'Referer'")).To(o.BeTrue())

		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-SSL-Server-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-XSS-Protection' '1; mode=block'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-Content-Type-Options' 'nosniff`'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-Frame-Options' 'SAMEORIGIN'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer1' '%[res.hdr(server),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer2' '%[res.hdr(server),base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer3' '%[res.hdr(server)]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'cache-control' 'private'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response del-header 'server'")).To(o.BeTrue())

		compat_otp.By("send traffic to the edge route, then check http headers in the request or response message")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routeHost + ":80:" + podIP
		curlHTTPRouteReq := []string{"-n", project1, clientPodName, "--", "curl", "http://" + routeHost + "/headers", "-v", "-e", "www.qe-test.com", "--resolve", toDst, "--connect-timeout", "10"}
		curlHTTPRouteRes := []string{"-n", project1, clientPodName, "--", "curl", "http://" + routeHost + "/headers", "-I", "-e", "www.qe-test.com", "--resolve", toDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		repeatCmdOnClient(oc, curlHTTPRouteRes, "200", 60, 1)
		reqHeaders, _ := oc.AsAdmin().Run("exec").Args(curlHTTPRouteReq...).Output()
		e2e.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(strings.Contains(reqHeaders, "\"X-Ssl-Client-Cert\": \"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"X-Target\": \""+routeHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost1\": \""+lowHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost2\": \""+base64Host+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost3\": \""+routeHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtestheader\": \"bbb\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Cache-Control\": \"private\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, _ := oc.AsAdmin().Run("exec").Args(curlHTTPRouteRes...).Output()
		e2e.Logf("resHeaders is: %v", resHeaders)
		o.Expect(strings.Contains(resHeaders, "x-ssl-server-cert: ")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-xss-protection: 1; mode=block")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-content-type-options: nosniff")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-frame-options: SAMEORIGIN")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver1: "+lowSrv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver2: "+base64Srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver3: "+srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "cache-control: private")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "server:")).NotTo(o.BeTrue())
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-ConnectedOnly-Medium-66566-supported max http headers, max length of a http header name, max length value of a http header", func() {
		var (
			buildPruningBaseDir      = compat_otp.FixturePath("testdata", "router")
			customTemp               = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc               = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName             = "httpbin-svc-insecure"
			clientPod                = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName            = "hello-pod"
			clientPodLabel           = "app=hello-pod"
			maxHTTPHeaders           = 20
			maxLengthHTTPHeaderName  = 255
			maxLengthHTTPHeaderValue = 16384
			ingctrl                  = ingressControllerDescription{
				name:      "ocp66566",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy a project with a client pod, a backend pod and its service resources")
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, clientPod)
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the project")
		routehost := "service-unsecure66566" + "." + "apps." + baseDomain
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routehost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := getRoutes(oc, project1)
		o.Expect(routeOutput).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("patch max number of http headers to a route")
		var maxCfg strings.Builder
		negMaxCfg := maxCfg
		patchHeadersPart1 := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": ["
		patchHeadersPart2 := "]}}}}"
		maxCfg.WriteString(patchHeadersPart1)
		negMaxCfg.WriteString(patchHeadersPart1)
		for i := 0; i < maxHTTPHeaders-1; i++ {
			maxCfg.WriteString("{\"name\": \"ocp66566testheader" + strconv.Itoa(i) + "\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}, ")
			negMaxCfg.WriteString("{\"name\": \"ocp66566testheader" + strconv.Itoa(i) + "\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}, ")
		}
		maxCfg.WriteString("{\"name\": \"ocp66566testheader" + strconv.Itoa(maxHTTPHeaders) + "\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}" + patchHeadersPart2)
		negMaxCfg.WriteString("{\"name\": \"ocp66566testheader" + strconv.Itoa(maxHTTPHeaders) + "\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}")
		patchHeaders := maxCfg.String()
		negMaxCfg.WriteString(", {\"name\": \"test123abc\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}" + patchHeadersPart2)
		negPatchHeaders := negMaxCfg.String()
		patchResourceAsAdmin(oc, project1, "route/"+unsecsvcName, patchHeaders)
		routeBackend := "be_http:" + project1 + ":" + unsecsvcName
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		routeBackendCfg := ensureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"testheader1"})
		o.Expect(strings.Count(routeBackendCfg, "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))

		compat_otp.By("send traffic and check the max http headers specified in a route")
		cmdOnPod := []string{"-n", project1, clientPodName, "--", "curl", "-Is", "http://" + routehost + "/headers", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, cmdOnPod, "200", 60, 1)
		resHeaders, err := oc.Run("exec").Args("-n", project1, clientPodName, "--", "curl", "-s", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Count(strings.ToLower(resHeaders), "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))

		compat_otp.By("try to patch the exceeded max headers to a route")
		patchResourceAsAdmin(oc, project1, "route/"+unsecsvcName, "{\"spec\": {\"httpHeaders\": null}}")
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", negPatchHeaders, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("request headers list can't exceed 20 items"))

		compat_otp.By("patch a http header with max header name to a route")
		maxHeaderName := strings.ToLower(getFixedLengthRandomString(maxLengthHTTPHeaderName))
		negHeaderName := maxHeaderName + "a"
		maxCfg.Reset()
		negMaxCfg.Reset()
		maxCfg.WriteString(patchHeadersPart1 + "{\"name\": \"")
		maxCfg.WriteString(maxHeaderName)
		maxCfg.WriteString("\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}" + patchHeadersPart2)
		patchHeaders = maxCfg.String()
		negMaxCfg.WriteString(patchHeadersPart1 + "{\"name\": \"")
		negMaxCfg.WriteString(negHeaderName)
		negMaxCfg.WriteString("\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}" + patchHeadersPart2)
		negPatchHeaders = negMaxCfg.String()
		patchResourceAsAdmin(oc, project1, "route/"+unsecsvcName, patchHeaders)
		ensureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{maxHeaderName})

		compat_otp.By("send traffic and check the max header name specified in a route")
		resHeaders, err = oc.Run("exec").Args(clientPodName, "--", "curl", "-s", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(strings.ToLower(resHeaders), maxHeaderName+"\": \"value123abc\"")).To(o.BeTrue())

		compat_otp.By("try to patch the header to a route with its name exceeded the max length")
		patchResourceAsAdmin(oc, project1, "route/"+unsecsvcName, "{\"spec\": {\"httpHeaders\": null}}")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", negPatchHeaders, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("exceeds the maximum length, which is 255"))

		compat_otp.By("patch a http header with max header value to a route")
		maxHeaderValue := getFixedLengthRandomString(maxLengthHTTPHeaderValue)
		negMaxHeaderValue := maxHeaderValue + "a"
		maxCfg.Reset()
		negMaxCfg.Reset()
		maxCfg.WriteString(patchHeadersPart1 + "{\"name\": \"header123abc\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"")
		maxCfg.WriteString(maxHeaderValue)
		maxCfg.WriteString("\"}}}" + patchHeadersPart2)
		patchHeaders = maxCfg.String()
		negMaxCfg.WriteString(patchHeadersPart1 + "{\"name\": \"header123abc\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"")
		negMaxCfg.WriteString(negMaxHeaderValue)
		negMaxCfg.WriteString("\"}}}" + patchHeadersPart2)
		negPatchHeaders = negMaxCfg.String()

		patchResourceAsAdmin(oc, project1, "route/"+unsecsvcName, patchHeaders)
		haproxyHeaderName := ensureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"header123abc"})
		o.Expect(strings.Contains(haproxyHeaderName, "http-request set-header 'header123abc' '"+maxHeaderValue+"'")).To(o.BeTrue())

		compat_otp.By("try to patch the header to a route with its value exceeded the max length")
		patchResourceAsAdmin(oc, project1, "route/"+unsecsvcName, "{\"spec\": {\"httpHeaders\": null}}")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", negPatchHeaders, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("exceeds the maximum length, which is 16384"))

		compat_otp.By("patch max number of http headers to an ingress controller")
		patchHeadersPart1 = "{\"spec\": {\"httpHeaders\": {\"actions\": {\"response\": ["
		maxCfg.Reset()
		negMaxCfg.Reset()
		maxCfg.WriteString(patchHeadersPart1)
		negMaxCfg.WriteString(patchHeadersPart1)
		for i := 0; i < maxHTTPHeaders-1; i++ {
			maxCfg.WriteString("{\"name\": \"ocp66566testheader" + strconv.Itoa(i) + "\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}, ")
			negMaxCfg.WriteString("{\"name\": \"ocp66566testheader" + strconv.Itoa(i) + "\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}, ")
		}
		maxCfg.WriteString("{\"name\": \"ocp66566testheader" + strconv.Itoa(maxHTTPHeaders) + "\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}" + patchHeadersPart2)
		negMaxCfg.WriteString("{\"name\": \"ocp66566testheader" + strconv.Itoa(maxHTTPHeaders) + "\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}")
		patchHeaders = maxCfg.String()
		negMaxCfg.WriteString(", {\"name\": \"test123abc\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}" + patchHeadersPart2)
		negPatchHeaders = negMaxCfg.String()
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchHeaders)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP = getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"testheader1"})
		routeBackendCfg = getBlockConfig(oc, routerpod, "defaults")
		o.Expect(strings.Count(routeBackendCfg, "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))
		routeBackendCfg = getBlockConfig(oc, routerpod, "frontend fe_sni")
		o.Expect(strings.Count(routeBackendCfg, "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))
		routeBackendCfg = getBlockConfig(oc, routerpod, "frontend fe_no_sni")
		o.Expect(strings.Count(routeBackendCfg, "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))

		compat_otp.By("send traffic and check the max http headers specified in an ingress controller")
		icResHeaders, err := oc.Run("exec").Args(clientPodName, "--", "curl", "-Is", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Count(strings.ToLower(icResHeaders), "ocp66566testheader") == maxHTTPHeaders).To(o.BeTrue())
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", negPatchHeaders, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Too many: 21: must have at most 20 items"))

		compat_otp.By("patch a http header with max header name to an ingress controller")
		maxCfg.Reset()
		negMaxCfg.Reset()
		maxCfg.WriteString(patchHeadersPart1 + "{\"name\": \"")
		maxCfg.WriteString(maxHeaderName)
		maxCfg.WriteString("\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}" + patchHeadersPart2)
		patchHeaders = maxCfg.String()
		negMaxCfg.WriteString(patchHeadersPart1 + "{\"name\": \"")
		negMaxCfg.WriteString(negHeaderName)
		negMaxCfg.WriteString("\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value123abc\"}}}" + patchHeadersPart2)
		negPatchHeaders = negMaxCfg.String()
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchHeaders)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP = getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{maxHeaderName})
		routeBackendCfg = getBlockConfig(oc, routerpod, "defaults")
		o.Expect(strings.Contains(routeBackendCfg, maxHeaderName)).To(o.BeTrue())
		routeBackendCfg = getBlockConfig(oc, routerpod, "frontend fe_sni")
		o.Expect(strings.Contains(routeBackendCfg, maxHeaderName)).To(o.BeTrue())
		routeBackendCfg = getBlockConfig(oc, routerpod, "frontend fe_no_sni")
		o.Expect(strings.Contains(routeBackendCfg, maxHeaderName)).To(o.BeTrue())

		compat_otp.By("send traffic and check the header with max length name specified in an ingress controller")
		icResHeaders, err = oc.Run("exec").Args(clientPodName, "--", "curl", "-Is", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(strings.ToLower(icResHeaders), maxHeaderName+": value123abc")).To(o.BeTrue())

		compat_otp.By("try to patch the header to an ingress controller with its name exceeded the max length")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", negPatchHeaders, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("Too long:.+than 255"))

		compat_otp.By("patch a http header with max header value to an ingress controller")
		maxCfg.Reset()
		negMaxCfg.Reset()
		maxCfg.WriteString(patchHeadersPart1 + "{\"name\": \"header123abc\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"")
		maxCfg.WriteString(maxHeaderValue)
		maxCfg.WriteString("\"}}}" + patchHeadersPart2)
		patchHeaders = maxCfg.String()
		negMaxCfg.WriteString(patchHeadersPart1 + "{\"name\": \"header123abc\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"")
		negMaxCfg.WriteString(negMaxHeaderValue)
		negMaxCfg.WriteString("\"}}}" + patchHeadersPart2)
		negPatchHeaders = negMaxCfg.String()
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchHeaders)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "4")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"header123abc"})
		routeBackendCfg = getBlockConfig(oc, routerpod, "defaults")
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'header123abc' '"+maxHeaderValue+"'")).To(o.BeTrue())
		routeBackendCfg = getBlockConfig(oc, routerpod, "frontend fe_sni")
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'header123abc' '"+maxHeaderValue+"'")).To(o.BeTrue())
		routeBackendCfg = getBlockConfig(oc, routerpod, "frontend fe_no_sni")
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'header123abc' '"+maxHeaderValue+"'")).To(o.BeTrue())
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", negPatchHeaders, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("Too long:.+than 16384"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-ConnectedOnly-Medium-66568-negative test of adding/deleting http headers", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = ingressControllerDescription{
				name:      "ocp66568",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy a project with a client pod, a backend pod and its service resources")
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, clientPod)
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the project")
		routehost := "service-unsecure66568" + "." + ingctrl.name + baseDomain
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routehost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := getRoutes(oc, project1)
		o.Expect(routeOutput).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("try to patch two same headers to a route")
		sameHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"testheader1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value1\"}}}, {\"name\": \"testheader1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value1\"}}}]}}}}"
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", sameHeaders, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Duplicate value: \"testheader1\""))

		compat_otp.By("try to patch proxy header to a route")
		proxyHeader := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"proxy\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"http://100.200.1.1:80\"}}}]}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", proxyHeader, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Forbidden: the following headers may not be modified using this API: strict-transport-security, proxy, cookie, set-cookie"))

		compat_otp.By("try to patch host header to a route")
		hostHeader := `{"spec": {"httpHeaders": {"actions": {"request": [{"name": "host", "action": {"type": "Set", "set": {"value": "www.neqe-test.com"}}}]}}}}`
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", hostHeader, "--type=merge", "-n", project1).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		jpath := `{.spec.httpHeaders.actions.request[?(@.name=="host")].action.set.value}`
		host := getByJsonPath(oc, project1, "route/"+unsecsvcName, jpath)
		o.Expect(host).To(o.ContainSubstring("www.neqe-test.com"))

		compat_otp.By("try to patch strict-transport-security header to a route")
		hstsHeader := `{"spec": {"httpHeaders": {"actions": {"request": [{"name": "strict-transport-security", "action": {"type": "Set", "set": {"value": "max-age=31536000;includeSubDomains;preload"}}}]}}}}`
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", hstsHeader, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Forbidden: the following headers may not be modified using this API: strict-transport-security, proxy, cookie, set-cookie"))

		compat_otp.By("try to patch cookie header to a route")
		cookieHeader := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"cookie\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"cookie-test\"}}}]}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", cookieHeader, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Forbidden: the following headers may not be modified using this API: strict-transport-security, proxy, cookie, set-cookie"))

		compat_otp.By("try to patch set-cookie header to a route")
		setCookieHeader := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"set-cookie\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"set-cookie-test\"}}}]}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", setCookieHeader, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Forbidden: the following headers may not be modified using this API: strict-transport-security, proxy, cookie, set-cookie"))

		compat_otp.By("try to patch two same headers to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", sameHeaders, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Duplicate value: map[string]interface {}{\"name\":\"testheader1\"}"))

		compat_otp.By("try to patch proxy header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", proxyHeader, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("proxy header may not be modified via header actions"))

		compat_otp.By("try to patch host header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", hostHeader, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("host header may not be modified via header actions"))

		compat_otp.By("try to patch strict-transport-security header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", hstsHeader, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("strict-transport-security header may not be modified via header actions"))

		compat_otp.By("try to patch cookie header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", cookieHeader, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("cookie header may not be modified via header actions"))

		compat_otp.By("try to patch set-cookie header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", setCookieHeader, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("set-cookie header may not be modified via header actions"))

		compat_otp.By("patch a request and a response headers to a route, while patch the same headers with the same header names but with different header values to an ingress-controller")
		routeHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"reqtestheader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"req111\"}}}], \"response\": [{\"name\": \"restestheader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"resaaa\"}}}]}}}}"
		icHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"reqtestheader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"req222\"}}}], \"response\": [{\"name\": \"restestheader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"resbbb\"}}}]}}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", routeHeaders, "--type=merge", "-n", project1).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, icHeaders)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("send traffic, check the request header reqtestheader which should be set to req111 by the route")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		cmdOnPod := []string{"-n", project1, clientPodName, "--", "curl", "-I", "http://" + routehost + "/headers", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, cmdOnPod, "200", 60, 1)
		reqHeaders, err := oc.Run("exec").Args("-n", project1, clientPodName, "--", "curl", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(strings.ToLower(reqHeaders), "\"reqtestheader\": \"req111\"")).To(o.BeTrue())

		compat_otp.By("send traffic, check the response header restestheader which should be set to resbbb by the ingress-controller")
		resHeaders, err := oc.Run("exec").Args(clientPodName, "--", "curl", "http://"+routehost+"/headers", "-I", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(resHeaders, "restestheader: resbbb")).To(o.BeTrue())
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-NonPreRelease-Longduration-ConnectedOnly-Medium-66569-set different type of values for a http header name and its value", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			ingctrl             = ingressControllerDescription{
				name:      "ocp66569",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
			routeResource   = "route/" + unsecsvcName
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy a project with a backend pod and its service resources")
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the project")
		routehost := "service-unsecure66569" + "." + ingctrl.name + baseDomain
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routehost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := getRoutes(oc, project1)
		o.Expect(routeOutput).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("patch http headers with valid number, alphabet, a combination of both header names and header values to a route, and then check the added headers in haproxy.conf")
		validHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"001\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"20230906\"}}}, {\"name\": \"aBc\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"Wednesday\"}}}, {\"name\": \"test01\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value01\"}}}]}}}}"
		patchResourceAsAdmin(oc, project1, routeResource, validHeaders)
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		routeBackend := "be_http:" + project1 + ":" + unsecsvcName
		routeBackendCfg := ensureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"test01"})
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header '001' '20230906'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aBc' 'Wednesday'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'test01' 'value01'")).To(o.BeTrue())

		compat_otp.By("try to patch http header with blank value in the header name to a route")
		blankHeaderName := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aa bb\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"abc\"}}}]}}}}"
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", blankHeaderName, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"aa bb\": name must be a valid HTTP header name as defined in RFC 2616 section 4.2"))

		compat_otp.By("patch http header with #$* in the header name to a route, and then check it in haproxy.config")
		specialHeaderName1 := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aabbccdd#$*ee\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"abc\"}}}]}}}}"
		patchResourceAsAdmin(oc, project1, routeResource, specialHeaderName1)
		ensureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"http-request set-header 'aabbccdd#$*ee' 'abc'"})

		compat_otp.By("patch http header with ' in the header name to a route, and then check it in haproxy.config")
		specialHeaderName2 := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aabbccdd'ee\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"abc\"}}}]}}}}"
		patchResourceAsAdmin(oc, project1, routeResource, specialHeaderName2)
		ensureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"http-request set-header 'aabbccdd'\\''ee' 'abc'"})

		compat_otp.By("try to patch http header with \" in the header name to a route")
		specialHeaderName3 := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aabbccdd\\\"ee\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"abc\"}}}]}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", specialHeaderName3, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"aabbccdd\\\"ee\": name must be a valid HTTP header name"))

		compat_otp.By("patch http header with specical characters in header value to a route")
		specialHeaderValues := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aabbccddee\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"vlalueabc #$*'\\\"cc\"}}}]}}}}"
		patchResourceAsAdmin(oc, project1, routeResource, specialHeaderValues)
		ensureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"http-request set-header 'aabbccddee' 'vlalueabc #$*'\\''\"cc'"})

		compat_otp.By("patch http headers with valid number, alphabet, a combination of both header names and header values to an ingress controller, then check the added headers in haproxy.conf")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, validHeaders)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"test01"})
		for _, backend := range []string{"defaults", "frontend fe_sni", "frontend fe_no_sni"} {
			routeBackendCfg = getBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header '001' '20230906'")).To(o.BeTrue())
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aBc' 'Wednesday'")).To(o.BeTrue())
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'test01' 'value01'")).To(o.BeTrue())
		}

		compat_otp.By("patch http header with blank value in the header name to an ingress controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", blankHeaderName, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		e2e.Logf("blanck output is: %v", output)
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"aa bb\""))

		compat_otp.By("patch http header with #$* in the header name to an ingress controller")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, specialHeaderName1)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"aabbccdd"})
		for _, backend := range []string{"defaults", "frontend fe_sni", "frontend fe_no_sni"} {
			routeBackendCfg = getBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aabbccdd#$*ee' 'abc'")).To(o.BeTrue())
		}

		compat_otp.By("patch http header with ' in the header name to an ingress controller")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, specialHeaderName2)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "4")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"aabbccdd"})
		for _, backend := range []string{"defaults", "frontend fe_sni", "frontend fe_no_sni"} {
			routeBackendCfg = getBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aabbccdd'\\''ee' 'abc'")).To(o.BeTrue())
		}

		compat_otp.By("patch http header with \" in the header name to an ingress controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", specialHeaderName3, "--type=merge", "-n", project1).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"aabbccdd\\\"ee\""))

		compat_otp.By("patch http header with specical characters in header value to an ingress controller")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, specialHeaderValues)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "5")
		routerpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"aabbccdd"})
		for _, backend := range []string{"defaults", "frontend fe_sni", "frontend fe_no_sni"} {
			routeBackendCfg = getBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aabbccddee' 'vlalueabc #$*'\\''\"cc'")).To(o.BeTrue())
		}
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-ConnectedOnly-High-66572-adding/deleting http headers to a http route by an ingress-controller as a cluster administrator", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			ingctrl             = ingressControllerDescription{
				name:      "ocp66572",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy a project with a client pod, a backend pod and its service resources")
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, clientPod)
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the project")
		routeHost := "service-unsecure66572" + "." + ingctrl.domain
		lowHost := strings.ToLower(routeHost)
		base64Host := base64.StdEncoding.EncodeToString([]byte(routeHost))
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routeHost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := getRoutes(oc, project1)
		o.Expect(routeOutput).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("Patch added/deleted http request/response headers to the custom ingress-controller")
		patchHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [" +
			"{\"name\": \"X-SSL-Client-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-Target\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),base64]\"}}}," +
			"{\"name\": \"reqTestHost3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(Host)]\"}}}," +
			"{\"name\": \"X-Forwarded-For\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"11.22.33.44\"}}}," +
			"{\"name\": \"x-forwarded-client-cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"reqTestHeader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"bbb\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"Referer\", \"action\": {\"type\": \"Delete\"}}" +
			"]," +
			"\"response\": [" +
			"{\"name\": \"X-SSL-Server-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-XSS-Protection\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"1; mode=block\"}}}," +
			"{\"name\": \"X-Content-Type-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"nosniff`\"}}}," +
			"{\"name\": \"X-Frame-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"SAMEORIGIN\"}}}," +
			"{\"name\": \"resTestServer1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),lower]\"}}}," +
			"{\"name\": \"resTestServer2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),base64]\"}}}," +
			"{\"name\": \"resTestServer3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server)]\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"server\", \"action\": {\"type\": \"Delete\"}}" +
			"]}}}}"
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchHeaders)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check the configured added/deleted headers under defaults/frontend fe_sni/frontend fe_no_sni in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"X-SSL-Client-Cert"})
		for _, backend := range []string{"defaults", "frontend fe_sni", "frontend fe_no_sni"} {
			haproxyBackendCfg := getBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-SSL-Client-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-Target' '%[req.hdr(host),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHost1' '%[req.hdr(host),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHost2' '%[req.hdr(host),base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-Forwarded-For' '11.22.33.44'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'x-forwarded-client-cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHeader' 'bbb'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'cache-control' 'private'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request del-header 'Referer'")).To(o.BeTrue())

			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-SSL-Server-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-XSS-Protection' '1; mode=block'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-Content-Type-Options' 'nosniff`'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-Frame-Options' 'SAMEORIGIN'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer1' '%[res.hdr(server),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer2' '%[res.hdr(server),base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer3' '%[res.hdr(server)]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'cache-control' 'private'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response del-header 'server'")).To(o.BeTrue())
		}

		compat_otp.By("send traffic to the edge route, then check http headers in the request or response message")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		routeDst := routeHost + ":80:" + podIP
		curlHTTPRouteReq := []string{"-n", project1, clientPodName, "--", "curl", "http://" + routeHost + "/headers", "-v", "-e", "www.qe-test.com", "--resolve", routeDst, "--connect-timeout", "10"}
		curlHTTPRouteRes := []string{"-n", project1, clientPodName, "--", "curl", "http://" + routeHost + "/headers", "-I", "-e", "www.qe-test.com", "--resolve", routeDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		repeatCmdOnClient(oc, curlHTTPRouteRes, "200", 60, 1)
		reqHeaders, err := oc.AsAdmin().Run("exec").Args(curlHTTPRouteReq...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(strings.Contains(reqHeaders, "\"X-Ssl-Client-Cert\": \"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"X-Target\": \""+routeHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost1\": \""+lowHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost2\": \""+base64Host+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost3\": \""+routeHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtestheader\": \"bbb\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Cache-Control\": \"private\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, err := oc.AsAdmin().Run("exec").Args(curlHTTPRouteRes...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("resHeaders is: %v", resHeaders)
		o.Expect(strings.Contains(resHeaders, "x-ssl-server-cert: ")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-xss-protection: 1; mode=block")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-content-type-options: nosniff")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-frame-options: SAMEORIGIN")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver1: "+lowSrv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver2: "+base64Srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver3: "+srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "cache-control: private")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "server:")).NotTo(o.BeTrue())
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-ConnectedOnly-High-66662-adding/deleting http headers to a reen route by a router owner", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			serverPod           = filepath.Join(buildPruningBaseDir, "httpbin-pod-withprivilege.json")
			secsvc              = filepath.Join(buildPruningBaseDir, "httpbin-service_secure.json")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			secsvcName          = "httpbin-svc-secure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			srvCert             = "/src/example_wildcard_chain.pem"
			srvKey              = "/src/example_wildcard.key"
			ingctrl             = ingressControllerDescription{
				name:      "ocp66662",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
			podFileDir      = "/data/OCP-66662-ca"
			podCaCert       = podFileDir + "/66662-ca.pem"
			podCustomKey    = podFileDir + "/user66662.key"
			podCustomCert   = podFileDir + "/user66662.pem"
			fileDir         = "/tmp/OCP-66662-ca"
			dirname         = "/tmp/OCP-66662-ca/"
			name            = dirname + "66662"
			validity        = 30
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = dirname + "user66662"
			customKey       = userCert + ".key"
			customCert      = userCert + ".pem"
			destSubj        = "/CN=*.edge.example.com"
			destCA          = dirname + "dst.pem"
			destKey         = dirname + "dst.key"
			destCsr         = dirname + "dst.csr"
			destCnf         = dirname + "openssl.cnf"
		)

		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		project1 := oc.Namespace()
		compat_otp.SetNamespacePrivileged(oc, project1)

		compat_otp.By("Try to create custom key and custom certification by openssl, create a new self-signed CA at first, creating the CA key")
		opensslCmd := fmt.Sprintf(`openssl genrsa -out %s-ca.key 2048`, name)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create the CA certificate")
		opensslCmd = fmt.Sprintf(`openssl req -x509 -new -nodes -key %s-ca.key -sha256 -days %d -out %s-ca.pem  -subj %s`, name, validity, name, caSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a new user certificate, crearing the user CSR with the private user key")
		userSubj := "/CN=example-ne.com"
		opensslCmd = fmt.Sprintf(`openssl req -nodes -newkey rsa:2048 -keyout %s.key -subj %s -out %s.csr`, userCert, userSubj, userCert)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Sign the user CSR and generate the certificate")
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.`+ingctrl.domain+`") -req -in %s.csr -CA %s-ca.pem -CAkey %s-ca.key -CAcreateserial -out %s.pem -days %d -sha256`, userCert, name, name, userCert, validity)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create the destination Certification for the reencrypt route, create the key")
		opensslCmd = fmt.Sprintf(`openssl genrsa -out %s 2048`, destKey)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create the csr for the destination Certification")
		opensslCmd = fmt.Sprintf(`openssl req -new -key %s -subj %s  -out %s`, destKey, destSubj, destCsr)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.3: Create the extension file, then create the destination certification")
		sanCfg := fmt.Sprintf(`
[ v3_req ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = *.edge.example.com
DNS.2 = *.%s.%s.svc
`, secsvcName, project1)

		cmd := fmt.Sprintf(`echo "%s" > %s`, sanCfg, destCnf)
		_, err = exec.Command("bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile %s -extensions v3_req  -req -in %s -signkey  %s -days %d -sha256 -out %s`, destCnf, destCsr, destKey, validity, destCA)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a custom ingresscontroller")
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		cmFile := "ca-bundle.pem=" + name + "-ca.pem"
		defer deleteConfigMap(oc, "openshift-config", "client-ca-"+ingctrl.name)
		createConfigMapFromFile(oc, "openshift-config", "client-ca-"+ingctrl.name, cmFile)

		compat_otp.By("patch the ingresscontroller to enable client certificate with required policy")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-"+ingctrl.name+"\"},\"clientCertificatePolicy\":\"Required\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")

		compat_otp.By("Deploy the project with a client pod, a backend pod and its service resources")
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", project1, fileDir, project1+"/"+clientPodName+":"+podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		operateResourceFromFile(oc, "create", project1, serverPod)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")
		createResourceFromFile(oc, project1, secsvc)

		compat_otp.By("Update the certification and key in the server pod")
		podName := getPodListByLabel(oc, project1, "name=httpbin-pod")
		newSrvCert := project1 + "/" + podName[0] + ":" + srvCert
		newSrvKey := project1 + "/" + podName[0] + ":" + srvKey
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", project1, podName[0], "-c", "httpbin-https", "--", "bash", "-c", "rm -f "+srvCert).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", project1, podName[0], "-c", "httpbin-https", "--", "bash", "-c", "rm -f "+srvKey).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", project1, destCA, "-c", "httpbin-https", newSrvCert).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", project1, destKey, "-c", "httpbin-https", newSrvKey).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("create a reen route")
		reenRouteHost := "r2-reen66662." + ingctrl.domain
		lowHostReen := strings.ToLower(reenRouteHost)
		base64HostReen := base64.StdEncoding.EncodeToString([]byte(reenRouteHost))
		reenRouteDst := reenRouteHost + ":443:" + podIP
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "route", "reencrypt", "r2-reen", "--service="+secsvcName, "--cert="+customCert, "--key="+customKey, "--ca-cert="+name+"-ca.pem", "--dest-ca-cert="+destCA, "--hostname="+reenRouteHost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Patch the reen route with added/deleted http request/response headers under the spec")
		patchHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [" +
			"{\"name\": \"X-SSL-Client-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-Target\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),base64]\"}}}," +
			"{\"name\": \"reqTestHost3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(Host)]\"}}}," +
			"{\"name\": \"X-Forwarded-For\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"11.22.33.44\"}}}," +
			"{\"name\": \"x-forwarded-client-cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"reqTestHeader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"bbb\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"x-ssl-client-der\", \"action\": {\"type\": \"Delete\"}}" +
			"]," +
			"\"response\": [" +
			"{\"name\": \"X-SSL-Server-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-XSS-Protection\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"1; mode=block\"}}}," +
			"{\"name\": \"X-Content-Type-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"nosniff`\"}}}," +
			"{\"name\": \"X-Frame-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"SAMEORIGIN\"}}}," +
			"{\"name\": \"resTestServer1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),lower]\"}}}," +
			"{\"name\": \"resTestServer2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),base64]\"}}}," +
			"{\"name\": \"resTestServer3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server)]\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"server\", \"action\": {\"type\": \"Delete\"}}" +
			"]}}}}"

		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/r2-reen", "-p", patchHeaders, "--type=merge", "-n", project1).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("patched"))

		compat_otp.By("check backend reen route in haproxy that headers to be set or deleted")
		routeBackendCfg := ensureHaproxyBlockConfigContains(oc, routerpod, "be_secure:"+project1+":r2-reen", []string{"X-SSL-Client-Cert"})
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-SSL-Client-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-Target' '%[req.hdr(host),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHost1' '%[req.hdr(host),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHost2' '%[req.hdr(host),base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'X-Forwarded-For' '11.22.33.44'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'x-forwarded-client-cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'reqTestHeader' 'bbb'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'cache-control' 'private'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request del-header 'x-ssl-client-der'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request del-header 'x-ssl-client-der'")).To(o.BeTrue())

		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-SSL-Server-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-XSS-Protection' '1; mode=block'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-Content-Type-Options' 'nosniff`'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'X-Frame-Options' 'SAMEORIGIN'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer1' '%[res.hdr(server),lower]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer2' '%[res.hdr(server),base64]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'resTestServer3' '%[res.hdr(server)]'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'cache-control' 'private'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-response del-header 'server'")).To(o.BeTrue())

		compat_otp.By("send traffic to the reen route, then check http headers in the request or response message")
		curlReenRouteReq := []string{"-n", project1, clientPodName, "--", "curl", "https://" + reenRouteHost + "/headers", "-v", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", reenRouteDst, "--connect-timeout", "10"}
		curlReenRouteRes := []string{"-n", project1, clientPodName, "--", "curl", "https://" + reenRouteHost + "/headers", "-I", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", reenRouteDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		e2e.Logf("curlReenRouteRes is: %v", curlReenRouteRes)
		repeatCmdOnClient(oc, curlReenRouteRes, "200", 60, 1)
		reqHeaders, _ := oc.AsAdmin().Run("exec").Args(curlReenRouteReq...).Output()
		e2e.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(len(regexp.MustCompile("\"X-Ssl-Client-Cert\": \"([0-9a-zA-Z]+)").FindStringSubmatch(reqHeaders)) > 0).To(o.BeTrue())
		o.Expect(reqHeaders).To(o.ContainSubstring("\"X-Target\": \"" + reenRouteHost + "\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Reqtesthost1\": \"" + lowHostReen + "\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Reqtesthost2\": \"" + base64HostReen + "\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Reqtesthost3\": \"" + reenRouteHost + "\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Reqtestheader\": \"bbb\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Cache-Control\": \"private\""))
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, _ := oc.AsAdmin().Run("exec").Args(curlReenRouteRes...).Output()
		e2e.Logf("resHeaders is: %v", resHeaders)
		o.Expect(len(regexp.MustCompile("x-ssl-server-cert: ([0-9a-zA-Z]+)").FindStringSubmatch(resHeaders)) > 0).To(o.BeTrue())
		o.Expect(resHeaders).To(o.ContainSubstring("x-xss-protection: 1; mode=block"))
		o.Expect(resHeaders).To(o.ContainSubstring("x-content-type-options: nosniff"))
		o.Expect(resHeaders).To(o.ContainSubstring("x-frame-options: SAMEORIGIN"))
		o.Expect(resHeaders).To(o.ContainSubstring("restestserver1: " + lowSrv))
		o.Expect(resHeaders).To(o.ContainSubstring("restestserver2: " + base64Srv))
		o.Expect(resHeaders).To(o.ContainSubstring("restestserver3: " + srv))
		o.Expect(resHeaders).To(o.ContainSubstring("cache-control: private"))
		o.Expect(strings.Contains(reqHeaders, "server:")).NotTo(o.BeTrue())
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-ConnectedOnly-High-67009-adding/deleting http headers to an edge route by an ingress-controller as a cluster administrator", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			ingctrl             = ingressControllerDescription{
				name:      "ocp67009",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
			podFileDir      = "/data/OCP-67009-ca"
			podCaCert       = podFileDir + "/67009-ca.pem"
			podCustomKey    = podFileDir + "/user67009.key"
			podCustomCert   = podFileDir + "/user67009.pem"
			fileDir         = "/tmp/OCP-67009-ca"
			dirname         = "/tmp/OCP-67009-ca/"
			name            = dirname + "67009"
			validity        = 30
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = dirname + "user67009"
			customKey       = userCert + ".key"
			customCert      = userCert + ".pem"
		)
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain

		compat_otp.By("Try to create custom key and custom certification by openssl, create a new self-signed CA at first, creating the CA key")
		opensslCmd := fmt.Sprintf(`openssl genrsa -out %s-ca.key 2048`, name)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create the CA certificate")
		opensslCmd = fmt.Sprintf(`openssl req -x509 -new -nodes -key %s-ca.key -sha256 -days %d -out %s-ca.pem  -subj %s`, name, validity, name, caSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a new user certificate, crearing the user CSR with the private user key")
		userSubj := "/CN=example-ne.com"
		opensslCmd = fmt.Sprintf(`openssl req -nodes -newkey rsa:2048 -keyout %s.key -subj %s -out %s.csr`, userCert, userSubj, userCert)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Sign the user CSR and generate the certificate")
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.`+ingctrl.domain+`") -req -in %s.csr -CA %s-ca.pem -CAkey %s-ca.key -CAcreateserial -out %s.pem -days %d -sha256`, userCert, name, name, userCert, validity)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a custom ingresscontroller")
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		cmFile := "ca-bundle.pem=" + name + "-ca.pem"
		defer deleteConfigMap(oc, "openshift-config", "client-ca-"+ingctrl.name)
		createConfigMapFromFile(oc, "openshift-config", "client-ca-"+ingctrl.name, cmFile)

		compat_otp.By("patch the ingresscontroller to enable client certificate with required policy")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-"+ingctrl.name+"\"},\"clientCertificatePolicy\":\"Required\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Deploy a project with a client pod, a backend pod and its service resources")
		project1 := oc.Namespace()
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", project1, fileDir, project1+"/"+clientPodName+":"+podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("create an edge route")
		edgeRouteHost := "r3-edge67009." + ingctrl.domain
		lowHostEdge := strings.ToLower(edgeRouteHost)
		base64HostEdge := base64.StdEncoding.EncodeToString([]byte(edgeRouteHost))
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "route", "edge", "r3-edge", "--service="+unsecsvcName, "--cert="+customCert, "--key="+customKey, "--ca-cert="+name+"-ca.pem", "--hostname="+edgeRouteHost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Patch added/deleted http request/response headers to the custom ingress-controller")
		patchHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [" +
			"{\"name\": \"X-SSL-Client-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-Target\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),base64]\"}}}," +
			"{\"name\": \"reqTestHost3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(Host)]\"}}}," +
			"{\"name\": \"X-Forwarded-For\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"11.22.33.44\"}}}," +
			"{\"name\": \"x-forwarded-client-cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"reqTestHeader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"bbb\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"x-ssl-client-der\", \"action\": {\"type\": \"Delete\"}}" +
			"]," +
			"\"response\": [" +
			"{\"name\": \"X-SSL-Server-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-XSS-Protection\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"1; mode=block\"}}}," +
			"{\"name\": \"X-Content-Type-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"nosniff`\"}}}," +
			"{\"name\": \"X-Frame-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"SAMEORIGIN\"}}}," +
			"{\"name\": \"resTestServer1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),lower]\"}}}," +
			"{\"name\": \"resTestServer2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),base64]\"}}}," +
			"{\"name\": \"resTestServer3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server)]\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"server\", \"action\": {\"type\": \"Delete\"}}" +
			"]}}}}"

		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchHeaders)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		compat_otp.By("check the configured added/deleted headers under defaults/frontend fe_sni/frontend fe_no_sni in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"X-SSL-Client-Cert"})
		for _, backend := range []string{"defaults", "frontend fe_sni", "frontend fe_no_sni"} {
			haproxyBackendCfg := getBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-SSL-Client-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-Target' '%[req.hdr(host),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHost1' '%[req.hdr(host),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHost2' '%[req.hdr(host),base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-Forwarded-For' '11.22.33.44'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'x-forwarded-client-cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHeader' 'bbb'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'cache-control' 'private'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request del-header 'x-ssl-client-der'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request del-header 'x-ssl-client-der'")).To(o.BeTrue())

			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-SSL-Server-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-XSS-Protection' '1; mode=block'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-Content-Type-Options' 'nosniff`'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-Frame-Options' 'SAMEORIGIN'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer1' '%[res.hdr(server),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer2' '%[res.hdr(server),base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer3' '%[res.hdr(server)]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'cache-control' 'private'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response del-header 'server'")).To(o.BeTrue())
		}

		compat_otp.By("send traffic to the edge route, then check http headers in the request or response message")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		edgeRouteDst := edgeRouteHost + ":443:" + podIP
		curlEdgeRouteReq := []string{"-n", project1, clientPodName, "--", "curl", "https://" + edgeRouteHost + "/headers", "-v", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		curlEdgeRouteRes := []string{"-n", project1, clientPodName, "--", "curl", "https://" + edgeRouteHost + "/headers", "-I", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		repeatCmdOnClient(oc, curlEdgeRouteRes, "200", 60, 1)
		reqHeaders, err := oc.AsAdmin().Run("exec").Args(curlEdgeRouteReq...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(len(regexp.MustCompile("\"X-Ssl-Client-Cert\": \"([0-9a-zA-Z]+)").FindStringSubmatch(reqHeaders)) > 0).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"X-Target\": \""+edgeRouteHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost1\": \""+lowHostEdge+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost2\": \""+base64HostEdge+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost3\": \""+edgeRouteHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtestheader\": \"bbb\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Cache-Control\": \"private\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, err := oc.AsAdmin().Run("exec").Args(curlEdgeRouteRes...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("resHeaders is: %v", resHeaders)
		o.Expect(len(regexp.MustCompile("x-ssl-server-cert: ([0-9a-zA-Z]+)").FindStringSubmatch(resHeaders)) > 0).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-xss-protection: 1; mode=block")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-content-type-options: nosniff")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-frame-options: SAMEORIGIN")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver1: "+lowSrv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver2: "+base64Srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver3: "+srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "cache-control: private")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "server:")).NotTo(o.BeTrue())
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-DEPRECATED-ROSA-OSD_CCS-ARO-ConnectedOnly-High-67010-adding/deleting http headers to a reen route by an ingress-controller as a cluster administrator", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			serverPod           = filepath.Join(buildPruningBaseDir, "httpbin-pod-withprivilege.json")
			secsvc              = filepath.Join(buildPruningBaseDir, "httpbin-service_secure.json")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			secsvcName          = "httpbin-svc-secure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			srvCert             = "/src/example_wildcard_chain.pem"
			srvKey              = "/src/example_wildcard.key"
			ingctrl             = ingressControllerDescription{
				name:      "ocp67010",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
			podFileDir      = "/data/OCP-67010-ca"
			podCaCert       = podFileDir + "/67010-ca.pem"
			podCustomKey    = podFileDir + "/user67010.key"
			podCustomCert   = podFileDir + "/user67010.pem"
			fileDir         = "/tmp/OCP-67010-ca"
			dirname         = "/tmp/OCP-67010-ca/"
			name            = dirname + "67010"
			validity        = 30
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = dirname + "user67010"
			customKey       = userCert + ".key"
			customCert      = userCert + ".pem"
			destSubj        = "/CN=*.edge.example.com"
			destCA          = dirname + "dst.pem"
			destKey         = dirname + "dst.key"
			destCsr         = dirname + "dst.csr"
			destCnf         = dirname + "openssl.cnf"
		)

		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		project1 := oc.Namespace()
		compat_otp.SetNamespacePrivileged(oc, project1)

		compat_otp.By("Try to create custom key and custom certification by openssl, create a new self-signed CA at first, creating the CA key")
		opensslCmd := fmt.Sprintf(`openssl genrsa -out %s-ca.key 2048`, name)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create the CA certificate")
		opensslCmd = fmt.Sprintf(`openssl req -x509 -new -nodes -key %s-ca.key -sha256 -days %d -out %s-ca.pem  -subj %s`, name, validity, name, caSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a new user certificate, crearing the user CSR with the private user key")
		userSubj := "/CN=example-ne.com"
		opensslCmd = fmt.Sprintf(`openssl req -nodes -newkey rsa:2048 -keyout %s.key -subj %s -out %s.csr`, userCert, userSubj, userCert)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Sign the user CSR and generate the certificate")
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.`+ingctrl.domain+`") -req -in %s.csr -CA %s-ca.pem -CAkey %s-ca.key -CAcreateserial -out %s.pem -days %d -sha256`, userCert, name, name, userCert, validity)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create the destination Certification for the reencrypt route, create the key")
		opensslCmd = fmt.Sprintf(`openssl genrsa -out %s 2048`, destKey)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create the csr for the destination Certification")
		opensslCmd = fmt.Sprintf(`openssl req -new -key %s -subj %s  -out %s`, destKey, destSubj, destCsr)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.3: Create the extension file, then create the destination certification")
		sanCfg := fmt.Sprintf(`
[ v3_req ]
subjectAltName = @alt_names

[ alt_names ]
DNS.1 = *.edge.example.com
DNS.2 = *.%s.%s.svc
`, secsvcName, project1)

		cmd := fmt.Sprintf(`echo "%s" > %s`, sanCfg, destCnf)
		_, err = exec.Command("bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile %s -extensions v3_req  -req -in %s -signkey  %s -days %d -sha256 -out %s`, destCnf, destCsr, destKey, validity, destCA)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a custom ingresscontroller")
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		cmFile := "ca-bundle.pem=" + name + "-ca.pem"
		defer deleteConfigMap(oc, "openshift-config", "client-ca-"+ingctrl.name)
		createConfigMapFromFile(oc, "openshift-config", "client-ca-"+ingctrl.name, cmFile)

		compat_otp.By("patch the ingresscontroller to enable client certificate with required policy")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-"+ingctrl.name+"\"},\"clientCertificatePolicy\":\"Required\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("Deploy the project with a client pod, a backend pod and its service resources")
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "-f", clientPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", project1, fileDir, project1+"/"+clientPodName+":"+podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		operateResourceFromFile(oc, "create", project1, serverPod)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")
		createResourceFromFile(oc, project1, secsvc)

		compat_otp.By("Update the certification and key in the server pod")
		podName := getPodListByLabel(oc, project1, "name=httpbin-pod")
		newSrvCert := project1 + "/" + podName[0] + ":" + srvCert
		newSrvKey := project1 + "/" + podName[0] + ":" + srvKey
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", project1, podName[0], "-c", "httpbin-https", "--", "bash", "-c", "rm -f "+srvCert).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", project1, podName[0], "-c", "httpbin-https", "--", "bash", "-c", "rm -f "+srvKey).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", project1, destCA, "-c", "httpbin-https", newSrvCert).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", project1, destKey, "-c", "httpbin-https", newSrvKey).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("create a reen route")
		reenRouteHost := "r2-reen67010." + ingctrl.domain
		lowHostReen := strings.ToLower(reenRouteHost)
		base64HostReen := base64.StdEncoding.EncodeToString([]byte(reenRouteHost))
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "route", "reencrypt", "r2-reen", "--service="+secsvcName, "--cert="+customCert, "--key="+customKey, "--ca-cert="+name+"-ca.pem", "--dest-ca-cert="+destCA, "--hostname="+reenRouteHost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Patch added/deleted http request/response headers to the custom ingress-controller")
		patchHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [" +
			"{\"name\": \"X-SSL-Client-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-Target\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),lower]\"}}}," +
			"{\"name\": \"reqTestHost2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(host),base64]\"}}}," +
			"{\"name\": \"reqTestHost3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[req.hdr(Host)]\"}}}," +
			"{\"name\": \"X-Forwarded-For\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"11.22.33.44\"}}}," +
			"{\"name\": \"x-forwarded-client-cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"reqTestHeader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"bbb\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"x-ssl-client-der\", \"action\": {\"type\": \"Delete\"}}" +
			"]," +
			"\"response\": [" +
			"{\"name\": \"X-SSL-Server-Cert\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%{+Q}[ssl_c_der,base64]\"}}}," +
			"{\"name\": \"X-XSS-Protection\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"1; mode=block\"}}}," +
			"{\"name\": \"X-Content-Type-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"nosniff`\"}}}," +
			"{\"name\": \"X-Frame-Options\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"SAMEORIGIN\"}}}," +
			"{\"name\": \"resTestServer1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),lower]\"}}}," +
			"{\"name\": \"resTestServer2\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server),base64]\"}}}," +
			"{\"name\": \"resTestServer3\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"%[res.hdr(server)]\"}}}," +
			"{\"name\": \"cache-control\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"private\"}}}," +
			"{\"name\": \"server\", \"action\": {\"type\": \"Delete\"}}" +
			"]}}}}"

		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchHeaders)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		compat_otp.By("check the configured added/deleted headers under defaults/frontend fe_sni/frontend fe_no_sni in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"X-SSL-Client-Cert"})
		for _, backend := range []string{"defaults", "frontend fe_sni", "frontend fe_no_sni"} {
			haproxyBackendCfg := getBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-SSL-Client-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-Target' '%[req.hdr(host),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHost1' '%[req.hdr(host),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHost2' '%[req.hdr(host),base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'X-Forwarded-For' '11.22.33.44'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'x-forwarded-client-cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'reqTestHeader' 'bbb'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request set-header 'cache-control' 'private'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request del-header 'x-ssl-client-der'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-request del-header 'x-ssl-client-der'")).To(o.BeTrue())

			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-SSL-Server-Cert' '%{+Q}[ssl_c_der,base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-XSS-Protection' '1; mode=block'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-Content-Type-Options' 'nosniff`'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'X-Frame-Options' 'SAMEORIGIN'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer1' '%[res.hdr(server),lower]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer2' '%[res.hdr(server),base64]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'resTestServer3' '%[res.hdr(server)]'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response set-header 'cache-control' 'private'")).To(o.BeTrue())
			o.Expect(strings.Contains(haproxyBackendCfg, "http-response del-header 'server'")).To(o.BeTrue())
		}

		compat_otp.By("send traffic to the reen route, then check http headers in the request or response message")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		reenRouteDst := reenRouteHost + ":443:" + podIP
		curlReenRouteReq := []string{"-n", project1, clientPodName, "--", "curl", "https://" + reenRouteHost + "/headers", "-v", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", reenRouteDst, "--connect-timeout", "10"}
		curlReenRouteRes := []string{"-n", project1, clientPodName, "--", "curl", "https://" + reenRouteHost + "/headers", "-I", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", reenRouteDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		repeatCmdOnClient(oc, curlReenRouteRes, "200", 60, 1)
		reqHeaders, err := oc.AsAdmin().Run("exec").Args(curlReenRouteReq...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(len(regexp.MustCompile("\"X-Ssl-Client-Cert\": \"([0-9a-zA-Z]+)").FindStringSubmatch(reqHeaders)) > 0).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"X-Target\": \""+reenRouteHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost1\": \""+lowHostReen+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost2\": \""+base64HostReen+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost3\": \""+reenRouteHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtestheader\": \"bbb\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Cache-Control\": \"private\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, err := oc.AsAdmin().Run("exec").Args(curlReenRouteRes...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("resHeaders is: %v", resHeaders)
		o.Expect(len(regexp.MustCompile("x-ssl-server-cert: ([0-9a-zA-Z]+)").FindStringSubmatch(resHeaders)) > 0).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-xss-protection: 1; mode=block")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-content-type-options: nosniff")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "x-frame-options: SAMEORIGIN")).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver1: "+lowSrv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver2: "+base64Srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "restestserver3: "+srv)).To(o.BeTrue())
		o.Expect(strings.Contains(resHeaders, "cache-control: private")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "server:")).NotTo(o.BeTrue())
	})

	// incorporate OCPBUGS-40850 and OCPBUGS-43095 into one
	// [OCPBUGS-40850](https://issues.redhat.com/browse/OCPBUGS-40850)
	// [OCPBUGS-43095](https://issues.redhat.com/browse/OCPBUGS-43095)
	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-ConnectedOnly-High-77284-http request with duplicated headers should not cause disruption to a router pod", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = ingressControllerDescription{
				name:      "77284",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("2.0 Deploy a project with a client pod, a backend pod and its service resources")
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, clientPod)
		ensurePodWithLabelReady(oc, project1, clientPodLabel)
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=httpbin-pod")

		compat_otp.By("3.0 Create a HTTP route inside the project")
		routehost := "service-unsecure77284" + "." + ingctrl.domain
		createRoute(oc, project1, "http", unsecsvcName, unsecsvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, project1, unsecsvcName, "default")

		compat_otp.By("4.0: Curl the http route with two same headers in the http request, expect to get a 400 bad request if the backend server does not support such an invalid http request")
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		cmdOnPod := []string{"-n", project1, clientPodName, "--", "curl", "-I", "http://" + routehost + "/headers", "-H", `"transfer-encoding: chunked"`, "-H", `"transfer-encoding: chunked"`, "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, cmdOnPod, "400", 60, 1)

		compat_otp.By("5.0: Check that the custom router pod is Running, not Terminating")
		output := getByJsonPath(oc, "openshift-ingress", "pods/"+routerpod, "{.status.phase}")
		o.Expect(output).To(o.ContainSubstring("Running"))
	})
})
