//go:build windows

// Command cinearr-uninstall is uninstall.exe, installed next to cinearr.exe
// by the Windows installer: one window with Uninstall and Cancel and a box
// to delete the data of Cinearr too, then the uninstaller of the
// installation runs.
package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"cinearr/internal/desktop"
	"cinearr/internal/i18n"
	"cinearr/internal/update"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		desktop.Alert("Cinearr", err.Error())
		return
	}
	dir := filepath.Dir(exe)
	data := update.DataDir(dir)
	i18n.SetLanguage(desktop.SystemLanguage())
	update.UseLanguage(data)
	title := i18n.T("Uninstall Cinearr")

	uninstallers, _ := filepath.Glob(filepath.Join(dir, "unins*.exe"))
	if len(uninstallers) == 0 {
		desktop.Info(title, i18n.Tf("This copy of Cinearr was not installed with the installer: to remove it, delete the folder %s. Its data is in %s.", dir, data))
		return
	}
	// one question: Uninstall or Cancel, and a box to delete the data too
	ok, deleteData, err := desktop.Confirm{
		Title:       title,
		Instruction: i18n.T("Uninstall Cinearr?"),
		Content:     i18n.Tf("Cinearr will be removed from %s.\n\nYour films, series and downloads are never deleted.", dir),
		Action:      i18n.T("Uninstall"),
		CheckBox:    i18n.Tf("Also delete the data of Cinearr (settings, list of films and series, history and logs, in %s)", data),
	}.Show()
	if err != nil {
		// without the task dialog: two plain questions
		if desktop.Ask(title, i18n.Tf("Uninstall Cinearr from %s?\n\nYour films, series and downloads are never deleted.", dir), false) != desktop.Yes {
			return
		}
		ok = true
		deleteData = desktop.Ask(title, i18n.Tf("Also delete the data of Cinearr (settings, list of films and series, history and logs, in %s)?", data), false) == desktop.Yes
	}
	if !ok {
		return
	}
	del := "no"
	if deleteData {
		del = "yes"
	}
	update.QuitRunning(data)
	if err := exec.Command(uninstallers[0], "/SILENT", "/DELETEDATA="+del).Start(); err != nil {
		desktop.Alert(title, err.Error())
	}
}
