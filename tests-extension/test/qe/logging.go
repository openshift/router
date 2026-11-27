package qe

import (
	"fmt"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	util "github.com/openshift/router-tests-extension/test"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Logging", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-logging", compat_otp.KubeConfigPath())

	//Author:shudili@redhat.com
	g.It("Capture and log http cookies with specific prefixes via httpCaptureCookies option", g.Label("OCP-34166"), func() {
		buildPruningBaseDir := TestdataDir()
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := `
    logging:
      access:
        destination:
          type: Container
        httpCaptureCookies:
        - matchType: Prefix
          maxLength: 100
          namePrefix: foo
`
		customTemp := util.AddExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		g.DeferCleanup(os.Remove, customTemp)

		var (
			testPodSvc     = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo      = "web-server-deploy"
			unsecSvcName   = "service-unsecure"
			clientPod      = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodLabel = "app=hello-pod"
			ingctrl        = util.IngressControllerDescription{
				Name:      "ocp34166",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create a custom IC for logging http cookies with the specific prefix")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		defer func() {
			o.Expect(ingctrl.Delete(oc)).NotTo(o.HaveOccurred(), "OCP-34166: 1.0 Create a custom IC for logging http cookies with the specific prefix")
		}()
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3.0: Create a http route for the testing")
		routehost := "unsecure34166." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.Name)

		compat_otp.By("4.0: Check httpCaptureCookies configuration in haproxy")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend public", []string{"capture cookie foo len 100"})

		compat_otp.By("5.0: Curl the http route with cookie fo=nobar, expect to get 200 OK")
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 80, podIP)
		curlCmd := []string{"-n", ns, "hello-pod", "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "fo=nobar", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("6.0: Curl the http route with cookie foo=bar, expect to get 200 OK")
		curlCmd = []string{"-n", ns, "hello-pod", "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo=bar", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("7.0: Check the router logs, which should contain both the cookie foo=bar and the url")
		logs := util.WaitRouterLogsAppear(oc, routerpod, "foo=bar")
		o.Expect(logs).To(o.ContainSubstring("index.html"), "OCP-34166: 7.0: Check the router logs, which should contain both the cookie foo=bar and the url")

		compat_otp.By("8.0: Check the router logs, which should NOT contain the cookie fo=nobar")
		logs, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", "-c", "logs", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-34166: 8.0: Check the router logs, which should NOT contain the cookie fo=nobar")
		o.Expect(logs).NotTo(o.ContainSubstring("fo=nobar"), "OCP-34166: 8.0: Check the router logs, which should NOT contain the cookie fo=nobar")

		compat_otp.By("9.0: Curl the http route with cookie foo22=bar22, expect to get 200 OK")
		curlCmd = []string{"-n", ns, "hello-pod", "--", "curl", "http://" + routehost + "/index.html", "-sI", "-b", "foo22=bar22", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("10.0: Check the router logs, which should contain both the cookie foo22=bar22 and the url")
		logs = util.WaitRouterLogsAppear(oc, routerpod, "foo22=bar22")
		o.Expect(logs).To(o.ContainSubstring("index.html"), "OCP-34166: 10.0: Check the router logs, which should contain both the cookie foo22=bar22 and the url")
	})

	//Author:shudili@redhat.com
	g.It("Capture and log http requests using UniqueID with custom logging format defined via httpHeader option", g.Label("OCP-34188"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodLabel      = "app=hello-pod"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp34188",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.Name
		)

		compat_otp.By("1.0: Create a custom IC for logging http requests")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		defer func() {
			o.Expect(ingctrl.Delete(oc)).NotTo(o.HaveOccurred(), "OCP-34188: 1.0: Create a custom IC for logging http requests")
		}()
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("1.1: Patch the hostNetwork ingresscontroller with logging http requests using the UniqueID")
		patchPath := `{"spec":{"httpHeaders":{"uniqueId":{"format":"%{+Q}b","name":"X-Request-Id"}},"logging":{"access":{"destination":{"type":"Container"},"httpLogFormat":"%ID %ci:%cp [%tr] %ft %s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %CC %CS %tsc %hr %hs %{+Q}r"}}}}`
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, patchPath)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3.0: Create a http route for the testing")
		routehost := "unsecure34188." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.Name)

		compat_otp.By("4.0: Check the unique-id configuration in haproxy")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "defaults", []string{`log-format "%ID %ci:%cp [%tr] %ft %s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %CC %CS %tsc %hr %hs %{+Q}r"`})
		publicCfg := util.GetBlockConfig(oc, routerpod, "frontend public")
		o.Expect(publicCfg).Should(o.And(
			o.ContainSubstring(`unique-id-format "%{+Q}b"`),
			o.ContainSubstring(`unique-id-header X-Request-Id`)), "OCP-34188: 4.0: Check the unique-id configuration in haproxy")

		compat_otp.By("5.0: Curl the http route, expect to get 200 OK")
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 80, podIP)
		curlCmd := []string{"-n", ns, "hello-pod", "--", "curl", "http://" + routehost + "/index.html", "-sI", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("6.0: Checking the access log verify if the UniqueID pattern is logged and matches")
		pattern := fmt.Sprintf(`"be_http:%s:route-http"`, ns)
		util.WaitRouterLogsAppear(oc, routerpod, pattern)
	})

	//Author:shudili@redhat.com
	g.It("The httpCaptureHeaders option strictly adheres to the maxlength parameter", g.Label("OCP-34191"), func() {
		buildPruningBaseDir := TestdataDir()
		baseTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		extraParas := `
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
`
		customTemp := util.AddExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		g.DeferCleanup(os.Remove, customTemp)

		var (
			testPodSvc     = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo      = "web-server-deploy"
			unsecSvcName   = "service-unsecure"
			clientPod      = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodLabel = "app=hello-pod"
			server         = "nginx"
			ingctrl        = util.IngressControllerDescription{
				Name:      "ocp34191",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0 Create a custom IC for logging http headers with the specified maxLength")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		defer func() {
			o.Expect(ingctrl.Delete(oc)).NotTo(o.HaveOccurred(), "OCP-34191: 1.0 Create a custom IC for logging http headers with the specified maxLength")
		}()
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2.0: Create a client pod, a deployment and the services in a namespace")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3.0: Create a http route for the testing")
		routehostPrefix := "unsecure34191"
		routehost := routehostPrefix + "." + ingctrl.Domain
		util.CreateRoute(oc, ns, "http", "route-http", unsecSvcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.Name)

		compat_otp.By("4.0: Check httpCaptureHeaders configuration in haproxy")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, "frontend fe_sni", []string{"capture request header Host len 13", "capture response header Server len 5"})

		compat_otp.By("5.0: Curl the http route, expect to get 200 OK")
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 80, podIP)
		curlCmd := []string{"-n", ns, "hello-pod", "--", "curl", "http://" + routehost + "/index.html", "-sI", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "200 OK", 90, 6)

		compat_otp.By("6.0: Check the router logs, which should contain the request host header and response server header with values not exceeding the maxLength")
		logs := util.WaitRouterLogsAppear(oc, routerpod, routehostPrefix)
		o.Expect(logs).NotTo(o.ContainSubstring(routehostPrefix+"."), "OCP-34191: 6.0: Check the router logs, which should contain the request host header and response server header with values not exceeding the maxLength")
		o.Expect(logs).To(o.ContainSubstring(server), "OCP-34191: 6.0: Check the router logs, which should contain the request host header and response server header with values not exceeding the maxLength")
		o.Expect(logs).NotTo(o.ContainSubstring(server+"/"), "OCP-34191: 6.0: Check the router logs, which should contain the request host header and response server header with values not exceeding the maxLength")
	})
})
