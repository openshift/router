package qe

import (
	"fmt"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	util "github.com/openshift/router-tests-extension/test"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Admission", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-admission", compat_otp.KubeConfigPath())

	// Test case creater: hongli@redhat.com
	g.It("Set namespaceOwnership of routeAdmission to InterNamespaceAllowed", g.Label("OCP-27594"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			srvrcInfo           = "web-server-deploy"
			srvName             = "service-unsecure"
			e2eTestNamespace2   = "e2e-ne-ocp27594-" + util.GetRandomString()
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp27594",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1. Create an additional namespace for this scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		e2eTestNamespace1 := oc.Namespace()
		path1 := "/path/first"
		path2 := "/path/second"
		// Use a unique host on every run so a route left behind by an interrupted
		// earlier invocation cannot claim this host and reject the current routes.
		routehost := srvName + "-ocp27594-" + util.GetRandomString() + ".apps.example.com"

		compat_otp.By("2. Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		// Edit a private fixture copy so parallel specs never share modified testdata.
		customTemp = util.CopyFixture(customTemp)
		ingctrl.Template = customTemp
		util.UpdateFilebySedCmd(customTemp, "Strict", "InterNamespaceAllowed")
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")
		custContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("3. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be true")
		namespaceOwnershipEnv := util.ReadRouterPodEnv(oc, custContPod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=true"))

		compat_otp.By("4. Create a server pod and an unsecure service in one ns")
		util.CreateResourceFromFile(oc, e2eTestNamespace1, testPodSvc)
		util.EnsurePodWithLabelReady(oc, e2eTestNamespace1, "name="+srvrcInfo)

		compat_otp.By("5. Create a server pod and an unsecure service in the other ns")
		util.OperateResourceFromFile(oc, "create", e2eTestNamespace2, testPodSvc)
		util.EnsurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		compat_otp.By("6. Expose a http route with path " + path1 + " in the first ns")
		err := oc.Run("expose").Args("service", srvName, "--hostname="+routehost, "--path="+path1, "-n", e2eTestNamespace1).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.GetRoutes(oc, e2eTestNamespace1)
		util.WaitForOutputEquals(oc, e2eTestNamespace1, "route", "{.items[0].metadata.name}", srvName)

		compat_otp.By("7. Create a edge route with the same hostname, but with different path " + path2 + " in the second ns")
		err = oc.AsAdmin().Run("create").Args("route", "edge", "route-edge", "--service="+srvName, "--hostname="+routehost, "--path="+path2, "-n", e2eTestNamespace2).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.GetRoutes(oc, e2eTestNamespace2)
		util.WaitForOutputEquals(oc, e2eTestNamespace2, "route", "{.items[0].metadata.name}", "route-edge")

		compat_otp.By("8 Check the custom router pod and ensure " + e2eTestNamespace1 + " http route is loaded in haproxy.config")
		util.EnsureHaproxyBlockConfigContains(oc, custContPod, e2eTestNamespace1, []string{"backend be_http:" + e2eTestNamespace1 + ":service-unsecure"})

		compat_otp.By("9. Check the custom router pod and ensure " + e2eTestNamespace2 + " edge route is loaded in haproxy.config")
		util.EnsureHaproxyBlockConfigContains(oc, custContPod, e2eTestNamespace2, []string{"backend be_edge_http:" + e2eTestNamespace2 + ":route-edge"})
	})

	// Test case creater: hongli@redhat.com
	g.It("Set namespaceOwnership of routeAdmission to Strict", g.Label("OCP-27595"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			e2eTestNamespace2   = "e2e-ne-ocp27595-" + util.GetRandomString()
		)

		compat_otp.By("1. Create an additional namespace for this scenario")
		defer oc.DeleteSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		oc.CreateSpecifiedNamespaceAsAdmin(e2eTestNamespace2)
		e2eTestNamespace1 := oc.Namespace()
		path1 := "/path/first"
		path2 := "/path/second"
		// Use a unique host on every run so a route left behind by an interrupted
		// earlier invocation cannot claim this host and reject the current routes.
		routehost := "ocp27595-" + util.GetRandomString() + ".apps.example.com"

		// Strict is by default so just need to check in default router
		compat_otp.By("2. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be false")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		namespaceOwnershipEnv := util.ReadRouterPodEnv(oc, routerpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=false"))

		compat_otp.By("3. Create a server pod and an unsecure service in one ns")
		util.CreateResourceFromFile(oc, e2eTestNamespace1, testPodSvc)
		util.EnsurePodWithLabelReady(oc, e2eTestNamespace1, "name="+srvrcInfo)

		compat_otp.By("4. Create a reen route with path " + path1 + " in the first ns")
		err := oc.AsAdmin().WithoutNamespace().Run("create").Args("route", "reencrypt", "route-reen", "--service=service-secure", "--hostname="+routehost, "--path="+path1, "-n", e2eTestNamespace1).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.GetRoutes(oc, e2eTestNamespace1)
		util.WaitForOutputEquals(oc, e2eTestNamespace1, "route", "{.items[0].metadata.name}", "route-reen")

		compat_otp.By("5 Check the custom router pod and ensure " + e2eTestNamespace1 + " route is loaded in haproxy.config")
		util.EnsureHaproxyBlockConfigContains(oc, routerpod, e2eTestNamespace1, []string{"backend be_secure:" + e2eTestNamespace1 + ":route-reen"})
		compat_otp.By("6. Create a server pod and an unsecure service in the other ns")
		util.OperateResourceFromFile(oc, "create", e2eTestNamespace2, testPodSvc)
		util.EnsurePodWithLabelReady(oc, e2eTestNamespace2, "name="+srvrcInfo)

		compat_otp.By("7. Create a http route with the same hostname, but with different path " + path2 + " in the second ns")
		err = oc.AsAdmin().WithoutNamespace().Run("expose").Args("service", "service-unsecure", "--hostname="+routehost, "--path="+path2, "-n", e2eTestNamespace2).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.GetRoutes(oc, e2eTestNamespace2)
		util.WaitForOutputEquals(oc, e2eTestNamespace2, "route", "{.items[0].metadata.name}", "service-unsecure")

		compat_otp.By("8. Confirm the route in the second ns is shown as HostAlreadyClaimed")
		util.WaitForOutputContains(oc, e2eTestNamespace2, "route", `{.items[*].status.ingress[?(@.routerName=="default")].conditions[*].reason}`, "HostAlreadyClaimed")
	})

	// Test case creater: hongli@redhat.com
	// For OCP-27596 and OCP-27605
	g.It("Update the namespaceOwnership of routeAdmission", g.Label("OCP-27596"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp27596",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)

		compat_otp.By("1. Create a custom ingresscontroller")
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("2. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be false")
		routerpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		namespaceOwnershipEnv := util.ReadRouterPodEnv(oc, routerpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=false"))

		compat_otp.By("3. Patch the custom ingress controller and set namespaceOwnership to InterNamespaceAllowed")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, "ingresscontrollers/"+ingctrl.Name, "{\"spec\":{\"routeAdmission\":{\"namespaceOwnership\":\"InterNamespaceAllowed\"}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "2")

		compat_otp.By("4. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be true")
		newRouterpod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		namespaceOwnershipEnv = util.ReadRouterPodEnv(oc, newRouterpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=true"))

		compat_otp.By("5. Patch the custom ingress controller and set namespaceOwnership to Strict")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, "ingresscontrollers/"+ingctrl.Name, "{\"spec\":{\"routeAdmission\":{\"namespaceOwnership\":\"Strict\"}}}")

		compat_otp.By("6. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be false")
		namespaceOwnershipEnv = util.ReadRouterPodEnv(oc, routerpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=false"))

		compat_otp.By("7. Patch the custom ingress controller and set namespaceOwnership to Null")
		util.PatchResourceAsAdmin(oc, ingctrl.Namespace, "ingresscontrollers/"+ingctrl.Name, "{\"spec\":{\"routeAdmission\":{\"namespaceOwnership\":null}}}")
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "3")

		compat_otp.By("8. Check the ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK env variable, which should be false")
		newRouterpod = util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)
		namespaceOwnershipEnv = util.ReadRouterPodEnv(oc, newRouterpod, "ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK=false"))

		// Incorporating the negative case OCP-27605 here
		compat_otp.By("9. Patch the custom ingress controller and set namespaceOwnership to a invalid string like 'InvalidTest'")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/"+ingctrl.Name, "-p", "{\"spec\":{\"routeAdmission\":{\"namespaceOwnership\":\"InvalidTest\"}}}", "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(output).To(o.ContainSubstring("spec.routeAdmission.namespaceOwnership: Unsupported value: \"InvalidTest\": supported values: \"InterNamespaceAllowed\", \"Strict\""))
	})

	// Test case creater: hongli@redhat.com
	g.It("Set wildcardPolicy of routeAdmission to WildcardsAllowed", g.Label("OCP-30190"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			srvName             = "service-unsecure"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp30190",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		compat_otp.By("1. Create a custom ingresscontroller")
		ns := oc.Namespace()
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		// Edit a private fixture copy so parallel specs never share modified testdata.
		customTemp = util.CopyFixture(customTemp)
		ingctrl.Template = customTemp
		util.UpdateFilebySedCmd(customTemp, "WildcardsDisallowed", "WildcardsAllowed")
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")
		routehost := "wildcard." + ns + "." + ingctrl.Domain
		anyhost := "any." + ns + "." + ingctrl.Domain
		custContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("2. Check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be true")
		namespaceOwnershipEnv := util.ReadRouterPodEnv(oc, custContPod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=true"))

		compat_otp.By("3. Create a server pod and an unsecure service")
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("4. Expose a http wildcard route")
		err := oc.WithoutNamespace().Run("expose").Args("service", srvName, "--hostname="+routehost, "-n", ns, "--wildcard-policy=Subdomain").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.GetRoutes(oc, ns)

		compat_otp.By("5. Check the reachability of the wildcard route")
		ingressContPod := util.GetPodListByLabel(oc, "openshift-ingress-operator", "name=ingress-operator")
		iplist := util.GetPodIP(oc, "openshift-ingress", custContPod)
		toDst := routehost + ":80:" + iplist[0]
		cmdOnPod := []string{"-n", "openshift-ingress-operator", ingressContPod[0], "--", "curl", "-I", "http://" + routehost, "--resolve", toDst, "--connect-timeout", "10"}
		result, _ := util.RepeatCmdOnClient(oc, cmdOnPod, "200", 30, 1)
		o.Expect(result).To(o.ContainSubstring("200"))

		compat_otp.By("6. Check the reachability of the test route")
		toDst = anyhost + ":80:" + iplist[0]
		cmdOnPod = []string{"-n", "openshift-ingress-operator", ingressContPod[0], "--", "curl", "-I", "http://" + anyhost, "--resolve", toDst, "--connect-timeout", "10"}
		result, _ = util.RepeatCmdOnClient(oc, cmdOnPod, "200", 30, 1)
		o.Expect(result).To(o.ContainSubstring("200"))
	})

	// Test case creater: hongli@redhat.com
	// For OCP-30191 and OCP-30192
	g.It("Set wildcardPolicy of routeAdmission to WildcardsDisallowed", g.Label("OCP-30191"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			srvName             = "service-unsecure"
			ingctrl             = util.IngressControllerDescription{
				Name:      "ocp30191",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		compat_otp.By("1. Create a custom ingresscontroller")
		ns := oc.Namespace()
		ingctrl.Domain = ingctrl.Name + "." + util.GetBaseDomain(oc)
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")
		routehost := "wildcard." + ns + "." + ingctrl.Domain
		custContPod := util.GetOneNewRouterPodFromRollingUpdate(oc, ingctrl.Name)

		compat_otp.By("2. Check the ROUTER_ALLOW_WILDCARD_ROUTES env variable, which should be false")
		namespaceOwnershipEnv := util.ReadRouterPodEnv(oc, custContPod, "ROUTER_ALLOW_WILDCARD_ROUTES")
		o.Expect(namespaceOwnershipEnv).To(o.ContainSubstring("ROUTER_ALLOW_WILDCARD_ROUTES=false"))

		compat_otp.By("3. Create a server pod and an unsecure service")
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("4. Expose a http wildcard route")
		err := oc.WithoutNamespace().Run("expose").Args("service", srvName, "--hostname="+routehost, "-n", ns, "--wildcard-policy=Subdomain").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		util.GetRoutes(oc, ns)

		compat_otp.By("5. Confirm the route status is 'RouteNotAdmitted' and confirm the route is not accessible")
		util.EnsureRouteIsNotAdmittedByIngressController(oc, ns, "service-unsecure", ingctrl.Name)
		ingressContPod := util.GetPodListByLabel(oc, "openshift-ingress-operator", "name=ingress-operator")
		iplist := util.GetPodIP(oc, "openshift-ingress", custContPod)
		curlCmd := fmt.Sprintf("curl --resolve %s:80:%s http://%s -I -k --connect-timeout 10", routehost, iplist[0], routehost)
		statsOut, err := compat_otp.RemoteShPod(oc, "openshift-ingress-operator", ingressContPod[0], "sh", "-c", curlCmd)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(statsOut).Should(o.ContainSubstring("HTTP/1.0 503 Service Unavailable"))

		// Incorporating the negative case OCP-30192 here
		compat_otp.By("6. Patch the custom ingress controller and set wildcardPolicy to a invalid string like 'unknown'")
		output, _ := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresscontroller/"+ingctrl.Name, "-p", "{\"spec\":{\"routeAdmission\":{\"wildcardPolicy\":\"unknown\"}}}", "--type=merge", "-n", ingctrl.Namespace).Output()
		o.Expect(output).To(o.ContainSubstring("spec.routeAdmission.wildcardPolicy: Unsupported value: \"unknown\": supported values: \"WildcardsAllowed\", \"WildcardsDisallowed\""))
	})
})
