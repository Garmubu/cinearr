//go:build !windows

// Command cinearr-update is the Windows updater (update.exe). On macOS and
// Linux, see packaging/linux/update.sh and the user guide.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "update.exe is the Windows updater; on Linux use update.sh, on macOS download the new version from the releases page")
	os.Exit(2)
}
