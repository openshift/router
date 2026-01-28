package router

import (
	"github.com/openshift/router-tests-extension/test/testdata"
	"fmt"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
)

var _ = g.Describe("[sig-network-edge][OTP] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-logging", compat_otp.KubeConfigPath())

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-34166-capture and log http cookies with specific prefixes via httpCaptureCookies option", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    logging:
      access:
        destination:
          type: Container
        httpCaptureCookies:
        - matchType: Prefix
          maxLength: 100
          namePrefix: foo
`)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			testPodSvc     = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo      = "web-server-deploy"
			unsecSvcName   = "service-unsecure"
			clientPod      = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName  = "hello-pod"
			clientPodLabel = "app=hello-pod"
			ingctrl        = ingressControllerDescription{
				name:      "ocp34166",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create an custom IC for logging http cookies with the specific prefix")
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
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3.0: Create a http route for the testing")
		routehost := "unsecure34166" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		compat_otp.By("4.0: Check httpCaptureCookies configuration in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend public", []string{"capture cookie foo len 100"})

		compat_otp.By("5.0: Curl the http route with cookie fo=nobar, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "fo=nobar", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("6.0: Curl the http route with cookie foo=bar, expect to get 200 OK")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo=bar", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("7.0: Check the router logs, which should contain both the cookie foo=bar and the url")
		logs := waitRouterLogsAppear(oc, routerpod, "foo=bar")
		o.Expect(logs).To(o.ContainSubstring("index.html"))

		compat_otp.By("8.0: Check the router logs, which should NOT contain the cookie fo=nobar")
		logs, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", "-c", "logs", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(logs).NotTo(o.ContainSubstring("fo=nobar"))

		compat_otp.By("9.0: Curl the http route with cookie foo22=bar22, expect to get 200 OK")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo22=bar22", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("10.0: Check the router logs, which should contain both the cookie foo22=bar22 and the url")
		logs = waitRouterLogsAppear(oc, routerpod, "foo22=bar22")
		o.Expect(logs).To(o.ContainSubstring("index.html"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-34178-capture and log http cookies with exact match via httpCaptureCookies option", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    logging:
      access:
        destination:
          type: Container
        httpCaptureCookies:
        - matchType: Exact
          maxLength: 100
          name: foo
`)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			testPodSvc     = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo      = "web-server-deploy"
			unsecSvcName   = "service-unsecure"
			clientPod      = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName  = "hello-pod"
			clientPodLabel = "app=hello-pod"
			ingctrl        = ingressControllerDescription{
				name:      "ocp34178",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create an custom IC for logging http cookies with the exact cookie name")
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
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3.0: Create a http route for the testing")
		routehost := "unsecure34178" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		compat_otp.By("4.0: Check httpCaptureCookies configuration in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend public", []string{"capture cookie foo= len 100"})

		compat_otp.By("5.0: Curl the http route with cookie fooor=nobar, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "fooor=nobar", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("6.0: Curl the http route with cookie foo=bar, expect to get 200 OK")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo=bar", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("7.0: Check the router logs, which should contain both the cookie foo=bar and the url")
		logs := waitRouterLogsAppear(oc, routerpod, "foo=bar")
		o.Expect(logs).To(o.ContainSubstring("index.html"))

		compat_otp.By("8.0: Check the router logs, which should NOT contain the cookie fooor=nobar")
		logs, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", "-c", "logs", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(logs).NotTo(o.ContainSubstring("fooor=nobar"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-34188-capture and log http requests using UniqueID with custom logging format defined via httpHeader option", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = ingressControllerDescription{
				name:      "ocp34188",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  baseTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		compat_otp.By("1.0: Create an custom IC for logging http cookies with maxLength 10")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("1.1: Patch the hostNetwork ingresscontroller with logging http requests using the UniqueID ")
		patchPath := `{"spec":{"httpHeaders":{"uniqueId":{"format":"%{+Q}b","name":"X-Request-Id"}},"logging":{"access":{"destination":{"type":"Container"},"httpLogFormat":"%ID %ci:%cp [%tr] %ft %s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %CC %CS %tsc %hr %hs %{+Q}r"}}}}`
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchPath)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3.0: Create a http route for the testing")
		routehost := "unsecure34188" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		compat_otp.By("4.0: Check the unique-id configuration in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "defaults", []string{`log-format "%ID %ci:%cp [%tr] %ft %s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %CC %CS %tsc %hr %hs %{+Q}r"`})
		publicCfg := getBlockConfig(oc, routerpod, "frontend public")
		o.Expect(publicCfg).Should(o.And(
			o.ContainSubstring(`unique-id-format "%{+Q}b"`),
			o.ContainSubstring(`unique-id-header X-Request-Id`)))

		compat_otp.By("5.0: Curl the http route, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("6.0: Checking the access log verify if the UniqueID pattern is logged and matches")
		pattern := fmt.Sprintf(`"be_http:%s:route-http"`, ns)
		waitRouterLogsAppear(oc, routerpod, pattern)
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Medium-34189-The httpCaptureCookies option strictly adheres to the maxlength parameter", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    logging:
      access:
        destination:
          type: Container
        httpCaptureCookies:
        - matchType: Prefix
          maxLength: 10
          namePrefix: foo
`)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			testPodSvc     = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo      = "web-server-deploy"
			unsecSvcName   = "service-unsecure"
			clientPod      = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName  = "hello-pod"
			clientPodLabel = "app=hello-pod"
			ingctrl        = ingressControllerDescription{
				name:      "ocp34189",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create an custom IC for logging http cookies with maxLength 10")
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
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3.0: Create a http route for the testing")
		routehost := "unsecure34189" + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		compat_otp.By("4.0: Check httpCaptureCookies configuration in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend public", []string{"capture cookie foo len 10"})

		compat_otp.By("5.0: Curl the http route with cookie foo=bar8 which length less than 10, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo=bar8", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("6.0: Curl the http route with cookie foo2=bar9abcdefg which length larger than 10, expect to get 200 OK")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo2=bar9abcdefg", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("7.0: Check the router logs for cookie foo=bar8 which length less than 10")
		logs := waitRouterLogsAppear(oc, routerpod, "foo=bar8")
		o.Expect(logs).To(o.ContainSubstring("index.html"))

		compat_otp.By("8.0: Check the router logs for cookie foo2=bar9abcdefg which length larger than 10")
		logs = waitRouterLogsAppear(oc, routerpod, "foo2=bar9a")
		o.Expect(logs).To(o.ContainSubstring("index.html"))
		o.Expect(logs).NotTo(o.ContainSubstring("foo2=bar9ab"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Medium-34191-The httpCaptureHeaders option strictly adheres to the maxlength parameter", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := fmt.Sprintf(`
    logging:
      access:
        destination:
          type: Container
        httpCaptureHeaders:
          request:
          - maxLength: 13
            name: Host
          response:
          - maxLength: 5
            name: Server
`)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)

		var (
			testPodSvc     = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo      = "web-server-deploy"
			unsecSvcName   = "service-unsecure"
			clientPod      = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName  = "hello-pod"
			clientPodLabel = "app=hello-pod"
			server         = "nginx"
			ingctrl        = ingressControllerDescription{
				name:      "ocp34191",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create an custom IC for logging http headers with the specified maxLength")
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
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3.0: Create a http route for the testing")
		routehostPrefix := "unsecure34191"
		routehost := routehostPrefix + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		compat_otp.By("4.0: Check httpCaptureHeaders configuration in haproxy")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"capture request header Host len 13", "capture response header Server len 5"})

		compat_otp.By("5.0: Curl the http route, expect to get 200 OK")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost + "/index.html", "-sI", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("6.0: Check the router logs, which should contain the request host header and response server header with values not exceeding the maxLength")
		logs := waitRouterLogsAppear(oc, routerpod, routehostPrefix)
		o.Expect(logs).NotTo(o.ContainSubstring(routehostPrefix + "."))
		o.Expect(logs).To(o.ContainSubstring(server))
		o.Expect(logs).NotTo(o.ContainSubstring(server + "/"))
	})
})
