package log

import (
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

// Logger is the root logger which should be used by all
// other packages in the codebase.
var Logger logr.Logger

func init() {
	// Set up logging.
	zapLogger, err := zap.NewDevelopment(zap.AddCallerSkip(1), zap.AddStacktrace(zap.FatalLevel))
	if err != nil {
		panic(err)
	}
	Logger = zapr.NewLogger(zapLogger).WithName("router")
}
