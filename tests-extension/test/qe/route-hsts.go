package qe

import (
	"fmt"
	"net/http"
	"path/filepath"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

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
		edgeHost := util.GetRouteHost(oc, ns, "edge-route")

		compat_otp.By("3. Set the Strict-Transport-Security max-age header as annotation")
		util.SetAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=31536000'"})

		compat_otp.By("4. Check the reachability of the host using the HSTS header")
		result := util.WaitForOutsideCurlStatus("--head https://"+edgeHost, "-k", http.StatusOK)
		o.Expect(util.HTTPResponseHeaders(result).Get("Strict-Transport-Security")).To(o.Equal("max-age=31536000"), "OCP-15976: 4. Check the reachability of the host using the HSTS header")

		compat_otp.By("5. Set the Strict-Transport-Security max-age, subdomains header as annotation")
		util.SetAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000;includeSubDomains")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=31536000;includeSubDomains'"})

		compat_otp.By("6. Check the reachability of the host using the HSTS header")
		result = util.WaitForOutsideCurlStatus("--head https://"+edgeHost, "-k", http.StatusOK)
		o.Expect(util.HTTPResponseHeaders(result).Get("Strict-Transport-Security")).To(o.Equal("max-age=31536000;includeSubDomains"), "OCP-15976: 6. Check the reachability of the host using the HSTS header")

		compat_otp.By("7. Set the Strict-Transport-Security max-age, subdomains preload header as annotation")
		util.SetAnnotation(oc, ns, "route/edge-route", "haproxy.router.openshift.io/hsts_header=max-age=100;includeSubDomains;preload")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, ns, []string{"set-header Strict-Transport-Security 'max-age=100;includeSubDomains;preload'"})

		compat_otp.By("8. Check the reachability of the host using the HSTS header")
		result = util.WaitForOutsideCurlStatus("--head https://"+edgeHost, "-k", http.StatusOK)
		o.Expect(util.HTTPResponseHeaders(result).Get("Strict-Transport-Security")).To(o.Equal("max-age=100;includeSubDomains;preload"), "OCP-15976: 8. Check the reachability of the host using the HSTS header")

		// OCP-16368: The reencrypt route should support HSTS
		compat_otp.By("9. Create a reencrypt route")
		util.CreateRoute(oc, ns, "reencrypt", "reen-route", "service-secure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "reen-route", "default")
		reenHost := util.GetRouteHost(oc, ns, "reen-route")

		compat_otp.By("10. Set the Strict-Transport-Security header as annotation")
		util.SetAnnotation(oc, ns, "route/reen-route", "haproxy.router.openshift.io/hsts_header=max-age=100;includeSubDomains;preload")
		util.EnsureHaproxyBlockConfigContains(oc, routerPod, "backend be_secure:"+ns+":reen-route", []string{"set-header Strict-Transport-Security 'max-age=100;includeSubDomains;preload'"})

		compat_otp.By("11. Check the reachability of the host using the HSTS header")
		result = util.WaitForOutsideCurlStatus("--head https://"+reenHost, "-k", http.StatusOK)
		o.Expect(util.HTTPResponseHeaders(result).Get("Strict-Transport-Security")).To(o.Equal("max-age=100;includeSubDomains;preload"), "OCP-15976: 11. Check the reachability of the host using the HSTS header")

		//OCP-16369: The unsecure/passthrough route should NOT support HSTS
		compat_otp.By("12. Create an unsecure route")
		util.CreateRoute(oc, ns, "http", "unsec-route", "service-unsecure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "unsec-route", "default")
		unsecureHost := util.GetRouteHost(oc, ns, "unsec-route")

		compat_otp.By("13. Set the Strict-Transport-Security max-age, subdomains preload header and cookie-name header as annotation")
		util.SetAnnotation(oc, ns, "route/unsec-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000;includeSubDomains;preload")
		util.SetAnnotation(oc, ns, "route/unsec-route", "router.openshift.io/cookie_name=unsecure-cookie_1")
		searchOutput := util.EnsureHaproxyBlockConfigContains(oc, routerPod, "backend be_http:"+ns+":unsec-route", []string{"cookie unsecure-cookie_1"})
		o.Expect(searchOutput).NotTo(o.ContainSubstring(`set-header Strict-Transport-Security`), "OCP-15976: 13. Set the Strict-Transport-Security max-age, subdomains preload header and cookie-name header as annotation")

		compat_otp.By("14. Check the reachability of the unsecure host using the HSTS header")
		result = util.WaitForOutsideCurlStatus("--head http://"+unsecureHost, "", http.StatusOK)
		o.Expect(util.HTTPResponseHeaders(result).Values("Strict-Transport-Security")).To(o.BeEmpty(), "OCP-15976: 14. Check the reachability of the unsecure host using the HSTS header")
		o.Expect(util.HTTPResponseHeaders(result).Get("Set-Cookie")).To(o.HavePrefix("unsecure-cookie_1="), "OCP-15976: 14. Check the reachability of the unsecure host using the HSTS header")

		compat_otp.By("15. Create a passthrough route")
		util.CreateRoute(oc, ns, "passthrough", "pass-route", "service-secure", []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "pass-route", "default")
		passthroughHost := util.GetRouteHost(oc, ns, "pass-route")

		compat_otp.By("16. Set the Strict-Transport-Security max-age header as annotation")
		util.SetAnnotation(oc, ns, "route/pass-route", "haproxy.router.openshift.io/hsts_header=max-age=31536000")
		searchOutput = util.EnsureHaproxyBlockConfigContains(oc, routerPod, "be_tcp:"+ns+":pass-route", []string{ns})
		o.Expect(searchOutput).NotTo(o.ContainSubstring(`set-header Strict-Transport-Security`), "OCP-15976: 16. Set the Strict-Transport-Security max-age header as annotation")

		compat_otp.By("17. Check the reachability of the passthrough host using the HSTS header")
		result = util.WaitForOutsideCurlStatus("--head https://"+passthroughHost, "-k", http.StatusOK)
		o.Expect(util.HTTPResponseHeaders(result).Values("Strict-Transport-Security")).To(o.BeEmpty(), "OCP-15976: 17. Check the reachability of the passthrough host using the HSTS header")
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
		ingctrl.Domain = ingctrl.Name + "-" + util.GetRandomString() + "." + baseDomain
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
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43474: Expose an edge route via the unsecure service inside project")
		o.Expect(output).To(o.ContainSubstring("route-edge"), "OCP-43474: Expose an edge route via the unsecure service inside project")

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43474: Annotate the edge route with preload HSTS header option")
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"), "OCP-43474: Annotate the edge route with preload HSTS header option")

		compat_otp.By("Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be absent")
		setHSTSPolicies := util.NewGlobalHSTSPolicySetter(oc)
		setHSTSPolicies(configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain}, IncludeSubDomainsPolicy: configv1.RequireNoIncludeSubDomains})
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", fmt.Sprintf(`-o=jsonpath={.spec.requiredHSTSPolicies[?(@.domainPatterns[0]==%q)]}`, "*."+ingctldomain)).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43474: Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be absent")
		o.Expect(output).To(o.ContainSubstring("RequireNoIncludeSubDomains"), "OCP-43474: Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be absent")

		compat_otp.By("Annotate the edge route with preload option to verify the effect")
		output1, err2 := oc.Run("annotate").Args("-n", oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000;includeSubDomains", "--overwrite").Output()
		o.Expect(err2).To(o.HaveOccurred(), "OCP-43474: Annotate the edge route with preload option to verify the effect")
		o.Expect(output1).To(o.ContainSubstring("HSTS includeSubDomains must not be specified"), "OCP-43474: Annotate the edge route with preload option to verify the effect")

		compat_otp.By("Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be present")
		setHSTSPolicies(configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain}, IncludeSubDomainsPolicy: configv1.RequireIncludeSubDomains})
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", fmt.Sprintf(`-o=jsonpath={.spec.requiredHSTSPolicies[?(@.domainPatterns[0]==%q)]}`, "*."+ingctldomain)).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43474: Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be present")
		o.Expect(output).To(o.ContainSubstring("RequireIncludeSubDomains"), "OCP-43474: Add the HSTS policy to global ingresses resource with IncludeSubdomain enforced to be present")

		compat_otp.By("verify the enforced policy by overwriting the route annotation to disable Preload headers")
		msg2, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header='max-age=50000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred(), "OCP-43474: verify the enforced policy by overwriting the route annotation to disable Preload headers")
		o.Expect(msg2).To(o.ContainSubstring("HSTS includeSubDomains must be specified"), "OCP-43474: verify the enforced policy by overwriting the route annotation to disable Preload headers")
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
		ingctrl.Domain = ingctrl.Name + "-" + util.GetRandomString() + "." + baseDomain
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
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43475: Expose an edge route via the unsecure service inside project")
		o.Expect(output).To(o.ContainSubstring("route-edge"), "OCP-43475: Expose an edge route via the unsecure service inside project")

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43475: Annotate the edge route with preload HSTS header option")
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"), "OCP-43475: Annotate the edge route with preload HSTS header option")

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload option set to NoOpinion")
		setHSTSPolicies := util.NewGlobalHSTSPolicySetter(oc)
		setHSTSPolicies(configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain}, IncludeSubDomainsPolicy: configv1.NoOpinionIncludeSubDomains, PreloadPolicy: configv1.NoOpinionPreloadPolicy})
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", fmt.Sprintf(`-o=jsonpath={.spec.requiredHSTSPolicies[?(@.domainPatterns[0]==%q)]}`, "*."+ingctldomain)).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43475: Add the HSTS policy to global ingresses resource with preload option set to NoOpinion")
		o.Expect(output).To(o.ContainSubstring("NoOpinion"), "OCP-43475: Add the HSTS policy to global ingresses resource with preload option set to NoOpinion")

		for _, annotation := range []string{
			"max-age=50000",
			"max-age=50000;includeSubDomains",
			"max-age=50000;preload",
			"max-age=50000;includeSubDomains;preload",
			"max-age=50000;preload",
			"max-age=50000;includeSubDomains",
			"max-age=50000",
		} {
			compat_otp.By("Verify NoOpinion permits annotation " + annotation)
			_, err := oc.Run("annotate").Args("route/route-edge", "haproxy.router.openshift.io/hsts_header="+annotation, "--overwrite").Output()
			o.Expect(err).NotTo(o.HaveOccurred(), "NoOpinion rejected HSTS annotation %q", annotation)
		}
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
		ingctrl.Domain = ingctrl.Name + "-" + util.GetRandomString() + "." + baseDomain
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
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43476: Expose an edge route via the unsecure service inside project")
		o.Expect(output).To(o.ContainSubstring("route-edge"), "OCP-43476: Expose an edge route via the unsecure service inside project")

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43476: Annotate the edge route with preload HSTS header option")
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"), "OCP-43476: Annotate the edge route with preload HSTS header option")

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload enforced to be absent")
		setHSTSPolicies := util.NewGlobalHSTSPolicySetter(oc)
		setHSTSPolicies(configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain}, PreloadPolicy: configv1.RequireNoPreloadPolicy})
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", fmt.Sprintf(`-o=jsonpath={.spec.requiredHSTSPolicies[?(@.domainPatterns[0]==%q)]}`, "*."+ingctldomain)).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43476: Add the HSTS policy to global ingresses resource with preload enforced to be absent")
		o.Expect(output).To(o.ContainSubstring("RequireNoPreload"), "OCP-43476: Add the HSTS policy to global ingresses resource with preload enforced to be absent")

		compat_otp.By("Annotate the edge route with preload option to verify the effect")
		output1, err2 := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000;preload", "--overwrite").Output()
		o.Expect(err2).To(o.HaveOccurred(), "OCP-43476: Annotate the edge route with preload option to verify the effect")
		o.Expect(output1).To(o.ContainSubstring("HSTS preload must not be specified"), "OCP-43476: Annotate the edge route with preload option to verify the effect")

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload enforced to be present")
		setHSTSPolicies(configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain}, PreloadPolicy: configv1.RequirePreloadPolicy})
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", fmt.Sprintf(`-o=jsonpath={.spec.requiredHSTSPolicies[?(@.domainPatterns[0]==%q)]}`, "*."+ingctldomain)).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43476: Add the HSTS policy to global ingresses resource with preload enforced to be present")
		o.Expect(output).To(o.ContainSubstring("RequirePreload"), "OCP-43476: Add the HSTS policy to global ingresses resource with preload enforced to be present")

		compat_otp.By("verify the enforced policy by overwriting the route annotation to disable Preload headers")
		msg2, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header='max-age=50000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred(), "OCP-43476: verify the enforced policy by overwriting the route annotation to disable Preload headers")
		o.Expect(msg2).To(o.ContainSubstring("HSTS preload must be specified"), "OCP-43476: verify the enforced policy by overwriting the route annotation to disable Preload headers")
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
		ingctrl.Domain = ingctrl.Name + "-" + util.GetRandomString() + "." + baseDomain
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
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43479: Expose an edge route via the unsecure service inside project")
		o.Expect(output).To(o.ContainSubstring("route-edge"), "OCP-43479: Expose an edge route via the unsecure service inside project")

		compat_otp.By("Annotate the edge route with preload HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge", "haproxy.router.openshift.io/hsts_header=max-age=50000")
		output, err = oc.Run("get").Args("route", "route-edge", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43479: Annotate the edge route with preload HSTS header option")
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"), "OCP-43479: Annotate the edge route with preload HSTS header option")

		compat_otp.By("Add the HSTS policy to global ingresses resource with preload option set to maxAge with lowest and highest timer option")
		setHSTSPolicies := util.NewGlobalHSTSPolicySetter(oc)
		largestMaxAge, smallestMaxAge := int32(40000), int32(100)
		setHSTSPolicies(configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain}, MaxAge: configv1.MaxAgePolicy{LargestMaxAge: &largestMaxAge, SmallestMaxAge: &smallestMaxAge}})
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", fmt.Sprintf(`-o=jsonpath={.spec.requiredHSTSPolicies[?(@.domainPatterns[0]==%q)]}`, "*."+ingctldomain)).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43479: Add the HSTS policy to global ingresses resource with preload option set to maxAge with lowest and highest timer option")
		o.Expect(output).To(o.ContainSubstring("largestMaxAge"), "OCP-43479: Add the HSTS policy to global ingresses resource with preload option set to maxAge with lowest and highest timer option")

		compat_otp.By("verify the enforced policy by overwriting the route annotation with largestMaxAge set higher than globally defined")
		msg2, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header='max-age=50000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred(), "OCP-43479: verify the enforced policy by overwriting the route annotation with largestMaxAge set higher than globally defined")
		o.Expect(msg2).To(o.ContainSubstring("HSTS max-age is greater than maximum age 40000s"), "OCP-43479: verify the enforced policy by overwriting the route annotation with largestMaxAge set higher than globally defined")

		compat_otp.By("verify the enforced policy by overwriting the route annotation with largestMaxAge set lower than globally defined")
		msg2, err = oc.Run("annotate").WithoutNamespace().Args("route/route-edge", "haproxy.router.openshift.io/hsts_header='max-age=50'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred(), "OCP-43479: verify the enforced policy by overwriting the route annotation with largestMaxAge set lower than globally defined")
		o.Expect(msg2).To(o.ContainSubstring("HSTS max-age is less than minimum age 100s"), "OCP-43479: verify the enforced policy by overwriting the route annotation with largestMaxAge set lower than globally defined")

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
		ingctrl1.Domain = ingctrl1.Name + "-" + util.GetRandomString() + "." + baseDomain
		defer ingctrl1.Delete(oc)
		ingctrl1.Create(oc)
		util.EnsureRouterDeployGenerationIs(oc, ingctrl1.Name, "1")

		compat_otp.By("Create second custom ingresscontroller")
		baseDomain = util.GetBaseDomain(oc)
		ingctrl2.Domain = ingctrl2.Name + "-" + util.GetRandomString() + "." + baseDomain
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
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43480: Expose an edge route via the unsecure service through ingresscontroller 1 inside project")
		o.Expect(output1).To(o.ContainSubstring("route-edge1"), "OCP-43480: Expose an edge route via the unsecure service through ingresscontroller 1 inside project")

		compat_otp.By("Expose an edge route via the unsecure service through ingresscontroller 2 inside project")
		var output2 string
		ingctldomain2 := util.GetIngressctlDomain(oc, ingctrl2.Name)
		routehost2 := "route-edge2" + "-" + oc.Namespace() + "." + ingctrl2.Domain
		util.CreateRoute(oc, oc.Namespace(), "edge", "route-edge2", "service-unsecure", []string{"--hostname=" + routehost2})
		output2, err = oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43480: Expose an edge route via the unsecure service through ingresscontroller 2 inside project")
		o.Expect(output2).To(o.ContainSubstring("route-edge2"), "OCP-43480: Expose an edge route via the unsecure service through ingresscontroller 2 inside project")

		compat_otp.By("Annotate the edge route 1 to enable HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge1", "haproxy.router.openshift.io/hsts_header=max-age=4000")
		output, err := oc.Run("get").Args("route", "route-edge1", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43480: Annotate the edge route 1 to enable HSTS header option")
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"), "OCP-43480: Annotate the edge route 1 to enable HSTS header option")

		compat_otp.By("Annotate the edge route 2 to enable HSTS header option")
		util.SetAnnotation(oc, oc.Namespace(), "route/route-edge2", "haproxy.router.openshift.io/hsts_header=max-age=2000")
		output, err = oc.Run("get").Args("route", "route-edge2", "-o=jsonpath={.metadata.annotations}").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43480: Annotate the edge route 2 to enable HSTS header option")
		o.Expect(output).To(o.ContainSubstring("haproxy.router.openshift.io/hsts_header"), "OCP-43480: Annotate the edge route 2 to enable HSTS header option")

		compat_otp.By("Set a different HSTS maxage policy for each domain in the global ingresses configuration")
		setHSTSPolicies := util.NewGlobalHSTSPolicySetter(oc)
		largestMaxAge1, largestMaxAge2, smallestMaxAge := int32(5000), int32(3000), int32(1)
		setHSTSPolicies(
			configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain1}, MaxAge: configv1.MaxAgePolicy{LargestMaxAge: &largestMaxAge1, SmallestMaxAge: &smallestMaxAge}},
			configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain2}, MaxAge: configv1.MaxAgePolicy{LargestMaxAge: &largestMaxAge2, SmallestMaxAge: &smallestMaxAge}},
		)
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", fmt.Sprintf(`-o=jsonpath={.spec.requiredHSTSPolicies[?(@.domainPatterns[0]==%q)]}`, "*."+ingctldomain1)).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43480: Set a different HSTS maxage policy for each domain in the global ingresses configuration")
		o.Expect(output).To(o.ContainSubstring("largestMaxAge"), "OCP-43480: Set a different HSTS maxage policy for each domain in the global ingresses configuration")

		compat_otp.By("verify the enforced policy by overwriting the annotation for route 1  with max-age set  higher than the largestMaxAge defined for the domain")
		msg1, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge1", "haproxy.router.openshift.io/hsts_header='max-age=6000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred(), "OCP-43480: verify the enforced policy by overwriting the annotation for route 1  with max-age set  higher than the largestMaxAge defined for the domain")
		o.Expect(msg1).To(o.ContainSubstring("HSTS max-age is greater than maximum age 5000s"), "OCP-43480: verify the enforced policy by overwriting the annotation for route 1  with max-age set  higher than the largestMaxAge defined for the domain")

		compat_otp.By("verify the enforced policy by overwriting the annotation for route 2  with max-age set  higher than the largestMaxAge defined for the domain")
		msg2, err := oc.Run("annotate").WithoutNamespace().Args("route/route-edge2", "haproxy.router.openshift.io/hsts_header='max-age=4000'", "--overwrite").Output()
		o.Expect(err).To(o.HaveOccurred(), "OCP-43480: verify the enforced policy by overwriting the annotation for route 2  with max-age set  higher than the largestMaxAge defined for the domain")
		o.Expect(msg2).To(o.ContainSubstring("HSTS max-age is greater than maximum age 3000s"), "OCP-43480: verify the enforced policy by overwriting the annotation for route 2  with max-age set  higher than the largestMaxAge defined for the domain")

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
		ingctrl.Domain = ingctrl.Name + "-" + util.GetRandomString() + "." + baseDomain
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
		setHSTSPolicies := util.NewGlobalHSTSPolicySetter(oc)
		largestMaxAge, smallestMaxAge := int32(5000), int32(1)
		setHSTSPolicies(configv1.RequiredHSTSPolicy{DomainPatterns: []string{"*." + ingctldomain}, MaxAge: configv1.MaxAgePolicy{LargestMaxAge: &largestMaxAge, SmallestMaxAge: &smallestMaxAge}, NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": project1}}})
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingresses.config.openshift.io/cluster", fmt.Sprintf(`-o=jsonpath={.spec.requiredHSTSPolicies[?(@.domainPatterns[0]==%q)]}`, "*."+ingctldomain)).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43884: set up HSTS policy for the custom domain with namespace selector set to label of project1 namespace")
		o.Expect(output).To(o.ContainSubstring("largestMaxAge"), "OCP-43884: set up HSTS policy for the custom domain with namespace selector set to label of project1 namespace")

		compat_otp.By("Test for outcome by creating an edge route via the HSTS implemented domain through the project1")
		routehost1 := "route-edge" + "-" + project1 + "." + ingctrl.Domain
		output, err1 := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project1, "route", "edge", "route-edge", "--service=service-unsecure", "--hostname="+routehost1).Output()
		o.Expect(err1).To(o.HaveOccurred(), "OCP-43884: Test for outcome by creating an edge route via the HSTS implemented domain through the project1")
		o.Expect(output).To(o.ContainSubstring("HSTS max-age must be set correctly in HSTS annotation"), "OCP-43884: Test for outcome by creating an edge route via the HSTS implemented domain through the project1")

		compat_otp.By("Test for outcome by creating an edge route via the default non-HSTS policy controlled domain through the project2")
		routehost2 := "route-edge2" + "-" + project2 + "." + ingctrl.Domain
		_, err2 := oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", project2, "route", "edge", "route-edge", "--service=service-unsecure", "--hostname="+routehost2).Output()
		o.Expect(err2).NotTo(o.HaveOccurred(), "OCP-43884: Test for outcome by creating an edge route via the default non-HSTS policy controlled domain through the project2")
		output2, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "-n", project2).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-43884: Test for outcome by creating an edge route via the default non-HSTS policy controlled domain through the project2")
		o.Expect(output2).To(o.ContainSubstring("route-edge2"), "OCP-43884: Test for outcome by creating an edge route via the default non-HSTS policy controlled domain through the project2")

	})

})
