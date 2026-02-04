//go:build windows

package cli

import (
	"os"
)

// shutdownSignals are the OS signals that trigger a graceful shutdown.
// Windows does not support SIGTERM; os.Interrupt maps to Ctrl+C / CTRL_BREAK_EVENT.
var shutdownSignals = []os.Signal{os.Interrupt}
