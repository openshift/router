package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
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
