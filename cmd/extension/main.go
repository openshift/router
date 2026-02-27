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

	// Import testdata package from same module
	_ "github.com/openshift/router/test/e2e/testdata"

	// Import test packages from same module
	_ "github.com/openshift/router/test/e2e"
)

func main() {
	// Initialize test framework flags (required for kubeconfig, provider, etc.)
	util.InitStandardFlags()
	framework.AfterReadingAllFlags(&framework.TestContext)

	logs.InitLogs()
	defer logs.FlushLogs()

	registry := e.NewRegistry()
	ext := e.NewExtension("openshift", "payload", "router")

	ext.AddSuite(e.Suite{
		Name:    "openshift/router/tests",
		Parents: []string{"openshift/conformance/parallel"},
	})

	// Build test specs from Ginkgo
	// Note: ModuleTestsOnly() is applied by default, which filters out /vendor/ and k8s.io/kubernetes tests
	allSpecs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		panic(fmt.Sprintf("couldn't build extension test specs from ginkgo: %+v", err.Error()))
	}

	// Filter to only include tests from this module's test directory
	// Excludes tests from /go/pkg/mod/ (module cache) and /vendor/
	componentSpecs := allSpecs.Select(func(spec *et.ExtensionTestSpec) bool {
		for _, loc := range spec.CodeLocations {
			// Include tests from local test directory (not from module cache or vendor)
			if strings.Contains(loc, "/test/e2e/") && !strings.Contains(loc, "/go/pkg/mod/") && !strings.Contains(loc, "/vendor/") {
				return true
			}
		}
		return false
	})

	// Initialize test framework before all tests
	componentSpecs.AddBeforeAll(func() {
		if err := compat_otp.InitTest(false); err != nil {
			panic(err)
		}
	})

	// Process all specs
	componentSpecs.Walk(func(spec *et.ExtensionTestSpec) {
		// Apply platform filters based on Platform: labels
		for label := range spec.Labels {
			if strings.HasPrefix(label, "Platform:") {
				platformName := strings.TrimPrefix(label, "Platform:")
				spec.Include(et.PlatformEquals(platformName))
			}
		}

		// Apply platform filters based on [platform:xxx] in test names
		re := regexp.MustCompile(`\[platform:([a-z]+)\]`)
		if match := re.FindStringSubmatch(spec.Name); match != nil {
			platform := match[1]
			spec.Include(et.PlatformEquals(platform))
		}

		// Set lifecycle to Informing
		spec.Lifecycle = et.LifecycleInforming
	})

	// Add filtered component specs to extension
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
