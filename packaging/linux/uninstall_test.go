// Package linux holds the tests of the Linux install scripts.
package linux

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// uninstall.sh deletes the program and, when asked, the data folder of
// Cinearr (with the players it installed), and nothing else: films,
// series, downloads and other programs stay.
func TestUninstallScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	script, err := os.ReadFile("uninstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		choice   string
		dataGone bool
	}{{"1", false}, {"2", true}, {"3", false}} {
		home := t.TempDir()
		app := filepath.Join(home, "Applications", "cinearr")
		data := filepath.Join(home, ".config", "cinearr")
		kept := []string{
			filepath.Join(home, "Films", "Inception (2010)", "Inception (2010).mkv"),
			filepath.Join(home, "Downloads", "Twin.Peaks.S02E01.mkv"),
			filepath.Join(home, ".config", "vlc", "vlcrc"),
			filepath.Join(home, ".local", "bin", "mpv"),
		}
		files := append([]string{
			filepath.Join(app, "cinearr"),
			filepath.Join(app, "uninstall.sh"),
			filepath.Join(data, "settings.json"),
			filepath.Join(data, "players", "vlc", "vlc"),
		}, kept...)
		for _, f := range files {
			if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				t.Fatal(err)
			}
			content := "x"
			if filepath.Base(f) == "settings.json" {
				content = `{"port": 1}` // no running Cinearr to stop
			}
			if err := os.WriteFile(f, []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(app, "uninstall.sh"), script, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", filepath.Join(app, "uninstall.sh"))
		cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH")}
		cmd.Stdin = strings.NewReader(c.choice + "\n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("choice %s: %v\n%s", c.choice, err, out)
		}
		if strings.Contains(string(out), "players") {
			t.Errorf("choice %s: the players are still offered:\n%s", c.choice, out)
		}
		_, appErr := os.Stat(filepath.Join(app, "cinearr"))
		_, dataErr := os.Stat(data)
		if c.choice == "3" {
			if appErr != nil || dataErr != nil {
				t.Errorf("cancel changed something:\n%s", out)
			}
			continue
		}
		if appErr == nil {
			t.Errorf("choice %s: program kept", c.choice)
		}
		if gone := os.IsNotExist(dataErr); gone != c.dataGone {
			t.Errorf("choice %s: data folder deleted = %v, want %v", c.choice, gone, c.dataGone)
		}
		for _, f := range kept {
			if _, err := os.Stat(f); err != nil {
				t.Errorf("choice %s: %s was touched", c.choice, f)
			}
		}
	}
}
