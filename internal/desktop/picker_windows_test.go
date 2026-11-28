//go:build windows

package desktop

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The PowerShell scripts of the dialogs parse without error (a title with
// a quote included); they are not run, a dialog would wait for the user.
func TestWindowsPickScript(t *testing.T) {
	for _, dir := range []bool{true, false} {
		script := windowsPickScript(dir, "Choose the player's folder")
		if !strings.Contains(script, "ShowDialog($o)") || !strings.Contains(script, "TopMost = $true") {
			t.Fatalf("no owner kept in front: %s", script)
		}
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			`$e = $null; [void][System.Management.Automation.Language.Parser]::ParseInput($env:PICK_SCRIPT, [ref]$null, [ref]$e); [Console]::Out.Write($e.Count)`)
		cmd.Env = append(os.Environ(), "PICK_SCRIPT="+script)
		out, err := cmd.Output()
		if err != nil {
			t.Skipf("powershell not available: %v", err)
		}
		if strings.TrimSpace(string(out)) != "0" {
			t.Fatalf("script (dir %v) has %s parse errors: %s", dir, out, script)
		}
	}
}
