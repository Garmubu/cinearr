package player

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"cinearr/internal/i18n"
)

// Progress of an installation, polled by the interface.
type Progress struct {
	Running bool   `json:"running"`
	Step    string `json:"step,omitempty"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Error   string `json:"error,omitempty"`
	Path    string `json:"path,omitempty"`
}

var (
	installMu sync.Mutex
	installs  = map[string]*Progress{}
)

// InstallStatus returns the progress of the last installation of a player.
func InstallStatus(id string) Progress {
	installMu.Lock()
	defer installMu.Unlock()
	if p := installs[id]; p != nil {
		return *p
	}
	return Progress{}
}

func setProgress(id string, f func(p *Progress)) {
	installMu.Lock()
	defer installMu.Unlock()
	p := installs[id]
	if p == nil {
		p = &Progress{}
		installs[id] = p
	}
	f(p)
}

// CanInstall reports whether Cinearr can download the player on this
// system (portable VLC or MPC-HC on Windows, VLC on macOS).
func CanInstall(id string) bool {
	switch runtime.GOOS {
	case "windows":
		return id == VLC || (id == MPCHC && runtime.GOARCH != "arm")
	case "darwin":
		return id == VLC
	}
	return false
}

// InstallHint explains how to install a player when Cinearr cannot do it.
func InstallHint(id string) string {
	switch runtime.GOOS {
	case "linux":
		switch id {
		case VLC:
			return i18n.T("Install the “vlc” package (for example sudo apt install vlc) or flatpak install flathub org.videolan.VLC")
		case MPV:
			return i18n.T("Install the “mpv” package (for example sudo apt install mpv)")
		}
	case "darwin":
		if id == MPV {
			return "brew install --cask mpv"
		}
	case "windows":
		if id == MPV {
			return "winget install shinchiro.mpv"
		}
	}
	return ""
}

// Install downloads and unpacks a player into the data folder. It returns
// the path of the executable.
func Install(ctx context.Context, id, dataDir string) (string, error) {
	installMu.Lock()
	if p := installs[id]; p != nil && p.Running {
		installMu.Unlock()
		return "", i18n.New("installation already in progress")
	}
	installs[id] = &Progress{Running: true, Step: "Looking for the latest version"}
	installMu.Unlock()

	exe, err := install(ctx, id, dataDir)
	setProgress(id, func(p *Progress) {
		p.Running = false
		p.Path = exe
		if err != nil {
			p.Error = err.Error()
			p.Step = ""
		} else {
			p.Step = "Installed"
		}
	})
	return exe, err
}

func install(ctx context.Context, id, dataDir string) (string, error) {
	if !CanInstall(id) {
		if h := InstallHint(id); h != "" {
			return "", i18n.Errorf("automatic installation is not possible on this system: %s", h)
		}
		return "", i18n.New("automatic installation is not possible on this system")
	}
	root := InstallDir(dataDir)
	tmp := filepath.Join(root, ".download")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	switch {
	case id == VLC && runtime.GOOS == "windows":
		return installVLCWindows(ctx, root, tmp)
	case id == VLC && runtime.GOOS == "darwin":
		return installVLCMac(ctx, root, tmp)
	case id == MPCHC:
		return installMPCHC(ctx, root, tmp)
	}
	return "", i18n.New("automatic installation is not possible on this system")
}

var httpGet = func(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Cinearr")
	if strings.Contains(url, "api.github.com") {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s : HTTP %d", url, resp.StatusCode)
	}
	return resp, nil
}

func fetchText(ctx context.Context, url string) (string, error) {
	resp, err := httpGet(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}

// shaSyntax is a SHA-256 in hexadecimal.
var shaSyntax = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// download saves url to dest, reporting the progress, and checks its
// SHA-256. Without a published checksum nothing is installed: a program
// that will run on the computer must match what its authors released.
func download(ctx context.Context, id, url, dest, sha string) error {
	if !shaSyntax.MatchString(sha) {
		return i18n.Errorf("no checksum published for %s: installation stopped, install %s from its official site", path.Base(url), Name(id))
	}
	setProgress(id, func(p *Progress) { p.Step = "Downloading"; p.Done, p.Total = 0, 0 })
	resp, err := httpGet(ctx, url)
	if err != nil {
		return i18n.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	setProgress(id, func(p *Progress) { p.Total = resp.ContentLength })
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	h := sha256.New()
	buf := make([]byte, 256<<10)
	var done int64
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if done > 2<<30 {
				f.Close()
				return i18n.New("abnormally large file")
			}
			if time.Since(last) > 200*time.Millisecond {
				d := done
				setProgress(id, func(p *Progress) { p.Done = d })
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return i18n.Errorf("download interrupted: %w", rerr)
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	setProgress(id, func(p *Progress) { p.Done = done })
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, sha) {
		return i18n.Errorf("wrong checksum (expected %s, got %s): corrupted or modified file", sha, got)
	}
	return nil
}

// unzip extracts an archive into dir, refusing paths that escape it.
func unzip(src, dir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return i18n.Errorf("unreadable archive: %w", err)
	}
	defer r.Close()
	var total uint64
	for _, f := range r.File {
		name := filepath.FromSlash(f.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(filepath.Clean(name), "..") || strings.Contains(name, ":") {
			return i18n.Errorf("path refused in the archive: %s", f.Name)
		}
		target := filepath.Join(dir, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		total += f.UncompressedSize64
		if total > 2<<30 {
			return i18n.New("abnormally large archive")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			in.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(in, int64(f.UncompressedSize64)+1))
		in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// findFile looks for one of names under dir.
func findFile(dir string, names ...string) string {
	var found string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() {
			for _, n := range names {
				if strings.EqualFold(d.Name(), n) {
					found = p
				}
			}
		}
		return nil
	})
	return found
}

// replaceDir moves the folder holding the executable to dest.
func replaceDir(fromDir, dest string) error {
	_ = os.RemoveAll(dest)
	if err := os.Rename(fromDir, dest); err != nil {
		return i18n.Errorf("cannot install into %s: %w", dest, err)
	}
	return nil
}

// ---------------------------------------------------------------- VLC

var vlcMirror = "https://download.videolan.org/pub/videolan/vlc/"

// vlcRelease finds the latest file of VideoLAN's "last" folder matching
// suffix (e.g. "-win64.zip"), falling back on VLC's own update file.
func vlcRelease(ctx context.Context, folder, suffix, statusFile string) (url, sha string, err error) {
	idx := vlcMirror + "last/" + folder + "/"
	re := regexp.MustCompile(`href="(vlc-[0-9][0-9.]*` + regexp.QuoteMeta(suffix) + `)"`)
	name := ""
	if page, err := fetchText(ctx, idx); err == nil {
		if m := re.FindStringSubmatch(page); m != nil {
			name = m[1]
		}
	}
	if name == "" && statusFile != "" {
		// first line of VLC's update status file is the version
		text, err := fetchText(ctx, "https://update.videolan.org/vlc/"+statusFile)
		if err != nil {
			return "", "", i18n.Errorf("latest version of VLC not found: %w", err)
		}
		v := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
		if !regexp.MustCompile(`^[0-9]+(\.[0-9]+)+$`).MatchString(v) {
			return "", "", i18n.New("latest version of VLC not found")
		}
		idx = vlcMirror + v + "/" + folder + "/"
		name = "vlc-" + v + suffix
	}
	if name == "" {
		return "", "", i18n.New("latest version of VLC not found on download.videolan.org")
	}
	url = idx + name
	if text, err := fetchText(ctx, url+".sha256"); err == nil {
		if f := strings.Fields(text); len(f) > 0 && len(f[0]) == 64 {
			sha = f[0]
		}
	}
	return url, sha, nil
}

func installVLCWindows(ctx context.Context, root, tmp string) (string, error) {
	folder, status := "win64", "status-win-x64"
	if runtime.GOARCH == "386" {
		folder, status = "win32", "status-win-x86"
	}
	url, sha, err := vlcRelease(ctx, folder, "-"+folder+".zip", status)
	if err != nil {
		return "", err
	}
	archive := filepath.Join(tmp, "vlc.zip")
	if err := download(ctx, VLC, url, archive, sha); err != nil {
		return "", err
	}
	setProgress(VLC, func(p *Progress) { p.Step = "Unpacking" })
	out := filepath.Join(tmp, "unpacked")
	if err := unzip(archive, out); err != nil {
		return "", err
	}
	exe := findFile(out, "vlc.exe")
	if exe == "" {
		return "", i18n.New("vlc.exe missing from the archive")
	}
	dest := filepath.Join(root, "vlc")
	if err := replaceDir(filepath.Dir(exe), dest); err != nil {
		return "", err
	}
	// portable mode: VLC keeps its settings next to it and leaves an
	// installed VLC alone
	_ = os.MkdirAll(filepath.Join(dest, "portable"), 0o755)
	return filepath.Join(dest, "vlc.exe"), nil
}

func installVLCMac(ctx context.Context, root, tmp string) (string, error) {
	url, sha, err := vlcRelease(ctx, "macosx", "-universal.dmg", "")
	if err != nil {
		return "", err
	}
	dmg := filepath.Join(tmp, "vlc.dmg")
	if err := download(ctx, VLC, url, dmg, sha); err != nil {
		return "", err
	}
	setProgress(VLC, func(p *Progress) { p.Step = "Installing" })
	mnt := filepath.Join(tmp, "mnt")
	if err := os.MkdirAll(mnt, 0o755); err != nil {
		return "", err
	}
	if out, err := exec.CommandContext(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg).CombinedOutput(); err != nil {
		return "", i18n.Errorf("cannot open the disk image: %v %s", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("hdiutil", "detach", "-force", mnt).Run()
	app := filepath.Join(mnt, "VLC.app")
	if _, err := os.Stat(app); err != nil {
		return "", i18n.New("VLC.app missing from the disk image")
	}
	dest := filepath.Join(root, "VLC.app")
	_ = os.RemoveAll(dest)
	if out, err := exec.CommandContext(ctx, "ditto", app, dest).CombinedOutput(); err != nil {
		return "", i18n.Errorf("cannot copy VLC: %v %s", err, strings.TrimSpace(string(out)))
	}
	return filepath.Join(dest, "Contents", "MacOS", "VLC"), nil
}

// ---------------------------------------------------------------- MPC-HC

var mpcRelease = "https://api.github.com/repos/clsid2/mpc-hc/releases/latest"

func installMPCHC(ctx context.Context, root, tmp string) (string, error) {
	resp, err := httpGet(ctx, mpcRelease)
	if err != nil {
		return "", i18n.Errorf("latest version of MPC-HC not found: %w", err)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	err = json.NewDecoder(bufio.NewReader(io.LimitReader(resp.Body, 4<<20))).Decode(&rel)
	resp.Body.Close()
	if err != nil {
		return "", i18n.Errorf("unreadable answer from GitHub: %w", err)
	}
	arch := "x64"
	if runtime.GOARCH == "386" {
		arch = "x86"
	}
	re := regexp.MustCompile(`(?i)^MPC-HC\.[0-9.]+\.` + arch + `\.zip$`)
	url, sha := "", ""
	for _, a := range rel.Assets {
		if re.MatchString(a.Name) {
			url = a.URL
			sha = strings.TrimPrefix(a.Digest, "sha256:")
			break
		}
	}
	if url == "" {
		return "", i18n.Errorf("MPC-HC %s archive not found in version %s", arch, rel.TagName)
	}
	archive := filepath.Join(tmp, "mpc-hc.zip")
	if err := download(ctx, MPCHC, url, archive, sha); err != nil {
		return "", err
	}
	setProgress(MPCHC, func(p *Progress) { p.Step = "Unpacking" })
	out := filepath.Join(tmp, "unpacked")
	if err := unzip(archive, out); err != nil {
		return "", err
	}
	exe := findFile(out, "mpc-hc64.exe", "mpc-hc.exe")
	if exe == "" {
		return "", i18n.New("mpc-hc64.exe missing from the archive")
	}
	name := filepath.Base(exe)
	dest := filepath.Join(root, "mpc-hc")
	if err := replaceDir(filepath.Dir(exe), dest); err != nil {
		return "", err
	}
	// an .ini next to the executable makes MPC-HC portable (no registry)
	ini := filepath.Join(dest, strings.TrimSuffix(name, filepath.Ext(name))+".ini")
	if _, err := os.Stat(ini); err != nil {
		_ = os.WriteFile(ini, nil, 0o644)
	}
	return filepath.Join(dest, name), nil
}
