// Package desktop opens the interface in a dedicated application window.
//
// A Chromium-based browser already present on the machine (Edge ships with
// Windows 10/11; Chrome, Chromium or Brave elsewhere) is started in "app"
// mode with its own profile: the result is a native window without address
// bar or tabs, and no web engine has to be bundled. When none is found the
// default browser is used.
package desktop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func browserCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		pf := os.Getenv("ProgramFiles")
		pf86 := os.Getenv("ProgramFiles(x86)")
		local := os.Getenv("LOCALAPPDATA")
		return []string{
			filepath.Join(pf86, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf, `BraveSoftware\Brave-Browser\Application\brave.exe`),
			filepath.Join(local, `Chromium\Application\chrome.exe`),
		}
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	default:
		var out []string
		for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "microsoft-edge-stable", "brave-browser"} {
			if p, err := exec.LookPath(n); err == nil {
				out = append(out, p)
			}
		}
		return out
	}
}

// FindBrowser returns a Chromium-based browser able to open app windows.
func FindBrowser() string {
	for _, p := range browserCandidates() {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Window is a running application window.
type Window struct {
	cmd *exec.Cmd
}

// Wait blocks until the window is closed.
func (w *Window) Wait() { _ = w.cmd.Wait() }

// OpenWindow starts an app window on url using profileDir as profile.
func OpenWindow(url, profileDir string) (*Window, error) {
	b := FindBrowser()
	if b == "" {
		return nil, errors.New("no Chromium browser found")
	}
	_ = os.MkdirAll(profileDir, 0o755)
	cmd := exec.Command(b,
		"--app="+url,
		"--user-data-dir="+profileDir,
		"--window-size=1440,900",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate,TranslateUI",
		"--disable-sync",
		"--autoplay-policy=no-user-gesture-required",
	)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Window{cmd: cmd}, nil
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) error { return OpenPath(url) }

// OpenPath opens a folder or URL with the system handler.
func OpenPath(p string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", p)
	case "darwin":
		cmd = exec.Command("open", p)
	default:
		cmd = exec.Command("xdg-open", p)
	}
	return cmd.Start()
}
