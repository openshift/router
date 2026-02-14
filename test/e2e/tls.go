package router

import (
	"github.com/openshift/router/test/e2e/testdata"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	e2e "k8s.io/kubernetes/test/e2e/framework"

	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("router-tls", exutil.KubeConfigPath())

	// incorporate OCP-12557, OCP-12563 into one
	// Test case creater: bmeng@redhat.com - OCP-12557: Only the certs file of the certain route will be updated when the route is updated
	// Test case creater: hongli@redhat.com - OCP-12563: The certs for the edge/reencrypt termination routes should be removed when the routes removed
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-12563-The certs for the edge/reencrypt termination routes should be removed when the routes removed", func() {
		// skip the test if featureSet is set there
		if exutil.IsTechPreviewNoUpgrade(oc) {
			g.Skip("Skip for the haproxy was't the realtime for the backend configuration after enabled DynamicConfigurationManager")
		}

		var (
			buildPruningBaseDir = testdata.FixturePath("router")
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

		exutil.By("1.0 Create a file folder and prepair for testing")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		baseDomain := getBaseDomain(oc)
		edgeRoute := "edge12563.apps." + baseDomain
		reenRoute := "reen12563.apps." + baseDomain

		exutil.By("2.0: Use openssl to create ca certification and key")
		opensslNewCa(caKey, caCrt, caSubj)

		exutil.By("3.0: Create a user CSR and the user key for the edge route")
		opensslNewCsr(edgeRouteKey, edgeRouteCsr, edgeRouteSubj)

		exutil.By("3.1: Sign the user CSR and generate the certificate for the edge route")
		san := "subjectAltName = DNS:" + edgeRoute
		opensslSignCsr(san, edgeRouteCsr, caCrt, caKey, edgeRouteCrt)

		exutil.By("4.0: Create a user CSR and the user key for the reen route")
		opensslNewCsr(reenRouteKey, reenRouteCsr, reenRouteSubj)

		exutil.By("4.1: Sign the user CSR and generate the certificate for the reen route")
		san = "subjectAltName = DNS:" + reenRoute
		opensslSignCsr(san, reenRouteCsr, caCrt, caKey, reenRouteCrt)

		exutil.By("5.0: Use openssl to create certification and key for the destination certification of the reen route")
		opensslNewCa(reenRouteDstKey, reenRouteDstCrt, reenRouteDstSubj)

		exutil.By("6.0 Create a deployment")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("7.0: Create the edge route and the reen route")
		createRoute(oc, ns, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute, "--ca-cert=" + caCrt, "--cert=" + edgeRouteCrt, "--key=" + edgeRouteKey})
		createRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{"--hostname=" + reenRoute, "--ca-cert=" + caCrt, "--cert=" + reenRouteCrt, "--key=" + reenRouteKey, "--dest-ca-cert=" + reenRouteDstCrt})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-edge", "default")
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		exutil.By("8.0: Check the certs for the edge/reencrypt termination routes")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		edgeCertIntialTime := checkRouteCertificationInRouterPod(oc, ns, "route-edge", routerpod, "certs", "--hasCert")
		reenCertIntialTime := checkRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "certs", "--hasCert")

		exutil.By("9.0: Check the cacert for the reencrypt termination route")
		reencaCertIntialTime := checkRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "cacerts", "--hasCert")

		// OCP-12557: Only the certs file of the certain route will be updated when that route is updated
		exutil.By("10.0: Show the cert files creation time")
		e2e.Logf("The intial edge certificate creation details is %s", edgeCertIntialTime)
		e2e.Logf("The intial reen certificate creation details is %s", reenCertIntialTime)
		e2e.Logf("The intial reen CA certificate creation details is %s", reencaCertIntialTime)

		exutil.By("11.0: Patch the reen route with path varibale")
		patchResourceAsAdmin(oc, ns, "route/route-reen", `{"spec": {"path": "/test"}}`)
		output, err := oc.Run("get").Args("route/route-reen", "-n", ns, "-o=jsonpath={.spec.path}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(`/test`))

		exutil.By("12.0: Recheck the creation time of the certs")
		// the cert details of reen route will be updated and the edge route will be same
		edgeUpdatedCertTime := checkRouteCertificationInRouterPod(oc, ns, "route-edge", routerpod, "certs", "--hasCert")
		reenUpdatedCertTime := checkRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "certs", "--hasCert")
		reencaCertUpdatedTime := checkRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "cacerts", "--hasCert")
		e2e.Logf("The Updated edge certificate creation details is %s", edgeUpdatedCertTime)
		e2e.Logf("The Updated reen certificate creation details is %s", reenUpdatedCertTime)
		e2e.Logf("The Updated reen CA certificate creation details is %s", reencaCertUpdatedTime)
		o.Expect(edgeCertIntialTime).To(o.ContainSubstring(edgeUpdatedCertTime))
		o.Expect(reenCertIntialTime).NotTo(o.ContainSubstring(reenUpdatedCertTime))
		o.Expect(reencaCertIntialTime).NotTo(o.ContainSubstring(reencaCertUpdatedTime))

		exutil.By("13.0: Delete the two routes")
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "route", "route-edge").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args("-n", ns, "route", "route-reen").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("14.0: Check the certs for the edge/reencrypt termination routes again after deleted the routes")
		checkRouteCertificationInRouterPod(oc, ns, "route-edge", routerpod, "certs", "--noCert")
		checkRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "certs", "--noCert")

		exutil.By("15.0: Check the cacert for the reencrypt termination route again after deleted the route")
		checkRouteCertificationInRouterPod(oc, ns, "route-reen", routerpod, "cacerts", "--noCert")
	})

	// author: iamin@redhat.com
	//Combine tls cases OCP-12573 and OCP-19799
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-12573-Default haproxy router should be able to skip invalid cert route", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
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

		exutil.By("1.0 Create a file folder and prepare for testing")
		defer os.RemoveAll(dirname)
		err := os.MkdirAll(dirname, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		baseDomain := getBaseDomain(oc)
		edgeRoute1 := "ocp12573-edge1.apps." + baseDomain
		reenRoute1 := "ocp12573-reen1.apps." + baseDomain
		edgeRoute2 := "ocp12573-edge2.apps." + baseDomain
		reenRoute2 := "ocp12573-reen2.apps." + baseDomain

		exutil.By("2.0: Use openssl to create ca certification and key")
		opensslNewCa(caKey1, caCrt1, caSubj)
		opensslNewCa(caKey2, caCrt2, caSubj)

		exutil.By("3.0: Create a user CSR and the user key for the edge route")
		opensslNewCsr(edgeRouteKey1, edgeRouteCsr1, edgeRouteSubj)
		opensslNewCsr(edgeRouteKey2, edgeRouteCsr2, edgeRouteSubj)

		exutil.By("3.1: Sign the user CSR and generate the certificate for the edge route")
		san1 := "subjectAltName = DNS:" + edgeRoute1
		san2 := "subjectAltName = DNS:" + edgeRoute2
		opensslSignCsr(san1, edgeRouteCsr1, caCrt1, caKey1, edgeRouteCrt1)
		opensslSignCsr(san2, edgeRouteCsr2, caCrt2, caKey2, edgeRouteCrt2)

		exutil.By("4.0: Create a user CSR and the user key for the reen route")
		opensslNewCsr(reenRouteKey1, reenRouteCsr1, reenRouteSubj)
		opensslNewCsr(reenRouteKey2, reenRouteCsr2, reenRouteSubj)

		exutil.By("4.1: Sign the user CSR and generate the certificate for the reen route")
		san1 = "subjectAltName = DNS:" + reenRoute1
		san2 = "subjectAltName = DNS:" + reenRoute2
		opensslSignCsr(san1, reenRouteCsr1, caCrt1, caKey1, reenRouteCrt1)
		opensslSignCsr(san2, reenRouteCsr2, caCrt2, caKey2, reenRouteCrt2)

		exutil.By("5.0: Use openssl to create certification and key for the destination certification of the reen route")
		opensslNewCa(reenRouteDstKey1, reenRouteDstCrt1, reenRouteDstSubj)
		opensslNewCa(reenRouteDstKey2, reenRouteDstCrt2, reenRouteDstSubj)

		exutil.By("6.0 Create a deployment")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("7.0: Create the edge route with invalid caCert and key")
		createRoute(oc, ns, "edge", "route-edge", unSecSvcName, []string{"--hostname=" + edgeRoute1, "--ca-cert=" + caCrt1, "--cert=" + edgeRouteCrt2, "--key=" + edgeRouteKey2})
		createRoute(oc, ns, "edge", "route-edge2", unSecSvcName, []string{"--hostname=" + edgeRoute2, "--ca-cert=" + caCrt2, "--cert=" + edgeRouteCrt2, "--key=" + edgeRouteKey1})

		exutil.By("8.0: Create the reencrypt route with invalid cert and destCA")
		createRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{"--hostname=" + reenRoute1, "--ca-cert=" + caCrt1, "--cert=" + reenRouteCrt2, "--key=" + reenRouteKey1, "--dest-ca-cert=" + reenRouteDstCrt1})
		createRoute(oc, ns, "reencrypt", "route-reen2", secSvcName, []string{"--hostname=" + reenRoute2, "--ca-cert=" + caCrt1, "--cert=" + reenRouteCrt2, "--key=" + reenRouteKey1, "--dest-ca-cert=" + reenRouteDstCrt1})

		exutil.By("9.0: Check the routes Host section")
		routeOutput := getRoutes(oc, ns)
		o.Expect(strings.Count(routeOutput, `ExtendedValidationFailed`) == 4).To(o.BeTrue())

		exutil.By("10.0: Edit the tls spec section of the edge route")
		patchResourceAsAdmin(oc, ns, "route/route-edge", `{"spec":{"tls" :{"key": "qe","certificate": "ocp","caCertificate": "redhat"}}}`)

		exutil.By("11.0: Check the tls spec to see updated info")
		patch, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("route", "route-edge", "-n", ns, "-ojsonpath={.spec.tls}").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(patch).To(o.ContainSubstring(`"caCertificate":"redhat","certificate":"ocp","key":"qe"`))
	})

	// author: iamin@redhat.com
	g.It("Author:iamin-ROSA-OSD_CCS-ARO-High-14089-Route cannot be accessed if the backend cannot be matched by the default destination CA of router", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			secSvcName          = "service-secure"
		)

		exutil.By("1.0 Create a deployment")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")

		exutil.By("2.0 Create the reencrypt route with the backend not matched the the default destination CA of router")
		createRoute(oc, ns, "reencrypt", "route-reen", secSvcName, []string{})
		ensureRouteIsAdmittedByIngressController(oc, ns, "route-reen", "default")

		exutil.By("3.0: Curl the route while it uses the defaulted destCA")
		reenHost := "route-reen-" + ns + ".apps." + getBaseDomain(oc)
		waitForOutsideCurlContains("https://"+reenHost, "-I -k", `HTTP/1.0 503`)

		exutil.By("4.0: Check the route help section")
		output, err := oc.Run("create").Args("route", "reencrypt", "-h").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.And(o.ContainSubstring("--dest-ca-cert"), o.ContainSubstring("Defaults to the Service CA")))
	})

	// also includes OCP-25665/25666/25668/25703
	// author: hongli@redhat.com
	g.It("Author:hongli-WRS-ROSA-OSD_CCS-ARO-Critical-25702-V-BR.12-the tlsSecurityProfile in ingresscontroller can be updated", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp25702",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("create custom IC without tls profile config (Intermediate is default)")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureCustomIngressControllerAvailable(oc, ingctrl.name)

		// OCP-25703
		exutil.By("check default TLS config and it should be same to Intermediate profile")
		newrouterpod := getOneRouterPodNameByIC(oc, ingctrl.name)
		env := readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384`))

		// OCP-25665
		exutil.By("patch custom IC with tls profile Old and check the config")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{"spec":{"tlsSecurityProfile":{"type":"Old"}}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.1`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA:ECDHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA256:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:DES-CBC3-SHA`))

		// OCP-25666
		exutil.By("patch custom IC with tls profile Intermidiate and check the config")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{"spec":{"tlsSecurityProfile":{"type":"Intermediate"}}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "3")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384`))

		// OCP-25668
		exutil.By("patch custom IC with tls profile Custom and check the config")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/"+ingctrl.name, `{"spec":{"tlsSecurityProfile":{"type":"Custom","custom":{"ciphers":["DHE-RSA-AES256-GCM-SHA384","ECDHE-ECDSA-AES256-GCM-SHA384"],"minTLSVersion":"VersionTLS12"}}}}`)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "4")
		newrouterpod = getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env = readRouterPodEnv(oc, newrouterpod, "SSL_MIN_VERSION")
		o.Expect(env).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.2`))
		env = readRouterPodEnv(oc, newrouterpod, "ROUTER_CIPHER")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_CIPHERS=DHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-AES256-GCM-SHA384`))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-WRS-Critical-43284-V-CM.01-setting tlssecurityprofile to TLSv1.3", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp43284",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("create and patch the ingresscontroller to enable tls security profile to modern type TLSv1.3")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp43284", "{\"spec\":{\"tlsSecurityProfile\":{\"type\":\"Modern\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		exutil.By("check the env variable of the router pod")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, "ocp43284")
		tlsProfile := readRouterPodEnv(oc, newrouterpod, "TLS")
		o.Expect(tlsProfile).To(o.ContainSubstring(`SSL_MIN_VERSION=TLSv1.3`))
		o.Expect(tlsProfile).To(o.ContainSubstring(`ROUTER_CIPHERSUITES=TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))

		exutil.By("check the haproxy config on the router pod to ensure the ssl version TLSv1.3 is reflected")
		tlsVersion := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "ssl-min-ver")
		o.Expect(tlsVersion).To(o.ContainSubstring(`ssl-default-bind-options ssl-min-ver TLSv1.3`))

		exutil.By("check the haproxy config on the router pod to ensure the tls1.3 ciphers are enabled")
		tlsCliper := readRouterPodData(oc, newrouterpod, "cat haproxy.config", "sl-default-bind-ciphersuites")
		o.Expect(tlsCliper).To(o.ContainSubstring(`ssl-default-bind-ciphersuites TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256`))
	})

	// author: hongli@redhat.com
	g.It("[Level0] Author:hongli-WRS-Critical-43300-V-ACS.05-enable client certificate with optional policy", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
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

		exutil.By("create configmap client-ca-xxxxx in namespace openshift-config")
		defer deleteConfigMap(oc, "openshift-config", "client-ca-43300")
		createConfigMapFromFile(oc, "openshift-config", "client-ca-43300", cmFile)

		exutil.By("create and patch custom IC to enable client certificate with Optional policy")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp43300", "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-43300\"},\"clientCertificatePolicy\":\"Optional\"}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		exutil.By("check client certification config after custom router rolled out")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env := readRouterPodEnv(oc, newrouterpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH=optional`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH_CA=/etc/pki/tls/client-ca/ca-bundle.pem`))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-WRS-Medium-43301-V-ACS.05-enable client certificate with required policy", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
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

		exutil.By("create configmap client-ca-xxxxx in namespace openshift-config")
		defer deleteConfigMap(oc, "openshift-config", "client-ca-43301")
		createConfigMapFromFile(oc, "openshift-config", "client-ca-43301", cmFile)

		exutil.By("create and patch custom IC to enable client certificate with required policy")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		patchResourceAsAdmin(oc, ingctrl.namespace, "ingresscontroller/ocp43301", "{\"spec\":{\"clientTLS\":{\"clientCA\":{\"name\":\"client-ca-43301\"},\"clientCertificatePolicy\":\"Required\",\"allowedSubjectPatterns\":[\"www.test2.com\"]}}}")
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "2")

		exutil.By("check client certification config after custom router rolled out")
		newrouterpod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		env := readRouterPodEnv(oc, newrouterpod, "ROUTER_MUTUAL_TLS_AUTH")
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH=required`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH_CA=/etc/pki/tls/client-ca/ca-bundle.pem`))
		o.Expect(env).To(o.ContainSubstring(`ROUTER_MUTUAL_TLS_AUTH_FILTER=(?:www.test2.com)`))
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

		exutil.By("Check the metrics endpoint to get the intial certificate details")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		curlCmd := fmt.Sprintf("curl -k -v https://localhost:1936/metrics --connect-timeout 10")
		statsOut, err := exutil.RemoteShPod(oc, "openshift-ingress", routerpod, "sh", "-c", curlCmd)
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(statsOut, "CAfile: /etc/pki/tls/certs/ca-bundle.crt")).Should(o.BeTrue())
		dateRe := regexp.MustCompile("(start date.*)")
		certStartDate := dateRe.FindAllString(string(statsOut), -1)

		exutil.By("Delete the default CA certificate in openshift-service-ca namespace")
		defer ensureAllClusterOperatorsNormal(oc, 920)
		err1 := oc.AsAdmin().WithoutNamespace().Run("delete").Args("secret", "signing-key", "-n", "openshift-service-ca").Execute()
		o.Expect(err1).NotTo(o.HaveOccurred())

		exutil.By("Waiting for some time till the cluster operators stabilize")
		ensureClusterOperatorNormal(oc, "authentication", 5, 720)

		exutil.By("Check the router logs to see the certificate in the metrics reloaded")
		ensureLogsContainString(oc, "openshift-ingress", ingressLabel, "reloaded metrics certificate")

		exutil.By("Check the metrics endpoint to get the certificate details after reload")
		curlCmd1 := fmt.Sprintf("curl -k -vvv https://localhost:1936/metrics --connect-timeout 10")
		statsOut1, err3 := exutil.RemoteShPod(oc, "openshift-ingress", routerpod, "sh", "-c", curlCmd1)
		o.Expect(err3).NotTo(o.HaveOccurred())
		o.Expect(strings.Contains(statsOut1, "CAfile: /etc/pki/tls/certs/ca-bundle.crt")).Should(o.BeTrue())
		certStartDate1 := dateRe.FindAllString(string(statsOut1), -1)
		// Cross check the start date of the ceritificate is not same after reloading
		o.Expect(certStartDate1[0]).NotTo(o.Equal(certStartDate[0]))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-50842-destination-ca-certificate-secret annotation for destination CA Opaque certifcate", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
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

		exutil.By("Create a pod")
		ns := oc.Namespace()
		baseDomain := getBaseDomain(oc)
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")

		exutil.By("create custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, "ocp50842")

		exutil.By("create a secret with destination CA Opaque certificate")
		createGenericSecret(oc, ns, "service-secret", "tls.crt", caCert)

		exutil.By("create ingress and get the details")
		ing.domain = ingctrl.name + "." + baseDomain
		ing.namespace = ns
		ing.create(oc)
		getIngress(oc, ns)
		getRoutes(oc, ns)
		routeNames := getResourceName(oc, ns, "route")

		exutil.By("check whether route details are present in custom controller domain")
		waitForOutputContains(oc, ns, "route/"+routeNames[0], "{.metadata.annotations}", `"route.openshift.io/destination-ca-certificate-secret":"service-secret"`)
		host := fmt.Sprintf(`service-secure-%s.ocp50842.%s`, ns, baseDomain)
		waitForOutputEquals(oc, ns, "route/"+routeNames[0], "{.spec.host}", host)

		exutil.By("check the reachability of the host in custom controller")
		controlerIP := getPodv4Address(oc, custContPod, "openshift-ingress")
		curlCmd := []string{"-n", ns, podName[0], "--", "curl", "https://service-secure-" + ns +
			".ocp50842." + baseDomain + ":443", "-k", "-I", "--resolve", "service-secure-" + ns + ".ocp50842." +
			baseDomain + ":443:" + controlerIP, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)

		exutil.By("check the router pod and ensure the routes are loaded in haproxy.config of custom controller")
		searchOutput := readRouterPodData(oc, custContPod, "cat haproxy.config", "ingress-dca-opq")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_secure:" + ns + ":" + routeNames[0]))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-51980-destination-ca-certificate-secret annotation for destination CA TLS certifcate", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
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

		exutil.By("Create a pod")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")

		exutil.By("create custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, "ocp51980")

		exutil.By("create ingress and get the details")
		ing.domain = ingctrl.name + "." + baseDomain
		ing.namespace = ns
		ing.create(oc)
		getIngress(oc, ns)
		getRoutes(oc, ns)
		routeNames := getResourceName(oc, ns, "route")

		exutil.By("check whether route details are present in custom controller domain")
		output := getByJsonPath(oc, ns, "route/"+routeNames[0], "{.metadata.annotations}")
		o.Expect(output).Should(o.ContainSubstring(`"route.openshift.io/destination-ca-certificate-secret":"service-secret"`))
		output = getByJsonPath(oc, ns, "route/"+routeNames[0], "{.spec.host}")
		o.Expect(output).Should(o.ContainSubstring(`service-secure-%s.ocp51980.%s`, ns, baseDomain))

		exutil.By("check the router pod and ensure the routes are loaded in haproxy.config of custom controller")
		searchOutput := pollReadPodData(oc, "openshift-ingress", custContPod, "cat haproxy.config", "ingress-dca-tls")
		o.Expect(searchOutput).To(o.ContainSubstring("backend be_secure:" + ns + ":" + routeNames[0]))

		exutil.By("check the reachability of the host in custom controller")
		controlerIP := getPodv4Address(oc, custContPod, "openshift-ingress")
		curlCmd := []string{"-n", ns, podName[0], "--", "curl", "https://service-secure-" + ns +
			".ocp51980." + baseDomain + ":443", "-k", "-I", "--resolve", "service-secure-" + ns + ".ocp51980." +
			baseDomain + ":443:" + controlerIP, "--connect-timeout", "10"}
		repeatCmdOnClient(oc, curlCmd, "200", 30, 1)
	})
})
