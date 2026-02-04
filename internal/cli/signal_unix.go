//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// shutdownSignals are the OS signals that trigger a graceful shutdown.
var shutdownSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
