package router

import (
	"github.com/openshift/router-tests-extension/test/testdata"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("load-balancer", compat_otp.KubeConfigPath())

	// incorporate OCP-21599 and 29204 into one
	// OCP-21599:NetworkEdge ingresscontroller can set proper endpointPublishingStrategy in cloud platform
	// OCP-29204:NetworkEdge ingresscontroller can set proper endpointPublishingStrategy in non-cloud platform
	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Critical-21599-ingresscontroller can set proper endpointPublishingStrategy in all platforms", func() {
		compat_otp.By("Get the platform type and check the endpointPublishingStrategy type")
		platformtype := compat_otp.CheckPlatform(oc)
		platforms := map[string]bool{
			"aws":          true,
			"azure":        true,
			"gcp":          true,
			"alibabacloud": true,
			"ibmcloud":     true,
			"powervs":      true,
		}

		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", "openshift-ingress-operator", "ingresscontroller/default", "-o=jsonpath={.status.endpointPublishingStrategy.type}").Output()
		if platforms[platformtype] {
			o.Expect(output).To(o.ContainSubstring("LoadBalancerService"))
		} else {
			o.Expect(output).To(o.ContainSubstring("HostNetwork"))
		}
	})

	// incorporate OCP-24504 and 36891 into one case
	// OCP-24504:NetworkEdge the load balancer scope can be set to Internal when creating ingresscontroller
	// OCP-36891:NetworkEdge ingress operator supports mutating load balancer scope
	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Critical-36891-ingress operator supports mutating load balancer scope", func() {
		// skip on non-cloud platform
		// ibmcloud/powervs has bug https://issues.redhat.com/browse/OCPBUGS-32776
		platformtype := compat_otp.CheckPlatform(oc)
		platforms := map[string]bool{
			"aws":          true,
			"azure":        true,
			"gcp":          true,
			"alibabacloud": true,
		}
		if !platforms[platformtype] {
			g.Skip("Skip for non-cloud platforms and ibmcloud/powervs due to OCPBUGS-32776")
		}
		// skip if private cluster in 4.19+
		if isInternalLBScopeInDefaultIngresscontroller(oc) {
			g.Skip("Skip for private cluster since Internal LB scope in default ingresscontroller")
		}

		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-external.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp36891",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ns            = "openshift-ingress"
			dnsRecordName = ingctrl.name + "-wildcard"
		)

		compat_otp.By("Create custom ingresscontroller with Internal scope")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		// Updating LB scope `External` to `Internal` in the yaml file
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|External|Internal|g' %s`, customTemp)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		err = waitForCustomIngressControllerAvailable(oc, ingctrl.name)
		// check the LB service event if any error before exit
		if err != nil {
			output, _ := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", ns, "service", "router-"+ingctrl.name).Output()
			e2e.Logf("The output of describe LB service: %v", output)
		}
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("ingresscontroller %s conditions not available", ingctrl.name))

		compat_otp.By("Get the Interanl LB ingress ip or hostname")
		// AWS, IBMCloud use hostname, other cloud platforms use ip
		internalLB, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, "service", "router-"+ingctrl.name, "-o=jsonpath={.status.loadBalancer.ingress}").Output()
		e2e.Logf("the internal LB is %v", internalLB)
		if platformtype == "aws" {
			o.Expect(internalLB).To(o.MatchRegexp(`"hostname":.*elb.*amazonaws.com`))
		} else {
			o.Expect(internalLB).To(o.MatchRegexp(`"ip":"10\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}"`))
		}

		compat_otp.By("Updating scope from Internal to External")
		patchScope := `{"spec":{"endpointPublishingStrategy":{"loadBalancer":{"scope":"External"}}}}`
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, patchScope)
		// AWS needs user to delete the LoadBalancer service manually
		if platformtype == "aws" {
			waitForOutputContains(oc, "default", "co/ingress", `{.status.conditions[?(@.type == "Progressing")].message}`, "To effectuate this change, you must delete the service")
			oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "service", "router-"+ingctrl.name).Execute()
		}
		waitForOutputEquals(oc, "openshift-ingress-operator", "dnsrecords/"+dnsRecordName, "{.metadata.generation}", "2")

		compat_otp.By("Ensure the ingress LB is updated and the IP is not private")
		externalLB, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", ns, "service", "router-"+ingctrl.name, "-o=jsonpath={.status.loadBalancer.ingress}").Output()
		e2e.Logf("the external LB is %v", externalLB)
		if platformtype == "aws" {
			o.Expect(externalLB).To(o.MatchRegexp(`"hostname":.*elb.*amazonaws.com`))
		} else {
			o.Expect(externalLB).NotTo(o.MatchRegexp(`"ip":"10\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}"`))
			o.Expect(externalLB).To(o.MatchRegexp(`"ip":"[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}"`))
		}

		compat_otp.By("Ensure the dnsrecord with new LB IP/hostname are published")
		publishStatus, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", "openshift-ingress-operator", "dnsrecord", dnsRecordName, `-o=jsonpath={.status.zones[*].conditions[?(@.type == "Published")].status})`).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(publishStatus).NotTo(o.ContainSubstring("False"))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-High-52837-switching of AWS CLB to NLB without deletion of ingresscontroller", func() {
		// skip if platform is not AWS
		compat_otp.SkipIfPlatformTypeNot(oc, "AWS")
		// skip if private cluster in 4.19+
		if isInternalLBScopeInDefaultIngresscontroller(oc) {
			g.Skip("Skip for private cluster since Internal LB scope in default ingresscontroller")
		}

		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		ns := "openshift-ingress"
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp52837",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("patch the existing custom ingress controller with NLB")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp52837", "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"providerParameters\":{\"aws\":{\"type\":\"NLB\"}}}}}}")
		// the LB svc keep the same but just annotation is updated
		waitForOutputContains(oc, ns, "service/router-"+ingctrl.name, `{.metadata.annotations}`, "aws-load-balancer-type")

		compat_otp.By("check the LB service and ensure the annotations are updated")
		findAnnotation := getAnnotation(oc, ns, "service", "router-"+ingctrl.name)
		e2e.Logf("all annotations are: %v", findAnnotation)
		o.Expect(findAnnotation).To(o.ContainSubstring(`"service.beta.kubernetes.io/aws-load-balancer-type":"nlb"`))
		o.Expect(findAnnotation).NotTo(o.ContainSubstring("aws-load-balancer-proxy-protocol"))

		compat_otp.By("patch the existing custom ingress controller with CLB")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp52837", "{\"spec\":{\"endpointPublishingStrategy\":{\"loadBalancer\":{\"providerParameters\":{\"aws\":{\"type\":\"Classic\"}}}}}}")
		waitForOutputContains(oc, ns, "service/router-"+ingctrl.name, `{.metadata.annotations}`, "aws-load-balancer-proxy-protocol")

		// Classic LB doesn't has explicit "classic" annotation but it needs proxy-protocol annotation
		// so we use "aws-load-balancer-proxy-protocol" to check if using CLB
		compat_otp.By("check the LB service and ensure the annotations are updated")
		findAnnotation = getAnnotation(oc, ns, "service", "router-"+ingctrl.name)
		e2e.Logf("all annotations are: %v", findAnnotation)
		o.Expect(findAnnotation).To(o.ContainSubstring("aws-load-balancer-proxy-protocol"))
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`"service.beta.kubernetes.io/aws-load-balancer-type":"nlb"`))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-High-72126-Support multiple cidr blocks for one NSG rule in the IngressController", func() {
		g.By("Pre-flight check for the platform type")
		compat_otp.SkipIfPlatformTypeNot(oc, "Azure")

		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-azure-cidr.yaml")
		ns := "openshift-ingress"
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp72126",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. Create the custom ingresscontroller with 3995 CIDRs, by default 2 CIDRs are occupied on non private cluster, and 3 more are occupied on profile with windows node")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		err := waitForCustomIngressControllerAvailable(oc, ingctrl.name)
		// check the LB service event if any error before exit
		if err != nil {
			output, _ := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", ns, "service", "router-"+ingctrl.name).Output()
			e2e.Logf("The output of describe LB service: %v", output)
		}
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("ingresscontroller %s conditions not available", ingctrl.name))

		compat_otp.By("2. Check the LB service event to ensure no exceeds maximum number error")
		output1, _ := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", ns, "service", "router-"+ingctrl.name).Output()
		o.Expect(output1).To(o.ContainSubstring(`Ensured load balancer`))
		o.Expect(output1).NotTo(o.ContainSubstring(`exceeds the maximum number of source IP addresses`))

		compat_otp.By("3. Patch the custom ingress controller and add 6 more IPs to allowedSourceRanges")
		jsonPatch := `[{"op":"add", "path": "/spec/endpointPublishingStrategy/loadBalancer/allowedSourceRanges/-", "value":"1.1.32.118/32"},{"op":"add", "path": "/spec/endpointPublishingStrategy/loadBalancer/allowedSourceRanges/-", "value":"1.1.32.120/32"},{"op":"add", "path": "/spec/endpointPublishingStrategy/loadBalancer/allowedSourceRanges/-", "value":"1.1.32.122/32"},{"op":"add", "path": "/spec/endpointPublishingStrategy/loadBalancer/allowedSourceRanges/-", "value":"1.1.32.124/32"},{"op":"add", "path": "/spec/endpointPublishingStrategy/loadBalancer/allowedSourceRanges/-", "value":"1.1.32.126/32"},{"op":"add", "path": "/spec/endpointPublishingStrategy/loadBalancer/allowedSourceRanges/-", "value":"1.1.32.128/32"}]`
		_, err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("-n", ingctrl.namespace, "ingresscontroller", ingctrl.name, "--type=json", "-p", jsonPatch).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("4. Check the error message in event of the Load Balancer service")
		// on some profiles it needs more than 6 seconds until the message appears
		expectedMessage := `exceeds the maximum number of source IP addresses \(400[1-9] > 4000\)`
		err = wait.PollImmediate(3*time.Second, 30*time.Second, func() (bool, error) {
			output2, _ := oc.AsAdmin().WithoutNamespace().Run("describe").Args("-n", ns, "service", "router-"+ingctrl.name).Output()
			if matched, _ := regexp.MatchString(expectedMessage, output2); matched {
				return true, nil
			}
			return false, nil
		})
		compat_otp.AssertWaitPollNoErr(err, fmt.Sprintf("reached max time allowed but the error message doesn't appear", err))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-NonHyperShiftHOST-ROSA-OSD_CCS-High-75439-AWS CLB supports to choose subnets", func() {
		g.By("Pre-flight check for the platform type")
		compat_otp.SkipIfPlatformTypeNot(oc, "AWS")

		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		ns := "openshift-ingress"
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp75439",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. Get public subnets and skip conditionally")
		publicSubnetList := getPublicSubnetList(oc)
		if len(publicSubnetList) < 1 {
			g.Skip("Skipping since no public subnet found")
		}

		compat_otp.By("2. Create the custom ingresscontroller with CLB")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("3. Patch the custom ingresscontroller, then delete the LB svc manually")
		jsonPatch := fmt.Sprintf(`{"spec":{"endpointPublishingStrategy":{"type":"LoadBalancerService","loadBalancer":{"providerParameters":{"type":"AWS","aws":{"type":"Classic","classicLoadBalancer":{"subnets":{"ids":null,"names":[%s]}}}}}}}}`, strings.Join(publicSubnetList, ","))
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPatch)
		waitForOutputContains(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{.status.conditions[?(@.type == "Progressing")].message}`, "To effectuate this change, you must delete the service")
		oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "service", "router-"+ingctrl.name).Execute()
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("4. Ensure the ingress LB svc is provisioned and the subnets annotation is added")
		externalLB := getByJsonPath(oc, ns, "service/router-"+ingctrl.name, "{.status.loadBalancer.ingress}")
		o.Expect(externalLB).To(o.MatchRegexp(`"hostname":.*elb.*amazonaws.com`))
		findAnnotation := getAnnotation(oc, ns, "service", "router-"+ingctrl.name)
		e2e.Logf("all annotations are: %v", findAnnotation)
		o.Expect(findAnnotation).To(o.ContainSubstring("service.beta.kubernetes.io/aws-load-balancer-subnets\":\"" + strings.Replace(strings.Join(publicSubnetList, ","), "\"", "", -1)))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-NonHyperShiftHOST-ROSA-OSD_CCS-High-75440-AWS NLB supports to choose subnets", func() {
		g.By("Pre-flight check for the platform type")
		compat_otp.SkipIfPlatformTypeNot(oc, "AWS")

		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		ns := "openshift-ingress"
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp75440",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. Get public subnets and skip conditionally")
		publicSubnetList := getPublicSubnetList(oc)
		if len(publicSubnetList) < 1 {
			g.Skip("Skipping since no public subnet found")
		}

		compat_otp.By("2. Create the custom ingresscontroller with NLB")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		// Updating LB type from `Classic` to `NLB` in the yaml file
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|Classic|NLB|g' %s`, customTemp)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("3. Annotate and patch the custom ingresscontroller, enable LB svc can be auto deleted")
		annotation := `ingress.operator.openshift.io/auto-delete-load-balancer=`
		err = oc.AsAdmin().WithoutNamespace().Run("annotate").Args("-n", ingctrl.namespace, "ingresscontroller/"+ingctrl.name, annotation, "--overwrite").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		jsonPatch := fmt.Sprintf(`{"spec":{"endpointPublishingStrategy":{"type":"LoadBalancerService","loadBalancer":{"providerParameters":{"type":"AWS","aws":{"type":"NLB","networkLoadBalancer":{"subnets":{"ids":null,"names":[%s]}}}}}}}}`, strings.Join(publicSubnetList, ","))
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPatch)
		// the old svc should be auto deleted in a few seconds and wait the new one has the annotation
		waitForOutputContains(oc, ns, "service/router-"+ingctrl.name, `{.metadata.annotations}`, "service.beta.kubernetes.io/aws-load-balancer-subnets")
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("4. Ensure the ingress LB svc is provisioned and subnets annotation is added")
		externalLB := getByJsonPath(oc, ns, "service/router-"+ingctrl.name, "{.status.loadBalancer.ingress}")
		o.Expect(externalLB).To(o.MatchRegexp(`"hostname":.*elb.*amazonaws.com`))
		findAnnotation := getAnnotation(oc, ns, "service", "router-"+ingctrl.name)
		e2e.Logf("all annotations are: %v", findAnnotation)
		o.Expect(findAnnotation).To(o.ContainSubstring("service.beta.kubernetes.io/aws-load-balancer-subnets\":\"" + strings.Replace(strings.Join(publicSubnetList, ","), "\"", "", -1)))
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-ROSA-OSD_CCS-High-75499-Allocating and updating EIPs on AWS NLB cluster", func() {
		g.By("Pre-flight check for the platform type")
		compat_otp.SkipIfPlatformTypeNot(oc, "AWS")
		// Number of EIPs should be same to number of public subnets
		num := len(getPublicSubnetList(oc))
		if num < 1 {
			g.Skip("Skipping since no public subnet found")
		}

		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		ns := "openshift-ingress"
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp75499",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. Create the required elastic address")
		var eipAllocationsList []string
		defer ensureReleaseElaticIP(oc, &eipAllocationsList)
		eipAllocationsList = allocateElaticIP(oc, num)
		e2e.Logf("The allocated eip list is %s ", eipAllocationsList)

		compat_otp.By("2. Create another set of elastic address")
		var eipAllocationsList1 []string
		defer ensureReleaseElaticIP(oc, &eipAllocationsList1)
		eipAllocationsList1 = allocateElaticIP(oc, num)
		e2e.Logf("The allocated second eip list is %s ", eipAllocationsList1)

		compat_otp.By("3. Create the custom ingresscontroller with NLB")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		// Updating LB type from `Classic` to `NLB` in the yaml file
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|Classic|NLB|g' %s`, customTemp)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("4. Patch the custom ingresscontroller with the EIPs")
		jsonPatch := fmt.Sprintf(`{"spec":{"endpointPublishingStrategy":{"type":"LoadBalancerService","loadBalancer":{"providerParameters":{"type":"AWS","aws":{"type":"NLB","networkLoadBalancer":{"eipAllocations":["%s"]}}}}}}}`, strings.Join(eipAllocationsList, "\",\""))
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPatch)

		compat_otp.By("5: Ensure the status of ingresscontroller which is in Progressing with the below message")
		jsonPath := `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		waitForOutputEquals(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPath, "TrueTrueFalse")
		waitForOutputContains(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{.status.conditions[?(@.type == "Progressing")].message}`, "To effectuate this change, you must delete the service")

		compat_otp.By("6. Delete the LB svc manually and check controller availability")
		oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "service", "router-"+ingctrl.name).Execute()
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("7. Ensure the eip annotation is added and ingress LB svc is provisioned")
		findAnnotation := getAnnotation(oc, ns, "service", "router-"+ingctrl.name)
		o.Expect(findAnnotation).To(o.ContainSubstring("service.beta.kubernetes.io/aws-load-balancer-eip-allocations\":\"" + strings.Replace(strings.Join(eipAllocationsList, ","), "\"", "", -1)))
		externalLB := getByJsonPath(oc, ns, "service/router-"+ingctrl.name, "{.status.loadBalancer.ingress}")
		o.Expect(externalLB).To(o.MatchRegexp(`"hostname":.*elb.*amazonaws.com`))

		compat_otp.By("8: Ensure the status of ingresscontroller is not degraded")
		waitForOutputEquals(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPath, "TrueFalseFalse")

		compat_otp.By("9:Patch the controller with a new set of EIP and observer the status of ingresscontroller is again in Progressing with the below message")
		jsonPatch1 := fmt.Sprintf(`{"spec":{"endpointPublishingStrategy":{"type":"LoadBalancerService","loadBalancer":{"providerParameters":{"type":"AWS","aws":{"type":"NLB","networkLoadBalancer":{"eipAllocations":["%s"]}}}}}}}`, strings.Join(eipAllocationsList1, "\",\""))
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPatch1)
		waitForOutputEquals(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPath, "TrueTrueFalse")
		waitForOutputContains(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{.status.conditions[?(@.type == "Progressing")].message}`, "To effectuate this change, you must delete the service")

		compat_otp.By("10. Again delete the LB svc manually and check controller availability")
		oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "service", "router-"+ingctrl.name).Execute()
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("11. Ensure the new eip annotation is added and ingress LB svc is provisioned")
		findAnnotation1 := getAnnotation(oc, ns, "service", "router-"+ingctrl.name)
		o.Expect(findAnnotation1).To(o.ContainSubstring("service.beta.kubernetes.io/aws-load-balancer-eip-allocations\":\"" + strings.Replace(strings.Join(eipAllocationsList1, ","), "\"", "", -1)))
		newExternalLB := getByJsonPath(oc, ns, "service/router-"+ingctrl.name, "{.status.loadBalancer.ingress}")
		o.Expect(newExternalLB).To(o.MatchRegexp(`"hostname":.*elb.*amazonaws.com`))

		compat_otp.By("12: Ensure the status of ingresscontroller is now working fine")
		waitForOutputEquals(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPath, "TrueFalseFalse")
	})

	g.It("Author:mjoseph-NonHyperShiftHOST-ROSA-OSD_CCS-High-75617-Update with 'auto-delete-loadbalancer' annotation and unmanaged EIP allocation annotation on AWS NLB cluster", func() {
		g.By("Pre-flight check for the platform type")
		compat_otp.SkipIfPlatformTypeNot(oc, "AWS")
		// The number of EIPs should be same to number of public subnets
		num := len(getPublicSubnetList(oc))
		if num < 1 {
			g.Skip("Skipping since no public subnet found")
		}

		buildPruningBaseDir := testdata.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-clb.yaml")
		ns := "openshift-ingress"

		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp75617one",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrl2 = ingressControllerDescription{
				name:      "ocp75617two",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. Create the required elastic address")
		var eipAllocationsList []string
		defer ensureReleaseElaticIP(oc, &eipAllocationsList)
		eipAllocationsList = allocateElaticIP(oc, num)
		e2e.Logf("The allocated eip list is %s ", eipAllocationsList)

		compat_otp.By("2. Create the custom ingresscontroller with NLB")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		// Updating LB type from `Classic` to `NLB` in the yaml file
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|Classic|NLB|g' %s`, customTemp)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("3. Annotate and patch the custom ingresscontroller, enabling LB svc to be auto deleted")
		setAnnotationAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `ingress.operator.openshift.io/auto-delete-load-balancer=`)
		jsonPatch := fmt.Sprintf(`{"spec":{"endpointPublishingStrategy":{"type":"LoadBalancerService","loadBalancer":{"providerParameters":{"type":"AWS","aws":{"type":"NLB","networkLoadBalancer":{"eipAllocations":["%s"]}}}}}}}`, strings.Join(eipAllocationsList, "\",\""))
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPatch)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		compat_otp.By("4. Ensure the ingress LB svc is provisioned and eip annotation is added")
		externalLB := getByJsonPath(oc, ns, "service/router-"+ingctrl.name, "{.status.loadBalancer.ingress}")
		o.Expect(externalLB).To(o.MatchRegexp(`"hostname":.*elb.*amazonaws.com`))
		findAnnotation := getAnnotation(oc, ns, "service", "router-"+ingctrl.name)
		o.Expect(findAnnotation).To(o.ContainSubstring("service.beta.kubernetes.io/aws-load-balancer-eip-allocations\":\"" + strings.Replace(strings.Join(eipAllocationsList, ","), "\"", "", -1)))

		compat_otp.By("5: Ensure the status of ingresscontroller is not degraded")
		jsonPath := `{.status.conditions[?(@.type=="Available")].status}{.status.conditions[?(@.type=="Progressing")].status}{.status.conditions[?(@.type=="Degraded")].status}`
		waitForOutputEquals(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, jsonPath, "TrueFalseFalse")

		compat_otp.By("6. Create a new set of elastic address")
		var eipAllocationsList1 []string
		defer ensureReleaseElaticIP(oc, &eipAllocationsList1)
		eipAllocationsList1 = allocateElaticIP(oc, num)
		e2e.Logf("The allocated second eip list is %s ", eipAllocationsList1)

		compat_otp.By("7. Create another custom ingresscontroller with NLB")
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl2.name)

		compat_otp.By("8. Set an EIP annotation on the LB service directly")
		setAnnotationAsAdmin(oc, ns, "svc/router-"+ingctrl2.name, `service.beta.kubernetes.io/aws-load-balancer-eip-allocations=`+strings.Join(eipAllocationsList1, ","))
		findAnnotation1 := getAnnotation(oc, ns, "service", "router-"+ingctrl2.name)
		o.Expect(findAnnotation1).To(o.ContainSubstring(`"service.beta.kubernetes.io/aws-load-balancer-eip-allocations":"` + strings.Join(eipAllocationsList1, ",") + "\""))

		compat_otp.By("9: Ensure the status of ingresscontroller is Progressing with the below message")
		waitForOutputEquals(oc, ingctrl2.namespace, "ingresscontroller/"+ingctrl2.name, jsonPath, "TrueTrueFalse")
		waitForOutputContains(oc, ingctrl2.namespace, "ingresscontroller/"+ingctrl2.name, `{.status.conditions[?(@.type == "Progressing")].message}`, "To effectuate this change, you must delete the service")

		compat_otp.By("10. Patch the custom ingresscontroller with same EIP values")
		jsonPatch2 := fmt.Sprintf(`{"spec":{"endpointPublishingStrategy":{"type":"LoadBalancerService","loadBalancer":{"providerParameters":{"type":"AWS","aws":{"type":"NLB","networkLoadBalancer":{"eipAllocations":["%s"]}}}}}}}`, strings.Join(eipAllocationsList1, "\",\""))
		patchResourceAsAdmin(oc, ingctrl2.namespace, "ingresscontroller/"+ingctrl2.name, jsonPatch2)
		ensureCustomIngressControllerAvailable(oc, ingctrl2.name)

		compat_otp.By("11. Ensure the ingress LB svc is provisioned")
		externalLB2 := getByJsonPath(oc, ns, "service/router-"+ingctrl2.name, "{.status.loadBalancer.ingress}")
		o.Expect(externalLB2).To(o.MatchRegexp(`"hostname":.*elb.*amazonaws.com`))

		compat_otp.By("12: Ensure the status of ingresscontroller is not degraded after the patching")
		waitForOutputEquals(oc, ingctrl2.namespace, "ingresscontroller/"+ingctrl2.name, jsonPath, "TrueFalseFalse")
	})
})
