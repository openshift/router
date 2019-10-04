package main

import (
	"fmt"
	"math/rand"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/component-base/logs"

	"k8s.io/client-go/pkg/version"

	"github.com/openshift/library-go/pkg/serviceability"

	"github.com/openshift/router/pkg/cmd/infra/router"
)

func main() {
	defer logs.FlushLogs()

	defer serviceability.BehaviorOnPanic(os.Getenv("OPENSHIFT_ON_PANIC"), version.Get())()
	defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()
	rand.Seed(time.Now().UTC().UnixNano())

	cmd := CommandFor(filepath.Base(os.Args[0]))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// CommandFor returns the appropriate command for this base name,
// or the OpenShift CLI command.
func CommandFor(basename string) *cobra.Command {
	var cmd *cobra.Command

	switch basename {
	case "openshift-router", "openshift-haproxy-router":
		cmd = router.NewCommandTemplateRouter(basename)
	default:
		fmt.Printf("unknown command name: %s\n", basename)
		os.Exit(1)
	}

	GLog(cmd.PersistentFlags())

	return cmd
}
