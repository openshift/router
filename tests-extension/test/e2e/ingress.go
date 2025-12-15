package router

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("router-ingressclass", compat_otp.KubeConfigPath())

	// incorporate OCP-33960, OCP-33961, OCP-33962 and OCP-33986 into one
	// Test case creater: aiyengar@redhat.com - OCP-33960 Setting "route.openshift.io/termination" annotation to "Edge" in ingress resource deploys "Edge" terminated route object
	// Test case creater: aiyengar@redhat.com - OCP-33961 Setting "route.openshift.io/termination" annotation to "Passthrough" in ingress resource deploys "passthrough" terminated route object
	// Test case creater: aiyengar@redhat.com - OCP-33962 Setting "route.openshift.io/termination" annotation to "Reencrypt" in ingress resource deploys "reen" terminated route object
	// Test case creater: aiyengar@redhat.com - OCP-33986 Setting values other than "edge/passthrough/reencrypt" for "route.openshift.io/termination" annotation are ignored by ingress object
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-33960-Setting 'route.openshift.io/termination' annotation to Edge/Passthrough/Reencrypt in ingress resource", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			testIngress         = filepath.Join(buildPruningBaseDir, "ingress-resource.yaml")
			tmpdir              = "/tmp/OCP-33960-CA/"
			caKey               = tmpdir + "ca.key"
			caCrt               = tmpdir + "ca.crt"
			serverKey           = tmpdir + "server.key"
			serverCsr           = tmpdir + "server.csr"
			serverCrt           = tmpdir + "server.crt"
		)

		compat_otp.By("1. Create a server and client pod")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		routeEdgeHost := "ingress-edge-" + ns + ".apps." + baseDomain
		routePassHost := "ingress-passth-" + ns + ".apps." + baseDomain
		routeReenHost := "ingress-reen-" + ns + ".apps." + baseDomain
		routeRandHost := "33960-random-" + ns + ".example.com"
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("2. Create a secret certificate for ingress edge and reen termination")
		// prepare the tmp folder and create self-signed cerfitcate and a secret
		defer os.RemoveAll(tmpdir)
		err := os.MkdirAll(tmpdir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		opensslNewCa(caKey, caCrt, "/CN=ne-root-ca")
		opensslNewCsr(serverKey, serverCsr, "/CN=ne-server-cert")
		// san just contains edge route host but not reen route host
		san := "subjectAltName=DNS:" + routeEdgeHost
		opensslSignCsr(san, serverCsr, caCrt, caKey, serverCrt)
		err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "secret", "tls", "ingress-secret", "--cert="+serverCrt, "--key="+serverKey).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("3. Create edge, passthroguh and reen routes")
		sedCmd := fmt.Sprintf(`sed -i'' -e 's@edgehostname@%s@g;s@passhostname@%s@g;s@reenhostname@%s@g;s@randomhostname@%s@g' %s`, routeEdgeHost, routePassHost, routeReenHost, routeRandHost, testIngress)
		_, err = exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, ns, testIngress)
		routeOutput := getRoutes(oc, ns)

		// OCP-33960: Setting "route.openshift.io/termination" annotation to "Edge" in ingress resource deploys "Edge" terminated route object
		compat_otp.By("4. Verify the haproxy configuration to ensure the ingress edge route is configured")
		edgeBackendName := "be_edge_http:" + ns + ":ingress-edge"
		ensureHaproxyBlockConfigContains(oc, podName, edgeBackendName, []string{":service-unsecure:http:"})

		compat_otp.By("5. Check the reachability of the edge route")
		waitForOutsideCurlContains("https://"+routeEdgeHost, "-kI", `200`)

		// OCP-33961: Setting "route.openshift.io/termination" annotation to "Passthrough" in ingress resource deploys "passthrough" terminated route object
		compat_otp.By("6. Verify the haproxy configuration to ensure the ingress passthrough route is configured")
		passthroughBackendName := "be_tcp:" + ns + ":ingress-passth"
		ensureHaproxyBlockConfigContains(oc, podName, passthroughBackendName, []string{":service-secure:https:"})

		compat_otp.By("7. Check the reachability of the passthrough route")
		waitForOutsideCurlContains("https://"+routePassHost, "-kI", `200`)

		// OCP-33962: Setting "route.openshift.io/termination" annotation to "Reencrypt" in ingress resource deploys "reen" terminated route object
		compat_otp.By("8. Verify the haproxy configuration to ensure the ingress reen route is configured")
		reenBackendName := "be_secure:" + ns + ":ingress-reencrypt"
		reenBackendOutput := ensureHaproxyBlockConfigContains(oc, podName, reenBackendName, []string{":service-secure:https:"})
		o.Expect(reenBackendOutput).To(o.ContainSubstring(`/var/run/configmaps/service-ca/service-ca.crt`))

		compat_otp.By("9. Check the reachability of the reen route")
		waitForOutsideCurlContains("https://"+routeReenHost, "-kI", `200`)

		// OCP-33986: Setting values other than "edge/passthrough/reencrypt" for "route.openshift.io/termination" annotation are ignored by ingress object
		compat_otp.By("10. Verify the random route type's annotation are ignored")
		o.Expect(routeOutput).NotTo(o.ContainSubstring("abcd"))
	})

	// bug: 1820075
	// author: hongli@redhat.com
	g.It("Author:hongli-Critical-41109-use IngressClass controller for ingress-to-route", func() {
		var (
			output              string
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			testIngress         = filepath.Join(buildPruningBaseDir, "ingress-with-class.yaml")
		)

		compat_otp.By("Create pod, svc, and ingress that mismatch with default ingressclass")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		createResourceFromFile(oc, ns, testIngress)

		compat_otp.By("ensure no route is created from the ingress")
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).NotTo(o.ContainSubstring("ingress-with-class"))

		compat_otp.By("patch the ingress to use default ingressclass")
		patchResourceAsUser(oc, ns, "ingress/ingress-with-class", "{\"spec\":{\"ingressClassName\": \"openshift-default\"}}")
		compat_otp.By("ensure one route is created from the ingress")
		waitForOutputContains(oc, ns, "route", "{.items[*].metadata.name}", "ingress-with-class")

		// bug:- 1820075
		compat_otp.By("Confirm the address field is getting populated with the Router domain details")
		baseDomain := getBaseDomain(oc)
		ingressOut := getIngress(oc, ns)
		o.Expect(ingressOut).To(o.ContainSubstring("router-default.apps." + baseDomain))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-Critical-41117-ingress operator manages the IngressClass for each ingresscontroller", func() {
		buildPruningBaseDir := compat_otp.FixturePath("testdata", "router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp41117",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		compat_otp.By("check the ingress class created by default ingresscontroller")
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingressclass/openshift-default").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("openshift.io/ingress-to-route"))

		compat_otp.By("create another custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		compat_otp.By("check the ingressclass is created by custom ingresscontroller")
		ingressclassname := "openshift-" + ingctrl.name
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingressclass", ingressclassname).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("openshift.io/ingress-to-route"))

		compat_otp.By("delete the custom ingresscontroller and ensure the ingresscalsss is removed")
		ingctrl.delete(oc)
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingressclass", ingressclassname).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("NotFound"))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-51148-host name of the route depends on the subdomain if provided", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp          = filepath.Join(buildPruningBaseDir, "subdomain-routes/ocp51148-route.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			rut                 = routeDescription{
				namespace: "",
				domain:    "",
				subDomain: "foo",
				template:  customTemp,
			}
		)

		compat_otp.By("Create a pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")
		baseDomain := getBaseDomain(oc)
		rut.domain = "apps" + "." + baseDomain
		rut.namespace = ns

		compat_otp.By("create routes and get the details")
		rut.create(oc)
		// to show the route details
		getRoutes(oc, ns)

		compat_otp.By("check the domain name is present in 'foo-unsecure1' route details")
		output := getByJsonPath(oc, ns, "route/foo-unsecure1", "{.spec}")
		o.Expect(output).Should(o.ContainSubstring(`"subdomain":"foo"`))

		compat_otp.By("check the domain name is not present in 'foo-unsecure2' route details")
		output = getByJsonPath(oc, ns, "route/foo-unsecure2", "{.spec}")
		o.Expect(output).NotTo(o.ContainSubstring("subdomain"))

		compat_otp.By("check the domain name is present in 'foo-unsecure3' route details")
		output = getByJsonPath(oc, ns, "route/foo-unsecure3", "{.spec}")
		o.Expect(output).Should(o.ContainSubstring(`"subdomain":"foo"`))

		compat_otp.By("check the domain name is not present in 'foo-unsecure4' route details")
		output = getByJsonPath(oc, ns, "route/foo-unsecure4", "{.spec}")
		o.Expect(output).NotTo(o.ContainSubstring("subdomain"))

		// curling through default controller will not work for proxy cluster.
		if checkProxy(oc) {
			e2e.Logf("This is proxy cluster, skiping the curling part.")
		} else {
			compat_otp.By("check the reachability of the 'foo-unsecure1' host")
			waitForCurl(oc, podName[0], baseDomain, "foo.apps.", "Hello-OpenShift", "")

			compat_otp.By("check the reachability of the 'foo-unsecure2' host")
			waitForCurl(oc, podName[0], baseDomain, "foo-unsecure2-"+ns+".apps.", "Hello-OpenShift", "")

			compat_otp.By("check the reachability of the 'foo-unsecure3' host")
			waitForCurl(oc, podName[0], baseDomain, "man-"+ns+".apps.", "Hello-OpenShift", "")

			compat_otp.By("check the reachability of the 'foo-unsecure4' host")
			waitForCurl(oc, podName[0], baseDomain, "bar-"+ns+".apps.", "Hello-OpenShift", "")
		}
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-High-51429-different router deployment with same route using subdomain", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp2         = filepath.Join(buildPruningBaseDir, "subdomain-routes/route.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
			ingctrl             = ingressControllerDescription{
				name:      "ocp51429",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
			rut = routeDescription{
				namespace: "",
				domain:    "",
				subDomain: "foobar",
				template:  customTemp2,
			}
		)

		compat_otp.By("Create a pod")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")
		rut.domain = "apps" + "." + baseDomain
		rut.namespace = ns

		compat_otp.By("Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		defaultContPod := getOneRouterPodNameByIC(oc, "default")

		compat_otp.By("create routes and get the details")
		rut.create(oc)
		getRoutes(oc, ns)

		compat_otp.By("check whether required host is present in 'foobar-unsecure' route details")
		waitForOutputContains(oc, ns, "route/foobar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"foobar.apps.%s"`, baseDomain))
		waitForOutputContains(oc, ns, "route/foobar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"foobar.ocp51429.%s"`, baseDomain))

		compat_otp.By("check the router pod and ensure the routes are loaded in haproxy.config in default controller")
		searchOutput1 := pollReadPodData(oc, "openshift-ingress", defaultContPod, "cat haproxy.config", "foobar-unsecure")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_http:" + ns + ":foobar-unsecure"))

		compat_otp.By("check the router pod and ensure the routes are loaded in haproxy.config of custom controller")
		searchOutput2 := pollReadPodData(oc, "openshift-ingress", custContPod, "cat haproxy.config", "foobar-unsecure")
		o.Expect(searchOutput2).To(o.ContainSubstring("backend be_http:" + ns + ":foobar-unsecure"))

		// curling through default controller will not work for proxy cluster.
		if checkProxy(oc) {
			e2e.Logf("This is proxy cluster, skiping the curling part through default controller.")
		} else {
			compat_otp.By("check the reachability of the 'foobar-unsecure' host in default controller")
			waitForCurl(oc, podName[0], baseDomain, "foobar.apps.", "Hello-OpenShift", "")
		}

		compat_otp.By("check the reachability of the 'foobar-unsecure' host in custom controller")
		custContIP := getPodv4Address(oc, custContPod, "openshift-ingress")
		waitForCurl(oc, podName[0], baseDomain, "foobar.ocp51429.", "Hello-OpenShift", custContIP)
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-NonHyperShiftHOST-ROSA-OSD_CCS-ARO-High-51437-Router deployment using different shard with same subdomain ", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			customTemp2         = filepath.Join(buildPruningBaseDir, "subdomain-routes/alpha-shard-route.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			customTemp          = filepath.Join(buildPruningBaseDir, "ingresscontroller-shard.yaml")
			ingctrl1            = ingressControllerDescription{
				name:      "alpha-ocp51437",
				namespace: "openshift-ingress-operator",
				domain:    "",
				shard:     "alpha",
				template:  customTemp,
			}
			ingctrl2 = ingressControllerDescription{
				name:      "beta-ocp51437",
				namespace: "openshift-ingress-operator",
				domain:    "",
				shard:     "beta",
				template:  customTemp,
			}
			rut = routeDescription{
				namespace: "",
				domain:    "",
				subDomain: "bar",
				template:  customTemp2,
			}
		)

		compat_otp.By("Create a pod")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")
		rut.domain = "apps" + "." + baseDomain
		rut.namespace = ns

		compat_otp.By("Create first shard ingresscontroller")
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "1")
		custContPod1 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)

		compat_otp.By("Create second shard ingresscontroller")
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")
		custContPod2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)

		compat_otp.By("create routes and get the details")
		rut.create(oc)
		getRoutes(oc, ns)

		compat_otp.By("check whether required host is present in alpha ingress controller domain")
		waitForOutputContains(oc, ns, "route/bar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"bar.apps.%s"`, baseDomain))
		waitForOutputContains(oc, ns, "route/bar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"bar.alpha-ocp51437.%s"`, baseDomain))

		compat_otp.By("check the router pod and ensure the routes are loaded in haproxy.config of alpha controller")
		searchOutput1 := pollReadPodData(oc, "openshift-ingress", custContPod1, "cat haproxy.config", "bar-unsecure")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_http:" + ns + ":bar-unsecure"))

		// curling through default controller will not work for proxy cluster.
		if checkProxy(oc) {
			e2e.Logf("This is proxy cluster, skiping the curling part through default controller.")
		} else {
			compat_otp.By("check the reachability of the 'bar-unsecure' host in default controller")
			waitForCurl(oc, podName[0], baseDomain, "bar.apps.", "Hello-OpenShift", "")
		}

		compat_otp.By("check the reachability of the 'bar-unsecure' host in 'alpha shard' controller")
		custContIP := getPodv4Address(oc, custContPod1, "openshift-ingress")
		waitForCurl(oc, podName[0], baseDomain, "bar.alpha-ocp51437.", "Hello-OpenShift", custContIP)

		compat_otp.By("Overwrite route with beta shard")
		_, err := oc.AsAdmin().WithoutNamespace().Run("label").Args("routes/bar-unsecure", "--overwrite", "shard=beta", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("check whether required host is present in beta ingress controller domain")
		waitForOutputContains(oc, ns, "route/bar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"bar.apps.%s"`, baseDomain))
		waitForOutputContains(oc, ns, "route/bar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"bar.beta-ocp51437.%s"`, baseDomain))

		compat_otp.By("check the router pod and ensure the routes are loaded in haproxy.config of beta controller")
		searchOutput2 := pollReadPodData(oc, "openshift-ingress", custContPod2, "cat haproxy.config", "bar-unsecure")
		o.Expect(searchOutput2).To(o.ContainSubstring("backend be_http:" + ns + ":bar-unsecure"))

		compat_otp.By("check the reachability of the 'bar-unsecure' host in 'beta shard' controller")
		custContIP2 := getPodv4Address(oc, custContPod2, "openshift-ingress")
		waitForCurl(oc, podName[0], baseDomain, "bar.beta-ocp51437.", "Hello-OpenShift", custContIP2)
	})

	// bug: 1914127
	g.It("Author:shudili-NonPreRelease-Longduration-High-56228-Deletion of default router service under the openshift ingress namespace hangs flag [Disruptive]", func() {
		var (
			svcResource = "service/router-default"
			namespace   = "openshift-ingress"
		)

		compat_otp.By("check if the cluster has the router-default service")
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(output, "router-default") {
			g.Skip("This cluster has NOT the router-defaut service, skip the test.")
		}

		compat_otp.By("check if all COs are in good status")
		badOpList := checkAllClusterOperatorsStatus(oc)
		if len(badOpList) > 0 {
			g.Skip("Some cluster operators are NOT in good status, skip the test.")
		}

		compat_otp.By("check the created time of svc router-default")
		jsonPath := "{.metadata.creationTimestamp}"
		svcCreatedTime1 := getByJsonPath(oc, namespace, svcResource, jsonPath)
		o.Expect(svcCreatedTime1).NotTo(o.BeEmpty())

		compat_otp.By("try to delete the svc router-default, should no errors")
		defer ensureAllClusterOperatorsNormal(oc, 720)
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args(svcResource, "-n", namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("wait for new svc router-default is created")
		jsonPath = "{.metadata.name}"
		waitForOutputEquals(oc, namespace, svcResource, jsonPath, "router-default")

		compat_otp.By("check the created time of the new svc router-default")
		jsonPath = "{.metadata.creationTimestamp}"
		svcCreatedTime2 := getByJsonPath(oc, namespace, svcResource, jsonPath)
		o.Expect(svcCreatedTime2).NotTo(o.BeEmpty())
		o.Expect(svcCreatedTime1).NotTo(o.Equal(svcCreatedTime2))
	})

	// bug: 2013004
	g.It("ARO-Author:shudili-High-57089-Error syncing load balancer and failed to parse the VMAS ID on Azure platform", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			lbServices          = filepath.Join(buildPruningBaseDir, "bug2013004-lb-services.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			externalSvc         = "external-lb-57089"
			internalSvc         = "internal-lb-57089"
		)

		// skip if platform is not AZURE
		compat_otp.By("Pre-flight check for the platform type")
		platformtype := compat_otp.CheckPlatform(oc)
		if platformtype != "azure" {
			g.Skip("Skip for it not azure platform")
		}

		compat_otp.By("create a server pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		compat_otp.By("try to create an external load balancer service and an internal load balancer service")
		operateResourceFromFile(oc, "create", ns, lbServices)
		waitForOutputEquals(oc, ns, "service/"+externalSvc, "{.metadata.name}", externalSvc)
		waitForOutputEquals(oc, ns, "service/"+internalSvc, "{.metadata.name}", internalSvc)

		compat_otp.By("check if the lb services have obtained the EXTERNAL-IPs")
		regExp := "([0-9]+.[0-9]+.[0-9]+.[0-9]+)"
		searchOutput1 := waitForOutputMatchRegexp(oc, ns, "service/"+externalSvc, "{.status.loadBalancer.ingress..ip}", regExp)
		o.Expect(searchOutput1).NotTo(o.ContainSubstring("NotMatch"))
		searchOutput2 := waitForOutputMatchRegexp(oc, ns, "service/"+internalSvc, "{.status.loadBalancer.ingress..ip}", regExp)
		o.Expect(searchOutput2).NotTo(o.ContainSubstring("NotMatch"))
	})

	// incorporate OCP-57370and OCP-14059 into one
	// Test case creater: mjoseph@redhat.com - OCP-57370: Hostname of componentRoutes should be RFC compliant
	// bugzilla: 2039256
	// Due to bug https://issues.redhat.com/browse/OCPBUGS-43431, this case may not run on HCP cluster.
	// Test case creater: zzhao@redhat.com - OCP-14059: Use the default destination CA of router if the route does not specify one for reencrypt route
	g.It("Author:mjoseph-NonHyperShiftHOST-High-57370-hostname of componentRoutes should be RFC compliant", func() {
		// Check whether the console operator is present or not
		output, err := oc.WithoutNamespace().AsAdmin().Run("get").Args("route", "console", "-n", "openshift-console").Output()
		if strings.Contains(output, "namespaces \"openshift-console\" not found") || err != nil {
			g.Skip("This cluster dont have console operator, so skipping the test.")
		}
		var (
			resourceName = "ingress.config/cluster"
		)

		compat_otp.By("1. Create route and get the details")
		removeRoute := fmt.Sprintf("[{\"op\":\"remove\", \"path\":\"/spec/componentRoutes\", \"value\":[{\"hostname\": \"1digit9.apps.%s\", \"name\": \"downloads\", \"namespace\": \"openshift-console\"}]}]}]", getBaseDomain(oc))
		addRoute := fmt.Sprintf("[{\"op\":\"add\", \"path\":\"/spec/componentRoutes\", \"value\":[{\"hostname\": \"1digit9.apps.%s\", \"name\": \"downloads\", \"namespace\": \"openshift-console\"}]}]}]", getBaseDomain(oc))
		defer patchGlobalResourceAsAdmin(oc, resourceName, removeRoute)
		patchGlobalResourceAsAdmin(oc, resourceName, addRoute)
		waitForOutputContains(oc, "openshift-console", "route", "{.items..metadata.name}", "downloads-custom")

		compat_otp.By("2. Check the router pod and ensure the routes are loaded in haproxy.config")
		podname := getOneRouterPodNameByIC(oc, "default")
		backendConfig := pollReadPodData(oc, "openshift-ingress", podname, "cat haproxy.config", "downloads-custom")
		o.Expect(backendConfig).To(o.ContainSubstring("backend be_edge_http:openshift-console:downloads-custom"))

		compat_otp.By("3. Confirm from the component Route, the RFC complaint hostname")
		cmd := fmt.Sprintf(`1digit9.apps.%s`, getBaseDomain(oc))
		waitForOutputContains(oc, oc.Namespace(), "ingress.config.openshift.io/cluster", "{.spec.componentRoutes[0].hostname}", cmd)

		// OCP-14059: Use the default destination CA of router if the route does not specify one for reencrypt route
		// since console route is using the reencrypt route without destination CA we are using it to check
		compat_otp.By("4. Confirm from the console service the 'serving-cert-secret-name'")
		findAnnotation := getAnnotation(oc, "openshift-console", "svc", "console")
		o.Expect(findAnnotation).To(o.Or(o.ContainSubstring(`service.alpha.openshift.io/serving-cert-secret-name":"console-serving-cert`), o.ContainSubstring(`service.beta.openshift.io/serving-cert-secret-name":"console-serving-cert`)))

		compat_otp.By("5. Confirm from the 'service-ca.crt' is present in haproxy.config for console route")
		backendConfig1 := pollReadPodData(oc, "openshift-ingress", podname, "cat haproxy.config", "console.openshift-console.svc")
		o.Expect(backendConfig1).To(o.ContainSubstring("required ca-file /var/run/configmaps/service-ca/service-ca.crt"))
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-73619-Checking whether the ingress operator is enabled as optional component", func() {
		var (
			enabledCapabilities = "{.status.capabilities.enabledCapabilities}"
			capability          = "{.metadata.annotations}"
			ingressCapability   = `"capability.openshift.io/name":"Ingress"`
		)

		compat_otp.By("Check whether 'enabledCapabilities' is enabled in cluster version resource")
		searchLine, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "version", "-o=jsonpath="+enabledCapabilities).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(searchLine).To(o.ContainSubstring("Ingress"))

		compat_otp.By("Check the Ingress capability in dnsrecords crd")
		searchLine, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("crd", "dnsrecords.ingress.operator.openshift.io", "-o=jsonpath="+capability).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(searchLine).To(o.ContainSubstring(ingressCapability))

		compat_otp.By("Check the Ingress capability in ingresscontrollers crd")
		searchLine, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("crd", "ingresscontrollers.operator.openshift.io", "-o=jsonpath="+capability).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(searchLine).To(o.ContainSubstring(ingressCapability))
	})

	// OCPBUGS-49769
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-85280-Haproxy router pods should validate the key and cert content before accepting", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			unSecSvcName        = "service-unsecure"
			svcPort             = "27017"
			fileDir             = "/tmp/OCP-85280"
			validity            = 30
			caSubj              = "/CN=NE-Test-Root-CA"
			caCert              = fileDir + "/exampleca.pem"
			caKey               = fileDir + "/exampleca.key"
			caCsr               = fileDir + "/exampleca.Csr"
			secretName          = "secret85280"
			ingressName         = "custom85280"
		)

		compat_otp.By("1.0: Create a file folder for saving the created key pair")
		defer os.RemoveAll(fileDir)
		err := os.MkdirAll(fileDir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.1: Create the CA key")
		opensslCmd := fmt.Sprintf(`openssl genpkey -algorithm RSA -out %s`, caKey)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.2: Create the CA Csr")
		opensslCmd = fmt.Sprintf(`openssl req -new -key %s -subj %s -out %s`, caKey, caSubj, caCsr)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("1.3: Create the broken CA certificate by used a different signature algorithm")
		opensslCmd = fmt.Sprintf(`openssl x509 -req -days %d  -in %s -signkey %s -outform der -out %s -sigopt rsa_padding_mode:pss -sigopt rsa_pss_saltlen:max`, validity, caCsr, caKey, caCert)
		_, err = exec.Command("bash", "-c", opensslCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("2.0: Create a pod and the serivce")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)

		compat_otp.By("3.0 Create a secret using the broken pair")
		output, err := oc.AsAdmin().WithoutNamespace().Run("create").Args("secret", "generic", secretName, "--from-file=tls.crt="+caCert, "--from-file=tls.key="+caKey, `--type=kubernetes.io/tls`, "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("created"))

		compat_otp.By("4.0 Create an ingress with the secret")
		routehost := "edge85280.apps." + getBaseDomain(oc)
		rule := fmt.Sprintf(`--rule=%s/*=%s:%s,tls=%s`, routehost, unSecSvcName, svcPort, secretName)
		output, err = oc.AsAdmin().WithoutNamespace().Run("create").Args("-n", ns, "ingress", ingressName, rule).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("created"))

		compat_otp.By("5.0 Check the route, which should not be admitted for ExtendedValidationFailed")
		output = getRoutes(oc, ns)
		o.Expect(output).To(o.ContainSubstring("ExtendedValidationFailed"))
	})

	// includes OCP-85950 and OCP-86100
	// OCP-85950: Propagate ingress labels to routes
	// OCP-86100: Negative tests of propagating ingress labels to routes
	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-High-85950-Propagate ingress labels to routes", func() {
		var (
			buildPruningBaseDir = compat_otp.FixturePath("testdata", "router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			testIngress         = filepath.Join(buildPruningBaseDir, "ingress-with-class.yaml")
			ingressName         = "unsecure-ingress85950"
		)

		compat_otp.By("1.0: Prepare the ingress file for testing")
		routehost := "unsecure85950" + ".apps." + getBaseDomain(oc)
		sedCmd := fmt.Sprintf(`sed -i'' -e 's@ingress-with-class@%s@g;s@mytest@openshift-default@g;s@foo.bar.com@%s@g' %s`, ingressName, routehost, testIngress)
		_, err := exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		compat_otp.By("2.0: Create pod, svc, and ingress")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		createResourceFromFile(oc, ns, testIngress)
		waitForOutputContains(oc, ns, "route", "{.items[0].metadata.name}", ingressName)
		routeName := getByJsonPath(oc, ns, "route", "{.items[0].metadata.name}")

		compat_otp.By(`3.0: Set route.openshift.io/reconcile-labels="true" annotation to the ingress`)
		setAnnotationAsAdmin(oc, ns, "ingress/"+ingressName, "route.openshift.io/reconcile-labels=true")

		compat_otp.By(`3.1: Add a label to the ingress`)
		addLabelAsAdmin(oc, ns, "ingress/"+ingressName, "shard=alpha")

		compat_otp.By(`3.2: Check and make sure the annotation and the label are synced to the route`)
		findAnnotation := getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`"route.openshift.io/reconcile-labels":"true"`))
		routeLabels := getByJsonPath(oc, ns, "route/"+routeName, "{.metadata.labels}")
		o.Expect(routeLabels).To(o.ContainSubstring(`"shard":"alpha"`))

		compat_otp.By(`4.0: Set route.openshift.io/reconcile-labels annotation with an invalid value ffalse to the ingress`)
		setAnnotationAsAdmin(oc, ns, "ingress/"+ingressName, `route.openshift.io/reconcile-labels=ffalse`)

		compat_otp.By(`4.1: Add another label to the ingress`)
		addLabelAsAdmin(oc, ns, "ingress/"+ingressName, "custom=internal")

		compat_otp.By(`4.2: Check and make sure the updated annotation is synced to the route, while the new added label isn't`)
		findAnnotation = getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`"route.openshift.io/reconcile-labels":"ffalse"`))
		routeLabels = getByJsonPath(oc, ns, "route/"+routeName, "{.metadata.labels}")
		o.Expect(routeLabels).To(o.ContainSubstring(`"shard":"alpha"`))
		o.Expect(routeLabels).NotTo(o.ContainSubstring(`"custom":"internal"`))

		compat_otp.By(`5.0: Modify the first added label of the ingress`)
		addLabelAsAdmin(oc, ns, "ingress/"+ingressName, "shard=beta")

		compat_otp.By(`5.1: Set route.openshift.io/reconcile-labels="true" annotation again to the ingress`)
		setAnnotationAsAdmin(oc, ns, "ingress/"+ingressName, `route.openshift.io/reconcile-labels=true`)

		compat_otp.By(`5.2: Check and make sure the updated annotation and all the ingress' labels are synced to the route`)
		findAnnotation = getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).To(o.ContainSubstring(`"route.openshift.io/reconcile-labels":"true"`))
		routeLabels = getByJsonPath(oc, ns, "route/"+routeName, "{.metadata.labels}")
		o.Expect(routeLabels).Should(o.And(
			o.ContainSubstring(`"shard":"beta"`),
			o.ContainSubstring(`"custom":"internal"`)))

		compat_otp.By(`6.0: Add a label with the name containing upper chars and the value containing the upper chars to the ingress`)
		addLabelAsAdmin(oc, ns, "ingress/"+ingressName, "Market_01=No1-In-APEC")

		compat_otp.By(`6.1: Check and make sure all the ingress' labels are synced to the route`)
		routeLabels = getByJsonPath(oc, ns, "route/"+routeName, "{.metadata.labels}")
		o.Expect(routeLabels).Should(o.And(
			o.ContainSubstring(`"shard":"beta"`),
			o.ContainSubstring(`"custom":"internal"`),
			o.ContainSubstring(`"Market_01":"No1-In-APEC"`)))

		// OCP-86100: Negative tests of propagating ingress labels to routes
		compat_otp.By(`7.0: Remove the "custom" label from the ingress, then check the route`)
		addLabelAsAdmin(oc, ns, "ingress/"+ingressName, "custom-")

		compat_otp.By(`7.1: Check the route that the "custom" label is removed as well`)
		routeLabels = getByJsonPath(oc, ns, "route/"+routeName, "{.metadata.labels}")
		o.Expect(routeLabels).NotTo(o.ContainSubstring(`custom`))
		o.Expect(routeLabels).Should(o.And(
			o.ContainSubstring(`"shard":"beta"`),
			o.ContainSubstring(`"Market_01":"No1-In-APEC"`)))

		compat_otp.By(`8.0: Delete the ingress route and wait for the new ingress route is created automatically`)
		err = oc.AsAdmin().Run("delete").Args("-n", ns, "route", routeName).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())
		waitForOutputContains(oc, ns, "route", "{.items[*].metadata.name}", ingressName)
		routeName = getByJsonPath(oc, ns, "route", "{.items[0].metadata.name}")

		compat_otp.By(`8.1: Check the route that all the ingress' labels are synced to it`)
		o.Expect(routeLabels).Should(o.And(
			o.ContainSubstring(`"shard":"beta"`),
			o.ContainSubstring(`"Market_01":"No1-In-APEC"`)))

		compat_otp.By(`9.0: Remove the "route.openshift.io/reconcile-labels" annotation from the ingress`)
		setAnnotationAsAdmin(oc, ns, "ingress/"+ingressName, `route.openshift.io/reconcile-labels-`)

		compat_otp.By(`9.1: Check the route that the annotation is removed from it`)
		findAnnotation = getAnnotation(oc, ns, "route", routeName)
		o.Expect(findAnnotation).NotTo(o.ContainSubstring(`route.openshift.io/reconcile-labels`))

		compat_otp.By(`9.2: Check the route that the labels are still there`)
		routeLabels = getByJsonPath(oc, ns, "route/"+routeName, "{.metadata.labels}")
		o.Expect(routeLabels).Should(o.And(
			o.ContainSubstring(`"shard":"beta"`),
			o.ContainSubstring(`"Market_01":"No1-In-APEC"`)))
	})
})
