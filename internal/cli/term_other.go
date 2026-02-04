//go:build !windows

package cli

// enableANSI is a no-op on non-Windows platforms where ANSI is natively supported.
func enableANSI() {}
