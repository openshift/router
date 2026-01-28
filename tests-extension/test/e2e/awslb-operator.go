package router

import (
	"github.com/openshift/router-tests-extension/test/testdata"
	"path/filepath"
	"strings"

	"github.com/openshift/origin/test/extended/util/compat_otp/architecture"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	clusterinfra "github.com/openshift/origin/test/extended/util/compat_otp/clusterinfra"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-network-edge][OTP][Level0] Network_Edge Component_ALBO", func() {
	defer g.GinkgoRecover()

	var (
		oc                = compat_otp.NewCLI("awslb", compat_otp.KubeConfigPath())
		operatorNamespace = "aws-load-balancer-operator"
		operatorPodLabel  = "control-plane=controller-manager"
	)

	g.BeforeEach(func() {
		// skip ARM64 arch
		architecture.SkipNonAmd64SingleArch(oc)
		compat_otp.SkipIfPlatformTypeNot(oc, "AWS")
		// skip if us-gov region
		region, err := compat_otp.GetAWSClusterRegion(oc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Contains(region, "us-gov") {
			g.Skip("Skipping for the aws cluster in us-gov region.")
		}
		// skip if OLM capability is disabled
		compat_otp.SkipNoOLMCore(oc)

		compat_otp.By("Deploy AWS Load Balancer konflux FBC")
		createAWSLoadBalancerCatalogSource(oc)

		output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("-n", operatorNamespace, "pod", "-l", operatorPodLabel).Output()
		if !strings.Contains(output, "Running") {
			createAWSLoadBalancerOperator(oc)
		}
	})

	g.AfterEach(func() {
		if compat_otp.IsSTSCluster(oc) {
			e2e.Logf("This is STS cluster so clear up AWS IAM resources as well as albo namespace")
			clearUpAlbOnStsCluster(oc)
		}
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-ConnectedOnly-High-51189-Install aws-load-balancer-operator and controller [Serial]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router", "awslb")
			AWSLBController     = filepath.Join(buildPruningBaseDir, "awslbcontroller.yaml")
			operandCRName       = "cluster"
			operandPodLabel     = "app.kubernetes.io/name=aws-load-balancer-operator"
		)

		compat_otp.By("Ensure the operartor pod is ready")
		ensurePodWithLabelReady(oc, operatorNamespace, operatorPodLabel)

		compat_otp.By("Create CR AWSLoadBalancerController")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("awsloadbalancercontroller", operandCRName).Output()
		_, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", AWSLBController).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if compat_otp.IsSTSCluster(oc) {
			patchAlbControllerWithRoleArn(oc, operatorNamespace)
		}
		ensurePodWithLabelReady(oc, operatorNamespace, operandPodLabel)
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-ConnectedOnly-Medium-51191-Provision ALB by creating an ingress [Serial]", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router", "awslb")
			AWSLBController     = filepath.Join(buildPruningBaseDir, "awslbcontroller.yaml")
			podsvc              = filepath.Join(buildPruningBaseDir, "podsvc.yaml")
			ingress             = filepath.Join(buildPruningBaseDir, "ingress-test.yaml")
			operandCRName       = "cluster"
			operandPodLabel     = "app.kubernetes.io/name=aws-load-balancer-operator"
		)

		compat_otp.By("Ensure the operartor pod is ready")
		ensurePodWithLabelReady(oc, operatorNamespace, operatorPodLabel)

		compat_otp.By("Create CR AWSLoadBalancerController")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("awsloadbalancercontroller", operandCRName).Output()
		_, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", AWSLBController).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if compat_otp.IsSTSCluster(oc) {
			patchAlbControllerWithRoleArn(oc, operatorNamespace)
		}
		ensurePodWithLabelReady(oc, operatorNamespace, operandPodLabel)

		compat_otp.By("Create user project, pod and NodePort service")
		oc.SetupProject()
		createResourceFromFile(oc, oc.Namespace(), podsvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server")

		compat_otp.By("create ingress with alb annotation in the project and ensure the alb is provsioned")
		// need to ensure the ingress is deleted before deleting the CR AWSLoadBalancerController
		// otherwise the lb resources cannot be cleared
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("ingress/ingress-test", "-n", oc.Namespace()).Output()
		createResourceFromFile(oc, oc.Namespace(), ingress)
		// if outpost cluster we need to add annotation to ingress
		if clusterinfra.IsAwsOutpostCluster(oc) {
			annotation := "alb.ingress.kubernetes.io/subnets=" + getOutpostSubnetId(oc)
			_, err := oc.AsAdmin().WithoutNamespace().Run("annotate").Args("ingress", "ingress-test", annotation, "-n", oc.Namespace()).Output()
			o.Expect(err).NotTo(o.HaveOccurred())
		}
		output, err := oc.Run("get").Args("ingress").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("ingress-test"))
		// ALB provision relies on the number of subnets (should >=2)
		internalSubnets, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("awsloadbalancercontroller/cluster", "-ojsonpath={.status.subnets.internal}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("The discovered subnets are: %v", internalSubnets)
		if len(internalSubnets) > 1 {
			waitForLoadBalancerProvision(oc, oc.Namespace(), "ingress-test")
		} else {
			output, err := oc.Run("describe").Args("ingress", "ingress-test").Output()
			o.Expect(err).NotTo(o.HaveOccurred())
			e2e.Logf("Number of subnets doesn't meet the requirement, see event of ingress: %v", output)
		}
	})
})
