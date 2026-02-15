//go:build wasmer

package main

import (
	"fmt"
	"os"
)

// NOTE: This is the all-in-one binary combining:
// - fiso-flow: Event streaming pipeline
// - wasmer: Full WASIX app runtime
// - fiso-link: HTTP proxy with interceptors
//
// Configuration example:
// flow:
//   configDir: /etc/fiso/flows
// link:
//   listenAddr: ":3500"
// wasmer:
//   apps:
//     - name: processor
//       module: /etc/fiso/wasm/processor.wasm
//       execution: longRunning
//       port: 8090

func main() {
	fmt.Fprintln(os.Stderr, "fiso-wasmer-aio: not yet implemented")
	fmt.Fprintln(os.Stderr, "This binary will combine fiso-flow + wasmer + fiso-link.")
	fmt.Fprintln(os.Stderr, "Deployment modes:")
	fmt.Fprintln(os.Stderr, "  1. Distributed: fiso-flow + wasmer + fiso-link as separate services")
	fmt.Fprintln(os.Stderr, "  2. Flow+Wasmer: fiso-flow-wasmer")
	fmt.Fprintln(os.Stderr, "  3. Wasmer+Link: fiso-wasmer-link")
	fmt.Fprintln(os.Stderr, "  4. All-in-one: fiso-wasmer-aio (this binary)")
	os.Exit(1)
}
