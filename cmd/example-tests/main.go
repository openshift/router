package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	"github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"
	_ "github.com/openshift/router/test/example"

	exutil "github.com/openshift/router/test/util"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

func main() {
	registry := extension.NewRegistry()
	ext := extension.NewExtension("openshift", "payload", "openshit-router")

	ext.AddSuite(extension.Suite{
		Name: "openshit-router",
	})

	specs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		panic(fmt.Sprintf("couldn't build extension test specs from ginkgo: %+v", err.Error()))
	}

	ext.AddSpecs(specs)
	registry.Register(ext)

	root := &cobra.Command{
		Long: "OpenShift Tests Extension for Cluster Version Operator",
	}

	exutil.InitStandardFlags()
	specs.AddBeforeAll(func() {
		if err := exutil.InitTest(false); err != nil {
			panic(err)
		}
		e2e.AfterReadingAllFlags(exutil.TestContext)
		e2e.TestContext.DumpLogsOnFailure = true
		exutil.TestContext.DumpLogsOnFailure = true
	})

	root.AddCommand(cmd.DefaultExtensionCommands(registry)...)

	if err := func() error {
		return root.Execute()
	}(); err != nil {
		os.Exit(1)
	}
}
