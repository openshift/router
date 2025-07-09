package router

import (
	"path/filepath"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/router/ginkgo-test/test/extended/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	wait "k8s.io/apimachinery/pkg/util/wait"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("gatewayapi", exutil.KubeConfigPath())

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-NonHyperShiftHOST-High-81976-NetworkEdge Ensure that RBAC works correctly for gatewayAPI resources for unprivileged users", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
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

		platforms := map[string]bool{
			"aws":      true,
			"azure":    true,
			"gcp":      true,
			"ibmcloud": true,
		}
		if !platforms[exutil.CheckPlatform(oc)] {
			g.Skip("Skip for non-cloud platforms")
		}

		if isInternalLBScopeInDefaultIngresscontroller(oc) {
			g.Skip("Skip for private cluster since GatewayClass is not accepted in cluster")
		}

		exutil.By("1.0: Create a gatewayClass object")
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("-f", gwcFile).Execute()
		if err != nil && !apierrors.IsAlreadyExists(err) {
			e2e.Logf("Failed to create GatewayClass")
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

		exutil.By("2.0: Create a gateway object as an admin")
		baseDomain := getBaseDomain(oc)
		gateway1.hostname = "*.api.apps." + baseDomain
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

		exutil.By("3.0: Attempt to Create a gateway object as a test user in 'openshift-ingress' namespace")
		output, err := createUserResourceToNsFromTemplate(oc, "openshift-ingress", "--ignore-unknown-parameters=true", "-f", gwFile)
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`cannot create resource "gateways" in API group "gateway.networking.k8s.io" in the namespace "openshift-ingress`))

		exutil.By("4.0: Attempt to Create a gateway object as a test user in namespace with admin access")
		project1 := oc.Namespace()
		output, err = createUserResourceToNsFromTemplate(oc, project1, "--ignore-unknown-parameters=true", "-f", gwFile, "-p", "NAMESPACE="+project1)
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`cannot create resource "gateways" in API group "gateway.networking.k8s.io" in the namespace ` + `"` + project1 + `"`))

		exutil.By("5.0: Create a web-server application")
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=web-server-deploy")

		exutil.By("6.0: Create a HTTPRoute as a test user")
		httpRoute.namespace = project1
		httpRoute.gwName = gateway1.name
		httpRoute.hostname = "test.api.apps." + baseDomain
		httpRoute.userCreate(oc)

		output, err = oc.Run("get").Args("-n", project1, "httproute", httpRoute.name, "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(`namespace "`+project1+`" is not allowed by the parent`), o.ContainSubstring(`NotAllowedByListeners`)))

		exutil.By("7.0: Label the namespace with the Gateway Selector")
		err = oc.AsAdmin().WithoutNamespace().Run("label").Args("ns", project1, "app=gwapi").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "Adding label to the namespace failed")

		exutil.By("8.0: Re-check the HTTPRoute")
		output, err = oc.Run("get").Args("-n", project1, "httproute", httpRoute.name, "-oyaml").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring(`Route was valid`), o.ContainSubstring(`True`)))

		exutil.By("Clean up cluster resources")
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-f", gwcFile).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
	})
})
