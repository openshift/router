package router

import (
	"github.com/openshift/router-tests-extension/test/e2e/testdata"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("ingress-operator", exutil.KubeConfigPath())

	// author: hongli@redhat.com
	// Includes OCP-27560: support NodePortService for custom Ingresscontroller
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Critical-21873-The replicas of router deployment is controlled by ingresscontroller", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp21873",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)

		exutil.By("Ensure NodePort service is created")
		serviceType := getByJsonPath(oc, "openshift-ingress", "svc/router-nodeport-"+ingctrl.name, "{.spec.type}")
		o.Expect(serviceType).To(o.ContainSubstring(`NodePort`))

		exutil.By("Scale the replicas to 0 and ensure router pod is deleted")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{"spec":{"replicas":0}}`)
		// no status.readyReplicas filed in deployment if set replicas=0, so just wait for pod to disappear
		err := waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("The router pod %v does not disapper", routerpod))

		exutil.By("Scale the replicas to 1 and ensure deployment is updated")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{"spec":{"replicas":1}}`)
		waitForOutputEquals(oc, "openshift-ingress", "deployment/router-"+ingctrl.name, "{.status.readyReplicas}", "1")
	})

	// author: hongli@redhat.com
	// Includes OCP-23169: The toleration of router deployment is controlled by ingresscontroller
	// No control-plane/master node on HCP
	g.It("Author:hongli-NonHyperShiftHOST-ROSA-OSD_CCS-ARO-Medium-22633-The nodeSelector and tolerations of router deployment are controlled by ingresscontrolle", func() {
		// skip if ingress.config .status.defaultPlacement == ControlPlane
		defaultPlacement := getByJsonPath(oc, "default", "ingress.config/cluster", "{.status.defaultPlacement}")
		if defaultPlacement == "ControlPlane" {
			g.Skip("Skip since nodeSelector is set to ControlPlane by default on this cluster")
		}

		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp22633",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("Check the default nodeSelector and tolerations")
		nodeSelector := getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.name, "{.spec.template.spec.nodeSelector}")
		o.Expect(nodeSelector).To(o.ContainSubstring(`node-role.kubernetes.io/worker`))
		// note: tolerations might be empty in old release (<4.18)
		tolerations := getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.name, "{.spec.template.spec.tolerations}")
		o.Expect(tolerations).NotTo(o.ContainSubstring(`NoSchedule`))

		exutil.By("Update the ingresscontroller nodeSelector and tolerations to deploy router pod to control-plane node")
		jsonpath := `{"spec": {"nodePlacement": {"nodeSelector": {"matchLabels": {"node-role.kubernetes.io/control-plane": ""}}, "tolerations": [{"effect": "NoSchedule", "operator": "Exists"}]}}}`
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonpath)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		exutil.By("Ensure the nodeSelector and tolerations of router deployment is updated")
		nodeSelector = getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.name, "{.spec.template.spec.nodeSelector}")
		o.Expect(nodeSelector).To(o.ContainSubstring(`node-role.kubernetes.io/control-plane`))
		tolerations = getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.name, "{.spec.template.spec.tolerations}")
		o.Expect(tolerations).To(o.ContainSubstring(`"effect":"NoSchedule","operator":"Exists"`))
	})

	// Test case creater: hongli@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-22636-The namespaceSelector of router is controlled by ingresscontroller", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvName             = "service-unsecure"
			ingctrl             = ingressControllerDescription{
				name:      "ocp22636",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		exutil.By("1. Create one custom ingresscontroller")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		routehost := srvName + ".ocp22636." + baseDomain
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2. Create a server pod and expose an unsecure service")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		err := oc.Run("expose").Args("service", srvName, "--hostname="+routehost, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, ns, "route/"+srvName, "{.spec.host}", routehost)

		exutil.By("3. Label the namespace to 'namespace=router-test'")
		err = oc.AsAdmin().WithoutNamespace().Run("label").Args("namespace", ns, "namespace=router-test").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("4. Patch the custom ingresscontroller with the namespaceSelector")
		patchNamespaceSelector := "{\"spec\":{\"namespaceSelector\":{\"matchLabels\":{\"namespace\": \"router-test\"}}}}"
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchNamespaceSelector)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newCustContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		exutil.By("5. Check the haproxy config on the custom router pod to find the backend details of the " + ns + " route")
		checkoutput := readRouterPodData(oc, newCustContPod, "cat haproxy.config", "service-unsecure")
		o.Expect(checkoutput).To(o.ContainSubstring("backend be_http:" + ns + ":service-unsecure"))

		exutil.By("6. Check the haproxy config on the custom router to confirm no backend details of other routes are present")
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newCustContPod, "--", "bash", "-c", "cat haproxy.config | grep canary").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("command terminated with exit code 1"))
	})

	// Test case creater: hongli@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-22637-The routeSelector of router is controlled by ingresscontroller", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvName             = "service-unsecure"
			ingctrl             = ingressControllerDescription{
				name:      "ocp22637",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		exutil.By("1. Create one custom ingresscontroller")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		routehost := srvName + ".ocp22637." + baseDomain
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2. Create a server pod and expose an unsecure service")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		err := oc.Run("expose").Args("service", srvName, "--hostname="+routehost, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, ns, "route/"+srvName, "{.spec.host}", routehost)

		exutil.By("3. Label the route to 'route=router-test'")
		err = oc.AsAdmin().WithoutNamespace().Run("label").Args("route", "service-unsecure", "route=router-test", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("4. Patch the custom ingresscontroller with the namespaceSelector")
		patchNamespaceSelector := "{\"spec\":{\"routeSelector\":{\"matchLabels\":{\"route\": \"router-test\"}}}}"
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, patchNamespaceSelector)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newCustContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		exutil.By("5. Check the haproxy config on the custom router pod to find the backend details of the route for service-unsecure")
		checkoutput := readRouterPodData(oc, newCustContPod, "cat haproxy.config", "service-unsecure")
		o.Expect(checkoutput).To(o.ContainSubstring("backend be_http:" + ns + ":service-unsecure"))

		exutil.By("6. Check the haproxy config on the custom router to confirm no backend details of other routes are present")
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", newCustContPod, "--", "bash", "-c", "cat haproxy.config | grep canary").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("command terminated with exit code 1"))
	})

	// author: shudili@redhat.com
	// Includes OCP-26150: Integrate ingress operator metrics with Prometheus
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Medium-26150-misc tests for ingress operator", func() {
		var (
			namespace          = "openshift-ingress-operator"
			servicemonitorName = "ingress-operator"
			rolebindingName    = "prometheus-k8s"
		)

		exutil.By(fmt.Sprintf("Check whether servicemonitor %s exists or not", servicemonitorName))
		jsonPath := "{.items[*].metadata.name}"
		servicemonitorList := getByJsonPath(oc, namespace, "servicemonitor", jsonPath)
		o.Expect(servicemonitorList).To(o.ContainSubstring(servicemonitorName))

		exutil.By(fmt.Sprintf("Check whether rolebinding %s exists or not", rolebindingName))
		rolebindingList := getByJsonPath(oc, namespace, "rolebinding", jsonPath)
		o.Expect(rolebindingList).To(o.ContainSubstring(rolebindingName))

		exutil.By(fmt.Sprintf("check the openshift.io/cluster-monitoring label of the namespace %s, which should be true", namespace))
		jsonPath = `{.metadata.labels.openshift\.io/cluster-monitoring}`
		value := getByJsonPath(oc, "default", "namespace/"+namespace, jsonPath)
		o.Expect(value).To(o.ContainSubstring("true"))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-29207-the PROXY protocol is disabled in non-AWS platform", func() {
		platforms := map[string]bool{
			"aws": true,
		}
		if platforms[exutil.CheckPlatform(oc)] {
			g.Skip("Skip for AWS platform")
		}

		exutil.By("1.0 Get default ingress pod")
		routerpod := getOneRouterPodNameByIC(oc, "default")

		exutil.By("2.0 Check HaProxy for PROXY protocol")
		cmd := fmt.Sprintf(`cat haproxy.config | grep -E 'bind :{1,3}(80|443)'`)
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).NotTo(o.ContainSubstring("accept-proxy"))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-30059-NetworkEdge Create an ingresscontroller that logs to a sidecar container", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-sidecar.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			ingctrl             = ingressControllerDescription{
				name:      "ocp30059",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("1. Create a sidecar-log ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2. Check if the a sidecar container with name 'logs' is deployed")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		containerName := getByJsonPath(oc, "openshift-ingress", "pod/"+routerpod, "{.spec.containers[1].name}")
		o.Expect(containerName).To(o.ContainSubstring("logs"))

		// check the function of logs to a sidecar container in the following steps by curl a route and check the router logs
		exutil.By("3. Create a client pod, a server pod and an unsecure service")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		exutil.By("4. Create an route")
		routehost := srvName + "-" + ns + "." + ingctrl.domain
		err := oc.Run("expose").Args("service", srvName, "--hostname="+routehost, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, ns, "route", "{.items[0].metadata.name}", srvName)

		exutil.By("5. Curl the route, and then check the router pod's logs which should contain the new http request")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		jsonPath := fmt.Sprintf(`{.items[?(@.metadata.generateName=="%s-")].metadata.name}`, srvName)
		epSliceName := getByJsonPath(oc, ns, "EndpointSlice", jsonPath)
		jsonPath = "{.endpoints[0].addresses[0]}:{.ports[0].port}"
		ep := getByJsonPath(oc, ns, "EndpointSlice/"+epSliceName, jsonPath)
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-I", "http://" + routehost, "--resolve", toDst, "--connect-timeout", "10"}
		result, _ := repeatCmdOnClient(oc, cmdOnPod, "200", 30, 1)
		o.Expect(result).To(o.ContainSubstring("200"))
		output := waitRouterLogsAppear(oc, routerpod, ep)
		log := regexp.MustCompile("haproxy.+" + ep + ".+HTTP/1.1").FindStringSubmatch(output)[0]
		o.Expect(len(log) > 1).To(o.BeTrue())
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-30060-NetworkEdge Create an ingresscontroller that logs to external rsyslog instance", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			syslogPod           = filepath.Join(buildPruningBaseDir, "rsyslogd-pod.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			syslogPodName       = "rsyslogd-pod"
			syslogPodLabel      = "name=rsyslogd"
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
		)

		exutil.By("1. Create a syslog pod for the log receiver")
		ns := oc.Namespace()
		exutil.SetNamespacePrivileged(oc, ns)
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", syslogPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, syslogPodLabel)

		exutil.By("2. Create a syslog ingresscontroller")
		syslogPodIP := getPodv4Address(oc, syslogPodName, ns)
		extraParas := "    logging:\n      access:\n        destination:\n          type: Syslog\n          syslog:\n            address: " + syslogPodIP + "\n            port: 514\n"
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)
		ingctrl := ingressControllerDescription{
			name:      "ocp30060",
			namespace: "openshift-ingress-operator",
			domain:    "",
			template:  customTemp,
		}
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("3. Check ROUTER_SYSLOG_ADDRESS env in a router pod")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		syslogEnv := readRouterPodEnv(oc, routerpod, "ROUTER_SYSLOG_ADDRESS")
		syslogEp := syslogPodIP + ":514"
		if strings.Contains(syslogPodIP, ":") {
			syslogEp = "[" + syslogPodIP + "]" + ":514"
		}
		o.Expect(syslogEnv).To(o.ContainSubstring(syslogEp))

		exutil.By("4. Create a client pod, a server pod and an unsecure service")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		exutil.By("5. Create a route")
		routehost := srvName + "-" + ns + "." + ingctrl.domain
		createRoute(oc, ns, "http", "route-http", srvName, []string{"--hostname=" + routehost})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-http", ingctrl.name)

		exutil.By("6. Curl the route, and then check the rsyslogd pod's logs which should contain the new http request")
		podIP := getPodv4Address(oc, routerpod, "openshift-ingress")
		toDst := routehost + ":80:" + podIP
		jsonPath := fmt.Sprintf(`{.items[?(@.metadata.generateName=="%s-")].metadata.name}`, srvName)
		epSliceName := getByJsonPath(oc, ns, "EndpointSlice", jsonPath)
		jsonPath = "{.endpoints[0].addresses[0]}:{.ports[0].port}"
		ep := getByJsonPath(oc, ns, "EndpointSlice/"+epSliceName, jsonPath)
		cmdOnPod := []string{"-n", ns, clientPodName, "--", "curl", "-I", "http://" + routehost, "--resolve", toDst, "--connect-timeout", "10"}
		result, _ := repeatCmdOnClient(oc, cmdOnPod, "200", 30, 5)
		o.Expect(result).To(o.ContainSubstring("200"))
		output, _ := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", ns, syslogPodName).Output()
		o.Expect(output).To(o.MatchRegexp("haproxy.+" + ep + ".+HTTP/1.1"))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Medium-30264-ROUTER_SYSLOG_ADDRESS changes according to the logging configuration", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-sidecar.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp30264",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("1. Create a sidecar-log ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2. check router deployment")
		err := oc.AsAdmin().WithoutNamespace().Run("get").Args("deployment", "-n", "openshift-ingress", "router-"+ingctrl.name).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("3. Verify presence of pod with label")
		ingPod := getOnePodNameByLabel(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+ingctrl.name)
		o.Expect(ingPod).To(o.ContainSubstring(ingctrl.name))
		err = waitForPodWithLabelReady(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+ingctrl.name)
		o.Expect(err).NotTo(o.HaveOccurred())

		// OCP-30066 Enable log-send-hostname in HAproxy configuration by default
		exutil.By("4. check haproxy for output")
		ensureHaproxyBlockConfigContains(oc, ingPod, "global", []string{"log-send-hostname", `/var/lib/haproxy/`})

		exutil.By("5. check custom ingress pod for ROUTER_SYSLOG_ADDRESS")
		grepCmd := "ROUTER_SYSLOG_ADDRESS"
		cmd := fmt.Sprintf(`env | grep "%s"`, grepCmd)
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", ingPod, "--", "bash", "-lc", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("ROUTER_SYSLOG_ADDRESS=/var/lib/rsyslog/rsyslog.sock"))

		exutil.By("6. Patch the ingress controller with a different address and port")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp30264", `{"spec":{"logging":{"access":{"destination":{"type": "Syslog", "syslog":{"address": "1.2.3.4", "port": 514}}}}}}`)

		exutil.By("7. Wait for previous pod to be deleted")
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+ingPod)
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("8. Retrieve new pod name with label")
		ingPod = getOnePodNameByLabel(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+ingctrl.name)
		o.Expect(ingPod).To(o.ContainSubstring(ingctrl.name))

		exutil.By("9. check custom ingress pod for ROUTER_SYSLOG_ADDRESS")
		output, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", ingPod, "--", "bash", "-lc", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("ROUTER_SYSLOG_ADDRESS=1.2.3.4:514"))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-33763-ingresscontroller supports AWS NLB", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp33763",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Check the Platform type")
		exutil.SkipIfPlatformTypeNot(oc, "AWS")

		exutil.By("1. Create a nlb ingress controller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		// Updating LB scope `External` to `Internal` and `Classic`` to `NLB` in the yaml file
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|External|Internal|g; s|Classic|NLB|g' %s`, customTemp)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2. Verify presence of pod with label")
		ensurePodWithLabelReady(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+ingctrl.name)

		exutil.By("3. check if LB service exists")
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", "openshift-ingress", "router-"+ingctrl.name).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(`router-`+ingctrl.name), o.ContainSubstring(`LoadBalancer`)))

		exutil.By("4. Check LB service annotations")
		annotation := getAnnotation(oc, "openshift-ingress", "service", `router-`+ingctrl.name)
		o.Expect(annotation).To(o.And(o.ContainSubstring(`service.beta.kubernetes.io/aws-load-balancer-type":"nlb`), o.ContainSubstring(`service.beta.kubernetes.io/aws-load-balancer-internal":"true`)))
	})

	// Due to bug https://issues.redhat.com/browse/OCPBUGS-43431, this case may not run on HCP cluster.
	g.It("Author:mjoseph-NonHyperShiftHOST-High-38674-hard-stop-after annotation can be applied globally on all ingresscontroller [Disruptive]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp38674",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		defaultRouterpod := getOneRouterPodNameByIC(oc, "default")

		exutil.By("Annotate the ingresses.config/cluster with ingress.operator.openshift.io/hard-stop-after globally")
		defer oc.AsAdmin().WithoutNamespace().Run("annotate").Args(
			"-n", ingctrl.namespace, "ingresses.config/cluster", "ingress.operator.openshift.io/hard-stop-after-").Execute()
		err0 := oc.AsAdmin().WithoutNamespace().Run("annotate").Args(
			"-n", ingctrl.namespace, "ingresses.config/cluster", "ingress.operator.openshift.io/hard-stop-after=30m", "--overwrite").Execute()
		o.Expect(err0).NotTo(o.HaveOccurred())
		err := waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+defaultRouterpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+defaultRouterpod))
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		exutil.By("Verify the annotation presence in the cluster gloabl config")
		newRouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		newDefaultRouterpod := getOneNewRouterPodFromRollingUpdate(oc, "default")
		findAnnotation := getAnnotation(oc, oc.Namespace(), "ingress.config.openshift.io", "cluster")
		o.Expect(findAnnotation).To(o.ContainSubstring(`"ingress.operator.openshift.io/hard-stop-after":"30m"`))

		exutil.By("Check the env variable of the custom router pod to verify the hard stop duration is 30m")
		env := readRouterPodEnv(oc, newRouterpod, "ROUTER_HARD_STOP_AFTER")
		o.Expect(env).To(o.ContainSubstring(`30m`))

		exutil.By("Check the env variable of the default router pod to verify the hard stop duration is 30m")
		env1 := readRouterPodEnv(oc, newDefaultRouterpod, "ROUTER_HARD_STOP_AFTER")
		o.Expect(env1).To(o.ContainSubstring(`30m`))

		exutil.By("Annotate the ingresses.config/cluster with ingress.operator.openshift.io/hard-stop-after per ingresscontroller basis")
		err2 := oc.AsAdmin().WithoutNamespace().Run("annotate").Args(
			"-n", ingctrl.namespace, "ingresscontrollers/"+ingctrl.name, "ingress.operator.openshift.io/hard-stop-after=45m", "--overwrite").Execute()
		o.Expect(err2).NotTo(o.HaveOccurred())
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		exutil.By("Verify the annotation presence in the ocp38674 controller config")
		newRouterpod1 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		findAnnotation2 := getAnnotation(oc, ingctrl.namespace, "ingresscontroller.operator.openshift.io", ingctrl.name)
		o.Expect(findAnnotation2).To(o.ContainSubstring(`"ingress.operator.openshift.io/hard-stop-after":"45m"`))

		exutil.By("Check the haproxy config on the defualt router pod to verify the hard stop value is still 30m")
		checkoutput := readRouterPodData(oc, newDefaultRouterpod, "cat haproxy.config", "hard")
		o.Expect(checkoutput).To(o.ContainSubstring(`hard-stop-after 30m`))

		exutil.By("Check the haproxy config on the router pod to verify the hard stop value is changed to 45m")
		checkoutput1 := readRouterPodData(oc, newRouterpod1, "cat haproxy.config", "hard")
		o.Expect(checkoutput1).To(o.ContainSubstring(`hard-stop-after 45m`))
	})

	g.It("Author:mjoseph-High-38675-hard-stop-after annotation can be applied on per ingresscontroller", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl38675one = ingressControllerDescription{
				name:      "ocp38675-1",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}

			ingctrl38675two = ingressControllerDescription{
				name:      "ocp38675-2",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("1. Create two custom ingresscontrollers")
		baseDomain := getBaseDomain(oc)
		ingctrl38675one.domain = ingctrl38675one.name + "." + baseDomain
		ingctrl38675two.domain = ingctrl38675two.name + "." + baseDomain
		defer ingctrl38675one.delete(oc)
		ingctrl38675one.create(oc)
		defer ingctrl38675two.delete(oc)
		ingctrl38675two.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl38675one.name)
		ensureCustomIngressControllerAvailable(oc, ingctrl38675two.name)

		exutil.By("2. Annotate the router of " + ingctrl38675one.name + " with 15m hardstop value")
		setAnnotationAsAdmin(oc, ingctrl38675one.namespace, "ingresscontroller/"+ingctrl38675one.name, `ingress.operator.openshift.io/hard-stop-after=15m`)
		ensureRouterDeployGenerationIs(oc, ingctrl38675one.name, "2")

		exutil.By("3. Annotate the router of " + ingctrl38675two.name + " with 30m hardstop value")
		setAnnotationAsAdmin(oc, ingctrl38675two.namespace, "ingresscontroller/"+ingctrl38675two.name, `ingress.operator.openshift.io/hard-stop-after=30m`)
		ensureRouterDeployGenerationIs(oc, ingctrl38675two.name, "2")

		exutil.By("4. Check the haproxy config on the " + ingctrl38675one.name + " router pod to verify the hard stop value is 15m")
		oneRouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl38675one.name)
		checkoutput := pollReadPodData(oc, "openshift-ingress", oneRouterpod, "cat haproxy.config", "hard")
		o.Expect(checkoutput).To(o.ContainSubstring(`hard-stop-after 15m`))

		exutil.By("5. Check the haproxy config on the " + ingctrl38675two.name + " router pod to verify the hard stop value is 30m")
		twoRouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl38675two.name)
		checkoutput1 := pollReadPodData(oc, "openshift-ingress", twoRouterpod, "cat haproxy.config", "hard")
		o.Expect(checkoutput1).To(o.ContainSubstring(`hard-stop-after 30m`))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonPreRelease-PreChkUpgrade-High-38812-upgrade with router shards", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			ns                  = "ingress-upgrade"
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-shard.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "shards",
				namespace: "openshift-ingress-operator",
				domain:    "",
				shard:     "alpha",
				template:  customTemp,
			}
		)

		exutil.By("1.0: Confirm that the ingress co is in normal status")
		jsonPath := `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		waitForOutputEquals(oc, "default", "co/ingress", jsonPath, "TrueFalseFalse")

		exutil.By("2.0: Create a new project and a web-server application")
		oc.CreateSpecifiedNamespaceAsAdmin(ns)
		baseDomain := getBaseDomain(oc)
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", testPodSvc, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getOnePodNameByLabel(oc, ns, "name=web-server-deploy")
		podIP, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pod", "-n", ns, podName, "-o=jsonpath={.status.podIPs[0].ip}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("3.0: Create an edge route and label the route")
		_, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("route", "edge", "-n", ns, "route-edge", "--service=service-unsecure", "--hostname=ingress-upgrade.example.com").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")

		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("route", "route-edge", "-n", ns, "shard=alpha").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("4.0: Get default ingresscontroller information and create shard ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + baseDomain
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		ensurePodWithLabelReady(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller=shards")

		exutil.By("5.0: Ensure the route in the matched namespace is loaded")
		shardpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, shardpod, podIP, []string{podName + ":service-unsecure"})

		exutil.By("6.0: Confirm that the ingress co is in normal status")
		waitForOutputEquals(oc, "default", "co/ingress", jsonPath, "TrueFalseFalse")
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonPreRelease-PstChkUpgrade-High-38812-upgrade with router shards", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			ns                  = "ingress-upgrade"
		)

		exutil.By("1.0: Confirm that the ingress co is in normal status")
		jsonPath := `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		waitForOutputEquals(oc, "default", "co/ingress", jsonPath, "TrueFalseFalse")

		exutil.By("2.0: Confirm that the custom ingress router pod is still available and get its IP")
		podName := getOnePodNameByLabel(oc, "openshift-ingress", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller=shards")
		podIP := getPodv4Address(oc, podName, "openshift-ingress")

		exutil.By("3.0: Ensure that the route served by shards is still accessible after upgrade")
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", clientPod, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, "app=hello-pod")
		routehost := "ingress-upgrade.example.com"
		toDst := routehost + ":443:" + podIP
		curlCmd := []string{"-n", ns, clientPodName, "--", "curl", "https://" + routehost, "-ks", "--resolve", toDst, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "Hello-OpenShift web-server-deploy", 120, 1)
	})

	// author: hongli@redhat.com
	// Bug: 1960284
	g.It("Author:hongli-Critical-42276-enable annotation traffic-policy.network.alpha.openshift.io/local-with-fallback on LB and nodePort service", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp42276",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("check the annotation of nodeport service")
		annotation := getByJsonPath(oc, "openshift-ingress", "svc/router-nodeport-ocp42276", "{.metadata.annotations}")
		o.Expect(annotation).To(o.ContainSubstring(`traffic-policy.network.alpha.openshift.io/local-with-fallback`))

		// In IBM cloud and PowerVS the externalTrafficPolicy will be 'Cluster' for default LB service, so skipping the same
		platformtype := exutil.CheckPlatform(oc)
		platforms := map[string]bool{
			"ibmcloud": true,
			"powervs":  true,
		}
		if !platforms[platformtype] {
			exutil.By("check the annotation of default LoadBalancer service if it is available")
			output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", "openshift-ingress", "service", "router-default", "-o=jsonpath={.spec.type}").Output()
			// LB service is supported on public cloud platform like aws, gcp, azure and alibaba
			if strings.Contains(output, "LoadBalancer") {
				annotation = getByJsonPath(oc, "openshift-ingress", "svc/router-default", "{.metadata.annotations}")
				o.Expect(annotation).To(o.ContainSubstring(`traffic-policy.network.alpha.openshift.io/local-with-fallback`))
			} else {
				e2e.Logf("skip the default LB service checking part, since it is not supported on this cluster")
			}
		}
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-High-46287-ingresscontroller supports to update maxlength for syslog message", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-syslog.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp46287",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("check the env variable of the router pod to verify the default log length")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		logLength := readRouterPodEnv(oc, newrouterpod, "ROUTER_LOG_MAX_LENGTH")
		o.Expect(logLength).To(o.ContainSubstring(`ROUTER_LOG_MAX_LENGTH=1024`))

		exutil.By("check the haproxy config on the router pod to verify the default log length is enabled")
		checkoutput := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "1024")
		o.Expect(checkoutput).To(o.ContainSubstring(`log 1.2.3.4:514 len 1024 local1 info`))

		exutil.By("patch the existing custom ingress controller with minimum log length value")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp46287", "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"syslog\":{\"maxLength\":480}}}}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		exutil.By("check the env variable of the router pod to verify the minimum log length")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, "ocp46287")
		minimumlogLength := readRouterPodEnv(oc, newrouterpod, "ROUTER_LOG_MAX_LENGTH")
		o.Expect(minimumlogLength).To(o.ContainSubstring(`ROUTER_LOG_MAX_LENGTH=480`))

		exutil.By("patch the existing custom ingress controller with maximum log length value")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp46287", "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"syslog\":{\"maxLength\":4096}}}}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		exutil.By("check the env variable of the router pod to verify the maximum log length")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, "ocp46287")
		maximumlogLength := readRouterPodEnv(oc, newrouterpod, "ROUTER_LOG_MAX_LENGTH")
		o.Expect(maximumlogLength).To(o.ContainSubstring(`ROUTER_LOG_MAX_LENGTH=4096`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Low-46288-ingresscontroller should deny invalid maxlengh value for syslog message", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-syslog.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp46288",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("patch the existing custom ingress controller with log length value less than minimum threshold")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/ocp46288", "-p", "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"syslog\":{\"maxLength\":479}}}}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output1).To(o.ContainSubstring("Invalid value: 479: spec.logging.access.destination.syslog.maxLength in body should be greater than or equal to 480"))

		exutil.By("patch the existing custom ingress controller with log length value more than maximum threshold")
		output2, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/ocp46288", "-p", "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"syslog\":{\"maxLength\":4097}}}}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output2).To(o.ContainSubstring("Invalid value: 4097: spec.logging.access.destination.syslog.maxLength in body should be less than or equal to 4096"))

		exutil.By("check the haproxy config on the router pod to verify the default log length is enabled")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		checkoutput := readRouterPodData(oc, routerpod, "cat haproxy.config", "1024")
		o.Expect(checkoutput).To(o.ContainSubstring(`log 1.2.3.4:514 len 1024 local1 info`))
	})

	g.It("[Level0] Author:mjoseph-Critical-51255-cluster-ingress-operator can set AWS ELB idle Timeout on per controller basis", func() {
		exutil.By("Pre-flight check for the platform type")
		exutil.SkipIfPlatformTypeNot(oc, "AWS")

		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp51255",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("Patch the new custom ingress controller with connectionIdleTimeout as 2m")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"providerParameters\":{\"aws\":{\"classicLoadBalancer\":{\"connectionIdleTimeout\":\"2m\"}}}}}}}")

		exutil.By("Check the LB service and ensure the annotations are updated")
		waitForOutputContains(oc, "openshift-ingress", "svc/router-"+ingctrl.name, "{.metadata.annotations}", `"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout":"120"`)

		exutil.By("Check the connectionIdleTimeout value in the controller status")
		waitForOutputContains(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, "{.status.endpointPublishingStrategy.loadBalancer.providerParameters.aws.classicLoadBalancer.connectionIdleTimeout}", "2m0s")
	})

	g.It("Author:mjoseph-Medium-51256-cluster-ingress-operator does not accept negative value of AWS ELB idle Timeout option", func() {
		exutil.By("Pre-flight check for the platform type")
		exutil.SkipIfPlatformTypeNot(oc, "AWS")

		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp51256",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("Patch the new custom ingress controller with connectionIdleTimeout with a negative value")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"providerParameters\":{\"aws\":{\"classicLoadBalancer\":{\"connectionIdleTimeout\":\"-2m\"}}}}}}}")

		exutil.By("Check the LB service and ensure the annotation is not added")
		findAnnotation := getAnnotation(oc, "openshift-ingress", "svc", "router-"+ingctrl.name)
		o.Expect(findAnnotation).NotTo(o.ContainSubstring("service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout"))

		exutil.By("Check the connectionIdleTimeout value is '0s' in the controller status")
		waitForOutputContains(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, "{.status.endpointPublishingStrategy.loadBalancer.providerParameters.aws.classicLoadBalancer.connectionIdleTimeout}", "0s")
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-High-54868-Configurable dns Management for LoadBalancerService Ingress Controllers on AWS", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-external.yaml")
			ingctrl1            = ingressControllerDescription{
				name:      "ocp54868cus11",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrl2 = ingressControllerDescription{
				name:      "ocp54868cus22",
				namespace: "openshift-ingress-operator",
				domain:    "ocp54868cus22.test.com",
				template:  customTemp,
			}
			ingctrlResource1   = "ingresscontrollers/" + ingctrl1.name
			dnsrecordResource1 = "dnsrecords/" + ingctrl1.name + "-wildcard"
			ingctrlResource2   = "ingresscontrollers/" + ingctrl2.name
			dnsrecordResource2 = "dnsrecords/" + ingctrl2.name + "-wildcard"
		)

		// skip if platform is not AWS
		exutil.SkipIfPlatformTypeNot(oc, "AWS")
		// skip if private cluster in 4.19+
		if isInternalLBScopeInDefaultIngresscontroller(oc) {
			g.Skip("Skip for private cluster since Internal LB scope in default ingresscontroller")
		}

		// skip if the AWS platform has NOT zones and thus the feature is not supported on this cluster
		dnsZone, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("dnses.config", "cluster", "-o=jsonpath={.spec.privateZone}").Output()
		if len(dnsZone) < 1 {
			jsonPath := "{.status.conditions[?(@.type==\"DNSManaged\")].status}: {.status.conditions[?(@.type==\"DNSManaged\")].reason}"
			output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresscontrollers/default", "-n", "openshift-ingress-operator", "-o=jsonpath="+jsonPath).Output()
			o.Expect(output).To(o.ContainSubstring("False: NoDNSZones"))
			g.Skip("Skip for this AWS platform has NOT DNS zones, which means this case is not supported on this AWS platform")
		}

		exutil.By("Create two custom ingresscontrollers, one matches the cluster's base domain, the other doesn't")
		baseDomain := getBaseDomain(oc)
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl1.name)
		ensureCustomIngressControllerAvailable(oc, ingctrl2.name)

		exutil.By("check the default dnsManagementPolicy value of ingress-controller1 matching the base domain, which should be Managed")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args(ingctrlResource1, "-n", ingctrl1.namespace, "-o=jsonpath={.spec.endpointPublishingStrategy.loadBalancer.dnsManagementPolicy}").Output()
		o.Expect(output).To(o.ContainSubstring("Managed"))

		exutil.By("check ingress-controller1's status")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args(ingctrlResource1, "-n", ingctrl1.namespace, "-o=jsonpath={.status.conditions[?(@.type==\"DNSManaged\")].status}{.status.conditions[?(@.type==\"DNSReady\")].status}").Output()
		o.Expect(output).To(o.ContainSubstring("TrueTrue"))

		exutil.By("check the default dnsManagementPolicy value of dnsrecord ocp54868cus1, which should be Managed, too")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args(dnsrecordResource1, "-n", ingctrl1.namespace, "-o=jsonpath={.spec.dnsManagementPolicy}").Output()
		o.Expect(output).To(o.ContainSubstring("Managed"))

		exutil.By("check dnsrecord ocp54868cus1's status")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args(dnsrecordResource1, "-n", ingctrl1.namespace, "-o=jsonpath={.status.zones[0].conditions[0].status}{.status.zones[0].conditions[0].reason}").Output()
		o.Expect(output).To(o.ContainSubstring("TrueProviderSuccess"))

		exutil.By("patch custom ingress-controller1 with dnsManagementPolicy Unmanaged")
		defer func() {
			jsonpath := `{.status.conditions[?(@.type=="DNSManaged")].status}{.status.conditions[?(@.type=="DNSReady")].status}`
			patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, `{"spec":{"endpointPublishingStrategy":{"loadBalancer":{"dnsManagementPolicy":"Managed"}}}}`)
			waitForOutputEquals(oc, ingctrl1.namespace, ingctrlResource1, jsonpath, "TrueTrue")
			jsonpath = "{.spec.dnsManagementPolicy}"
			waitForOutputEquals(oc, ingctrl1.namespace, dnsrecordResource1, jsonpath, "Managed")
		}()
		patchResourceAsAdmin(oc, ingctrl1.namespace, ingctrlResource1, `{"spec":{"endpointPublishingStrategy":{"loadBalancer":{"dnsManagementPolicy":"Unmanaged"}}}}`)

		exutil.By("check the dnsManagementPolicy value of ingress-controller1, which should be Unmanaged")
		jpath := "{.spec.endpointPublishingStrategy.loadBalancer.dnsManagementPolicy}"
		waitForOutputEquals(oc, ingctrl1.namespace, ingctrlResource1, jpath, "Unmanaged")

		exutil.By("check ingress-controller1's status")
		jpath = `{.status.conditions[?(@.type=="DNSManaged")].status}{.status.conditions[?(@.type=="DNSReady")].status}`
		waitForOutputEquals(oc, ingctrl1.namespace, ingctrlResource1, jpath, "FalseUnknown")

		exutil.By("check the dnsManagementPolicy value of dnsrecord ocp54868cus1, which should be Unmanaged, too")
		jpath = "{.spec.dnsManagementPolicy}"
		waitForOutputEquals(oc, ingctrl1.namespace, dnsrecordResource1, jpath, "Unmanaged")

		exutil.By("check dnsrecord ocp54868cus1's status")
		jpath = "{.status.zones[0].conditions[0].status}{.status.zones[0].conditions[0].reason}"
		waitForOutputEquals(oc, ingctrl1.namespace, dnsrecordResource1, jpath, "UnknownUnmanagedDNS")

		// there was a bug OCPBUGS-2247 in the below test step
		// exutil.By("check the default dnsManagementPolicy value of ingress-controller2 not matching the base domain, which should be Unmanaged")
		// output, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args(ingctrlResource2, "-n", ingctrl2.namespace, "-o=jsonpath={.spec.endpointPublishingStrategy.loadBalancer.dnsManagementPolicy}").Output()
		// o.Expect(output).To(o.ContainSubstring("Unmanaged"))

		exutil.By("check ingress-controller2's status")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args(ingctrlResource2, "-n", ingctrl2.namespace, "-o=jsonpath={.status.conditions[?(@.type==\"DNSManaged\")].status}{.status.conditions[?(@.type==\"DNSReady\")].status}").Output()
		o.Expect(output).To(o.ContainSubstring("FalseUnknown"))

		// there was a bug OCPBUGS-2247 in the below test step
		// exutil.By("check the default dnsManagementPolicy value of dnsrecord ocp54868cus2, which should be Unmanaged, too")
		// output, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args(dnsrecordResource2, "-n", ingctrl2.namespace, "-o=jsonpath={.spec.dnsManagementPolicy}").Output()
		// o.Expect(output).To(o.ContainSubstring("Unmanaged"))

		exutil.By("check dnsrecord ocp54868cus2's status")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("get").Args(dnsrecordResource2, "-n", ingctrl2.namespace, "-o=jsonpath={.status.zones[0].conditions[0].status}{.status.zones[0].conditions[0].reason}").Output()
		o.Expect(output).To(o.ContainSubstring("UnknownUnmanagedDNS"))

	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Low-54995-Negative Test of Configurable dns Management for LoadBalancerService Ingress Controllers on AWS", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-external.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp54995",
				namespace: "openshift-ingress-operator",
				domain:    "ocp54995.test.com",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		// skip if platform is not AWS
		exutil.SkipIfPlatformTypeNot(oc, "AWS")

		exutil.By("Create a custom ingresscontrollers")
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("try to patch the custom ingress-controller with dnsManagementPolicy unmanaged")
		patch := "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"dnsManagementPolicy\":\"unmanaged\"}}}}"
		output, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patch, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("dnsManagementPolicy: Unsupported value: \"unmanaged\": supported values: \"Managed\", \"Unmanaged\""))

		exutil.By("try to patch the custom ingress-controller with dnsManagementPolicy abc")
		patch = "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"dnsManagementPolicy\":\"abc\"}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patch, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("dnsManagementPolicy: Unsupported value: \"abc\": supported values: \"Managed\", \"Unmanaged\""))

		exutil.By("try to patch the custom ingress-controller with dnsManagementPolicy 123")
		patch = "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"dnsManagementPolicy\":123}}}}"
		output, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patch, "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("dnsManagementPolicy: Unsupported value: 123: supported values: \"Managed\", \"Unmanaged\""))
	})

	// author: mjoseph@redhat.com
	g.It("[Level0] Author:mjoseph-Critical-55223-Configuring list of IP address ranges using allowedSourceRanges in LoadBalancerService", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-external.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp55223",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/ocp55223"
		)

		// skip if platform is not AWS, GCP, AZURE or IBM
		exutil.By("Pre-flight check for the platform type")
		platformtype := exutil.CheckPlatform(oc)
		platforms := map[string]bool{
			"aws":      true,
			"azure":    true,
			"gcp":      true,
			"ibmcloud": true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-supported platform")
		}

		exutil.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("Patch the custom ingress-controller with IP address ranges to which access to the load balancer should be restricted")
		output, errCfg := patchResourceAsAdminAndGetLog(oc, ingctrl.namespace, ingctrlResource,
			"{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"allowedSourceRanges\":[\"10.0.0.0/8\"]}}}}")
		o.Expect(errCfg).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("ingresscontroller.operator.openshift.io/ocp55223 patched"))

		exutil.By("Check the LB svc of the custom controller")
		jsonPath := "{.spec.loadBalancerSourceRanges}"
		waitForOutputContains(oc, "openshift-ingress", "svc/router-ocp55223", jsonPath, `10.0.0.0/8`)

		exutil.By("Patch the custom ingress-controller with more 'allowedSourceRanges' value")
		output, errCfg = patchResourceAsAdminAndGetLog(oc, ingctrl.namespace, ingctrlResource,
			"{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"allowedSourceRanges\":[\"20.0.0.0/8\", \"50.0.0.0/16\", \"3dee:ef5::/12\"]}}}}")
		o.Expect(errCfg).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("ingresscontroller.operator.openshift.io/ocp55223 patched"))

		exutil.By("Check the LB svc of the custom controller for additional values")
		waitForOutputContains(oc, "openshift-ingress", "svc/router-ocp55223", jsonPath, `20.0.0.0/8`)
		waitForOutputContains(oc, "openshift-ingress", "svc/router-ocp55223", jsonPath, `50.0.0.0/16`)
		waitForOutputContains(oc, "openshift-ingress", "svc/router-ocp55223", jsonPath, `3dee:ef5::/12`)
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-High-55341-configuring list of IP address ranges using load-balancer-source-ranges annotation", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-external.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp55341",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		// skip if platform is not AWS, GCP, AZURE or IBM
		exutil.By("Pre-flight check for the platform type")
		platformtype := exutil.CheckPlatform(oc)
		platforms := map[string]bool{
			"aws":      true,
			"azure":    true,
			"gcp":      true,
			"ibmcloud": true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-supported platform")
		}

		exutil.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("Add the IP address ranges for an custom IngressController using annotation")
		err1 := oc.AsAdmin().WithoutNamespace().Run("annotate").Args(
			"-n", "openshift-ingress", "svc/router-ocp55341", "service.beta.kubernetes.io/load-balancer-source-ranges=10.0.0.0/8", "--overwrite").Execute()
		o.Expect(err1).NotTo(o.HaveOccurred())

		exutil.By("Verify the annotation presence")
		findAnnotation := getAnnotation(oc, "openshift-ingress", "svc", "router-ocp55341")
		o.Expect(findAnnotation).To(o.ContainSubstring("service.beta.kubernetes.io/load-balancer-source-ranges"))
		o.Expect(findAnnotation).To(o.ContainSubstring("10.0.0.0/8"))

		exutil.By("Check the annotation value in the allowedSourceRanges in the controller status")
		waitForOutputContains(oc, "openshift-ingress-operator", "ingresscontroller/ocp55341", "{.status.endpointPublishingStrategy.loadBalancer.allowedSourceRanges}", `10.0.0.0/8`)

		exutil.By("Patch the loadBalancerSourceRanges in the LB service")
		patchResourceAsAdmin(oc, "openshift-ingress", "svc/router-ocp55341", "{\"spec\":{\"loadBalancerSourceRanges\":[\"30.0.0.0/16\"]}}")

		exutil.By("Check the annotation value and sourcerange value in LB svc")
		findAnnotation = getAnnotation(oc, "openshift-ingress", "svc", "router-ocp55341")
		o.Expect(findAnnotation).To(o.ContainSubstring("service.beta.kubernetes.io/load-balancer-source-ranges"))
		o.Expect(findAnnotation).To(o.ContainSubstring("10.0.0.0/8"))
		waitForOutputContains(oc, "openshift-ingress", "svc/router-ocp55341", "{.spec.loadBalancerSourceRanges}", `30.0.0.0/16`)

		exutil.By("Check the controller status and confirm the sourcerange value's precedence over the annotation")
		waitForOutputContains(oc, "openshift-ingress-operator", "ingresscontroller/ocp55341", "{.status.endpointPublishingStrategy.loadBalancer.allowedSourceRanges}", `30.0.0.0/16`)
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Medium-55381-Configuring wrong data for allowedSourceRanges in LoadBalancerService", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-external.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp55381",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/ocp55381"
		)

		// skip if platform is not AWS, GCP, AZURE or IBM
		exutil.By("Pre-flight check for the platform type")
		platformtype := exutil.CheckPlatform(oc)
		platforms := map[string]bool{
			"aws":      true,
			"azure":    true,
			"gcp":      true,
			"ibmcloud": true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-supported platform")
		}

		exutil.By("Create a custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("Patch the custom ingress-controller with only IP address")
		output, errCfg := patchResourceAsAdminAndGetLog(oc, ingctrl.namespace, ingctrlResource,
			"{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"allowedSourceRanges\":[\"10.0.0.0\"]}}}}")
		o.Expect(errCfg).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("The IngressController \"ocp55381\" is invalid"))

		exutil.By("Patch the custom ingress-controller with a invalid IPv6 address")
		output, errCfg = patchResourceAsAdminAndGetLog(oc, ingctrl.namespace, ingctrlResource,
			"{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"allowedSourceRanges\":[\"3dee:ef5:/12\"]}}}}")
		o.Expect(errCfg).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("The IngressController \"ocp55381\" is invalid"))

		exutil.By("Patch the custom ingress-controller with IP address ranges")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"allowedSourceRanges\":[\"10.0.0.0/8\"]}}}}")

		exutil.By("Delete the allowedSourceRanges from custom controller")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"allowedSourceRanges\":[]}}}}")

		exutil.By("Check the ingress operator status to confirm whether it is still in Progress")
		ensureClusterOperatorProgress(oc, "ingress")

		exutil.By("Patch the same loadBalancerSourceRanges value in the LB service to remove the Progressing from the ingress operator")
		patchResourceAsAdmin(oc, "openshift-ingress", "svc/router-ocp55381", "{\"spec\":{\"loadBalancerSourceRanges\":[]}}")
	})

	// bug: 2007246
	g.It("Author:shudili-Medium-56772-Ingress Controller does not set allowPrivilegeEscalation in the router deployment [Serial]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			scc                 = filepath.Join(buildPruningBaseDir, "scc-bug2007246.json")
			ingctrl             = ingressControllerDescription{
				name:      "ocp56772",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("Create a custom ingresscontrollers")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		exutil.By("Create the custom-restricted SecurityContextConstraints")
		defer operateResourceFromFile(oc, "delete", "openshift-ingress", scc)
		operateResourceFromFile(oc, "create", "openshift-ingress", scc)

		exutil.By("check the allowPrivilegeEscalation in the router deployment, which should be true")
		jsonPath := "{.spec.template.spec.containers..securityContext.allowPrivilegeEscalation}"
		value := getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrl.name, jsonPath)
		o.Expect(value).To(o.ContainSubstring("true"))

		exutil.By("get router pods and then delete one router pod")
		podList1, err1 := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+ingctrl.name, "-o=jsonpath={.items[*].metadata.name}", "-n", "openshift-ingress").Output()
		o.Expect(err1).NotTo(o.HaveOccurred())
		routerpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		err := oc.AsAdmin().WithoutNamespace().Run("delete").Args("pod", routerpod, "-n", "openshift-ingress").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("get router pods again, and check if it is different with the previous router pod list")
		podList2, err2 := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+ingctrl.name, "-o=jsonpath={.items[*].metadata.name}", "-n", "openshift-ingress").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
		o.Expect(len(podList1)).To(o.Equal(len(podList2)))
		o.Expect(strings.Compare(podList1, podList2)).NotTo(o.Equal(0))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-NonPreRelease-Medium-60012-matchExpressions for routeSelector defined in an ingress-controller", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			ingctrl             = ingressControllerDescription{
				name:      "ocp60012",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		exutil.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("Create an unsecure service and its backend pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		exutil.By("Create 4 routes for the testing")
		err := oc.WithoutNamespace().Run("expose").Args("service", srvName, "--name=unsrv-1", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.WithoutNamespace().Run("expose").Args("service", srvName, "--name=unsrv-2", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.WithoutNamespace().Run("expose").Args("service", srvName, "--name=unsrv-3", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.WithoutNamespace().Run("expose").Args("service", srvName, "--name=unsrv-4", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, ns, "route/unsrv-1", "{.metadata.name}", "unsrv-1")
		waitForOutputEquals(oc, ns, "route/unsrv-2", "{.metadata.name}", "unsrv-2")
		waitForOutputEquals(oc, ns, "route/unsrv-3", "{.metadata.name}", "unsrv-3")
		waitForOutputEquals(oc, ns, "route/unsrv-4", "{.metadata.name}", "unsrv-4")

		exutil.By("Add labels to 3 routes")
		err = oc.WithoutNamespace().Run("label").Args("route", "unsrv-1", "test=aaa", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.WithoutNamespace().Run("label").Args("route", "unsrv-2", "test=bbb", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.WithoutNamespace().Run("label").Args("route", "unsrv-3", "test=ccc", "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Patch the custom ingress-controllers with In matchExpressions routeSelector")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		patchRouteSelector := "{\"spec\":{\"routeSelector\":{\"matchExpressions\":[{\"key\": \"test\", \"operator\": \"In\", \"values\":[\"aaa\", \"bbb\"]}]}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patchRouteSelector, "--type=merge", "-n", ingctrl.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("Check if route unsrv-1 and unsrv-2 are admitted by the custom IC with In matchExpressions routeSelector, while route unsrv-3 and unsrv-4 not")
		jsonPath := "{.status.ingress[?(@.routerName==\"" + ingctrl.name + "\")].conditions[?(@.type==\"Admitted\")].status}"
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsrv-1", ingctrl.name)
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsrv-2", ingctrl.name)
		waitForOutputEquals(oc, ns, "route/unsrv-3", jsonPath, "")
		waitForOutputEquals(oc, ns, "route/unsrv-4", jsonPath, "")

		exutil.By("Patch the custom ingress-controllers with NotIn matchExpressions routeSelector")
		routerpod = getOneRouterPodNameByIC(oc, ingctrl.name)
		patchRouteSelector = "{\"spec\":{\"routeSelector\":{\"matchExpressions\":[{\"key\": \"test\", \"operator\": \"NotIn\", \"values\":[\"aaa\", \"bbb\"]}]}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patchRouteSelector, "--type=merge", "-n", ingctrl.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("Check if route unsrv-3 and unsrv-4 are admitted by the custom IC with NotIn matchExpressions routeSelector, while route unsrv-1 and unsrv-2 not")
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsrv-3", ingctrl.name)
		waitForOutputEquals(oc, ns, "route/unsrv-1", jsonPath, "")
		waitForOutputEquals(oc, ns, "route/unsrv-2", jsonPath, "")
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsrv-4", ingctrl.name)

		exutil.By("Patch the custom ingress-controllers with Exists matchExpressions routeSelector")
		routerpod = getOneRouterPodNameByIC(oc, ingctrl.name)
		patchRouteSelector = "{\"spec\":{\"routeSelector\":{\"matchExpressions\":[{\"key\": \"test\", \"operator\": \"Exists\"}]}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patchRouteSelector, "--type=merge", "-n", ingctrl.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("Check if route unsrv-1, unsrv-2 and unsrv-3 are admitted by the custom IC with Exists matchExpressions routeSelector, while route unsrv-4 not")
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsrv-1", ingctrl.name)
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsrv-2", ingctrl.name)
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsrv-3", ingctrl.name)
		waitForOutputEquals(oc, ns, "route/unsrv-4", jsonPath, "")

		exutil.By("Patch the custom ingress-controllers with DoesNotExist matchExpressions routeSelector")
		routerpod = getOneRouterPodNameByIC(oc, ingctrl.name)
		patchRouteSelector = "{\"spec\":{\"routeSelector\":{\"matchExpressions\":[{\"key\": \"test\", \"operator\": \"DoesNotExist\"}]}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patchRouteSelector, "--type=merge", "-n", ingctrl.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("Check if route unsrv-4 is admitted by the custom IC with DoesNotExist matchExpressions routeSelector, while route unsrv-1, unsrv-2 and unsrv-3 not")
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsrv-4", ingctrl.name)
		waitForOutputEquals(oc, ns, "route/unsrv-1", jsonPath, "")
		waitForOutputEquals(oc, ns, "route/unsrv-2", jsonPath, "")
		waitForOutputEquals(oc, ns, "route/unsrv-3", jsonPath, "")
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-NonPreRelease-Medium-60013-matchExpressions for namespaceSelector defined in an ingress-controller", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			ingctrl             = ingressControllerDescription{
				name:      "ocp60013",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
		)

		exutil.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("create 3 more projects")
		project1 := oc.Namespace()
		oc.SetupProject()
		project2 := oc.Namespace()
		oc.SetupProject()
		project3 := oc.Namespace()
		oc.SetupProject()
		project4 := oc.Namespace()

		exutil.By("Create an unsecure service and its backend pod, create the route in each of the 4 projects, then wait for some time until the backend pod and route are available")
		for index, ns := range []string{project1, project2, project3, project4} {
			nsSeq := index + 1
			err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", testPodSvc, "-n", ns).Execute()
			o.Expect(err).NotTo(o.HaveOccurred())
			err = oc.AsAdmin().WithoutNamespace().Run("expose").Args("service", srvName, "--name=shard-ns"+strconv.Itoa(nsSeq), "-n", ns).Execute()
			o.Expect(err).NotTo(o.HaveOccurred())
		}
		for indexWt, nsWt := range []string{project1, project2, project3, project4} {
			nsSeqWt := indexWt + 1
			ensurePodWithLabelReady(oc, nsWt, "name="+srvrcInfo)
			waitForOutputEquals(oc, nsWt, "route/shard-ns"+strconv.Itoa(nsSeqWt), "{.metadata.name}", "shard-ns"+strconv.Itoa(nsSeqWt))
		}

		exutil.By("Add labels to 3 projects")
		err := oc.AsAdmin().WithoutNamespace().Run("label").Args("namespace", project1, "test=aaa").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("label").Args("namespace", project2, "test=bbb").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("label").Args("namespace", project3, "test=ccc").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Patch the custom ingresscontroller with In matchExpressions namespaceSelector")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		patchNamespaceSelector := "{\"spec\":{\"namespaceSelector\":{\"matchExpressions\":[{\"key\": \"test\", \"operator\": \"In\", \"values\":[\"aaa\", \"bbb\"]}]}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patchNamespaceSelector, "--type=merge", "-n", ingctrl.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("Check if route shard-ns1 and shard-ns2 are admitted by the custom IC with In matchExpressions namespaceSelector, while route shard-ns3 and shard-ns4 not")
		jsonPath := "{.status.ingress[?(@.routerName==\"" + ingctrl.name + "\")].conditions[?(@.type==\"Admitted\")].status}"
		ensureRouteIsAdmittedByIngressController(oc, project1, "shard-ns1", ingctrl.name)
		ensureRouteIsAdmittedByIngressController(oc, project2, "shard-ns2", ingctrl.name)
		waitForOutputEquals(oc, project3, "route/shard-ns3", jsonPath, "")
		waitForOutputEquals(oc, project4, "route/shard-ns4", jsonPath, "")

		exutil.By("Patch the custom ingresscontroller with NotIn matchExpressions namespaceSelector")
		routerpod = getOneRouterPodNameByIC(oc, ingctrl.name)
		patchNamespaceSelector = "{\"spec\":{\"namespaceSelector\":{\"matchExpressions\":[{\"key\": \"test\", \"operator\": \"NotIn\", \"values\":[\"aaa\", \"bbb\"]}]}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patchNamespaceSelector, "--type=merge", "-n", ingctrl.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("Check if route shard-ns3 and shard-ns4 are admitted by the custom IC with NotIn matchExpressions namespaceSelector, while route shard-ns1 and shard-ns2 not")
		ensureRouteIsAdmittedByIngressController(oc, project3, "shard-ns3", ingctrl.name)
		waitForOutputEquals(oc, project1, "route/shard-ns1", jsonPath, "")
		waitForOutputEquals(oc, project2, "route/shard-ns2", jsonPath, "")
		ensureRouteIsAdmittedByIngressController(oc, project4, "shard-ns4", ingctrl.name)

		exutil.By("Patch the custom ingresscontroller with Exists matchExpressions namespaceSelector")
		routerpod = getOneRouterPodNameByIC(oc, ingctrl.name)
		patchNamespaceSelector = "{\"spec\":{\"namespaceSelector\":{\"matchExpressions\":[{\"key\": \"test\", \"operator\": \"Exists\"}]}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patchNamespaceSelector, "--type=merge", "-n", ingctrl.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("Check if route shard-ns1, shard-ns2 and shard-ns3 are admitted by the custom IC with Exists matchExpressions namespaceSelector, while route shard-ns4 not")
		ensureRouteIsAdmittedByIngressController(oc, project1, "shard-ns1", ingctrl.name)
		ensureRouteIsAdmittedByIngressController(oc, project2, "shard-ns2", ingctrl.name)
		ensureRouteIsAdmittedByIngressController(oc, project3, "shard-ns3", ingctrl.name)
		waitForOutputEquals(oc, project4, "route/shard-ns4", jsonPath, "")

		exutil.By("Patch the custom ingresscontroller with DoesNotExist matchExpressions namespaceSelector")
		routerpod = getOneRouterPodNameByIC(oc, ingctrl.name)
		patchNamespaceSelector = "{\"spec\":{\"namespaceSelector\":{\"matchExpressions\":[{\"key\": \"test\", \"operator\": \"DoesNotExist\"}]}}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args(ingctrlResource, "-p", patchNamespaceSelector, "--type=merge", "-n", ingctrl.namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-ingress", "pod/"+routerpod)
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+routerpod))

		exutil.By("Check if route shard-ns4 is admitted by the custom IC with DoesNotExist matchExpressions namespaceSelector, while route shard-ns1, shard-ns2 and shard-ns3 not")
		ensureRouteIsAdmittedByIngressController(oc, project4, "shard-ns4", ingctrl.name)
		waitForOutputEquals(oc, project1, "route/shard-ns1", jsonPath, "")
		waitForOutputEquals(oc, project2, "route/shard-ns2", jsonPath, "")
		waitForOutputEquals(oc, project3, "route/shard-ns3", jsonPath, "")
	})

	// OCPBUGS-853
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-62530-openshift ingress operator is failing to update router-certs [Serial]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp62530",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontroller/" + ingctrl.name
			ingctrlCert     = "custom-cert-62530"
			dirname         = "/tmp/-OCP-62530-ca/"
			name            = dirname + "custom62530"
			validity        = 3650
			caSubj          = "/CN=NE-Test-Root-CA"
			userCert        = dirname + "test"
			userSubj        = "/CN=*.ocp62530.example.com"
			customKey       = userCert + ".key"
			customCert      = userCert + ".crt"
		)

		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Try to create custom key and custom certification by openssl, create a new self-signed CA at first, creating the CA key")
		// Generation of a new self-signed CA, in case a corporate or another CA is already existing can be used.
		opensslCmd := fmt.Sprintf(`openssl genrsa -out %s-ca.key 4096`, name)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create the CA certificate")
		opensslCmd = fmt.Sprintf(`openssl req -x509 -new -nodes -key %s-ca.key -sha256 -days %d -out %s-ca.crt -subj %s`, name, validity, name, caSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create a new user certificate, crearing the user CSR with the private user key")
		opensslCmd = fmt.Sprintf(`openssl req -nodes -newkey rsa:2048 -keyout %s.key -subj %s -out %s.csr`, userCert, userSubj, userCert)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Sign the user CSR and generate the certificate")
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS:*.ocp62530.example.com") -req -in %s.csr -CA %s-ca.crt -CAkey %s-ca.key -CAcreateserial -out %s.crt -days %d -sha256`, userCert, name, name, userCert, validity)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create a tls secret in openshift-ingress ns")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", "openshift-ingress", "secret", ingctrlCert).Execute()
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", "openshift-ingress", "secret", "tls", ingctrlCert, "--cert="+customCert, "--key="+customKey).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + ".example.com"
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("Patch defaultCertificate with custom secret to the IC")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, "{\"spec\":{\"defaultCertificate\":{\"name\": \""+ingctrlCert+"\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		secret := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, "{.spec.defaultCertificate.name}")
		o.Expect(secret).To(o.ContainSubstring(ingctrlCert))

		exutil.By("Check the router-certs in the openshift-config-managed namespace, the data is 1, which should not be increased")
		output, err2 := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", "openshift-config-managed", "secret", "router-certs", "-o=go-template='{{len .data}}'").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
		o.Expect(strings.Trim(output, "'")).To(o.Equal("1"))
	})

	//author: asood@redhat.com
	//bug: https://issues.redhat.com/browse/OCPBUGS-6013
	g.It("Author:asood-NonHyperShiftHOST-ConnectedOnly-ROSA-OSD_CCS-Medium-63832-Cluster ingress health checks and routes fail on swapping application router between public and private", func() {
		var (
			namespace         = "openshift-ingress"
			operatorNamespace = "openshift-ingress-operator"
			caseID            = "63832"
			curlURL           = ""
			strategyScope     []string
		)
		platform := exutil.CheckPlatform(oc)
		acceptedPlatform := strings.Contains(platform, "aws")
		if !acceptedPlatform {
			g.Skip("Test cases should be run on AWS cluster with ovn network plugin, skip for other platforms or other network plugin!!")
		}
		exutil.By("0. Create a custom ingress controller")
		buildPruningBaseDir := testdata.FixturePath("router")
		customIngressControllerTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp" + caseID,
				namespace: operatorNamespace,
				domain:    caseID + ".test.com",
				template:  customIngressControllerTemp,
			}
		)

		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("1. Annotate ingress controller")
		addAnnotationPatch := `{"metadata":{"annotations":{"ingress.operator.openshift.io/auto-delete-load-balancer":""}}}`
		errAnnotate := oc.AsAdmin().WithoutNamespace().Run("patch").Args("-n", ingctrl.namespace, "ingresscontrollers/"+ingctrl.name, "--type=merge", "-p", addAnnotationPatch).Execute()
		o.Expect(errAnnotate).NotTo(o.HaveOccurred())

		strategyScope = append(strategyScope, `{"spec":{"endpointPublishingStrategy":{"loadBalancer":{"scope":"Internal"},"type":"LoadBalancerService"}}}`)
		strategyScope = append(strategyScope, `{"spec":{"endpointPublishingStrategy":{"loadBalancer":{"scope":"External"},"type":"LoadBalancerService"}}}`)

		exutil.By("2. Get the health check node port")
		prevHealthCheckNodePort, err := oc.AsAdmin().Run("get").Args("svc", "router-"+ingctrl.name, "-n", namespace, "-o=jsonpath={.spec.healthCheckNodePort}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		for i := 0; i < len(strategyScope); i++ {
			exutil.By("3. Change the endpoint publishing strategy")
			changeScope := strategyScope[i]
			changeScopeErr := oc.AsAdmin().WithoutNamespace().Run("patch").Args("-n", ingctrl.namespace, "ingresscontrollers/"+ingctrl.name, "--type=merge", "-p", changeScope).Execute()
			o.Expect(changeScopeErr).NotTo(o.HaveOccurred())

			exutil.By("3.1 Check the state of custom ingress operator")
			ensureCustomIngressControllerAvailable(oc, ingctrl.name)

			exutil.By("3.2 Check the pods are in running state")
			podList, podListErr := exutil.GetAllPodsWithLabel(oc, namespace, "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+ingctrl.name)
			o.Expect(podListErr).NotTo(o.HaveOccurred())
			o.Expect(len(podList)).ShouldNot(o.Equal(0))
			podName := podList[0]

			exutil.By("3.3 Get node name of one of the pod")
			nodeName, nodeNameErr := exutil.GetPodNodeName(oc, namespace, podName)
			o.Expect(nodeNameErr).NotTo(o.HaveOccurred())

			exutil.By("3.4. Get new health check node port")
			err = wait.Poll(30*time.Second, 5*time.Minute, func() (bool, error) {
				healthCheckNodePort, healthCheckNPErr := oc.AsAdmin().Run("get").Args("svc", "router-"+ingctrl.name, "-n", namespace, "-o=jsonpath={.spec.healthCheckNodePort}").Output()
				o.Expect(healthCheckNPErr).NotTo(o.HaveOccurred())
				if healthCheckNodePort == prevHealthCheckNodePort {
					return false, nil
				}
				curlURL = net.JoinHostPort(nodeName, healthCheckNodePort)
				prevHealthCheckNodePort = healthCheckNodePort
				return true, nil
			})
			exutil.AssertWaitPollNoErr(err, fmt.Sprintf("Failed to get health check node port %s", err))

			exutil.By("3.5. Check endpoint is 1")
			cmd := fmt.Sprintf("curl %s -s --connect-timeout 5", curlURL)
			output, err := exutil.DebugNode(oc, nodeName, "bash", "-c", cmd)
			o.Expect(err).NotTo(o.HaveOccurred())
			o.Expect(strings.Contains(output, "\"localEndpoints\": 1")).To(o.BeTrue())
		}
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-Critical-64611-Ingress operator support for private hosted zones in Shared VPC clusters", func() {
		exutil.By("Pre-flight check for the platform type")
		exutil.SkipIfPlatformTypeNot(oc, "AWS")

		exutil.By("Pre-flight check for the shared VPC platform")
		// privateZoneIAMRole needs to be present for shared vpc cluster
		privateZoneIAMRole, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.platform.aws}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(privateZoneIAMRole, "privateZoneIAMRole") {
			g.Skip("Skip since this is not a shared vpc cluster")
		}

		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp1 := filepath.Join(buildPruningBaseDir, "ingresscontroller-external.yaml")
		customTemp2 := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		var (
			ingctrl1 = ingressControllerDescription{
				name:      "ocp64611external",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp1,
			}
			ingctrl2 = ingressControllerDescription{
				name:      "ocp64611clb",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp2,
			}
		)

		exutil.By("1. Check the STS Role in the cluster")
		output, ouputErr := oc.AsAdmin().WithoutNamespace().Run("get").Args("CredentialsRequest/openshift-ingress", "-n", "openshift-cloud-credential-operator", "-o=jsonpath={.spec.providerSpec.statementEntries[0].action}").Output()
		o.Expect(ouputErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("sts:AssumeRole"))

		exutil.By("2. Check whether the privateZoneIAMRole is created using the ARN")
		arn, getArnErr := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.platform.aws.privateZoneIAMRole}").Output()
		o.Expect(getArnErr).NotTo(o.HaveOccurred())
		privateZoneIAMRoleRegex := regexp.MustCompile("arn:(aws|aws-cn|aws-us-gov):iam::[0-9]{12}:role/.*")
		privateZoneIAMRoleMatch := privateZoneIAMRoleRegex.FindStringSubmatch(arn)
		o.Expect(arn).To(o.ContainSubstring(privateZoneIAMRoleMatch[0]))

		exutil.By("3. Check the default DNS management status")
		dnsManagementPolicy := getByJsonPath(oc, "openshift-ingress-operator", "dnsrecords/default-wildcard", "{.spec.dnsManagementPolicy}")
		o.Expect("Managed").To(o.ContainSubstring(dnsManagementPolicy))

		exutil.By("4. Collecting the public zone and private zone id from dns config")
		privateZoneId := getByJsonPath(oc, "openshift-dns", "dns.config/cluster", "{.spec.privateZone.id}")
		publicZoneId := getByJsonPath(oc, "openshift-dns", "dns.config/cluster", "{.spec.publicZone.id}")

		exutil.By("5. Collecting zone details from default ingress controller and cross checking it wih dns config details")
		checkDnsRecordsInIngressOperator(oc, "default-wildcard", privateZoneId, publicZoneId)

		exutil.By("6. Check the default dnsrecord of the ingress operator to confirm there is no degardes")
		checkDnsRecordStatusOfIngressOperator(oc, "default-wildcard", "status", "True")
		checkDnsRecordStatusOfIngressOperator(oc, "default-wildcard", "reason", "ProviderSuccess")

		exutil.By("7. Create two custom ingresscontrollers")
		baseDomain := getBaseDomain(oc)
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl1.name)
		ensureCustomIngressControllerAvailable(oc, ingctrl2.name)

		exutil.By("8. Check the custom DNS management status")
		dnsManagementPolicy1 := getByJsonPath(oc, "openshift-ingress-operator", "dnsrecords/ocp64611external-wildcard", "{.spec.dnsManagementPolicy}")
		o.Expect("Managed").To(o.ContainSubstring(dnsManagementPolicy1))
		dnsManagementPolicy2 := getByJsonPath(oc, "openshift-ingress-operator", "dnsrecords/ocp64611clb-wildcard", "{.spec.dnsManagementPolicy}")
		o.Expect("Managed").To(o.ContainSubstring(dnsManagementPolicy2))

		exutil.By("9. Collecting zone details from custom ingress controller and cross checking it wih dns zone details")
		checkDnsRecordsInIngressOperator(oc, "ocp64611external-wildcard", privateZoneId, publicZoneId)
		checkDnsRecordsInIngressOperator(oc, "ocp64611clb-wildcard", privateZoneId, publicZoneId)

		exutil.By("10. Check the custom dnsrecord of the ingress operator to confirm there is no degardes")
		checkDnsRecordStatusOfIngressOperator(oc, "ocp64611external-wildcard", "status", "True")
		checkDnsRecordStatusOfIngressOperator(oc, "ocp64611external-wildcard", "reason", "ProviderSuccess")
		checkDnsRecordStatusOfIngressOperator(oc, "ocp64611clb-wildcard", "status", "True")
		checkDnsRecordStatusOfIngressOperator(oc, "ocp64611clb-wildcard", "reason", "ProviderSuccess")
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-High-65827-allow Ingress to modify the HAProxy Log Length when using a Sidecar", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-sidecar.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp65827",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("1. Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2. Check the env variable of the router pod to verify the default log length")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		logLength := readRouterPodEnv(oc, newrouterpod, "ROUTER_LOG_MAX_LENGTH")
		o.Expect(logLength).To(o.ContainSubstring(`ROUTER_LOG_MAX_LENGTH=1024`))

		exutil.By("3. Check the haproxy config on the router pod to verify the default log length is enabled")
		checkoutput := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "1024")
		o.Expect(checkoutput).To(o.ContainSubstring(`log /var/lib/rsyslog/rsyslog.sock len 1024 local1 info`))

		exutil.By("4. Patch the existing custom ingress controller with minimum log length value")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp65827", "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"container\":{\"maxLength\":480}}}}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		exutil.By("5. Check the env variable of the router pod to verify the minimum log length")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		minimumlogLength := readRouterPodEnv(oc, newrouterpod, "ROUTER_LOG_MAX_LENGTH")
		o.Expect(minimumlogLength).To(o.ContainSubstring(`ROUTER_LOG_MAX_LENGTH=480`))

		exutil.By("6. Patch the existing custom ingress controller with maximum log length value")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp65827", "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"container\":{\"maxLength\":8192}}}}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		exutil.By("7. Check the env variable of the router pod to verify the maximum log length")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		maximumlogLength := readRouterPodEnv(oc, newrouterpod, "ROUTER_LOG_MAX_LENGTH")
		o.Expect(maximumlogLength).To(o.ContainSubstring(`ROUTER_LOG_MAX_LENGTH=8192`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Low-65903-ingresscontroller should deny invalid maxlengh value when using a Sidecar", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp1 := filepath.Join(buildPruningBaseDir, "ingresscontroller-syslog.yaml")
		customTemp2 := filepath.Join(buildPruningBaseDir, "ingresscontroller-sidecar.yaml")

		var (
			ingctrl1 = ingressControllerDescription{
				name:      "ocp65903syslog",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp1,
			}
			ingctrl2 = ingressControllerDescription{
				name:      "ocp65903sidecar",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp2,
			}
		)
		exutil.By("1. Create two custom ingresscontrollers")
		baseDomain := getBaseDomain(oc)
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "1")
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")

		exutil.By("2. Check the haproxy config on the syslog router pod to verify the default log length")
		syslogRouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)
		checkoutput1 := readRouterPodData(oc, syslogRouterpod, "cat haproxy.config", "1024")
		o.Expect(checkoutput1).To(o.ContainSubstring(`log 1.2.3.4:514 len 1024 local1 info`))

		exutil.By("3. Check the haproxy config on the sidecar router pod to verify the default log length")
		sidecarRouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)
		checkoutput2 := readRouterPodData(oc, sidecarRouterpod, "cat haproxy.config", "1024")
		o.Expect(checkoutput2).To(o.ContainSubstring(`log /var/lib/rsyslog/rsyslog.sock len 1024 local1 info`))

		exutil.By("4. Patch the existing sidecar ingress controller with log length value less than minimum threshold")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/ocp65903sidecar", "-p", "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"container\":{\"maxLength\":479}}}}}}", "--type=merge", "-n", ingctrl2.namespace).Output()
		o.Expect(output1).To(o.ContainSubstring("spec.logging.access.destination.container.maxLength: Invalid value: 479: spec.logging.access.destination.container.maxLength in body should be greater than or equal to 480"))

		exutil.By("5. Patch the existing sidecar ingress controller with log length value more than maximum threshold")
		output2, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/ocp65903sidecar", "-p", "{\"spec\":{\"logging\":{\"access\":{\"destination\":{\"container\":{\"maxLength\":8193}}}}}}", "--type=merge", "-n", ingctrl2.namespace).Output()
		o.Expect(output2).To(o.ContainSubstring("spec.logging.access.destination.container.maxLength: Invalid value: 8193: spec.logging.access.destination.container.maxLength in body should be less than or equal to 8192"))
	})

	// OCPBUGS-33657(including OCPBUGS-35027 and OCPBUGS-35454 in OCP-75907)
	// guest hypershift cluster had not the ingress-operator pod, skipped on it
	g.It("Author:shudili-NonHyperShiftHOST-ROSA-OSD_CCS-ARO-High-75907-Ingress Operator should not always remain in the progressing state [Disruptive]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		privateTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-private.yaml")
		hostnetworkTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")

		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we at least need one Linux worker node")
		}

		// make sure the ingress operator is normal
		jsonPath := `-o=jsonpath={.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		output := getByJsonPath(oc, "default", "clusteroperator/ingress", jsonPath)
		if !strings.Contains(output, "TrueFalseFalse") {
			jsonPath = `-o=jsonpath={.status}`
			output = getByJsonPath(oc, "openshift-ingress-operator", "ingresscontroller/default", jsonPath)
			e2e.Logf("check the status of the default ingresscontroller:\n%v", output)
			ensureClusterOperatorNormal(oc, "ingress", 1, 120)
		}

		// OCPBUGS-35027
		extraParas := "    clientTLS:\n      clientCA:\n        name: client-ca-cert\n      clientCertificatePolicy: Required\n"
		customTemp35027 := addExtraParametersToYamlFile(privateTemp, "spec:", extraParas)
		defer os.Remove(customTemp35027)
		var (
			ingctrl35027 = ingressControllerDescription{
				name:      "bug35027",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp35027,
			}
		)

		exutil.By("1. Create a configmap with empty configration")
		output, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", "openshift-config", "configmap", "custom-ca35027").Output()
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", "openshift-config", "configmap", "custom-ca35027").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("configmap/custom-ca35027 created"))

		exutil.By("2. Create a ingresscontroller for OCPBUGS-35027")
		baseDomain := getBaseDomain(oc)
		ingctrl35027.domain = ingctrl35027.name + "." + baseDomain
		defer ingctrl35027.delete(oc)
		ingctrl35027.create(oc)
		err = waitForCustomIngressControllerAvailable(oc, ingctrl35027.name)
		o.Expect(err).To(o.HaveOccurred())

		exutil.By("3. Check the custom router pod should not be created for the custom ingresscontroller was abnormal")
		wait.PollImmediate(5*time.Second, 30*time.Second, func() (bool, error) {
			_, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("pods", "-l", "ingresscontroller.operator.openshift.io/deployment-ingresscontroller="+ingctrl35027.name, "-o=jsonpath={.items[0].metadata.name}", "-n", "openshift-ingress").Output()
			o.Expect(err).To(o.HaveOccurred())
			return false, nil
		})

		exutil.By("4. Delete the custom ingress controller, and then check the logs that clientca-configmap finalizer log should not appear")
		ingctrl35027.delete(oc)
		output, err = oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress-operator", "-c", "ingress-operator", "deployments/ingress-operator", "--tail=20").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(output, `failed to add custom-ca35027-configmap finalizer: IngressController.operator.openshift.io \"custom-ca35027-configmap\" is invalid`)).NotTo(o.BeTrue())

		// OCPBUGS-35454
		var (
			ingctrlhp35454 = ingctrlHostPortDescription{
				name:      "bug35454",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  22080,
				httpsport: 22443,
				statsport: 22936,
				template:  hostnetworkTemp,
			}
		)

		exutil.By("5. Create the custom ingress controller for OCPBUGS-35454")
		ingctrlhp35454.domain = ingctrlhp35454.name + "." + baseDomain
		defer ingctrlhp35454.delete(oc)
		ingctrlhp35454.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrlhp35454.name)

		exutil.By(`6. Check the service's spec ports of http/https/metrics`)
		output = getByJsonPath(oc, "openshift-ingress", "service/router-internal-"+ingctrlhp35454.name, "{.spec.ports}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring(`{"name":"http","port":80,"protocol":"TCP","targetPort":"http"}`),
			o.ContainSubstring(`{"name":"https","port":443,"protocol":"TCP","targetPort":"https"}`),
			o.ContainSubstring(`{"name":"metrics","port":1936,"protocol":"TCP","targetPort":"metrics"}`)))

		exutil.By(`7. Check the service's ep of http/https/metrics`)
		output = getByJsonPath(oc, "openshift-ingress", "ep/router-internal-"+ingctrlhp35454.name, "{.subsets[0].ports}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring(`{"name":"http","port":22080,"protocol":"TCP"}`),
			o.ContainSubstring(`{"name":"https","port":22443,"protocol":"TCP"}`),
			o.ContainSubstring(`{"name":"metrics","port":22936,"protocol":"TCP"}`)))

		exutil.By(`8. Check the configuration update for the custom router deployment and the internal service`)
		output = getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrlhp35454.name, "{..livenessProbe}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring(`"failureThreshold":3`),
			o.ContainSubstring(`"scheme":"HTTP"`),
			o.ContainSubstring(`"periodSeconds":10`),
			o.ContainSubstring(`"successThreshold":1`),
			o.ContainSubstring(`"timeoutSeconds":1`)))

		output = getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrlhp35454.name, "{..readinessProbe}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring(`"failureThreshold":3`),
			o.ContainSubstring(`"scheme":"HTTP"`),
			o.ContainSubstring(`"periodSeconds":10`),
			o.ContainSubstring(`"successThreshold":1`),
			o.ContainSubstring(`"timeoutSeconds":1`)))

		output = getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrlhp35454.name, "{..startupProbe}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring(`"scheme":"HTTP"`),
			o.ContainSubstring(`"successThreshold":1`),
			o.ContainSubstring(`"timeoutSeconds":1`)))

		output = getByJsonPath(oc, "openshift-ingress", "deployment/router-"+ingctrlhp35454.name, "{..configMap.defaultMode}")
		o.Expect(output).To(o.ContainSubstring("420"))

		exutil.By(`9. Check the service's sessionAffinity, which should be None`)
		output = getByJsonPath(oc, "openshift-ingress", "service/router-internal-"+ingctrlhp35454.name, "{.spec.sessionAffinity}")
		o.Expect(output).To(o.ContainSubstring("None"))

		exutil.By(`10. Patch the custom ingress controller with other http/https/metrics ports`)
		jsonpath := `{"spec": { "endpointPublishingStrategy": { "hostNetwork": {"httpPort": 23080, "httpsPort": 23443, "statsPort": 23936 }}}}`
		patchResourceAsAdmin(oc, ingctrlhp35454.namespace, "ingresscontroller/"+ingctrlhp35454.name, jsonpath)
		ensureRouterDeployGenerationIs(oc, ingctrlhp35454.name, "2")

		exutil.By(`11. Check the service's ep of http/https/metrics, which should be updated to the specified ports`)
		output = getByJsonPath(oc, "openshift-ingress", "ep/router-internal-"+ingctrlhp35454.name, "{.subsets[0].ports}")
		o.Expect(output).Should(o.And(
			o.ContainSubstring(`{"name":"http","port":23080,"protocol":"TCP"}`),
			o.ContainSubstring(`{"name":"https","port":23443,"protocol":"TCP"}`),
			o.ContainSubstring(`{"name":"metrics","port":23936,"protocol":"TCP"}`)))
	})

	// OCPBUGS-29373
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-75908-http2 connection coalescing component routing should not be broken with single certificate [Disruptive]", func() {
		var (
			dirname  = "/tmp/OCP-75908-ca/"
			validity = 30
			caSubj   = "/CN=NE-Test-Root-CA"
			caCrt    = dirname + "75908-ca.crt"
			caKey    = dirname + "75908-ca.key"
			usrCrt   = dirname + "75908-usr.crt"
			usrKey   = dirname + "75908-usr.key"
			usrCsr   = dirname + "75908-usr.csr"
		)

		exutil.By("1.0: skip for http2 not enabled clusters")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		disableHttp2 := readRouterPodEnv(oc, routerpod, "ROUTER_DISABLE_HTTP2")
		if !strings.Contains(disableHttp2, "false") {
			g.Skip("OCPBUGS-29373 occur on ROSA/OSD cluster, skip for http2 not enabled clusters!")
		}

		exutil.By("2.0: Get some info including hostnames of console/oauth route for the testing")
		appsDomain := "apps." + getBaseDomain(oc)
		consoleRoute := getByJsonPath(oc, "openshift-console", "route/console", "{.spec.host}")
		oauthRoute := getByJsonPath(oc, "openshift-authentication", "route/oauth-openshift", "{.spec.host}")
		defaultRoute := "foo." + appsDomain
		ingressOperatorPod := getPodListByLabel(oc, "openshift-ingress-operator", "name=ingress-operator")[0]
		backupComponentRoutes := getByJsonPath(oc, "default", "ingresses.config.openshift.io/cluster", "{.spec.componentRoutes}")
		defaultComponentRoutes := fmt.Sprintf(`[{"op":"replace", "path":"/spec/componentRoutes", "value":%s}]`, backupComponentRoutes)

		exutil.By("3.0: Use openssl to create the certification and key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("3.1: Create a new self-signed CA including the ca certification and ca key")
		opensslCmd := fmt.Sprintf(`openssl req -x509 -newkey rsa:2048 -days %d -keyout %s -out %s -nodes -subj %s`, validity, caKey, caCrt, caSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("3.2: Create the user CSR and the user key")
		opensslCmd = fmt.Sprintf(`openssl req -newkey rsa:2048 -nodes -keyout %s  -out %s -subj "/CN=*.%s"`, usrKey, usrCsr, appsDomain)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("3.3: Create the user certification")
		opensslCmd = fmt.Sprintf(`openssl x509 -extfile <(printf "subjectAltName = DNS.1:%s,DNS.2:%s") -req -in %s -CA %s -CAkey %s -CAcreateserial -out %s -days %d`, consoleRoute, oauthRoute, usrCsr, caCrt, caKey, usrCrt, validity)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("4.0: Create the custom secret on the cluster with the created user certification and user key")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", "openshift-config", "secret", "custom-cert75908").Output()
		output, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", "openshift-config", "secret", "tls", "custom-cert75908", "--cert="+usrCrt, "--key="+usrKey).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("created"))

		exutil.By("5.0: path the ingress with the console route host and the custom secret")
		patchContent := fmt.Sprintf(`
spec:
  componentRoutes:
  - hostname: %s
    name: console
    namespace: openshift-console
    servingCertKeyPairSecret:
      name: custom-cert75908
`, consoleRoute)
		defer patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", defaultComponentRoutes)
		patchResourceAsAdminAnyType(oc, "default", "ingresses.config.openshift.io/cluster", patchContent, "merge")

		exutil.By("6.0: Check the console route has HTTP/2 enabled")
		output, err = oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", "cat cert_config.map").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`console.pem [alpn h2,http/1.1] ` + consoleRoute))

		exutil.By("7.0: Check console certificate has different SHA1 Fingerprint with OAuth certificate and default certificate, by using openssl command")
		curlCmd := []string{"-n", "openshift-ingress-operator", "-c", "ingress-operator", ingressOperatorPod, "--", "curl", "https://" + consoleRoute + "/headers", "-kI", "--connect-timeout", "10"}
		opensslConnectCmd := fmt.Sprintf(`openssl s_client -connect %s:443 </dev/null 2>/dev/null | openssl x509 -sha1 -in /dev/stdin -noout -fingerprint`, consoleRoute)
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)
		consoleOutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress-operator", "-c", "ingress-operator", ingressOperatorPod, "--", "bash", "-c", opensslConnectCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		curlCmd = []string{"-n", "openshift-ingress-operator", "-c", "ingress-operator", ingressOperatorPod, "--", "curl", "https://" + oauthRoute + "/headers", "-kI", "--connect-timeout", "10"}
		opensslConnectCmd = fmt.Sprintf(`openssl s_client -connect %s:443 </dev/null 2>/dev/null | openssl x509 -sha1 -in /dev/stdin -noout -fingerprint`, oauthRoute)
		repeatCmdOnClient(oc, curlCmd, "403", 30, 1)
		oauthOutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress-operator", "-c", "ingress-operator", ingressOperatorPod, "--", "bash", "-c", opensslConnectCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		curlCmd = []string{"-n", "openshift-ingress-operator", "-c", "ingress-operator", ingressOperatorPod, "--", "curl", "https://" + defaultRoute + "/headers", "-kI", "--connect-timeout", "10"}
		opensslConnectCmd = fmt.Sprintf(`openssl s_client -connect %s:443 </dev/null 2>/dev/null | openssl x509 -sha1 -in /dev/stdin -noout -fingerprint`, defaultRoute)
		repeatCmdOnClient(oc, curlCmd, "503", 30, 1)
		defaultOutput, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress-operator", "-c", "ingress-operator", ingressOperatorPod, "--", "bash", "-c", opensslConnectCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		o.Expect(consoleOutput).NotTo(o.ContainSubstring(oauthOutput))
		o.Expect(oauthOutput).To(o.ContainSubstring(defaultOutput))
	})

	// OCPBUGS-33657(including OCPBUGS-34757, OCPBUGS-34110 and OCPBUGS-34888 in OCP-75909)
	// guest hypershift cluster had not the ingress-operator pod, skipped on it
	g.It("Author:shudili-NonHyperShiftHOST-ROSA-OSD_CCS-ARO-High-75909-Ingress Operator should not always remain in the progressing state [Disruptive]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		nodePortTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		privateTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-private.yaml")
		hostnetworkTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-hostnetwork-only.yaml")

		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount < 1 {
			g.Skip("Skipping as we at least need one Linux worker node")
		}

		// OCPBUGS-34757
		var (
			ingctrl34757one = ingressControllerDescription{
				name:      "bug34757one",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  nodePortTemp,
			}

			ingctrl34757two = ingressControllerDescription{
				name:      "bug34757two",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  privateTemp,
			}
		)

		exutil.By("1. after the cluster is ready, check openshift-ingress-operator logs, which should not contain updated internal service")
		output, err := oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress-operator", "deployment/ingress-operator").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(output, "updated internal service")).NotTo(o.BeTrue())

		exutil.By("2. Create two custom ingresscontrollers for OCPBUGS-34757")
		baseDomain := getBaseDomain(oc)
		ingctrl34757one.domain = ingctrl34757one.name + "." + baseDomain
		ingctrl34757two.domain = ingctrl34757two.name + "." + baseDomain
		defer ingctrl34757one.delete(oc)
		ingctrl34757one.create(oc)
		defer ingctrl34757two.delete(oc)
		ingctrl34757two.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl34757one.name)
		ensureCustomIngressControllerAvailable(oc, ingctrl34757two.name)

		exutil.By("3. Check the logs again after the custom ingresscontrollers are ready, which should not contain updated internal service")
		output, err = oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress-operator", "deployment/ingress-operator").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(output, "updated internal service")).NotTo(o.BeTrue())
		ingctrl34757one.delete(oc)
		ingctrl34757two.delete(oc)

		// OCPBUGS-34110
		exutil.By("4. delete the ingress-operator pod")
		_, err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", "openshift-ingress-operator", "pods", "-l", "name=ingress-operator").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, "openshift-ingress-operator", "name=ingress-operator")
		ensureClusterOperatorNormal(oc, "ingress", 1, 120)
		output, err = oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress-operator", "deployment/ingress-operator").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(output, "updated IngressClass")).NotTo(o.BeTrue())

		// OCPBUGS-34888
		var (
			ingctrlhp34888one = ingctrlHostPortDescription{
				name:      "bug34888one",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  10080,
				httpsport: 10443,
				statsport: 10936,
				template:  hostnetworkTemp,
			}

			ingctrlhp34888two = ingctrlHostPortDescription{
				name:      "bug34888two",
				namespace: "openshift-ingress-operator",
				domain:    "",
				httpport:  11080,
				httpsport: 11443,
				statsport: 11936,
				template:  hostnetworkTemp,
			}
		)

		exutil.By("5. Create two custom ingresscontrollers for OCPBUGS-34888")
		ingctrlhp34888one.domain = ingctrlhp34888one.name + "." + baseDomain
		ingctrlhp34888two.domain = ingctrlhp34888two.name + "." + baseDomain

		defer ingctrlhp34888one.delete(oc)
		ingctrlhp34888one.create(oc)
		defer ingctrlhp34888two.delete(oc)
		ingctrlhp34888two.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrlhp34888one.name)
		ensureCustomIngressControllerAvailable(oc, ingctrlhp34888two.name)

		exutil.By("6. Check there was not the updated router deployment log")
		output, err = oc.AsAdmin().WithoutNamespace().Run("logs").Args("-n", "openshift-ingress-operator", "-c", "ingress-operator", "deployment/ingress-operator").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).NotTo(o.ContainSubstring("updated router deployment"))
	})

	// author: shudili@redhat.com
	// [OCPBUGS-42480](https://issues.redhat.com/browse/OCPBUGS-42480)
	// [OCPBUGS-43063](https://issues.redhat.com/browse/OCPBUGS-43063)
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-77283-Router should support SHA1 CA certificates in the default certificate chain", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			baseTemp            = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "77283",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  "",
			}

			dirname        = "/tmp/OCP-77283-ca/"
			validity       = 30
			caSubj         = "/C=US/ST=SC/L=Default City/O=Default Company Ltd/OU=Test CA/CN=www.exampleca.com/emailAddress=example@example.com"
			caCrt          = dirname + "77283-ca.crt"
			caKey          = dirname + "77283-ca.key"
			usrSubj        = "/CN=www.example.com/ST=SC/C=US/emailAddress=example@example.com/O=Example/OU=Example"
			usrCrt         = dirname + "77283-usr.crt"
			usrKey         = dirname + "77283-usr.key"
			usrCsr         = dirname + "77283-usr.csr"
			ext            = dirname + "77283-extfile"
			combinationCrt = dirname + "77283-combo.crt"
		)

		exutil.By("1.0: Use openssl to create the certification and key")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		output := getByJsonPath(oc, "default", "ingresses.config/cluster", "{.spec.domain}")
		wildcard := "*." + output

		exutil.By("1.1: Create a new self-signed sha1 root CA including the ca certification and ca key")
		opensslCmd := fmt.Sprintf(`openssl req -x509 -sha1 -newkey rsa:2048 -days %d -keyout %s -out %s -nodes -subj '%s'`, validity, caKey, caCrt, caSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		if err != nil {
			// The CI OpenSSL 3.0.7 1 Nov 2022 (Library: OpenSSL 3.0.7 1 Nov 2022) under Red Hat Enterprise Linux release doesn't support sha1 certification, skip this case if the error occur
			g.Skip("Skipping as openssl under the OS doesn't support sha1 certification")
		}

		exutil.By("1.2: Create the user CSR and the user key")
		opensslCmd = fmt.Sprintf(`openssl req -newkey rsa:2048 -nodes -keyout %s  -out %s -subj %s`, usrKey, usrCsr, usrSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("1.3: Create the extension file, then create the user certification")
		cmd := fmt.Sprintf(`echo $'[ext]\nbasicConstraints = CA:FALSE\nsubjectKeyIdentifier = none\nauthorityKeyIdentifier = none\nextendedKeyUsage=serverAuth,clientAuth\nkeyUsage=nonRepudiation, digitalSignature, keyEncipherment\nsubjectAltName = DNS:'%s > %s`, ext, wildcard)
		_, err = exec.Command("bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		opensslCmd = fmt.Sprintf(`openssl x509 -req -days %d -sha256 -in %s -CA %s -CAcreateserial -CAkey %s -extensions ext -out %s`, validity, usrCsr, caCrt, caKey, usrCrt)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("1.4: create the file including the sha1 certification and user certification")
		catCmd := fmt.Sprintf(`cat %s %s > %s`, usrCrt, caCrt, combinationCrt)
		_, err = exec.Command("bash", "-c", catCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("2.0: Create the custom secret on the cluster with the created the combination certifications and user key")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", "openshift-ingress", "secret", "custom-cert77283").Output()
		output, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", "openshift-ingress", "secret", "tls", "custom-cert77283", "--cert="+combinationCrt, "--key="+usrKey).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("created"))

		exutil.By("3.0: Create the custom ingresscontroller for the testing")
		extraParas := fmt.Sprintf(`
    defaultCertificate:
      name: custom-cert77283
`)
		customTemp := addExtraParametersToYamlFile(baseTemp, "spec:", extraParas)
		defer os.Remove(customTemp)
		ingctrl.template = customTemp
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		exutil.By("4.0: Check the ingress co, it should be upgradable")
		jsonPath := `{.status.conditions[?(@.type=="Upgradeable")].status}`
		status := getByJsonPath(oc, "default", "co/ingress", jsonPath)
		o.Expect(status).To(o.ContainSubstring("True"))

		exutil.By("5.0: The canary route is accessable")
		jsonPath = "{.status.ingress[0].host}"
		routehost := getByJsonPath(oc, "openshift-ingress-canary", "route/canary", jsonPath)
		waitForOutsideCurlContains("https://"+routehost, "-kI", `200`)
	})

	// https://issues.redhat.com/browse/OCPBUGS-66885
	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-86155-Supporting ClosedClientConnectionPolicy in the IngressController", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp86155",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrlResource = "ingresscontrollers/" + ingctrl.name
		)

		exutil.By("1.0: Create an custom ingresscontroller for the testing")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("2.0: Check the default closedClientConnectionPolicy in the ingress-controller which should be Continue")
		policy := getByJsonPath(oc, ingctrl.namespace, ingctrlResource, "{.spec.closedClientConnectionPolicy}")
		o.Expect(policy).To(o.ContainSubstring("Continue"))

		exutil.By("2.1: Check in the haproxy.conf there is NOT option abortonclose")
		podname := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		defaultsCfg := getBlockConfig(oc, podname, "defaults")
		o.Expect(defaultsCfg).NotTo(o.ContainSubstring("abortonclose"))

		exutil.By("2.2: Check in the haproxy-config.template whether or not there is option abortonclose")
		output, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", podname, "--", "bash", "-c", "cat haproxy-config.template | grep abortonclose").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("option abortonclose"))

		exutil.By("3.0: Patch the ingress-controller with Abort closedClientConnectionPolicy")
		patchResourceAsAdmin(oc, ingctrl.namespace, ingctrlResource, `{"spec":{"closedClientConnectionPolicy":"Abort"}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		exutil.By("3.1: Check the spec of the ingress-controller, closedClientConnectionPolicy should be Abort")
		policy = getByJsonPath(oc, ingctrl.namespace, ingctrlResource, "{.spec.closedClientConnectionPolicy}")
		o.Expect(policy).To(o.ContainSubstring("Abort"))

		exutil.By("3.2: Check in the haproxy.config whether or not there is option abortonclose")
		podname = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		ensureHaproxyBlockConfigContains(oc, podname, "defaults", []string{"option abortonclose"})

		exutil.By("3.3: Check ROUTER_ABORT_ON_CLOSE env in a route pod which should be true")
		policyEnv := readRouterPodEnv(oc, podname, "ROUTER_ABORT_ON_CLOSE")
		o.Expect(policyEnv).To(o.ContainSubstring("ROUTER_ABORT_ON_CLOSE=true"))
	})
})
