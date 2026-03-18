package router

import (
	"github.com/openshift/router-tests-extension/test/e2e/testdata"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/openshift-tests-private/test/extended/util"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

var _ = g.Describe("[OTP][sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("router-ingressclass", exutil.KubeConfigPath())

	// incorporate OCP-33960, OCP-33961, OCP-33962 and OCP-33986 into one
	// Test case creater: aiyengar@redhat.com - OCP-33960 Setting "route.openshift.io/termination" annotation to "Edge" in ingress resource deploys "Edge" terminated route object
	// Test case creater: aiyengar@redhat.com - OCP-33961 Setting "route.openshift.io/termination" annotation to "Passthrough" in ingress resource deploys "passthrough" terminated route object
	// Test case creater: aiyengar@redhat.com - OCP-33962 Setting "route.openshift.io/termination" annotation to "Reencrypt" in ingress resource deploys "reen" terminated route object
	// Test case creater: aiyengar@redhat.com - OCP-33986 Setting values other than "edge/passthrough/reencrypt" for "route.openshift.io/termination" annotation are ignored by ingress object
	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-33960-Setting 'route.openshift.io/termination' annotation to Edge/Passthrough/Reencrypt in ingress resource", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-signed-deploy.yaml")
			testIngress         = filepath.Join(buildPruningBaseDir, "ingress-resource.yaml")
			tmpdir              = "/tmp/OCP-33960-CA/"
			caKey               = tmpdir + "ca.key"
			caCrt               = tmpdir + "ca.crt"
			serverKey           = tmpdir + "server.key"
			serverCsr           = tmpdir + "server.csr"
			serverCrt           = tmpdir + "server.crt"
		)

		exutil.By("1. Create a server and client pod")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		routeEdgeHost := "ingress-edge-" + ns + ".apps." + baseDomain
		routePassHost := "ingress-passth-" + ns + ".apps." + baseDomain
		routeReenHost := "ingress-reen-" + ns + ".apps." + baseDomain
		routeRandHost := "33960-random-" + ns + ".example.com"
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getOneRouterPodNameByIC(oc, "default")

		exutil.By("2. Create a secret certificate for ingress edge and reen termination")
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

		exutil.By("3. Create edge, passthroguh and reen routes")
		sedCmd := fmt.Sprintf(`sed -i'' -e 's@edgehostname@%s@g;s@passhostname@%s@g;s@reenhostname@%s@g;s@randomhostname@%s@g' %s`, routeEdgeHost, routePassHost, routeReenHost, routeRandHost, testIngress)
		_, err = exec.Command("bash", "-c", sedCmd).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		createResourceFromFile(oc, ns, testIngress)
		routeOutput := getRoutes(oc, ns)

		// OCP-33960: Setting "route.openshift.io/termination" annotation to "Edge" in ingress resource deploys "Edge" terminated route object
		exutil.By("4. Verify the haproxy configuration to ensure the ingress edge route is configured")
		edgeBackendName := "be_edge_http:" + ns + ":ingress-edge"
		ensureHaproxyBlockConfigContains(oc, podName, edgeBackendName, []string{":service-unsecure:http:"})

		exutil.By("5. Check the reachability of the edge route")
		waitForOutsideCurlContains("https://"+routeEdgeHost, "-kI", `200`)

		// OCP-33961: Setting "route.openshift.io/termination" annotation to "Passthrough" in ingress resource deploys "passthrough" terminated route object
		exutil.By("6. Verify the haproxy configuration to ensure the ingress passthrough route is configured")
		passthroughBackendName := "be_tcp:" + ns + ":ingress-passth"
		ensureHaproxyBlockConfigContains(oc, podName, passthroughBackendName, []string{":service-secure:https:"})

		exutil.By("7. Check the reachability of the passthrough route")
		waitForOutsideCurlContains("https://"+routePassHost, "-kI", `200`)

		// OCP-33962: Setting "route.openshift.io/termination" annotation to "Reencrypt" in ingress resource deploys "reen" terminated route object
		exutil.By("8. Verify the haproxy configuration to ensure the ingress reen route is configured")
		reenBackendName := "be_secure:" + ns + ":ingress-reencrypt"
		reenBackendOutput := ensureHaproxyBlockConfigContains(oc, podName, reenBackendName, []string{":service-secure:https:"})
		o.Expect(reenBackendOutput).To(o.ContainSubstring(`/var/run/configmaps/service-ca/service-ca.crt`))

		exutil.By("9. Check the reachability of the reen route")
		waitForOutsideCurlContains("https://"+routeReenHost, "-kI", `200`)

		// OCP-33986: Setting values other than "edge/passthrough/reencrypt" for "route.openshift.io/termination" annotation are ignored by ingress object
		exutil.By("10. Verify the random route type's annotation are ignored")
		o.Expect(routeOutput).NotTo(o.ContainSubstring("abcd"))
	})

	// bug: 1820075
	// author: hongli@redhat.com
	g.It("Author:hongli-Critical-41109-use IngressClass controller for ingress-to-route", func() {
		var (
			output              string
			buildPruningBaseDir = testdata.FixturePath("router")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			testIngress         = filepath.Join(buildPruningBaseDir, "ingress-with-class.yaml")
		)

		exutil.By("Create pod, svc, and ingress that mismatch with default ingressclass")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		createResourceFromFile(oc, ns, testIngress)

		exutil.By("ensure no route is created from the ingress")
		output, err := oc.Run("get").Args("route").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).NotTo(o.ContainSubstring("ingress-with-class"))

		exutil.By("patch the ingress to use default ingressclass")
		patchResourceAsUser(oc, ns, "ingress/ingress-with-class", "{\"spec\":{\"ingressClassName\": \"openshift-default\"}}")
		exutil.By("ensure one route is created from the ingress")
		waitForOutputContains(oc, ns, "route", "{.items[*].metadata.name}", "ingress-with-class")

		// bug:- 1820075
		exutil.By("Confirm the address field is getting populated with the Router domain details")
		baseDomain := getBaseDomain(oc)
		ingressOut := getIngress(oc, ns)
		o.Expect(ingressOut).To(o.ContainSubstring("router-default.apps." + baseDomain))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-Critical-41117-ingress operator manages the IngressClass for each ingresscontroller", func() {
		buildPruningBaseDir := testdata.FixturePath("router")
		customTemp := filepath.Join(buildPruningBaseDir, "ingresscontroller-np.yaml")
		var (
			ingctrl = ingressControllerDescription{
				name:      "ocp41117",
				namespace: "openshift-ingress-operator",
				domain:    "",
				template:  customTemp,
			}
		)

		exutil.By("check the ingress class created by default ingresscontroller")
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("ingressclass/openshift-default").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("openshift.io/ingress-to-route"))

		exutil.By("create another custom ingresscontroller")
		baseDomain := getBaseDomain(oc)
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")

		exutil.By("check the ingressclass is created by custom ingresscontroller")
		ingressclassname := "openshift-" + ingctrl.name
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingressclass", ingressclassname).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("openshift.io/ingress-to-route"))

		exutil.By("delete the custom ingresscontroller and ensure the ingresscalsss is removed")
		ingctrl.delete(oc)
		output, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("ingressclass", ingressclassname).Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring("NotFound"))
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-Critical-51148-host name of the route depends on the subdomain if provided", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			customTemp          = filepath.Join(buildPruningBaseDir, "subdomain-routes/ocp51148-route.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			rut                 = routeDescription{
				namespace: "",
				domain:    "",
				subDomain: "foo",
				template:  customTemp,
			}
		)

		exutil.By("Create a pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")
		baseDomain := getBaseDomain(oc)
		rut.domain = "apps" + "." + baseDomain
		rut.namespace = ns

		exutil.By("create routes and get the details")
		rut.create(oc)
		// to show the route details
		getRoutes(oc, ns)

		exutil.By("check the domain name is present in 'foo-unsecure1' route details")
		output := getByJsonPath(oc, ns, "route/foo-unsecure1", "{.spec}")
		o.Expect(output).Should(o.ContainSubstring(`"subdomain":"foo"`))

		exutil.By("check the domain name is not present in 'foo-unsecure2' route details")
		output = getByJsonPath(oc, ns, "route/foo-unsecure2", "{.spec}")
		o.Expect(output).NotTo(o.ContainSubstring("subdomain"))

		exutil.By("check the domain name is present in 'foo-unsecure3' route details")
		output = getByJsonPath(oc, ns, "route/foo-unsecure3", "{.spec}")
		o.Expect(output).Should(o.ContainSubstring(`"subdomain":"foo"`))

		exutil.By("check the domain name is not present in 'foo-unsecure4' route details")
		output = getByJsonPath(oc, ns, "route/foo-unsecure4", "{.spec}")
		o.Expect(output).NotTo(o.ContainSubstring("subdomain"))

		// curling through default controller will not work for proxy cluster.
		if checkProxy(oc) {
			e2e.Logf("This is proxy cluster, skiping the curling part.")
		} else {
			exutil.By("check the reachability of the 'foo-unsecure1' host")
			waitForCurl(oc, podName[0], baseDomain, "foo.apps.", "Hello-OpenShift", "")

			exutil.By("check the reachability of the 'foo-unsecure2' host")
			waitForCurl(oc, podName[0], baseDomain, "foo-unsecure2-"+ns+".apps.", "Hello-OpenShift", "")

			exutil.By("check the reachability of the 'foo-unsecure3' host")
			waitForCurl(oc, podName[0], baseDomain, "man-"+ns+".apps.", "Hello-OpenShift", "")

			exutil.By("check the reachability of the 'foo-unsecure4' host")
			waitForCurl(oc, podName[0], baseDomain, "bar-"+ns+".apps.", "Hello-OpenShift", "")
		}
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-High-51429-different router deployment with same route using subdomain", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
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

		exutil.By("Create a pod")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")
		rut.domain = "apps" + "." + baseDomain
		rut.namespace = ns

		exutil.By("Create a custom ingresscontroller")
		ingctrl.domain = ingctrl.name + "." + baseDomain
		defer ingctrl.delete(oc)
		ingctrl.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl.name, "1")
		custContPod := getOneNewRouterPodFromRollingUpdate(oc, ingctrl.name)
		defaultContPod := getOneRouterPodNameByIC(oc, "default")

		exutil.By("create routes and get the details")
		rut.create(oc)
		getRoutes(oc, ns)

		exutil.By("check whether required host is present in 'foobar-unsecure' route details")
		waitForOutputContains(oc, ns, "route/foobar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"foobar.apps.%s"`, baseDomain))
		waitForOutputContains(oc, ns, "route/foobar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"foobar.ocp51429.%s"`, baseDomain))

		exutil.By("check the router pod and ensure the routes are loaded in haproxy.config in default controller")
		searchOutput1 := pollReadPodData(oc, "openshift-ingress", defaultContPod, "cat haproxy.config", "foobar-unsecure")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_http:" + ns + ":foobar-unsecure"))

		exutil.By("check the router pod and ensure the routes are loaded in haproxy.config of custom controller")
		searchOutput2 := pollReadPodData(oc, "openshift-ingress", custContPod, "cat haproxy.config", "foobar-unsecure")
		o.Expect(searchOutput2).To(o.ContainSubstring("backend be_http:" + ns + ":foobar-unsecure"))

		// curling through default controller will not work for proxy cluster.
		if checkProxy(oc) {
			e2e.Logf("This is proxy cluster, skiping the curling part through default controller.")
		} else {
			exutil.By("check the reachability of the 'foobar-unsecure' host in default controller")
			waitForCurl(oc, podName[0], baseDomain, "foobar.apps.", "Hello-OpenShift", "")
		}

		exutil.By("check the reachability of the 'foobar-unsecure' host in custom controller")
		custContIP := getPodv4Address(oc, custContPod, "openshift-ingress")
		waitForCurl(oc, podName[0], baseDomain, "foobar.ocp51429.", "Hello-OpenShift", custContIP)
	})

	// author: mjoseph@redhat.com
	g.It("Author:mjoseph-NonHyperShiftHOST-ROSA-OSD_CCS-ARO-High-51437-Router deployment using different shard with same subdomain ", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
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

		exutil.By("Create a pod")
		baseDomain := getBaseDomain(oc)
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name=web-server-deploy")
		podName := getPodListByLabel(oc, ns, "name=web-server-deploy")
		rut.domain = "apps" + "." + baseDomain
		rut.namespace = ns

		exutil.By("Create first shard ingresscontroller")
		ingctrl1.domain = ingctrl1.name + "." + baseDomain
		defer ingctrl1.delete(oc)
		ingctrl1.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl1.name, "1")
		custContPod1 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl1.name)

		exutil.By("Create second shard ingresscontroller")
		ingctrl2.domain = ingctrl2.name + "." + baseDomain
		defer ingctrl2.delete(oc)
		ingctrl2.create(oc)
		ensureRouterDeployGenerationIs(oc, ingctrl2.name, "1")
		custContPod2 := getOneNewRouterPodFromRollingUpdate(oc, ingctrl2.name)

		exutil.By("create routes and get the details")
		rut.create(oc)
		getRoutes(oc, ns)

		exutil.By("check whether required host is present in alpha ingress controller domain")
		waitForOutputContains(oc, ns, "route/bar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"bar.apps.%s"`, baseDomain))
		waitForOutputContains(oc, ns, "route/bar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"bar.alpha-ocp51437.%s"`, baseDomain))

		exutil.By("check the router pod and ensure the routes are loaded in haproxy.config of alpha controller")
		searchOutput1 := pollReadPodData(oc, "openshift-ingress", custContPod1, "cat haproxy.config", "bar-unsecure")
		o.Expect(searchOutput1).To(o.ContainSubstring("backend be_http:" + ns + ":bar-unsecure"))

		// curling through default controller will not work for proxy cluster.
		if checkProxy(oc) {
			e2e.Logf("This is proxy cluster, skiping the curling part through default controller.")
		} else {
			exutil.By("check the reachability of the 'bar-unsecure' host in default controller")
			waitForCurl(oc, podName[0], baseDomain, "bar.apps.", "Hello-OpenShift", "")
		}

		exutil.By("check the reachability of the 'bar-unsecure' host in 'alpha shard' controller")
		custContIP := getPodv4Address(oc, custContPod1, "openshift-ingress")
		waitForCurl(oc, podName[0], baseDomain, "bar.alpha-ocp51437.", "Hello-OpenShift", custContIP)

		exutil.By("Overwrite route with beta shard")
		_, err := oc.AsAdmin().WithoutNamespace().Run("label").Args("routes/bar-unsecure", "--overwrite", "shard=beta", "-n", ns).Output()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("check whether required host is present in beta ingress controller domain")
		waitForOutputContains(oc, ns, "route/bar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"bar.apps.%s"`, baseDomain))
		waitForOutputContains(oc, ns, "route/bar-unsecure", "{.status.ingress}", fmt.Sprintf(`"host":"bar.beta-ocp51437.%s"`, baseDomain))

		exutil.By("check the router pod and ensure the routes are loaded in haproxy.config of beta controller")
		searchOutput2 := pollReadPodData(oc, "openshift-ingress", custContPod2, "cat haproxy.config", "bar-unsecure")
		o.Expect(searchOutput2).To(o.ContainSubstring("backend be_http:" + ns + ":bar-unsecure"))

		exutil.By("check the reachability of the 'bar-unsecure' host in 'beta shard' controller")
		custContIP2 := getPodv4Address(oc, custContPod2, "openshift-ingress")
		waitForCurl(oc, podName[0], baseDomain, "bar.beta-ocp51437.", "Hello-OpenShift", custContIP2)
	})

	// bug: 1914127
	g.It("Author:shudili-NonPreRelease-Longduration-High-56228-Deletion of default router service under the openshift ingress namespace hangs flag [Disruptive]", func() {
		var (
			svcResource = "service/router-default"
			namespace   = "openshift-ingress"
		)

		exutil.By("check if the cluster has the router-default service")
		output, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("service", "-n", namespace).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		if !strings.Contains(output, "router-default") {
			g.Skip("This cluster has NOT the router-defaut service, skip the test.")
		}

		exutil.By("check if all COs are in good status")
		badOpList := checkAllClusterOperatorsStatus(oc)
		if len(badOpList) > 0 {
			g.Skip("Some cluster operators are NOT in good status, skip the test.")
		}

		exutil.By("check the created time of svc router-default")
		jsonPath := "{.metadata.creationTimestamp}"
		svcCreatedTime1 := getByJsonPath(oc, namespace, svcResource, jsonPath)
		o.Expect(svcCreatedTime1).NotTo(o.BeEmpty())

		exutil.By("try to delete the svc router-default, should no errors")
		defer ensureAllClusterOperatorsNormal(oc, 720)
		err = oc.AsAdmin().WithoutNamespace().Run("delete").Args(svcResource, "-n", namespace).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("wait for new svc router-default is created")
		jsonPath = "{.metadata.name}"
		waitForOutputEquals(oc, namespace, svcResource, jsonPath, "router-default")

		exutil.By("check the created time of the new svc router-default")
		jsonPath = "{.metadata.creationTimestamp}"
		svcCreatedTime2 := getByJsonPath(oc, namespace, svcResource, jsonPath)
		o.Expect(svcCreatedTime2).NotTo(o.BeEmpty())
		o.Expect(svcCreatedTime1).NotTo(o.Equal(svcCreatedTime2))
	})

	// bug: 2013004
	g.It("ARO-Author:shudili-High-57089-Error syncing load balancer and failed to parse the VMAS ID on Azure platform", func() {
		var (
			buildPruningBaseDir = testdata.FixturePath("router")
			lbServices          = filepath.Join(buildPruningBaseDir, "bug2013004-lb-services.yaml")
			testPodSvc          = filepath.Join(buildPruningBaseDir, "web-server-deploy.yaml")
			srvrcInfo           = "web-server-deploy"
			externalSvc         = "external-lb-57089"
			internalSvc         = "internal-lb-57089"
		)

		// skip if platform is not AZURE
		exutil.By("Pre-flight check for the platform type")
		platformtype := exutil.CheckPlatform(oc)
		if platformtype != "azure" {
			g.Skip("Skip for it not azure platform")
		}

		exutil.By("create a server pod")
		ns := oc.Namespace()
		createResourceFromFile(oc, ns, testPodSvc)
		ensurePodWithLabelReady(oc, ns, "name="+srvrcInfo)

		exutil.By("try to create an external load balancer service and an internal load balancer service")
		operateResourceFromFile(oc, "create", ns, lbServices)
		waitForOutputEquals(oc, ns, "service/"+externalSvc, "{.metadata.name}", externalSvc)
		waitForOutputEquals(oc, ns, "service/"+internalSvc, "{.metadata.name}", internalSvc)

		exutil.By("check if the lb services have obtained the EXTERNAL-IPs")
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

		exutil.By("1. Create route and get the details")
		removeRoute := fmt.Sprintf("[{\"op\":\"remove\", \"path\":\"/spec/componentRoutes\", \"value\":[{\"hostname\": \"1digit9.apps.%s\", \"name\": \"downloads\", \"namespace\": \"openshift-console\"}]}]}]", getBaseDomain(oc))
		addRoute := fmt.Sprintf("[{\"op\":\"add\", \"path\":\"/spec/componentRoutes\", \"value\":[{\"hostname\": \"1digit9.apps.%s\", \"name\": \"downloads\", \"namespace\": \"openshift-console\"}]}]}]", getBaseDomain(oc))
		defer patchGlobalResourceAsAdmin(oc, resourceName, removeRoute)
		patchGlobalResourceAsAdmin(oc, resourceName, addRoute)
		waitForOutputContains(oc, "openshift-console", "route", "{.items..metadata.name}", "downloads-custom")

		exutil.By("2. Check the router pod and ensure the routes are loaded in haproxy.config")
		podname := getOneRouterPodNameByIC(oc, "default")
		backendConfig := pollReadPodData(oc, "openshift-ingress", podname, "cat haproxy.config", "downloads-custom")
		o.Expect(backendConfig).To(o.ContainSubstring("backend be_edge_http:openshift-console:downloads-custom"))

		exutil.By("3. Confirm from the component Route, the RFC complaint hostname")
		cmd := fmt.Sprintf(`1digit9.apps.%s`, getBaseDomain(oc))
		waitForOutputContains(oc, oc.Namespace(), "ingress.config.openshift.io/cluster", "{.spec.componentRoutes[0].hostname}", cmd)

		// OCP-14059: Use the default destination CA of router if the route does not specify one for reencrypt route
		// since console route is using the reencrypt route without destination CA we are using it to check
		exutil.By("4. Confirm from the console service the 'serving-cert-secret-name'")
		findAnnotation := getAnnotation(oc, "openshift-console", "svc", "console")
		o.Expect(findAnnotation).To(o.Or(o.ContainSubstring(`service.alpha.openshift.io/serving-cert-secret-name":"console-serving-cert`), o.ContainSubstring(`service.beta.openshift.io/serving-cert-secret-name":"console-serving-cert`)))

		exutil.By("5. Confirm from the 'service-ca.crt' is present in haproxy.config for console route")
		backendConfig1 := pollReadPodData(oc, "openshift-ingress", podname, "cat haproxy.config", "console.openshift-console.svc")
		o.Expect(backendConfig1).To(o.ContainSubstring("required ca-file /var/run/configmaps/service-ca/service-ca.crt"))
	})

	g.It("Author:mjoseph-ROSA-OSD_CCS-ARO-Critical-73619-Checking whether the ingress operator is enabled as optional component", func() {
		var (
			enabledCapabilities = "{.status.capabilities.enabledCapabilities}"
			capability          = "{.metadata.annotations}"
			ingressCapability   = `"capability.openshift.io/name":"Ingress"`
		)

		exutil.By("Check whether 'enabledCapabilities' is enabled in cluster version resource")
		searchLine, err := oc.AsAdmin().WithoutNamespace().Run("get").Args("clusterversion", "version", "-o=jsonpath="+enabledCapabilities).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(searchLine).To(o.ContainSubstring("Ingress"))

		exutil.By("Check the Ingress capability in dnsrecords crd")
		searchLine, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("crd", "dnsrecords.ingress.operator.openshift.io", "-o=jsonpath="+capability).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(searchLine).To(o.ContainSubstring(ingressCapability))

		exutil.By("Check the Ingress capability in ingresscontrollers crd")
		searchLine, err = oc.AsAdmin().WithoutNamespace().Run("get").Args("crd", "ingresscontrollers.operator.openshift.io", "-o=jsonpath="+capability).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(searchLine).To(o.ContainSubstring(ingressCapability))
	})
})
