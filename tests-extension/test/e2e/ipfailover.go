package router

import (
    "github.com/openshift/router-tests-extension/test/e2e/testdata"
	"path/filepath"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("router-ipfailover", exutil.KubeConfigPath())
	var HAInterfaces = "br-ex"

	g.BeforeEach(func() {
		g.By("Check platforms")
		platformtype := exutil.CheckPlatform(oc)
		platforms := map[string]bool{
			// 'None' also for Baremetal
			"none":      true,
			"baremetal": true,
			"vsphere":   true,
			"openstack": true,
			"nutanix":   true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-supported platform")
		}

		g.By("check whether there are two worker nodes present for testing hostnetwork")
		workerNodeCount, _ := exactNodeDetails(oc)
		if workerNodeCount < 2 {
			g.Skip("Skipping as we need two worker nodes")
		}

		g.By("check the cluster has remote worker profile")
		remoteWorkerDetails, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("nodes", "-l", "kubernetes.io/hostname").Output()
		if strings.Contains(remoteWorkerDetails, "remote-worker") {
			g.Skip("Skip as ipfailover currently doesn't support on remote-worker profile")
		}

		g.By("check whether the cluster is not ipv6 single stack")
		stacktype := exutil.GetIPVersionStackType(oc)
		if stacktype == "ipv6single" {
			g.Skip("Skip as ipfailover currently doesn't support ipv6 single stack")
		}

	})

	g.JustBeforeEach(func() {
		g.By("Check network type")
		networkType := exutil.CheckNetworkType(oc)
		if strings.Contains(networkType, "openshiftsdn") {
			HAInterfaces = "ens3"
		}
	})

	// author: hongli@redhat.com
	// might conflict with other ipfailover cases so set it as Serial
	g.It("Author:hongli-NonHyperShiftHOST-ConnectedOnly-Critical-41025-support to deploy ipfailover [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ipfailover.yaml")
		var (
			ipf = ipfailoverDescription{
				name:        "ipf-41025",
				namespace:   "",
				image:       "",
				HAInterface: HAInterfaces,
				template:    customTemp,
			}
		)

		g.By("get pull spec of ipfailover image from payload")
		ipf.image = getImagePullSpecFromPayload(oc, "keepalived-ipfailover")
		ipf.namespace = oc.Namespace()
		ns := oc.Namespace()
		g.By("create ipfailover deployment and ensure one of pod enter MASTER state")
		ipf.create(oc, ns)
		unicastIPFailover(oc, ns, ipf.name)
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		podName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		ensureIpfailoverMasterBackup(oc, ns, podName)
	})

	// author: mjoseph@redhat.com
	// might conflict with other ipfailover cases so set it as Serial
	g.It("Author:mjoseph-NonHyperShiftHOST-ConnectedOnly-Medium-41027-pod and service automatically switched over to standby when master fails [Disruptive]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ipfailover.yaml")
		var (
			ipf = ipfailoverDescription{
				name:        "ipf-41027",
				namespace:   "",
				image:       "",
				HAInterface: HAInterfaces,
				template:    customTemp,
			}
		)
		g.By("1. Get pull spec of ipfailover image from payload")
		ipf.image = getImagePullSpecFromPayload(oc, "keepalived-ipfailover")
		ipf.namespace = oc.Namespace()
		ns := oc.Namespace()
		g.By("2. Create ipfailover deployment and ensure one of pod enter MASTER state")
		ipf.create(oc, ns)
		unicastIPFailover(oc, ns, ipf.name)
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		podNames := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		ensureIpfailoverMasterBackup(oc, ns, podNames)

		g.By("3. Set the HA virtual IP for the failover group")
		ipv4Address := getPodIP(oc, ns, podNames[0])
		virtualIP := replaceIPOctet(ipv4Address, 3, "100")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_VIRTUAL_IPS="+virtualIP)

		g.By("4. Verify the HA virtual ip ENV variable")
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		newPodName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		masterNode, _ := ensureIpfailoverMasterBackup(oc, ns, newPodName)
		checkenv := pollReadPodData(oc, ns, newPodName[0], "/usr/bin/env ", "OPENSHIFT_HA_VIRTUAL_IPS")
		o.Expect(checkenv).To(o.ContainSubstring("OPENSHIFT_HA_VIRTUAL_IPS=" + virtualIP))

		g.By("5. Find the primary and the secondary pod using the virtual IP")
		primaryPod := getVipOwnerPod(oc, ns, newPodName, virtualIP)
		o.Expect(masterNode).To(o.ContainSubstring(primaryPod))

		g.By("6. Restarting the ipfailover primary pod")
		err := oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "pod", primaryPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("7. Verify the virtual IP is floated onto the new MASTER node")
		newPodName1 := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		newMasterNode, _ := ensureIpfailoverMasterBackup(oc, ns, newPodName1)
		waitForPrimaryPod(oc, ns, newMasterNode, virtualIP)
	})

	// author: mjoseph@redhat.com
	// might conflict with other ipfailover cases so set it as Serial
	g.It("Author:mjoseph-NonHyperShiftHOST-ConnectedOnly-Medium-41028-ipfailover configuration can be customized by ENV [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ipfailover.yaml")
		var (
			ipf = ipfailoverDescription{
				name:        "ipf-41028",
				namespace:   "",
				image:       "",
				HAInterface: HAInterfaces,
				template:    customTemp,
			}
		)

		g.By("get pull spec of ipfailover image from payload")
		ipf.image = getImagePullSpecFromPayload(oc, "keepalived-ipfailover")
		ipf.namespace = oc.Namespace()
		ns := oc.Namespace()
		g.By("create ipfailover deployment and ensure one of pod enter MASTER state")
		ipf.create(oc, ns)
		unicastIPFailover(oc, ns, ipf.name)
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")

		g.By("set the HA virtual IP for the failover group")
		podNames := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		ipv4Address := getPodIP(oc, ns, podNames[0])
		virtualIP := replaceIPOctet(ipv4Address, 3, "100")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_VIRTUAL_IPS="+virtualIP)

		g.By("set other ipfailover env varibales")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_CONFIG_NAME=ipfailover")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_VIP_GROUPS=4")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_MONITOR_PORT=30061")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_VRRP_ID_OFFSET=2")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_REPLICA_COUNT=3")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `OPENSHIFT_HA_USE_UNICAST=true`)
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `OPENSHIFT_HA_IPTABLES_CHAIN=OUTPUT`)
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `OPENSHIFT_HA_NOTIFY_SCRIPT=/etc/keepalive/mynotifyscript.sh`)
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `OPENSHIFT_HA_CHECK_SCRIPT=/etc/keepalive/mycheckscript.sh`)
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `OPENSHIFT_HA_PREEMPTION=preempt_delay 600`)
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_CHECK_INTERVAL=3")

		g.By("verify the HA virtual ip ENV variable")
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		newPodName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		ensureIpfailoverMasterBackup(oc, ns, newPodName)
		checkenv := pollReadPodData(oc, ns, newPodName[0], "/usr/bin/env ", "OPENSHIFT_HA_VIRTUAL_IPS")
		o.Expect(checkenv).To(o.ContainSubstring("OPENSHIFT_HA_VIRTUAL_IPS=" + virtualIP))

		g.By("check the ipfailover configurations and verify the other ENV variables")
		result := describePodResource(oc, newPodName[0], ns)
		o.Expect(result).To(o.ContainSubstring("OPENSHIFT_HA_VIP_GROUPS:         4"))
		o.Expect(result).To(o.ContainSubstring("OPENSHIFT_HA_CONFIG_NAME:        ipfailover"))
		o.Expect(result).To(o.ContainSubstring("OPENSHIFT_HA_MONITOR_PORT:       30061"))
		o.Expect(result).To(o.ContainSubstring("OPENSHIFT_HA_VRRP_ID_OFFSET:     2"))
		o.Expect(result).To(o.ContainSubstring("OPENSHIFT_HA_REPLICA_COUNT:      3"))
		o.Expect(result).To(o.ContainSubstring(`OPENSHIFT_HA_USE_UNICAST:        true`))
		o.Expect(result).To(o.ContainSubstring(`OPENSHIFT_HA_IPTABLES_CHAIN:     OUTPUT`))
		o.Expect(result).To(o.ContainSubstring(`OPENSHIFT_HA_NOTIFY_SCRIPT:      /etc/keepalive/mynotifyscript.sh`))
		o.Expect(result).To(o.ContainSubstring(`OPENSHIFT_HA_CHECK_SCRIPT:       /etc/keepalive/mycheckscript.sh`))
		o.Expect(result).To(o.ContainSubstring(`OPENSHIFT_HA_PREEMPTION:         preempt_delay 600`))
		o.Expect(result).To(o.ContainSubstring("OPENSHIFT_HA_CHECK_INTERVAL:     3"))
		o.Expect(result).To(o.ContainSubstring("OPENSHIFT_HA_VIRTUAL_IPS:        " + virtualIP))
	})

	// author: mjoseph@redhat.com
	// might conflict with other ipfailover cases so set it as Serial
	g.It("Author:mjoseph-NonHyperShiftHOST-ConnectedOnly-Medium-41029-ipfailover can support up to a maximum of 255 VIPs for the entire cluster [Serial]", func() {
		if exutil.CheckPlatform(oc) == "nutanix" {
			g.Skip("This test will not works for Nutanix")
		}
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ipfailover.yaml")
		var (
			ipf = ipfailoverDescription{
				name:        "ipf-41029",
				namespace:   "",
				image:       "",
				HAInterface: HAInterfaces,
				template:    customTemp,
			}
		)

		g.By("get pull spec of ipfailover image from payload")
		ipf.image = getImagePullSpecFromPayload(oc, "keepalived-ipfailover")
		ipf.namespace = oc.Namespace()
		ns := oc.Namespace()
		g.By("create ipfailover deployment and ensure one of pod enter MASTER state")
		ipf.create(oc, ns)
		unicastIPFailover(oc, ns, ipf.name)
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		podName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		ensureIpfailoverMasterBackup(oc, ns, podName)

		g.By("add some VIP configuration for the failover group")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_VRRP_ID_OFFSET=0")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_VIP_GROUPS=238")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `OPENSHIFT_HA_VIRTUAL_IPS=192.168.254.1-255`)

		g.By("verify from the ipfailover pod, the 255 VIPs are added")
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		newPodName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		checkenv := pollReadPodData(oc, ns, newPodName[0], "/usr/bin/env ", "OPENSHIFT_HA_VIP_GROUPS")
		o.Expect(checkenv).To(o.ContainSubstring("OPENSHIFT_HA_VIP_GROUPS=238"))
	})

	// author: mjoseph@redhat.com
	// might conflict with other ipfailover cases so set it as Serial
	g.It("Author:mjoseph-NonHyperShiftHOST-ConnectedOnly-High-41030-preemption strategy for keepalived ipfailover [Disruptive]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ipfailover.yaml")
		var (
			ipf = ipfailoverDescription{
				name:        "ipf-41030",
				namespace:   "",
				image:       "",
				HAInterface: HAInterfaces,
				template:    customTemp,
			}
		)
		g.By("1. Get pull spec of ipfailover image from payload")
		ipf.image = getImagePullSpecFromPayload(oc, "keepalived-ipfailover")
		ipf.namespace = oc.Namespace()
		ns := oc.Namespace()
		g.By("2. Create ipfailover deployment and ensure one of pod enter MASTER state")
		ipf.create(oc, ns)
		unicastIPFailover(oc, ns, ipf.name)
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		podName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		ensureIpfailoverMasterBackup(oc, ns, podName)

		g.By("3. Set the HA virtual IP for the failover group")
		podNames := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		ipv4Address := getPodIP(oc, ns, podNames[0])
		virtualIP := replaceIPOctet(ipv4Address, 3, "100")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, "OPENSHIFT_HA_VIRTUAL_IPS="+virtualIP)

		g.By("4. Verify the HA virtual ip ENV variable")
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		newPodName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		master, backup := ensureIpfailoverMasterBackup(oc, ns, newPodName)
		checkenv := pollReadPodData(oc, ns, newPodName[0], "/usr/bin/env ", "OPENSHIFT_HA_VIRTUAL_IPS")
		o.Expect(checkenv).To(o.ContainSubstring("OPENSHIFT_HA_VIRTUAL_IPS=" + virtualIP))
		checkenv1 := pollReadPodData(oc, ns, newPodName[0], "/usr/bin/env ", "OPENSHIFT_HA_PREEMPTION")
		o.Expect(checkenv1).To(o.ContainSubstring("nopreempt"))

		g.By("5. Find the primary and the secondary pod")
		primaryPod := getVipOwnerPod(oc, ns, newPodName, virtualIP)
		secondaryPod := slicingElement(primaryPod, newPodName)
		o.Expect(master).To(o.ContainSubstring(primaryPod))
		o.Expect(backup).To(o.ContainSubstring(secondaryPod[0]))

		g.By("6. Restarting the ipfailover primary pod")
		err := oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "pod", primaryPod).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("7. Verify whether the other pod becomes primary and it has the VIP")
		waitForPrimaryPod(oc, ns, secondaryPod[0], virtualIP)

		g.By("8. Now set the preemption delay timer of 120s for the failover group")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `OPENSHIFT_HA_PREEMPTION=preempt_delay 120`)
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		newPodName1 := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		new_master, new_backup := ensureIpfailoverMasterBackup(oc, ns, newPodName1)
		checkenv2 := pollReadPodData(oc, ns, newPodName1[0], "/usr/bin/env ", "OPENSHIFT_HA_PREEMPTION")
		o.Expect(checkenv2).To(o.ContainSubstring("preempt_delay 120"))

		g.By("9. Again restart the ipfailover primary(master) pod")
		// the below steps will make the 'new_backup' pod the master
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "pod", new_master).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By("10. Verify the newly created pod preempts the exiting primary after the delay expires")
		latestpods := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		// removing the existing master pod from the latest pods
		futurePrimaryPod := slicingElement(new_backup, latestpods)
		// waiting till the preempt delay 120 seconds expires
		time.Sleep(125 * time.Second)
		// confirming the newer pod is the master by checking the VIP
		waitForPrimaryPod(oc, ns, futurePrimaryPod[0], virtualIP)
	})

	// author: mjoseph@redhat.com
	// might conflict with other ipfailover cases so set it as Serial
	g.It("Author:mjoseph-NonHyperShiftHOST-ConnectedOnly-Medium-49214-Excluding the existing VRRP cluster ID from ipfailover deployments [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ipfailover.yaml")
		var (
			ipf = ipfailoverDescription{
				name:        "ipf-49214",
				namespace:   "",
				image:       "",
				HAInterface: HAInterfaces,
				template:    customTemp,
			}
		)

		g.By("get pull spec of ipfailover image from payload")
		ipf.image = getImagePullSpecFromPayload(oc, "keepalived-ipfailover")
		ipf.namespace = oc.Namespace()
		ns := oc.Namespace()
		g.By("create ipfailover deployment and ensure one of pod enter MASTER state")
		ipf.create(oc, ns)
		unicastIPFailover(oc, ns, ipf.name)
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		podName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		ensureIpfailoverMasterBackup(oc, ns, podName)

		g.By("add 254 VIPs for the failover group")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `OPENSHIFT_HA_VIRTUAL_IPS=192.168.254.1-254`)

		g.By("Exclude VIP '9' from the ipfailover group")
		getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		setEnvVariable(oc, ns, "deploy/"+ipf.name, `HA_EXCLUDED_VRRP_IDS=9`)

		g.By("verify from the ipfailover pod, the excluded VRRP_ID is configured")
		ensurePodWithLabelReady(oc, ns, "ipfailover=hello-openshift")
		newPodName := getPodListByLabel(oc, ns, "ipfailover=hello-openshift")
		checkenv := pollReadPodData(oc, ns, newPodName[0], "/usr/bin/env ", "HA_EXCLUDED_VRRP_IDS")
		o.Expect(checkenv).To(o.ContainSubstring("HA_EXCLUDED_VRRP_IDS=9"))

		g.By("verify the excluded VIP is removed from the router_ids of ipfailover pods")
		routerIds := pollReadPodData(oc, ns, newPodName[0], `cat /etc/keepalived/keepalived.conf`, `virtual_router_id`)
		o.Expect(routerIds).NotTo(o.ContainSubstring(`virtual_router_id 9`))
	})
})
