package qe

import (
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	//e2e "k8s.io/kubernetes/test/e2e/framework"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	util "github.com/openshift/router-tests-extension/test"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-HSTS", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-hsts", compat_otp.KubeConfigPath())

	// incorporate OCP-15976, OCP-16368 and OCP-16369 into one
	// Test case creater: zzhao@redhat.com, modified by iamin@redhat.com - OCP-15976: The edge route should support HSTS
	// Test case creater: zzhao@redhat.com - OCP-16368: The reencrypt route should support HSTS
	// Test case creater: zzhao@redhat.com, modfiied by iamin@redhat.com - OCP-16369: The unsecure/passthrough route should NOT support HSTS
	g.It("The edge/reencrypt route supports HSTS but unsecure/passthrough route does not", g.Label("OCP-15976"), func() {
		buildPruningBaseDir := TestdataDir()
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")

		compat_otp.By("1. Create a server pod and its service")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		ns := oc.Namespace()
		util.CreateResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Create an edge route")
		util.CreateRoute(oc, ns, "edge", "edge-route", "service-unsecure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "edge-route", "default")
		edgeHost := "edge-route-" + ns + ".apps." + util.GetBaseDomain(oc)

		compat_otp.By("3. Set the Strict-Transport-Security max-age header as annotation")
		util.SetAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=31536000'"})

		compat_otp.By("4. Check the reachability of the host using the HSTS header")
		result := util.WaitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).To(o.ContainSubstring(`strict-transport-security: max-age=31536000`))

		compat_otp.By("5. Set the Strict-Transport-Security max-age, subdomains header as annotation")
		util.SetAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000;includeSubDomains")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=31536000;includeSubDomains'"})

		compat_otp.By("6. Check the reachability of the host using the HSTS header")
		result = util.WaitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).To(o.ContainSubstring(`strict-transport-security: max-age=31536000;includeSubDomains`))

		compat_otp.By("7. Set the Strict-Transport-Security max-age, subdomains preload header as annotation")
		util.SetAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=100;includeSubDomains;preload")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=100;includeSubDomains;preload'"})

		compat_otp.By("8. Check the reachability of the host using the HSTS header")
		result = util.WaitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).To(o.ContainSubstring(`strict-transport-security: max-age=100;includeSubDomains;preload`))

		// OCP-16368: The reencrypt route should support HSTS
		compat_otp.By("9. Create a reencrypt route")
		util.CreateRoute(oc, ns, "reencrypt", "reen-route", "service-secure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "reen-route", "default")
		reenHost := "reen-route-" + ns + ".apps." + util.GetBaseDomain(oc)

		compat_otp.By("10. Set the Strict-Transport-Security header as annotation")
		util.SetAnnotation(oc, ns, "route/reen-route", "haproxy.router.openshift.io/hsts_header=max-age=100;includeSubDomains;preload")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, "backend be_secure:"+ns+":reen-route", []string{"set-header Strict-Transport-Security 'max-age=100;includeSubDomains;preload'"})

		compat_otp.By("11. Check the reachability of the host using the HSTS header")
		result = util.WaitForOutsideCurlContains("--head https://"+reenHost, "-k", `200`)
		o.Expect(result).To(o.ContainSubstring(`strict-transport-security: max-age=100;includeSubDomains;preload`))

		//OCP-16369: The unsecure/passthrough route should NOT support HSTS
		compat_otp.By("12. Create an unsecure route")
		util.CreateRoute(oc, ns, "http", "unsec-route", "service-unsecure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "unsec-route", "default")
		unsecureHost := "unsec-route-" + ns + ".apps." + util.GetBaseDomain(oc)

		compat_otp.By("13. Set the Strict-Transport-Security max-age, subdomains preload header and cookie-name header as annotation")
		util.SetAnnotation(oc, ns, "route/unsec-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000;includeSubDomains;preload")
		util.SetAnnotation(oc, ns, "route/unsec-route", "router.openshift.io/cookie_name=unsecure-cookie_1")
		searchOutput := util.EnsureHaproxyBlockConfigContains(oc, routerPod, "backend be_http:"+ns+":unsec-route", []string{"cookie unsecure-cookie_1"})
		o.Expect(searchOutput).NotTo(o.ContainSubstring(`set-header Strict-Transport-Security`))

		compat_otp.By("14. Check the reachability of the unsecure host using the HSTS header")
		result = util.WaitForOutsideCurlContains("--head http://"+unsecureHost, "", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`strict-transport-security`))
		o.Expect(result).To(o.MatchRegexp("(S|s)et-(C|c)ookie: unsecure-cookie_1"))

		compat_otp.By("15. Create a passthrough route")
		util.CreateRoute(oc, ns, "passthrough", "pass-route", "service-secure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "pass-route", "default")
		passthroughHost := "pass-route-" + ns + ".apps." + util.GetBaseDomain(oc)

		compat_otp.By("16. Set the Strict-Transport-Security max-age header as annotation")
		util.SetAnnotation(oc, ns, "route/pass-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000")
		searchOutput = util.EnsureHaproxyBlockConfigContains(oc, routerPod, "be_tcp:"+ns+":pass-route", []string{ns})
		o.Expect(searchOutput).NotTo(o.ContainSubstring(`set-header Strict-Transport-Security`))

		compat_otp.By("17. Check the reachability of the passthrough host using the HSTS header")
		result = util.WaitForOutsideCurlContains("--head https://"+passthroughHost, "-k", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`strict-transport-security`))
	})

	// author: aiyengar@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43431
	g.It("The includeSubDomainsPolicy parameter can configure subdomain policy to inherit the HSTS policy of parent domain [Serial]", g.Label("OCP-43474"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp43474",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		util.CreateResourceFromFile(oc, oc.Namespace(), testPodSvc)
		util.EnsurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := util.GetIngressctlDomain(oc, ingctrl.Name)
		routehost := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.Domain
		util.CreateRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be absent")
		defer util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireNoIncludeSubDomains\" , \"maxAge\":{}}]}]")
		util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireNoIncludeSubDomains\", \"maxAge\":{}}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("RequireNoIncludeSubDomains"))

		compat_otp.By("Annotate the edge route with preload option to verify the effect")
		output1, err2 := oc.Run("annotate").Args("-n", oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000;includeSubDomains", "--overwrite").Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("HSTS includeSubDomains must not be specified"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be present")
		util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"includeSubDomainsPolicy\" : \"RequireIncludeSubDomains\"}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("RequireIncludeSubDomains"))

		compat_otp.By("verify the enforced policy by overwriting the route annotation to disable Preload headers")
		msg2, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header='max-age=50000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(msg2).To(o.ContainSubstring("HSTS includeSubDomains must be specified"))
	})

	// author: aiyengar@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43431
	g.It("The includeSubDomainsPolicy and PreloadPolicy option can be configured to be permissive with NoOpinion flag [Serial]", g.Label("OCP-43475"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp43475",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		util.CreateResourceFromFile(oc, oc.Namespace(), testPodSvc)
		util.EnsurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := util.GetIngressctlDomain(oc, ingctrl.Name)
		routehost := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.Domain
		util.CreateRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload option set to NoOpinion")
		defer util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireIncludeSubDomains\" , \"maxAge\":{}, \"preloadPolicy\" :\"NoOpinion\"}]}]")
		util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"preloadPolicy\" :\"NoOpinion\"}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("NoOpinion"))

		compat_otp.By("Annotate the edge route with preload option to verify")
		_, err2 := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000;preload", "--overwrite").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())

		compat_otp.By("Annotate the edge route without preload option to verify")
		_, err2 = oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000", "--overwrite").Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
	})

	// author: aiyengar@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43431
	g.It("The PreloadPolicy option can be set to be enforced strictly to be present or absent in HSTS preload header checks [Serial]", g.Label("OCP-43476"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp43476",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		util.CreateResourceFromFile(oc, oc.Namespace(), testPodSvc)
		util.EnsurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := util.GetIngressctlDomain(oc, ingctrl.Name)
		routehost := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.Domain
		util.CreateRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload enforced to be absent")
		defer util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireIncludeSubDomains\" , \"maxAge\":{}, \"preloadPolicy\" :\"RequireNoPreload\"}]}]")
		util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"preloadPolicy\" :\"RequireNoPreload\"}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("RequireNoPreload"))

		compat_otp.By("Annotate the edge route with preload option to verify the effect")
		output1, err2 := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000;preload", "--overwrite").Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("HSTS preload must not be specified"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload enforced to be present")
		util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"preloadPolicy\" :\"RequirePreload\"}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("RequirePreload"))

		compat_otp.By("verify the enforced policy by overwriting the route annotation to disable Preload headers")
		msg2, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header='max-age=50000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(msg2).To(o.ContainSubstring("HSTS preload must be specified"))
	})

	// author: aiyengar@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43431
	g.It("The Maxage HSTS policy strictly adheres to validation of route based on largestMaxAge and smallestMaxAge parameter [Serial]", g.Label("OCP-43479"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp43479",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		util.CreateResourceFromFile(oc, oc.Namespace(), testPodSvc)
		util.EnsurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := util.GetIngressctlDomain(oc, ingctrl.Name)
		routehost := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.Domain
		util.CreateRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload option set to maxAge with lowest and highest timer option")
		defer util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{\"largestMaxAge\": 40000, \"smallestMaxAge\": 100 }}]}]")
		util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{\"largestMaxAge\": 40000, \"smallestMaxAge\": 100 }}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("largestMaxAge"))

		compat_otp.By("verify the enforced policy by overwriting the route annotation with largestMaxAge set higher than globally defined")
		msg2, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header='max-age=50000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(msg2).To(o.ContainSubstring("HSTS max-age is greater than maximum age 40000s"))

		compat_otp.By("verify the enforced policy by overwriting the route annotation with largestMaxAge set lower than globally defined")
		msg2, err = oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header='max-age=50'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(msg2).To(o.ContainSubstring("HSTS max-age is less than minimum age 100s"))

	})

	// author: aiyengar@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43431
	g.It("The HSTS domain policy can be configured with multiple domainPatterns options [Disruptive]", g.Label("OCP-43480"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl1 = util.IngressControllerDescription{
				Name:      "ocp43480-1",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
			ingctrl2 = util.IngressControllerDescription{
				Name:      "ocp43480-2",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		compat_otp.By("Create first custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl1.Domain = ingctrl1.Name + "." + baseDomain
		defer ingctrl1.Delete(oc)
		ingctrl1.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl1.Name, "1")

		compat_otp.By("Create second custom ingresscontroller")
		baseDomain = util.GetBaseDomain(oc)
		ingctrl2.Domain = ingctrl2.Name + "." + baseDomain
		defer ingctrl2.Delete(oc)
		ingctrl2.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl2.Name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		util.CreateResourceFromFile(oc, oc.Namespace(), testPodSvc)
		util.EnsurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service through ingresscontroller 1 inside project")
		var output1 string
		ingctldomain1 := util.GetIngressctlDomain(oc, ingctrl1.Name)
		routehost1 := "route-edge1" + "-" + oc.Namespace() + "." + ingctrl1.Domain
		util.CreateRoute(oc, oc.Namespace(), "edge", "route-edge1", "service-unsecure", []string{"--hostname=" + routehost1})
		output1, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("route-edge1"))

		compat_otp.By("Expose an edge route via the unsecure service through ingresscontroller 2 inside project")
		var output2 string
		ingctldomain2 := util.GetIngressctlDomain(oc, ingctrl2.Name)
		routehost2 := "route-edge2" + "-" + oc.Namespace() + "." + ingctrl2.Domain
		util.CreateRoute(oc, oc.Namespace(), "edge", "route-edge2", "service-unsecure", []string{"--hostname=" + routehost2})
		output2, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output2).To(o.ContainSubstring("route-edge2"))

		compat_otp.By("Annotate the edge route 1 to enable HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge1", "haproxy.router.openshift.io/hsts_header=max-age=4000")
		output, err := oc.Run("get").Args("route", "route-edge1", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Annotate the edge route 2 to enable HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge2", "haproxy.router.openshift.io/hsts_header=max-age=2000")
		output, err = oc.Run("get").Args("route", "route-edge2", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Set a different HSTS maxage policy for each domain in the global ingresses configuration")
		defer util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\"}]")
		util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain1+"'"+"] , \"includeSubDomainsPolicy\":\"NoOpinion\",\"maxAge\":{\"largestMaxAge\":5000,\"smallestMaxAge\":1},\"preloadPolicy\":\"NoOpinion\"},{\"domainPatterns\":"+" ['*"+"."+ingctldomain2+"'"+"],\"includeSubDomainsPolicy\":\"NoOpinion\",\"maxAge\":{\"largestMaxAge\":3000,\"smallestMaxAge\":1},\"preloadPolicy\":\"NoOpinion\"}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("largestMaxAge"))

		compat_otp.By("verify the enforced policy by overwriting the annotation for route 1  with max-age set  higher than the largestMaxAge defined for the domain")
		msg1, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge1", "haproxy.router.openshift.io/hsts_header='max-age=6000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(msg1).To(o.ContainSubstring("HSTS max-age is greater than maximum age 5000s"))

		compat_otp.By("verify the enforced policy by overwriting the annotation for route 2  with max-age set  higher than the largestMaxAge defined for the domain")
		msg2, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge2", "haproxy.router.openshift.io/hsts_header='max-age=4000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(msg2).To(o.ContainSubstring("HSTS max-age is greater than maximum age 3000s"))

	})

	// author: aiyengar@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43431
	g.It("Global HSTS policy can be enforced strictly on a specific namespace using namespaceSelector for given domain pattern filtering [Serial]", g.Label("OCP-43884"), func() {
		buildPruningBaseDir := TestdataDir()
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = util.IngressControllerDescription{
				Name:      "ocp43884",
				Namespace: "openshift-ingress-operator",
				Domain:    "",
				Template:  customTemp,
			}
		)
		compat_otp.By("Create one custom ingresscontroller")
		baseDomain := util.GetBaseDomain(oc)
		ingctrl.Domain = ingctrl.Name + "." + baseDomain
		defer ingctrl.Delete(oc)
		ingctrl.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl.Name, "1")

		compat_otp.By("Deploy project 1 with pods and service resources")
		project1 := oc.Namespace()
		util.CreateResourceFromFile(oc, project1, testPodSvc)
		util.EnsurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Deploy project 2 with pods and service resources")
		project2 := "e2e-ne-ocp43884-" + util.GetRandomString()
		defer oc.DeleteSpecifiedNamespaceAsAdmin(project2)
		oc.CreateSpecifiedNamespaceAsAdmin(project2)
		util.OperateResourceFromFile(oc, "create", project2, testPodSvc)
		util.EnsurePodWithLabelReady(oc, project2, "name=web-server-deploy")

		compat_otp.By("set up HSTS policy for the custom domain with namespace selector set to label of project1 namespace")
		ingctldomain := util.GetIngressctlDomain(oc, ingctrl.Name)
		defer util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\"}]")
		util.PatchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"],\"includeSubDomainsPolicy\":\"NoOpinion\",\"maxAge\":{\"largestMaxAge\":5000,\"smallestMaxAge\":1},\"namespaceSelector\":{\"matchLabels\":{\"kubernetes.io/metadata.name\":\""+project1+"\"}},\"preloadPolicy\":\"NoOpinion\"}]}]")
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("largestMaxAge"))

		compat_otp.By("Test for outcome by creating an edge route via the HSTS implemented domain through the project1")
		routehost1 := "route-edge" + "-" + project1 + "." + ingctrl.Domain
		output, err1 := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "route", "edge", "route-edge", "--service=service-unsecure", "--hostname="+routehost1).Output()
		o.Expect(err1).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("HSTS max-age must be set correctly in HSTS annotation"))

		compat_otp.By("Test for outcome by creating an edge route via the default non-HSTS policy controlled domain through the project2")
		routehost2 := "route-edge2" + "-" + project2 + "." + ingctrl.Domain
		_, err2 := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project2, "route", "edge", "route-edge", "--service=service-unsecure", "--hostname="+routehost2).Output()
		o.Expect(err2).NotTo(o.HaveOccurred())
		output2, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", project2).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output2).To(o.ContainSubstring("route-edge2"))

	})

})
