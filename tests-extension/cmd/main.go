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

	// Import testdata package from this module
	_ "github.com/openshift/router-tests-extension/test/e2e/testdata"

	// Import test packages from this module
	_ "github.com/openshift/router-tests-extension/test/e2e"
)

func main() {
	util.InitStandardFlags()
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
			if strings.Contains(loc, "/test/e2e/") && !strings.Contains(loc, "/go/pkg/mod/") && !strings.Contains(loc, "/vendor/") {
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
			Description: "Serial conformance tests (must run sequentially)",
			Qualifiers: []string{
				`name.contains("[Level0]") && name.contains("[Serial]") && !name.contains("[Disruptive]")`,
			},
		},
		{
			Name:        "router/disruptive",
			Parents:     []string{"openshift/disruptive"},
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
