package router

import (
	"github.com/openshift/router/test/e2e/testdata"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/openshift/openshift-tests-private/test/extended/util/architecture"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_ExtDNS", func() {
	defer g.GinkgoRecover()

	var (
		oc                = exutil.NewCLI("externaldns", exutil.KubeConfigPath())
		operatorNamespace = "external-dns-operator"
		operatorLabel     = "name=external-dns-operator"
		operandLabelKey   = "app.kubernetes.io/instance="
		addLabel          = "external-dns.mydomain.org/publish=yes"
		delLabel          = "external-dns.mydomain.org/publish-"
		recordsReadyLog   = "All records are already up to date"
	)

	g.BeforeEach(func() {
		// skip ARM64 arch
		architecture.SkipNonAmd64SingleArch(oc)
		// skip if no catalog source
		skipMissingCatalogsource(oc)
	})

	// author: hongli@redhat.com
	g.It("[Level0] Author:hongli-ConnectedOnly-ROSA-OSD_CCS-High-48138-Support External DNS on AWS platform", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router", "extdns")
			sampleAWS           = filepath.Join(buildPruningBaseDir, "sample-aws-rt.yaml")
			crName              = "sample-aws-rt"
			operandLabel        = operandLabelKey + crName
			routeNamespace      = "openshift-ingress-canary"
			routeName           = "canary"
		)

		exutil.By("Ensure the case is runnable on the cluster")
		exutil.SkipIfPlatformTypeNot(oc, "AWS")
		if exutil.IsSTSCluster(oc) {
			g.Skip("Skip on STS cluster")
		}
		baseDomain, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.baseDomain}").Output()
		// this case cannot be executed on a shared vpc cluster
		privateZoneIAMRole, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.platform.aws}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Contains(privateZoneIAMRole, "privateZoneIAMRole") {
			g.Skip("Skipping since this case will not run on a shared vpc cluster")
		}
		createExternalDNSOperator(oc)

		exutil.By("Create CR ExternalDNS sample-aws-rt and ensure operand pod is ready")
		ensurePodWithLabelReady(oc, operatorNamespace, operatorLabel)
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("externaldns", crName).Output()
		// To avoid connection refused flake error, as the controller CR creation needs extra prepare time after the operator pod is ready
		time.Sleep(3 * time.Second)
		sedCmd := fmt.Sprintf(`sed -i'' -e 's/basedomain/%s/g' %s`, baseDomain, sampleAWS)
		_, err = exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", sampleAWS).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, operatorNamespace, operandLabel)
		ensureLogsContainString(oc, operatorNamespace, operandLabel, recordsReadyLog)

		exutil.By("Add label to canary route, ensure ExternalDNS added the record")
		defer oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, delLabel, "--overwrite").Output()
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, addLabel).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Desired change: CREATE external-dns-canary-openshift-ingress-canary")

		exutil.By("Remove label from the canary route, ensure ExternalDNS deleted the record")
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, delLabel, "--overwrite").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Desired change: DELETE external-dns-canary-openshift-ingress-canary")
	})

	// author: hongli@redhat.com
	g.It("ConnectedOnly-ARO-Author:hongli-High-48139-Support External DNS on Azure DNS provider", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router", "extdns")
			sampleAzure         = filepath.Join(buildPruningBaseDir, "sample-azure-rt.yaml")
			crName              = "sample-azure-rt"
			operandLabel        = operandLabelKey + crName
			routeNamespace      = "openshift-ingress-canary"
			routeName           = "canary"
		)

		exutil.By("Ensure the case is runnable on the cluster")
		exutil.SkipIfPlatformTypeNot(oc, "Azure")
		if exutil.IsSTSCluster(oc) {
			g.Skip("Skip on STS cluster")
		}
		cloudName, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("infrastructure", "cluster", "-o=jsonpath={.status.platformStatus.azure.cloudName}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.ToLower(cloudName) == "azureusgovernmentcloud" {
			g.Skip("Skip on MAG (Microsoft Azure Gov) cloud")
		}
		zoneID, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.privateZone.id}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(zoneID, "openshift") {
			g.Skip("Skip since no valid DNS privateZone is configured in this cluster")
		}
		createExternalDNSOperator(oc)

		exutil.By("Create CR ExternalDNS sample-azure-svc with invalid zone ID")
		ensurePodWithLabelReady(oc, operatorNamespace, operatorLabel)
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("externaldns", crName).Output()
		time.Sleep(3 * time.Second)
		_, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", sampleAzure).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, operatorNamespace, operandLabel)
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Found 0 Azure DNS zone")
		operandPod := getPodListByLabel(oc, operatorNamespace, operandLabel)

		exutil.By("Patch externaldns with valid privateZone ID and wait until new operand pod ready")
		patchStr := "[{\"op\":\"replace\",\"path\":\"/spec/zones/0\",\"value\":" + zoneID + "}]"
		patchGlobalResourceAsAdmin(oc, "externaldnses.externaldns.olm.openshift.io/"+crName, patchStr)
		err = waitForResourceToDisappear(oc, operatorNamespace, "pod/"+operandPod[0])
		exutil.AssertWaitPollNoErr(err, fmt.Sprintf("resource %v does not disapper", "pod/"+operandPod[0]))
		ensurePodWithLabelReady(oc, operatorNamespace, operandLabel)
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Found 1 Azure Private DNS zone")

		exutil.By("Add label to canary route, ensure ExternalDNS added the record")
		defer oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, delLabel, "--overwrite").Output()
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, addLabel).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Updating TXT record named 'external-dns-canary")

		exutil.By("Remove label from the canary route, ensure ExternalDNS deleted the record")
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, delLabel, "--overwrite").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Deleting TXT record named 'external-dns-canary")
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-ConnectedOnly-High-48140-Support External DNS on GCP DNS provider", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router", "extdns")
			sampleGCP           = filepath.Join(buildPruningBaseDir, "sample-gcp-svc.yaml")
			crName              = "sample-gcp-svc"
			operandLabel        = operandLabelKey + crName
			serviceNamespace    = "openshift-ingress-canary"
			serviceName         = "ingress-canary"
		)

		exutil.By("Ensure the case is runnable on the cluster")
		exutil.SkipIfPlatformTypeNot(oc, "GCP")
		if exutil.IsSTSCluster(oc) {
			g.Skip("Skip on STS cluster")
		}
		zoneID, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.privateZone.id}").Output()
		if !strings.Contains(zoneID, "private") {
			g.Skip("Skip since no valid DNS privateZone is configured in this cluster")
		}
		// Extract zone name from the full path format (4.20+) or use as-is (4.19 and earlier)
		zoneName := extractGCPZoneName(zoneID)
		createExternalDNSOperator(oc)
		baseDomain := getBaseDomain(oc)

		exutil.By("Create CR ExternalDNS sample-gcp-svc and ensure operand pod is ready")
		ensurePodWithLabelReady(oc, operatorNamespace, operatorLabel)
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("externaldns", crName).Output()
		time.Sleep(3 * time.Second)
		_, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", sampleGCP).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, operatorNamespace, operandLabel)
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "No zones in the project")
		operandPod := getPodListByLabel(oc, operatorNamespace, operandLabel)

		exutil.By("Patch externaldns with valid privateZone ID and wait until new operand pod ready")
		patchStr := "[{\"op\":\"replace\",\"path\":\"/spec/source/fqdnTemplate/0\",\"value\":'{{.Name}}." + baseDomain + "'},{\"op\":\"replace\",\"path\":\"/spec/zones/0\",\"value\":" + zoneName + "}]"
		patchGlobalResourceAsAdmin(oc, "externaldnses.externaldns.olm.openshift.io/"+crName, patchStr)
		waitErr := waitForResourceToDisappear(oc, operatorNamespace, "pod/"+operandPod[0])
		exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("resource %v does not disapper", "pod/"+operandPod[0]))
		ensurePodWithLabelReady(oc, operatorNamespace, operandLabel)
		ensureLogsContainString(oc, operatorNamespace, operandLabel, recordsReadyLog)

		exutil.By("Add label to canary service, ensure ExternalDNS added the record")
		defer oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", serviceNamespace, "service", serviceName, delLabel, "--overwrite").Output()
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", serviceNamespace, "service", serviceName, addLabel).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Add records: external-dns-ingress-canary")

		exutil.By("Remove label from the canary service, ensure ExternalDNS deleted the record")
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", serviceNamespace, "service", serviceName, delLabel, "--overwrite").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Del records: external-dns-ingress-canary")
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-ConnectedOnly-ROSA-OSD_CCS-Critical-68826-External DNS support for preexisting Route53 for Shared VPC clusters", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router", "extdns")
			sampleAWSVPC        = filepath.Join(buildPruningBaseDir, "sample-aws-sharedvpc-rt.yaml")
			crName              = "sample-aws-sharedvpc-rt"
			operandLabel        = operandLabelKey + crName
			routeNamespace      = "openshift-ingress-canary"
			routeName           = "canary"
		)

		exutil.By("Ensure the case is runnable on the cluster")
		exutil.SkipIfPlatformTypeNot(oc, "AWS")
		if exutil.IsSTSCluster(oc) {
			g.Skip("Skip on STS cluster")
		}
		baseDomain, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.baseDomain}").Output()

		// privateZoneIAMRole needs to be present for shared vpc cluster
		privateZoneIAMRole, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.platform.aws.privateZoneIAMRole}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(privateZoneIAMRole, "arn:aws:iam::") {
			g.Skip("Skip since this is not a shared vpc cluster")
		}

		exutil.By("1. Check the STS Role in the cluster")
		output, ouputErr := oc.AsAdmin().WithoutNamespace().Run("get").Args("CredentialsRequest/openshift-ingress", "-n", "openshift-cloud-credential-operator", "-o=jsonpath={.spec.providerSpec.statementEntries[0].action}").Output()
		o.Expect(ouputErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("sts:AssumeRole"))
		// Getting the private zone id from dns config")
		privateZoneId := getByJsonPath(oc, "openshift-dns", "dns.config/cluster", "{.spec.privateZone.id}")

		exutil.By("2. Create External DNS Operator in the cluster")
		createExternalDNSOperator(oc)
		ensurePodWithLabelReady(oc, operatorNamespace, operatorLabel)

		exutil.By("3. Create CR ExternalDNS sample-aws-sharedvpc-rt and ensure operand pod is ready")
		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("externaldns", crName).Output()
		// To avoid connection refused flake error, as the controller CR creation needs extra prepare time after the operator pod is ready
		time.Sleep(3 * time.Second)
		// Updating the yaml file with basedomin and ARN value
		sedCmd := fmt.Sprintf(`sed -i'' -e 's@basedomain@%s@g;s@privatezoneiamrole@%v@g' %s`, baseDomain, privateZoneIAMRole, sampleAWSVPC)
		_, err = exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		_, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", sampleAWSVPC).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, operatorNamespace, operandLabel)
		ensureLogsContainString(oc, operatorNamespace, operandLabel, recordsReadyLog)

		exutil.By("4. Add label to canary route, ensure ExternalDNS added the record")
		defer oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, delLabel, "--overwrite").Output()
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, addLabel).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Desired change: CREATE canary-openshift-ingress-canary.apps."+baseDomain+" CNAME [Id: /hostedzone/"+privateZoneId+"]")

		exutil.By("5. Remove label from the canary route, ensure ExternalDNS deleted the record")
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, delLabel, "--overwrite").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Desired change: DELETE canary-openshift-ingress-canary.apps."+baseDomain+" CNAME [Id: /hostedzone/"+privateZoneId+"]")
	})
})

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_ExtDNS on STS", func() {
	defer g.GinkgoRecover()

	var (
		oc                = exutil.NewCLI("externaldnssts", exutil.KubeConfigPath())
		operatorNamespace = "external-dns-operator"
		operatorLabel     = "name=external-dns-operator"
		operandLabelKey   = "app.kubernetes.io/instance="
		addLabel          = "external-dns.mydomain.org/publish=yes"
		delLabel          = "external-dns.mydomain.org/publish-"
		recordsReadyLog   = "All records are already up to date"
	)
	g.BeforeEach(func() {
		// skip ARM64 arch
		architecture.SkipNonAmd64SingleArch(oc)
	})

	// this case runs only on AWS STS and hypershift cluster
	g.It("Author:mjoseph-ConnectedOnly-ROSA-OSD_CCS-High-74949-ExternalDNS operand support on STS cluster", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router", "extdns")
			sampleAWSSTS        = filepath.Join(buildPruningBaseDir, "sample-aws-sts-rt.yaml")
			crName              = "sample-aws-sts-rt"
			operandLabel        = operandLabelKey + crName
			routeNamespace      = "openshift-ingress-canary"
			routeName           = "canary"
		)

		exutil.By("1. Ensure the case is runnable on the cluster")
		exutil.SkipIfPlatformTypeNot(oc, "AWS")
		// Skip in Gov cluster
		region, err := exutil.GetAWSClusterRegion(oc)
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Contains(region, "us-gov") {
			g.Skip("Skipping for the aws cluster in us-gov region")
		}
		// Skip in non STS cluster
		if !exutil.IsSTSCluster(oc) {
			g.Skip("Skip for non-STS cluster")
		}
		// Skip in Shared VPC cluster
		privateZoneIAMRole, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("dns.config", "cluster", "-o=jsonpath={.spec.platform.aws}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if strings.Contains(privateZoneIAMRole, "privateZoneIAMRole") {
			g.Skip("Skipping since this case will not run on a shared vpc cluster")
		}

		exutil.By("2. Create the External DNS Operator")
		createExternalDNSOperator(oc)
		ensurePodWithLabelReady(oc, operatorNamespace, operatorLabel)

		exutil.By("3. Prepare the STS related credentials like role, policy and secret for the cluster")
		defer clearUpExDnsStsCluster(oc, "74949")
		prepareStsCredForCluster(oc, "74949")

		exutil.By("4. Create STS ExternalDNS CR `sample-aws-sts-rt` and ensure operand pod is ready")
		baseDomain := getBaseDomain(oc)
		// get the Hosted zone ID from AWS route53
		privateZoneID := getPrivateZoneID(oc, baseDomain)
		updateFilebySedCmd(sampleAWSSTS, "basedomain", baseDomain)
		updateFilebySedCmd(sampleAWSSTS, "privatezone", privateZoneID)

		defer oc.AsAdmin().WithoutNamespace().Run("delete").Args("externaldns", crName).Output()
		_, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", sampleAWSSTS).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensurePodWithLabelReady(oc, operatorNamespace, operandLabel)
		ensureLogsContainString(oc, operatorNamespace, operandLabel, recordsReadyLog)

		exutil.By("5. Add label to canary route, ensure ExternalDNS added the record")
		defer oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, delLabel, "--overwrite").Output()
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, addLabel).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Desired change: CREATE external-dns-canary-openshift-ingress-canary")

		exutil.By("6. Remove label from the canary route, ensure ExternalDNS deleted the record")
		_, err = oc.AsAdmin().WithoutNamespace().Run("label").Args("-n", routeNamespace, "route", routeName, delLabel, "--overwrite").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		ensureLogsContainString(oc, operatorNamespace, operandLabel, "Desired change: DELETE external-dns-canary-openshift-ingress-canary")
	})
})
