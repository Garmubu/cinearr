package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedaction(t *testing.T) {
	u := RedactURL("https://user:pw@site.org/rss/0123456789abcdef0123456789abcdef/feed?apikey=SECRET&q=film&passkey=XYZ")
	for _, leak := range []string{"SECRET", "XYZ", "pw@", "0123456789abcdef0123456789abcdef"} {
		if strings.Contains(u, leak) {
			t.Fatalf("leak %q in %s", leak, u)
		}
	}
	if !strings.Contains(u, "q=film") {
		t.Fatalf("over-redacted: %s", u)
	}
	if s := RedactText(`GET /api?api_key=abc123&x=1 password=hunter2`); strings.Contains(s, "abc123") || strings.Contains(s, "hunter2") {
		t.Fatalf("text leak: %s", s)
	}
}

// The log keeps what helps a diagnosis and loses secrets and personal data
// (Sonarr's CleanseLogMessage cases).
func TestRedactText(t *testing.T) {
	leaks := map[string]string{
		`GET https://tracker.org/rss?uid=12345&torrent_pass=abcdefgh&q=x`:         "abcdefgh",
		`GET https://tracker.org/rss?authkey=SECRETKEY`:                           "SECRETKEY",
		`qbit login: username=admin&password=hunter2`:                             "hunter2",
		`Cookie: SID=0123456789; lang=fr`:                                         "0123456789",
		`Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijkl`: "eyJhbGciOiJIUzI1NiJ9",
		`token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abcdefghijkl in body`:         "eyJzdWIiOiIxIn0",
		`{"username":"jean","password":"hunter2","tmdbKey":"0f0f0f"}`:             "hunter2",
		`http://tracker.example.org/announce/0123456789abcdefXYZ`:                 "0123456789abcdefXYZ",
		`udp://tracker.example.org/0123456789abcdefxyz/announce`:                  "0123456789abcdefxyz",
		`http://admin:secretpw@192.168.1.5:8080/api/v2`:                           "secretpw",
		`saved C:\Users\Jean Dupont\Videos\Films\Inception.mkv`:                   "Jean Dupont",
		`saved C:\\Users\\Jean\\Videos in JSON`:                                   "Jean",
		`open /home/jean/Films/Inception (2010)`:                                  "jean",
		`open /Users/marie/Movies`:                                                "marie",
		`OpenSubtitles account jean.dupont@example.com refused`:                   "jean.dupont",
	}
	for in, leak := range leaks {
		if out := RedactText(in); strings.Contains(out, leak) {
			t.Errorf("leak %q in %q", leak, out)
		}
	}
	keep := []string{
		`[http] GET https://api.themoviedb.org/3/movie/27205?language=fr-FR -> 200 (120ms)`,
		`[import] /data/films/Inception (2010)/Inception (2010) [Bluray-1080p].mkv`,
		`magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=Film&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce`,
		`[scan] Library scan: 3 added, 2 files found, 0 missing, 1 not recognised`,
		`search Twin Peaks season=2&episode=3&imdbid=tt0098936&cat=5000`,
	}
	for _, in := range keep {
		if out := RedactText(in); out != in {
			t.Errorf("over-redacted:\n  %s\n  %s", in, out)
		}
	}
}

// Every line written to the log is cleaned.
func TestLogIsRedacted(t *testing.T) {
	dir := t.TempDir()
	logger, closeFn := Setup(dir, false)
	logger.Printf("client http://admin:secretpw@127.0.0.1:8080 password=hunter2")
	closeFn()
	b, err := os.ReadFile(filepath.Join(dir, LogFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secretpw") || strings.Contains(string(b), "hunter2") || !strings.Contains(string(b), "127.0.0.1:8080") {
		t.Fatalf("log = %s", b)
	}
}

func TestPanicCapture(t *testing.T) {
	dir := t.TempDir()
	_, closeFn := Setup(dir, false)
	defer closeFn()
	done := make(chan struct{})
	Go("test", func() {
		defer close(done)
		panic("boom")
	})
	<-done
	time.Sleep(50 * time.Millisecond)
	report, _ := CrashReport()
	if !strings.Contains(report, "boom") || !strings.Contains(report, "PANIC in test") {
		t.Fatalf("crash report: %q", report)
	}
	if err := AckCrash(); err != nil {
		t.Fatal(err)
	}
	if r, _ := CrashReport(); r != "" {
		t.Fatal("crash not acknowledged")
	}
}
