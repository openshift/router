package router

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	exutil "github.com/openshift/router/ginkgo-test/test/extended/util"
)

var _ = g.Describe("[sig-network-edge] Network_Edge Component_Router", func() {
	defer g.GinkgoRecover()

	var oc = exutil.NewCLI("route-weight", exutil.KubeConfigPath())

	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Medium-10889-Sticky session could work normally after set weight for route", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = webServerDeployDescription{
				deployName:      "web-server-deploy1",
				svcSecureName:   "service-secure1",
				svcUnsecureName: "service-unsecure1",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy2 = webServerDeployDescription{
				deployName:      "web-server-deploy2",
				svcSecureName:   "service-secure2",
				svcUnsecureName: "service-unsecure2",
				template:        webServerTemplate,
				namespace:       "",
			}
			deploy1Label = "name=" + webServerDeploy1.deployName
			deploy2Label = "name=" + webServerDeploy2.deployName
			fileDir      = "/tmp/OCP-10889"
			cookie       = fileDir + "/cookie"
			routeName    = "edge10889"
		)

		exutil.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.namespace = ns
		webServerDeploy2.create(oc)
		ensurePodWithLabelReady(oc, ns, deploy1Label)
		ensurePodWithLabelReady(oc, ns, deploy2Label)

		exutil.By("Create edge route and set route-backends with multi serivces")
		createRoute(oc, ns, "edge", routeName, webServerDeploy1.svcUnsecureName, []string{})
		waitForOutput(oc, ns, "route/"+routeName, "{.status.ingress[0].conditions[0].status}", "True")
		// Note: the "balance roundrobin" is used for the route once set route-backends, no need to annotate the route"
		err := oc.Run("set").Args("route-backends", routeName, "service-unsecure1=60", "service-unsecure2=40").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Check haproxy.config and ensure deploy2 pod is added")
		routerPod := getOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_edge_http:" + ns + ":" + routeName
		backendConfig := readHaproxyConfig(oc, routerPod, backendBegin, "-A22", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 170"))

		exutil.By("Access the route, ensure web server 2 is in service and save the cookie")
		defer os.RemoveAll(fileDir)
		err = os.MkdirAll(fileDir, 0755)
		o.Expect(err).NotTo(o.HaveOccurred())
		edgeRouteHost := getRouteHost(oc, ns, routeName)
		curlCmd := fmt.Sprintf(`curl https://%s -sk -c %s --connect-timeout 10`, edgeRouteHost, cookie)
		expectedOutput := []string{"Hello-OpenShift web-server-deploy2"}
		repeatCmdOnClient(oc, curlCmd, expectedOutput, 60, 1)

		exutil.By("Access the route several times withoug cookie and ensure web server 1 is in service as well")
		curlCmd = fmt.Sprintf(`curl https://%s -sk --connect-timeout 10`, edgeRouteHost)
		expectedOutput = []string{"Hello-OpenShift web-server-deploy1"}
		repeatCmdOnClient(oc, curlCmd, expectedOutput, 60, 1)

		exutil.By("Access the route with the saved cookie for 6 times, ensure only web server 2 provides the service")
		curlCmd = fmt.Sprintf(`curl https://%s -sk -b %s --connect-timeout 10`, edgeRouteHost, cookie)
		expectedOutput = []string{"Hello-OpenShift web-server-deploy1", "Hello-OpenShift web-server-deploy2"}
		_, result := repeatCmdOnClient(oc, curlCmd, expectedOutput, 90, 6)
		o.Expect(result[0]).To(o.Equal(0))
		o.Expect(result[1]).To(o.Equal(6))
	})

	// author: hongli@redhat.com
	// Includes OCP-11306: Set negative backends weight for ab routing
	//          OCP-15382: Set max backends weight for ab routing
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Low-11351-Set backends weight to zero for ab routing", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = webServerDeployDescription{
				deployName:      "web-server-deploy1",
				svcSecureName:   "service-secure1",
				svcUnsecureName: "service-unsecure1",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy2 = webServerDeployDescription{
				deployName:      "web-server-deploy2",
				svcSecureName:   "service-secure2",
				svcUnsecureName: "service-unsecure2",
				template:        webServerTemplate,
				namespace:       "",
			}
			deploy1Label = "name=" + webServerDeploy1.deployName
			deploy2Label = "name=" + webServerDeploy2.deployName
			routeName    = "edge11351"
		)

		exutil.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.namespace = ns
		webServerDeploy2.create(oc)
		ensurePodWithLabelReady(oc, ns, deploy1Label)
		ensurePodWithLabelReady(oc, ns, deploy2Label)

		exutil.By("Create edge route and set route-backends with multi serivces")
		createRoute(oc, ns, "edge", routeName, webServerDeploy1.svcUnsecureName, []string{})
		waitForOutput(oc, ns, "route/"+routeName, "{.status.ingress[0].conditions[0].status}", "True")
		err := oc.Run("set").Args("route-backends", routeName, "service-unsecure1=0", "service-unsecure2=1").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Check haproxy.config and ensure weight of deploy2 is 1")
		routerPod := getOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_edge_http:" + ns + ":" + routeName
		backendConfig := readHaproxyConfig(oc, routerPod, backendBegin, "-A22", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 1"))

		exutil.By("Access the route for 6 times, ensure only deploy2 is in service")
		edgeRouteHost := getRouteHost(oc, ns, routeName)
		curlCmd := fmt.Sprintf(`curl https://%s -sk --connect-timeout 10`, edgeRouteHost)
		expectedOutput := []string{"Hello-OpenShift web-server-deploy1", "Hello-OpenShift web-server-deploy2"}
		_, result := repeatCmdOnClient(oc, curlCmd, expectedOutput, 90, 6)
		o.Expect(result[0]).To(o.Equal(0))
		o.Expect(result[1]).To(o.Equal(6))

		exutil.By("Set route-backends to zero for all serivces/backends")
		err = oc.Run("set").Args("route-backends", routeName, "--zero=true").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Check haproxy.config and ensure weight of deploy2 is 0")
		backendConfig = readHaproxyConfig(oc, routerPod, backendBegin, "-A22", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 0"))

		exutil.By("Access the route for 6 times, ensure all request are failing")
		curlCmd = fmt.Sprintf(`curl https://%s -skI --connect-timeout 10`, edgeRouteHost)
		expectedOutput = []string{"503"}
		_, result = repeatCmdOnClient(oc, curlCmd, expectedOutput, 90, 6)
		o.Expect(result[0]).To(o.Equal(6))

		exutil.By("Attempt to set route-backends to char")
		output, err := oc.Run("set").Args("route-backends", routeName, "service-unsecure1=abc", "service-unsecure2=^*%").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("invalid argument.*WEIGHT must be a number"))

		exutil.By("Attempt to set route-backends to negative weight")
		output, err = oc.Run("set").Args("route-backends", routeName, "service-unsecure1=-80", "service-unsecure2=-20").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("negative percentages are not allowed"))

		exutil.By("Attempt to set route-backends weight to 257")
		output, err = oc.Run("set").Args("route-backends", routeName, "service-unsecure1=257", "service-unsecure2=0").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("weight must be an integer between 0 and 256"))
	})

	// author: hongli@redhat.com
	// Includes OCP-11809: Set backends weight for passthough route
	//          OCP-11970: Set backends weight for reencrypt route
	//          OCP-12076: Set backends weight for unsecure route
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-High-11608-Set backends weight for edge/passthrough/reencrypt/unsecure route", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")
			destCA              = filepath.Join(buildPruningBaseDir, "ca-bundle.pem")

			webServerDeploy1 = webServerDeployDescription{
				deployName:      "web-server-deploy1",
				svcSecureName:   "service-secure1",
				svcUnsecureName: "service-unsecure1",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy2 = webServerDeployDescription{
				deployName:      "web-server-deploy2",
				svcSecureName:   "service-secure2",
				svcUnsecureName: "service-unsecure2",
				template:        webServerTemplate,
				namespace:       "",
			}
			deploy1Label      = "name=" + webServerDeploy1.deployName
			deploy2Label      = "name=" + webServerDeploy2.deployName
			edgeRouteName     = "edge11608"
			passRouteName     = "pass11608"
			reenRouteName     = "reen11608"
			unsecureRouteName = "unsecure11608"
		)

		exutil.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.namespace = ns
		webServerDeploy2.create(oc)
		ensurePodWithLabelReady(oc, ns, deploy1Label)
		ensurePodWithLabelReady(oc, ns, deploy2Label)

		exutil.By("Create edge route and set route-backends with multi serivces")
		createRoute(oc, ns, "edge", edgeRouteName, webServerDeploy1.svcUnsecureName, []string{})
		waitForOutput(oc, ns, "route/"+edgeRouteName, "{.status.ingress[0].conditions[0].status}", "True")
		err := oc.Run("set").Args("route-backends", edgeRouteName, "service-unsecure1=10", "service-unsecure2=10").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create passthrough route and set route-backends with multi serivces")
		createRoute(oc, ns, "passthrough", passRouteName, webServerDeploy1.svcSecureName, []string{})
		waitForOutput(oc, ns, "route/"+passRouteName, "{.status.ingress[0].conditions[0].status}", "True")
		err = oc.Run("set").Args("route-backends", passRouteName, "service-secure1=20%", "service-secure2=80%").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create reencrypt route and set route-backends with multi serivces")
		createRoute(oc, ns, "reencrypt", reenRouteName, webServerDeploy1.svcSecureName, []string{"--dest-ca-cert=" + destCA})
		waitForOutput(oc, ns, "route/"+reenRouteName, "{.status.ingress[0].conditions[0].status}", "True")
		// Note: the "balance roundrobin" is used for the route once set route-backends, no need to annotate the route"
		err = oc.Run("set").Args("route-backends", reenRouteName, "service-secure1=256", "service-secure2=256").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create unsecure route and set route-backends with multi serivces")
		createRoute(oc, ns, "http", unsecureRouteName, webServerDeploy1.svcUnsecureName, []string{})
		waitForOutput(oc, ns, "route/"+unsecureRouteName, "{.status.ingress[0].conditions[0].status}", "True")
		// Note: the "balance roundrobin" is used for the route once set route-backends, no need to annotate the route"
		err = oc.Run("set").Args("route-backends", unsecureRouteName, "service-unsecure1=50", "service-unsecure2=100").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Check edge route weight in haproxy.config")
		routerPod := getOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_edge_http:" + ns + ":" + edgeRouteName
		backendConfig := readHaproxyConfig(oc, routerPod, backendBegin, "-A22", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 256"))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 256"))

		exutil.By("Check passthrough route weight in haproxy.config")
		backendBegin = "be_tcp:" + ns + ":" + passRouteName
		backendConfig = readHaproxyConfig(oc, routerPod, backendBegin, "-A10", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 64"))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 256"))

		exutil.By("Check reencryp route weight in haproxy.config")
		backendBegin = "be_secure:" + ns + ":" + reenRouteName
		backendConfig = readHaproxyConfig(oc, routerPod, backendBegin, "-A22", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 256"))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 256"))

		exutil.By("Check unsecure route weight in haproxy.config")
		backendBegin = "be_http:" + ns + ":" + unsecureRouteName
		backendConfig = readHaproxyConfig(oc, routerPod, backendBegin, "-A22", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 128"))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 256"))
	})

	// author: hongli@redhat.com
	// Includes OCP-15259: Could not set more than 3 additional backends for route
	//          OCP-13521: The passthrough route with multiple service will set load balance policy to RoundRobin by default
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Medium-12088-Set multiple backends weight for route", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = webServerDeployDescription{
				deployName:      "web-server-deploy1",
				svcSecureName:   "service-secure1",
				svcUnsecureName: "service-unsecure1",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy2 = webServerDeployDescription{
				deployName:      "web-server-deploy2",
				svcSecureName:   "service-secure2",
				svcUnsecureName: "service-unsecure2",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy3 = webServerDeployDescription{
				deployName:      "web-server-deploy3",
				svcSecureName:   "service-secure3",
				svcUnsecureName: "service-unsecure3",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy4 = webServerDeployDescription{
				deployName:      "web-server-deploy4",
				svcSecureName:   "service-secure4",
				svcUnsecureName: "service-unsecure4",
				template:        webServerTemplate,
				namespace:       "",
			}

			deploy1Label = "name=" + webServerDeploy1.deployName
			deploy2Label = "name=" + webServerDeploy2.deployName
			deploy3Label = "name=" + webServerDeploy3.deployName
			deploy4Label = "name=" + webServerDeploy4.deployName
			routeName    = "pass12088"
		)

		exutil.By("Deploy four sets of web-server and services, three of them will be set as alternate backends")
		ns := oc.Namespace()
		webServerDeploy1.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.namespace = ns
		webServerDeploy2.create(oc)
		webServerDeploy3.namespace = ns
		webServerDeploy3.create(oc)
		webServerDeploy4.namespace = ns
		webServerDeploy4.create(oc)
		ensurePodWithLabelReady(oc, ns, deploy1Label)
		ensurePodWithLabelReady(oc, ns, deploy2Label)
		ensurePodWithLabelReady(oc, ns, deploy3Label)
		ensurePodWithLabelReady(oc, ns, deploy4Label)

		exutil.By("Create edge route and set route-backends with multi serivces")
		createRoute(oc, ns, "passthrough", routeName, webServerDeploy1.svcSecureName, []string{})
		waitForOutput(oc, ns, "route/"+routeName, "{.status.ingress[0].conditions[0].status}", "True")
		// Note: the "balance roundrobin" is used for the route once set route-backends, no need to annotate the route"
		err := oc.Run("set").Args("route-backends", routeName, "service-secure1=10", "service-secure2=20", "service-secure3=30", "service-secure4=40").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Check haproxy.config and ensure weight of deploy2/3/4 is added and balance is roundrobin")
		routerPod := getOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_tcp:" + ns + ":" + routeName
		backendConfig := readHaproxyConfig(oc, routerPod, backendBegin, "-A10", "pod:"+webServerDeploy3.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 64"))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 128"))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy3.deployName + ".+weight 192"))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy4.deployName + ".+weight 256"))
		o.Expect(backendConfig).To(o.ContainSubstring("balance roundrobin"))

		exutil.By("Attempt to set route-backends more than 3 alternate backends")
		output, err := oc.Run("set").Args("route-backends", routeName, "service-secure1=1", "service-secure2=1", "service-secure3=1", "service-secure4=1", "service-secure5=1").Output()
		o.Expect(err).To(o.HaveOccurred())
		o.Expect(output).To(o.MatchRegexp("cannot specify more than 3 .*backends"))
	})

	// author: hongli@redhat.com
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Medium-15902-Endpoint will end up weight 1 when scaled weight per endpoint is less than 1", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = webServerDeployDescription{
				deployName:      "web-server-deploy1",
				svcSecureName:   "service-secure1",
				svcUnsecureName: "service-unsecure1",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy2 = webServerDeployDescription{
				deployName:      "web-server-deploy2",
				svcSecureName:   "service-secure2",
				svcUnsecureName: "service-unsecure2",
				template:        webServerTemplate,
				namespace:       "",
			}
			deploy1Label = "name=" + webServerDeploy1.deployName
			deploy2Label = "name=" + webServerDeploy2.deployName
			routeName    = "edge15902"
		)

		exutil.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.namespace = ns
		webServerDeploy2.create(oc)
		ensurePodWithLabelReady(oc, ns, deploy1Label)
		ensurePodWithLabelReady(oc, ns, deploy2Label)

		exutil.By("Scale deploy1 to replicas 2")
		scaleDeploy(oc, ns, webServerDeploy1.deployName, 2)
		waitForOutput(oc, ns, "deployment/"+webServerDeploy1.deployName, "{.status.readyReplicas}", "2")

		exutil.By("Create edge route and set route-backends with multi serivces")
		createRoute(oc, ns, "edge", routeName, webServerDeploy1.svcUnsecureName, []string{})
		waitForOutput(oc, ns, "route/"+routeName, "{.status.ingress[0].conditions[0].status}", "True")
		err := oc.Run("set").Args("route-backends", routeName, "service-unsecure1=1", "service-unsecure2=256").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Check haproxy.config and ensure weight of each deploy1 pod is 1")
		routerPod := getOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_edge_http:" + ns + ":" + routeName
		backendConfig := readHaproxyConfig(oc, routerPod, backendBegin, "-A22", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 1"))
		o.Expect(strings.Count(backendConfig, "weight 1")).To(o.Equal(2))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 256"))
	})

	// author: hongli@redhat.com
	// Includes OCP-15994: Each endpoint gets weight/numberOfEndpoints portion of the requests - passthrough route
	//          OCP-15993: Each endpoint gets weight/numberOfEndpoints portion of the requests - edge route
	//          OCP-15995: Each endpoint gets weight/numberOfEndpoints portion of the requests - reencrypt route
	g.It("Author:hongli-ROSA-OSD_CCS-ARO-Medium-15910-Each endpoint gets weight/numberOfEndpoints portion of the requests", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")
			destCA              = filepath.Join(buildPruningBaseDir, "ca-bundle.pem")

			webServerDeploy1 = webServerDeployDescription{
				deployName:      "web-server-deploy1",
				svcSecureName:   "service-secure1",
				svcUnsecureName: "service-unsecure1",
				template:        webServerTemplate,
				namespace:       "",
			}

			webServerDeploy2 = webServerDeployDescription{
				deployName:      "web-server-deploy2",
				svcSecureName:   "service-secure2",
				svcUnsecureName: "service-unsecure2",
				template:        webServerTemplate,
				namespace:       "",
			}
			deploy1Label      = "name=" + webServerDeploy1.deployName
			deploy2Label      = "name=" + webServerDeploy2.deployName
			edgeRouteName     = "edge15910"
			passRouteName     = "pass15910"
			reenRouteName     = "reen15910"
			unsecureRouteName = "unsecure15910"
		)

		exutil.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.namespace = ns
		webServerDeploy2.create(oc)
		ensurePodWithLabelReady(oc, ns, deploy1Label)
		ensurePodWithLabelReady(oc, ns, deploy2Label)

		exutil.By("Scale deploy1 to replicas 2 and scale deploy2 to replicas 3")
		scaleDeploy(oc, ns, webServerDeploy1.deployName, 2)
		waitForOutput(oc, ns, "deployment/"+webServerDeploy1.deployName, "{.status.readyReplicas}", "2")
		scaleDeploy(oc, ns, webServerDeploy2.deployName, 3)
		waitForOutput(oc, ns, "deployment/"+webServerDeploy2.deployName, "{.status.readyReplicas}", "3")

		exutil.By("Create edge route and set route-backends with multi serivces")
		createRoute(oc, ns, "edge", edgeRouteName, webServerDeploy1.svcUnsecureName, []string{})
		waitForOutput(oc, ns, "route/"+edgeRouteName, "{.status.ingress[0].conditions[0].status}", "True")
		err := oc.Run("set").Args("route-backends", edgeRouteName, "service-unsecure1=10", "service-unsecure2=10").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create passthrough route and set route-backends with multi serivces")
		createRoute(oc, ns, "passthrough", passRouteName, webServerDeploy1.svcSecureName, []string{})
		waitForOutput(oc, ns, "route/"+passRouteName, "{.status.ingress[0].conditions[0].status}", "True")
		err = oc.Run("set").Args("route-backends", passRouteName, "service-secure1=20%", "service-secure2=80%").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create reencrypt route and set route-backends with multi serivces")
		createRoute(oc, ns, "reencrypt", reenRouteName, webServerDeploy1.svcSecureName, []string{"--dest-ca-cert=" + destCA})
		waitForOutput(oc, ns, "route/"+reenRouteName, "{.status.ingress[0].conditions[0].status}", "True")
		err = oc.Run("set").Args("route-backends", reenRouteName, "service-secure1=256", "service-secure2=256").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Create unsecure route and set route-backends with multi serivces")
		createRoute(oc, ns, "http", unsecureRouteName, webServerDeploy1.svcUnsecureName, []string{})
		waitForOutput(oc, ns, "route/"+unsecureRouteName, "{.status.ingress[0].conditions[0].status}", "True")
		err = oc.Run("set").Args("route-backends", unsecureRouteName, "service-unsecure1=50", "service-unsecure2=100").Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("Check edge route weight in haproxy.config")
		routerPod := getOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_edge_http:" + ns + ":" + edgeRouteName
		backendConfig := readHaproxyConfig(oc, routerPod, backendBegin, "-A25", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 256"))
		o.Expect(strings.Count(backendConfig, "weight 256")).To(o.Equal(2))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 170"))
		o.Expect(strings.Count(backendConfig, "weight 170")).To(o.Equal(3))

		exutil.By("Check passthrough route weight in haproxy.config")
		backendBegin = "be_tcp:" + ns + ":" + passRouteName
		backendConfig = readHaproxyConfig(oc, routerPod, backendBegin, "-A15", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 96"))
		o.Expect(strings.Count(backendConfig, "weight 96")).To(o.Equal(2))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 256"))
		o.Expect(strings.Count(backendConfig, "weight 256")).To(o.Equal(3))

		exutil.By("Check reencryp route weight in haproxy.config")
		backendBegin = "be_secure:" + ns + ":" + reenRouteName
		backendConfig = readHaproxyConfig(oc, routerPod, backendBegin, "-A25", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 256"))
		o.Expect(strings.Count(backendConfig, "weight 256")).To(o.Equal(2))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 170"))
		o.Expect(strings.Count(backendConfig, "weight 170")).To(o.Equal(3))

		exutil.By("Check unsecure route weight in haproxy.config")
		backendBegin = "be_http:" + ns + ":" + unsecureRouteName
		backendConfig = readHaproxyConfig(oc, routerPod, backendBegin, "-A25", "pod:"+webServerDeploy2.deployName)
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy1.deployName + ".+weight 192"))
		o.Expect(strings.Count(backendConfig, "weight 192")).To(o.Equal(2))
		o.Expect(backendConfig).To(o.MatchRegexp("server pod:" + webServerDeploy2.deployName + ".+weight 256"))
		o.Expect(strings.Count(backendConfig, "weight 256")).To(o.Equal(3))
	})

	// author: shudili@redhat.com
	g.It("Author:shudili-ROSA-OSD_CCS-ARO-Critical-67093-Alternate Backends and Weights for a route work well", func() {
		var (
			buildPruningBaseDir = exutil.FixturePath("testdata", "router")
			testPodSvcTP        = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = webServerDeployDescription{
				deployName:      "web-server-deploy01",
				svcSecureName:   "service-secure01",
				svcUnsecureName: "service-unsecure01",
				template:        testPodSvcTP,
				namespace:       "",
			}

			webServerDeploy2 = webServerDeployDescription{
				deployName:      "web-server-deploy02",
				svcSecureName:   "service-secure02",
				svcUnsecureName: "service-unsecure02",
				template:        testPodSvcTP,
				namespace:       "",
			}

			webServerDeploy3 = webServerDeployDescription{
				deployName:      "web-server-deploy03",
				svcSecureName:   "service-secure03",
				svcUnsecureName: "service-unsecure03",
				template:        testPodSvcTP,
				namespace:       "",
			}
			srv1Label    = "name=" + webServerDeploy1.deployName
			srv2Label    = "name=" + webServerDeploy2.deployName
			srv3Label    = "name=" + webServerDeploy3.deployName
			service1Name = webServerDeploy1.svcUnsecureName
			service2Name = webServerDeploy2.svcUnsecureName
			service3Name = webServerDeploy3.svcUnsecureName
		)

		exutil.By("Create 3 server pods and 3 unsecure services")
		ns := oc.Namespace()
		webServerDeploy1.namespace = ns
		webServerDeploy1.create(oc)
		webServerDeploy2.namespace = ns
		webServerDeploy2.create(oc)
		webServerDeploy3.namespace = ns
		webServerDeploy3.create(oc)
		ensurePodWithLabelReady(oc, ns, srv1Label)
		ensurePodWithLabelReady(oc, ns, srv2Label)
		ensurePodWithLabelReady(oc, ns, srv3Label)

		exutil.By("Expose a route with the unsecure service inside the project")
		output, SrvErr := oc.Run("expose").Args("service", service1Name).Output()
		o.Expect(SrvErr).NotTo(o.HaveOccurred())
		o.Expect(output).To(o.ContainSubstring(service1Name))

		// the below test step was for [OCPBUGS-29690] haproxy shouldn't be oom
		exutil.By("check the default weights for the selected routes are 1")
		routerpod := getOneRouterPodNameByIC(oc, "default")
		srvPod1Name, err := oc.Run("get").Args("pods", "-l", srv1Label, "-o=jsonpath=\"{.items[0].metadata.name}\"").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPod2Name, err := oc.Run("get").Args("pods", "-l", srv2Label, "-o=jsonpath=\"{.items[0].metadata.name}\"").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPod3Name, err := oc.Run("get").Args("pods", "-l", srv3Label, "-o=jsonpath=\"{.items[0].metadata.name}\"").Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		srvPod1Name = strings.Trim(srvPod1Name, "\"")
		srvPod2Name = strings.Trim(srvPod2Name, "\"")
		srvPod3Name = strings.Trim(srvPod3Name, "\"")
		// make sure all ingress-canary pods are ready
		ensurePodWithLabelReady(oc, "openshift-ingress-canary", `ingresscanary.operator.openshift.io/daemonset-ingresscanary=canary_controller`)
		selectedSrvNum := fmt.Sprintf("cat haproxy.config | grep -E \"server pod:ingress-canary|server pod:%s|server pod:%s|server pod:%s\"| wc -l", srvPod1Name, srvPod3Name, srvPod3Name)
		selectedWeight1Num := fmt.Sprintf("cat haproxy.config | grep -E \"server pod:ingress-canary|server pod:%s|server pod:%s|server pod:%s\"| grep \"weight 1\" |wc -l", srvPod1Name, srvPod3Name, srvPod3Name)
		srvPodNum, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", selectedSrvNum).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		weight1Num, err := oc.AsAdmin().WithoutNamespace().Run("exec").Args("-n", "openshift-ingress", routerpod, "--", "bash", "-c", selectedWeight1Num).Output()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(srvPodNum).To(o.Equal(weight1Num))

		exutil.By("patch the route with alternate backends and weights")
		patchRrAlBackend := "{\"metadata\":{\"annotations\":{\"haproxy.router.openshift.io/balance\": \"roundrobin\"}}, " +
			"\"spec\": {\"to\": {\"kind\": \"Service\", \"name\": \"" + service1Name + "\", \"weight\": 20}, \"alternateBackends\": [{\"kind\": \"Service\", \"name\": \"" + service2Name + "\", \"weight\": 10}, {\"kind\": \"Service\", \"name\": \"" + service3Name + "\", \"weight\": 10}]}}"
		err = oc.AsAdmin().WithoutNamespace().Run("patch").Args("-n", ns, "route/"+service1Name, "--type=merge", "-p", patchRrAlBackend).Execute()
		o.Expect(err).NotTo(o.HaveOccurred())

		exutil.By("check the route's backend config")
		backend := "be_http:" + ns + ":" + service1Name
		bk1Re := regexp.MustCompile("server pod:" + srvPod1Name + ".+weight 256")
		bk2Re := regexp.MustCompile("server pod:" + srvPod2Name + ".+weight 128")
		bk3Re := regexp.MustCompile("server pod:" + srvPod3Name + ".+weight 128")
		bk1 := readHaproxyConfig(oc, routerpod, backend, "-A27", "pod:"+srvPod1Name)
		o.Expect(len(bk1Re.FindStringSubmatch(bk1)[0]) > 1).To(o.BeTrue())
		bk2 := readHaproxyConfig(oc, routerpod, backend, "-A27", "pod:"+srvPod2Name)
		o.Expect(len(bk2Re.FindStringSubmatch(bk2)[0]) > 1).To(o.BeTrue())
		bk3 := readHaproxyConfig(oc, routerpod, backend, "-A27", "pod:"+srvPod3Name)
		o.Expect(len(bk3Re.FindStringSubmatch(bk3)[0]) > 1).To(o.BeTrue())
	})
})
