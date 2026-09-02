package main

import (
	"fmt"
	"os"
)

// Test module for the guest-stderr contract: it writes a distinctive
// diagnostic to stderr and exits non-zero, so host-side tests can assert
// that a failing guest's stderr reaches the operator instead of being
// discarded.
func main() {
	fmt.Fprintln(os.Stderr, "auth-config-boom: no verification key configured")
	os.Exit(1)
}
