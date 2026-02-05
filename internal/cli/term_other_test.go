//go:build !windows

package cli

import "testing"

func TestEnableANSI(t *testing.T) {
	enableANSI() // no-op on non-Windows, should not panic
}
