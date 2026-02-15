//go:build wasmer

package main

import (
	"fmt"
	"os"
)

// NOTE: This binary is fiso-flow compiled with Wasmer support.
// The implementation delegates to the main fiso-flow code but
// includes the wasmer runtime for wasmer-app interceptors.
//
// Configuration example:
// interceptors:
//   - type: wasmer-app
//     config:
//       module: /path/to/app.wasm
//       execution: longRunning

func main() {
	fmt.Fprintln(os.Stderr, "fiso-flow-wasmer: not yet implemented")
	fmt.Fprintln(os.Stderr, "This binary will be fiso-flow with embedded Wasmer support.")
	fmt.Fprintln(os.Stderr, "For now, use fiso-flow with runtime: wasmer in interceptor config.")
	os.Exit(1)
}
