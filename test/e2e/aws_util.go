package router

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
	clusterinfra "github.com/openshift/openshift-tests-private/test/extended/util/clusterinfra"
	"k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

// check if AWS Credentials exist
func checkAwsCredentials() {
	envAws, present := os.LookupEnv("AWS_SHARED_CREDENTIALS_FILE")
	if present {
		e2e.Logf("The env AWS_SHARED_CREDENTIALS_FILE has been set: %v", envAws)
	} else {
		e2e.Logf("The env AWS_SHARED_CREDENTIALS_FILE is not set")
		envDir, present := os.LookupEnv("CLUSTER_PROFILE_DIR")
		if present {
			e2e.Logf("But the env CLUSTER_PROFILE_DIR has been set: %v", envDir)
			awsCredFile := filepath.Join(envDir, ".awscred")
			if _, err := os.Stat(awsCredFile); err == nil {
				e2e.Logf("And the .awscred file exists, export env AWS_SHARED_CREDENTIALS_FILE")
				err := os.Setenv("AWS_SHARED_CREDENTIALS_FILE", awsCredFile)
				o.Expect(err).NotTo(o.HaveOccurred())
			} else {
				e2e.Logf("Error: %v", err)
				g.Skip("Skip since .awscred file does not exist\n")
			}
		} else {
			g.Skip("Skip since env CLUSTER_PROFILE_DIR is not set either, no valid aws credential")
		}
	}
}

// new AWS STS client
func newStsClient() *sts.Client {
	checkAwsCredentials()
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		e2e.Logf("failed to load AWS configuration, %v", err)
	}
	o.Expect(err).NotTo(o.HaveOccurred())

	return sts.NewFromConfig(cfg)
}

// get AWS Account
func getAwsAccount(stsClient *sts.Client) string {
	result, err := stsClient.GetCallerIdentity(context.TODO(), &sts.GetCallerIdentityInput{})
	if err != nil {
		e2e.Logf("Couldn't get AWS caller identity. Here's why: %v\n", err)
	}
	o.Expect(err).NotTo(o.HaveOccurred())
	awsAccount := aws.ToString(result.Account)
	e2e.Logf("The current AWS account is: %v", awsAccount)
	return awsAccount
}

// new AWS IAM client
func newIamClient() *iam.Client {
	checkAwsCredentials()
	sdkConfig, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		e2e.Logf("failed to load AWS configuration, %v", err)
	}
	o.Expect(err).NotTo(o.HaveOccurred())

	return iam.NewFromConfig(sdkConfig)
}

// AWS IAM CreateRole (== aws iam create-role)
func iamCreateRole(iamClient *iam.Client, trustPolicy string, roleName string) string {
	result, err := iamClient.CreateRole(context.TODO(), &iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String(string(trustPolicy)),
		RoleName:                 aws.String(roleName),
	})
	if err != nil {
		e2e.Logf("Couldn't create role %v. Here's why: %v\n", roleName, err)
	}
	o.Expect(err).NotTo(o.HaveOccurred())
	roleArn := aws.ToString(result.Role.Arn)
	e2e.Logf("The created role ARN is: %v", roleArn)
	return roleArn
}

// AWS IAM PutRolePolicy (== aws iam put-role-policy)
func iamPutRolePolicy(iamClient *iam.Client, permissionPolicy string, policyName string, roleName string) {
	e2e.Logf("To put/attach role policy %v to the role %v......", policyName, roleName)
	_, err := iamClient.PutRolePolicy(context.TODO(), &iam.PutRolePolicyInput{
		PolicyDocument: aws.String(string(permissionPolicy)),
		PolicyName:     aws.String(policyName),
		RoleName:       aws.String(roleName),
	})

	if err != nil {
		e2e.Logf("Couldn't attach policy to role %v. Here's why: %v\n", roleName, err)
	}
	o.Expect(err).NotTo(o.HaveOccurred())
}

// AWS IAM DeleteRole (== aws iam delete-role)
// Before attempting to delete a role, remove the attached items: Inline policies ( DeleteRolePolicy )
func iamDeleteRole(iamClient *iam.Client, roleName string) {
	e2e.Logf("To delete the role %v......", roleName)
	_, err := iamClient.DeleteRole(context.TODO(), &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		e2e.Logf("Couldn't delete role %v. Here's why: %v\n", roleName, err)
	}
	// it is used for clear up, won't fail the case even err != nil
}

// AWS IAM DeleteRolePolicy (== aws iam delete-role-policy)
func iamDeleteRolePolicy(iamClient *iam.Client, policyName string, roleName string) {
	e2e.Logf("To delete the inline policy %v from role %v......", policyName, roleName)
	_, err := iamClient.DeleteRolePolicy(context.TODO(), &iam.DeleteRolePolicyInput{
		PolicyName: aws.String(policyName),
		RoleName:   aws.String(roleName),
	})

	if err != nil {
		e2e.Logf("Couldn't delete inline policy %v from role %v. Here's why: %v\n", policyName, roleName, err)
	}
	// it is used for clear up, won't fail the case even err != nil
}

// Create ALB Operator Role and inline Policy
func createAlboRolePolicy(iamClient *iam.Client, infraID string, oidcArnPrefix string, oidcName string) string {
	buildPruningBaseDir := exutil.FixturePath("testdata", "router", "awslb")
	alboPermissionPolicyFile := filepath.Join(buildPruningBaseDir, "sts-albo-perms-policy.json")

	alboRoleName := infraID + "-albo-role"
	alboPolicyName := infraID + "-albo-perms-policy"

	alboTrustPolicy := `{
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
                    "%s:sub": "system:serviceaccount:aws-load-balancer-operator:aws-load-balancer-operator-controller-manager"
                }
            }
        }
    ]
}`
	oidcArn := oidcArnPrefix + oidcName
	alboTrustPolicy = fmt.Sprintf(alboTrustPolicy, oidcArn, oidcName)
	alboRoleArn := iamCreateRole(iamClient, alboTrustPolicy, alboRoleName)

	alboPermissionPolicy, err := os.ReadFile(alboPermissionPolicyFile)
	o.Expect(err).NotTo(o.HaveOccurred())
	iamPutRolePolicy(iamClient, string(alboPermissionPolicy), alboPolicyName, alboRoleName)

	return alboRoleArn
}

// Create ALB Controller (operand) Role and inline policy
func createAlbcRolePolicy(iamClient *iam.Client, infraID string, oidcArnPrefix string, oidcName string) string {
	buildPruningBaseDir := exutil.FixturePath("testdata", "router", "awslb")
	albcPermissionPolicyFile := filepath.Join(buildPruningBaseDir, "sts-albc-perms-policy.json")
	albcRoleName := infraID + "-albc-role"
	albcPolicyName := infraID + "-albc-perms-policy"

	albcTrustPolicy := `{
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
                    "%s:sub": "system:serviceaccount:aws-load-balancer-operator:aws-load-balancer-controller-cluster"
                }
            }
        }
    ]
}`
	oidcArn := oidcArnPrefix + oidcName
	albcTrustPolicy = fmt.Sprintf(albcTrustPolicy, oidcArn, oidcName)
	albcRoleArn := iamCreateRole(iamClient, albcTrustPolicy, albcRoleName)

	albcPermissionPolicy, err := os.ReadFile(albcPermissionPolicyFile)
	o.Expect(err).NotTo(o.HaveOccurred())
	iamPutRolePolicy(iamClient, string(albcPermissionPolicy), albcPolicyName, albcRoleName)
	return albcRoleArn
}

// Remove ALB Operator role on STS cluster
func deleteAlboRolePolicy(iamClient *iam.Client, infraID string) {
	alboRoleName := infraID + "-albo-role"
	alboPolicyName := infraID + "-albo-perms-policy"
	iamDeleteRolePolicy(iamClient, alboPolicyName, alboRoleName)
	iamDeleteRole(iamClient, alboRoleName)
}

// Remove ALB Controller role on STS cluster
func deleteAlbcRolePolicy(iamClient *iam.Client, infraID string) {
	albcRoleName := infraID + "-albc-role"
	albcPolicyName := infraID + "-albc-perms-policy"
	iamDeleteRolePolicy(iamClient, albcPolicyName, albcRoleName)
	iamDeleteRole(iamClient, albcRoleName)
}

// Prepare all roles and secrets for STS cluster
func prepareAllForStsCluster(oc *exutil.CLI) {
	infraID, _ := exutil.GetInfraID(oc)
	oidcName := getOidc(oc)
	iamClient := newIamClient()
	stsClient := newStsClient()
	account := getAwsAccount(stsClient)
	oidcArnPrefix := "arn:aws:iam::" + account + ":oidc-provider/"

	alboRoleArn := createAlboRolePolicy(iamClient, infraID, oidcArnPrefix, oidcName)
	err := os.Setenv("ALBO_ROLE_ARN", alboRoleArn)
	o.Expect(err).NotTo(o.HaveOccurred())
	albcRoleArn := createAlbcRolePolicy(iamClient, infraID, oidcArnPrefix, oidcName)
	err = os.Setenv("ALBC_ROLE_ARN", albcRoleArn)
	o.Expect(err).NotTo(o.HaveOccurred())
}

// Clear up all roles for STS cluster and namespace aws-load-balancer-operator
func clearUpAlbOnStsCluster(oc *exutil.CLI) {
	ns := "aws-load-balancer-operator"
	infraID, _ := exutil.GetInfraID(oc)
	iamClient := newIamClient()
	deleteAlboRolePolicy(iamClient, infraID)
	deleteAlbcRolePolicy(iamClient, infraID)

	// delete all resources of aws-load-balancer-operator (only for STS cluster)
	deleteNamespace(oc, ns)
	// delete the credentialsrequest for ALB operator
	oc.AsAdmin().WithoutNamespace().Run("delete").Args("credentialsrequest", "aws-load-balancer-operator", "-n", "openshift-cloud-credential-operator", "--ignore-not-found").Execute()
}

// Create as many as Elatic IPs as number of subnets that are attached to the load balancer
func allocateElaticIP(oc *exutil.CLI, num int) []string {
	var eipAllocationsList []string
	// get the aws region
	clusterinfra.GetAwsCredentialFromCluster(oc)
	mySession := session.Must(session.NewSession())
	// Create an EC2 service client.
	svc := ec2.New(mySession)
	for i := 0; i < num; i++ {
		// Attempt to allocate the Elastic IP address.
		allocRes, err := svc.AllocateAddress(&ec2.AllocateAddressInput{Domain: aws.String("vpc")})
		o.Expect(err).NotTo(o.HaveOccurred())
		e2e.Logf("The eip allocation details for the %v element is %v", i, allocRes)
		eipAllocationsList = append(eipAllocationsList, *allocRes.AllocationId)
	}
	return eipAllocationsList
}

// Delete the Elastic IP
func ensureReleaseElaticIP(oc *exutil.CLI, eipList *[]string) {
	var eipAllocationsList []string = *eipList
	clusterinfra.GetAwsCredentialFromCluster(oc)
	awsSession := session.Must(session.NewSession())
	// Create an EC2 service client.
	svc := ec2.New(awsSession)
	for i := range eipAllocationsList {
		waitErr := wait.Poll(10*time.Second, 150*time.Second, func() (bool, error) {
			// Attempt to release the Elastic IP address.
			_, err := svc.ReleaseAddress(&ec2.ReleaseAddressInput{AllocationId: aws.String(eipAllocationsList[i])})
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "AuthFailure" {
					e2e.Logf("Try again as EIP allocation %s is not yet released", eipAllocationsList[i])
				} else {
					e2e.Logf("Error releasing EIP %s: %v, keep polling", eipAllocationsList[i], err)
				}
				return false, nil
			}
			return true, nil
		})
		exutil.AssertWaitPollNoErr(waitErr, fmt.Sprintf("reached max time, but unable to delete the EIP %s", eipAllocationsList[i]))
		e2e.Logf("EIP allocation %s is released", eipAllocationsList[i])
	}
}
