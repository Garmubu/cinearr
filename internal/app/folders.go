package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"cinearr/internal/i18n"
	"cinearr/internal/store"
)

// Checks on the films and series folders, after Radarr's path validators
// (system folder, startup folder, root folder ancestor, download client
// root folder check). A wrong folder makes the scan read the whole disk,
// the downloads in progress, or the other library.

// systemFolders are never accepted as a library folder, nor anything
// inside them.
func systemFolders() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, v := range []string{"SystemRoot", "windir", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "ProgramData"} {
			if p := os.Getenv(v); p != "" {
				out = append(out, p)
			}
		}
		return out
	case "darwin":
		return []string{"/System", "/Library", "/Applications", "/bin", "/sbin", "/usr", "/etc", "/private/etc", "/dev", "/cores"}
	}
	return []string{"/bin", "/boot", "/dev", "/etc", "/lib", "/lib32", "/lib64", "/libx32", "/proc", "/run", "/sbin", "/sys", "/usr"}
}

// exactFolders are refused as such but accept subfolders (the home folder,
// the parent of the home folders).
func exactFolders() []string {
	var out []string
	if h, err := os.UserHomeDir(); err == nil {
		out = append(out, h, filepath.Dir(h))
	}
	return out
}

// systemDrive is the root of the disk that holds the system ("C:\", "/").
func systemDrive() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("SystemDrive"); d != "" {
			return d + `\`
		}
		return `C:\`
	}
	return "/"
}

// realPath resolves the links of a path, through its nearest existing
// parent when the folder does not exist yet ("/bin/Films" is
// "/usr/bin/Films" when /bin links to /usr/bin).
func realPath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// within reports whether p is parent or inside it.
func within(p, parent string) bool {
	_, ok := rebase(p, parent, parent)
	return ok
}

// mapLocal translates a path seen by the torrent client with the path
// mappings of the settings.
func mapLocal(p string, maps []store.PathMapping) string {
	for _, m := range maps {
		if m.Remote != "" && strings.HasPrefix(p, m.Remote) {
			rest := strings.TrimLeft(strings.ReplaceAll(strings.TrimPrefix(p, m.Remote), "\\", "/"), "/")
			return filepath.Join(m.Local, filepath.FromSlash(rest))
		}
	}
	return p
}

// CheckFolders refuses films and series folders that would harm the
// library: system disk root, system folders, home folder, Cinearr's own
// folders, a folder holding the other library or the downloads.
func CheckFolders(s store.Settings, dataDir string) error {
	type folder struct{ label, path string }
	var list []folder
	if s.LibraryPath != "" {
		list = append(list, folder{i18n.T("Films folder"), s.LibraryPath})
	}
	if s.SeriesPath != "" {
		list = append(list, folder{i18n.T("Series folder"), s.SeriesPath})
	}
	exe := ""
	if p, err := os.Executable(); err == nil {
		exe = filepath.Dir(realPath(p))
	}
	downloads := ""
	if s.Client.SavePath != "" {
		downloads = mapLocal(s.Client.SavePath, s.PathMappings)
		if filepath.IsAbs(downloads) {
			downloads = realPath(downloads)
		} else {
			downloads = ""
		}
	}
	for _, f := range list {
		if !filepath.IsAbs(f.path) {
			return i18n.Errorf("%s: give a full path (for example %s)", f.label, example())
		}
		p := realPath(f.path)
		if samePath(p, realPath(systemDrive())) {
			return i18n.Errorf("%s: %s is the root of the system disk, choose a folder in it (for example %s)", f.label, p, example())
		}
		for _, sys := range systemFolders() {
			if within(p, realPath(sys)) || within(filepath.Clean(f.path), sys) {
				return i18n.Errorf("%s: %s is a system folder", f.label, p)
			}
		}
		for _, x := range exactFolders() {
			if samePath(p, realPath(x)) {
				return i18n.Errorf("%s: %s holds much more than videos, choose a folder in it (for example %s)", f.label, p, example())
			}
		}
		if dataDir != "" && (within(p, realPath(dataDir)) || within(realPath(dataDir), p)) {
			return i18n.Errorf("%s: %s contains the data of Cinearr or is inside it", f.label, p)
		}
		if exe != "" && samePath(p, exe) {
			return i18n.Errorf("%s: %s is the folder of the Cinearr program", f.label, p)
		}
		if downloads != "" && within(downloads, p) {
			return i18n.Errorf("%s: the torrent client downloads into %s, choose a library folder that does not contain the downloads", f.label, downloads)
		}
	}
	if len(list) == 2 && (within(realPath(list[0].path), realPath(list[1].path)) || within(realPath(list[1].path), realPath(list[0].path))) {
		return i18n.New("the films folder and the series folder must be separate, neither inside the other")
	}
	return nil
}

func example() string {
	if runtime.GOOS == "windows" {
		return `D:\Films`
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Videos", "Films")
	}
	return "/media/films"
}
