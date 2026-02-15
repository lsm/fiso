//go:build wasmer

package main

import (
	"fmt"
	"os"
)

// NOTE: This binary combines fiso-link with Wasmer app support.
// It runs a link proxy with embedded Wasmer apps for request processing.
//
// Configuration example:
// link:
//   listenAddr: ":3500"
//   targets:
//     - name: backend
//       host: localhost:8080
// wasmer:
//   apps:
//     - name: transformer
//       module: /path/to/transform.wasm

func main() {
	fmt.Fprintln(os.Stderr, "fiso-wasmer-link: not yet implemented")
	fmt.Fprintln(os.Stderr, "This binary will combine fiso-link with embedded Wasmer apps.")
	fmt.Fprintln(os.Stderr, "For now, use fiso-link with wasm interceptors.")
	os.Exit(1)
}
