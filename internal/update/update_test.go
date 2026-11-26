package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"0.2.0", "0.1.0", true}, {"v0.1.1", "0.1.0", true}, {"1.0.0", "0.9.9", true}, {"0.10.0", "0.9.0", true},
		{"0.1.0", "0.1.0", false}, {"0.1.0", "0.2.0", false}, {"0.2.0", "dev", false}, {"x", "0.1.0", false},
		{"0.2.0-rc1", "0.1.0", true},
	} {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestLatestAndDownload(t *testing.T) {
	setup := []byte("MZ fake installer")
	sum := sha256.Sum256(setup)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/me/cinearr/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.2.0","html_url":"https://example.org/r","assets":[
				{"name":"Cinearr-Setup-0.2.0.exe","browser_download_url":"%[1]s/setup"},
				{"name":"checksums.txt","browser_download_url":"%[1]s/sums"}]}`, srv.URL)
		case "/setup":
			_, _ = w.Write(setup)
		case "/sums":
			fmt.Fprintf(w, "%s  cinearr_0.2.0_linux_amd64.tar.gz\n%s  Cinearr-Setup-0.2.0.exe\n", sha(nil), hex.EncodeToString(sum[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldBase, oldRepo := APIBase, Repo
	APIBase, Repo = srv.URL, "me/cinearr"
	defer func() { APIBase, Repo = oldBase, oldRepo }()

	ctx := context.Background()
	r, err := Latest(ctx)
	if err != nil || r.Version() != "0.2.0" {
		t.Fatalf("%+v %v", r, err)
	}
	a := r.Asset(SetupName("0.2.0"))
	if a == nil {
		t.Fatal("setup asset not found")
	}
	want, err := Checksum(ctx, *r, a.Name)
	if err != nil || want != hex.EncodeToString(sum[:]) {
		t.Fatalf("checksum %q %v", want, err)
	}
	dest := filepath.Join(t.TempDir(), "setup.exe")
	if err := Download(ctx, *a, want, dest); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); string(b) != string(setup) {
		t.Fatal("content")
	}
	if err := Download(ctx, *a, sha([]byte("other")), dest); err == nil {
		t.Fatal("tampered file accepted")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("tampered file kept")
	}
	Repo = "me/none"
	if _, err := Latest(ctx); err == nil {
		t.Fatal("missing repository accepted")
	}
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
