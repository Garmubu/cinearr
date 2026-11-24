//go:build !windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Alert shows an error dialog when a desktop is available, and always
// prints the message on stderr.
func Alert(title, msg string) {
	fmt.Fprintf(os.Stderr, "%s : %s\n", title, msg)
	switch runtime.GOOS {
	case "darwin":
		esc := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) }
		_ = exec.Command("osascript", "-e", `display alert "`+esc(title)+`" message "`+esc(msg)+`" as critical`).Run()
	default:
		if p, err := exec.LookPath("zenity"); err == nil {
			_ = exec.Command(p, "--error", "--title="+title, "--text="+msg).Run()
		} else if p, err := exec.LookPath("kdialog"); err == nil {
			_ = exec.Command(p, "--error", msg, "--title", title).Run()
		}
	}
}
