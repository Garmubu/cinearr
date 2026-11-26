//go:build !windows

// Command cinearr-uninstall is the Windows uninstaller (uninstall.exe). On
// Linux, see packaging/linux/uninstall.sh; on macOS, move Cinearr to the
// Trash (the user guide lists the data folder).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "uninstall.exe is the Windows uninstaller; on Linux use uninstall.sh, on macOS move Cinearr.app to the Trash")
	os.Exit(2)
}
