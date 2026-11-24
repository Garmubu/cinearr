package desktop

import (
	"os/exec"
	"runtime"
	"strings"

	"cinearr/internal/i18n"
)

// ErrNoPicker is returned when no native dialog is available.
var ErrNoPicker = i18n.New("no native dialog available: type the path")

// Pick opens a native dialog to choose a folder (dir=true) or a file and
// returns the selected path ("" if cancelled).
func Pick(dir bool, title string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		var script string
		if dir {
			script = `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.FolderBrowserDialog; $f.Description = '` + psEscape(title) + `'; $f.ShowNewFolderButton = $true; if ($f.ShowDialog() -eq 'OK') { [Console]::Out.Write($f.SelectedPath) }`
		} else {
			script = `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.OpenFileDialog; $f.Title = '` + psEscape(title) + `'; $f.Filter = 'Programs (*.exe)|*.exe|All files (*.*)|*.*'; if ($f.ShowDialog() -eq 'OK') { [Console]::Out.Write($f.FileName) }`
		}
		cmd = exec.Command("powershell", "-NoProfile", "-STA", "-Command", script)
	case "darwin":
		what := "choose file"
		if dir {
			what = "choose folder"
		}
		cmd = exec.Command("osascript", "-e", `POSIX path of (`+what+` with prompt "`+strings.ReplaceAll(title, `"`, `'`)+`")`)
	default:
		if p, err := exec.LookPath("zenity"); err == nil {
			args := []string{"--file-selection", "--title=" + title}
			if dir {
				args = append(args, "--directory")
			}
			cmd = exec.Command(p, args...)
		} else if p, err := exec.LookPath("kdialog"); err == nil {
			if dir {
				cmd = exec.Command(p, "--getexistingdirectory", ".", "--title", title)
			} else {
				cmd = exec.Command(p, "--getopenfilename", ".", "--title", title)
			}
		} else {
			return "", ErrNoPicker
		}
	}
	hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", nil // dialog cancelled
	}
	return strings.TrimRight(strings.TrimSpace(string(out)), "/\\"), nil
}

func psEscape(s string) string { return strings.ReplaceAll(s, "'", "''") }
