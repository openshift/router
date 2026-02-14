package router

import (
	"github.com/openshift/router/test/e2e/testdata"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
	e2e "k8s.io/kubernetes/test/e2e/framework"
	netutils "k8s.io/utils/net"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_DNS", func() {
	defer g.GinkgoRecover()
	var oc = exutil.NewCLI("coredns", exutil.KubeConfigPath())

	// author: shudili@redhat.com
	g.It("Author:shudili-High-39842-CoreDNS supports dual stack ClusterIP Services for OCP4.8 or higher", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-v4v6rc.yaml")
			unsecsvcName        = "service-unsecurev4v6"
			secsvcName          = "service-securev4v6"
		)

		exutil.By("check the IP stack tpye, skip for non-dualstack platform")
		ipStackType := checkIPStackType(oc)
		e2e.Logf("the cluster IP stack type is: %v", ipStackType)
		if ipStackType != "dualstack" {
			g.Skip("Skip for non-dualstack platform")
		}

		exutil.By("Create a backend pod and its services resources")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-v4v6rc")
		srvPod := getPodListByLabel(oc, ns, "name=web-server-v4v6rc")[0]

		exutil.By("check the services v4v6 addresses")
		IPAddresses := getByJsonPath(oc, ns, "service/"+unsecsvcName, "{.spec.clusterIPs}")
		o.Expect(IPAddresses).To(o.MatchRegexp(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`))
		o.Expect(strings.Count(IPAddresses, ":") >= 2).To(o.BeTrue())

		IPAddresses = getByJsonPath(oc, ns, "service/"+secsvcName, "{.spec.clusterIPs}")
		o.Expect(IPAddresses).To(o.MatchRegexp(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`))
		o.Expect(strings.Count(IPAddresses, ":") >= 2).To(o.BeTrue())

		exutil.By("check the services names can be resolved to their v4v6 addresses")
		IPAddress1 := getByJsonPath(oc, ns, "service/"+unsecsvcName, "{.spec.clusterIPs[0]}")
		IPAddress2 := getByJsonPath(oc, ns, "service/"+unsecsvcName, "{.spec.clusterIPs[1]}")
		cmdOnPod := []string{"-n", ns, srvPod, "--", "getent", "ahosts", unsecsvcName}
		repeatCmdOnClient(oc, cmdOnPod, IPAddress1, 30, 1)
		repeatCmdOnClient(oc, cmdOnPod, IPAddress2, 30, 1)

		IPAddress1 = getByJsonPath(oc, ns, "service/"+secsvcName, "{.spec.clusterIPs[0]}")
		IPAddress2 = getByJsonPath(oc, ns, "service/"+secsvcName, "{.spec.clusterIPs[1]}")
		cmdOnPod = []string{"-n", ns, srvPod, "--", "getent", "ahosts", secsvcName}
		repeatCmdOnClient(oc, cmdOnPod, IPAddress1, 30, 1)
		repeatCmdOnClient(oc, cmdOnPod, IPAddress2, 30, 1)
	})

	// incorporate OCP-56047 and OCP-40718 into one
	// Test case creater: shudili@redhat.com - OCP-56047-Set CoreDNS cache entries for forwarded zones
	// Test case creater: jechen@redhat.com - OCP-40718-CoreDNS cache should use 900s for positive responses and 30s for negative responses
	g.It("Author:shudili-Critical-40718-CoreDNS cache should use 900s for positive responses and 30s for negative responses [Disruptive]", func() {
		exutil.By("Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		// OCP-40718
		exutil.By("1. Check the cache entries of the default corefiles in CoreDNS")
		zoneInCoreFile1 := pollReadDnsCorefile(oc, oneDnsPod, ".:5353", "-A20", "cache 900")
		o.Expect(zoneInCoreFile1).Should(o.And(
			o.ContainSubstring("cache 900"),
			o.ContainSubstring("denial 9984 30")))

		// OCP-56047
		// bug: 2006803
		exutil.By("2. Patch the dns.operator/default and add a custom forward zone config")
		resourceName := "dns.operator.openshift.io/default"
		jsonPatch := "[{\"op\":\"add\", \"path\":\"/spec/servers\", \"value\":[{\"forwardPlugin\":{\"policy\":\"Random\",\"upstreams\":[\"8.8.8.8\"]},\"name\":\"test\",\"zones\":[\"mytest.ocp\"]}]}]"
		patchGlobalResourceAsAdmin(oc, resourceName, jsonPatch)

		exutil.By("3. Check the cache entries of the custom forward zone in CoreDNS")
		zoneInCoreFile := pollReadDnsCorefile(oc, oneDnsPod, "mytest.ocp", "-A15", "cache 900")
		o.Expect(zoneInCoreFile).Should(o.And(
			o.ContainSubstring("cache 900"),
			o.ContainSubstring("denial 9984 30")))
	})

	// Bug: 1916907
	g.It("Author:mjoseph-High-40867-Deleting the internal registry should not corrupt /etc/hosts [Disruptive]", func() {
		exutil.By("Step1: Get the Cluster IP of image-registry")
		// Skip the test case if openshift-image-registry namespace is not found
		clusterIP, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(
			"service", "image-registry", "-n", "openshift-image-registry", "-o=jsonpath={.spec.clusterIP}").Output()
		if err != nil || strings.Contains(clusterIP, `namespaces \"openshift-image-registry\" not found`) {
			g.Skip("Skip for non-supported platform")
		}
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Step2: SSH to the node and confirm the /etc/hosts have the same clusterIP")
		allNodeList, _ := exutil.GetAllNodes(oc)
		// get a random node
		node := getRandomElementFromList(allNodeList)
		hostOutput, err := exutil.DebugNodeWithChroot(oc, node, "cat", "/etc/hosts")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(hostOutput).To(o.And(
			o.ContainSubstring("127.0.0.1   localhost localhost.localdomain localhost4 localhost4.localdomain4"),
			o.ContainSubstring("::1         localhost localhost.localdomain localhost6 localhost6.localdomain6"),
			o.ContainSubstring(clusterIP+" image-registry.openshift-image-registry.svc image-registry.openshift-image-registry.svc.cluster.local")))
		o.Expect(hostOutput).NotTo(o.And(o.ContainSubstring("error"), o.ContainSubstring("failed"), o.ContainSubstring("timed out")))

		// Set status variables
		expectedStatus := map[string]string{"Available": "True", "Progressing": "False", "Degraded": "False"}

		exutil.By("Step3: Delete the image-registry svc and check whether it receives a new Cluster IP")
		err1 := oc.AsAdmin().WithoutNamespace().Run("delete").Args("svc", "image-registry", "-n", "openshift-image-registry").Execute()
		o.Expect(err1).NotTo(o.HaveOccurred())
		err = waitCoBecomes(oc, "image-registry", 240, expectedStatus)
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Step4: Get the new Cluster IP of image-registry")
		newClusterIP, err2 := oc.AsAdmin().WithoutNamespace().Run("get").Args(
			"service", "image-registry", "-n", "openshift-image-registry", "-o=jsonpath={.spec.clusterIP}").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
		o.Expect(newClusterIP).NotTo(o.ContainSubstring(clusterIP))
		e2e.Logf("The new cluster IP is %v", newClusterIP)

		exutil.By("Step5: SSH to the node and confirm the /etc/hosts details, after deletion")
		cmdList := []string{"cat", "/etc/hosts"}
		expectedString := fmt.Sprintf(`%s image-registry.openshift-image-registry.svc image-registry.openshift-image-registry.svc.cluster.local # openshift-generated-node-resolver`, newClusterIP)
		waitForDebugNodeOutputContains(oc, "default", node, cmdList, expectedString, 90*time.Second)
	})

	// incorporate OCP-40717 into existing OCP-46867
	// Test case creater: jechen@redhat.com - OCP-40717-Hostname lookup does not delay when master node dow
	// Test case creater: shudili@redhat.com - OCP-46867-Configure upstream resolvers for CoreDNS flag
	g.It("Author:shudili-Critical-46867-Configure upstream resolvers for CoreDNS flag [Disruptive]", func() {
		var (
			resourceName        = "dns.operator.openshift.io/default"
			cfgMulIPv4Upstreams = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[" +
				"{\"address\":\"10.100.1.11\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"10.100.1.12\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"10.100.1.13\",\"port\":5353,\"type\":\"Network\"}]}]"
			expMulIPv4Upstreams = "forward . 10.100.1.11:53 10.100.1.12:53 10.100.1.13:5353"
			cfgOneIPv4Upstreams = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[" +
				"{\"address\":\"20.100.1.11\",\"port\":53,\"type\":\"Network\"}]}]"
			expOneIPv4Upstreams = "forward . 20.100.1.11:53"
			cfgMax15Upstreams   = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[" +
				"{\"address\":\"30.100.1.11\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.12\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.13\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.14\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.15\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.16\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.17\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.18\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.19\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.20\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.21\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.22\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.23\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.24\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.25\",\"port\":53,\"type\":\"Network\"}]}]"
			expMax15Upstreams = "forward . 30.100.1.11:53 30.100.1.12:53 30.100.1.13:53 30.100.1.14:53 30.100.1.15:53 " +
				"30.100.1.16:53 30.100.1.17:53 30.100.1.18:53 30.100.1.19:53 30.100.1.20:53 " +
				"30.100.1.21:53 30.100.1.22:53 30.100.1.23:53 30.100.1.24:53 30.100.1.25:53"
			cfgMulIPv6Upstreams = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[" +
				"{\"address\":\"1001::aaaa\",\"port\":5353,\"type\":\"Network\"}, " +
				"{\"address\":\"1001::BBBB\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"1001::cccc\",\"port\":53,\"type\":\"Network\"}]}]"
			expMulIPv6Upstreams = "forward . [1001::AAAA]:5353 [1001::BBBB]:53 [1001::CCCC]:53"
		)
		exutil.By("Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		// OCP-40717
		exutil.By("Check the readiness probe period and timeout parameters are both set to 3 seconds")
		output, err := oc.AsAdmin().Run("get").Args("pod/"+oneDnsPod, "-n", "openshift-dns", "-o=jsonpath={.spec.containers[0].readinessProbe.periodSeconds}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`3`))
		output1, err1 := oc.AsAdmin().Run("get").Args("pod/"+oneDnsPod, "-n", "openshift-dns", "-o=jsonpath={.spec.containers[0].readinessProbe.timeoutSeconds}").Output()
		o.Expect(err1).NotTo(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring(`3`))

		// OCP-46867
		exutil.By("Check default values of forward upstream resolvers for CoreDNS")
		upstreams := pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", "resolv.conf")
		o.Expect(upstreams).To(o.ContainSubstring("forward . /etc/resolv.conf"))

		exutil.By("Patch dns operator with multiple ipv4 upstreams")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgMulIPv4Upstreams)

		exutil.By("Check multiple ipv4 forward upstream resolvers in CoreDNS")
		upstreams = pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", expMulIPv4Upstreams)
		o.Expect(upstreams).To(o.ContainSubstring(expMulIPv4Upstreams))

		exutil.By("Patch dns operator with a single ipv4 upstream, and then check the single ipv4 forward upstream resolver for CoreDNS")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgOneIPv4Upstreams)
		upstreams = pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", expOneIPv4Upstreams)
		o.Expect(upstreams).To(o.ContainSubstring(expOneIPv4Upstreams))

		exutil.By("Patch dns operator with max 15 ipv4 upstreams, and then the max 15 ipv4 forward upstream resolvers for CoreDNS")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgMax15Upstreams)
		upstreams = pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", expMax15Upstreams)
		o.Expect(upstreams).To(o.ContainSubstring(expMax15Upstreams))

		exutil.By("Patch dns operator with multiple ipv6 upstreams, and then check the multiple ipv6 forward upstream resolvers for CoreDNS")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgMulIPv6Upstreams)
		upstreams = pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", "1001")
		o.Expect(upstreams).To(o.ContainSubstring(expMulIPv6Upstreams))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Critical-46868-Configure forward policy for CoreDNS flag [Disruptive]", func() {
		var (
			resourceName        = "dns.operator.openshift.io/default"
			cfgMulIPv4Upstreams = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[" +
				"{\"address\":\"10.100.1.11\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"10.100.1.12\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"10.100.1.13\",\"port\":5353,\"type\":\"Network\"}]}]"
			cfgPolicyRandom = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/policy\", \"value\":\"Random\"}]"
			cfgPolicyRr     = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/policy\", \"value\":\"RoundRobin\"}]"
			cfgPolicySeq    = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/policy\", \"value\":\"Sequential\"}]"
		)
		exutil.By("Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("Check default values of forward policy for CoreDNS")
		policy := pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", "policy sequential")
		o.Expect(policy).To(o.ContainSubstring("policy sequential"))

		exutil.By("Patch dns operator with multiple ipv4 upstreams, and check multiple ipv4 forward upstreams in CoreDNS")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgMulIPv4Upstreams)
		upstreams := pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", "10.100.1.11")
		o.Expect(upstreams).To(o.ContainSubstring("forward . 10.100.1.11:53 10.100.1.12:53 10.100.1.13:5353"))

		exutil.By("Check default forward policy in CoreDNS after multiple ipv4 forward upstreams are configured")
		o.Expect(upstreams).To(o.ContainSubstring("policy sequential"))

		exutil.By("Patch dns operator with policy random for upstream resolvers, and then check forward policy random in Corefile of coredns")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgPolicyRandom)
		policy = pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", "policy random")
		o.Expect(policy).To(o.ContainSubstring("policy random"))

		exutil.By("Patch dns operator with policy roundrobin for upstream resolvers, and then check forward policy roundrobin in Corefile of coredns")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgPolicyRr)
		policy = pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", "policy round_robin")
		o.Expect(policy).To(o.ContainSubstring("policy round_robin"))

		exutil.By("Patch dns operator with policy sequential for upstream resolvers, and then check forward policy sequential in Corefile of coredns")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgPolicySeq)
		policy = pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", "policy sequential")
		o.Expect(policy).To(o.ContainSubstring("policy sequential"))
	})

	g.It("Author:shudili-Medium-46869-Negative test of configuring upstream resolvers and policy flag [Disruptive]", func() {
		var (
			resourceName       = "dns.operator.openshift.io/default"
			cfgAddOneUpstreams = "[{\"op\":\"add\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[" +
				"{\"address\":\"30.100.1.11\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.12\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.13\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.14\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.15\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.16\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.17\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.18\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.19\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.20\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.21\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.22\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.23\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.24\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.25\",\"port\":53,\"type\":\"Network\"}, " +
				"{\"address\":\"30.100.1.26\",\"port\":53,\"type\":\"Network\"}]}]"
			invalidCfgStringUpstreams = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[" +
				"{\"address\":\"str_test\",\"port\":53,\"type\":\"Network\"}]}]"
			invalidCfgNumberUpstreams = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[" +
				"{\"address\":\"100\",\"port\":53,\"type\":\"Network\"}]}]"
			invalidCfgSringPolicy  = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/policy\", \"value\":\"string_test\"}]"
			invalidCfgNumberPolicy = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/policy\", \"value\":\"2\"}]"
			invalidCfgRandomPolicy = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/policy\", \"value\":\"random\"}]"
		)
		exutil.By("Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		forceOnlyOneDnsPodExist(oc)

		exutil.By("Try to add one more upstream resolver, totally 16 upstream resolvers by patching dns operator")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+cfgAddOneUpstreams, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("have at most 15 items"))

		exutil.By("Try to add a upstream resolver with a string as an address")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgStringUpstreams, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"str_test\""))

		exutil.By("Try to add a upstream resolver with a number as an address")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgNumberUpstreams, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Invalid value: \"100\""))

		exutil.By("Try to configure the polciy with a string")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgSringPolicy, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"string_test\""))

		exutil.By("Try to configure the polciy with a number")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgNumberPolicy, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"2\""))

		exutil.By("Try to configure the polciy with a similar string like random")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgRandomPolicy, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"random\""))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-Critical-46872-Configure logLevel for CoreDNS under DNS operator flag [Disruptive]", func() {
		var (
			resourceName     = "dns.operator.openshift.io/default"
			cfgLogLevelDebug = "[{\"op\":\"replace\", \"path\":\"/spec/logLevel\", \"value\":\"Debug\"}]"
			cfgLogLevelTrace = "[{\"op\":\"replace\", \"path\":\"/spec/logLevel\", \"value\":\"Trace\"}]"
		)
		exutil.By("Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("Check default log level of CoreDNS")
		logOutput := pollReadDnsCorefile(oc, oneDnsPod, "log", "-A2", "class error")
		o.Expect(logOutput).To(o.ContainSubstring("class error"))

		exutil.By("Patch dns operator with logLevel Debug for CoreDNS, and then check log class for logLevel Debug in both CM and the Corefile of coredns")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgLogLevelDebug)
		logOutput = pollReadDnsCorefile(oc, oneDnsPod, "log", "-A2", "class denial error")
		o.Expect(logOutput).To(o.ContainSubstring("class denial error"))

		exutil.By("Patch dns operator with logLevel Trace for CoreDNS, and then check log class for logLevel Trace in Corefile of coredns")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgLogLevelTrace)
		logOutput = pollReadDnsCorefile(oc, oneDnsPod, "log", "-A2", "class all")
		o.Expect(logOutput).To(o.ContainSubstring("class all"))
	})

	g.It("Author:shudili-Medium-46874-negative test for configuring logLevel and operatorLogLevel flag [Disruptive]", func() {
		var (
			resourceName               = "dns.operator.openshift.io/default"
			invalidCfgStringLogLevel   = "[{\"op\":\"replace\", \"path\":\"/spec/logLevel\", \"value\":\"string_test\"}]"
			invalidCfgNumberLogLevel   = "[{\"op\":\"replace\", \"path\":\"/spec/logLevel\", \"value\":\"2\"}]"
			invalidCfgTraceLogLevel    = "[{\"op\":\"replace\", \"path\":\"/spec/logLevel\", \"value\":\"trace\"}]"
			invalidCfgStringOPLogLevel = "[{\"op\":\"replace\", \"path\":\"/spec/operatorLogLevel\", \"value\":\"string_test\"}]"
			invalidCfgNumberOPLogLevel = "[{\"op\":\"replace\", \"path\":\"/spec/operatorLogLevel\", \"value\":\"2\"}]"
			invalidCfgTraceOPLogLevel  = "[{\"op\":\"replace\", \"path\":\"/spec/operatorLogLevel\", \"value\":\"trace\"}]"
		)
		exutil.By("Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		forceOnlyOneDnsPodExist(oc)

		exutil.By("Try to configure log level with a string")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgStringLogLevel, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"string_test\""))

		exutil.By("Try to configure log level with a number")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgNumberLogLevel, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"2\""))

		exutil.By("Try to configure log level with a similar string like trace")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgTraceLogLevel, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"trace\""))

		exutil.By("Try to configure dns operator log level with a string")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgStringOPLogLevel, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"string_test\""))

		exutil.By("Try to configure dns operator log level with a number")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgNumberOPLogLevel, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"2\""))

		exutil.By("Try to configure dns operator log level with a similar string like trace")
		output, _ = oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+invalidCfgTraceOPLogLevel, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("Unsupported value: \"trace\""))
	})

	g.It("Author:shudili-Low-46875-Different LogLevel logging function of CoreDNS flag [Disruptive]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodName       = "hello-pod"
			clientPodLabel      = "app=hello-pod"
			coreDNSSrvPod       = filepath.Join(buildPruningBaseDir, "coreDNS-pod.yaml")
			srvPodName          = "test-coredns"
			srvPodLabel         = "name=test-coredns"
			failedDNSReq        = "failed.not-myocp-test.com"
			nxDNSReq            = "notexist.myocp-test.com"
			normalDNSReq        = "www.myocp-test.com"
			resourceName        = "dns.operator.openshift.io/default"
			cfgDebug            = "[{\"op\":\"replace\", \"path\":\"/spec/logLevel\", \"value\":\"Debug\"}]"
			cfgTrace            = "[{\"op\":\"replace\", \"path\":\"/spec/logLevel\", \"value\":\"Trace\"}]"
		)
		exutil.By("Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)
		podList := []string{oneDnsPod}

		exutil.By("Create a dns server pod")
		ns := oc.Namespace()
		defer exutil.RecoverNamespaceRestricted(oc, ns)
		exutil.SetNamespacePrivileged(oc, ns)
		replaceCoreDnsImage(oc, coreDNSSrvPod)
		err := oc.AsAdmin().Run("create").Args("-f", coreDNSSrvPod, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, srvPodLabel)

		exutil.By("get the user's dns server pod's IP")
		srvPodIP := getPodv4Address(oc, srvPodName, ns)

		exutil.By("patch upstream dns resolver with the user's dns server, and then wait the corefile is updated")
		dnsUpstreamResolver := "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/upstreams\", \"value\":[{\"address\":\"" + srvPodIP + "\",\"port\":53,\"type\":\"Network\"}]}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsUpstreamResolver)
		// Converting the IPV6 address to upper case for searching in the coreDNS file
		if strings.Count(srvPodIP, ":") >= 2 {
			srvPodIP = fmt.Sprintf("%s", strings.ToUpper(srvPodIP))
			srvPodIP = "[" + srvPodIP + "]"
		}
		pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", srvPodIP)

		exutil.By("create a client pod")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		exutil.By("Let client send out SERVFAIL nslookup to the dns server, and check the desired SERVFAIL logs from a coredns pod")
		output := nslookupsAndWaitForDNSlog(oc, clientPodName, failedDNSReq, podList, failedDNSReq+".")
		o.Expect(output).To(o.ContainSubstring(failedDNSReq))

		exutil.By("Patch dns operator with logLevel Debug for CoreDNS, and wait the Corefile is updated")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgDebug)
		pollReadDnsCorefile(oc, oneDnsPod, "log", "-A2", "class denial error")

		exutil.By("Let client send out NXDOMAIN nslookup to the dns server, and check the desired NXDOMAIN logs from a coredns pod")
		output = nslookupsAndWaitForDNSlog(oc, clientPodName, nxDNSReq, podList, "-type=mx", nxDNSReq+".")
		o.Expect(output).To(o.ContainSubstring(nxDNSReq))

		exutil.By("Patch dns operator with logLevel Trace for CoreDNS, and wait the Corefile is updated")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgTrace)
		pollReadDnsCorefile(oc, oneDnsPod, "log", "-A2", "class all")

		exutil.By("Let client send out normal nslookup which will get correct response, and check the desired TRACE logs from a coredns pod")
		output = nslookupsAndWaitForDNSlog(oc, clientPodName, normalDNSReq, podList, normalDNSReq+".")
		o.Expect(output).To(o.ContainSubstring(normalDNSReq))
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-Critical-51536-Support CoreDNS forwarding DNS requests over TLS using ForwardPlugin [Disruptive]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			cmFile              = filepath.Join(buildPruningBaseDir, "ca-bundle.pem")
			coreDNSSrvPod       = filepath.Join(buildPruningBaseDir, "coreDNS-pod.yaml")
			srvPodName          = "test-coredns"
			srvPodLabel         = "name=test-coredns"
			resourceName        = "dns.operator.openshift.io/default"
		)

		exutil.By("1.Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("2.Create a dns server pod")
		ns := oc.Namespace()
		defer exutil.RecoverNamespaceRestricted(oc, ns)
		exutil.SetNamespacePrivileged(oc, ns)
		replaceCoreDnsImage(oc, coreDNSSrvPod)
		err := oc.AsAdmin().Run("create").Args("-f", coreDNSSrvPod, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, srvPodLabel)

		exutil.By("3.Get the user's dns server pod's IP")
		srvPodIP := getPodv4Address(oc, srvPodName, ns)

		exutil.By("4.Create configmap client-ca-xxxxx in namespace openshift-config")
		defer deleteConfigMap(oc, "openshift-config", "ca-51536-bundle")
		createConfigMapFromFile(oc, "openshift-config", "ca-51536-bundle", cmFile)

		exutil.By("5.Patch the dns.operator/default with transport option as TLS for forwardplugin")
		dnsForwardPlugin := "[{\"op\":\"replace\", \"path\":\"/spec\", \"value\":{\"servers\":[{\"forwardPlugin\":{\"policy\":\"Sequential\",\"transportConfig\": {\"tls\":{\"caBundle\": {\"name\": \"ca-51536-bundle\"}, \"serverName\": \"dns.ocp51536.ocp\"}, \"transport\": \"TLS\"}, \"upstreams\":[\"" + srvPodIP + "\"]}, \"name\": \"test\", \"zones\":[\"ocp51536.ocp\"]}]}}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsForwardPlugin)

		exutil.By("6.Check and confirm the upstream resolver's IP(srvPodIP) and custom CAbundle name appearing in the dns pod")
		forward := pollReadDnsCorefile(oc, oneDnsPod, srvPodIP, "-b6", "ocp51536")
		o.Expect(forward).To(o.ContainSubstring("ocp51536.ocp:5353"))
		o.Expect(forward).To(o.ContainSubstring("forward . tls://" + srvPodIP))
		o.Expect(forward).To(o.ContainSubstring("tls_servername dns.ocp51536.ocp"))
		o.Expect(forward).To(o.ContainSubstring("tls /etc/pki/dns.ocp51536.ocp-ca-ca-51536-bundle"))

		exutil.By("7.Check no error logs from dns operator pod")
		dnsOperatorPodName := getPodListByLabel(oc, "openshift-dns-operator", "name=dns-operator")
		podLogs, errLogs := exutil.GetSpecificPodLogs(oc, "openshift-dns-operator", "dns-operator", dnsOperatorPodName[0], `ocp51536.ocp:5353 -A3`)
		o.Expect(errLogs).NotTo(o.HaveOccurred(), "Error in getting logs from the pod")
		o.Expect(podLogs).To(o.ContainSubstring(`msg="reconciling request: /default"`))
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-Low-51857-Support CoreDNS forwarding DNS requests over TLS - non existing CA bundle [Disruptive]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			coreDNSSrvPod       = filepath.Join(buildPruningBaseDir, "coreDNS-pod.yaml")
			srvPodName          = "test-coredns"
			srvPodLabel         = "name=test-coredns"
			resourceName        = "dns.operator.openshift.io/default"
		)

		exutil.By("1.Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("2.Create a dns server pod")
		ns := oc.Namespace()
		defer exutil.RecoverNamespaceRestricted(oc, ns)
		exutil.SetNamespacePrivileged(oc, ns)
		replaceCoreDnsImage(oc, coreDNSSrvPod)
		err := oc.AsAdmin().Run("create").Args("-f", coreDNSSrvPod, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, srvPodLabel)

		exutil.By("3.Get the user's dns server pod's IP")
		srvPodIP := getPodv4Address(oc, srvPodName, ns)

		exutil.By("4.Patch the dns.operator/default with non existing CA bundle for forwardplugin")
		dnsForwardPlugin := "[{\"op\":\"replace\", \"path\":\"/spec\", \"value\":{\"servers\":[{\"forwardPlugin\":{\"policy\":\"Sequential\",\"transportConfig\": {\"tls\":{\"caBundle\": {\"name\": \"ca-51857-bundle\"}, \"serverName\": \"dns.ocp51857.ocp\"}, \"transport\": \"TLS\"}, \"upstreams\":[\"" + srvPodIP + "\"]}, \"name\": \"test\", \"zones\":[\"ocp51857.ocp\"]}]}}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsForwardPlugin)

		exutil.By("5.Check and confirm the upstream resolver's IP(srvPodIP) appearing without the custom CAbundle name")
		forward := pollReadDnsCorefile(oc, oneDnsPod, srvPodIP, "-b6", "ocp51857")
		o.Expect(forward).To(o.ContainSubstring("ocp51857.ocp:5353"))
		o.Expect(forward).To(o.ContainSubstring("forward . tls://" + srvPodIP))
		o.Expect(forward).To(o.ContainSubstring("tls_servername dns.ocp51857.ocp"))
		o.Expect(forward).To(o.ContainSubstring("tls"))
		o.Expect(forward).NotTo(o.ContainSubstring("/etc/pki/dns.ocp51857.ocp-ca-ca-51857-bundle"))

		exutil.By("6.Check and confirm the non configured CABundle warning message from dns operator pod")
		dnsOperatorPodName := getPodListByLabel(oc, "openshift-dns-operator", "name=dns-operator")
		podLogs1, errLogs := exutil.GetSpecificPodLogs(oc, "openshift-dns-operator", "dns-operator", dnsOperatorPodName[0], `ocp51857.ocp:5353 -A3`)
		o.Expect(errLogs).NotTo(o.HaveOccurred(), "Error in getting logs from the pod")
		o.Expect(podLogs1).To(o.ContainSubstring(`level=warning msg="source ca bundle configmap ca-51857-bundle does not exist"`))
		o.Expect(podLogs1).To(o.ContainSubstring(`level=warning msg="failed to get destination ca bundle configmap ca-ca-51857-bundle: configmaps \"ca-ca-51857-bundle\" not found"`))
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-Critical-51946-Support CoreDNS forwarding DNS requests over TLS using UpstreamResolvers [Disruptive]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			coreDNSSrvPod       = filepath.Join(buildPruningBaseDir, "coreDNS-pod.yaml")
			srvPodName          = "test-coredns"
			srvPodLabel         = "name=test-coredns"
			resourceName        = "dns.operator.openshift.io/default"
			dirname             = "/tmp/OCP-51946-ca/"
			caKey               = dirname + "ca.key"
			caCert              = dirname + "ca-bundle.crt"
			caSubj              = "/CN=NE-Test-Root-CA"
			dnsPodLabel         = "dns.operator.openshift.io/daemonset-dns=default"
		)

		exutil.By("1.Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("2.Create a dns server pod")
		ns := oc.Namespace()
		defer exutil.RecoverNamespaceRestricted(oc, ns)
		exutil.SetNamespacePrivileged(oc, ns)
		replaceCoreDnsImage(oc, coreDNSSrvPod)
		err := oc.AsAdmin().Run("create").Args("-f", coreDNSSrvPod, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, srvPodLabel)
		srvPodIP := getPodv4Address(oc, srvPodName, ns)

		exutil.By("3.Generate a new self-signed CA")
		defer os.RemoveAll(dirname)
		err = os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("Generate the CA private key")
		opensslCmd := fmt.Sprintf(`openssl genrsa -out %s 4096`, caKey)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		e2e.Logf("Create the CA certificate")
		opensslCmd = fmt.Sprintf(`openssl req -x509 -new -nodes -key %s -sha256 -days 1 -out %s -subj %s`, caKey, caCert, caSubj)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("4.Create configmap ca-xxxxx-bundle in namespace openshift-config")
		defer deleteConfigMap(oc, "openshift-config", "ca-51946-bundle")
		createConfigMapFromFile(oc, "openshift-config", "ca-51946-bundle", caCert)

		exutil.By("5.Patch the dns.operator/default with transport option as TLS for upstreamresolver")
		dnsUpstreamResolver := "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers\", \"value\":{\"transportConfig\": {\"tls\":{\"caBundle\": {\"name\": \"ca-51946-bundle\"}, \"serverName\": \"dns.ocp51946.ocp\"}, \"transport\": \"TLS\"}, \"upstreams\":[{\"address\":\"" + srvPodIP + "\",  \"port\": 853, \"type\":\"Network\"}]}}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsUpstreamResolver)

		exutil.By("6.Check and confirm the upstream resolver's IP(srvPodIP) and custom CAbundle name appearing in the dns pod")
		// Converting the IPV6 address to upper case for searching in the coreDNS file
		if strings.Count(srvPodIP, ":") >= 2 {
			srvPodIP = fmt.Sprintf("%s", strings.ToUpper(srvPodIP))
			srvPodIP = "[" + srvPodIP + "]"
		}
		// since new configmap is mounted so dns pod is restarted
		waitErr := waitForResourceToDisappear(oc, "openshift-dns", "pod/"+oneDnsPod)
		exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("max time reached but pod %s is not terminated", oneDnsPod))
		ensurePodWithLabelReady(oc, "openshift-dns", dnsPodLabel)
		newDnsPod := getDNSPodName(oc)
		upstreams := readDNSCorefile(oc, newDnsPod, srvPodIP, "-A4")
		o.Expect(upstreams).To(o.ContainSubstring("forward . tls://" + srvPodIP + ":853"))
		o.Expect(upstreams).To(o.ContainSubstring("tls_servername dns.ocp51946.ocp"))
		o.Expect(upstreams).To(o.ContainSubstring("tls /etc/pki/dns.ocp51946.ocp-ca-ca-51946-bundle"))

		exutil.By("7.Check no error logs from dns operator pod")
		dnsOperatorPodName := getPodListByLabel(oc, "openshift-dns-operator", "name=dns-operator")
		podLogs, errLogs := exutil.GetSpecificPodLogs(oc, "openshift-dns-operator", "dns-operator", dnsOperatorPodName[0], srvPodIP+` -A3`)
		o.Expect(errLogs).NotTo(o.HaveOccurred(), "Error in getting logs from the pod")
		o.Expect(podLogs).To(o.ContainSubstring(`msg="reconciling request: /default"`))
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-High-52077-CoreDNS forwarding DNS requests over TLS with CLEAR TEXT [Disruptive]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			coreDNSSrvPod       = filepath.Join(buildPruningBaseDir, "coreDNS-pod.yaml")
			srvPodName          = "test-coredns"
			srvPodLabel         = "name=test-coredns"
			resourceName        = "dns.operator.openshift.io/default"
		)

		exutil.By("1.Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("2.Create a dns server pod")
		ns := oc.Namespace()
		defer exutil.RecoverNamespaceRestricted(oc, ns)
		exutil.SetNamespacePrivileged(oc, ns)
		replaceCoreDnsImage(oc, coreDNSSrvPod)
		err := oc.AsAdmin().Run("create").Args("-f", coreDNSSrvPod, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, srvPodLabel)

		exutil.By("3.Get the user's dns server pod's IP")
		srvPodIP := getPodv4Address(oc, srvPodName, ns)

		exutil.By("4.Patch the dns.operator/default with transport option as Cleartext for forwardplugin")
		dnsForwardPlugin := "[{\"op\":\"add\", \"path\":\"/spec/servers\", \"value\":[{\"forwardPlugin\":{\"policy\":\"Sequential\",\"transportConfig\": {\"transport\": \"Cleartext\"}, \"upstreams\":[\"" + srvPodIP + "\"]}, \"name\": \"test\", \"zones\":[\"ocp52077.ocp\"]}]}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsForwardPlugin)

		exutil.By("5.Check and confirm the upstream resolver's IP(srvPodIP) appearing in the dns pod")
		forward := pollReadDnsCorefile(oc, oneDnsPod, srvPodIP, "-b6", "ocp52077")
		o.Expect(forward).To(o.ContainSubstring("ocp52077.ocp:5353"))
		o.Expect(forward).To(o.ContainSubstring("forward . " + srvPodIP))

		exutil.By("6.Check no error logs from dns operator pod")
		dnsOperatorPodName := getPodListByLabel(oc, "openshift-dns-operator", "name=dns-operator")
		podLogs1, errLogs1 := exutil.GetSpecificPodLogs(oc, "openshift-dns-operator", "dns-operator", dnsOperatorPodName[0], `ocp52077.ocp:5353 -A3`)
		o.Expect(errLogs1).NotTo(o.HaveOccurred(), "Error in getting logs from the pod")
		o.Expect(podLogs1).To(o.ContainSubstring(`msg="reconciling request: /default"`))
		// Patching to remove the forwardplugin configurations.
		dnsDefault := "[{\"op\":\"remove\", \"path\":\"/spec/servers\"}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsDefault)

		exutil.By("7.Patch dns.operator/default with transport option as Cleartext for upstreamresolver")
		dnsUpstreamResolver := "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers\", \"value\":{\"transportConfig\":{\"transport\":\"Cleartext\"}, \"upstreams\":[{\"address\":\"" + srvPodIP + "\", \"type\":\"Network\"}]}}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsUpstreamResolver)

		exutil.By("8.Check and confirm the upstream resolver's IP(srvPodIP) appearing in the dns pod")
		// Converting the IPV6 address to upper case for searching in the coreDNS file
		if strings.Count(srvPodIP, ":") >= 2 {
			srvPodIP = fmt.Sprintf("%s", strings.ToUpper(srvPodIP))
			srvPodIP = "[" + srvPodIP + "]"
		}
		upstreams := pollReadDnsCorefile(oc, oneDnsPod, srvPodIP, "-A2", "forward")
		o.Expect(upstreams).To(o.ContainSubstring("forward . " + srvPodIP + ":53"))

		exutil.By("9.Check no error logs from dns operator pod")
		podLogs, errLogs := exutil.GetSpecificPodLogs(oc, "openshift-dns-operator", "dns-operator", dnsOperatorPodName[0], srvPodIP+`:53 -A3`)
		o.Expect(errLogs).NotTo(o.HaveOccurred(), "Error in getting logs from the pod")
		o.Expect(podLogs).To(o.ContainSubstring(`msg="reconciling request: /default"`))
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-High-52497-Support CoreDNS forwarding DNS requests over TLS - using system CA [Disruptive]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			coreDNSSrvPod       = filepath.Join(buildPruningBaseDir, "coreDNS-pod.yaml")
			srvPodName          = "test-coredns"
			srvPodLabel         = "name=test-coredns"
			resourceName        = "dns.operator.openshift.io/default"
		)

		exutil.By("1.Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("2.Create a dns server pod")
		ns := oc.Namespace()
		defer exutil.RecoverNamespaceRestricted(oc, ns)
		exutil.SetNamespacePrivileged(oc, ns)
		replaceCoreDnsImage(oc, coreDNSSrvPod)
		err := oc.AsAdmin().Run("create").Args("-f", coreDNSSrvPod, "-n", ns).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, ns, srvPodLabel)

		exutil.By("3.Get the user's dns server pod's IP")
		srvPodIP := getPodv4Address(oc, srvPodName, ns)

		exutil.By("4.Patch the dns.operator/default with transport option as tls for forwardplugin")
		dnsForwardPlugin := "[{\"op\":\"add\", \"path\":\"/spec/servers\", \"value\":[{\"forwardPlugin\":{\"policy\":\"Sequential\",\"transportConfig\": {\"tls\":{\"serverName\": \"dns.ocp52497.ocp\"}, \"transport\": \"TLS\"}, \"upstreams\":[\"" + srvPodIP + "\"]}, \"name\": \"test\", \"zones\":[\"ocp52497.ocp\"]}]}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsForwardPlugin)

		exutil.By("5.Check and confirm the upstream resolver's IP(srvPodIP) appearing in the dns pod")
		forward := pollReadDnsCorefile(oc, oneDnsPod, srvPodIP, "-b6", "ocp52497")
		o.Expect(forward).To(o.ContainSubstring("ocp52497.ocp:5353"))
		o.Expect(forward).To(o.ContainSubstring("forward . tls://" + srvPodIP))
		o.Expect(forward).To(o.ContainSubstring("tls_servername dns.ocp52497.ocp"))
		o.Expect(forward).To(o.ContainSubstring("tls"))

		exutil.By("6.Check no error logs from dns operator pod")
		dnsOperatorPodName := getPodListByLabel(oc, "openshift-dns-operator", "name=dns-operator")
		podLogs1, errLogs1 := exutil.GetSpecificPodLogs(oc, "openshift-dns-operator", "dns-operator", dnsOperatorPodName[0], `ocp52497.ocp:5353 -A3`)
		o.Expect(errLogs1).NotTo(o.HaveOccurred(), "Error in getting logs from the pod")
		o.Expect(podLogs1).To(o.ContainSubstring(`msg="reconciling request: /default"`))
		// Patching to remove the forwardplugin configurations.
		dnsDefault := "[{\"op\":\"remove\", \"path\":\"/spec/servers\"}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsDefault)

		exutil.By("7.Patch dns.operator/default with transport option as tls for upstreamresolver")
		dnsUpstreamResolver := "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers\", \"value\":{\"transportConfig\": {\"tls\":{\"serverName\": \"dns.ocp52497.ocp\"}, \"transport\": \"TLS\"}, \"upstreams\":[{\"address\":\"" + srvPodIP + "\",  \"port\": 853, \"type\":\"Network\"}]}}]"
		patchGlobalResourceAsAdmin(oc, resourceName, dnsUpstreamResolver)

		exutil.By("8.Check and confirm the upstream resolver's IP(srvPodIP) appearing in the dns pod")
		// Converting the IPV6 address to upper case for searching in the coreDNS file
		if strings.Count(srvPodIP, ":") >= 2 {
			srvPodIP = fmt.Sprintf("%s", strings.ToUpper(srvPodIP))
			srvPodIP = "[" + srvPodIP + "]"
		}
		upstreams := pollReadDnsCorefile(oc, oneDnsPod, srvPodIP, "-A3", "forward")
		o.Expect(upstreams).To(o.ContainSubstring("forward . tls://" + srvPodIP + ":853"))
		o.Expect(upstreams).To(o.ContainSubstring("tls_servername dns.ocp52497.ocp"))
		o.Expect(upstreams).To(o.ContainSubstring("tls"))

		exutil.By("9.Check no error logs from dns operator pod")
		podLogs, errLogs := exutil.GetSpecificPodLogs(oc, "openshift-dns-operator", "dns-operator", dnsOperatorPodName[0], srvPodIP+` -A3`)
		o.Expect(errLogs).NotTo(o.HaveOccurred(), "Error in getting logs from the pod")
		o.Expect(podLogs).To(o.ContainSubstring(`msg="reconciling request: /default"`))
	})

	g.It("Author:mjoseph-Critical-54042-Configuring CoreDNS caching and TTL parameters [Disruptive]", func() {
		var (
			resourceName      = "dns.operator.openshift.io/default"
			cacheValue        = "[{\"op\":\"replace\", \"path\":\"/spec/cache\", \"value\":{\"negativeTTL\":\"1800s\", \"positiveTTL\":\"604801s\"}}]"
			cacheSmallValue   = "[{\"op\":\"replace\", \"path\":\"/spec/cache\", \"value\":{\"negativeTTL\":\"1s\", \"positiveTTL\":\"1s\"}}]"
			cacheDecimalValue = "[{\"op\":\"replace\", \"path\":\"/spec/cache\", \"value\":{\"negativeTTL\":\"1.9s\", \"positiveTTL\":\"1.6m\"}}]"
			cacheWrongValue   = "[{\"op\":\"replace\", \"path\":\"/spec/cache\", \"value\":{\"negativeTTL\":\"-9s\", \"positiveTTL\":\"1.6\"}}]"
		)

		exutil.By("1. Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("2. Patch the dns.operator/default with postive and negative cache values")
		patchGlobalResourceAsAdmin(oc, resourceName, cacheValue)

		exutil.By("3. Check the cache value in Corefile of coredn")
		cache := pollReadDnsCorefile(oc, oneDnsPod, "cache 604801", "-A2", "denial")
		o.Expect(cache).To(o.ContainSubstring("denial 9984 1800"))

		exutil.By("4. Patch the dns.operator/default with smallest cache values and verify the same")
		patchGlobalResourceAsAdmin(oc, resourceName, cacheSmallValue)
		cache1 := pollReadDnsCorefile(oc, oneDnsPod, "cache 1", "-A2", "denial")
		o.Expect(cache1).To(o.ContainSubstring("denial 9984 1"))

		exutil.By("5. Patch the dns.operator/default with decimal cache values and verify the same")
		patchGlobalResourceAsAdmin(oc, resourceName, cacheDecimalValue)
		cache2 := pollReadDnsCorefile(oc, oneDnsPod, "cache 96", "-A2", "denial")
		o.Expect(cache2).To(o.ContainSubstring("denial 9984 2"))

		exutil.By("6. Patch the dns.operator/default with unrelasitc cache values and check the error messages")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args(resourceName, "--patch="+cacheWrongValue, "--type=json").Output()
		o.Expect(output).To(o.ContainSubstring("spec.cache.positiveTTL: Invalid value: \"1.6\""))
		o.Expect(output).To(o.ContainSubstring("spec.cache.negativeTTL: Invalid value: \"-9s\""))
	})

	// Bug: 1949361, 1884053, 1756344
	g.It("Author:mjoseph-NonHyperShiftHOST-High-55821-Check CoreDNS default bufsize, readinessProbe path and policy", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodLabel      = "app=hello-pod"
			clientPodName       = "hello-pod"
			ptrValue            = "10.0.30.172.in-addr.arpa"
		)
		ns := oc.Namespace()

		exutil.By("Check updated value in dns operator file")
		output, err := oc.AsAdmin().Run("get").Args("cm/dns-default", "-n", "openshift-dns", "-o=jsonpath={.data.Corefile}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("bufsize 1232"))

		exutil.By("Check the cache value in Corefile of coredns under all dns-default-xxx pods")
		podList := getAllDNSPodsNames(oc)
		keepSearchInAllDNSPods(oc, podList, "bufsize 1232")

		exutil.By("Create a client pod")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		exutil.By("Client send out a dig for google.com to check response")
		digOutput, err2 := oc.Run("exec").Args(clientPodName, "--", "dig", "google.com").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
		o.Expect(digOutput).To(o.ContainSubstring("udp: 1232"))

		exutil.By("Client send out a dig for NXDOMAIN to check response")
		digOutput1, err3 := oc.Run("exec").Args(clientPodName, "--", "dig", "nxdomain.google.com").Output()
		o.Expect(err3).NotTo(o.HaveOccurred())
		o.Expect(digOutput1).To(o.ContainSubstring("udp: 1232"))

		exutil.By("Check the different DNS records")
		ingressContPod := getPodListByLabel(oc, "openshift-ingress-operator", "name=ingress-operator")
		// To identify which address type the cluster IP belongs
		clusterIP := getSvcClusterIPByName(oc, "openshift-dns", "dns-default")
		if netutils.IsIPv6String(clusterIP) {
			ptrValue = convertV6AddressToPTR(clusterIP)
		}

		// To find the PTR record
		digOutput3, err3 := oc.AsAdmin().Run("exec").Args("-n", "openshift-ingress-operator", ingressContPod[0],
			"--", "dig", "+short", ptrValue, "PTR").Output()
		o.Expect(err3).NotTo(o.HaveOccurred())
		o.Expect(digOutput3).To(o.ContainSubstring("dns-default.openshift-dns.svc.cluster.local."))

		// To find the SRV record
		digOutput4, err4 := oc.AsAdmin().Run("exec").Args("-n", "openshift-ingress-operator", ingressContPod[0], "--", "dig",
			"+short", "_8443-tcp._tcp.ingress-canary.openshift-ingress-canary.svc.cluster.local", "SRV").Output()
		o.Expect(err4).NotTo(o.HaveOccurred())
		o.Expect(digOutput4).To(o.ContainSubstring("ingress-canary.openshift-ingress-canary.svc.cluster.local."))

		// bug:- 1884053
		exutil.By("Check Readiness probe configured to use the '/ready' path")
		dnsPodName2 := getRandomElementFromList(podList)
		output2, err4 := oc.AsAdmin().Run("get").Args("pod/"+dnsPodName2, "-n", "openshift-dns", "-o=jsonpath={.spec.containers[0].readinessProbe.httpGet}").Output()
		o.Expect(err4).NotTo(o.HaveOccurred())
		o.Expect(output2).To(o.ContainSubstring(`"path":"/ready"`))

		// bug:- 1756344
		exutil.By("Check the policy is sequential in Corefile of coredns under all dns-default-xxx pods")
		keepSearchInAllDNSPods(oc, podList, "policy sequential")
	})

	// Bug: 2061244
	// no master nodes on HyperShift guest cluster so this case is not available
	g.It("Author:mjoseph-NonHyperShiftHOST-High-56325-DNS pod should not work on nodes with taint configured [Disruptive]", func() {

		exutil.By("Check whether the dns pods eviction annotation is set or not")
		podList := getAllDNSPodsNames(oc)
		dnsPodName := getRandomElementFromList(podList)
		findAnnotation := getAnnotation(oc, "openshift-dns", "po", dnsPodName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`cluster-autoscaler.kubernetes.io/enable-ds-eviction":"true`))

		// get the worker and master node name
		masterNodes := getByLabelAndJsonPath(oc, "default", "node", "node-role.kubernetes.io/master", "{.items[*].metadata.name}")
		workerNodes := getByLabelAndJsonPath(oc, "default", "node", "node-role.kubernetes.io/worker", "{.items[*].metadata.name}")
		masterNodeName := getRandomElementFromList(strings.Split(masterNodes, " "))
		workerNodeName := getRandomElementFromList(strings.Split(workerNodes, " "))

		exutil.By("Apply NoSchedule taint to worker node and confirm the dns pod is not scheduled")
		defer deleteTaint(oc, "node", workerNodeName, "dedicated-")
		addTaint(oc, "node", workerNodeName, "dedicated=Kafka:NoSchedule")
		// Confirming one node is not schedulable with dns pod
		podOut, err := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", "openshift-dns", "ds", "dns-default").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(podOut, "Number of Nodes Misscheduled: 1") {
			e2e.Logf("Number of Nodes Misscheduled: 1 is not expected")
		}

		exutil.By("Apply NoSchedule taint to master node and confirm the dns pod is not scheduled on it")
		defer deleteTaint(oc, "node", masterNodeName, "dns-taint-")
		addTaint(oc, "node", masterNodeName, "dns-taint=test:NoSchedule")
		// Confirming two nodes are not schedulable with dns pod
		podOut2, err := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", "openshift-dns", "ds", "dns-default").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(podOut2, "Number of Nodes Misscheduled: 2") {
			e2e.Logf("Number of Nodes Misscheduled: 2 is not expected")
		}
	})

	// Bug: 1916907
	// Bug: OCPBUGS-35063
	g.It("Author:mjoseph-NonHyperShiftHOST-Longduration-NonPreRelease-High-56539-Disabling the internal registry should not corrupt /etc/hosts [Disruptive]", func() {
		exutil.By("Step1: Get the Cluster IP of image-registry")
		// Skip the test case if openshift-image-registry namespace is not found
		clusterIP, err := oc.AsAdmin().WithoutNamespace().Run("get").Args(
			"service", "image-registry", "-n", "openshift-image-registry", "-o=jsonpath={.spec.clusterIP}").Output()
		if err != nil || strings.Contains(clusterIP, `namespaces \"openshift-image-registry\" not found`) {
			g.Skip("Skip for non-supported platform")
		}
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Step2: SSH to the node and confirm the /etc/hosts have the same clusterIP")
		allNodeList, _ := exutil.GetAllNodes(oc)
		// get a random node
		node := getRandomElementFromList(allNodeList)
		hostOutput, err := exutil.DebugNodeWithChroot(oc, node, "cat", "/etc/hosts")
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(hostOutput).To(o.And(
			o.ContainSubstring("127.0.0.1   localhost localhost.localdomain localhost4 localhost4.localdomain4"),
			o.ContainSubstring("::1         localhost localhost.localdomain localhost6 localhost6.localdomain6"),
			o.ContainSubstring(clusterIP)))
		o.Expect(hostOutput).NotTo(o.And(o.ContainSubstring("error"), o.ContainSubstring("failed"), o.ContainSubstring("timed out")))

		// Set status variables
		expectedStatus := map[string]string{"Available": "True", "Progressing": "False", "Degraded": "False"}

		exutil.By("Step3: Disable the internal registry and check /host details")
		defer func() {
			exutil.By("Recover image registry change")
			err4 := oc.AsAdmin().Run("patch").Args("configs.imageregistry/cluster", "-p", "{\"spec\":{\"managementState\":\"Managed\"}}", "--type=merge").Execute()
			o.Expect(err4).NotTo(o.HaveOccurred())
			err = waitCoBecomes(oc, "image-registry", 240, expectedStatus)
			o.Expect(err).NotTo(o.HaveOccurred())
			err = waitCoBecomes(oc, "openshift-apiserver", 480, expectedStatus)
			o.Expect(err).NotTo(o.HaveOccurred())
			err = waitCoBecomes(oc, "kube-apiserver", 800, expectedStatus)
			o.Expect(err).NotTo(o.HaveOccurred())
		}()
		// Set image registry to 'Removed'
		_, err = oc.WithoutNamespace().AsAdmin().Run("patch").Args("configs.imageregistry/cluster", "-p", `{"spec":{"managementState":"Removed"}}`, "--type=merge").Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Step4: SSH to the node and confirm the /etc/hosts details, after disabling")
		hostOutput2, err5 := exutil.DebugNodeWithChroot(oc, node, "cat", "/etc/hosts")
		o.Expect(err5).NotTo(o.HaveOccurred())
		o.Expect(hostOutput2).To(o.And(
			o.ContainSubstring("127.0.0.1   localhost localhost.localdomain localhost4 localhost4.localdomain4"),
			o.ContainSubstring("::1         localhost localhost.localdomain localhost6 localhost6.localdomain6")))
		o.Expect(hostOutput2).NotTo(o.And(o.ContainSubstring("error"), o.ContainSubstring("failed"), o.ContainSubstring("timed out")))
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-56884-Confirm the coreDNS version and Kubernetes version of the oc client", func() {
		var kubernetesVersion = "v1.33"
		var coreDNS = "CoreDNS-1.11.3"

		exutil.By("1.Check the Kubernetes version")
		ocClientOutput, err := oc.AsAdmin().WithoutNamespace().Run("version").Args("--client=false").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(ocClientOutput).To(o.ContainSubstring(kubernetesVersion))

		exutil.By("2.Check all default dns pods for coredns version")
		cmd := fmt.Sprintf("coredns --version")
		podList := getAllDNSPodsNames(oc)
		dnsPod := getRandomElementFromList(podList)
		output, err := oc.AsAdmin().Run("exec").Args("-n", "openshift-dns", dnsPod, "-c", "dns", "--", "bash", "-c", cmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(coreDNS))
	})

	g.It("Author:mjoseph-Critical-60350-Check the max number of domains in the search path list of any pod", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			clientPod           = filepath.Join(buildPruningBaseDir, "testpod-60350.yaml")
			clientPodLabel      = "app=testpod-60350"
			clientPodName       = "testpod-60350"
		)
		ns := oc.Namespace()

		exutil.By("Create a pod with 32 DNS search list")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		exutil.By("Check the pod event logs and confirm there is no Search Line limits")
		checkPodEvent := describePodResource(oc, clientPodName, ns)
		o.Expect(checkPodEvent).NotTo(o.ContainSubstring("Warning  DNSConfigForming"))

		exutil.By("Check the resulting pod have all those search entries in its /etc/resolf.conf")
		execOutput, err := oc.Run("exec").Args(clientPodName, "--", "sh", "-c", "cat /etc/resolv.conf").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(execOutput).To(o.ContainSubstring("8th.com 9th.com 10th.com 11th.com 12th.com 13th.com 14th.com 15th.com 16th.com 17th.com 18th.com 19th.com 20th.com 21th.com 22th.com 23th.com 24th.com 25th.com 26th.com 27th.com 28th.com 29th.com 30th.com 31th.com 32th.com"))
	})

	g.It("Author:mjoseph-Critical-60492-Check the max number of characters in the search path of any pod", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			clientPod           = filepath.Join(buildPruningBaseDir, "testpod-60492.yaml")
			clientPodLabel      = "app=testpod-60492"
			clientPodName       = "testpod-60492"
		)
		ns := oc.Namespace()

		exutil.By("Create a pod with a single search path with 253 characters")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		exutil.By("Check the pod event logs and confirm there is no Search Line limits")
		checkPodEvent := describePodResource(oc, clientPodName, ns)
		o.Expect(checkPodEvent).NotTo(o.ContainSubstring("Warning  DNSConfigForming"))

		exutil.By("Check the resulting pod have all those search entries in its /etc/resolf.conf")
		execOutput, err := oc.Run("exec").Args(clientPodName, "--", "sh", "-c", "cat /etc/resolv.conf").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(execOutput).To(o.ContainSubstring("t47x6d4lzz1zxm1bakrmiceb0tljzl9n8r19kqu9s3731ectkllp9mezn7cldozt25nlenyh5jus5b9rr687u2icimakjpyf4rsux3c66giulc0d2ipsa6bpa6dykgd0mc25r1m89hvzjcix73sdwfbu5q67t0c131i1fqne0o7we20ve2emh1046h9m854wfxo0spb2gv5d65v9x2ibuiti7rhr2y8u72hil5cutp63sbhi832kf3v4vuxa0"))
	})

	// Bug: 2095941, OCPBUGS-5943
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-High-63553-Annotation 'TopologyAwareHints' presents should not cause any pathological events", func() {
		// OCPBUGS-5943
		exutil.By("Check dns daemon set for minReadySeconds to 9, maxSurge to 10% and maxUnavailable to 0")
		jsonPath := `{.spec.minReadySeconds}-{.spec.updateStrategy.rollingUpdate.maxSurge}-{.spec.updateStrategy.rollingUpdate.maxUnavailable}`
		spec := getByJsonPath(oc, "openshift-dns", "daemonset/dns-default", jsonPath)
		o.Expect(spec).To(o.ContainSubstring("9-10%-0"))

		// Checking whether there are windows nodes
		windowNodeList, err := exutil.GetAllNodesbyOSType(oc, "windows")
		o.Expect(err).NotTo(o.HaveOccurred())

		if len(windowNodeList) > 1 {
			g.Skip("This case will not work on clusters having windows nodes")
		}

		exutil.By("Check whether the topology-aware-hints annotation is auto set or not")
		// Get all dns pods then check the resident nodes labels one by one
		// search unique `topology.kubernetes.io/zone` info on worker nodes
		zoneList := []string{}
		for _, dnsPod := range getAllDNSPodsNames(oc) {
			node := getByJsonPath(oc, "openshift-dns", "pod/"+dnsPod, "{.spec.nodeName}")
			labels := getByJsonPath(oc, "default", "node/"+node, "{.metadata.labels}")
			// excluding the master nodes
			if strings.Contains(labels, "node-role.kubernetes.io/master") || strings.Contains(labels, "node-role.kubernetes.io/control-plane") {
				continue
			}
			zoneInfo := getByJsonPath(oc, "default", "node/"+node, `{.metadata.labels.topology\.kubernetes\.io/zone}`)
			// set zone as invalid if no zone label or its value is ""
			if zoneInfo == "" {
				zoneList = append(zoneList, "Invalid")
				break
			}
			if !slices.Contains(zoneList, zoneInfo) {
				e2e.Logf("new zone is found: %v", zoneInfo)
				zoneList = append(zoneList, zoneInfo)
			}
		}
		e2e.Logf("all found zones are: %v", zoneList)

		// Topology-aware hints annotation present only if all nodes having the topology.kubernetes.io/zone label and from at least two zones
		findAnnotation := getAnnotation(oc, "openshift-dns", "svc", "dns-default")
		if slices.Contains(zoneList, "Invalid") || len(zoneList) < 2 {
			o.Expect(findAnnotation).NotTo(o.ContainSubstring(`"service.kubernetes.io/topology-aware-hints":"auto"`))
		} else {
			o.Expect(findAnnotation).To(o.ContainSubstring(`"service.kubernetes.io/topology-aware-hints":"auto"`))
		}
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-ConnectedOnly-Critical-73379-DNSNameResolver CR get updated with IP addresses and TTL of the DNS name [Serial]", func() {
		// skip the test if featureSet is not there
		if !exutil.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodLabel      = "app=hello-pod"
			clientPodName       = "hello-pod"
			egressFirewall      = filepath.Join(buildPruningBaseDir, "egressfirewall-wildcard.yaml")
		)

		exutil.By("1. Create egressfirewall file")
		ns := oc.Namespace()
		operateResourceFromFile(oc, "create", ns, egressFirewall)
		waitEgressFirewallApplied(oc, "default", ns)

		exutil.By("2. Create a client pod")
		createResourceFromFile(oc, ns, clientPod)
		ensurePodWithLabelReady(oc, ns, clientPodLabel)

		exutil.By("3. Verify the record created with the dns name in the DNSNameResolver CR")
		wildcardDnsName := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..spec.name}")
		o.Expect(wildcardDnsName).To(o.ContainSubstring("*.google.com."))

		exutil.By("4. Verify the allowed rules which matches the wildcard take effect.")
		// as per the egress firewall, only domains having "*.google.com" will only allowed
		checkDomainReachability(oc, clientPodName, ns, "www.google.com", true)
		checkDomainReachability(oc, clientPodName, ns, "www.redhat.com", false)
		checkDomainReachability(oc, clientPodName, ns, "calendar.google.com", true)

		exutil.By("5. Confirm the wildcard entry is resolved to dnsName with IP address and TTL value")
		// resolved DNS names
		dnsName := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..status.resolvedNames..dnsName}")
		o.Expect(dnsName).To(o.ContainSubstring("www.google.com. calendar.google.com."))
		// resolved TTL values
		ttlValues := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..status.resolvedNames..resolvedAddresses..ttlSeconds}")
		o.Expect(ttlValues).To(o.MatchRegexp(`[0-9]{1,3}`))
		// resolved IP address
		ipAddress := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..status.resolvedNames..resolvedAddresses..ip}")
		o.Expect(ipAddress).To(o.MatchRegexp(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`))
		o.Expect(strings.Count(ipAddress, ":") >= 2).To(o.BeTrue())
	})

	// Bug: OCPBUGS-33750
	g.It("Author:mjoseph-NonHyperShiftHOST-ConnectedOnly-High-75426-DNSNameResolver CR should resolve multiple DNS names [Serial]", func() {
		// skip the test if featureSet is not there
		if !exutil.IsTechPreviewNoUpgrade(oc) {
			g.Skip("featureSet: TechPreviewNoUpgrade is required for this test, skipping")
		}
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			clientPod           = filepath.Join(buildPruningBaseDir, "test-client-pod.yaml")
			clientPodLabel      = "app=hello-pod"
			clientPodName       = "hello-pod"
			egressFirewall      = filepath.Join(buildPruningBaseDir, "egressfirewall-wildcard.yaml")
			egressFirewall2     = filepath.Join(buildPruningBaseDir, "egressfirewall-multiDomain.yaml")
		)

		exutil.By("1. Create four egressfirewall rules and client pods in different namepaces, then wait until there are available")
		var project []string
		for i := range 4 {
			project = append(project, oc.Namespace())
			exutil.SetNamespacePrivileged(oc, project[i])
			operateResourceFromFile(oc, "create", project[i], clientPod)
			operateResourceFromFile(oc, "create", project[i], egressFirewall)
			ensurePodWithLabelReady(oc, project[i], clientPodLabel)
			waitEgressFirewallApplied(oc, "default", project[i])
			oc.SetupProject()
		}

		exutil.By("2. Check whether the default dnsnameresolver CR got created and its resolved dns name")
		wildcardDnsName := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..spec.name}")
		o.Expect(wildcardDnsName).To(o.ContainSubstring("*.google.com."))
		randomNS := getRandomElementFromList(project)
		checkDomainReachability(oc, clientPodName, randomNS, "www.google.com", true)

		exutil.By("3. Edit some egressfirewalls")
		updateValueTest1 := "[{\"op\":\"replace\",\"path\":\"/spec/egress/0/to/dnsName\", \"value\":\"www.yahoo.com\"}]"
		updateValueTest2 := "[{\"op\":\"add\",\"path\":\"/spec/egress/1\", \"value\":{\"type\":\"Deny\",\"to\":{\"dnsName\":\"www.redhat.com\"}}}]"
		updateValueTest3 := "[{\"op\":\"add\",\"path\":\"/spec/egress/0\", \"value\":{\"type\":\"Deny\",\"to\":{\"dnsName\":\"calendar.google.com\"}}}]"
		updateValueTest4 := "[{\"op\":\"add\",\"path\":\"/spec/egress/1\", \"value\":{\"type\":\"Deny\",\"to\":{\"dnsName\":\"calendar.google.com\"}}}]"
		patchResourceAsAdminAnyType(oc, project[0], "egressfirewall.k8s.ovn.org/default", updateValueTest1, "json")
		patchResourceAsAdminAnyType(oc, project[1], "egressfirewall.k8s.ovn.org/default", updateValueTest2, "json")
		patchResourceAsAdminAnyType(oc, project[2], "egressfirewall.k8s.ovn.org/default", updateValueTest3, "json")
		patchResourceAsAdminAnyType(oc, project[3], "egressfirewall.k8s.ovn.org/default", updateValueTest4, "json")
		waitEgressFirewallApplied(oc, "default", project[0])
		waitEgressFirewallApplied(oc, "default", project[1])
		waitEgressFirewallApplied(oc, "default", project[2])
		waitEgressFirewallApplied(oc, "default", project[3])

		exutil.By("4. Check the changes made to dnsnameresolver CR and its resolved dns name in different namespace")
		wildcardDnsName = getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..spec.name}")
		o.Expect(wildcardDnsName).To(o.And(o.ContainSubstring(
			"calendar.google.com."), o.ContainSubstring(
			"*.google.com."), o.ContainSubstring(
			"www.redhat.com."), o.ContainSubstring(
			"www.yahoo.com.")))
		checkDomainReachability(oc, clientPodName, project[0], "www.yahoo.com", true)
		checkDomainReachability(oc, clientPodName, project[0], "www.google.com", false)
		checkDomainReachability(oc, clientPodName, project[1], "www.google.com", true)
		checkDomainReachability(oc, clientPodName, project[1], "www.redhat.com", false)
		checkDomainReachability(oc, clientPodName, project[2], "calendar.google.com", false)
		checkDomainReachability(oc, clientPodName, project[2], "www.google.com", true)
		checkDomainReachability(oc, clientPodName, project[3], "calendar.google.com", true)

		exutil.By("5. Delete an egressfirewall and confirm the same")
		err1 := oc.AsAdmin().WithoutNamespace().Run("delete").Args("egressfirewall", "default", "-n", project[0]).Execute()
		o.Expect(err1).NotTo(o.HaveOccurred())
		// the firewall was previous blocking the dns resolution of 'google.com' in the namespace and now not
		checkDomainReachability(oc, clientPodName, project[0], "www.google.com", true)
		wildcardDnsName = getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..spec.name}")
		o.Expect(wildcardDnsName).NotTo(o.ContainSubstring("www.yahoo.com."))

		exutil.By("6. Recreate an egressfirewall and confirm the same")
		// Updating in the yaml file with dnsName '*.google.com' as 'amazon.com'
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|"\*.google.com\"|www.amazon.com|g' %s`, egressFirewall)
		_, sedErr := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(sedErr).NotTo(o.HaveOccurred())
		operateResourceFromFile(oc, "create", project[0], egressFirewall)
		waitEgressFirewallApplied(oc, "default", project[0])
		checkDomainReachability(oc, clientPodName, project[0], "www.amazon.com", true)
		wildcardDnsName = getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..spec.name}")
		o.Expect(wildcardDnsName).To(o.ContainSubstring("www.amazon.com."))

		exutil.By("7. Create another egressfirewall and its client pod in a different namespace")
		project5 := oc.Namespace()
		exutil.SetNamespacePrivileged(oc, project5)
		operateResourceFromFile(oc, "create", project5, egressFirewall2)
		waitEgressFirewallApplied(oc, "default", project5)
		operateResourceFromFile(oc, "create", project5, clientPod)
		ensurePodWithLabelReady(oc, project5, clientPodLabel)

		exutil.By("8. Verify the  three dnsnameresolver records created in DNSNameResolver CR")
		wildcardDnsNames := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..spec.name}")
		o.Expect(wildcardDnsNames).To(o.And(o.ContainSubstring("*.google.com."), o.ContainSubstring(
			"www.facebook.com."), o.ContainSubstring("registry-1.docker.io.")))

		exutil.By("9. Verify the dns records are resolved based on allowed rules only")
		checkDomainReachability(oc, clientPodName, project5, "www.facebook.com:80", true)
		checkDomainReachability(oc, clientPodName, project5, "registry-1.docker.io", true)
		// as per the egress firewall, domain name having "www.facebook.com" with port 80 will only resolved
		checkDomainReachability(oc, clientPodName, project5, "www.facebook.com:443", false)

		exutil.By("10. Confirm the dns records are resolved with IP address and TTL value")
		// resolved DNS names
		dnsName := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..status.resolvedNames..dnsName}")
		o.Expect(dnsName).To(o.And(o.ContainSubstring("www.facebook.com."), o.ContainSubstring("registry-1.docker.io.")))
		// resolved TTL values
		ttlValues := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..status.resolvedNames..resolvedAddresses..ttlSeconds}")
		o.Expect(ttlValues).To(o.MatchRegexp(`[0-9]{1,3}`))
		// resolved IP address
		ipAddress := getByJsonPath(oc, "openshift-ovn-kubernetes", "dnsnameresolver", "{.items..status.resolvedNames..resolvedAddresses..ip}")
		o.Expect(ipAddress).To(o.MatchRegexp(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`))
		o.Expect(strings.Count(ipAddress, ":") >= 2).To(o.BeTrue())
	})
})
