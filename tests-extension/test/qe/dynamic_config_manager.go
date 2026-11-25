package qe

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	util "github.com/openshift/router-tests-extension/test"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-DCM", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-dcm", compat_otp.KubeConfigPath())

	// Test case creater: shudili@redhat.com
	g.It("Check whether required ENV variables are configured after enabling Dynamic Configuration Manager [Serial]", g.Label("OCP-77862"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		srvrcInfo := "web-server-deploy"
		srvName := "service-unsecure"
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp77862",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1. Create a custom ingresscontroller, and get its router name")
		baseDomain := util.GetBaseDomain(oc)
		ns := oc.Namespace()
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		g.DeferCleanup(func() {
			o.Expect(ingctrl.Delete(oc)).To(o.Succeed(), "failed to delete DCM ingresscontroller %s", ingctrl.Name)
		})
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("1.1 Patch ingresscontroller with unsupportedConfigOverrides option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"unsupportedConfigOverrides\":{\"dynamicConfigManager\":\"true\", \"maxDynamicServers\":\"1\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		podname := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("2. Create an unsecure service and its backend pod")
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("3. Expose a route with the unsecure service inside the namespace")
		util.CreateRoute(oc, ns, "http", srvName, srvName, []string{})
		util.WaitForOutputEquals(oc, ns, "route", "{.items[0].metadata.name}", srvName)

		compat_otp.By("4. Check the env variable of the custom router pod")
		checkenv2 := util.ReadRouterPodEnv(oc, podname, "ROUTER")
		o.Expect(checkenv2).To(o.ContainSubstring(`ROUTER_BLUEPRINT_ROUTE_POOL_SIZE=0`), "OCP-77862: 4. Check the env variable of the custom router pod")
		o.Expect(checkenv2).To(o.ContainSubstring(`ROUTER_MAX_DYNAMIC_SERVERS=1`), "OCP-77862: 4. Check the env variable of the custom router pod")
		o.Expect(checkenv2).To(o.ContainSubstring(`ROUTER_HAPROXY_CONFIG_MANAGER=true`), "OCP-77862: 4. Check the env variable of the custom router pod")

		compat_otp.By("5. Check the haproxy config on the custom router pod for dynamic pod config")
		insecBackend := "be_http:" + ns + ":service-unsecure"
		util.EnsureHaproxyBlockConfigContains(oc, podname, insecBackend, []string{"dynamic-cookie-key"})
	})

	// author: shudili@redhat.com
	g.It("Dynamic Configuration Manager for plain HTTP route [Serial]", g.Label("OCP-77892"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp77892",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "unsecure77892" + "." + ingctrl.Domain
		g.DeferCleanup(func() {
			o.Expect(ingctrl.Delete(oc)).To(o.Succeed(), "failed to delete DCM ingresscontroller %s", ingctrl.Name)
		})
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		compat_otp.By("1.1 Patch ingresscontroller with unsupportedConfigOverrides option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"unsupportedConfigOverrides\":{\"dynamicConfigManager\":\"true\", \"maxDynamicServers\":\"1\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("2.0 Create a deployment, a HTTP route and a client pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		util.CreateRoute(oc, ns, "http", "unsecure77892", svcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "unsecure77892", ingctrl.Name)
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Curl the HTTP route")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.Logf("init routerpod is: %s", routerpod)
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 80, podIP)
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + routehost, "-s", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4.0 Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
		backend := "be_http:" + ns + ":unsecure77892"
		util.CheckDcmBackendConfig(oc, routerpod, backend)

		compat_otp.By("5.0 Use debug command to check the dynamic server's state")
		// used the socat command under the router pod to get all the route's endpoints status
		socatCmd := util.DCMServerStateCommand(backend)
		util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, 1)
		currentSrvStates := ""

		compat_otp.By("6.0 get the initial router reloaded log")
		util.ScaleDeploy(oc, ns, srvdmInfo, 2)
		// Both baselines describe two converged replicas. The loop classifies
		// changes starting with the next scale, from two replicas to three.
		initSrvStates := util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, 2)
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-77892: 6.0 get the initial router reloaded log")
		initReloadedNum := strings.Count(log, `"msg"="router reloaded"`)

		compat_otp.By("7.0 keep scaling up the deployment with replicas 1")
		for i := 2; i < desiredReplicas; i++ {
			compat_otp.By("7." + strconv.Itoa(i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i+1))
			util.ScaleDeploy(oc, ns, srvdmInfo, i+1)
			util.WaitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(i+1))

			compat_otp.By("7." + strconv.Itoa(i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("7." + strconv.Itoa(i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i+1)

			compat_otp.By("7." + strconv.Itoa(i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("8.0 keep scaling down the deployment with replicas 1")
		for i := desiredReplicas - 1; i >= 1; i-- {
			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("9.0 keep scaling up the deployment with replicas 2")
		maxReplicas := 0
		for i := 2; i <= desiredReplicas-2; i = i + 2 {
			compat_otp.By("9." + strconv.Itoa((i+2)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
			maxReplicas = i
		}

		compat_otp.By("10.0 keep scaling down the deployment with replicas 2")
		for i := maxReplicas - 2; i >= 0; i = i - 2 {
			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".4: get the router reloaded log")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}
	})

	// author: shudili@redhat.com
	g.It("Enable ALPN for reencrypt routes when DCM is enabled [Serial]", g.Label("OCP-77906"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
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
			dirname             = ""
			caSubj              = "/CN=NE-Test-Root-CA"
			caCrt               = ""
			caKey               = ""
			userSubj            = "/CN=example-ne.com"
			usrCrt              = ""
			usrKey              = ""
			usrCsr              = ""
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
		customTemp := util.AddExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		g.DeferCleanup(func() {
			o.Expect(os.Remove(customTemp)).To(o.Succeed(), "failed to remove DCM controller fixture %s", customTemp)
		})

		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp77906",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1.0 Get the domain info for testing")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "reen77906" + "." + ingctrl.Domain

		compat_otp.By("2.0: Start to use openssl to create ca certification&key and user certification&key")
		var err error
		dirname, err = os.MkdirTemp("", "ocp-77906-ca-")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private CA directory for OCP-77906")
		g.DeferCleanup(func() {
			o.Expect(os.RemoveAll(dirname)).To(o.Succeed(), "failed to remove DCM CA directory %s", dirname)
		})
		caCrt = filepath.Join(dirname, "77906-ca.crt")
		caKey = filepath.Join(dirname, "77906-ca.key")
		usrCrt = filepath.Join(dirname, "77906-usr.crt")
		usrKey = filepath.Join(dirname, "77906-usr.key")
		usrCsr = filepath.Join(dirname, "77906-usr.csr")

		compat_otp.By("2.1: Create a new self-signed CA including the ca certification and ca key")
		util.OpenSSLNewCA(caKey, caCrt, caSubj)

		compat_otp.By("2.2: Create a user CSR and the user key")
		util.OpenSSLNewCSR(usrKey, usrCsr, userSubj)

		compat_otp.By("2.3: Sign the user CSR and generate the certificate")
		san := "subjectAltName = DNS:*." + ingctrl.Domain
		util.OpenSSLSignCSR(san, usrCsr, caCrt, caKey, usrCrt)

		compat_otp.By("3.0: create a cm with date ca certification, then create the custom ingresscontroller")
		g.DeferCleanup(util.DeleteConfigMap, oc, "openshift-config", cmName)
		util.CreateConfigMapFromFile(oc, "openshift-config", cmName, "ca-bundle.pem="+caCrt)

		g.DeferCleanup(func() {
			o.Expect(ingctrl.Delete(oc)).To(o.Succeed(), "failed to delete DCM ingresscontroller %s", ingctrl.Name)
		})
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		compat_otp.By("4.0: enable http2 on the custom ingresscontroller by the annotation if env ROUTER_DISABLE_HTTP2 is true")
		jsonPath := "{.spec.template.spec.containers[0].env[?(@.name==\"ROUTER_DISABLE_HTTP2\")].value}"
		envValue := util.GetByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.Name, jsonPath)
		if envValue == "true" {
			util.SetAnnotationAsAdmin(oc, ingctrl.Namespace, "ingresscontroller/"+ingctrl.Name, `ingress.operator.openshift.io/default-enable-http2=true`)
			util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")
		}

		util.EnableDynamicConfigManager(oc, ingctrl)

		compat_otp.By("5.0 Create a deployment and a client pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)
		// The client pod runs unprivileged but /data is group-writable; create the
		// target directory before copying the certificates into it.
		err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", ns, clientPodName, "--", "mkdir", "-p", podDirname).Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to prepare %s in the client pod", podDirname)
		// Copy only the client certificate, its key and the CA certificate; the
		// CA signing key must not be placed in the workload pod.
		for _, f := range []string{caCrt, usrCrt, usrKey} {
			err = oc.AsAdmin().WithoutNamespace().Run("cp").Args("-n", ns, f, ns+"/"+clientPodName+":"+podDirname+"/"+filepath.Base(f)).Execute()
			o.Expect(err).NotTo(o.HaveOccurred(), "OCP-77906: 5.0 Create a deployment and a client pod")
		}

		compat_otp.By("6.0 Create a reencrypt route inside the namespace")
		util.CreateRoute(oc, ns, "reencrypt", "route-reen", svcName, []string{"--hostname=" + routehost, "--ca-cert=" + caCrt, "--cert=" + usrCrt, "--key=" + usrKey})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen", ingctrl.Name)

		compat_otp.By("7.0 Check the haproxy.config, make sure alpn is enabled for the reencrypt route's backend endpoint")
		backend := "be_secure:" + ns + ":route-reen"
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, backend, []string{"ssl alpn h2,http/1.1"})

		compat_otp.By("8.0 Curl the reencrypt route with specified protocol http2")
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 443, podIP)
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--http2", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "HTTP/2 200", 60, 1)

		compat_otp.By("9.0 Curl the reencrypt route with specified protocol http1.1")
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-sI", "--cacert", podCaCrt, "--cert", podUsrCrt, "--key", podUsrKey, "--http1.1", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "HTTP/1.1 200", 60, 1)
	})

	// author: shudili@redhat.com
	g.It("Dynamic Configuration Manager for edge route [Serial]", g.Label("OCP-77973"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp77973",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "edge77973" + "." + ingctrl.Domain
		g.DeferCleanup(func() {
			o.Expect(ingctrl.Delete(oc)).To(o.Succeed(), "failed to delete DCM ingresscontroller %s", ingctrl.Name)
		})
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		compat_otp.By("1.1 Patch ingresscontroller with unsupportedConfigOverrides option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"unsupportedConfigOverrides\":{\"dynamicConfigManager\":\"true\", \"maxDynamicServers\":\"1\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("2.0 Create a deployment, an edge route and a client pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		util.CreateRoute(oc, ns, "edge", "edge77973", svcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "edge77973", ingctrl.Name)
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Curl the edge route")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.Logf("init routerpod is: %s", routerpod)
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 443, podIP)
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4.0 Check the route's backend configuration including server pod, dynamic pool and dynamic cookie")
		backend := "be_edge_http:" + ns + ":edge77973"
		util.CheckDcmBackendConfig(oc, routerpod, backend)

		compat_otp.By("5.0 Use debug command to check the dynamic server's state")
		// used the socat command under the router pod to get all the route's endpoints status
		socatCmd := util.DCMServerStateCommand(backend)
		util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, 1)
		currentSrvStates := ""

		compat_otp.By("6.0 get the initial router reloaded log")
		util.ScaleDeploy(oc, ns, srvdmInfo, 2)
		// Both baselines describe two converged replicas. The loop classifies
		// changes starting with the next scale, from two replicas to three.
		initSrvStates := util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, 2)
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-77973: 6.0 get the initial router reloaded log")
		initReloadedNum := strings.Count(log, `"msg"="router reloaded"`)

		compat_otp.By("7.0 keep scaling up the deployment with replicas 1")
		for i := 2; i < desiredReplicas; i++ {
			compat_otp.By("7." + strconv.Itoa(i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i+1))
			util.ScaleDeploy(oc, ns, srvdmInfo, i+1)
			util.WaitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(i+1))

			compat_otp.By("7." + strconv.Itoa(i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("7." + strconv.Itoa(i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i+1)

			compat_otp.By("7." + strconv.Itoa(i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("8.0 keep scaling down the deployment with replicas 1")
		for i := desiredReplicas - 1; i >= 1; i-- {
			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("9.0 keep scaling up the deployment with replicas 2")
		maxReplicas := 0
		for i := 2; i <= desiredReplicas-2; i = i + 2 {
			compat_otp.By("9." + strconv.Itoa((i+2)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
			maxReplicas = i
		}

		compat_otp.By("10.0 keep scaling down the deployment with replicas 2")
		for i := maxReplicas - 2; i >= 0; i = i - 2 {
			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".4: get the router reloaded log")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}
	})

	// author: shudili@redhat.com
	g.It("Dynamic Configuration Manager for passthrough route [Serial]", g.Label("OCP-77974"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp77974",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "passth77974" + "." + ingctrl.Domain
		g.DeferCleanup(func() {
			o.Expect(ingctrl.Delete(oc)).To(o.Succeed(), "failed to delete DCM ingresscontroller %s", ingctrl.Name)
		})
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		compat_otp.By("1.1 Patch ingresscontroller with unsupportedConfigOverrides option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"unsupportedConfigOverrides\":{\"dynamicConfigManager\":\"true\", \"maxDynamicServers\":\"1\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("2.0 Create a deployment, a passthrough route and a client pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		util.CreateRoute(oc, ns, "passthrough", "passth77974", svcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "passth77974", ingctrl.Name)
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Curl the passthrough route")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.Logf("init routerpod is: %s", routerpod)
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 443, podIP)
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4.0 Check the route's backend configuration including server pod and dynamic pool")
		backend := "be_tcp:" + ns + ":passth77974"
		util.CheckDcmBackendConfig(oc, routerpod, backend)

		compat_otp.By("5.0 Use debug command to check the dynamic server's state")
		// used the socat command under the router pod to get all the route's endpoints status
		socatCmd := util.DCMServerStateCommand(backend)
		util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, 1)
		currentSrvStates := ""

		compat_otp.By("6.0 get the initial router reloaded log")
		util.ScaleDeploy(oc, ns, srvdmInfo, 2)
		// Both baselines describe two converged replicas. The loop classifies
		// changes starting with the next scale, from two replicas to three.
		initSrvStates := util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, 2)
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-77974: 6.0 get the initial router reloaded log")
		initReloadedNum := strings.Count(log, `"msg"="router reloaded"`)

		compat_otp.By("7.0 keep scaling up the deployment with replicas 1")
		for i := 2; i < desiredReplicas; i++ {
			compat_otp.By("7." + strconv.Itoa(i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i+1))
			util.ScaleDeploy(oc, ns, srvdmInfo, i+1)
			util.WaitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(i+1))

			compat_otp.By("7." + strconv.Itoa(i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("7." + strconv.Itoa(i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i+1)

			compat_otp.By("7." + strconv.Itoa(i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("8.0 keep scaling down the deployment with replicas 1")
		for i := desiredReplicas - 1; i >= 1; i-- {
			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("9.0 keep scaling up the deployment with replicas 2")
		maxReplicas := 0
		for i := 2; i <= desiredReplicas-2; i = i + 2 {
			compat_otp.By("9." + strconv.Itoa((i+2)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
			maxReplicas = i
		}

		compat_otp.By("10.0 keep scaling down the deployment with replicas 2")
		for i := maxReplicas - 2; i >= 0; i = i - 2 {
			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".4: get the router reloaded log")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}
	})

	// author: shudili@redhat.com
	g.It("Dynamic Configuration Manager for reencrypt route [Serial]", g.Label("OCP-77975"), func() {

		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			svcName             = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp77975",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "reen77975" + "." + ingctrl.Domain
		g.DeferCleanup(func() {
			o.Expect(ingctrl.Delete(oc)).To(o.Succeed(), "failed to delete DCM ingresscontroller %s", ingctrl.Name)
		})
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		compat_otp.By("1.1 Patch ingresscontroller with unsupportedConfigOverrides option")
		ingctrlResource := "ingresscontrollers/" + ingctrl.Name
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, ingctrlResource, "{\"spec\":{\"unsupportedConfigOverrides\":{\"dynamicConfigManager\":\"true\", \"maxDynamicServers\":\"1\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("2.0 Create a deployment, a reencrypt route and a client pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		util.CreateRoute(oc, ns, "reencrypt", "reen77975", svcName, []string{"--hostname=" + routehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "reen77975", ingctrl.Name)
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Curl the reencrypt route")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		util.Logf("init routerpod is: %s", routerpod)
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 443, podIP)
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift", 60, 1)

		compat_otp.By("4.0 Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
		backend := "be_secure:" + ns + ":reen77975"
		util.CheckDcmBackendConfig(oc, routerpod, backend)

		compat_otp.By("5.0 Use debug command to check the dynamic server's state")
		// used the socat command under the router pod to get all the route's endpoints status
		socatCmd := util.DCMServerStateCommand(backend)
		util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, 1)
		currentSrvStates := ""

		compat_otp.By("6.0 get the initial router reloaded log")
		util.ScaleDeploy(oc, ns, srvdmInfo, 2)
		// Both baselines describe two converged replicas. The loop classifies
		// changes starting with the next scale, from two replicas to three.
		initSrvStates := util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, 2)
		log, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress", routerpod).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-77975: 6.0 get the initial router reloaded log")
		initReloadedNum := strings.Count(log, `"msg"="router reloaded"`)

		compat_otp.By("7.0 keep scaling up the deployment with replicas 1")
		for i := 2; i < desiredReplicas; i++ {
			compat_otp.By("7." + strconv.Itoa(i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i+1))
			util.ScaleDeploy(oc, ns, srvdmInfo, i+1)
			util.WaitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(i+1))

			compat_otp.By("7." + strconv.Itoa(i) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("7." + strconv.Itoa(i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i+1)

			compat_otp.By("7." + strconv.Itoa(i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("8.0 keep scaling down the deployment with replicas 1")
		for i := desiredReplicas - 1; i >= 1; i-- {
			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".2: Check the route's backend configuration including server pod, dynamic pool and dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("8." + strconv.Itoa(desiredReplicas-i) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}

		compat_otp.By("9.0 keep scaling up the deployment with replicas 2")
		maxReplicas := 0
		for i := 2; i <= desiredReplicas-2; i = i + 2 {
			compat_otp.By("9." + strconv.Itoa((i+2)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("9." + strconv.Itoa(i/2) + ".4: check whether got the router reloaded log or not")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
			maxReplicas = i
		}

		compat_otp.By("10.0 keep scaling down the deployment with replicas 2")
		for i := maxReplicas - 2; i >= 0; i = i - 2 {
			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".1: scale up the deployment with replicas " + strconv.Itoa(i))
			util.ScaleDeploy(oc, ns, srvdmInfo, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".2: Check the route's backend configuration including server pod, dynamic pool, dynamic cookie")
			util.CheckDcmBackendConfig(oc, routerpod, backend)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".3: Use debug command to check the dynamic server's state")
			currentSrvStates = util.CheckDcmUpEndpoints(oc, routerpod, socatCmd, i)

			compat_otp.By("10." + strconv.Itoa((maxReplicas-i)/2) + ".4: get the router reloaded log")
			initReloadedNum = util.CheckRouterReloadedLogs(oc, routerpod, initReloadedNum, initSrvStates, currentSrvStates)
			initSrvStates = currentSrvStates
		}
	})

	// author: shudili@redhat.com
	g.It("Traffic test for dynamic servers [Serial]", g.Label("OCP-78239"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			srvdmInfo           = "web-server-deploy"
			unsecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			desiredReplicas     = 8
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp78239",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		httpRoutehost := "unsecure78239" + "." + ingctrl.Domain
		reenRoutehost := "reen78239" + "." + ingctrl.Domain
		g.DeferCleanup(func() {
			o.Expect(ingctrl.Delete(oc)).To(o.Succeed(), "failed to delete DCM ingresscontroller %s", ingctrl.Name)
		})
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		util.EnableDynamicConfigManager(oc, ingctrl)

		compat_otp.By("2.0 Create a deployment and a client pod")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name="+srvdmInfo)
		util.CreateResourceFromFile(oc, ns, clientPod)
		util.EnsurePodWithLabelReady(oc, ns, clientPodLabel)

		compat_otp.By("3.0 Create the HTTP route and the reencrypt route")
		util.CreateRoute(oc, ns, "http", "unsecure78239", unsecSvcName, []string{"--hostname=" + httpRoutehost})
		util.CreateRoute(oc, ns, "reencrypt", "reen78239", secSvcName, []string{"--hostname=" + reenRoutehost})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "unsecure78239", ingctrl.Name)
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "reen78239", ingctrl.Name)

		compat_otp.By("4.0 set lb roundrobin to the routes")
		util.SetAnnotation(oc, ns, "route/unsecure78239", "haproxy.router.openshift.io/balance=roundrobin")
		util.SetAnnotation(oc, ns, "route/reen78239", "haproxy.router.openshift.io/balance=roundrobin")

		compat_otp.By("5.0 Scale up the deployment with the desired replicas strconv.Itoa(desiredReplicas)")
		podList := util.ScaleDeploy(oc, ns, srvdmInfo, desiredReplicas)
		util.WaitForOutputEquals(oc, ns, "deployment/"+srvdmInfo, "{.status.availableReplicas}", strconv.Itoa(desiredReplicas))

		compat_otp.By("6.0 Keep curing the http route, make sure all backend endpoints are hit")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(httpRoutehost, 80, podIP)
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "http://" + httpRoutehost, "-s", "--resolve", toDst, "--connect-timeout", "10"}
		util.CheckDcmServersAccessible(oc, curlCmd, podList, 180, desiredReplicas)

		compat_otp.By("7.0 Keep curing the reencrypt route, make sure all backend endpoints are hit")
		toDst = util.CurlResolve(reenRoutehost, 443, podIP)
		curlCmd = []string{"-n", ns, clientPodName, "--", "curl", "https://" + reenRoutehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		util.CheckDcmServersAccessible(oc, curlCmd, podList, 180, desiredReplicas)
	})

	// OCPBUGS-75009(https://redhat.atlassian.net/browse/OCPBUGS-75009)
	//Author:shudili@redhat.com
	g.It("Map entries added in the end when DCM is enabled [Serial]", g.Label("OCP-88370"), func() {
		// Updating namespaceOwnership as 'InterNamespaceAllowed' in file ingresscontroller-np.yaml
		buildPruningBaseDir := TestdataDir()
		baseTemp := util.CopyFixture(filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml"))
		util.UpdateFilebySedCmd(baseTemp, "Strict", "InterNamespaceAllowed")

		var (
			testPodSvc     = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvdmInfo      = "web-server-deploy"
			unSecSvc       = "service-unsecure"
			clientPod      = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName  = "hello-pod"
			clientPodLabel = "app=hello-pod"
			path1          = "/path"
			path2          = "/path/second"
			ingctrl        = util.IngressControllerDescription{
				Name:      "ocp88370",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  baseTemp,
			}
		)

		compat_otp.By("1.0: Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		routehost := "unsecure88370" + "." + ingctrl.Domain
		g.DeferCleanup(func() {
			o.Expect(ingctrl.Delete(oc)).To(o.Succeed(), "failed to delete DCM ingresscontroller %s", ingctrl.Name)
		})
		ingctrl.Create(oc)
		util.EnsureCustomIngressControllerAvailable(oc, ingctrl.Name)

		util.EnableDynamicConfigManager(oc, ingctrl)

		compat_otp.By("2.0: Create a deployment and a client pod in one namespace")
		ns1 := oc.Namespace()
		user1 := oc.Username()
		srvPodList1 := util.CreateResourceFromWebServer(oc, ns1, testPodSvc, srvdmInfo)
		util.CreateResourceFromFile(oc, ns1, clientPod)
		util.EnsurePodWithLabelReady(oc, ns1, clientPodLabel)

		compat_otp.By("3.0: Create a deployment and a client pod in the other namespace")
		oc.SetupProject()
		ns2 := oc.Namespace()
		user2 := oc.Username()
		srvPodList2 := util.CreateResourceFromWebServer(oc, ns2, testPodSvc, srvdmInfo)

		compat_otp.By("4.0: Create a http route with path " + path1 + " in the first namespace")
		oc.ChangeUser(user1)
		util.CreateRoute(oc, ns1, "http", "route-http88370a", unSecSvc, []string{"--hostname=" + routehost, "--path=" + path1})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns1, "route-http88370a", ingctrl.Name)

		compat_otp.By("5.0: Create a http route with the same hostnamepath, but with different path " + path2 + " in the second namespace")
		oc.ChangeUser(user2)
		util.CreateRoute(oc, ns2, "http", "route-http88370b", unSecSvc, []string{"--hostname=" + routehost, "--path=" + path2})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns2, "route-http88370b", ingctrl.Name)

		compat_otp.By("6.0: Curl the first  http route")
		// The ingresscontroller was patched for DCM in step 1.0, so a rollout may
		// still be in flight; an arbitrary pod can be a terminating one from the
		// previous ReplicaSet or a new one whose router is not yet serving.  Wait
		// for the newest ReplicaSet's pod to be Ready before reading its IP.
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		podIP := util.GetPodAddress(oc, routerpod, "openshift-ingress")
		toDst := util.CurlResolve(routehost, 80, podIP)
		curlCmd1 := []string{"-n", ns1, clientPodName, "--", "curl", "http://" + routehost + path1 + "/index.html", "-s", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd1, `ocp-test `+srvPodList1[0], 180, 1)

		compat_otp.By("6.1: Curl the second  http route")
		curlCmd2 := []string{"-n", ns1, clientPodName, "--", "curl", "http://" + routehost + path2 + "/index.html", "-s", "--resolve", toDst, "--connect-timeout", "10"}
		util.RepeatCmdOnClient(oc, curlCmd2, `second-test `+srvPodList2[0], 60, 1)

		compat_otp.By("7.0: Delete the first route")
		oc.ChangeUser(user1)
		_, err := oc.WithoutNamespace().Run("delete").Args("-n", ns1, "route", "route-http88370a").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-88370: 7.0: Delete the first route")

		compat_otp.By("8.0: Create the http route with path " + path1 + " in the first namespace again")
		util.CreateRoute(oc, ns1, "http", "route-http88370a", unSecSvc, []string{"--hostname=" + routehost, "--path=" + path1})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns1, "route-http88370a", ingctrl.Name)

		// Make sure the map entries are recreated correctly, curl the two routes with same host but with different paths should get the desired file content as same as steps 6.0 and 6.1
		compat_otp.By("9.0: Curl the two http routes again")
		util.RepeatCmdOnClient(oc, curlCmd1, `ocp-test `+srvPodList1[0], 60, 1)
		util.RepeatCmdOnClient(oc, curlCmd2, `second-test `+srvPodList2[0], 60, 1)
	})
})
