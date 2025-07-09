package router

import (
	"fmt"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/router/ginkgo-test/test/extended/util"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_DNS", func() {
	defer g.GinkgoRecover()
	var oc = exutil.NewCLI("dns-operator", exutil.KubeConfigPath())

	// incorporate OCP-26151 and OCP-23278 into one
	// Test case creater: hongli@redhat.com - OCP-26151-Integrate DNS operator metrics with Prometheus
	// Test case creater: hongli@redhat.com - OCP-23278-Integrate coredns metrics with monitoring component
	g.It("Author:mjoseph-Critical-26151-Integrate DNS operator metrics with Prometheus", func() {
		var (
			ons        = "openshift-dns-operator"
			dns        = "openshift-dns"
			label      = `"openshift.io/cluster-monitoring":"true"`
			prometheus = "prometheus-k8s"
		)
		// OCP-26151
		exutil.By("1. Check the `cluster-monitoring` label exist in the dns operator namespace")
		oplabels := getByJsonPath(oc, ons, "ns/openshift-dns-operator", "{.metadata.labels}")
		o.Expect(oplabels).To(o.ContainSubstring(label))

		exutil.By("2. Check whether servicemonitor exist in the dns operator namespace")
		smname := getByJsonPath(oc, ons, "servicemonitor/dns-operator", "{.metadata.name}")
		o.Expect(smname).To(o.ContainSubstring("dns-operator"))

		exutil.By("3. Check whether rolebinding exist in the dns operator namespace")
		poname := getByJsonPath(oc, ons, "rolebinding/prometheus-k8s", "{.metadata.name}")
		o.Expect(poname).To(o.ContainSubstring(prometheus))

		// OCP-23278
		// Bug: 1688969
		exutil.By("4. Check the `cluster-monitoring` label exist in the dns namespace")
		polabels := getByJsonPath(oc, dns, "ns/openshift-dns", "{.metadata.labels}")
		o.Expect(polabels).To(o.ContainSubstring(label))

		exutil.By("5. Check whether servicemonitor exist in the dns namespace")
		smname1 := getByJsonPath(oc, dns, "servicemonitor/dns-default", "{.metadata.name}")
		o.Expect(smname1).To(o.ContainSubstring("dns-default"))

		exutil.By("6. Check whether rolebinding exist in the dns namespace")
		pdname := getByJsonPath(oc, dns, "rolebinding/prometheus-k8s", "{.metadata.name}")
		o.Expect(pdname).To(o.ContainSubstring(prometheus))
	})

	// Test case creater: hongli@redhat.com
	// No dns operator namespace on HyperShift guest cluster so this case is not available
	g.It("Author:mjoseph-NonHyperShiftHOST-High-37912-DNS operator should show clear error message when DNS service IP already allocated [Disruptive]", func() {
		// Bug: 1813062
		// Store the clusterip from the cluster
		clusterIp := getByJsonPath(oc, "openshift-dns", "service/dns-default", "{.spec.clusterIP}")

		exutil.By("Step1: Scale the CVO and DNS operator pod to zero and delete the default DNS service")
		dnsOperatorPodName := getPodListByLabel(oc, "openshift-dns-operator", "name=dns-operator")[0]
		defer func() {
			exutil.By("Recover the CVO and DNS")
			oc.AsAdmin().WithoutNamespace().Run("scale").Args("deployment/cluster-version-operator", "--replicas=1", "-n", "openshift-cluster-version").Output()
			oc.AsAdmin().WithoutNamespace().Run("scale").Args("deployment/dns-operator", "--replicas=1", "-n", "openshift-dns-operator").Output()
			// if the dns-default svc didn't came up in given time, dns operator restoration will help
			deleteDnsOperatorToRestore(oc)
		}()
		_, err := oc.AsAdmin().WithoutNamespace().Run("scale").Args("deployment/cluster-version-operator", "--replicas=0", "-n", "openshift-cluster-version").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = oc.AsAdmin().WithoutNamespace().Run("scale").Args("deployment/dns-operator", "--replicas=0", "-n", "openshift-dns-operator").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		errPodDis := waitForResourceToDisappear(oc, "openshift-dns-operator", "pod/"+dnsOperatorPodName)
		exutil.AssertWaitPollNoErr(errPodDis, fmt.Sprintf("resource %v does not disapper", "pod/"+dnsOperatorPodName))
		err1 := oc.AsAdmin().WithoutNamespace().Run("delete").Args("svc", "dns-default", "-n", "openshift-dns").Execute()
		o.Expect(err1).NotTo(o.HaveOccurred())
		err = waitForResourceToDisappear(oc, "openshift-dns", "service/dns-default")
		exutil.AssertWaitPollNoErr(err, "the service/dns-default does not disapper within allowed time")

		exutil.By("Step2: Create a test server with the Cluster IP and scale up the dns operator")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("svc", "svc-37912", "-n", "openshift-dns").Execute()
		err2 := oc.AsAdmin().WithoutNamespace().Run("create").Args(
			"svc", "clusterip", "svc-37912", "--tcp=53:53", "--clusterip="+clusterIp, "-n", "openshift-dns").Execute()
		o.Expect(err2).NotTo(o.HaveOccurred())
		oc.AsAdmin().WithoutNamespace().Run("scale").Args("deployment/dns-operator", "--replicas=1", "-n", "openshift-dns-operator").Output()
		// wait for the dns operator pod to come up
		ensurePodWithLabelReady(oc, "openshift-dns-operator", "name=dns-operator")

		exutil.By("Step3: Confirm the new dns service came with the given address")
		newClusterIp := getByJsonPath(oc, "openshift-dns", "service/svc-37912", "{.spec.clusterIP}")
		o.Expect(newClusterIp).To(o.BeEquivalentTo(clusterIp))

		exutil.By("Step4: Confirm the error message from the DNS operator status")
		outputOpcfg, errOpcfg := oc.AsAdmin().WithoutNamespace().Run("get").Args(
			"dns.operator", "default", `-o=jsonpath={.status.conditions[?(@.type=="Degraded")].message}}`).Output()
		o.Expect(errOpcfg).NotTo(o.HaveOccurred())
		o.Expect(outputOpcfg).To(o.ContainSubstring("No IP address is assigned to the DNS service"))

		exutil.By("Step5: Check the degraded status of dns operator among the cluster operators")
		jsonPath := `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		waitForOutput(oc, "default", "co/dns", jsonPath, "FalseTrueTrue")
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-41049-DNS controlls pod placement by node selector [Disruptive]", func() {
		podList := getAllDNSPodsNames(oc)
		if len(podList) == 1 {
			g.Skip("Skipping on SNO cluster (just has one dns pod)")
		}

		var (
			ns    = "openshift-dns"
			label = "ne-dns-testing=true"
		)

		exutil.By("check the default dns nodeSelector is present")
		nodePlacement := getByJsonPath(oc, ns, "ds/dns-default", "{.spec.template.spec.nodeSelector}")
		o.Expect(nodePlacement).To(o.BeEquivalentTo(`{"kubernetes.io/os":"linux"}`))

		// Since func forceOnlyOneDnsPodExist() has implemented DNS nodeSelector
		// just ensure the pod is running on the node with label "ne-dns-testing=true"
		exutil.By("check the nodeSelector can be updated")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)
		nodePlacement = getByJsonPath(oc, "openshift-dns", "ds/dns-default", "{.spec.template.spec.nodeSelector}")
		o.Expect(nodePlacement).To(o.ContainSubstring(`{"ne-dns-testing":"true"}`))

		exutil.By("check the dns pod is running on the expected node")
		nodeByPod := getByJsonPath(oc, ns, "pod/"+oneDnsPod, "{.spec.nodeName}")
		nodeByLabel := getByLabelAndJsonPath(oc, "default", "node", label, "{.items[*].metadata.name}")
		o.Expect(nodeByPod).To(o.BeEquivalentTo(nodeByLabel))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-41050-DNS controll pod placement by tolerations [Disruptive]", func() {
		// the case needs at least one worker node since dns pods will be removed from master
		// so skip on SNO and Compact cluster that no dedicated worker node
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "-l node-role.kubernetes.io/worker=,node-role.kubernetes.io/master!=", `-ojsonpath={.items[*].status.conditions[?(@.type=="Ready")].status}`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Count(output, "True") < 1 {
			g.Skip("Skipping as there is no dedicated worker nodes")
		}

		var (
			ns                  = "openshift-dns"
			dnsCustomToleration = `[{"op":"replace", "path":"/spec/nodePlacement", "value":{"tolerations":[{"effect":"NoExecute","key":"my-dns-test","operator":"Equal","value":"abc"}]}}]`
		)

		exutil.By("check dns pod placement to confirm it is running on default tolerations")
		tolerationCfg := getByJsonPath(oc, ns, "ds/dns-default", "{.spec.template.spec.tolerations}")
		o.Expect(tolerationCfg).To(o.ContainSubstring(`{"key":"node-role.kubernetes.io/master","operator":"Exists"}`))

		exutil.By("Patch dns operator config with custom tolerations of dns pod, not to tolerate master node taints")
		dnsPodsList := getAllDNSPodsNames(oc)
		jsonPath := `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		defer deleteDnsOperatorToRestore(oc)
		patchGlobalResourceAsAdmin(oc, "dns.operator.openshift.io/default", dnsCustomToleration)
		waitForRangeOfPodsToDisappear(oc, ns, dnsPodsList)
		waitForOutput(oc, "default", "co/dns", jsonPath, "TrueFalseFalse")
		// Get new created DNS pods and ensure they are not running on master
		dnsPodsList = getAllDNSPodsNames(oc)
		for _, podName := range dnsPodsList {
			nodeName := getNodeNameByPod(oc, ns, podName)
			nodeLabels := getByJsonPath(oc, "default", "node/"+nodeName, "{.metadata.labels}")
			o.Expect(nodeLabels).NotTo(o.ContainSubstring("node-role.kubernetes.io/master"))
		}

		exutil.By("check dns pod placement to check the custom tolerations")
		tolerationCfg = getByJsonPath(oc, ns, "ds/dns-default", "{.spec.template.spec.tolerations}")
		o.Expect(tolerationCfg).To(o.ContainSubstring(`{"effect":"NoExecute","key":"my-dns-test","operator":"Equal","value":"abc"}`))

		exutil.By("check dns.operator status to see any error messages")
		status := getByJsonPath(oc, "default", "dns.operator/default", "{.status}")
		o.Expect(status).NotTo(o.ContainSubstring("error"))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-High-46183-DNS operator supports Random, RoundRobin and Sequential policy for servers.forwardPlugin [Disruptive]", func() {
		resourceName := "dns.operator.openshift.io/default"
		jsonPatch := "[{\"op\":\"add\", \"path\":\"/spec/servers\", \"value\":[{\"forwardPlugin\":{\"policy\":\"Random\",\"upstreams\":[\"8.8.8.8\"]},\"name\":\"test\",\"zones\":[\"mytest.ocp\"]}]}]"

		exutil.By("Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("patch the dns.operator/default and add custom zones config, check Corefile and ensure the policy is Random")
		patchGlobalResourceAsAdmin(oc, resourceName, jsonPatch)
		policy := pollReadDnsCorefile(oc, oneDnsPod, "8.8.8.8", "-A2", "policy random")
		o.Expect(policy).To(o.ContainSubstring(`policy random`))

		exutil.By("updateh the custom zones policy to RoundRobin, check Corefile and ensure it is updated ")
		patchGlobalResourceAsAdmin(oc, resourceName, "[{\"op\":\"replace\", \"path\":\"/spec/servers/0/forwardPlugin/policy\", \"value\":\"RoundRobin\"}]")
		policy = pollReadDnsCorefile(oc, oneDnsPod, "8.8.8.8", "-A2", "policy round_robin")
		o.Expect(policy).To(o.ContainSubstring(`policy round_robin`))

		exutil.By("updateh the custom zones policy to Sequential, check Corefile and ensure it is updated")
		patchGlobalResourceAsAdmin(oc, resourceName, "[{\"op\":\"replace\", \"path\":\"/spec/servers/0/forwardPlugin/policy\", \"value\":\"Sequential\"}]")
		policy = pollReadDnsCorefile(oc, oneDnsPod, "8.8.8.8", "-A2", "policy sequential")
		o.Expect(policy).To(o.ContainSubstring(`policy sequential`))
	})

	// author: shudili@redhat.com
	// no dns operator namespace on HyperShift guest cluster so this case is not available
	g.It("Author:shudili-NonHyperShiftHOST-Medium-46873-Configure operatorLogLevel under the default dns operator and check the logs flag [Disruptive]", func() {
		var (
			resourceName        = "dns.operator.openshift.io/default"
			cfgOploglevelDebug  = "[{\"op\":\"replace\", \"path\":\"/spec/operatorLogLevel\", \"value\":\"Debug\"}]"
			cfgOploglevelTrace  = "[{\"op\":\"replace\", \"path\":\"/spec/operatorLogLevel\", \"value\":\"Trace\"}]"
			cfgOploglevelNormal = "[{\"op\":\"replace\", \"path\":\"/spec/operatorLogLevel\", \"value\":\"Normal\"}]"
		)
		defer deleteDnsOperatorToRestore(oc)

		exutil.By("Check default log level of dns operator")
		outputOpcfg, errOpcfg := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.operator", "default", "-o=jsonpath={.spec.operatorLogLevel}").Output()
		o.Expect(errOpcfg).NotTo(o.HaveOccurred())
		o.Expect(outputOpcfg).To(o.ContainSubstring("Normal"))

		//Remove the dns operator pod and wait for the new pod is created, which is useful to check the dns operator log
		exutil.By("Remove dns operator pod")
		dnsOperatorPodName := getPodListByLabel(oc, "openshift-dns-operator", "name=dns-operator")[0]
		_, errDelpod := oc.AsAdmin().WithoutNamespace().Run("delete").Args("pod", dnsOperatorPodName, "-n", "openshift-dns-operator").Output()
		o.Expect(errDelpod).NotTo(o.HaveOccurred())
		errPodDis := waitForResourceToDisappear(oc, "openshift-dns-operator", "pod/"+dnsOperatorPodName)
		exutil.AssertWaitPollNoErr(errPodDis, fmt.Sprintf("the dns-operator pod isn't terminated"))
		ensurePodWithLabelReady(oc, "openshift-dns-operator", "name=dns-operator")

		exutil.By("Patch dns operator with operator logLevel Debug")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgOploglevelDebug)
		exutil.By("Check logLevel debug in dns operator")
		outputOpcfg, errOpcfg = oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.operator", "default", "-o=jsonpath={.spec.operatorLogLevel}").Output()
		o.Expect(errOpcfg).NotTo(o.HaveOccurred())
		o.Expect(outputOpcfg).To(o.ContainSubstring("Debug"))

		exutil.By("Patch dns operator with operator logLevel trace")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgOploglevelTrace)
		exutil.By("Check logLevel trace in dns operator")
		outputOpcfg, errOpcfg = oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.operator", "default", "-o=jsonpath={.spec.operatorLogLevel}").Output()
		o.Expect(errOpcfg).NotTo(o.HaveOccurred())
		o.Expect(outputOpcfg).To(o.ContainSubstring("Trace"))

		exutil.By("Patch dns operator with operator logLevel normal")
		patchGlobalResourceAsAdmin(oc, resourceName, cfgOploglevelNormal)
		exutil.By("Check logLevel normal in dns operator")
		outputOpcfg, errOpcfg = oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.operator", "default", "-o=jsonpath={.spec.operatorLogLevel}").Output()
		o.Expect(errOpcfg).NotTo(o.HaveOccurred())
		o.Expect(outputOpcfg).To(o.ContainSubstring("Normal"))

		exutil.By("Check logs of dns operator")
		outputLogs, errLog := oc.AsAdmin().Run("logs").Args("deployment/dns-operator", "-n", "openshift-dns-operator", "-c", "dns-operator").Output()
		o.Expect(errLog).NotTo(o.HaveOccurred())
		o.Expect(outputLogs).To(o.ContainSubstring("level=info"))
	})

	// Bug: OCPBUGS-6829
	g.It("Author:mjoseph-High-63512-Enbaling force_tcp for protocolStrategy field to allow DNS queries to send on TCP to upstream server [Disruptive]", func() {
		var (
			resourceName                = "dns.operator.openshift.io/default"
			upstreamResolverPatch       = "[{\"op\":\"add\", \"path\":\"/spec/upstreamResolvers/protocolStrategy\", \"value\":\"TCP\"}]"
			upstreamResolverPatchRemove = "[{\"op\":\"replace\", \"path\":\"/spec/upstreamResolvers/protocolStrategy\", \"value\":\"\"}]"
			dnsForwardPluginPatch       = "[{\"op\":\"replace\", \"path\":\"/spec/servers\", \"value\":[{\"forwardPlugin\":{\"policy\":\"Sequential\",\"protocolStrategy\": \"TCP\",\"upstreams\":[\"8.8.8.8\"]},\"name\":\"test\",\"zones\":[\"mytest.ocp\"]}]}]"
		)

		exutil.By("1. Check the default dns operator config for “protocol strategy” is none")
		output, err := oc.AsAdmin().Run("get").Args("cm/dns-default", "-n", "openshift-dns", "-o=jsonpath={.data.Corefile}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(output, "force_tcp")).NotTo(o.BeTrue())

		exutil.By("2. Prepare the dns testing node and pod")
		defer deleteDnsOperatorToRestore(oc)
		oneDnsPod := forceOnlyOneDnsPodExist(oc)

		exutil.By("3. Patch dns operator with 'TCP' as protocol strategy for upstreamresolver")
		patchGlobalResourceAsAdmin(oc, resourceName, upstreamResolverPatch)

		exutil.By("4. Check the upstreamresolver for “protocol strategy” is TCP in Corefile of coredns")
		tcp := pollReadDnsCorefile(oc, oneDnsPod, "forward", "-A2", "force_tcp")
		o.Expect(tcp).To(o.ContainSubstring("force_tcp"))
		//remove the patch from upstreamresolver
		patchGlobalResourceAsAdmin(oc, resourceName, upstreamResolverPatchRemove)
		output, err = oc.AsAdmin().Run("get").Args("dns.operator.openshift.io/default", "-o=jsonpath={.spec.upstreamResolvers.protocolStrategy}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.BeEmpty())

		exutil.By("5. Patch dns operator with 'TCP' as protocol strategy for forwardPlugin")
		patchGlobalResourceAsAdmin(oc, resourceName, dnsForwardPluginPatch)

		exutil.By("6. Check the protocol strategy value as 'TCP' in Corefile of coredns under forwardPlugin")
		tcp1 := pollReadDnsCorefile(oc, oneDnsPod, "test", "-A5", "force_tcp")
		o.Expect(tcp1).To(o.ContainSubstring("force_tcp"))
	})
})
