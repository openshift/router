package qe

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

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	util "github.com/openshift/router-tests-extension/test"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Headers", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-headers", compat_otp.KubeConfigPath())

	// Includes OCP-34247: Different routes can have different policies with set-forwarded-headers annotations.
	//Author:shudili@redhat.com
	g.It("Configure Ingresscontroller to preserve existing header with forwardedHeaderPolicy set to Append", g.Label("OCP-34231"), func() {
		buildPruningBaseDir := TestdataDir()
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    httpHeaders:
      forwardedHeaderPolicy: Append
`)
		customTemp := util.AddExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			testPodSvc            = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecSvcName          = "httpbin-svc-insecure"
			clientPod             = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName         = "hello-pod"
			clientPodLabel        = "app=hello-pod"
			exampleXForwardedHost = "www.example-ne.com"
			ingctrl               = util.IngressControllerDescription{
				Name:      "ocp34231",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create an custom IC with forwardedHeaderPolicy Append")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("3.0: Create a http route for testing OCP-34231")
		routehost := "unsecure34231" + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.Name)

		compat_otp.By("4.0: Check the forwardedHeaderPolicy env, which should be append")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.PollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=append`)

		compat_otp.By("5.0: Curl the http route with a specified X-Forwarded-Host, then check the X-Forwarded-Host header value, which should contain both the exampleXForwardedHost and routehost")
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ := util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s,%s"`, exampleXForwardedHost, routehost)))

		// OCP-34247(Different Routes can have different policy with haproxy.router.openshift.io/set-forwarded-headers annotations)
		compat_otp.By("6.0: Create two http routes for testing OCP-34247")
		routehostOCP34247a := "unsecure34247a" + "." + ingctrl.Domain
		routehostOCP34247b := "unsecure34247b" + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", "route-http34247a", unsecSvcName, []string{"--hostname=" + routehostOCP34247a})
		util.CreateRoute(oc, ns, "http", "route-http34247b", unsecSvcName, []string{"--hostname=" + routehostOCP34247b})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http34247a", ingctrl.Name)
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http34247b", ingctrl.Name)

		compat_otp.By("7.0 Add the set-forwarded-headers annotations to the two routes, one with replace, the other with never, then check it in haproxy")
		util.SetAnnotation(oc, ns, "route/route-http34247a", "haproxy.router.openshift.io/set-forwarded-headers=replace")
		util.SetAnnotation(oc, ns, "route/route-http34247b", "haproxy.router.openshift.io/set-forwarded-headers=never")
		backend1Start := "backend be_http:" + ns + ":route-http34247a"
		backend2Start := "backend be_http:" + ns + ":route-http34247b"
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backend1Start, []string{"http-request set-header X-Forwarded-Host %[req.hdr(host)]"})
		backend2Cfg := util.GetBlockConfig(oc, routerpod, backend2Start)
		o.Expect(backend2Cfg).NotTo(o.ContainSubstring(`http-request set-header X-Forwarded-Host`))

		compat_otp.By("8.0: Curl the replace annotation http route with a specified X-Forwarded-Host, then check the header value which should be replaced by routehostOCP34247a")
		toDst = routehostOCP34247a + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehostOCP34247a + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s"`, routehostOCP34247a)))

		compat_otp.By("9.0: Curl the never annotation http route, then check the http headers which should not contain the X-Forwarded-Host header")
		toDst = routehostOCP34247b + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehostOCP34247b + "/headers", "-sv", "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).NotTo(o.ContainSubstring(`X-Forwarded-Host`))
	})

	// includes OCP-34233 and OCP-34246
	// author: shudili@redhat.com
	g.It("Configure Ingresscontroller to replace any existing Forwarded header with forwardedHeaderPolicy set to Replace", g.Label("OCP-34233"), func() {
		buildPruningBaseDir := TestdataDir()
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    httpHeaders:
      forwardedHeaderPolicy: Replace
`)
		customTemp := util.AddExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			testPodSvc            = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecSvcName          = "httpbin-svc-insecure"
			clientPod             = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName         = "hello-pod"
			clientPodLabel        = "app=hello-pod"
			exampleXForwardedHost = "www.example-ne.com"
			ingctrl               = util.IngressControllerDescription{
				Name:      "ocp34233",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create an custom IC with forwardedHeaderPolicy Replace")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("3.0: Create a http route for testing OCP-34233")
		routehost := "unsecure34233" + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.Name)

		compat_otp.By("4.0: Check the forwardedHeaderPolicy env, which should be replace")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.PollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=replace`)

		compat_otp.By("5.0: Curl the http route, then check the X-Forwarded-Host header value: exampleXForwardedHost should be replaced by routehost")
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ := util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s"`, routehost)))

		// OCP-34246(Configure a different header policy for the route with haproxy.router.openshift.io/set-forwarded-headers annotations)
		compat_otp.By("6.0: Create a http route for the testing OCP-34246")
		routehost = "unsecure34246" + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", "route-http34246", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http34246", ingctrl.Name)

		compat_otp.By("7.0 Add set-forwarded-headers=if-none annotation to the route, then check it in haproxy")
		util.SetAnnotation(oc, ns, "route/route-http34246", "haproxy.router.openshift.io/set-forwarded-headers=if-none")
		backendStart := "be_http:" + ns + ":route-http34246"
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backendStart, []string{"option forwardfor if-none"})

		// For if-none, if the http request already has the X-Forwarded-Host header, cluster won't append or replace with its info
		compat_otp.By("8.0: Curl the http route with a specified X-Forwarded-Host, then check the X-Forwarded-Host header value, which should not be replaced by routehost or not be appended by routehost")
		toDst = routehost + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s"`, exampleXForwardedHost)))

		// For if-none, if the http request has not the X-Forwarded-Proto-Version header, cluster will create it
		compat_otp.By("9.0: Curl the http route with a specified X-Forwarded-Proto-Version, then check the header value is added")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Proto-Version: http2", "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Proto-Version": "%s"`, "http2")))
	})

	// includes OCP-34234, OCP-34235 and OCP-34236
	// author: shudili@redhat.com
	g.It("Configure Ingresscontroller to set the headers if they are not already set with forwardedHeaderPolicy set to Ifnone", g.Label("OCP-34234"), func() {
		// OCP-34236(forwardedHeaderPolicy option defaults to Append if none is defined in the ingresscontroller configuration)
		var (
			buildPruningBaseDir   = TestdataDir()
			baseTemp              = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc            = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecSvcName          = "httpbin-svc-insecure"
			clientPod             = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName         = "hello-pod"
			clientPodLabel        = "app=hello-pod"
			exampleXForwardedHost = "www.example-ne.com"
			ingctrl               = util.IngressControllerDescription{
				Name:      "ocp34236",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
		)

		compat_otp.By("1.0 Create an custom IC with default forwardedHeaderPolicy Append")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("3.0: Create a http route for the testing")
		routehost := "unsecure34236" + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.Name)

		compat_otp.By("4.0: Check httpCaptureHeaders configuration in haproxy")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.PollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=append`)

		compat_otp.By("5.0: Curl the http route with specified X-Forwarded-Host, then check the X-Forwarded-Host header value, which should contain both the exampleXForwardedHost and routehost")
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ := util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s,%s"`, exampleXForwardedHost, routehost)))

		// OCP-34234(Configure Ingresscontroller to set the headers if they are not already set with forwardedHeaderPolicy set to Ifnone)
		compat_otp.By("6.0 Patch the httpHeaders with forwardedHeaderPolicy IfNone")
		patchPath := `{"spec":{"httpHeaders":{"forwardedHeaderPolicy":"IfNone"}}}`
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("7.0: Check httpCaptureHeaders configuration in haproxy")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.PollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=if-none`)

		// For if-none, if the http request already has the X-Forwarded-Host header, cluster won't append or replace with its info
		compat_otp.By("8.0: Curl the http route with a specified X-Forwarded-Host, then check the X-Forwarded-Host header value, which should not be replaced by routehost or not be appended by routehost")
		podIP = util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Host: " + exampleXForwardedHost, "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Host": "%s"`, exampleXForwardedHost)))

		// For if-none, if the http request has not the X-Forwarded-Host header, cluster will create it
		compat_otp.By("9.0: Curl the http route with a specified X-Forwarded-Proto-Version, then check the header value is added")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "-H", "X-Forwarded-Proto-Version: http2", "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).To(o.ContainSubstring(fmt.Sprintf(`"X-Forwarded-Proto-Version": "%s"`, "http2")))

		// OCP-34235(Configure Ingresscontroller to never set the headers and preserve existing with forwardedHeaderPolicy set to Never)
		compat_otp.By("10.0 Patch the httpHeaders with forwardedHeaderPolicy Never")
		patchPath = `{"spec":{"httpHeaders":{"forwardedHeaderPolicy":"Never"}}}`
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")

		compat_otp.By("11.0: Check httpCaptureHeaders configuration in haproxy")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.PollReadPodData(oc, "openshift-ingress", routerpod, "/usr/bin/env", `ROUTER_SET_FORWARDED_HEADERS=never`)

		compat_otp.By("12.0: Curl the http route with a specified X-Forwarded-Host, then check the http headers which should not contain it")
		podIP = util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/headers", "-sv", "--resolve", toDst, "--connect-timeout", "10"}
		output, _ = util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 60, 1)
		o.Expect(output).NotTo(o.ContainSubstring(`X-Forwarded-Host`))
	})

	// bug: 1816540 1803001 1816544
	g.It("Forwarded header includes empty quoted proto-version parameter", g.Label("OCP-57012"), func() {
		compat_otp.By("Check haproxy-config.template file in a router pod and make sure proto-version is removed from the Forwarded header")
		podname := util.GetOneRouterPodNameByIC(oc, "default")
		templateConfig := util.ReadRouterPodData(oc, podname, "cat haproxy-config.template", "http-request add-header Forwarded")
		o.Expect(templateConfig).To(o.ContainSubstring("proto"))
		o.Expect(templateConfig).NotTo(o.ContainSubstring("proto-version"))

		compat_otp.By("Check proto-version is also removed from the haproxy.config file in a router pod")
		haproxyConfig := util.ReadRouterPodData(oc, podname, "cat haproxy.config", "proto")
		o.Expect(haproxyConfig).NotTo(o.ContainSubstring("proto-version"))
	})

	// author: shudili@redhat.com
	g.It("adding/deleting http headers to an edge route by a router owner", g.Label("OCP-62528"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp62528",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
			podFileDir      = "/data/OCP-62528-ca"
			podCaCert       = podFileDir + "/62528-ca.pem"
			podCustomKey    = podFileDir + "/user62528.key"
			podCustomCert   = podFileDir + "/user62528.pem"
			fileDir         = ""
			dirname         = ""
			name            = ""
			validity        = 30
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = ""
			customKey       = ""
			customCert      = ""
		)
		var err error
		dirname, err = os.MkdirTemp("", "ocp-62528-ca-")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private CA directory for OCP-62528")
		defer os.RemoveAll(dirname)
		fileDir = dirname
		name = filepath.Join(dirname, "62528")
		userCert = filepath.Join(dirname, "user62528")
		customKey = userCert + ".key"
		customCert = userCert + ".pem"
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain

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
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.`+ingctrl.Domain+`") -req -in %s.csr -CA %s-ca.pem -CAkey %s-ca.key -CAcreateserial -out %s.pem -days %d -sha256`, userCert, name, name, userCert, validity)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a custom ingresscontroller")
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		cmFile := "ca-bundle.pem=" + name + "-ca.pem"
		defer util.DeleteConfigMap(oc, "openshift-config", "client-ca-"+ingctrl.Name)
		util.CreateConfigMapFromFile(oc, "openshift-config", "client-ca-"+ingctrl.Name, cmFile)

		compat_otp.By("patch the ingresscontroller to enable client certificate with required policy")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-"+ingctrl.Name+"\"},\"clientCertificatePolicy\":\"Required\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		// Mirror the client image so the pod pulls from the integrated registry
		// instead of the external source; the known-image-checker monitor rejects
		// pods that reference an unmirrored image.
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", util.MirrorFixtureImages(oc, ns, clientPod)).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, fileDir, ns+"/"+clientPodName+":"+podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("create an edge route")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		edgeRouteHost := "r3-edge62528." + ingctrl.Domain
		lowHostEdge := strings.ToLower(edgeRouteHost)
		base64HostEdge := base64.StdEncoding.EncodeToString([]byte(edgeRouteHost))
		edgeRouteDst := edgeRouteHost + ":443:" + podIP
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "route", "edge", "r3-edge", "--service="+unsecsvcName, "--cert="+customCert, "--key="+customKey, "--ca-cert="+name+"-ca.pem", "--hostname="+edgeRouteHost).Execute()
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

		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/r3-edge", "-p", patchHeaders, "--type=merge", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("patched"))

		compat_otp.By("check backend edge route in haproxy that headers to be set or deleted")
		routeBackendCfg := util.EnsureHaproxyBlockConfigContains(oc, routerpod, "be_edge_http:"+ns+":r3-edge", []string{"X-SSL-Client-Cert"})
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
		curlEdgeRouteReq := []string{"-n", ns, clientPodName, "--", "curl", "https://" + edgeRouteHost + "/headers", "-v", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		curlEdgeRouteRes := []string{"-n", ns, clientPodName, "--", "curl", "https://" + edgeRouteHost + "/headers", "-I", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		util.RepeatCmdOnClient(oc, curlEdgeRouteRes, "200", 60, 1)
		reqHeaders, _ := oc.AsAdmin().Run("exec").Args(curlEdgeRouteReq...).Output()
		util.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(len(regexp.MustCompile("\"X-Ssl-Client-Cert\": \"([0-9a-zA-Z]+)").FindStringSubmatch(reqHeaders)) > 0).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"X-Target\": \""+edgeRouteHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost1\": \""+lowHostEdge+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost2\": \""+base64HostEdge+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost3\": \""+edgeRouteHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtestheader\": \"bbb\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Cache-Control\": \"private\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, _ := oc.AsAdmin().Run("exec").Args(curlEdgeRouteRes...).Output()
		util.Logf("resHeaders is: %v", resHeaders)
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
	g.It("adding/deleting http headers to a http route by a router owner", g.Label("OCP-66560"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp66560",
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
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routeHost := "service-unsecure66560" + "." + ingctrl.Domain
		lowHost := strings.ToLower(routeHost)
		base64Host := base64.StdEncoding.EncodeToString([]byte(routeHost))
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routeHost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := util.GetRoutes(oc, ns)
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

		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", patchHeaders, "--type=merge", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("patched"))

		compat_otp.By("check backend edge route in haproxy that headers to be set or deleted")
		routerpod := util.GetOneRouterPodNameByIC(oc, ingctrl.Name)
		routeBackendCfg := util.EnsureHaproxyBlockConfigContains(oc, routerpod, "be_http:"+ns+":"+unsecsvcName, []string{"X-SSL-Client-Cert"})
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
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routeHost + ":80:" + podIP
		curlHTTPRouteReq := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routeHost + "/headers", "-v", "-e", "www.qe-test.com", "--resolve", toDst, "--connect-timeout", "10"}
		curlHTTPRouteRes := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routeHost + "/headers", "-I", "-e", "www.qe-test.com", "--resolve", toDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		util.RepeatCmdOnClient(oc, curlHTTPRouteRes, "200", 60, 1)
		reqHeaders, _ := oc.AsAdmin().Run("exec").Args(curlHTTPRouteReq...).Output()
		util.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(strings.Contains(reqHeaders, "\"X-Ssl-Client-Cert\": \"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"X-Target\": \""+routeHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost1\": \""+lowHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost2\": \""+base64Host+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtesthost3\": \""+routeHost+"\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Reqtestheader\": \"bbb\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "\"Cache-Control\": \"private\"")).To(o.BeTrue())
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, _ := oc.AsAdmin().Run("exec").Args(curlHTTPRouteRes...).Output()
		util.Logf("resHeaders is: %v", resHeaders)
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
	g.It("supported max http headers, max length of a http header name, max length value of a http header", g.Label("OCP-66566"), func() {
		var (
			buildPruningBaseDir      = TestdataDir()
			customTemp               = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc               = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName             = "httpbin-svc-insecure"
			clientPod                = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName            = "hello-pod"
			clientPodLabel           = "app=hello-pod"
			maxHTTPHeaders           = 20
			maxLengthHTTPHeaderName  = 255
			maxLengthHTTPHeaderValue = 16384
			ingctrl                  = util.IngressControllerDescription{
				Name:      "ocp66566",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "service-unsecure66566" + "." + "apps." + baseDomain
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routehost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := util.GetRoutes(oc, ns)
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
		util.PatchResourceAsAdmin(oc, ns, "route/"+unsecsvcName, patchHeaders)
		routeBackend := "be_http:" + ns + ":" + unsecsvcName
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		routeBackendCfg := util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"testheader1"})
		o.Expect(strings.Count(routeBackendCfg, "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))

		compat_otp.By("send traffic and check the max http headers specified in a route")
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-Is", "http://" + routehost + "/headers", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, cmdOnPod, "200", 60, 1)
		resHeaders, err := oc.Run("exec").Args("-n", ns, clientPodName, "--", "curl", "-s", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Count(strings.ToLower(resHeaders), "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))

		compat_otp.By("try to patch the exceeded max headers to a route")
		util.PatchResourceAsAdmin(oc, ns, "route/"+unsecsvcName, "{\"spec\": {\"httpHeaders\": null}}")
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", negPatchHeaders, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("request headers list can't exceed 20 items"))

		compat_otp.By("patch a http header with max header name to a route")
		maxHeaderName := strings.ToLower(util.GetFixedLengthRandomString(maxLengthHTTPHeaderName))
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
		util.PatchResourceAsAdmin(oc, ns, "route/"+unsecsvcName, patchHeaders)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{maxHeaderName})

		compat_otp.By("send traffic and check the max header name specified in a route")
		resHeaders, err = oc.Run("exec").Args(clientPodName, "--", "curl", "-s", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(strings.ToLower(resHeaders), maxHeaderName+"\": \"value123abc\"")).To(o.BeTrue())

		compat_otp.By("try to patch the header to a route with its name exceeded the max length")
		util.PatchResourceAsAdmin(oc, ns, "route/"+unsecsvcName, "{\"spec\": {\"httpHeaders\": null}}")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", negPatchHeaders, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("exceeds the maximum length, which is 255"))

		compat_otp.By("patch a http header with max header value to a route")
		maxHeaderValue := util.GetFixedLengthRandomString(maxLengthHTTPHeaderValue)
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

		util.PatchResourceAsAdmin(oc, ns, "route/"+unsecsvcName, patchHeaders)
		haproxyHeaderName := util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"header123abc"})
		o.Expect(strings.Contains(haproxyHeaderName, "http-request set-header 'header123abc' '"+maxHeaderValue+"'")).To(o.BeTrue())

		compat_otp.By("try to patch the header to a route with its value exceeded the max length")
		util.PatchResourceAsAdmin(oc, ns, "route/"+unsecsvcName, "{\"spec\": {\"httpHeaders\": null}}")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", negPatchHeaders, "--type=merge", "-n", ns).Output()
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
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchHeaders)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP = util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"testheader1"})
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend public")
		o.Expect(strings.Count(routeBackendCfg, "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend fe_sni")
		o.Expect(strings.Count(routeBackendCfg, "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend fe_no_sni")
		o.Expect(strings.Count(routeBackendCfg, "ocp66566testheader")).To(o.Equal(maxHTTPHeaders))

		compat_otp.By("send traffic and check the max http headers specified in an ingress controller")
		icResHeaders, err := oc.Run("exec").Args(clientPodName, "--", "curl", "-Is", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Count(strings.ToLower(icResHeaders), "ocp66566testheader") == maxHTTPHeaders).To(o.BeTrue())
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", negPatchHeaders, "--type=merge", "-n", ingctrl.Namespace).Output()
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
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchHeaders)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP = util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst = routehost + ":80:" + podIP
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{maxHeaderName})
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend public")
		o.Expect(strings.Contains(routeBackendCfg, maxHeaderName)).To(o.BeTrue())
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend fe_sni")
		o.Expect(strings.Contains(routeBackendCfg, maxHeaderName)).To(o.BeTrue())
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend fe_no_sni")
		o.Expect(strings.Contains(routeBackendCfg, maxHeaderName)).To(o.BeTrue())

		compat_otp.By("send traffic and check the header with max length name specified in an ingress controller")
		icResHeaders, err = oc.Run("exec").Args(clientPodName, "--", "curl", "-Is", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(strings.ToLower(icResHeaders), maxHeaderName+": value123abc")).To(o.BeTrue())

		compat_otp.By("try to patch the header to an ingress controller with its name exceeded the max length")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", negPatchHeaders, "--type=merge", "-n", ingctrl.Namespace).Output()
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
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchHeaders)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "4")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"header123abc"})
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend public")
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'header123abc' '"+maxHeaderValue+"'")).To(o.BeTrue())
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend fe_sni")
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'header123abc' '"+maxHeaderValue+"'")).To(o.BeTrue())
		routeBackendCfg = util.GetBlockConfig(oc, routerpod, "frontend fe_no_sni")
		o.Expect(strings.Contains(routeBackendCfg, "http-response set-header 'header123abc' '"+maxHeaderValue+"'")).To(o.BeTrue())
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", negPatchHeaders, "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("Too long:.+than 16384"))
	})

	// author: shudili@redhat.com
	g.It("negative test of adding/deleting http headers", g.Label("OCP-66568"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp66568",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "service-unsecure66568" + "." + ingctrl.Name + baseDomain
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routehost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := util.GetRoutes(oc, ns)
		o.Expect(routeOutput).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("try to patch two same headers to a route")
		sameHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"testheader1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value1\"}}}, {\"name\": \"testheader1\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value1\"}}}]}}}}"
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", sameHeaders, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("Duplicate value:.+testheader1"))

		compat_otp.By("try to patch proxy header to a route")
		proxyHeader := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"proxy\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"http://100.200.1.1:80\"}}}]}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", proxyHeader, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Forbidden: the following headers may not be modified using this API: strict-transport-security, proxy, cookie, set-cookie"))

		compat_otp.By("try to patch host header to a route")
		hostHeader := `{"spec": {"httpHeaders": {"actions": {"request": [{"name": "host", "action": {"type": "Set", "set": {"value": "www.neqe-test.com"}}}]}}}}`
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", hostHeader, "--type=merge", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		jpath := `{.spec.httpHeaders.actions.request[?(@.name=="host")].action.set.value}`
		host := util.GetByJsonPath(oc, ns, "route/"+unsecsvcName, jpath)
		o.Expect(host).To(o.ContainSubstring("www.neqe-test.com"))

		compat_otp.By("try to patch strict-transport-security header to a route")
		hstsHeader := `{"spec": {"httpHeaders": {"actions": {"request": [{"name": "strict-transport-security", "action": {"type": "Set", "set": {"value": "max-age=31536000;includeSubDomains;preload"}}}]}}}}`
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", hstsHeader, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Forbidden: the following headers may not be modified using this API: strict-transport-security, proxy, cookie, set-cookie"))

		compat_otp.By("try to patch cookie header to a route")
		cookieHeader := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"cookie\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"cookie-test\"}}}]}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", cookieHeader, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Forbidden: the following headers may not be modified using this API: strict-transport-security, proxy, cookie, set-cookie"))

		compat_otp.By("try to patch set-cookie header to a route")
		setCookieHeader := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"set-cookie\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"set-cookie-test\"}}}]}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", setCookieHeader, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Forbidden: the following headers may not be modified using this API: strict-transport-security, proxy, cookie, set-cookie"))

		compat_otp.By("try to patch two same headers to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", sameHeaders, "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("Duplicate value:.+testheader1"))

		compat_otp.By("try to patch proxy header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", proxyHeader, "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("proxy header may not be modified via header actions"))

		compat_otp.By("try to patch host header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", hostHeader, "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("host header may not be modified via header actions"))

		compat_otp.By("try to patch strict-transport-security header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", hstsHeader, "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("strict-transport-security header may not be modified via header actions"))

		compat_otp.By("try to patch cookie header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", cookieHeader, "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("cookie header may not be modified via header actions"))

		compat_otp.By("try to patch set-cookie header to an ingress-controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", setCookieHeader, "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("set-cookie header may not be modified via header actions"))

		compat_otp.By("patch a request and a response headers to a route, while patch the same headers with the same header names but with different header values to an ingress-controller")
		routeHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"reqtestheader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"req111\"}}}], \"response\": [{\"name\": \"restestheader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"resaaa\"}}}]}}}}"
		icHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"reqtestheader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"req222\"}}}], \"response\": [{\"name\": \"restestheader\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"resbbb\"}}}]}}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", routeHeaders, "--type=merge", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, icHeaders)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("send traffic, check the request header reqtestheader which should be set to req111 by the route")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-I", "http://" + routehost + "/headers", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, cmdOnPod, "200", 60, 1)
		reqHeaders, err := oc.Run("exec").Args("-n", ns, clientPodName, "--", "curl", "http://"+routehost+"/headers", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(strings.ToLower(reqHeaders), "\"reqtestheader\": \"req111\"")).To(o.BeTrue())

		compat_otp.By("send traffic, check the response header restestheader which should be set to resbbb by the ingress-controller")
		resHeaders, err := oc.Run("exec").Args(clientPodName, "--", "curl", "http://"+routehost+"/headers", "-I", "--resolve", toDst, "--connect-timeout", "10").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(resHeaders, "restestheader: resbbb")).To(o.BeTrue())
	})

	// author: shudili@redhat.com
	g.It("set different type of values for a http header name and its value", g.Label("OCP-66569"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp66569",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
			routeResource   = "route/" + unsecsvcName
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Create a backend pod and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routehost := "service-unsecure66569" + "." + ingctrl.Name + baseDomain
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routehost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := util.GetRoutes(oc, ns)
		o.Expect(routeOutput).To(o.ContainSubstring(unsecsvcName))

		compat_otp.By("patch http headers with valid number, alphabet, a combination of both header names and header values to a route, and then check the added headers in haproxy.conf")
		validHeaders := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"001\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"20230906\"}}}, {\"name\": \"aBc\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"Wednesday\"}}}, {\"name\": \"test01\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"value01\"}}}]}}}}"
		util.PatchResourceAsAdmin(oc, ns, routeResource, validHeaders)
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		routeBackend := "be_http:" + ns + ":" + unsecsvcName
		routeBackendCfg := util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"test01"})
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header '001' '20230906'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aBc' 'Wednesday'")).To(o.BeTrue())
		o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'test01' 'value01'")).To(o.BeTrue())

		compat_otp.By("try to patch http header with blank value in the header name to a route")
		blankHeaderName := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aa bb\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"abc\"}}}]}}}}"
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", blankHeaderName, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"aa bb\": name must be a valid HTTP header name as defined in RFC 2616 section 4.2"))

		compat_otp.By("patch http header with #$* in the header name to a route, and then check it in haproxy.config")
		specialHeaderName1 := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aabbccdd#$*ee\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"abc\"}}}]}}}}"
		util.PatchResourceAsAdmin(oc, ns, routeResource, specialHeaderName1)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"http-request set-header 'aabbccdd#$*ee' 'abc'"})

		compat_otp.By("patch http header with ' in the header name to a route, and then check it in haproxy.config")
		specialHeaderName2 := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aabbccdd'ee\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"abc\"}}}]}}}}"
		util.PatchResourceAsAdmin(oc, ns, routeResource, specialHeaderName2)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"http-request set-header 'aabbccdd'\\''ee' 'abc'"})

		compat_otp.By("try to patch http header with \" in the header name to a route")
		specialHeaderName3 := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aabbccdd\\\"ee\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"abc\"}}}]}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", specialHeaderName3, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"aabbccdd\\\"ee\": name must be a valid HTTP header name"))

		compat_otp.By("patch http header with specical characters in header value to a route")
		specialHeaderValues := "{\"spec\": {\"httpHeaders\": {\"actions\": {\"request\": [{\"name\": \"aabbccddee\", \"action\": {\"type\": \"Set\", \"set\": {\"value\": \"vlalueabc #$*'\\\"cc\"}}}]}}}}"
		util.PatchResourceAsAdmin(oc, ns, routeResource, specialHeaderValues)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, routeBackend, []string{"http-request set-header 'aabbccddee' 'vlalueabc #$*'\\''\"cc'"})

		compat_otp.By("patch http headers with valid number, alphabet, a combination of both header names and header values to an ingress controller, then check the added headers in haproxy.conf")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, validHeaders)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"test01"})
		for _, backend := range []string{"frontend public", "frontend fe_sni", "frontend fe_no_sni"} {
			routeBackendCfg = util.GetBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header '001' '20230906'")).To(o.BeTrue())
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aBc' 'Wednesday'")).To(o.BeTrue())
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'test01' 'value01'")).To(o.BeTrue())
		}

		compat_otp.By("patch http header with blank value in the header name to an ingress controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", blankHeaderName, "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		util.Logf("blanck output is: %v", output)
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"aa bb\""))

		compat_otp.By("patch http header with #$* in the header name to an ingress controller")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, specialHeaderName1)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"aabbccdd"})
		for _, backend := range []string{"frontend public", "frontend fe_sni", "frontend fe_no_sni"} {
			routeBackendCfg = util.GetBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aabbccdd#$*ee' 'abc'")).To(o.BeTrue())
		}

		compat_otp.By("patch http header with ' in the header name to an ingress controller")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, specialHeaderName2)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "4")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"aabbccdd"})
		for _, backend := range []string{"frontend public", "frontend fe_sni", "frontend fe_no_sni"} {
			routeBackendCfg = util.GetBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aabbccdd'\\''ee' 'abc'")).To(o.BeTrue())
		}

		compat_otp.By("patch http header with \" in the header name to an ingress controller")
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/"+unsecsvcName, "-p", specialHeaderName3, "--type=merge", "-n", ns).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"aabbccdd\\\"ee\""))

		compat_otp.By("patch http header with specical characters in header value to an ingress controller")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, specialHeaderValues)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "5")
		routerpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"aabbccdd"})
		for _, backend := range []string{"frontend public", "frontend fe_sni", "frontend fe_no_sni"} {
			routeBackendCfg = util.GetBlockConfig(oc, routerpod, backend)
			o.Expect(strings.Contains(routeBackendCfg, "http-request set-header 'aabbccddee' 'vlalueabc #$*'\\''\"cc'")).To(o.BeTrue())
		}
	})

	// author: shudili@redhat.com
	g.It("adding/deleting http headers to a http route by an ingress-controller as a cluster administrator", g.Label("OCP-66572"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp66572",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
		)

		compat_otp.By("Create a custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("Expose a route with the unsecure service inside the namespace")
		routeHost := "service-unsecure66572" + "." + ingctrl.Domain
		lowHost := strings.ToLower(routeHost)
		base64Host := base64.StdEncoding.EncodeToString([]byte(routeHost))
		err := oc.Run("expose").Args("svc/"+unsecsvcName, "--hostname="+routeHost).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		routeOutput := util.GetRoutes(oc, ns)
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
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchHeaders)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("check the configured added/deleted headers under public frontend, fe_sni frontend and fe_no_sni frontend in haproxy")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"X-SSL-Client-Cert"})
		for _, backend := range []string{"frontend public", "frontend fe_sni", "frontend fe_no_sni"} {
			haproxyBackendCfg := util.GetBlockConfig(oc, routerpod, backend)
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
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		routeDst := routeHost + ":80:" + podIP
		curlHTTPRouteReq := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routeHost + "/headers", "-v", "-e", "www.qe-test.com", "--resolve", routeDst, "--connect-timeout", "10"}
		curlHTTPRouteRes := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routeHost + "/headers", "-I", "-e", "www.qe-test.com", "--resolve", routeDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		util.RepeatCmdOnClient(oc, curlHTTPRouteRes, "200", 60, 1)
		reqHeaders, err := oc.AsAdmin().Run("exec").Args(curlHTTPRouteReq...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.Logf("reqHeaders is: %v", reqHeaders)
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
		util.Logf("resHeaders is: %v", resHeaders)
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
	g.It("adding/deleting http headers to a reen route by a router owner", g.Label("OCP-66662"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
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
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp66662",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
			podFileDir      = "/data/OCP-66662-ca"
			podCaCert       = podFileDir + "/66662-ca.pem"
			podCustomKey    = podFileDir + "/user66662.key"
			podCustomCert   = podFileDir + "/user66662.pem"
			fileDir         = ""
			dirname         = ""
			name            = ""
			validity        = 30
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = ""
			customKey       = ""
			customCert      = ""
			destSubj        = "/CN=*.edge.example.com"
			destCA          = ""
			destKey         = ""
			destCsr         = ""
			destCnf         = ""
		)

		var err error
		dirname, err = os.MkdirTemp("", "ocp-66662-ca-")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private CA directory for OCP-66662")
		defer os.RemoveAll(dirname)
		fileDir = dirname
		name = filepath.Join(dirname, "66662")
		userCert = filepath.Join(dirname, "user66662")
		customKey = userCert + ".key"
		customCert = userCert + ".pem"
		destCA = filepath.Join(dirname, "dst.pem")
		destKey = filepath.Join(dirname, "dst.key")
		destCsr = filepath.Join(dirname, "dst.csr")
		destCnf = filepath.Join(dirname, "openssl.cnf")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		ns := oc.Namespace()
		compat_otp.SetNamespacePrivileged(oc, ns)

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
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.`+ingctrl.Domain+`") -req -in %s.csr -CA %s-ca.pem -CAkey %s-ca.key -CAcreateserial -out %s.pem -days %d -sha256`, userCert, name, name, userCert, validity)
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
`, secsvcName, ns)

		cmd := fmt.Sprintf(`echo "%s" > %s`, sanCfg, destCnf)
		_, err = exec.Command("bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile %s -extensions v3_req  -req -in %s -signkey  %s -days %d -sha256 -out %s`, destCnf, destCsr, destKey, validity, destCA)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a custom ingresscontroller")
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		cmFile := "ca-bundle.pem=" + name + "-ca.pem"
		defer util.DeleteConfigMap(oc, "openshift-config", "client-ca-"+ingctrl.Name)
		util.CreateConfigMapFromFile(oc, "openshift-config", "client-ca-"+ingctrl.Name, cmFile)

		compat_otp.By("patch the ingresscontroller to enable client certificate with required policy")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-"+ingctrl.Name+"\"},\"clientCertificatePolicy\":\"Required\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		// Mirror the client image so the pod pulls from the integrated registry
		// instead of the external source; the known-image-checker monitor rejects
		// pods that reference an unmirrored image.
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", util.MirrorFixtureImages(oc, ns, clientPod)).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, fileDir, ns+"/"+clientPodName+":"+podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.OperateResourceFromFile(oc, "create", ns, serverPod)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")
		util.CreateResourceFromFile(oc, ns, secsvc)

		compat_otp.By("Update the certification and key in the server pod")
		podName := util.GetPodListByLabel(oc, ns, "name=httpbin-pod")
		newSrvCert := ns + "/" + podName[0] + ":" + srvCert
		newSrvKey := ns + "/" + podName[0] + ":" + srvKey
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podName[0], "-c", "httpbin-https", "--", "bash", "-c", "rm -f "+srvCert).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podName[0], "-c", "httpbin-https", "--", "bash", "-c", "rm -f "+srvKey).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, destCA, "-c", "httpbin-https", newSrvCert).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, destKey, "-c", "httpbin-https", newSrvKey).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("create a reen route")
		reenRouteHost := "r2-reen66662." + ingctrl.Domain
		lowHostReen := strings.ToLower(reenRouteHost)
		base64HostReen := base64.StdEncoding.EncodeToString([]byte(reenRouteHost))
		reenRouteDst := reenRouteHost + ":443:" + podIP
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "route", "reencrypt", "r2-reen", "--service="+secsvcName, "--cert="+customCert, "--key="+customKey, "--ca-cert="+name+"-ca.pem", "--dest-ca-cert="+destCA, "--hostname="+reenRouteHost).Execute()
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

		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("route/r2-reen", "-p", patchHeaders, "--type=merge", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("patched"))

		compat_otp.By("check backend reen route in haproxy that headers to be set or deleted")
		routeBackendCfg := util.EnsureHaproxyBlockConfigContains(oc, routerpod, "be_secure:"+ns+":r2-reen", []string{"X-SSL-Client-Cert"})
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
		curlReenRouteReq := []string{"-n", ns, clientPodName, "--", "curl", "https://" + reenRouteHost + "/headers", "-v", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", reenRouteDst, "--connect-timeout", "10"}
		curlReenRouteRes := []string{"-n", ns, clientPodName, "--", "curl", "https://" + reenRouteHost + "/headers", "-I", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", reenRouteDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		util.Logf("curlReenRouteRes is: %v", curlReenRouteRes)
		util.RepeatCmdOnClient(oc, curlReenRouteRes, "200", 60, 1)
		reqHeaders, _ := oc.AsAdmin().Run("exec").Args(curlReenRouteReq...).Output()
		util.Logf("reqHeaders is: %v", reqHeaders)
		o.Expect(len(regexp.MustCompile("\"X-Ssl-Client-Cert\": \"([0-9a-zA-Z]+)").FindStringSubmatch(reqHeaders)) > 0).To(o.BeTrue())
		o.Expect(reqHeaders).To(o.ContainSubstring("\"X-Target\": \"" + reenRouteHost + "\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Reqtesthost1\": \"" + lowHostReen + "\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Reqtesthost2\": \"" + base64HostReen + "\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Reqtesthost3\": \"" + reenRouteHost + "\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Reqtestheader\": \"bbb\""))
		o.Expect(reqHeaders).To(o.ContainSubstring("\"Cache-Control\": \"private\""))
		o.Expect(strings.Contains(reqHeaders, "x-ssl-client-der")).NotTo(o.BeTrue())

		resHeaders, _ := oc.AsAdmin().Run("exec").Args(curlReenRouteRes...).Output()
		util.Logf("resHeaders is: %v", resHeaders)
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
	g.It("adding/deleting http headers to an edge route by an ingress-controller as a cluster administrator", g.Label("OCP-67009"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			srv                 = "gunicorn"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp67009",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
			podFileDir      = "/data/OCP-67009-ca"
			podCaCert       = podFileDir + "/67009-ca.pem"
			podCustomKey    = podFileDir + "/user67009.key"
			podCustomCert   = podFileDir + "/user67009.pem"
			fileDir         = ""
			dirname         = ""
			name            = ""
			validity        = 30
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = ""
			customKey       = ""
			customCert      = ""
		)
		var err error
		dirname, err = os.MkdirTemp("", "ocp-67009-ca-")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private CA directory for OCP-67009")
		defer os.RemoveAll(dirname)
		fileDir = dirname
		name = filepath.Join(dirname, "67009")
		userCert = filepath.Join(dirname, "user67009")
		customKey = userCert + ".key"
		customCert = userCert + ".pem"
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain

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
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.`+ingctrl.Domain+`") -req -in %s.csr -CA %s-ca.pem -CAkey %s-ca.key -CAcreateserial -out %s.pem -days %d -sha256`, userCert, name, name, userCert, validity)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a custom ingresscontroller")
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		cmFile := "ca-bundle.pem=" + name + "-ca.pem"
		defer util.DeleteConfigMap(oc, "openshift-config", "client-ca-"+ingctrl.Name)
		util.CreateConfigMapFromFile(oc, "openshift-config", "client-ca-"+ingctrl.Name, cmFile)

		compat_otp.By("patch the ingresscontroller to enable client certificate with required policy")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-"+ingctrl.Name+"\"},\"clientCertificatePolicy\":\"Required\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		// Mirror the client image so the pod pulls from the integrated registry
		// instead of the external source; the known-image-checker monitor rejects
		// pods that reference an unmirrored image.
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", util.MirrorFixtureImages(oc, ns, clientPod)).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, fileDir, ns+"/"+clientPodName+":"+podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("create an edge route")
		edgeRouteHost := "r3-edge67009." + ingctrl.Domain
		lowHostEdge := strings.ToLower(edgeRouteHost)
		base64HostEdge := base64.StdEncoding.EncodeToString([]byte(edgeRouteHost))
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "route", "edge", "r3-edge", "--service="+unsecsvcName, "--cert="+customCert, "--key="+customKey, "--ca-cert="+name+"-ca.pem", "--hostname="+edgeRouteHost).Execute()
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

		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchHeaders)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")

		compat_otp.By("check the configured added/deleted headers under public frontend, fe_sni frontend and fe_no_sni frontend in haproxy")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"X-SSL-Client-Cert"})
		for _, backend := range []string{"frontend public", "frontend fe_sni", "frontend fe_no_sni"} {
			haproxyBackendCfg := util.GetBlockConfig(oc, routerpod, backend)
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
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		edgeRouteDst := edgeRouteHost + ":443:" + podIP
		curlEdgeRouteReq := []string{"-n", ns, clientPodName, "--", "curl", "https://" + edgeRouteHost + "/headers", "-v", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		curlEdgeRouteRes := []string{"-n", ns, clientPodName, "--", "curl", "https://" + edgeRouteHost + "/headers", "-I", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", edgeRouteDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		util.RepeatCmdOnClient(oc, curlEdgeRouteRes, "200", 60, 1)
		reqHeaders, err := oc.AsAdmin().Run("exec").Args(curlEdgeRouteReq...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.Logf("reqHeaders is: %v", reqHeaders)
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
		util.Logf("resHeaders is: %v", resHeaders)
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
	g.It("adding/deleting http headers to a reen route by an ingress-controller as a cluster administrator", g.Label("OCP-67010"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
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
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp67010",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
			podFileDir      = "/data/OCP-67010-ca"
			podCaCert       = podFileDir + "/67010-ca.pem"
			podCustomKey    = podFileDir + "/user67010.key"
			podCustomCert   = podFileDir + "/user67010.pem"
			fileDir         = ""
			dirname         = ""
			name            = ""
			validity        = 30
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = ""
			customKey       = ""
			customCert      = ""
			destSubj        = "/CN=*.edge.example.com"
			destCA          = ""
			destKey         = ""
			destCsr         = ""
			destCnf         = ""
		)

		var err error
		dirname, err = os.MkdirTemp("", "ocp-67010-ca-")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private CA directory for OCP-67010")
		defer os.RemoveAll(dirname)
		fileDir = dirname
		name = filepath.Join(dirname, "67010")
		userCert = filepath.Join(dirname, "user67010")
		customKey = userCert + ".key"
		customCert = userCert + ".pem"
		destCA = filepath.Join(dirname, "dst.pem")
		destKey = filepath.Join(dirname, "dst.key")
		destCsr = filepath.Join(dirname, "dst.csr")
		destCnf = filepath.Join(dirname, "openssl.cnf")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		ns := oc.Namespace()
		compat_otp.SetNamespacePrivileged(oc, ns)

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
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.`+ingctrl.Domain+`") -req -in %s.csr -CA %s-ca.pem -CAkey %s-ca.key -CAcreateserial -out %s.pem -days %d -sha256`, userCert, name, name, userCert, validity)
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
`, secsvcName, ns)

		cmd := fmt.Sprintf(`echo "%s" > %s`, sanCfg, destCnf)
		_, err = exec.Command("bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile %s -extensions v3_req  -req -in %s -signkey  %s -days %d -sha256 -out %s`, destCnf, destCsr, destKey, validity, destCA)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("Create a custom ingresscontroller")
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		cmFile := "ca-bundle.pem=" + name + "-ca.pem"
		defer util.DeleteConfigMap(oc, "openshift-config", "client-ca-"+ingctrl.Name)
		util.CreateConfigMapFromFile(oc, "openshift-config", "client-ca-"+ingctrl.Name, cmFile)

		compat_otp.By("patch the ingresscontroller to enable client certificate with required policy")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-"+ingctrl.Name+"\"},\"clientCertificatePolicy\":\"Required\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("Create a client pod, a backend pod and its service resources")
		// Mirror the client image so the pod pulls from the integrated registry
		// instead of the external source; the known-image-checker monitor rejects
		// pods that reference an unmirrored image.
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", util.MirrorFixtureImages(oc, ns, clientPod)).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, fileDir, ns+"/"+clientPodName+":"+podFileDir).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.OperateResourceFromFile(oc, "create", ns, serverPod)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")
		util.CreateResourceFromFile(oc, ns, secsvc)

		compat_otp.By("Update the certification and key in the server pod")
		podName := util.GetPodListByLabel(oc, ns, "name=httpbin-pod")
		newSrvCert := ns + "/" + podName[0] + ":" + srvCert
		newSrvKey := ns + "/" + podName[0] + ":" + srvKey
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podName[0], "-c", "httpbin-https", "--", "bash", "-c", "rm -f "+srvCert).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, podName[0], "-c", "httpbin-https", "--", "bash", "-c", "rm -f "+srvKey).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, destCA, "-c", "httpbin-https", newSrvCert).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, destKey, "-c", "httpbin-https", newSrvKey).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("create a reen route")
		reenRouteHost := "r2-reen67010." + ingctrl.Domain
		lowHostReen := strings.ToLower(reenRouteHost)
		base64HostReen := base64.StdEncoding.EncodeToString([]byte(reenRouteHost))
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "route", "reencrypt", "r2-reen", "--service="+secsvcName, "--cert="+customCert, "--key="+customKey, "--ca-cert="+name+"-ca.pem", "--dest-ca-cert="+destCA, "--hostname="+reenRouteHost).Execute()
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

		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchHeaders)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")

		compat_otp.By("check the configured added/deleted headers under public frontend, fe_sni frontend and fe_no_sni frontend in haproxy")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"X-SSL-Client-Cert"})
		for _, backend := range []string{"frontend public", "frontend fe_sni", "frontend fe_no_sni"} {
			haproxyBackendCfg := util.GetBlockConfig(oc, routerpod, backend)
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
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		reenRouteDst := reenRouteHost + ":443:" + podIP
		curlReenRouteReq := []string{"-n", ns, clientPodName, "--", "curl", "https://" + reenRouteHost + "/headers", "-v", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", reenRouteDst, "--connect-timeout", "10"}
		curlReenRouteRes := []string{"-n", ns, clientPodName, "--", "curl", "https://" + reenRouteHost + "/headers", "-I", "--cacert", podCaCert, "--cert", podCustomCert, "--key", podCustomKey, "--resolve", reenRouteDst, "--connect-timeout", "10"}
		lowSrv := strings.ToLower(srv)
		base64Srv := base64.StdEncoding.EncodeToString([]byte(srv))
		util.RepeatCmdOnClient(oc, curlReenRouteRes, "200", 60, 1)
		reqHeaders, err := oc.AsAdmin().Run("exec").Args(curlReenRouteReq...).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.Logf("reqHeaders is: %v", reqHeaders)
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
		util.Logf("resHeaders is: %v", resHeaders)
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
	g.It("http request with duplicated headers should not cause disruption to a router pod", g.Label("OCP-77284"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "httpbin-deploy.yaml")
			unsecsvcName        = "httpbin-svc-insecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = util.IngressControllerDescription{
				Name:      "77284",
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
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		compat_otp.By("2.0 Create a client pod, a backend pod and its service resources")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=httpbin-pod")

		compat_otp.By("3.0 Create a HTTP route inside the namespace")
		routehost := "service-unsecure77284" + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", unsecsvcName, unsecsvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unsecsvcName, "default")

		compat_otp.By("4.0: Curl the http route with two same headers in the http request, expect to get a 400 bad request if the backend server does not support such an invalid http request")
		routerpod := util.GetOneRouterPodNameByIC(oc, ingctrl.Name)
		podIP := util.GetPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-I", "http://" + routehost + "/headers", "-H", `"transfer-encoding: chunked"`, "-H", `"transfer-encoding: chunked"`, "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, cmdOnPod, "400", 60, 1)

		compat_otp.By("5.0: Check that the custom router pod is Running, not Terminating")
		output := util.GetByJsonPath(oc, "openshift-ingress", "pods/"+routerpod, "{.status.phase}")
		o.Expect(output).To(o.ContainSubstring("Running"))
	})
})
