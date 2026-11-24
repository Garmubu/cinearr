// Package player finds and launches the native video player chosen by the
// user: VLC, MPC-HC (Windows) or mpv. Playback does not go through the web
// interface: these players decode every modern codec (HEVC, AV1, VP9,
// HDR10, Dolby Vision, TrueHD/Atmos) with hardware acceleration, which a
// browser cannot do without transcoding. When none is installed, Cinearr
// can download VLC or MPC-HC into its data folder (see install.go).
package player

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"cinearr/internal/i18n"
)

// Kinds of player.
const (
	VLC    = "vlc"
	MPCHC  = "mpc-hc"
	MPV    = "mpv"
	Custom = "custom"
)

// Info describes a player for the interface.
type Info struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
}

var all = []Info{
	{ID: VLC, Name: "VLC", Desc: "Full interface: volume bar, choice of audio tracks and subtitles with the mouse. Plays every format."},
	{ID: MPCHC, Name: "MPC-HC", Desc: "Light and fast, classic Windows interface with a volume bar. Plays every format (built-in LAV filters)."},
	{ID: MPV, Name: "mpv", Desc: "Minimalist, driven mostly by the keyboard (volume: mouse wheel or keys 9 and 0). Excellent HDR rendering."},
}

// Players lists the players available on this system, in order of
// preference for the automatic choice.
func Players() []Info {
	var out []Info
	for _, p := range all {
		if p.ID == MPCHC && runtime.GOOS != "windows" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Name returns the display name of a player kind.
func Name(id string) string {
	for _, p := range all {
		if p.ID == id {
			return p.Name
		}
	}
	return i18n.T("the player")
}

// Options for one playback.
type Options struct {
	Title      string
	Subtitles  []string
	Fullscreen bool
	AudioLang  string
	SubLang    string
	Resume     bool
	DataDir    string
	ExtraArgs  string
}

// InstallDir is where Cinearr installs players.
func InstallDir(dataDir string) string { return filepath.Join(dataDir, "players") }

func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return ""
}

// programDirs returns the Windows program folders.
func programDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		if d := os.Getenv(env); d != "" && !seen[strings.ToLower(d)] {
			seen[strings.ToLower(d)] = true
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		dirs = []string{`C:\Program Files`, `C:\Program Files (x86)`}
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		dirs = append(dirs, filepath.Join(local, "Programs"))
	}
	return dirs
}

// candidates lists the usual locations of a player.
func candidates(id, dataDir string) []string {
	home, _ := os.UserHomeDir()
	own := InstallDir(dataDir)
	var c []string
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		switch id {
		case VLC:
			c = append(c, filepath.Join(own, "vlc", "vlc.exe"), filepath.Join(exeDir(), "vlc", "vlc.exe"))
			for _, d := range programDirs() {
				c = append(c, filepath.Join(d, "VideoLAN", "VLC", "vlc.exe"))
			}
			c = append(c, filepath.Join(home, "scoop", "apps", "vlc", "current", "vlc.exe"))
		case MPCHC:
			c = append(c, filepath.Join(own, "mpc-hc", "mpc-hc64.exe"), filepath.Join(own, "mpc-hc", "mpc-hc.exe"))
			for _, d := range programDirs() {
				c = append(c, filepath.Join(d, "MPC-HC", "mpc-hc64.exe"), filepath.Join(d, "MPC-HC", "mpc-hc.exe"),
					filepath.Join(d, "K-Lite Codec Pack", "MPC-HC64", "mpc-hc64.exe"), filepath.Join(d, "K-Lite Codec Pack", "MPC-HC", "mpc-hc.exe"),
					filepath.Join(d, "MPC-BE x64", "mpc-be64.exe"), filepath.Join(d, "MPC-BE", "mpc-be64.exe"), filepath.Join(d, "MPC-BE", "mpc-be.exe"))
			}
			c = append(c, filepath.Join(home, "scoop", "apps", "mpc-hc-fork", "current", "mpc-hc64.exe"))
		case MPV:
			c = append(c, filepath.Join(own, "mpv", "mpv.exe"), filepath.Join(exeDir(), "mpv.exe"), filepath.Join(exeDir(), "mpv", "mpv.exe"))
			for _, d := range programDirs() {
				c = append(c, filepath.Join(d, "mpv", "mpv.exe"))
			}
			c = append(c, filepath.Join(home, "scoop", "apps", "mpv", "current", "mpv.exe"),
				`C:\ProgramData\chocolatey\bin\mpv.exe`, filepath.Join(local, "Microsoft", "WinGet", "Links", "mpv.exe"))
		}
	case "darwin":
		switch id {
		case VLC:
			c = append(c, filepath.Join(own, "VLC.app", "Contents", "MacOS", "VLC"), "/Applications/VLC.app/Contents/MacOS/VLC",
				filepath.Join(home, "Applications", "VLC.app", "Contents", "MacOS", "VLC"))
		case MPV:
			c = append(c, filepath.Join(exeDir(), "mpv"), "/Applications/mpv.app/Contents/MacOS/mpv",
				filepath.Join(home, "Applications", "mpv.app", "Contents", "MacOS", "mpv"), "/opt/homebrew/bin/mpv", "/usr/local/bin/mpv")
		}
	default:
		switch id {
		case VLC:
			c = append(c, "/usr/bin/vlc", "/usr/local/bin/vlc", "/snap/bin/vlc", "/var/lib/flatpak/exports/bin/org.videolan.VLC",
				filepath.Join(home, ".local", "share", "flatpak", "exports", "bin", "org.videolan.VLC"))
		case MPV:
			c = append(c, filepath.Join(exeDir(), "mpv"), "/usr/bin/mpv", "/usr/local/bin/mpv", "/snap/bin/mpv",
				"/var/lib/flatpak/exports/bin/io.mpv.Mpv", filepath.Join(home, ".local", "share", "flatpak", "exports", "bin", "io.mpv.Mpv"))
		}
	}
	return c
}

// exeNames are the executable names looked up in PATH and, on Windows,
// in the sub-folders of the program folders (installers that use their
// own folder name, e.g. some winget packages).
func exeNames(id string) []string {
	win := runtime.GOOS == "windows"
	switch id {
	case VLC:
		if win {
			return []string{"vlc.exe"}
		}
		return []string{"vlc"}
	case MPCHC:
		return []string{"mpc-hc64.exe", "mpc-hc.exe", "mpc-be64.exe"}
	case MPV:
		if win {
			return []string{"mpv.exe"}
		}
		return []string{"mpv"}
	}
	return nil
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Detect returns the path of an installed player, or "".
func Detect(id, dataDir string) string {
	for _, p := range candidates(id, dataDir) {
		if p != "" && isFile(p) {
			return p
		}
	}
	for _, n := range exeNames(id) {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	if runtime.GOOS == "windows" {
		for _, d := range programDirs() {
			for _, n := range exeNames(id) {
				for _, pattern := range []string{filepath.Join(d, "*", n), filepath.Join(d, "*", "*", n)} {
					if m, _ := filepath.Glob(pattern); len(m) > 0 {
						return m[0]
					}
				}
			}
		}
	}
	return ""
}

// Find returns the executable to use for the settings.
func Find(kind, path, dataDir string) (string, error) {
	if kind == Custom || (kind == "" && path != "") {
		if path == "" {
			return "", i18n.New("no player given: choose its file in Settings › Player")
		}
		if isFile(path) {
			return path, nil
		}
		return "", i18n.Errorf("the configured player cannot be found: %s", path)
	}
	if kind != "" {
		if p := Detect(kind, dataDir); p != "" {
			return p, nil
		}
		return "", i18n.Errorf("%s is not installed: install it from Settings › Player or choose another player", Name(kind))
	}
	for _, p := range Players() {
		if exe := Detect(p.ID, dataDir); exe != "" {
			return exe, nil
		}
	}
	return "", i18n.New("no video player found: install VLC from Settings › Player")
}

// Family tells which command line an executable understands.
func Family(exe string) string {
	b := strings.ToLower(exe)
	if i := strings.LastIndexAny(b, `/\`); i >= 0 {
		b = b[i+1:]
	}
	switch {
	case strings.HasPrefix(b, "mpv") || strings.HasPrefix(b, "io.mpv"):
		return MPV
	case strings.Contains(b, "vlc"):
		return VLC
	case strings.HasPrefix(b, "mpc-hc") || strings.HasPrefix(b, "mpc-be"):
		return MPCHC
	}
	return Custom
}

// Args builds the command line for a player.
func Args(exe, file string, o Options) []string {
	var args []string
	switch Family(exe) {
	case MPV:
		args = append(args, "--force-window=immediate", "--hwdec=auto-safe", "--keep-open=no", "--sub-auto=fuzzy", "--title="+o.Title)
		if o.SubLang != "" {
			args = append(args, "--slang="+o.SubLang)
		}
		if o.AudioLang != "" {
			args = append(args, "--alang="+o.AudioLang)
		}
		if o.Fullscreen {
			args = append(args, "--fs")
		}
		if o.Resume && o.DataDir != "" {
			wl := filepath.Join(o.DataDir, "watch_later")
			_ = os.MkdirAll(wl, 0o755)
			args = append(args, "--save-position-on-quit", "--watch-later-dir="+wl)
		}
		for _, s := range o.Subtitles {
			args = append(args, "--sub-file="+s)
		}
		args = append(args, strings.Fields(o.ExtraArgs)...)
		return append(args, "--", file)
	case VLC:
		args = append(args, "--no-one-instance", "--play-and-exit", "--meta-title="+o.Title)
		if o.SubLang != "" {
			args = append(args, "--sub-language="+o.SubLang)
		}
		if o.AudioLang != "" {
			args = append(args, "--audio-language="+o.AudioLang)
		}
		if o.Fullscreen {
			args = append(args, "--fullscreen")
		}
		if o.Resume {
			args = append(args, "--qt-continue=2") // resume without asking
		} else {
			args = append(args, "--qt-continue=0")
		}
		// VLC loads the other subtitles of the folder by itself
		if len(o.Subtitles) > 0 {
			args = append(args, "--sub-file="+o.Subtitles[0])
		}
		args = append(args, strings.Fields(o.ExtraArgs)...)
		return append(args, file)
	case MPCHC:
		args = append(args, file, "/play", "/close")
		if o.Fullscreen {
			args = append(args, "/fullscreen")
		}
		for _, s := range o.Subtitles {
			args = append(args, "/sub", s)
		}
		return append(args, strings.Fields(o.ExtraArgs)...)
	}
	return append(strings.Fields(o.ExtraArgs), file)
}

// Play starts the player on file and returns immediately.
func Play(exe, file string, o Options) (*exec.Cmd, error) {
	cmd := exec.Command(exe, Args(exe, file, o)...)
	cmd.Dir = filepath.Dir(file)
	hideConsole(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return cmd, nil
}
