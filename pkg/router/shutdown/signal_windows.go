package shutdown

import (
	"os"
)

var shutdownSignals = []os.Signal{os.Interrupt}
