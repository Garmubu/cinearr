// Package update finds the latest published version of Cinearr on GitHub,
// downloads and checks a file of that release, and closes the running
// Cinearr before an update. It is used by the updater (update.exe) and by
// the "Check for updates" button of the settings.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cinearr/internal/i18n"
)

var (
	// Version is the version of this build ("0.2.0"; "dev" for a local build).
	Version = "dev"
	// Repo is the GitHub repository of the releases ("owner/name"), set by
	// the release workflow.
	Repo = ""
	// APIBase is GitHub's API (tests).
	APIBase = "https://api.github.com"
)

var client = &http.Client{Timeout: 60 * time.Second}

// Release is a published version.
type Release struct {
	Tag    string  `json:"tag_name"`
	URL    string  `json:"html_url"`
	Notes  string  `json:"body"`
	Assets []Asset `json:"assets"`
}

// Asset is a file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Version returns the version number of the release ("0.2.0").
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Asset returns the file of the release named name.
func (r Release) Asset(name string) *Asset {
	for i := range r.Assets {
		if strings.EqualFold(r.Assets[i].Name, name) {
			return &r.Assets[i]
		}
	}
	return nil
}

// SetupName is the name of the Windows installer of a version.
func SetupName(version string) string { return "Cinearr-Setup-" + version + ".exe" }

// Latest returns the latest published release.
func Latest(ctx context.Context) (*Release, error) {
	if Repo == "" {
		return nil, i18n.New("this build does not know where its updates are published (local build)")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Cinearr/"+Version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, i18n.Errorf("GitHub cannot be reached: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, i18n.New("no version has been published yet")
	case resp.StatusCode != http.StatusOK:
		return nil, i18n.Errorf("GitHub answered HTTP %d", resp.StatusCode)
	}
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if _, ok := parse(r.Version()); !ok {
		return nil, i18n.Errorf("unexpected version number: %s", r.Tag)
	}
	return &r, nil
}

// parse reads "1.2.3" (a "v" and a "-suffix" are ignored).
func parse(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether version latest comes after current. A local build
// ("dev") is never offered an update.
func Newer(latest, current string) bool {
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// Checksum returns the SHA-256 of a file of the release, read from its
// checksums.txt.
func Checksum(ctx context.Context, r Release, name string) (string, error) {
	a := r.Asset("checksums.txt")
	if a == nil {
		return "", i18n.New("the release has no checksums.txt: the download cannot be checked")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", i18n.Errorf("%s is missing from checksums.txt", name)
}

// Download saves a file of the release to dest and checks its SHA-256.
func Download(ctx context.Context, a Asset, sha, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return i18n.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return i18n.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 1<<30))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dest)
		return i18n.Errorf("download interrupted: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, sha) {
		_ = os.Remove(dest)
		return i18n.Errorf("wrong checksum for %s: corrupted or modified file", a.Name)
	}
	return nil
}

// DataDir is the data folder of an installation: "data" next to the
// program in portable mode, otherwise the per-user folder.
func DataDir(installDir string) string {
	if st, err := os.Stat(filepath.Join(installDir, "data")); err == nil && st.IsDir() {
		return filepath.Join(installDir, "data")
	}
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "Cinearr")
	}
	return ""
}

// settings reads the port and the language of the interface.
func settings(dataDir string) (port int, lang string) {
	var s struct {
		Port       int    `json:"port"`
		UILanguage string `json:"uiLanguage"`
	}
	if b, err := os.ReadFile(filepath.Join(dataDir, "settings.json")); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Port == 0 {
		s.Port = 7979
	}
	return s.Port, s.UILanguage
}

// UseLanguage sets the language of the messages to the one of the
// interface of Cinearr.
func UseLanguage(dataDir string) {
	if _, lang := settings(dataDir); lang != "" {
		i18n.SetLanguage(lang)
	}
}

// QuitRunning asks a running Cinearr to stop and waits for it (true when it
// was running).
func QuitRunning(dataDir string) bool {
	port, _ := settings(dataDir)
	base := fmt.Sprintf("http://127.0.0.1:%d/api/", port)
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(base + "ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	req, _ := http.NewRequest(http.MethodPost, base+"quit", nil)
	req.Header.Set("X-Cinearr", "1")
	if resp, err := c.Do(req); err == nil {
		resp.Body.Close()
	}
	for i := 0; i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		resp, err := c.Get(base + "ping")
		if err != nil {
			return true
		}
		resp.Body.Close()
	}
	return true
}
