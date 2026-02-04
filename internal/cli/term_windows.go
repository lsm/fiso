//go:build windows

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

const enableVirtualTerminalProcessing = 0x0004

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode = kernel32.NewProc("GetConsoleMode")
	setConsoleMode = kernel32.NewProc("SetConsoleMode")
)

// enableANSI enables ANSI escape code processing on the Windows console.
// This allows colors, cursor hiding, and line clearing to work in cmd.exe
// and older PowerShell hosts. Silently ignored on failure.
func enableANSI() {
	fd := os.Stdout.Fd()
	var mode uint32
	r, _, _ := getConsoleMode.Call(fd, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return
	}
	_, _, _ = setConsoleMode.Call(fd, uintptr(mode|enableVirtualTerminalProcessing))
}
