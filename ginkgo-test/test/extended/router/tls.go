package router

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	e2e "k8s.io/kubernetes/test/e2e/framework"

	"github.com/openshift/origin/test/extended/util/compat_otp"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-tls", compat_otp.KubeConfigPath())

	// incorporate OCP-12557, OCP-12563 into one
	// Test case creater: bmeng@redhat.com - OCP-12557: Only the certs file of the certain route will be updated when the route is updated
	// Test case creater: hongli@redhat.com - OCP-12563: The certs for the edge/reencrypt termination routes should be removed when the routes removed
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-12563-The certs for the edge/reencrypt termination routes should be removed when the routes removed", func() {
		// skip the test if featureSet is set there
		if compat_otp.IsTechPreviewNoUpgrade(oc) {
			g.Skip("Skip for the haproxy was't the realtime for the backend configuration after enabled DynamicConfigurationManager")
		}

		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			dirname             = "/tmp/OCP-12563"
			caSubj              = "/CN=NE-Test-Root-CA"
			caCrt               = dirname + "/12563-ca.crt"
			caKey               = dirname + "/12563-ca.key"
			edgeRouteSubj       = "/CN=example-edge.com"
			edgeRouteCrt        = dirname + "/12563-edgeroute.crt"
			edgeRouteKey        = dirname + "/12563-edgeroute.key"
			edgeRouteCsr        = dirname + "/12563-edgeroute.csr"
			reenRouteSubj       = "/CN=example-reen.com"
			reenRouteCrt        = dirname + "/12563-reenroute.crt"
			reenRouteKey        = dirname + "/12563-reenroute.key"
			reenRouteCsr        = dirname + "/12563-reenroute.csr"
			reenRouteDstSubj    = "/CN=example-reen-dst.com"
			reenRouteDstCrt     = dirname + "/12563-reenroutedst.crt"
			reenRouteDstKey     = dirname + "/12563-reenroutedst.key"
		)

		compat_otp.By("1.0 Create a file folder and prepair for testing")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		baseDomain := getBaseDomain(oc)
		edgeRoute := "edge12563.apps." + baseDomain
		reenRoute := "reen12563.apps." + baseDomain

		compat_otp.By("2.0: Use openssl to create ca certification and key")
		opensslNewCa(caKey, caCrt, caSubj)

		compat_otp.By("3.0: Create a user CSR and the user key for the edge route")
		opensslNewCsr(edgeRouteKey, edgeRouteCsr, edgeRouteSubj)

		compat_otp.By("3.1: Sign the user CSR and generate the certificate for the edge route")
		san := "subjectAltName = DNS:" + edgeRoute
		opensslSignCsr(san, edgeRouteCsr, caCrt, caKey, edgeRouteCrt)

		compat_otp.By("4.0: Create a user CSR and the user key for the reen route")
		opensslNewCsr(reenRouteKey, reenRouteCsr, reenRouteSubj)

		compat_otp.By("4.1: Sign the user CSR and generate the certificate for the reen route")
		san = "subjectAltName = DNS:" + reenRoute
		opensslSignCsr(san, reenRouteCsr, caCrt, caKey, reenRouteCrt)

		compat_otp.By("5.0: Use openssl to create certification and key for the destination certification of the reen route")
		opensslNewCa(reenRouteDstKey, reenRouteDstCrt, reenRouteDstSubj)

		compat_otp.By("6.0 Deploy a project with a deployment")
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=web-server-deploy")

		compat_otp.By("7.0: Create the edge route and the reen route")
		createRoute(oc, project1, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute, "--ca-cert=" + caCrt, "--cert=" + edgeRouteCrt, "--key=" + edgeRouteKey})
		createRoute(oc, project1, "reencrypt", "route-reen", secSvcName, []string{"--hostname=" + reenRoute, "--ca-cert=" + caCrt, "--cert=" + reenRouteCrt, "--key=" + reenRouteKey, "--dest-ca-cert=" + reenRouteDstCrt})
		ensureRouteIsAdmittedByIngressController(oc, project1, "route-edge", "default")
		ensureRouteIsAdmittedByIngressController(oc, project1, "route-reen", "default")

		compat_otp.By("8.0: Check the certs for the edge/reencrypt termination routes")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		edgeCertIntialTime := checkRouteCertificationInRouterPod(oc, project1, "route-edge", routerpod, "certs", "--hasCert")
		reenCertIntialTime := checkRouteCertificationInRouterPod(oc, project1, "route-reen", routerpod, "certs", "--hasCert")

		compat_otp.By("9.0: Check the cacert for the reencrypt termination route")
		reencaCertIntialTime := checkRouteCertificationInRouterPod(oc, project1, "route-reen", routerpod, "cacerts", "--hasCert")

		// OCP-12557: Only the certs file of the certain route will be updated when that route is updated
		compat_otp.By("10.0: Show the cert files creation time")
		e2e.Logf("The intial edge certificate creation details is %s", edgeCertIntialTime)
		e2e.Logf("The intial reen certificate creation details is %s", reenCertIntialTime)
		e2e.Logf("The intial reen CA certificate creation details is %s", reencaCertIntialTime)

		compat_otp.By("11.0: Patch the reen route with path varibale")
		patchResourceAsAdmin(oc, project1, "route/route-reen", `{"spec": {"path": "/test"}}`)
		output, err := oc.Run("get").Args("route/route-reen", "-n", project1, "-o=jsonpath={.spec.path}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`/test`))

		compat_otp.By("12.0: Recheck the creation time of the certs")
		// the cert details of reen route will be updated and the edge route will be same
		edgeUpdatedCertTime := checkRouteCertificationInRouterPod(oc, project1, "route-edge", routerpod, "certs", "--hasCert")
		reenUpdatedCertTime := checkRouteCertificationInRouterPod(oc, project1, "route-reen", routerpod, "certs", "--hasCert")
		reencaCertUpdatedTime := checkRouteCertificationInRouterPod(oc, project1, "route-reen", routerpod, "cacerts", "--hasCert")
		e2e.Logf("The Updated edge certificate creation details is %s", edgeUpdatedCertTime)
		e2e.Logf("The Updated reen certificate creation details is %s", reenUpdatedCertTime)
		e2e.Logf("The Updated reen CA certificate creation details is %s", reencaCertUpdatedTime)
		o.Expect(edgeCertIntialTime).To(o.ContainSubstring(edgeUpdatedCertTime))
		o.Expect(reenCertIntialTime).NotTo(o.ContainSubstring(reenUpdatedCertTime))
		o.Expect(reencaCertIntialTime).NotTo(o.ContainSubstring(reencaCertUpdatedTime))

		compat_otp.By("13.0: Delete the two routes")
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", project1, "route", "route-edge").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", project1, "route", "route-reen").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("14.0: Check the certs for the edge/reencrypt termination routes again after deleted the routes")
		checkRouteCertificationInRouterPod(oc, project1, "route-edge", routerpod, "certs", "--noCert")
		checkRouteCertificationInRouterPod(oc, project1, "route-reen", routerpod, "certs", "--noCert")

		compat_otp.By("15.0: Check the cacert for the reencrypt termination route again after deleted the route")
		checkRouteCertificationInRouterPod(oc, project1, "route-reen", routerpod, "cacerts", "--noCert")
	})

	// author: iamin@redhat.com
	//Combine tls cases OCP-12573 and OCP-19799
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-12573-Default haproxy router should be able to skip invalid cert route", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			secSvcName          = "service-secure"
			dirname             = "/tmp/OCP-12573"
			caSubj              = "/CN=NE-Test-Root-CA"
			caCrt1              = dirname + "/12573-ca1.crt"
			caKey1              = dirname + "/12573-ca1.key"
			caCrt2              = dirname + "/12573-ca2.crt"
			caKey2              = dirname + "/12573-ca2.key"
			edgeRouteSubj       = "/CN=example-edge.com"
			edgeRouteCrt1       = dirname + "/12573-edgeroute1.crt"
			edgeRouteKey1       = dirname + "/12573-edgeroute1.key"
			edgeRouteCsr1       = dirname + "/12573-edgeroute1.csr"
			edgeRouteCrt2       = dirname + "/12573-edgeroute2.crt"
			edgeRouteKey2       = dirname + "/12573-edgeroute2.key"
			edgeRouteCsr2       = dirname + "/12573-edgeroute2.csr"
			reenRouteSubj       = "/CN=example-reen.com"
			reenRouteCrt1       = dirname + "/12573-reenroute1.crt"
			reenRouteKey1       = dirname + "/12573-reenroute1.key"
			reenRouteCsr1       = dirname + "/12573-reenroute1.csr"
			reenRouteCrt2       = dirname + "/12573-reenroute2.crt"
			reenRouteKey2       = dirname + "/12573-reenroute2.key"
			reenRouteCsr2       = dirname + "/12573-reenroute2.csr"
			reenRouteDstSubj    = "/CN=example-reen-dst.com"
			reenRouteDstCrt1    = dirname + "/12573-reenroutedst1.crt"
			reenRouteDstKey1    = dirname + "/12573-reenroutedst1.key"
			reenRouteDstCrt2    = dirname + "/12573-reenroutedst2.crt"
			reenRouteDstKey2    = dirname + "/12573-reenroutedst2.key"
		)

		compat_otp.By("1.0 Create a file folder and prepare for testing")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		baseDomain := getBaseDomain(oc)
		edgeRoute1 := "ocp12573-edge1.apps." + baseDomain
		reenRoute1 := "ocp12573-reen1.apps." + baseDomain
		edgeRoute2 := "ocp12573-edge2.apps." + baseDomain
		reenRoute2 := "ocp12573-reen2.apps." + baseDomain

		compat_otp.By("2.0: Use openssl to create ca certification and key")
		opensslNewCa(caKey1, caCrt1, caSubj)
		opensslNewCa(caKey2, caCrt2, caSubj)

		compat_otp.By("3.0: Create a user CSR and the user key for the edge route")
		opensslNewCsr(edgeRouteKey1, edgeRouteCsr1, edgeRouteSubj)
		opensslNewCsr(edgeRouteKey2, edgeRouteCsr2, edgeRouteSubj)

		compat_otp.By("3.1: Sign the user CSR and generate the certificate for the edge route")
		san1 := "subjectAltName = DNS:" + edgeRoute1
		san2 := "subjectAltName = DNS:" + edgeRoute2
		opensslSignCsr(san1, edgeRouteCsr1, caCrt1, caKey1, edgeRouteCrt1)
		opensslSignCsr(san2, edgeRouteCsr2, caCrt2, caKey2, edgeRouteCrt2)

		compat_otp.By("4.0: Create a user CSR and the user key for the reen route")
		opensslNewCsr(reenRouteKey1, reenRouteCsr1, reenRouteSubj)
		opensslNewCsr(reenRouteKey2, reenRouteCsr2, reenRouteSubj)

		compat_otp.By("4.1: Sign the user CSR and generate the certificate for the reen route")
		san1 = "subjectAltName = DNS:" + reenRoute1
		san2 = "subjectAltName = DNS:" + reenRoute2
		opensslSignCsr(san1, reenRouteCsr1, caCrt1, caKey1, reenRouteCrt1)
		opensslSignCsr(san2, reenRouteCsr2, caCrt2, caKey2, reenRouteCrt2)

		compat_otp.By("5.0: Use openssl to create certification and key for the destination certification of the reen route")
		opensslNewCa(reenRouteDstKey1, reenRouteDstCrt1, reenRouteDstSubj)
		opensslNewCa(reenRouteDstKey2, reenRouteDstCrt2, reenRouteDstSubj)

		compat_otp.By("6.0 Deploy a project with a deployment")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("7.0: Create the edge route with invalid caCert and key")
		createRoute(oc, ns, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute1, "--ca-cert=" + caCrt1, "--cert=" + edgeRouteCrt2, "--key=" + edgeRouteKey2})
		createRoute(oc, ns, "edge", "route-edge2", unSecSvcName, []string{"--hostname=" + edgeRoute2, "--ca-cert=" + caCrt2, "--cert=" + edgeRouteCrt2, "--key=" + edgeRouteKey1})

		compat_otp.By("8.0: Create the reencrypt route with invalid cert and destCA")
		createRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{"--hostname=" + reenRoute1, "--ca-cert=" + caCrt1, "--cert=" + reenRouteCrt2, "--key=" + reenRouteKey1, "--dest-ca-cert=" + reenRouteDstCrt1})
		createRoute(oc, ns, "reencrypt", "route-reen2", secSvcName, []string{"--hostname=" + reenRoute2, "--ca-cert=" + caCrt1, "--cert=" + reenRouteCrt2, "--key=" + reenRouteKey1, "--dest-ca-cert=" + reenRouteDstCrt1})

		compat_otp.By("9.0: Check the routes Host section")
		routeOutput := getRoutes(oc, ns)
		o.Expect(strings.Count(routeOutput, `ExtendedValidationFailed`) == 4).To(o.BeTrue())

		compat_otp.By("10.0: Edit the tls spec section of the edge route")
		patchResourceAsAdmin(oc, ns, "route/route-edge", `{"spec":{"tls" :{"key": "qe","certificate": "ocp","caCertificate": "redhat"}}}`)

		compat_otp.By("11.0: Check the tls spec to see updated info")
		patch, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "route-edge", "-n", ns, "-ojsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(patch).To(o.ContainSubstring(`"caCertificate":"redhat","certificate":"ocp","key":"qe"`))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-14089-Route cannot be accessed if the backend cannot be matched by the default destination CA of router", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			secSvcName          = "service-secure"
		)

		compat_otp.By("1.0 Deploy a project with a deployment")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		compat_otp.By("2.0 Create the reencrypt route with the backend not matched the the default destination CA of router")
		createRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		compat_otp.By("3.0: Curl the route while it uses the defaulted destCA")
		reenHost := "route-reen-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("https://"+reenHost, "-I -k", `HTTP/1.0 503`)

		compat_otp.By("4.0: Check the route help section")
		output, err := oc.Run("create").Args("route", "reencrypt", "-h").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring("--dest-ca-cert"), o.ContainSubstring("Defaults to the Service CA")))
	})

	// also includes OCP-25665/25666/25668/25703
	// author: hongli@redhat.com
	g.It("Author:hongli-WRS-ROSA-OSD_CCS-ARO-Critical-25702-V-BR.12-the tlsSecurityProfile in ingresscontroller can be updated", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp25702",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("create custom IC without tls profile config (Intermediate is default)")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		// OCP-25703
		compat_otp.By("check default TLS config and it should be same to Intermediate profile")
		newrouterpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		env := readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384`))

		// OCP-25665
		compat_otp.By("patch custom IC with tls profile Old and check the config")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{"spec":{"tlsSecurityProfile":{"type":"Old"}}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.1`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA256:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA`))

		// OCP-25666
		compat_otp.By("patch custom IC with tls profile Intermidiate and check the config")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{"spec":{"tlsSecurityProfile":{"type":"Intermediate"}}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384`))

		// OCP-25668
		compat_otp.By("patch custom IC with tls profile Custom and check the config")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{"spec":{"tlsSecurityProfile":{"type":"Custom","custom":{"ciphers":["DHE-RSA-AES256-GCM-SHA384","ECDHE-ECDSA-AES256-GCM-SHA384"],"minTLSVersion":"VersionTLS12"}}}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "4")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES256-GCM-SHA384`))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-WRS-LEVEL0-Critical-43300-V-ACS.05-enable client certificate with optional policy", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		cmFile := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43300",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		defer deleteConfigMap(oc, "openshift-config", "client-ca-43300")
		createConfigMapFromFile(oc, "openshift-config", "client-ca-43300", cmFile)

		compat_otp.By("create and patch custom IC to enable client certificate with Optional policy")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp43300", "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-43300\"},\"clientCertificatePolicy\":\"Optional\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check client certification config after custom router rolled out")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env := readRouterPodEnv(oc, newrouterpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH=optional`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH_CA=/etc/pki/tls/client-ca/ca-bundle.pem`))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-WRS-Medium-43301-V-ACS.05-enable client certificate with required policy", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		cmFile := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43301",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("create configmap client-ca-xxxxx in namespace openshift-config")
		defer deleteConfigMap(oc, "openshift-config", "client-ca-43301")
		createConfigMapFromFile(oc, "openshift-config", "client-ca-43301", cmFile)

		compat_otp.By("create and patch custom IC to enable client certificate with required policy")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp43301", "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-43301\"},\"clientCertificatePolicy\":\"Required\",\"allowedSubjectPatterns\":[\"www.test2.com\"]}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check client certification config after custom router rolled out")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env := readRouterPodEnv(oc, newrouterpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH=required`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH_CA=/etc/pki/tls/client-ca/ca-bundle.pem`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH_FILTER=(?:www.test2.com)`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-WRS-Critical-43284-V-CM.01-setting tlssecurityprofile to TLSv1.3", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43284",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("create and patch the ingresscontroller to enable tls security profile to modern type TLSv1.3")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp43284", "{\"spec\":{\"tlsSecurityProfile\":{\"type\":\"Modern\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		compat_otp.By("check the env variable of the router pod")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, "ocp43284")
		tlsProfile := readRouterPodEnv(oc, newrouterpod, "TLS")
		o.Expect(tlsProfile).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.3`))
		o.Expect(tlsProfile).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))

		compat_otp.By("check the haproxy config on the router pod to ensure the ssl version TLSv1.3 is reflected")
		tlsVersion := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "ssl-min-ver")
		o.Expect(tlsVersion).To(o.ContainSubstring(`ssl-default-bind-options ssl-min-ver TLSv1.3`))

		compat_otp.By("check the haproxy config on the router pod to ensure the tls1.3 ciphers are enabled")
		tlsCliper := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "sl-default-bind-ciphersuites")
		o.Expect(tlsCliper).To(o.ContainSubstring(`ssl-default-bind-ciphersuites TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-50842-destination-ca-certificate-secret annotation for destination CA Opaque certifcate", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
		ingressTemp := filepath.Join(buildPruningBaseDir, "ingress-destCA.yaml")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		caCert := filepath.Join(buildPruningBaseDir, "ca-bundle.pem")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp50842",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ing = ingressDescription{
				name:        "ingress-dca-opq",
				namespace:   "",
				domain:      "",
				serviceName: "service-secure",
				template:    ingressTemp,
			}
		)

		compat_otp.By("create project and a pod")
		baseDomain := getBaseDomain(oc)
		createResourceFromFile(oc, oc.Namespace(), testPodSvc)
		ensurePodWithLabelReady(oc, oc.Namespace(), "name=web-server-deploy")
		podName := getPodListByLabel(oc, oc.Namespace(), "name=web-server-deploy")

		compat_otp.By("create custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, "ocp50842")

		compat_otp.By("create a secret with destination CA Opaque certificate")
		createGenericSecret(oc, oc.Namespace(), "service-secret", "tls.crt", caCert)

		compat_otp.By("create ingress and get the details")
		ing.domain = ingctrl.name + "." + baseDomain
		ing.namespace = oc.Namespace()
		ing.create(oc)
		getIngress(oc, oc.Namespace())
		getRoutes(oc, oc.Namespace())
		routeNames := getResourceName(oc, oc.Namespace(), "route")

		compat_otp.By("check whether route details are present in custom controller domain")
		waitForOutputContains(oc, oc.Namespace(), "route/"+routeNames[0], "{.metadata.annotations}", `"route.openshift.io/destination-ca-certificate-secret":"service-secret"`)
		host := fmt.Sprintf(`service-secure-%s.ocp50842.%s`, oc.Namespace(), baseDomain)
		waitForOutputEquals(oc, oc.Namespace(), "route/"+routeNames[0], "{.spec.host}", host)

		compat_otp.By("check the reachability of the host in custom controller")
		controlerIP := getPodv4Address(oc, custContPod, "openshift-ingress")
		curlCmd := []string{"-n", oc.Namespace(), podName[0], "--", "curl", "https://service-secure-" + oc.Namespace() +
			".ocp50842." + baseDomain + ":443", "-k", "-I", "--resolve", "service-secure-" + oc.Namespace() + ".ocp50842." +
			baseDomain + ":443:" + controlerIP, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		compat_otp.By("check the router pod and ensure the routes are loaded in haproxy.config of custom controller")
		searchOutput := readRouterPodData(oc, custContPod, "cat haproxy.config", "ingress-dca-opq")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_secure:" + oc.Namespace() + ":" + routeNames[0]))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-51980-destination-ca-certificate-secret annotation for destination CA TLS certifcate", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		testPodSvc := filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
		ingressTemp := filepath.Join(buildPruningBaseDir, "ingress-destCA.yaml")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp51980",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			ing = ingressDescription{
				name:        "ingress-dca-tls",
				namespace:   "",
				domain:      "",
				serviceName: "service-secure",
				template:    ingressTemp,
			}
		)

		compat_otp.By("create project and a pod")
		baseDomain := getBaseDomain(oc)
		project1 := oc.Namespace()
		createResourceFromFile(oc, project1, testPodSvc)
		ensurePodWithLabelReady(oc, project1, "name=web-server-deploy")
		podName := getPodListByLabel(oc, project1, "name=web-server-deploy")

		compat_otp.By("create custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, "ocp51980")

		compat_otp.By("create ingress and get the details")
		ing.domain = ingctrl.name + "." + baseDomain
		ing.namespace = project1
		ing.create(oc)
		getIngress(oc, project1)
		getRoutes(oc, project1)
		routeNames := getResourceName(oc, project1, "route")

		compat_otp.By("check whether route details are present in custom controller domain")
		output := getByJsonPath(oc, project1, "route/"+routeNames[0], "{.metadata.annotations}")
		o.Expect(output).Should(o.ContainSubstring(`"route.openshift.io/destination-ca-certificate-secret":"service-secret"`))
		output = getByJsonPath(oc, project1, "route/"+routeNames[0], "{.spec.host}")
		o.Expect(output).Should(o.ContainSubstring(`service-secure-%s.ocp51980.%s`, project1, baseDomain))

		compat_otp.By("check the router pod and ensure the routes are loaded in haproxy.config of custom controller")
		searchOutput := pollReadPodData(oc, "openshift-ingress", custContPod, "cat haproxy.config", "ingress-dca-tls")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_secure:" + project1 + ":" + routeNames[0]))

		compat_otp.By("check the reachability of the host in custom controller")
		controlerIP := getPodv4Address(oc, custContPod, "openshift-ingress")
		curlCmd := []string{"-n", project1, podName[0], "--", "curl", "https://service-secure-" + project1 +
			".ocp51980." + baseDomain + ":443", "-k", "-I", "--resolve", "service-secure-" + project1 + ".ocp51980." +
			baseDomain + ":443:" + controlerIP, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)
	})

	// bugzilla: 2025624
	g.It("Author:mjoseph-Longduration-NonPreRelease-High-49750-After certificate rotation, ingress router's metrics endpoint will auto update certificates [Disruptive]", func() {
		// Check whether the authentication operator is present or not
		output, err := oc.WithoutNamespace().AsAdmin().Run("get").Args("route", "oauth-openshift", "-n", "openshift-authentication").Output()
		if strings.Contains(output, "namespaces \"openshift-authentication\" not found") || err != nil {
			g.Skip("This cluster dont have authentication operator, so skipping the test.")
		}
		var (
			ingressLabel = "ingresscontroller.operator.openshift.io/deployment-ingresscontroller=default"
		)

		compat_otp.By("Check the metrics endpoint to get the intial certificate details")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		curlCmd := fmt.Sprintf("curl -k -v https://localhost:1936/metrics --connect-timeout 10")
		statsOut, err := compat_otp.RemoteShPod(oc, "openshift-ingress", routerpod, "sh", "-c", curlCmd)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(statsOut, "CAfile: /etc/pki/tls/certs/ca-bundle.crt")).Should(o.BeTrue())
		dateRe := regexp.MustCompile("(start date.*)")
		certStartDate := dateRe.FindAllString(string(statsOut), -1)

		compat_otp.By("Delete the default CA certificate in openshift-service-ca namespace")
		defer ensureAllClusterOperatorsNormal(oc, 920)
		err1 := oc.AsAdmin().WithoutNamespace().Run("delete").Args("secret", "signing-key", "-n", "openshift-service-ca").Execute()
		o.Expect(err1).NotTo(o.HaveOccurred())

		compat_otp.By("Waiting for some time till the cluster operators stabilize")
		ensureClusterOperatorNormal(oc, "authentication", 5, 720)

		compat_otp.By("Check the router logs to see the certificate in the metrics reloaded")
		ensureLogsContainString(oc, "openshift-ingress", ingressLabel, "reloaded metrics certificate")

		compat_otp.By("Check the metrics endpoint to get the certificate details after reload")
		curlCmd1 := fmt.Sprintf("curl -k -vvv https://localhost:1936/metrics --connect-timeout 10")
		statsOut1, err3 := compat_otp.RemoteShPod(oc, "openshift-ingress", routerpod, "sh", "-c", curlCmd1)
		o.Expect(err3).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(statsOut1, "CAfile: /etc/pki/tls/certs/ca-bundle.crt")).Should(o.BeTrue())
		certStartDate1 := dateRe.FindAllString(string(statsOut1), -1)
		// Cross check the start date of the ceritificate is not same after reloading
		o.Expect(certStartDate1[0]).NotTo(o.Equal(certStartDate[0]))
	})
})
