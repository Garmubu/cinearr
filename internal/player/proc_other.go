//go:build !windows

package player

import "os/exec"

func hideConsole(cmd *exec.Cmd) {}
