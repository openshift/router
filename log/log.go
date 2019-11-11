package log

import (
	"github.com/go-logr/logr"

	"k8s.io/klog/klogr"
)

// Logger is the root logger which should be used by all
// other packages in the codebase.
var Logger logr.Logger

func init() {
	Logger = klogr.New()
}
