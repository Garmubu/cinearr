//go:build windows

// Command cinearr-uninstall is uninstall.exe, installed next to cinearr.exe
// by the Windows installer: it asks whether the data of Cinearr must be
// deleted too, then runs the uninstaller of the installation.
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
	q := i18n.Tf("Uninstall Cinearr from %s?\n\nDo you also want to delete all its data: settings, list of films and series, history, logs and the players it installed (%s)?\n\nYes: uninstall and delete the data\nNo: uninstall and keep the data, for a later reinstallation\nCancel: do nothing\n\nYour films, series and downloads are never deleted.", dir, data)
	var del string
	switch desktop.Ask(title, q, true) {
	case desktop.Yes:
		del = "yes"
	case desktop.No:
		del = "no"
	default:
		return
	}
	update.QuitRunning(data)
	if err := exec.Command(uninstallers[0], "/SILENT", "/DELETEDATA="+del).Start(); err != nil {
		desktop.Alert(title, err.Error())
	}
}
