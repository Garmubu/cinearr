//go:build !windows

package desktop

import "os/exec"

func hide(cmd *exec.Cmd) {}
