package qe

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	util "github.com/openshift/router-tests-extension/test"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = g.Describe("[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Route-Weight", func() {
	defer g.GinkgoRecover()

	var oc = compat_otp.NewCLI("route-weight", compat_otp.KubeConfigPath())

	//Author:hongli@redhat.com
	g.It("Sticky session could work normally after set weight for route", g.Label("OCP-10889"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy1",
				SvcSecureName:   "service-secure1",
				SvcUnsecureName: "service-unsecure1",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy2 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy2",
				SvcSecureName:   "service-secure2",
				SvcUnsecureName: "service-unsecure2",
				Template:        webServerTemplate,
				Namespace:       "",
			}
			deploy1Label = "name=" + webServerDeploy1.DeployName
			deploy2Label = "name=" + webServerDeploy2.DeployName
			routeName    = "edge10889"
		)

		compat_otp.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.Namespace = ns
		webServerDeploy1.Create(oc)
		webServerDeploy2.Namespace = ns
		webServerDeploy2.Create(oc)
		util.EnsurePodWithLabelReady(oc, ns, deploy1Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy2Label)

		compat_otp.By("Create edge route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "edge", routeName, webServerDeploy1.SvcUnsecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")
		err := oc.Run("set").Args("route-backends", routeName, "service-unsecure1=60", "service-unsecure2=40").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-10889: Create edge route and set route-backends with multi services")

		compat_otp.By("Check haproxy.config and ensure deploy2 pod is added")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, "be_edge_http:"+ns+":"+routeName, []string{"server pod:" + webServerDeploy2.DeployName + ".+weight 170"})

		compat_otp.By("Access the route, ensure web server 2 is in service and save the cookie")
		fileDir, err := os.MkdirTemp("", "ocp-10889-cookie")
		o.Expect(err).NotTo(o.HaveOccurred(), "failed to create private cookie directory for OCP-10889")
		defer os.RemoveAll(fileDir)
		cookie := filepath.Join(fileDir, "cookie")
		edgeRouteHost := util.GetRouteHost(oc, ns, routeName)
		curlCmd := fmt.Sprintf(`curl https://%s -sk -c %s --connect-timeout 10`, edgeRouteHost, cookie)
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift web-server-deploy2", 60, 1)

		compat_otp.By("Access the route several times without cookie and ensure web server 1 is in service as well")
		curlCmd = fmt.Sprintf(`curl https://%s -sk --connect-timeout 10`, edgeRouteHost)
		util.RepeatCmdOnClient(oc, curlCmd, "Hello-OpenShift web-server-deploy1", 60, 1)

		compat_otp.By("Access the route with the saved cookie for 6 times, ensure only web server 2 provides the service")
		curlCmd = fmt.Sprintf(`curl https://%s -sk -b %s --connect-timeout 10`, edgeRouteHost, cookie)
		expectedOutput := []string{"Hello-OpenShift web-server-deploy1", "Hello-OpenShift web-server-deploy2"}
		_, result := util.RepeatCmdOnClient(oc, curlCmd, expectedOutput, 90, 6)
		o.Expect(result[0]).To(o.Equal(0), "OCP-10889: Access the route with the saved cookie for 6 times, ensure only web server 2 provides the service")
		o.Expect(result[1]).To(o.Equal(6), "OCP-10889: Access the route with the saved cookie for 6 times, ensure only web server 2 provides the service")
	})

	// Includes OCP-11809: Set backends weight for passthrough route
	//          OCP-11970: Set backends weight for reencrypt route
	//          OCP-12076: Set backends weight for unsecure route
	//Author:hongli@redhat.com
	g.It("Set backends weight for edge/passthrough/reencrypt/unsecure route", g.Label("OCP-11608"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")
			destCA              = filepath.Join(buildPruningBaseDir, "ca-bundle.pem")

			webServerDeploy1 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy1",
				SvcSecureName:   "service-secure1",
				SvcUnsecureName: "service-unsecure1",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy2 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy2",
				SvcSecureName:   "service-secure2",
				SvcUnsecureName: "service-unsecure2",
				Template:        webServerTemplate,
				Namespace:       "",
			}
			deploy1Label      = "name=" + webServerDeploy1.DeployName
			deploy2Label      = "name=" + webServerDeploy2.DeployName
			edgeRouteName     = "edge11608"
			passRouteName     = "pass11608"
			reenRouteName     = "reen11608"
			unsecureRouteName = "unsecure11608"
		)

		compat_otp.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.Namespace = ns
		webServerDeploy1.Create(oc)
		webServerDeploy2.Namespace = ns
		webServerDeploy2.Create(oc)
		util.EnsurePodWithLabelReady(oc, ns, deploy1Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy2Label)

		compat_otp.By("Create edge route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "edge", edgeRouteName, webServerDeploy1.SvcUnsecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, edgeRouteName, "default")
		err := oc.Run("set").Args("route-backends", edgeRouteName, "service-unsecure1=10", "service-unsecure2=10").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-11608: Create edge route and set route-backends with multi services")

		compat_otp.By("Create passthrough route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "passthrough", passRouteName, webServerDeploy1.SvcSecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, passRouteName, "default")
		err = oc.Run("set").Args("route-backends", passRouteName, "service-secure1=20%", "service-secure2=80%").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-11608: Create passthrough route and set route-backends with multi services")

		compat_otp.By("Create reencrypt route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "reencrypt", reenRouteName, webServerDeploy1.SvcSecureName, []string{"--dest-ca-cert=" + destCA})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, reenRouteName, "default")
		err = oc.Run("set").Args("route-backends", reenRouteName, "service-secure1=256", "service-secure2=256").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-11608: Create reencrypt route and set route-backends with multi services")

		compat_otp.By("Create unsecure route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "http", unsecureRouteName, webServerDeploy1.SvcUnsecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unsecureRouteName, "default")
		err = oc.Run("set").Args("route-backends", unsecureRouteName, "service-unsecure1=50", "service-unsecure2=100").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-11608: Create unsecure route and set route-backends with multi services")

		compat_otp.By("Check edge route weight in haproxy.config")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_edge_http:" + ns + ":" + edgeRouteName
		util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{"server pod:" + webServerDeploy1.DeployName + ".+weight 256", "server pod:" + webServerDeploy2.DeployName + ".+weight 256"})

		compat_otp.By("Check passthrough route weight in haproxy.config")
		backendBegin = "be_tcp:" + ns + ":" + passRouteName
		util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{"server pod:" + webServerDeploy1.DeployName + ".+weight 64", "server pod:" + webServerDeploy2.DeployName + ".+weight 256"})

		compat_otp.By("Check reencrypt route weight in haproxy.config")
		backendBegin = "be_secure:" + ns + ":" + reenRouteName
		util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{"server pod:" + webServerDeploy1.DeployName + ".+weight 256", "server pod:" + webServerDeploy2.DeployName + ".+weight 256"})

		compat_otp.By("Check unsecure route weight in haproxy.config")
		backendBegin = "be_http:" + ns + ":" + unsecureRouteName
		util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{"server pod:" + webServerDeploy1.DeployName + ".+weight 128", "server pod:" + webServerDeploy2.DeployName + ".+weight 256"})
	})

	// Includes OCP-15259: Could not set more than 3 additional backends for route
	//          OCP-13521: The passthrough route with multiple service will set load balance policy to RoundRobin by default
	//Author:hongli@redhat.com
	g.It("Set multiple backends weight for route", g.Label("OCP-12088"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy1",
				SvcSecureName:   "service-secure1",
				SvcUnsecureName: "service-unsecure1",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy2 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy2",
				SvcSecureName:   "service-secure2",
				SvcUnsecureName: "service-unsecure2",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy3 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy3",
				SvcSecureName:   "service-secure3",
				SvcUnsecureName: "service-unsecure3",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy4 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy4",
				SvcSecureName:   "service-secure4",
				SvcUnsecureName: "service-unsecure4",
				Template:        webServerTemplate,
				Namespace:       "",
			}
			deploy1Label = "name=" + webServerDeploy1.DeployName
			deploy2Label = "name=" + webServerDeploy2.DeployName
			deploy3Label = "name=" + webServerDeploy3.DeployName
			deploy4Label = "name=" + webServerDeploy4.DeployName
			routeName    = "pass12088"
		)

		compat_otp.By("Deploy four sets of web-server and services, three of them will be set as alternate backends")
		ns := oc.Namespace()
		webServerDeploy1.Namespace = ns
		webServerDeploy1.Create(oc)
		webServerDeploy2.Namespace = ns
		webServerDeploy2.Create(oc)
		webServerDeploy3.Namespace = ns
		webServerDeploy3.Create(oc)
		webServerDeploy4.Namespace = ns
		webServerDeploy4.Create(oc)
		util.EnsurePodWithLabelReady(oc, ns, deploy1Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy2Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy3Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy4Label)

		compat_otp.By("Create passthrough route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "passthrough", routeName, webServerDeploy1.SvcSecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")
		err := oc.Run("set").Args("route-backends", routeName, "service-secure1=10", "service-secure2=20", "service-secure3=30", "service-secure4=40").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-12088: Create passthrough route and set route-backends with multi services")

		compat_otp.By("Check haproxy.config and ensure weight of deploy2/3/4 is added and balance is roundrobin")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_tcp:" + ns + ":" + routeName
		util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{
			"server pod:" + webServerDeploy1.DeployName + ".+weight 64",
			"server pod:" + webServerDeploy2.DeployName + ".+weight 128",
			"server pod:" + webServerDeploy3.DeployName + ".+weight 192",
			"server pod:" + webServerDeploy4.DeployName + ".+weight 256",
		})

		compat_otp.By("Attempt to set route-backends more than 3 alternate backends")
		output, err := oc.Run("set").Args("route-backends", routeName, "service-secure1=1", "service-secure2=1", "service-secure3=1", "service-secure4=1", "service-secure5=1").Output()
		o.Expect(err).To(o.HaveOccurred(), "OCP-12088: Attempt to set route-backends more than 3 alternate backends")
		o.Expect(output).To(o.MatchRegexp("cannot specify more than 3 .*backends"), "OCP-12088: Attempt to set route-backends more than 3 alternate backends")
	})

	//Author:hongli@redhat.com
	g.It("Endpoint will end up weight 1 when scaled weight per endpoint is less than 1", g.Label("OCP-15902"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy1",
				SvcSecureName:   "service-secure1",
				SvcUnsecureName: "service-unsecure1",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy2 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy2",
				SvcSecureName:   "service-secure2",
				SvcUnsecureName: "service-unsecure2",
				Template:        webServerTemplate,
				Namespace:       "",
			}
			deploy1Label = "name=" + webServerDeploy1.DeployName
			deploy2Label = "name=" + webServerDeploy2.DeployName
			routeName    = "edge15902"
		)

		compat_otp.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.Namespace = ns
		webServerDeploy1.Create(oc)
		webServerDeploy2.Namespace = ns
		webServerDeploy2.Create(oc)
		util.EnsurePodWithLabelReady(oc, ns, deploy1Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy2Label)

		compat_otp.By("Scale deploy1 to replicas 2")
		util.ScaleDeploy(oc, ns, webServerDeploy1.DeployName, 2)
		util.WaitForOutputEquals(oc, ns, "deployment/"+webServerDeploy1.DeployName, "{.status.readyReplicas}", "2")

		compat_otp.By("Create edge route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "edge", routeName, webServerDeploy1.SvcUnsecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, routeName, "default")
		err := oc.Run("set").Args("route-backends", routeName, "service-unsecure1=1", "service-unsecure2=256").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-15902: Create edge route and set route-backends with multi services")

		compat_otp.By("Check haproxy.config and ensure weight of each deploy1 pod is 1")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_edge_http:" + ns + ":" + routeName
		backendConfig := util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{
			"server pod:" + webServerDeploy1.DeployName + ".+weight 1",
			"server pod:" + webServerDeploy2.DeployName + ".+weight 256",
		})
		o.Expect(strings.Count(backendConfig, "weight 1")).To(o.Equal(2), "OCP-15902: Check haproxy.config and ensure weight of each deploy1 pod is 1")
	})

	// Includes OCP-15994: Each endpoint gets weight/numberOfEndpoints portion of the requests - passthrough route
	//          OCP-15993: Each endpoint gets weight/numberOfEndpoints portion of the requests - edge route
	//          OCP-15995: Each endpoint gets weight/numberOfEndpoints portion of the requests - reencrypt route
	//Author:hongli@redhat.com
	g.It("Each endpoint gets weight/numberOfEndpoints portion of the requests", g.Label("OCP-15910"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")
			destCA              = filepath.Join(buildPruningBaseDir, "ca-bundle.pem")

			webServerDeploy1 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy1",
				SvcSecureName:   "service-secure1",
				SvcUnsecureName: "service-unsecure1",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy2 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy2",
				SvcSecureName:   "service-secure2",
				SvcUnsecureName: "service-unsecure2",
				Template:        webServerTemplate,
				Namespace:       "",
			}
			deploy1Label      = "name=" + webServerDeploy1.DeployName
			deploy2Label      = "name=" + webServerDeploy2.DeployName
			edgeRouteName     = "edge15910"
			passRouteName     = "pass15910"
			reenRouteName     = "reen15910"
			unsecureRouteName = "unsecure15910"
		)

		compat_otp.By("Deploy two sets of web-server and services")
		ns := oc.Namespace()
		webServerDeploy1.Namespace = ns
		webServerDeploy1.Create(oc)
		webServerDeploy2.Namespace = ns
		webServerDeploy2.Create(oc)
		util.EnsurePodWithLabelReady(oc, ns, deploy1Label)
		util.EnsurePodWithLabelReady(oc, ns, deploy2Label)

		compat_otp.By("Scale deploy1 to replicas 2 and scale deploy2 to replicas 3")
		util.ScaleDeploy(oc, ns, webServerDeploy1.DeployName, 2)
		util.WaitForOutputEquals(oc, ns, "deployment/"+webServerDeploy1.DeployName, "{.status.readyReplicas}", "2")
		util.ScaleDeploy(oc, ns, webServerDeploy2.DeployName, 3)
		util.WaitForOutputEquals(oc, ns, "deployment/"+webServerDeploy2.DeployName, "{.status.readyReplicas}", "3")

		compat_otp.By("Create edge route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "edge", edgeRouteName, webServerDeploy1.SvcUnsecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, edgeRouteName, "default")
		err := oc.Run("set").Args("route-backends", edgeRouteName, "service-unsecure1=10", "service-unsecure2=10").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-15910: Create edge route and set route-backends with multi services")

		compat_otp.By("Create passthrough route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "passthrough", passRouteName, webServerDeploy1.SvcSecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, passRouteName, "default")
		err = oc.Run("set").Args("route-backends", passRouteName, "service-secure1=20%", "service-secure2=80%").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-15910: Create passthrough route and set route-backends with multi services")

		compat_otp.By("Create reencrypt route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "reencrypt", reenRouteName, webServerDeploy1.SvcSecureName, []string{"--dest-ca-cert=" + destCA})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, reenRouteName, "default")
		err = oc.Run("set").Args("route-backends", reenRouteName, "service-secure1=256", "service-secure2=256").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-15910: Create reencrypt route and set route-backends with multi services")

		compat_otp.By("Create unsecure route and set route-backends with multi services")
		util.CreateRoute(oc, ns, "http", unsecureRouteName, webServerDeploy1.SvcUnsecureName, []string{})
		util.EnsureRouteIsAdmittedByIngressController(oc, ns, unsecureRouteName, "default")
		err = oc.Run("set").Args("route-backends", unsecureRouteName, "service-unsecure1=50", "service-unsecure2=100").Execute()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-15910: Create unsecure route and set route-backends with multi services")

		compat_otp.By("Check edge route weight in haproxy.config")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		backendBegin := "be_edge_http:" + ns + ":" + edgeRouteName
		backendConfig := util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{
			"server pod:" + webServerDeploy1.DeployName + ".+weight 256",
			"server pod:" + webServerDeploy2.DeployName + ".+weight 170",
		})
		o.Expect(strings.Count(backendConfig, "weight 256")).To(o.Equal(2), "OCP-15910: Check edge route weight in haproxy.config")
		o.Expect(strings.Count(backendConfig, "weight 170")).To(o.Equal(3), "OCP-15910: Check edge route weight in haproxy.config")

		compat_otp.By("Check passthrough route weight in haproxy.config")
		backendBegin = "be_tcp:" + ns + ":" + passRouteName
		backendConfig = util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{
			"server pod:" + webServerDeploy1.DeployName + ".+weight 96",
			"server pod:" + webServerDeploy2.DeployName + ".+weight 256",
		})
		o.Expect(strings.Count(backendConfig, "weight 96")).To(o.Equal(2), "OCP-15910: Check passthrough route weight in haproxy.config")
		o.Expect(strings.Count(backendConfig, "weight 256")).To(o.Equal(3), "OCP-15910: Check passthrough route weight in haproxy.config")

		compat_otp.By("Check reencrypt route weight in haproxy.config")
		backendBegin = "be_secure:" + ns + ":" + reenRouteName
		backendConfig = util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{
			"server pod:" + webServerDeploy1.DeployName + ".+weight 256",
			"server pod:" + webServerDeploy2.DeployName + ".+weight 170",
		})
		o.Expect(strings.Count(backendConfig, "weight 256")).To(o.Equal(2), "OCP-15910: Check reencrypt route weight in haproxy.config")
		o.Expect(strings.Count(backendConfig, "weight 170")).To(o.Equal(3), "OCP-15910: Check reencrypt route weight in haproxy.config")

		compat_otp.By("Check unsecure route weight in haproxy.config")
		backendBegin = "be_http:" + ns + ":" + unsecureRouteName
		backendConfig = util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backendBegin, []string{
			"server pod:" + webServerDeploy1.DeployName + ".+weight 192",
			"server pod:" + webServerDeploy2.DeployName + ".+weight 256",
		})
		o.Expect(strings.Count(backendConfig, "weight 192")).To(o.Equal(2), "OCP-15910: Check unsecure route weight in haproxy.config")
		o.Expect(strings.Count(backendConfig, "weight 256")).To(o.Equal(3), "OCP-15910: Check unsecure route weight in haproxy.config")
	})

	//Author:shudili@redhat.com
	g.It("Alternate Backends and Weights for a route work well", g.Label("OCP-67093"), func() {
		var (
			buildPruningBaseDir = TestdataDir()
			webServerTemplate   = filepath.Join(buildPruningBaseDir, "template-web-server-deploy.yaml")

			webServerDeploy1 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy01",
				SvcSecureName:   "service-secure01",
				SvcUnsecureName: "service-unsecure01",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy2 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy02",
				SvcSecureName:   "service-secure02",
				SvcUnsecureName: "service-unsecure02",
				Template:        webServerTemplate,
				Namespace:       "",
			}

			webServerDeploy3 = util.WebServerDeployDescription{
				DeployName:      "web-server-deploy03",
				SvcSecureName:   "service-secure03",
				SvcUnsecureName: "service-unsecure03",
				Template:        webServerTemplate,
				Namespace:       "",
			}
			srv1Label    = "name=" + webServerDeploy1.DeployName
			srv2Label    = "name=" + webServerDeploy2.DeployName
			srv3Label    = "name=" + webServerDeploy3.DeployName
			service1Name = webServerDeploy1.SvcUnsecureName
			service2Name = webServerDeploy2.SvcUnsecureName
			service3Name = webServerDeploy3.SvcUnsecureName
		)

		compat_otp.By("Create three server pods and three unsecure services")
		ns := oc.Namespace()
		webServerDeploy1.Namespace = ns
		webServerDeploy1.Create(oc)
		webServerDeploy2.Namespace = ns
		webServerDeploy2.Create(oc)
		webServerDeploy3.Namespace = ns
		webServerDeploy3.Create(oc)
		util.EnsurePodWithLabelReady(oc, ns, srv1Label)
		util.EnsurePodWithLabelReady(oc, ns, srv2Label)
		util.EnsurePodWithLabelReady(oc, ns, srv3Label)

		compat_otp.By("Expose a route with the unsecure service inside the project")
		output, err := oc.Run("expose").Args("service", service1Name).Output()
		o.Expect(err).NotTo(o.HaveOccurred(), "OCP-67093: Expose a route with the unsecure service inside the project")
		o.Expect(output).To(o.ContainSubstring(service1Name), "OCP-67093: Expose a route with the unsecure service inside the project")

		compat_otp.By("Check the default weights for the selected routes are 1")
		routerPod := util.GetOneRouterPodNameByIC(oc, "default")
		srvPod1Name := util.GetOnePodNameByLabel(oc, ns, srv1Label)
		srvPod2Name := util.GetOnePodNameByLabel(oc, ns, srv2Label)
		srvPod3Name := util.GetOnePodNameByLabel(oc, ns, srv3Label)
		util.EnsurePodWithLabelReady(oc, "openshift-ingress-canary", `ingresscanary.operator.openshift.io/daemonset-ingresscanary=canary_controller`)
		isSelectedServerLine := func(line string) bool {
			return strings.Contains(line, "server pod:ingress-canary") ||
				strings.Contains(line, "server pod:"+srvPod1Name) ||
				strings.Contains(line, "server pod:"+srvPod2Name) ||
				strings.Contains(line, "server pod:"+srvPod3Name)
		}
		// The router writes the new backend only after the route is admitted and the
		// config is regenerated, so poll instead of reading the config once.
		var serverConfig string
		waitErr := wait.Poll(2*time.Second, time.Minute, func() (bool, error) {
			serverConfig = util.ReadRouterPodData(oc, routerPod, "cat haproxy.config", "server pod:")
			found := 0
			for _, line := range strings.Split(serverConfig, "\n") {
				if isSelectedServerLine(line) {
					found++
				}
			}
			return found >= 4, nil
		})
		compat_otp.AssertWaitPollNoErr(waitErr, "OCP-67093: expected the four backend server lines in haproxy.config")

		// Match the whole weight field: a substring match would also accept weight 10
		// and weight 128.
		weightOne := regexp.MustCompile(`\bweight 1(\s|$)`)
		selectedLines := 0
		weight1Lines := 0
		for _, line := range strings.Split(serverConfig, "\n") {
			if !isSelectedServerLine(line) {
				continue
			}
			selectedLines++
			if weightOne.MatchString(line) {
				weight1Lines++
			}
		}
		o.Expect(selectedLines).To(o.Equal(weight1Lines), "OCP-67093: Check the default weights for the selected routes are 1")

		compat_otp.By("Patch the route with alternate backends and weights")
		patchRrAlBackend := `{"metadata":{"annotations":{"haproxy.router.openshift.io/balance":"roundrobin"}},"spec":{"to":{"kind":"Service","name":"` + service1Name + `","weight":20},"alternateBackends":[{"kind":"Service","name":"` + service2Name + `","weight":10},{"kind":"Service","name":"` + service3Name + `","weight":10}]}}`
		util.PatchResourceAsAdmin(oc, ns, "route/"+service1Name, patchRrAlBackend)

		compat_otp.By("Check the route's backend config")
		backend := "be_http:" + ns + ":" + service1Name
		bk1 := util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backend, []string{"server pod:" + srvPod1Name + ".+weight 256"})
		o.Expect(strings.Count(bk1, "weight 256")).To(o.BeNumerically(">=", 1), "OCP-67093: Check the route's backend config")
		bk2 := util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backend, []string{"server pod:" + srvPod2Name + ".+weight 128"})
		o.Expect(strings.Count(bk2, "weight 128")).To(o.BeNumerically(">=", 1), "OCP-67093: Check the route's backend config")
		bk3 := util.EnsureHaproxyBlockConfigMatchRegexp(oc, routerPod, backend, []string{"server pod:" + srvPod3Name + ".+weight 128"})
		o.Expect(strings.Count(bk3, "weight 128")).To(o.BeNumerically(">=", 1), "OCP-67093: Check the route's backend config")
	})
})
