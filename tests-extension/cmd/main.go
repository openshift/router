package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	et "github.com/openshift-eng/openshift-tests-extension/pkg/extension/extensiontests"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"

	// Import testdata package
	"github.com/openshift/router-tests-extension/test/testdata"

	// Import test packages
	_ "github.com/openshift/router-tests-extension/test/e2e"
)

func main() {
	registry := e.NewRegistry()
	ext := e.NewExtension("openshift", "payload", "router")

	// Add main test suite
	ext.AddSuite(e.Suite{
		Name:    "openshift/router/tests",
		Parents: []string{"openshift/conformance/parallel"},
	})

	// Build test specs from Ginkgo
	specs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		panic(fmt.Sprintf("couldn't build extension test specs from ginkgo: %+v", err.Error()))
	}

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

	// Add testdata validation and cleanup hooks
	specs.AddBeforeAll(func() {
		// List available fixtures
		fixtures := testdata.ListFixtures()
		fmt.Printf("Loaded %d test fixtures\n", len(fixtures))

		// Optional: Validate required fixtures
		// requiredFixtures := []string{
		//     "manifests/deployment.yaml",
		// }
		// if err := testdata.ValidateFixtures(requiredFixtures); err != nil {
		//     panic(fmt.Sprintf("Missing required fixtures: %v", err))
		// }
	})

	specs.AddAfterAll(func() {
		if err := testdata.CleanupFixtures(); err != nil {
			fmt.Printf("Warning: failed to cleanup fixtures: %v\n", err)
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
