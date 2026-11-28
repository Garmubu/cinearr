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
		script := windowsPickScript(dir, title)
		cmd = exec.Command("powershell", "-NoProfile", "-STA", "-Command", script)
	case "darwin":
		// "activate" brings the dialog of osascript in front of the window
		// of Cinearr (it targets osascript itself: no permission asked)
		what := "choose file"
		if dir {
			what = "choose folder"
		}
		cmd = exec.Command("osascript", "-e", "activate", "-e", `POSIX path of (`+what+` with prompt "`+strings.ReplaceAll(title, `"`, `'`)+`")`)
	default:
		// zenity and kdialog open a new window, given the focus by the
		// window manager
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

// windowsPickScript is the PowerShell script of the dialog. The dialog is
// opened by a hidden PowerShell: Windows does not let a process in the
// background take the foreground, so it would open behind the window of
// Cinearr. Its owner is an invisible form kept above every window
// (TopMost): the dialog, owned by it, is shown in front too, centred on
// the screen.
func windowsPickScript(dir bool, title string) string {
	owner := `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; ` +
		`$o = New-Object System.Windows.Forms.Form; $o.TopMost = $true; $o.ShowInTaskbar = $false; $o.FormBorderStyle = 'None'; ` +
		`$o.Opacity = 0; $o.StartPosition = 'CenterScreen'; $o.Size = New-Object System.Drawing.Size(1, 1); $o.Show(); $o.Activate(); `
	if dir {
		return owner + `$f = New-Object System.Windows.Forms.FolderBrowserDialog; $f.Description = '` + psEscape(title) + `'; $f.ShowNewFolderButton = $true; if ($f.ShowDialog($o) -eq 'OK') { [Console]::Out.Write($f.SelectedPath) }; $o.Close()`
	}
	return owner + `$f = New-Object System.Windows.Forms.OpenFileDialog; $f.Title = '` + psEscape(title) + `'; $f.Filter = 'Programs (*.exe)|*.exe|All files (*.*)|*.*'; if ($f.ShowDialog($o) -eq 'OK') { [Console]::Out.Write($f.FileName) }; $o.Close()`
}

func psEscape(s string) string { return strings.ReplaceAll(s, "'", "''") }
