package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/component-base/logs"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	et "github.com/openshift-eng/openshift-tests-extension/pkg/extension/extensiontests"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	"github.com/openshift/origin/test/extended/util"
	compat_otp "github.com/openshift/origin/test/extended/util/compat_otp"
	framework "k8s.io/kubernetes/test/e2e/framework"

	// Import test packages from this module
	_ "github.com/openshift/router-tests-extension/test/qe"
)

func main() {
	// Ginkgo child processes are started simultaneously. The e2e namespace
	// generator seeds its random source from the process clock, so parallel
	// children can generate identical namespaces and fail in BeforeEach with
	// "user already exists". Mix in the PID to give every child a distinct
	// random sequence.
	utilrand.Seed(time.Now().UnixNano() ^ int64(os.Getpid()))

	util.InitStandardFlags()
	if framework.TestContext.Provider == "" {
		framework.TestContext.Provider = "skeleton"
	}
	framework.AfterReadingAllFlags(&framework.TestContext)

	logs.InitLogs()
	defer logs.FlushLogs()

	registry := e.NewRegistry()
	ext := e.NewExtension("openshift", "payload", "router")

	registerSuites(ext)

	allSpecs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		panic(fmt.Sprintf("couldn't build extension test specs from ginkgo: %+v", err.Error()))
	}

	componentSpecs := allSpecs.Select(func(spec *et.ExtensionTestSpec) bool {
		for _, loc := range spec.CodeLocations {
			if strings.Contains(loc, "/test/qe/") && !strings.Contains(loc, "/go/pkg/mod/") && !strings.Contains(loc, "/vendor/") {
				return true
			}
		}
		return false
	})

	componentSpecs.AddBeforeAll(func() {
		if err := compat_otp.InitTest(false); err != nil {
			panic(err)
		}
		util.WithCleanup(func() {})
	})

	componentSpecs.Walk(func(spec *et.ExtensionTestSpec) {
		for label := range spec.Labels {
			if strings.HasPrefix(label, "Platform:") {
				platformName := strings.TrimPrefix(label, "Platform:")
				spec.Include(et.PlatformEquals(platformName))
			}
		}

		re := regexp.MustCompile(`\[platform:([a-z]+)\]`)
		if match := re.FindStringSubmatch(spec.Name); match != nil {
			platform := match[1]
			spec.Include(et.PlatformEquals(platform))
		}

		// Record the pre-[Serial] name for hostNetwork cases that were renamed
		// in this branch so that metadata regeneration treats them as renames.
		if originalName, ok := renamedTests[spec.Name]; ok && spec.OriginalName == "" {
			spec.OriginalName = originalName
		}

		spec.Lifecycle = et.LifecycleInforming

		// Enforce serial execution for [Disruptive] and [Serial] tests,
		// matching openshift-tests-private behavior where these tests
		// never run concurrently with other tests.
		if strings.Contains(spec.Name, "[Disruptive]") || strings.Contains(spec.Name, "[Serial]") {
			spec.Resources.Isolation.Taint = append(spec.Resources.Isolation.Taint, "disruptive")
			spec.Resources.Isolation.Conflict = append(spec.Resources.Isolation.Conflict, "disruptive")
		} else {
			spec.Resources.Isolation.Taint = append(spec.Resources.Isolation.Taint, "parallel-running")
			spec.Resources.Isolation.Toleration = append(spec.Resources.Isolation.Toleration, "parallel-running")
		}
	})

	normalizeCodeLocations(componentSpecs)
	applyEnvironmentSelectors(componentSpecs)

	// Reorder specs: parallel tests first, then disruptive/serial tests.
	// This ensures the scheduler dispatches all parallel tests before
	// attempting any serial tests when running the router/all suite.
	parallelSpecs := componentSpecs.Select(func(spec *et.ExtensionTestSpec) bool {
		return !strings.Contains(spec.Name, "[Disruptive]") && !strings.Contains(spec.Name, "[Serial]")
	})
	serialSpecs := componentSpecs.Select(func(spec *et.ExtensionTestSpec) bool {
		return strings.Contains(spec.Name, "[Disruptive]") || strings.Contains(spec.Name, "[Serial]")
	})
	componentSpecs = append(parallelSpecs, serialSpecs...)

	ext.AddSpecs(componentSpecs)

	ext.IgnoreObsoleteTests(removedHAProxyTests...)

	registry.Register(ext)

	root := &cobra.Command{
		Long: "Router Tests",
	}

	root.AddCommand(cmd.DefaultExtensionCommands(registry)...)

	if err := func() error {
		return root.Execute()
	}(); err != nil {
		os.Exit(1)
	}
}

const (
	// clusterConfigAPIGroup is the API group that serves dnses.config.openshift.io,
	// the resource from which every Route test reads the cluster's base domain.
	clusterConfigAPIGroup = "config.openshift.io"

	// hyperShiftTopology is the value a HyperShift hosted cluster reports for the
	// --topology environment flag, because its control plane is external to the
	// cluster the tests run against.
	hyperShiftTopology = "External"
)

// hyperShiftUnsupportedTests lists the stable OCP case labels of tests that carried the
// NonHyperShiftHOST token in openshift-tests-private.  Those tests inspect
// resources that live on the management cluster, which a hosted cluster does not
// expose.  A token in a test name is not an executable skip, so the constraint is
// applied as an environment selector instead.
var hyperShiftUnsupportedTests = []string{
	"OCP-11036",
	"OCP-13753",
	"OCP-13839",
	"OCP-14678",
	"OCP-17145",
	"OCP-55895",
	"OCP-56240",
}

// renamedTests maps the current name of a test to the very first name it was
// registered under, so that metadata regeneration records the rename.  The
// HAProxy cases were originally registered under the Route component prefix;
// they now use Component_Haproxy to distinguish them from the Route cases.
var renamedTests = map[string]string{
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Configmap mounted on router volume after ingresscontroller has spec field HttpErrorCodePage populated with configmap name": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Configmap mounted on router volume after ingresscontroller has spec field HttpErrorCodePage populated with configmap name",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Errorfile stanzas and dummy default html files have been added to the router": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Errorfile stanzas and dummy default html files have been added to the router",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy HAProxyDown message only for pods and No reaper messages for zombie processes": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router HAProxyDown message only for pods and No reaper messages for zombie processes",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Health check when there are multi service and each service has one backend": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Health check when there are multi service and each service has one backend",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Idling StatefulSet is not supported": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Idling StatefulSet is not supported",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Ingress controller stats port is not set according to endpointPublishingStrategy": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Ingress controller stats port is not set according to endpointPublishingStrategy",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy IngressController option to use PROXY protocol with IBM Cloud load-balancers - TCP, PROXY and omitted": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router IngressController option to use PROXY protocol with IBM Cloud load-balancers - TCP, PROXY and omitted",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Ingresscontroller routes traffic only to ready pods/backends": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Ingresscontroller routes traffic only to ready pods/backends",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Ingresscontroller with private endpoint publishing strategy supports PROXY protocol": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Ingresscontroller with private endpoint publishing strategy supports PROXY protocol",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy No health check when there is only one endpoint for a route": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router No health check when there is only one endpoint for a route",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Power-of-two feature allows unsupportedConfigOverrides ingress operator option to enable leastconn balancing algorithm": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Power-of-two feature allows unsupportedConfigOverrides ingress operator option to enable leastconn balancing algorithm",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Routers with hostnetwork endpoint strategy with same http/https/stat port numbers cannot be deployed on the same worker node [Serial]": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Routers with hostnetwork endpoint strategy with same http/https/stat port numbers cannot be deployed on the same worker node",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Set Cookie2 by an application in a route should not kill all router pods": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Set Cookie2 by an application in a route should not kill all router pods",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy Setting ROUTER_ENABLE_COMPRESSION and ROUTER_COMPRESSION_MIME in HAProxy": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Setting ROUTER_ENABLE_COMPRESSION and ROUTER_COMPRESSION_MIME in HAProxy",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The Power of two balancing  honours the per route balancing algorithm defined via haproxy.router.openshift.io/balance annotation": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The Power of two balancing  honours the per route balancing algorithm defined via haproxy.router.openshift.io/balance annotation",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The Power-of-two balancing features defaults to random LB algorithm instead of leastconn for REEN/Edge/insecure routes": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The Power-of-two balancing features defaults to random LB algorithm instead of leastconn for REEN/Edge/insecure routes",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The Power-of-two balancing features switches to roundrobin mode for REEN/Edge/insecure/passthrough routes with multiple backends configured with weights": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The Power-of-two balancing features switches to roundrobin mode for REEN/Edge/insecure/passthrough routes with multiple backends configured with weights",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The Power-of-two balancing features switches to source algorithm for passthrough routes": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The Power-of-two balancing features switches to source algorithm for passthrough routes",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The backend health check interval of unsecure/edge/passthrough/reencrypt route can be set by annotation": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The backend health check interval of unsecure/edge/passthrough/reencrypt route can be set by annotation",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The endpointPublishingStrategy parameter allows TCP, PROXY, and empty definitions for HostNetwork [Serial]": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The endpointPublishingStrategy parameter allows TCP, PROXY, and empty definitions for HostNetwork",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The logEmptyRequests ingresscontroller parameter set to Ignore add the dontlognull option in the haproxy configuration": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The logEmptyRequests ingresscontroller parameter set to Ignore add the dontlognull option in the haproxy configuration",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy The logEmptyRequests option only gets applied when the access logging is configured for the ingresscontroller": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The logEmptyRequests option only gets applied when the access logging is configured for the ingresscontroller",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy User can customize HAProxy 2.0 Error Page": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router User can customize HAProxy 2.0 Error Page",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy User can delete configmap and update configmap with new custom error page": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router User can delete configmap and update configmap with new custom error page",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy httpEmptyRequestsPolicy ingresscontroller parameter set to ignore adds the http-ignore-probes option in the haproxy configuration": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router httpEmptyRequestsPolicy ingresscontroller parameter set to ignore adds the http-ignore-probes option in the haproxy configuration",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy misc tests for haproxy router": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router misc tests for haproxy router",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy router continues to function normally with the service selector of exposed route gets removed/deleted": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router router continues to function normally with the service selector of exposed route gets removed/deleted",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy router support HTTP2 for passthrough route and reencrypt route with custom certs": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router router support HTTP2 for passthrough route and reencrypt route with custom certs",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Haproxy the 503 Error page should not contain license for a vulnerable release of Bootstrap": "[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router the 503 Error page should not contain license for a vulnerable release of Bootstrap",
}

// removedHAProxyTests lists the previously registered names of HAProxy cases
// that were dropped from test/qe/haproxy.go.  Listing them here lets
// "make update-metadata" retire the stale entries.
var removedHAProxyTests = []string{
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Ingresscontroller with endpointPublishingStrategy of hostNetwork allows PROXY protocol for source forwarding",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Ingresscontroller with endpointPublishingStrategy of nodePort allows PROXY protocol for source forwarding",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Setting wrong data in ROUTER_ENABLE_COMPRESSION and ROUTER_COMPRESSION_MIME in HAProxy",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router check haproxy router v4v6 mode",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router check haproxy's version and router base image",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Allow Ingress to be modified on the settings of livenessProbe and readinessProbe",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Negative test of allow Ingress to be modified on the settings of livenessProbe and readinessProbe",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Multiple routers with hostnetwork endpoint strategy can be deployed on same worker node with different http/https/stat port numbers",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router The http/https/stat port field in the ingresscontroller does not accept negative values during configuration",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Accessing the route should wake up the idled resources",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router edge terminated h2 (gRPC) connections need a haproxy template change to work correctly",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router option idle-close-on-response is configurable",
	"[sig-network-edge][Feature:Router][OTP][Level0] Network_Edge Component_Router Support Prometheus monitoring metric for router max connections",
}

// moduleDirSegment is the path segment that ends at this module's root.  It is
// used to trim the build directory out of the source paths Ginkgo records.
const moduleDirSegment = "/tests-extension/"

// normalizeCodeLocations rewrites the source paths Ginkgo records for each spec
// so that they are relative to this module's root.  Ginkgo captures absolute
// paths, which differ between a developer's checkout and the CI build image;
// leaving them absolute would make .openshift-tests-extension metadata depend on
// where the binary happened to be compiled, so it could never be verified.
//
// This must run after the specs have been filtered by code location, since that
// filter matches on the absolute form.
func normalizeCodeLocations(specs et.ExtensionTestSpecs) {
	specs.Walk(func(spec *et.ExtensionTestSpec) {
		for i, location := range spec.CodeLocations {
			if idx := strings.LastIndex(location, moduleDirSegment); idx != -1 {
				spec.CodeLocations[i] = location[idx+len(moduleDirSegment):]
			}
		}
	})
}

// applyEnvironmentSelectors restricts the environments in which the Route tests
// run.  openshift-tests evaluates these CEL expressions when it lists the
// extension's tests, so an excluded test is never scheduled and is reported as a
// clean skip instead of failing in setup.  See OCPBUGS-132070 for the CI failures
// these exclusions address.
func applyEnvironmentSelectors(specs et.ExtensionTestSpecs) {
	// Every Route test resolves the cluster's base domain from dns.config/cluster
	// and relies on the ingress operator to admit routes.  MicroShift serves
	// neither, so without this gate every test fails during setup on the MicroShift
	// periodic job with "failed to get base domain ... the server could not find
	// the requested resource".
	//
	// Gate on the API group rather than on the platform.  Excluding
	// PlatformEquals("none") looks like the obvious way to skip MicroShift, but
	// "none" is not unique to it: openshift-tests derives the platform name from
	// ClusterConfiguration.ProviderName, which origin only sets for vSphere, AWS,
	// GCP, Azure and IBM Cloud and otherwise defaults to "skeleton", which is in
	// turn reported as "none".  Bare metal, OpenStack and Nutanix clusters
	// therefore report platform "none" as well, so excluding it would silently
	// disable these tests on supported platforms. Keep platform selection separate
	// from the temporary fixture-image restriction below.
	//
	// openshift-tests only passes --api-group when it can discover cluster state,
	// which it cannot do on MicroShift, so the group is absent there and present on
	// every real cluster.
	specs.Include(et.APIGroupEnabled(clusterConfigAPIGroup))

	// Fixture setup imports images from quay.io, which disconnected clusters
	// cannot reach, regardless of their network stack. Connected and proxied
	// IPv6-only clusters remain eligible. Remove this exclusion once fixture setup
	// uses the configured mirror and has been validated without external access.
	// See the re-enable checklist in test/qe/README.md.
	specs.Exclude(et.ExternalConnectivityEquals("Disconnected"))

	selectHyperShiftUnsupported := make([]et.SelectFunction, 0, len(hyperShiftUnsupportedTests))
	for _, caseID := range hyperShiftUnsupportedTests {
		selectHyperShiftUnsupported = append(selectHyperShiftUnsupported, et.HasLabel(caseID))
	}
	excluded := specs.SelectAny(selectHyperShiftUnsupported)
	if len(excluded) != len(hyperShiftUnsupportedTests) {
		panic(fmt.Sprintf("expected %d tests unsupported on HyperShift but matched %d; "+
			"hyperShiftUnsupportedTests is out of sync with test/qe/route.go",
			len(hyperShiftUnsupportedTests), len(excluded)))
	}
	excluded.Exclude(et.TopologyEquals(hyperShiftTopology))

	// This test creates worker and infra Machines, which single-node clusters
	// cannot provide even when the Machine API is available.
	specs.Select(et.HasLabel("OCP-56240")).Exclude(et.TopologyEquals("SingleReplica"))
}

// registerSuites adds router suites and their payload conformance parents.
func registerSuites(ext *e.Extension) {
	suites := []e.Suite{
		{
			Name: "router/conformance/parallel",
			Parents: []string{
				"openshift/conformance/parallel",
			},
			Description: "Parallel conformance tests (Level0, non-serial, non-disruptive)",
			Qualifiers: []string{
				`name.contains("[Level0]") && !(name.contains("[Serial]") || name.contains("[Disruptive]"))`,
			},
		},
		{
			Name: "router/conformance/serial",
			Parents: []string{
				"openshift/conformance/serial",
			},
			Description: "Non-disruptive serial conformance tests (must run sequentially)",
			Qualifiers: []string{
				`name.contains("[Level0]") && name.contains("[Serial]") && !name.contains("[Disruptive]")`,
			},
		},
		{
			// These tests intentionally disrupt platform routes or Machines and
			// must not enter conformance jobs whose monitors require availability.
			// Run this suite explicitly in a lane that supports those disruptions;
			// it has no conformance parent. Isolation still applies without [Serial].
			Name:        "router/disruptive",
			Description: "Disruptive tests (may affect cluster state)",
			Qualifiers: []string{
				`name.contains("[Disruptive]")`,
			},
		},
		{
			Name:        "router/non-disruptive",
			Description: "All non-disruptive tests (safe for development clusters)",
			Qualifiers: []string{
				`!name.contains("[Disruptive]")`,
			},
		},
		{
			Name:        "router/all",
			Description: "All router tests",
		},
	}

	for _, suite := range suites {
		ext.AddSuite(suite)
	}
}
