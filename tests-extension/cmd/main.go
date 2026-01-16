package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	et "github.com/openshift-eng/openshift-tests-extension/pkg/extension/extensiontests"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"

	// Import test framework packages for initialization
	"github.com/openshift/origin/test/extended/util"
	"k8s.io/kubernetes/test/e2e/framework"

	// Import test packages
	_ "github.com/openshift/router-tests-extension/test/e2e"
)

func main() {
	// Initialize test framework
	// This sets TestContext.KubeConfig from KUBECONFIG env var and initializes the cloud provider
	util.InitStandardFlags()
	if err := util.InitTest(false); err != nil {
		panic(fmt.Sprintf("couldn't initialize test framework: %+v", err.Error()))
	}
	framework.AfterReadingAllFlags(&framework.TestContext)

	registry := e.NewRegistry()
	ext := e.NewExtension("openshift", "payload", "router")

	// Add main test suite
	ext.AddSuite(e.Suite{
		Name:    "openshift/router/tests",
		Parents: []string{"openshift/conformance/parallel"},
	})

	// Build test specs from Ginkgo
	allSpecs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		panic(fmt.Sprintf("couldn't build extension test specs from ginkgo: %+v", err.Error()))
	}

	// IMPORTANT: Filter out upstream Kubernetes tests
	// Only keep tests that have "Author:" in the name (component-specific tests)
	// This excludes upstream K8s feature tests like [Feature:BootstrapTokens], [Feature:DRA], etc.
	var filteredSpecs []*et.ExtensionTestSpec
	allSpecs.Walk(func(spec *et.ExtensionTestSpec) {
		if strings.Contains(spec.Name, "Author:") {
			filteredSpecs = append(filteredSpecs, spec)
		}
	})

	// Create new specs collection from filtered list
	specs := et.ExtensionTestSpecs(filteredSpecs)

	// Apply platform filters based on Platform: labels
	specs.Walk(func(spec *et.ExtensionTestSpec) {
		for label := range spec.Labels {
			if strings.HasPrefix(label, "Platform:") {
				platformName := strings.TrimPrefix(label, "Platform:")
				spec.Include(et.PlatformEquals(platformName))
			}
		}
	})

	// Apply platform filters based on [platform:xxx] in test names
	specs.Walk(func(spec *et.ExtensionTestSpec) {
		re := regexp.MustCompile(`\[platform:([a-z]+)\]`)
		if match := re.FindStringSubmatch(spec.Name); match != nil {
			platform := match[1]
			spec.Include(et.PlatformEquals(platform))
		}
	})

	// Wrap test execution with cleanup handler
	// This marks tests as started and ensures proper cleanup
	specs.Walk(func(spec *et.ExtensionTestSpec) {
		originalRun := spec.Run
		spec.Run = func(ctx context.Context) *et.ExtensionTestResult {
			var result *et.ExtensionTestResult
			util.WithCleanup(func() {
				result = originalRun(ctx)
			})
			return result
		}
	})

	ext.AddSpecs(specs)
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
