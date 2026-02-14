package router

import (
    "github.com/openshift/router-tests-extension/test/e2e/testdata"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go/service/route53"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

// Create External DNS Controller (operand) Role and inline policy
func createExDnsRolePolicy(iamClient *iam.Client, infraID string, oidcArnPrefix string, oidcName string) string {
	buildPruningBaseDir := testdata.FixturePath("router", "extdns")
	exDnsPermissionPolicyFile := filepath.Join(buildPruningBaseDir, "sts-exdns-perms-policy.json")
	exDnsRoleName := infraID + "-exdns-role"
	exDnsPolicyName := infraID + "-exdns-perms-policy"

	exdDnsTrustPolicy := `{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "Federated": "%s"
            },
            "Action": "sts:AssumeRoleWithWebIdentity",
            "Condition": {
                "StringEquals": {
                    "%s:sub": "system:serviceaccount:external-dns-operator:external-dns-sample-aws-sts-rt"
                }
            }
        }
    ]
}`
	oidcArn := oidcArnPrefix + oidcName
	exdDnsTrustPolicy = fmt.Sprintf(exdDnsTrustPolicy, oidcArn, oidcName)
	// create role
	exDnsRoleArn := iamCreateRole(iamClient, exdDnsTrustPolicy, exDnsRoleName)

	exDnsPermissionPolicy, err := os.ReadFile(exDnsPermissionPolicyFile)
	o.Expect(err).NotTo(o.HaveOccurred())
	// create policy
	iamPutRolePolicy(iamClient, string(exDnsPermissionPolicy), exDnsPolicyName, exDnsRoleName)
	return exDnsRoleArn
}

// Remove External DNS Operand role and policy on the STS cluster
func deleteExDnsRolePolicy(iamClient *iam.Client, infraID, prefix string) {
	exDnsRoleName := infraID + "-" + prefix + "-exdns-role"
	exDnsPolicyName := infraID + "-" + prefix + "-exdns-perms-policy"
	iamDeleteRolePolicy(iamClient, exDnsPolicyName, exDnsRoleName)
	iamDeleteRole(iamClient, exDnsRoleName)
}

// Prepare roles, policies and secrets for STS cluster
func prepareStsCredForCluster(oc *exutil.CLI, prefix string) {
	infraID, _ := exutil.GetInfraID(oc)
	oidcName := getOidc(oc)
	iamClient := newIamClient()
	stsClient := newStsClient()
	account := getAwsAccount(stsClient)
	oidcArnPrefix := "arn:aws:iam::" + account + ":oidc-provider/"

	// create role and policy
	exDnsRoleArn := createExDnsRolePolicy(iamClient, infraID+"-"+prefix, oidcArnPrefix, oidcName)
	// create a secret with the external dns ARN role
	createSecretUsingRoleARN(oc, "external-dns-operator", exDnsRoleArn)
}

// Clear up all roles, policies and secrets of the STS cluster
func clearUpExDnsStsCluster(oc *exutil.CLI, prefix string) {
	infraID, _ := exutil.GetInfraID(oc)
	iamClient := newIamClient()
	deleteExDnsRolePolicy(iamClient, infraID, prefix)

	// deleting secret
	oc.AsAdmin().WithoutNamespace().Run("delete").Args("secret", "-n", "external-dns-operator", "aws-sts-creds").Output()
}

// Create the STS secret with the external dns ARN role
func createSecretUsingRoleARN(oc *exutil.CLI, ns, exDnsRoleArn string) {
	buildPruningBaseDir := testdata.FixturePath("router", "extdns")
	awsStsCredSecret := filepath.Join(buildPruningBaseDir, "aws-sts-creds-secret.yaml")
	updateFilebySedCmd(awsStsCredSecret, "external-dns-role-arn", exDnsRoleArn)

	_, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", awsStsCredSecret).Output()
	o.Expect(err).NotTo(o.HaveOccurred())
	// verify the secret creation
	output := getByJsonPath(oc, ns, "secret", "{.items[*].metadata.name}")
	o.Expect(output).Should(o.ContainSubstring("aws-sts-creds"))
}

// Collect the Zone details from the AWS route53 and return the Hosted zone ID
func getPrivateZoneID(oc *exutil.CLI, domainName string) string {
	route53Client := exutil.NewRoute53Client()
	var hostedZoneDetails *route53.ListHostedZonesByNameOutput

	// collect the hostZone Details from the AWS route53 using the domain name
	hostedZoneDetails, err := route53Client.ListHostedZonesByNameWithContext(
		context.Background(), &route53.ListHostedZonesByNameInput{
			DNSName: aws.String(domainName)})
	o.Expect(err).NotTo(o.HaveOccurred())
	e2e.Logf("The zone Id of the host domain '%s' is '%s'", *hostedZoneDetails.HostedZones[0].Name, *hostedZoneDetails.HostedZones[0].Id)
	return strings.TrimPrefix(*hostedZoneDetails.HostedZones[0].Id, "/hostedzone/")
}
