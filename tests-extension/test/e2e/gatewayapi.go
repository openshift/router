package router

import (
	"github.com/openshift/router-tests-extension/test/e2e/testdata"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	wait "k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("gatewayapi", compat_otp.KubeConfigPath())

	g.BeforeEach(func() {
		platforms := map[string]bool{
			"aws":      true,
			"azure":    true,
			"gcp":      true,
			"ibmcloud": true,
			"powervs":  true,
		}
		if !platforms[compat_otp.CheckPlatform(oc)] {
			g.Skip("Skip for non-cloud platforms")
		}

		if isInternalLBScopeInDefaultIngresscontroller(oc) {
			g.Skip("Skip for private cluster since GatewayClass is not accepted in cluster")
		}
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-High-81976-Ensure that RBAC works correctly for gatewayAPI resources for unprivileged users", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			gwcFile             = filepath.Join(buildPruningBaseDir, "gatewayclass.yaml")
			gwcName             = "openshift-default"
			gwFile              = filepath.Join(buildPruningBaseDir, "gateway.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			httpRouteFile       = filepath.Join(buildPruningBaseDir, "httproute.yaml")

			gateway1 = gatewayDescription{
				name:      "gateway",
				namespace: "openshift-ingress",
				hostname:  "",
				template:  gwFile,
			}

			httpRoute = httpRouteDescription{
				name:      "route81976",
				namespace: "",
				gwName:    "",
				hostname:  "",
				template:  httpRouteFile,
			}
		)

		compat_otp.By("1.0: Create a gatewayClass object")
		output, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", gwcFile).Output()
		if err != nil && !strings.Contains(output, "AlreadyExists") {
			e2e.Failf("Failed to create gatewayclass: %v", err)
		}

		waitErr := wait.PollImmediate(2*time.Second, 300*time.Second, func() (bool, error) {
			output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("gatewayclass", gwcName, "-ojsonpath={.status.conditions}").Output()
			if strings.Contains(output, "True") {
				e2e.Logf("the gatewayclass is accepted")
				return true, nil
			}
			e2e.Logf("Waiting for the GatewayClass to be accepted")
			return false, nil
		})
		o.Expect(waitErr).NotTo(o.HaveOccurred(), "The GatewayClass was never accepted")

		compat_otp.By("2.0: Create a gateway object as an admin")
		baseDomain := getBaseDomain(oc)
		gateway1.hostname = "*.gwapi." + baseDomain
		defer gateway1.delete(oc)
		gateway1.create(oc)

		waitErr = wait.PollImmediate(2*time.Second, 120*time.Second, func() (bool, error) {
			output, _ := oc.AsAdmin().WithoutNamespace().Run("get").Args("gateway", gateway1.name, "-n", gateway1.namespace).Output()
			if strings.Contains(output, "True") {
				e2e.Logf("the Gateway is programmed")
				return true, nil
			}
			e2e.Logf("Waiting for the Gateway to be accepted")
			return false, nil
		})
		o.Expect(waitErr).NotTo(o.HaveOccurred(), "The Gateway was never accepted")

		compat_otp.By("3.0: Attempt to Create a gateway object as a test user in 'openshift-ingress' namespace")
		output, err = createUserResourceToNsFromTemplate(oc, "openshift-ingress", "--ignore-unknown-parameters=true", "-f", gwFile)
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`cannot create resource "gateways" in API group "gateway.networking.k8s.io" in the namespace "openshift-ingress`))

		compat_otp.By("4.0: Attempt to Create a gateway object as a test user in namespace with admin access")
		ns := oc.Namespace()
		output, err = createUserResourceToNsFromTemplate(oc, ns, "--ignore-unknown-parameters=true", "-f", gwFile, "-p", "NAMESPACE="+ns)
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`cannot create resource "gateways" in API group "gateway.networking.k8s.io" in the namespace ` + `"` + ns + `"`))

		compat_otp.By("5.0: Create a web-server application")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("6.0: Create a HTTPRoute as a test user")
		httpRoute.namespace = ns
		httpRoute.gwName = gateway1.name
		httpRoute.hostname = "ocp81976.gwapi." + baseDomain
		httpRoute.userCreate(oc)

		waitForOutputEquals(oc, ns, "httproute/"+httpRoute.name, `{.status.parents[].conditions[?(@.type=="Accepted")].status}`, "False")
		output = getByJsonPath(oc, ns, "httproute/"+httpRoute.name, `{.status.parents[].conditions[?(@.type=="Accepted")]}`)
		o.Expect(output).To(o.And(o.ContainSubstring(`namespace \"`+ns+`\" is not allowed by the parent`), o.ContainSubstring(`NotAllowedByListeners`)))

		compat_otp.By("7.0: Label the namespace with the Gateway Selector")
		err = oc.AsAdmin().WithoutNamespace().Run("label").Args("ns", ns, "app=gwapi").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "Adding label to the namespace failed")

		compat_otp.By("8.0: Re-check the HTTPRoute")
		waitForOutputEquals(oc, ns, "httproute/"+httpRoute.name, `{.status.parents[].conditions[?(@.type=="Accepted")].status}`, "True")

		compat_otp.By("Clean up cluster resources")
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-f", gwcFile).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
	})

	// author: hongli@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-58358
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-NonPreRelease-PreChkUpgrade-High-83185-Ensure GatewayAPI works well after upgrade", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			gwcFile             = filepath.Join(buildPruningBaseDir, "gatewayclass.yaml")
			gwcName             = "openshift-default"
			gwFile              = filepath.Join(buildPruningBaseDir, "gateway.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			httpRouteFile       = filepath.Join(buildPruningBaseDir, "httproute.yaml")
			ns                  = "e2e-test-gatewayapi-ocp83185"

			gateway = gatewayDescription{
				name:      "gateway",
				namespace: "openshift-ingress",
				hostname:  "",
				template:  gwFile,
			}

			httpRoute = httpRouteDescription{
				name:      "route83185",
				namespace: "",
				gwName:    "",
				hostname:  "",
				template:  httpRouteFile,
			}
		)

		compat_otp.By("1.0: Create a gatewayClass object and wait until it is Accepted")
		output, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", gwcFile).Output()
		if err != nil && !strings.Contains(output, "AlreadyExists") {
			e2e.Failf("Failed to create gatewayclass: %v", err)
		}
		waitForOutputEquals(oc, "default", "gatewayclass/"+gwcName, `{.status.conditions[?(@.type=="Accepted")].status}`, "True")

		compat_otp.By("2.0: Create a gateway object and wait until it is Programmed")
		baseDomain := getBaseDomain(oc)
		gateway.hostname = "*.gwapi." + baseDomain
		gateway.create(oc)
		waitForOutputEquals(oc, "openshift-ingress", "gateway/gateway", `{.status.conditions[?(@.type=="Programmed")].status}`, "True")

		// Use admin user to create the pod/svc/httproute since they should be kept until post upgrade
		compat_otp.By("3.0: Create a project and apply required label, then create web-server application")
		oc.CreateSpecifiedNamespaceAsAdmin(ns)
		err = oc.AsAdmin().WithoutNamespace().Run("label").Args("ns", ns, "app=gwapi").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "Adding label to the namespace failed")
		operateResourceFromFile(oc, "create", ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("4.0: Create a HTTPRoute and wait until it is accepted")
		httpRoute.namespace = ns
		httpRoute.gwName = gateway.name
		httpRoute.hostname = "ocp83185.gwapi." + baseDomain
		jsonCfg := parseToJSON(oc, []string{"--ignore-unknown-parameters=true", "-f", httpRoute.template, "-p", "NAME=" + httpRoute.name, "NAMESPACE=" + httpRoute.namespace, "GWNAME=" + httpRoute.gwName, "HOSTNAME=" + httpRoute.hostname})
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "-f", jsonCfg).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputEquals(oc, ns, "httproute/route83185", `{.status.parents[].conditions[?(@.type=="Accepted")].status}`, "True")
	})

	// author: hongli@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-58358
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-NonPreRelease-PstChkUpgrade-High-83185-Ensure GatewayAPI works well after upgrade", func() {
		var (
			ns = "e2e-test-gatewayapi-ocp83185"
		)

		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ns", ns).Output()
		if err != nil || strings.Contains(output, "NotFound") {
			g.Skip("Skipping since can not find the namespace that should be created in Pre-Upgrade test")
		}

		compat_otp.By("1.0: Check the GatewayClass status in post upgrade")
		status := getByJsonPath(oc, "default", "gatewayclass/openshift-default", `{.status.conditions[?(@.type=="Accepted")].status}`)
		o.Expect(status).To(o.ContainSubstring("True"))

		compat_otp.By("2.0: Check istio status in post upgrade")
		status = getByJsonPath(oc, "default", "istio/openshift-gateway", `{.status.state}`)
		o.Expect(status).To(o.ContainSubstring("Healthy"))

		compat_otp.By("3.0: Check the Gateway status in post upgrade")
		status = getByJsonPath(oc, "openshift-ingress", "gateway/gateway", `{.status.conditions[?(@.type=="Programmed")].status}`)
		o.Expect(status).To(o.ContainSubstring("True"))

		compat_otp.By("4.0: Check the HTTPRoute in post upgrade")
		status = getByJsonPath(oc, ns, "httproute/route83185", `{.status.parents[].conditions[?(@.type=="Accepted")].status}`)
		o.Expect(status).To(o.ContainSubstring("True"))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-86110-View metrics showing who is using Gateway API", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			gwcFile             = filepath.Join(buildPruningBaseDir, "gatewayclass.yaml")
			gwcName             = "openshift-default"
			gwFile              = filepath.Join(buildPruningBaseDir, "gateway.yaml")

			gateway1 = gatewayDescription{
				name:      "gateway",
				namespace: "openshift-ingress",
				hostname:  "",
				template:  gwFile,
			}
		)

		compat_otp.By("1.0: Create a gatewayClass object")
		output, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", gwcFile).Output()
		if err != nil && !strings.Contains(output, "AlreadyExists") {
			e2e.Failf("Failed to create gatewayclass: %v", err)
		}

		waitForOutputContains(oc, "default", "gatewayclass/"+gwcName, "{.status.conditions}", "True", 300*time.Second)

		compat_otp.By("2.0: Create a gateway object under the openshift gatewayClass")
		baseDomain := getBaseDomain(oc)
		gateway1.hostname = "*.gwapi." + baseDomain
		defer gateway1.delete(oc)
		gateway1.create(oc)

		waitForOutputContains(oc, gateway1.namespace, "gateway/"+gateway1.name, "{.status.conditions}", "True", 120*time.Second)

		compat_otp.By("3.0: Check prometheus metrics for the created gateways")
		token, err := getSAToken(oc, "prometheus-k8s", "openshift-monitoring")
		o.Expect(err).NotTo(o.HaveOccurred())

		url := "https://prometheus-k8s.openshift-monitoring.svc:9091/api/v1/"
		query := "query?query=openshift:gateway_api_usage:count"

		curlCmd := []string{"-n", "openshift-monitoring", "-c", "prometheus", "prometheus-k8s-0", "-i", "--", "curl", "-k", "-s", "-H", fmt.Sprintf("Authorization: Bearer %v", token), fmt.Sprintf("%s%s", url, query), "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, `"gateway_class_type":"openshift"`, 300, 1)

		compat_otp.By("4.0: Confirm the metrics values are correct")
		repeatCmdOnClient(oc, curlCmd, `"1"`, 120, 1)

		compat_otp.By("Clean up cluster resources")
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-f", gwcFile).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
	})
})
