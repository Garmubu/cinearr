package player

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArgs(t *testing.T) {
	o := Options{Title: "Inception (2010)", Subtitles: []string{"/m/Inception.fr.srt", "/m/Inception.en.srt"}, Fullscreen: true,
		AudioLang: "fre,fr", SubLang: "fr,en", Resume: true}
	vlc := strings.Join(Args(`C:\Program Files\VideoLAN\VLC\vlc.exe`, "/m/Inception.mkv", o), " ")
	for _, want := range []string{"--meta-title=Inception (2010)", "--fullscreen", "--sub-language=fr,en", "--audio-language=fre,fr",
		"--qt-continue=2", "--sub-file=/m/Inception.fr.srt", "--play-and-exit"} {
		if !strings.Contains(vlc, want) {
			t.Errorf("vlc args %q lack %q", vlc, want)
		}
	}
	if !strings.HasSuffix(vlc, "/m/Inception.mkv") {
		t.Errorf("vlc: file not last: %q", vlc)
	}
	mpc := Args(`C:\x\mpc-hc64.exe`, `D:\Films\Inception.mkv`, o)
	if mpc[0] != `D:\Films\Inception.mkv` || strings.Join(mpc[1:], " ") != "/play /close /fullscreen /sub /m/Inception.fr.srt /sub /m/Inception.en.srt" {
		t.Errorf("mpc args = %q", mpc)
	}
	mpv := strings.Join(Args("/usr/bin/mpv", "/m/Inception.mkv", o), " ")
	if !strings.Contains(mpv, "--fs") || !strings.HasSuffix(mpv, "-- /m/Inception.mkv") || !strings.Contains(mpv, "--alang=fre,fr") {
		t.Errorf("mpv args = %q", mpv)
	}
	if got := Args("/opt/other/player", "/m/x.mkv", Options{}); len(got) != 1 || got[0] != "/m/x.mkv" {
		t.Errorf("other args = %q", got)
	}
}

func TestFamily(t *testing.T) {
	for exe, want := range map[string]string{
		`C:\Program Files\VideoLAN\VLC\vlc.exe`:         VLC,
		"/Applications/VLC.app/Contents/MacOS/VLC":      VLC,
		"/var/lib/flatpak/exports/bin/org.videolan.VLC": VLC,
		`C:\MPC-HC\mpc-hc64.exe`:                        MPCHC,
		`C:\MPC-BE x64\mpc-be64.exe`:                    MPCHC,
		"/usr/bin/mpv":                                  MPV,
		"/opt/x/celluloid":                              Custom,
	} {
		if got := Family(exe); got != want {
			t.Errorf("Family(%q) = %q, want %q", exe, got, want)
		}
	}
}

func TestFindCustom(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "player")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := Find(Custom, exe, dir); err != nil || got != exe {
		t.Fatalf("Find custom = %q %v", got, err)
	}
	// old settings: a path without kind
	if got, err := Find("", exe, dir); err != nil || got != exe {
		t.Fatalf("Find legacy = %q %v", got, err)
	}
	if _, err := Find(Custom, filepath.Join(dir, "missing"), dir); err == nil {
		t.Fatal("missing custom player accepted")
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(content))
	}
	w.Close()
	return buf.Bytes()
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestInstallMPCHC(t *testing.T) {
	archive := makeZip(t, map[string]string{"MPC-HC.2.8.4.x64/mpc-hc64.exe": "exe", "MPC-HC.2.8.4.x64/Lang/x.dll": "dll"})
	digest := sum(archive)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			fmt.Fprintf(w, `{"tag_name":"2.8.4","assets":[
				{"name":"MPC-HC.2.8.4.x64.exe","browser_download_url":"%[1]s/bad","digest":"sha256:00"},
				{"name":"MPC-HC.2.8.4.x64.pdb.zip","browser_download_url":"%[1]s/bad","digest":"sha256:00"},
				{"name":"MPC-HC.2.8.4.x64.zip","browser_download_url":"%[1]s/mpc.zip","digest":"sha256:%[2]s"}]}`, srv.URL, digest)
		case "/mpc.zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := mpcRelease
	mpcRelease = srv.URL + "/release"
	defer func() { mpcRelease = old }()

	root := t.TempDir()
	tmp := filepath.Join(root, ".download")
	_ = os.MkdirAll(tmp, 0o755)
	exe, err := installMPCHC(context.Background(), root, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if exe != filepath.Join(root, "mpc-hc", "mpc-hc64.exe") || !isFile(exe) || !isFile(filepath.Join(root, "mpc-hc", "Lang", "x.dll")) {
		t.Fatalf("exe = %q", exe)
	}
	if !isFile(filepath.Join(root, "mpc-hc", "mpc-hc64.ini")) {
		t.Error("portable ini missing")
	}
}

func TestInstallVLCChecksum(t *testing.T) {
	archive := makeZip(t, map[string]string{"vlc-3.0.23/vlc.exe": "exe", "vlc-3.0.23/plugins/a.dll": "dll"})
	good := sum(archive)
	checksum := good
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/last/win64/":
			fmt.Fprint(w, `<a href="vlc-3.0.23-win64.7z">x</a> <a href="vlc-3.0.23-win64.exe">x</a> <a href="vlc-3.0.23-win64.zip">x</a>`)
		case "/last/win64/vlc-3.0.23-win64.zip":
			_, _ = w.Write(archive)
		case "/last/win64/vlc-3.0.23-win64.zip.sha256":
			fmt.Fprintf(w, "%s  vlc-3.0.23-win64.zip\n", checksum)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := vlcMirror
	vlcMirror = srv.URL + "/"
	defer func() { vlcMirror = old }()

	root := t.TempDir()
	tmp := filepath.Join(root, ".download")
	_ = os.MkdirAll(tmp, 0o755)
	exe, err := installVLCWindows(context.Background(), root, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if !isFile(exe) || !isFile(filepath.Join(root, "vlc", "plugins", "a.dll")) {
		t.Fatalf("exe = %q", exe)
	}
	if st, err := os.Stat(filepath.Join(root, "vlc", "portable")); err != nil || !st.IsDir() {
		t.Error("portable folder missing")
	}

	// a tampered archive is refused
	checksum = strings.Repeat("0", 64)
	_ = os.RemoveAll(tmp)
	_ = os.MkdirAll(tmp, 0o755)
	if _, err := installVLCWindows(context.Background(), root, tmp); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered archive: %v", err)
	}

	// no published checksum: nothing is downloaded nor installed
	checksum = ""
	_ = os.RemoveAll(filepath.Join(root, "vlc"))
	if _, err := installVLCWindows(context.Background(), root, tmp); err == nil || !strings.Contains(err.Error(), "no checksum") {
		t.Fatalf("archive without checksum: %v", err)
	}
	if isFile(filepath.Join(root, "vlc", "vlc.exe")) {
		t.Fatal("installed without checksum")
	}
}

func TestUnzipRefusesTraversal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "evil.zip")
	if err := os.WriteFile(src, makeZip(t, map[string]string{"../evil.exe": "x"}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := unzip(src, filepath.Join(dir, "out")); err == nil {
		t.Fatal("path traversal accepted")
	}
}
