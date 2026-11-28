//go:build windows

package desktop

import (
	"os/exec"
	"testing"
	"time"
)

// The window of a started program is found by its process and handed the
// foreground (or at least raised): the path used for the players.
func TestFrontWindowOfProcess(t *testing.T) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.Form; $f.Text = 'cinearr-test'; $f.Show(); [System.Windows.Forms.Application]::DoEvents(); Start-Sleep -Seconds 15`)
	if err := cmd.Start(); err != nil {
		t.Skipf("powershell not available: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	var h uintptr
	for end := time.Now().Add(12 * time.Second); time.Now().Before(end) && h == 0; time.Sleep(200 * time.Millisecond) {
		h = mainWindow(uint32(cmd.Process.Pid))
	}
	if h == 0 {
		t.Skip("no window shown here")
	}
	if c := windowClass(h); c == "" {
		t.Errorf("window without class")
	}
	t.Logf("foreground: %v", foreground(h))
	if !windowsOfClass(windowClass(h))[h] {
		t.Error("window not listed by its class")
	}
}
