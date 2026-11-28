package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"cinearr/internal/desktop"
	"cinearr/internal/i18n"
	"cinearr/internal/update"
)

// updater returns update.exe when Cinearr was installed with the Windows
// installer.
func updater() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	p := filepath.Join(filepath.Dir(exe), "update.exe")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// checkUpdate compares this version with the latest release.
func (s *Server) checkUpdate(w http.ResponseWriter, r *http.Request) (any, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rel, err := update.Latest(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"current": Version, "latest": rel.Version(), "newer": update.Newer(rel.Version(), Version),
		"url": rel.URL, "installer": updater() != ""}, nil
}

// runUpdate starts update.exe, which asks before updating and closes
// Cinearr itself.
func (s *Server) runUpdate(w http.ResponseWriter, r *http.Request) (any, error) {
	p := updater()
	if p == "" {
		return nil, i18n.New("automatic update is only available for the version installed with the Windows installer")
	}
	cmd := exec.Command(p)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	desktop.BringToFront(cmd.Process.Pid)
	return nil, nil
}
