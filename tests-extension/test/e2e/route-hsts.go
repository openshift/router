package router

import (
	"github.com/openshift/router-tests-extension/test/testdata"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	//e2e "k8s.io/kubernetes/test/e2e/framework"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
)

var _ = g.Describe("[sig-network-edge][OTP] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-hsts", compat_otp.KubeConfigPath())

	// incorporate OCP-15976, OCP-16368 and OCP-16369 into one
	// Test case creater: zzhao@redhat.com, modified by iamin@redhat.com - OCP-15976: The edge route should support HSTS
	// Test case creater: zzhao@redhat.com - OCP-16368: The reencrypt route should support HSTS
	// Test case creater: zzhao@redhat.com, modfiied by iamin@redhat.com - OCP-16369: The unsecure/passthrough route should NOT support HSTS
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-Critical-15976-The edge/reencrypt route supports HSTS but unsecure/passthrough route does not", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")

		compat_otp.By("1. Create a server pod and its service")
		routerPod := getOneRouterPodNameByIC(oc, "default")
		ns := oc.Namespace()
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Create an edge route")
		createRoute(oc, ns, "edge", "edge-route", "service-unsecure", []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "edge-route", "default")
		edgeHost := "edge-route-" + ns + ".apps." + getBaseDomain(oc)

		compat_otp.By("3. Set the Strict-Transport-Security max-age header as annotation")
		setAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000")
		ensureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=31536000'"})

		compat_otp.By("4. Check the reachability of the host using the HSTS header")
		result := waitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).To(o.ContainSubstring(`strict-transport-security: max-age=31536000`))

		compat_otp.By("5. Set the Strict-Transport-Security max-age, subdomains header as annotation")
		setAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000;includeSubDomains")
		ensureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=31536000;includeSubDomains'"})

		compat_otp.By("6. Check the reachability of the host using the HSTS header")
		result = waitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).To(o.ContainSubstring(`strict-transport-security: max-age=31536000;includeSubDomains`))

		compat_otp.By("7. Set the Strict-Transport-Security max-age, subdomains preload header as annotation")
		setAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=100;includeSubDomains;preload")
		ensureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=100;includeSubDomains;preload'"})

		compat_otp.By("8. Check the reachability of the host using the HSTS header")
		result = waitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).To(o.ContainSubstring(`strict-transport-security: max-age=100;includeSubDomains;preload`))

		// OCP-16368: The reencrypt route should support HSTS
		compat_otp.By("9. Create a reencrypt route")
		createRoute(oc, ns, "reencrypt", "reen-route", "service-secure", []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "reen-route", "default")
		reenHost := "reen-route-" + ns + ".apps." + getBaseDomain(oc)

		compat_otp.By("10. Set the Strict-Transport-Security header as annotation")
		setAnnotation(oc, ns, "route/reen-route", "haproxy.router.openshift.io/hsts_header=max-age=100;includeSubDomains;preload")
		ensureHaproxyBlockConfigContains(oc, routerPod, "backend be_secure:"+ns+":reen-route", []string{"set-header Strict-Transport-Security 'max-age=100;includeSubDomains;preload'"})

		compat_otp.By("11. Check the reachability of the host using the HSTS header")
		result = waitForOutsideCurlContains("--head https://"+reenHost, "-k", `200`)
		o.Expect(result).To(o.ContainSubstring(`strict-transport-security: max-age=100;includeSubDomains;preload`))

		//OCP-16369: The unsecure/passthrough route should NOT support HSTS
		compat_otp.By("12. Create an unsecure route")
		createRoute(oc, ns, "http", "unsec-route", "service-unsecure", []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "unsec-route", "default")
		unsecureHost := "unsec-route-" + ns + ".apps." + getBaseDomain(oc)

		compat_otp.By("13. Set the Strict-Transport-Security max-age, subdomains preload header and cookie-name header as annotation")
		setAnnotation(oc, ns, "route/unsec-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000;includeSubDomains;preload")
		setAnnotation(oc, ns, "route/unsec-route", "router.openshift.io/cookie_name=unsecure-cookie_1")
		searchOutput := ensureHaproxyBlockConfigContains(oc, routerPod, "backend be_http:"+ns+":unsec-route", []string{"cookie unsecure-cookie_1"})
		o.Expect(searchOutput).NotTo(o.ContainSubstring(`set-header Strict-Transport-Security`))

		compat_otp.By("14. Check the reachability of the unsecure host using the HSTS header")
		result = waitForOutsideCurlContains("--head http://"+unsecureHost, "", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`strict-transport-security`))
		o.Expect(result).To(o.MatchRegexp("(S|s)et-(C|c)ookie: unsecure-cookie_1"))

		compat_otp.By("15. Create a passthrough route")
		createRoute(oc, ns, "passthrough", "pass-route", "service-secure", []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "pass-route", "default")
		passthroughHost := "pass-route-" + ns + ".apps." + getBaseDomain(oc)

		compat_otp.By("16. Set the Strict-Transport-Security max-age header as annotation")
		setAnnotation(oc, ns, "route/pass-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000")
		searchOutput = ensureHaproxyBlockConfigContains(oc, routerPod, "be_tcp:"+ns+":pass-route", []string{ns})
		o.Expect(searchOutput).NotTo(o.ContainSubstring(`set-header Strict-Transport-Security`))

		compat_otp.By("17. Check the reachability of the passthrough host using the HSTS header")
		result = waitForOutsideCurlContains("--head https://"+passthroughHost, "-k", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`strict-transport-security`))
	})

	// Test case creater: zzhao@redhat.com, modified by iamin@redhat.com - OCP-15977: Negative testing for route HSTS policy
	g.It("Author:iamin-NonHyperShiftHOST-Low-15977-Negative testing for route HSTS policy", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")

		compat_otp.By("1. Create a server pod and its service")
		routerPod := getOneRouterPodNameByIC(oc, "default")
		ns := oc.Namespace()
		createResourceFromWebServer(oc, ns, testPodSvc, "web-server-deploy")

		compat_otp.By("2. Create an edge route")
		createRoute(oc, ns, "edge", "edge-route", "service-unsecure", []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "edge-route", "default")
		edgeHost := "edge-route-" + ns + ".apps." + getBaseDomain(oc)

		compat_otp.By("3. Set the Negative Strict-Transport-Security max-age header and cookie-name header as annotation")
		setAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=-20")
		setAnnotation(oc, ns, "route/edge-route", "router.openshift.io/cookie_name=edge-with-invalid-hsts")
		ensureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"cookie edge-with-invalid-hsts"})
		ensureHaproxyBlockConfigNotContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security"})

		compat_otp.By("4. Check the reachability of the host using a negative HSTS header")
		result := waitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`strict-transport-security`))
		o.Expect(result).To(o.ContainSubstring(`set-cookie: edge-with-invalid-hsts`))

		compat_otp.By("5. Set an invalid NOT 'includeSubDomains' Strict-Transport-Security header and cookie-name header as annotation")
		setAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=20;invalid")
		setAnnotation(oc, ns, "route/edge-route", "router.openshift.io/cookie_name=edge-with-invalid-hsts-subdomain")
		ensureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"cookie edge-with-invalid-hsts-subdomain"})
		ensureHaproxyBlockConfigNotContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security"})

		compat_otp.By("6. Check the reachability of the host using an invalid HSTS header")
		result = waitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`strict-transport-security`))
		o.Expect(result).To(o.ContainSubstring(`set-cookie: edge-with-invalid-hsts-subdomain`))

		compat_otp.By("7. Set an invalid NOT 'preload' Strict-Transport-Security header and cookie-name header as annotation")
		setAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=20;includeSubDomains;invalid")
		setAnnotation(oc, ns, "route/edge-route", "router.openshift.io/cookie_name=edge-with-invalid-hsts-preload")
		ensureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"cookie edge-with-invalid-hsts-preload"})
		ensureHaproxyBlockConfigNotContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security"})

		compat_otp.By("8. Check the reachability of the edge host using an invalid preload HSTS header")
		result = waitForOutsideCurlContains("--head https://"+edgeHost, "-k", `200`)
		o.Expect(result).NotTo(o.ContainSubstring(`strict-transport-security`))
		o.Expect(result).To(o.ContainSubstring(`set-cookie: edge-with-invalid-hsts-preload`))
	})

	// author: aiyengar@redhat.com
	// https://issues.redhat.com/browse/OCPBUGS-43431
	g.It("Author:aiyengar-NonHyperShiftHOST-Critical-43474-The includeSubDomainsPolicy parameter can configure subdomain policy to inherit the HSTS policy of parent domain [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43474",
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
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		oc.SetupProject()
		createResourceFromFile(oc, oc.Namespace(), testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := getIngressctlDomain(oc, ingctrl.name)
		routehost := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.domain
		createRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		setAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be absent")
		defer patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireNoIncludeSubDomains\" , \"maxAge\":{}}]}]")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireNoIncludeSubDomains\", \"maxAge\":{}}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("RequireNoIncludeSubDomains"))

		compat_otp.By("Annotate the edge route with preload option to verify the effect")
		output1, err2 := oc.Run("annotate").Args("-n", oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000;includeSubDomains", "--overwrite").Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("HSTS includeSubDomains must not be specified"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be present")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"includeSubDomainsPolicy\" : \"RequireIncludeSubDomains\"}]}]")
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
	g.It("Author:aiyengar-NonHyperShiftHOST-High-43475-The includeSubDomainsPolicy option can be configured to be permissive with NoOpinion flag [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43475",
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
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		oc.SetupProject()
		createResourceFromFile(oc, oc.Namespace(), testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := getIngressctlDomain(oc, ingctrl.name)
		routehost := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.domain
		createRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		setAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload option set to NoOpinion")
		defer patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireIncludeSubDomains\" , \"maxAge\":{}, \"preloadPolicy\" :\"NoOpinion\"}]}]")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"preloadPolicy\" :\"NoOpinion\"}]}]")
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
	g.It("Author:aiyengar-NonHyperShiftHOST-Critical-43476-The PreloadPolicy option can be set to be enforced strictly to be present or absent in HSTS preload header checks [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43476",
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
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		oc.SetupProject()
		createResourceFromFile(oc, oc.Namespace(), testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := getIngressctlDomain(oc, ingctrl.name)
		routehost := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.domain
		createRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		setAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload enforced to be absent")
		defer patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireIncludeSubDomains\" , \"maxAge\":{}, \"preloadPolicy\" :\"RequireNoPreload\"}]}]")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"preloadPolicy\" :\"RequireNoPreload\"}]}]")
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("RequireNoPreload"))

		compat_otp.By("Annotate the edge route with preload option to verify the effect")
		output1, err2 := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000;preload", "--overwrite").Output()
		o.Expect(err2).To(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("HSTS preload must not be specified"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload enforced to be present")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"preloadPolicy\" :\"RequirePreload\"}]}]")
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
	g.It("Author:aiyengar-NonHyperShiftHOST-High-43478-The PreloadPolicy option can be configured to be permissive with NoOpinion flag [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43478",
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
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		oc.SetupProject()
		createResourceFromFile(oc, oc.Namespace(), testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := getIngressctlDomain(oc, ingctrl.name)
		routedomain := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.domain
		createRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routedomain})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		setAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload option set to NoOpinion")
		defer patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"includeSubDomainsPolicy\" : \"RequireIncludeSubDomains\" , \"maxAge\":{}, \"preloadPolicy\" :\"NoOpinion\"}]}]")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{}, \"preloadPolicy\" :\"NoOpinion\"}]}]")
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
	g.It("Author:aiyengar-NonHyperShiftHOST-High-43479-The Maxage HSTS policy strictly adheres to validation of route based based on largestMaxAge and smallestMaxAge parameter [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43479",
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
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		oc.SetupProject()
		createResourceFromFile(oc, oc.Namespace(), testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service inside project")
		var output string
		ingctldomain := getIngressctlDomain(oc, ingctrl.name)
		routehost := "route-edge" + "-" + oc.Namespace() + "." + ingctrl.domain
		createRoute(oc, oc.Namespace(), "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost})
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("route-edge"))

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		setAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload option set to maxAge with lowest and highest timer option")
		defer patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{\"largestMaxAge\": 40000, \"smallestMaxAge\": 100 }}]}]")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"] , \"maxAge\":{\"largestMaxAge\": 40000, \"smallestMaxAge\": 100 }}]}]")
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
	g.It("Author:aiyengar-NonHyperShiftHOST-High-43480-The HSTS domain policy can be configure with multiple domainPatterns options [Disruptive]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl1 = ingressControllerDescription{
				name:      "ocp43480-1",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ingctrl2 = ingressControllerDescription{
				name:      "ocp43480-2",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)
		compat_otp.By("Create first custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "1")

		compat_otp.By("Create second custom ingresscontroller")
		baseDomain = getBaseDomain(oc)
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")

		compat_otp.By("Deploy project with pods and service resources")
		oc.SetupProject()
		createResourceFromFile(oc, oc.Namespace(), testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Expose an edge route via the unsecure service through ingresscontroller 1 inside project")
		var output1 string
		ingctldomain1 := getIngressctlDomain(oc, ingctrl1.name)
		routehost1 := "route-edge1" + "-" + oc.Namespace() + "." + ingctrl1.domain
		createRoute(oc, oc.Namespace(), "edge", "route-edge1", "service-unsecure", []string{"--hostname=" + routehost1})
		output1, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("route-edge1"))

		compat_otp.By("Expose an edge route via the unsecure service through ingresscontroller 2 inside project")
		var output2 string
		ingctldomain2 := getIngressctlDomain(oc, ingctrl2.name)
		routehost2 := "route-edge2" + "-" + oc.Namespace() + "." + ingctrl2.domain
		createRoute(oc, oc.Namespace(), "edge", "route-edge2", "service-unsecure", []string{"--hostname=" + routehost2})
		output2, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output2).To(o.ContainSubstring("route-edge2"))

		compat_otp.By("Annotate the edge route 1 to enable HSTS header option")
		setAnnotation(oc, oc.Namespace(), "route/route-edge1", "haproxy.router.openshift.io/hsts_header=max-age=4000")
		output, err := oc.Run("get").Args("route", "route-edge1", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Annotate the edge route 2 to enable HSTS header option")
		setAnnotation(oc, oc.Namespace(), "route/route-edge2", "haproxy.router.openshift.io/hsts_header=max-age=2000")
		output, err = oc.Run("get").Args("route", "route-edge2", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"))

		compat_otp.By("Set a different HSTS maxage policy for each domain in the global ingresses configuration")
		defer patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\"}]")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain1+"'"+"] , \"includeSubDomainsPolicy\":\"NoOpinion\",\"maxAge\":{\"largestMaxAge\":5000,\"smallestMaxAge\":1},\"preloadPolicy\":\"NoOpinion\"},{\"domainPatterns\":"+" ['*"+"."+ingctldomain2+"'"+"],\"includeSubDomainsPolicy\":\"NoOpinion\",\"maxAge\":{\"largestMaxAge\":3000,\"smallestMaxAge\":1},\"preloadPolicy\":\"NoOpinion\"}]}]")
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
	g.It("Author:aiyengar-NonHyperShiftHOST-High-43884-lobal HSTS policy can be enforced strictly on a specific namespace using namespaceSelector for given domain pattern filtering [Serial]", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43884",
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
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Deploy project 1 with pods and service resources")
		oc.SetupProject()
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("Deploy project 2 with pods and service resources")
		oc.SetupProject()
		project2 := oc.Namespace()
		createResourceFromFile(oc, project2, testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("set up HSTS policy for the custom domain with namespace selector set to label of project1 namespace")
		ingctldomain := getIngressctlDomain(oc, ingctrl.name)
		defer patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"remove\" , \"path\" : \"/spec/requiredHSTSPolicies\"}]")
		patchGlobalResourceAsAdmin(oc, "ingresses.config.openshift.io/cluster", "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :"+"['*"+"."+ingctldomain+"'"+"],\"includeSubDomainsPolicy\":\"NoOpinion\",\"maxAge\":{\"largestMaxAge\":5000,\"smallestMaxAge\":1},\"namespaceSelector\":{\"matchLabels\":{\"kubernetes.io/metadata.name\":\""+project1+"\"}},\"preloadPolicy\":\"NoOpinion\"}]}]")
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", "-o=jsonpath={.spec.requiredHSTSPolicies[0]}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("largestMaxAge"))

		compat_otp.By("Test for outcome by creating an edge route via the HSTS implemented domain through the project1")
		routehost1 := "route-edge" + "-" + project1 + "." + ingctrl.domain
		output, err1 := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "route", "edge", "route-edge", "--service=service-unsecure", "--hostname="+routehost1).Output()
		o.Expect(err1).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("HSTS max-age must be set correctly in HSTS annotation"))

		compat_otp.By("Test for outcome by creating an edge route via the default non-HSTS policy controlled domain through the project2")
		routehost2 := "route-edge2" + "-" + project2 + "." + ingctrl.domain
		createRoute(oc, project2, "edge", "route-edge", "service-unsecure", []string{"--hostname=" + routehost2})
		output2, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output2).To(o.ContainSubstring("route-edge2"))

	})

	// author: aiyengar@redhat.com
	g.It("Author:aiyengar-Low-43966-Negative values for largestMaxAge and smallestMaxAge option under Maxage HSTS policy are rejected", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43966",
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
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("Add the HSTS policy with  largestMaxAge set to negative value")
		ingctldomain := getIngressctlDomain(oc, ingctrl.name)
		patch := "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :" + "['*" + "." + ingctldomain + "'" + "] , \"maxAge\":{\"largestMaxAge\": -40000, \"smallestMaxAge\": 100 }}]}]"
		output1, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresses.config.openshift.io/cluster", "--patch="+patch, "--type=json").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output1).To(o.ContainSubstring("largestMaxAge in body should be greater than or equal to 0"))

		compat_otp.By("Add the HSTS policy with  smallestMaxAge set to negative value")
		patch = "[{\"op\":\"add\" , \"path\" : \"/spec/requiredHSTSPolicies\" , \"value\" : [{\"domainPatterns\" :" + "['*" + "." + ingctldomain + "'" + "] , \"maxAge\":{\"largestMaxAge\": 40000, \"smallestMaxAge\": -100 }}]}]"
		output2, err := oc.AsAdmin().WithoutNamespace().Run("patch").Args("ingresses.config.openshift.io/cluster", "--patch="+patch, "--type=json").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output2).To(o.ContainSubstring("smallestMaxAge in body should be greater than or equal to 0"))
	})
})
