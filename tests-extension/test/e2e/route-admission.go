package router

import (
	"github.com/openshift/router-tests-extension/test/e2e/testdata"
	"fmt"
	"os/exec"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
)

var _ = g.Describe("[sig-network-edge][OTP] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-admission", compat_otp.KubeConfigPath())

	// Test case creater: hongli@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-27594-Set namespaceOwnership of routeAdmission to InterNamespaceAllowed", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			e2eTestNamespace2   = "e2e-ne-ocp27594-" + getRandomString()
			ingctrl             = ingressControllerDescription{
				name:      "ocp27594",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. Create an additional namespace for this scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		e2eTestNamespace1 := oc.Namespace()
		path1 := "/path/first"
		path2 := "/path/second"
		routehost := srvName + "-" + "ocp27594." + "apps.example.com"

		compat_otp.By("2. Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		// Updating namespaceOwnership as 'InterNamespaceAllowed' in the yaml file
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|Strict|%s|g' %s`, "InterNamespaceAllowed", customTemp)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("3. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be true")
		namespaceOwnershipEnv := readRouterPodEnv(oc, custContPod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=true"))

		compat_otp.By("4. Create a server pod and an unsecure service in one ns")
		createResourceFromFile(oc, e2eTestNamespace1, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace1, "name="+srvrcInfo)

		compat_otp.By("5. Create a server pod and an unsecure service in the other ns")
		operateResourceFromFile(oc, "create", e2eTestNamespace2, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		compat_otp.By("6. Expose a http route with path " + path1 + " in the first ns")
		err = oc.Run("expose").Args("service", srvName, "--hostname="+routehost, "--path="+path1, "-n", e2eTestNamespace1).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		getRoutes(oc, e2eTestNamespace1)
		waitForOutputEquals(oc, e2eTestNamespace1, "route", "{.items[0].metadata.name}", srvName)

		compat_otp.By("7. Create a edge route with the same hostname, but with different path " + path2 + " in the second ns")
		err = oc.AsAdmin().Run("create").Args("route", "edge", "route-edge", "--service="+srvName, "--hostname="+routehost, "--path="+path2, "-n", e2eTestNamespace2).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		getRoutes(oc, e2eTestNamespace2)
		waitForOutputEquals(oc, e2eTestNamespace2, "route", "{.items[0].metadata.name}", "route-edge")

		compat_otp.By("8 Check the custom router pod and ensure " + e2eTestNamespace1 + " http route is loaded in haproxy.config")
		ensureHaproxyBlockConfigContains(oc, custContPod, e2eTestNamespace1, []string{"backend be_http:" + e2eTestNamespace1 + ":service-unsecure"})

		compat_otp.By("9. Check the custom router pod and ensure " + e2eTestNamespace2 + " edge route is loaded in haproxy.config")
		ensureHaproxyBlockConfigContains(oc, custContPod, e2eTestNamespace2, []string{"backend be_edge_http:" + e2eTestNamespace2 + ":route-edge"})
	})

	// Test case creater: hongli@redhat.com
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-27595-Set namespaceOwnership of routeAdmission to Strict", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			e2eTestNamespace2   = "e2e-ne-ocp27595-" + getRandomString()
		)

		compat_otp.By("1. Create an additional namespace for this scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		e2eTestNamespace1 := oc.Namespace()
		path1 := "/path/first"
		path2 := "/path/second"
		routehost := "ocp27595.apps.example.com"

		// Strict is by default so just need to check in default router
		compat_otp.By("2. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be false")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		namespaceOwnershipEnv := readRouterPodEnv(oc, routerpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=false"))

		compat_otp.By("3. Create a server pod and an unsecure service in one ns")
		createResourceFromFile(oc, e2eTestNamespace1, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace1, "name="+srvrcInfo)

		compat_otp.By("4. Create a reen route with path " + path1 + " in the first ns")
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("route", "reencrypt", "route-reen", "--service=service-secure", "--hostname="+routehost, "--path="+path1, "-n", e2eTestNamespace1).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		getRoutes(oc, e2eTestNamespace1)
		waitForOutputEquals(oc, e2eTestNamespace1, "route", "{.items[0].metadata.name}", "route-reen")

		compat_otp.By("5 Check the custom router pod and ensure " + e2eTestNamespace1 + " route is loaded in haproxy.config")
		ensureHaproxyBlockConfigContains(oc, routerpod, e2eTestNamespace1, []string{"backend be_secure:" + e2eTestNamespace1 + ":route-reen"})
		compat_otp.By("6. Create a server pod and an unsecure service in the other ns")
		operateResourceFromFile(oc, "create", e2eTestNamespace2, testPodSvc)
		ensurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		compat_otp.By("7. Create a http route with the same hostname, but with different path " + path2 + " in the second ns")
		err = oc.AsAdmin().WithoutNamespace().Run("expose").Args("service", "service-unsecure", "--hostname="+routehost, "--path="+path2, "-n", e2eTestNamespace2).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		getRoutes(oc, e2eTestNamespace2)
		waitForOutputEquals(oc, e2eTestNamespace2, "route", "{.items[0].metadata.name}", "service-unsecure")

		compat_otp.By("8. Confirm the route in the second ns is shown as HostAlreadyClaimed")
		waitForOutputContains(oc, e2eTestNamespace2, "route", `{.items[*].status.ingress[?(@.routerName=="default")].conditions[*].reason}`, "HostAlreadyClaimed")
	})

	// Test case creater: hongli@redhat.com
	// For OCP-27596 and OCP-27605
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-27596-Update the namespaceOwnership of routeAdmission", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp27596",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("1. Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("2. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be false")
		routerpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		namespaceOwnershipEnv := readRouterPodEnv(oc, routerpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=false"))

		compat_otp.By("3. Patch the custom ingress controller and set namespaceOwnership to InterNamespaceAllowed")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontrollers/"+ingctrl.name, "{\"spec\":{\"routeAdmission\":{\"namespaceOwnership\":\"InterNamespaceAllowed\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("4. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be true")
		newRouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		namespaceOwnershipEnv = readRouterPodEnv(oc, newRouterpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=true"))

		compat_otp.By("5. Patch the custom ingress controller and set namespaceOwnership to Strict")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontrollers/"+ingctrl.name, "{\"spec\":{\"routeAdmission\":{\"namespaceOwnership\":\"Strict\"}}}")

		compat_otp.By("6. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be false")
		namespaceOwnershipEnv = readRouterPodEnv(oc, routerpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=false"))

		compat_otp.By("7. Patch the custom ingress controller and set namespaceOwnership to Null")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontrollers/"+ingctrl.name, "{\"spec\":{\"routeAdmission\":{\"namespaceOwnership\":null}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")

		compat_otp.By("8. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be false")
		newRouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		namespaceOwnershipEnv = readRouterPodEnv(oc, newRouterpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=false"))

		// Incorporating the negative case OCP-27605 here
		compat_otp.By("9. Patch the custom ingress controller and set namespaceOwnership to a invalid string like 'InvalidTest'")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/"+ingctrl.name, "-p", "{\"spec\":{\"routeAdmission\":{\"namespaceOwnership\":\"InvalidTest\"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output).To(o.ContainSubstring("spec.routeAdmission.namespaceOwnership: Unsupported value: \"InvalidTest\": supported values: \"InterNamespaceAllowed\", \"Strict\""))
	})

	// Test case creater: hongli@redhat.com
	g.It("Author:mjoseph-NonHyperShiftHOST-Critical-30190-Set wildcardPolicy of routeAdmission to WildcardsAllowed", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			srvName             = "service-unsecure"
			ingctrl             = ingressControllerDescription{
				name:      "ocp30190",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)
		compat_otp.By("1. Create a custom ingresscontroller")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		// Updating wildcardPolicy as 'WildcardsAllowed' in the yaml file
		sedCmd := fmt.Sprintf(`sed -i'' -e 's|WildcardsDisallowed|%s|g' %s`, "WildcardsAllowed", customTemp)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		routehost := "wildcard." + ns + "." + ingctrl.domain
		anyhost := "any." + ns + "." + ingctrl.domain
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("2. Check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be true")
		namespaceOwnershipEnv := readRouterPodEnv(oc, custContPod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=true"))

		compat_otp.By("3. Create a server pod and an unsecure service")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("4. Expose a http wildcard route")
		err = oc.WithoutNamespace().Run("expose").Args("service", srvName, "--hostname="+routehost, "-n", ns, "--wildcard-policy=Subdomain").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		getRoutes(oc, ns)

		compat_otp.By("5. Check the reachability of the wildcard route")
		ingressContPod := getPodListByLabel(oc, "openshift-ingress-operator", "name=ingress-operator")
		iplist := getPodIP(oc, "openshift-ingress", custContPod)
		toDst := routehost + ":80:" + iplist[0]
		cmdOnPod := []string{"-n", "openshift-ingress-operator", ingressContPod[0], "--", "curl", "-I", "http://" + routehost, "--resolve", toDst, "--connect-timeout", "10"}
		result, _ := repeatCmdOnClient(oc, cmdOnPod, "200", 30, 1)
		o.Expect(result).To(o.ContainSubstring("200"))

		compat_otp.By("6. Check the reachability of the test route")
		toDst = anyhost + ":80:" + iplist[0]
		cmdOnPod = []string{"-n", "openshift-ingress-operator", ingressContPod[0], "--", "curl", "-I", "http://" + anyhost, "--resolve", toDst, "--connect-timeout", "10"}
		result, _ = repeatCmdOnClient(oc, cmdOnPod, "200", 30, 1)
		o.Expect(result).To(o.ContainSubstring("200"))
	})

	// Test case creater: hongli@redhat.com
	// For OCP-30191 and OCP-30192
	g.It("Author:mjoseph-NonHyperShiftHOST-Medium-30191-Set wildcardPolicy of routeAdmission to WildcardsDisallowed", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			srvName             = "service-unsecure"
			ingctrl             = ingressControllerDescription{
				name:      "ocp30191",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)
		compat_otp.By("1. Create a custom ingresscontroller")
		ns := oc.Namespace()
		ingctrl.domain = ingctrl.name + "." + getBaseDomain(oc)
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		routehost := "wildcard." + ns + "." + ingctrl.domain
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)

		compat_otp.By("2. Check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be false")
		namespaceOwnershipEnv := readRouterPodEnv(oc, custContPod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=false"))

		compat_otp.By("3. Create a server pod and an unsecure service")
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("4. Expose a http wildcard route")
		err := oc.WithoutNamespace().Run("expose").Args("service", srvName, "--hostname="+routehost, "-n", ns, "--wildcard-policy=Subdomain").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		getRoutes(oc, ns)

		compat_otp.By("5. Confirm the route status is 'RouteNotAdmitted' and confirm the route is not accessible")
		ensureRouteIsNotAdmittedByIngressController(oc, ns, "service-unsecure", ingctrl.name)
		ingressContPod := getPodListByLabel(oc, "openshift-ingress-operator", "name=ingress-operator")
		iplist := getPodIP(oc, "openshift-ingress", custContPod)
		curlCmd := fmt.Sprintf("curl --resolve %s:80:%s http://%s -I -k --connect-timeout 10", routehost, iplist[0], routehost)
		statsOut, err := compat_otp.RemoteShPod(oc, "openshift-ingress-operator", ingressContPod[0], "sh", "-c", curlCmd)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(statsOut).Should(o.ContainSubstring("HTTP/1.0 503 Service Unavailable"))

		// Incorporating the negative case OCP-30192 here
		compat_otp.By("6. Patch the custom ingress controller and set wildcardPolicy to a invalid string like 'unknown'")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/"+ingctrl.name, "-p", "{\"spec\":{\"routeAdmission\":{\"wildcardPolicy\":\"unknown\"}}}", "--type=merge", "-n", ingctrl.namespace).Output()
		o.Expect(output).To(o.ContainSubstring("spec.routeAdmission.wildcardPolicy: Unsupported value: \"unknown\": supported values: \"WildcardsAllowed\", \"WildcardsDisallowed\""))
	})
})
