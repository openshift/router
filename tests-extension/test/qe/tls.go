package qe

import (
	"os"
	"path/filepath"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	util "github.com/openshift/router-tests-extension/test"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-TLS", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-tls", compat_otp.KubeConfigPath())

	// Includes OCP-12557: only the updated route's certificate files change.
	//Author:shudili
	g.It("The certs for the edge and reencrypt termination routes are removed when the routes are removed", g.Label("OCP-12563"), func() {
		if compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("Skip because Dynamic Configuration Manager does not update backend certificate files in real time")
		}

		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
		)

		dirname, err := os.MkdirTemp("", "ocp-12563-")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private certificate directory")
		defer func() {
			o.Expect(os.RemoveAll(dirname)).NotTo(o.HaveOccurred(), "failed to remove private certificate directory")
		}()

		caSubj := "/CN=NE-Test-Root-CA"
		caCrt := filepath.Join(dirname, "ca.crt")
		caKey := filepath.Join(dirname, "ca.key")
		edgeRouteCrt := filepath.Join(dirname, "edge-route.crt")
		edgeRouteKey := filepath.Join(dirname, "edge-route.key")
		edgeRouteCsr := filepath.Join(dirname, "edge-route.csr")
		reenRouteCrt := filepath.Join(dirname, "reen-route.crt")
		reenRouteKey := filepath.Join(dirname, "reen-route.key")
		reenRouteCsr := filepath.Join(dirname, "reen-route.csr")
		reenRouteDstCrt := filepath.Join(dirname, "reen-route-dst.crt")
		reenRouteDstKey := filepath.Join(dirname, "reen-route-dst.key")

		baseDomain := util.GetBaseDomain(oc)
		edgeRoute := "edge12563.apps." + baseDomain
		reenRoute := "reen12563.apps." + baseDomain

		compat_otp.By("1. Create the test CA and route certificates")
		util.OpenSSLNewCA(caKey, caCrt, caSubj)
		util.OpenSSLNewCSR(edgeRouteKey, edgeRouteCsr, "/CN=example-edge.com")
		util.OpenSSLSignCSR("subjectAltName = DNS:"+edgeRoute, edgeRouteCsr, caCrt, caKey, edgeRouteCrt)
		util.OpenSSLNewCSR(reenRouteKey, reenRouteCsr, "/CN=example-reen.com")
		util.OpenSSLSignCSR("subjectAltName = DNS:"+reenRoute, reenRouteCsr, caCrt, caKey, reenRouteCrt)
		util.OpenSSLNewCA(reenRouteDstKey, reenRouteDstCrt, "/CN=example-reen-dst.com")

		compat_otp.By("2. Create a deployment with unsecure and secure services")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("3. Create the edge and reencrypt routes")
		util.CreateRoute(oc, ns, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute, "--ca-cert=" + caCrt, "--cert=" + edgeRouteCrt, "--key=" + edgeRouteKey})
		util.CreateRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{"--hostname=" + reenRoute, "--ca-cert=" + caCrt, "--cert=" + reenRouteCrt, "--key=" + reenRouteKey, "--dest-ca-cert=" + reenRouteDstCrt})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		compat_otp.By("4. Check the initial route certificate files")
		routerpod := util.GetOneRouterPodNameByIC(oc, "default")
		edgeCertInitialTime := util.CheckRouteCertificationInRouterPod(oc, ns, "route-edge", routerpod, "certs", "--hasCert")
		reenCertInitialTime := util.CheckRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "certs", "--hasCert")
		reenCACertInitialTime := util.CheckRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "cacerts", "--hasCert")

		compat_otp.By("5. Update only the reencrypt route")
		util.PatchResourceAsAdmin(oc, ns, "route/route-reen", `{"spec":{"path":"/test"}}`)
		output, err := oc.Run("get").Args("route/route-reen", "-n", ns, "-o=jsonpath={.spec.path}").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to read the updated route path")
		o.Expect(output).To(o.Equal("/test"))

		compat_otp.By("6. Verify the edge certificate is unchanged while the reencrypt certificates are replaced")
		edgeUpdatedCertTime := util.CheckRouteCertificationInRouterPod(oc, ns, "route-edge", routerpod, "certs", "--hasCert")
		reenUpdatedCertTime := util.CheckRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "certs", "--hasCert")
		reenCACertUpdatedTime := util.CheckRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "cacerts", "--hasCert")
		o.Expect(edgeUpdatedCertTime).To(o.ContainSubstring(edgeCertInitialTime), "edge route certificate should not change")
		o.Expect(reenUpdatedCertTime).NotTo(o.ContainSubstring(reenCertInitialTime), "reencrypt route certificate should be replaced")
		o.Expect(reenCACertUpdatedTime).NotTo(o.ContainSubstring(reenCACertInitialTime), "reencrypt destination CA should be replaced")

		compat_otp.By("7. Delete both routes")
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "route", "route-edge").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to delete the edge route")
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "route", "route-reen").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to delete the reencrypt route")

		compat_otp.By("8. Verify the route certificate files are removed")
		util.CheckRouteCertificationInRouterPod(oc, ns, "route-edge", routerpod, "certs", "--noCert")
		util.CheckRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "certs", "--noCert")
		util.CheckRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "cacerts", "--noCert")
	})

	// Includes OCP-19799: invalid TLS material can be replaced through a route patch.
	//Author:iamin
	g.It("Default haproxy router can skip invalid certificate routes", g.Label("OCP-12573"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
		)

		dirname, err := os.MkdirTemp("", "ocp-12573-")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private certificate directory")
		defer func() {
			o.Expect(os.RemoveAll(dirname)).NotTo(o.HaveOccurred(), "failed to remove private certificate directory")
		}()

		caSubj := "/CN=NE-Test-Root-CA"
		caCrt1 := filepath.Join(dirname, "ca1.crt")
		caKey1 := filepath.Join(dirname, "ca1.key")
		caCrt2 := filepath.Join(dirname, "ca2.crt")
		caKey2 := filepath.Join(dirname, "ca2.key")
		edgeRouteCrt1 := filepath.Join(dirname, "edge-route1.crt")
		edgeRouteKey1 := filepath.Join(dirname, "edge-route1.key")
		edgeRouteCsr1 := filepath.Join(dirname, "edge-route1.csr")
		edgeRouteCrt2 := filepath.Join(dirname, "edge-route2.crt")
		edgeRouteKey2 := filepath.Join(dirname, "edge-route2.key")
		edgeRouteCsr2 := filepath.Join(dirname, "edge-route2.csr")
		reenRouteCrt1 := filepath.Join(dirname, "reen-route1.crt")
		reenRouteKey1 := filepath.Join(dirname, "reen-route1.key")
		reenRouteCsr1 := filepath.Join(dirname, "reen-route1.csr")
		reenRouteCrt2 := filepath.Join(dirname, "reen-route2.crt")
		reenRouteKey2 := filepath.Join(dirname, "reen-route2.key")
		reenRouteCsr2 := filepath.Join(dirname, "reen-route2.csr")
		reenRouteDstCrt1 := filepath.Join(dirname, "reen-route-dst1.crt")
		reenRouteDstKey1 := filepath.Join(dirname, "reen-route-dst1.key")

		baseDomain := util.GetBaseDomain(oc)
		edgeRoute1 := "ocp12573-edge1.apps." + baseDomain
		reenRoute1 := "ocp12573-reen1.apps." + baseDomain
		edgeRoute2 := "ocp12573-edge2.apps." + baseDomain
		reenRoute2 := "ocp12573-reen2.apps." + baseDomain

		compat_otp.By("1. Create two CAs and mismatched route certificates")
		util.OpenSSLNewCA(caKey1, caCrt1, caSubj)
		util.OpenSSLNewCA(caKey2, caCrt2, caSubj)
		util.OpenSSLNewCSR(edgeRouteKey1, edgeRouteCsr1, "/CN=example-edge.com")
		util.OpenSSLNewCSR(edgeRouteKey2, edgeRouteCsr2, "/CN=example-edge.com")
		util.OpenSSLSignCSR("subjectAltName = DNS:"+edgeRoute1, edgeRouteCsr1, caCrt1, caKey1, edgeRouteCrt1)
		util.OpenSSLSignCSR("subjectAltName = DNS:"+edgeRoute2, edgeRouteCsr2, caCrt2, caKey2, edgeRouteCrt2)
		util.OpenSSLNewCSR(reenRouteKey1, reenRouteCsr1, "/CN=example-reen.com")
		util.OpenSSLNewCSR(reenRouteKey2, reenRouteCsr2, "/CN=example-reen.com")
		util.OpenSSLSignCSR("subjectAltName = DNS:"+reenRoute1, reenRouteCsr1, caCrt1, caKey1, reenRouteCrt1)
		util.OpenSSLSignCSR("subjectAltName = DNS:"+reenRoute2, reenRouteCsr2, caCrt2, caKey2, reenRouteCrt2)
		util.OpenSSLNewCA(reenRouteDstKey1, reenRouteDstCrt1, "/CN=example-reen-dst.com")

		compat_otp.By("2. Create a deployment with unsecure and secure services")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("3. Create edge routes with invalid certificate and key combinations")
		util.CreateRoute(oc, ns, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute1, "--ca-cert=" + caCrt1, "--cert=" + edgeRouteCrt2, "--key=" + edgeRouteKey2})
		util.CreateRoute(oc, ns, "edge", "route-edge2", unSecSvcName, []string{"--hostname=" + edgeRoute2, "--ca-cert=" + caCrt2, "--cert=" + edgeRouteCrt2, "--key=" + edgeRouteKey1})

		compat_otp.By("4. Create reencrypt routes with invalid certificates")
		util.CreateRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{"--hostname=" + reenRoute1, "--ca-cert=" + caCrt1, "--cert=" + reenRouteCrt2, "--key=" + reenRouteKey1, "--dest-ca-cert=" + reenRouteDstCrt1})
		util.CreateRoute(oc, ns, "reencrypt", "route-reen2", secSvcName, []string{"--hostname=" + reenRoute2, "--ca-cert=" + caCrt1, "--cert=" + reenRouteCrt2, "--key=" + reenRouteKey1, "--dest-ca-cert=" + reenRouteDstCrt1})

		compat_otp.By("5. Verify all four routes fail extended validation")
		routeOutput := util.GetRoutes(oc, ns)
		o.Expect(strings.Count(routeOutput, "ExtendedValidationFailed")).To(o.Equal(4), "all invalid certificate routes should be rejected")

		compat_otp.By("6. Replace the edge route TLS fields")
		util.PatchResourceAsAdmin(oc, ns, "route/route-edge", `{"spec":{"tls":{"key":"qe","certificate":"ocp","caCertificate":"redhat"}}}`)

		compat_otp.By("7. Verify the patched TLS fields are retained")
		patch, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "route-edge", "-n", ns, "-o=jsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to read the patched route TLS configuration")
		o.Expect(patch).To(o.ContainSubstring(`"caCertificate":"redhat","certificate":"ocp","key":"qe"`))
	})

	//Author:iamin
	g.It("Route cannot be accessed if the backend cannot be matched by the default destination CA of router", g.Label("OCP-14089"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			secSvcName          = "service-secure"
		)

		compat_otp.By("1. Create a deployment with a secure service")
		ns := oc.Namespace()
		util.CreateResourceFromFile(oc, ns, testPodSvc)
		util.EnsurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2. Create a reencrypt route without an explicit destination CA")
		util.CreateRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		compat_otp.By("3. Verify the route returns 503 while using the default destination CA")
		reenHost := "route-reen-" + ns + ".apps." + util.GetBaseDomain(oc)
		util.WaitForOutsideCurlContains("https://"+reenHost, "-I -k", "HTTP/1.0 503")

		compat_otp.By("4. Verify the route help documents the destination CA default")
		output, err := oc.Run("create").Args("route", "reencrypt", "-h").Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to read route creation help")
		o.Expect(output).To(o.And(o.ContainSubstring("--dest-ca-cert"), o.ContainSubstring("Defaults to the Service CA")))
	})

})
