//go:build windows

// Command cinearr-update is update.exe, installed next to cinearr.exe by
// the Windows installer: it looks for the latest release on GitHub and,
// when it is newer, downloads its installer, checks it and runs it in
// update mode (settings and library are kept).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"cinearr/internal/desktop"
	"cinearr/internal/i18n"
	"cinearr/internal/update"
)

func main() {
	dir := flag.String("dir", "", "installation folder (set when run from a temporary copy)")
	flag.Parse()
	exe, err := os.Executable()
	if err != nil {
		desktop.Alert("Cinearr", err.Error())
		return
	}
	installDir := *dir
	if installDir == "" {
		installDir = filepath.Dir(exe)
		// the installer replaces update.exe: the work is done by a copy
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("cinearr-update-%d.exe", os.Getpid()))
		if copyFile(exe, tmp) == nil && exec.Command(tmp, "-dir", installDir).Start() == nil {
			return
		}
	}
	data := update.DataDir(installDir)
	i18n.SetLanguage(desktop.SystemLanguage())
	update.UseLanguage(data)
	title := i18n.T("Cinearr update")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	r, err := update.Latest(ctx)
	if err != nil {
		desktop.Alert(title, i18n.Tf("Could not check for updates: %v", err))
		return
	}
	if !update.Newer(r.Version(), update.Version) {
		desktop.Info(title, i18n.Tf("Cinearr %s is up to date.", update.Version))
		return
	}
	q := i18n.Tf("Cinearr %s is available (installed: %s).\n\nUpdate now? Cinearr closes during the update and opens again afterwards. Your settings and your library are kept.\n\nWhat's new: %s", r.Version(), update.Version, r.URL)
	if desktop.Ask(title, q, false) != desktop.Yes {
		return
	}
	a := r.Asset(update.SetupName(r.Version()))
	if a == nil {
		desktop.Alert(title, i18n.Tf("The installer of version %s is not available yet: try again in a few minutes.", r.Version()))
		return
	}
	sha, err := update.Checksum(ctx, *r, a.Name)
	if err != nil {
		desktop.Alert(title, err.Error())
		return
	}
	setup := filepath.Join(os.TempDir(), a.Name)
	if err := update.Download(ctx, *a, sha, setup); err != nil {
		desktop.Alert(title, err.Error())
		return
	}
	update.QuitRunning(data)
	// silent update: progress window only, Cinearr is started again at the end
	cmd := exec.Command(setup, "/SILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CLOSEAPPLICATIONS", "/UPDATE", "/DIR="+installDir)
	if err := cmd.Start(); err != nil {
		desktop.Alert(title, i18n.Tf("The installer could not be started: %v", err))
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
