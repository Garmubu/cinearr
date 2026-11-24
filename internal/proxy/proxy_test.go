package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestNormalize(t *testing.T) {
	s, err := Normalize(Settings{Type: "SOCKS5", Host: " socks5://nl.example.com:1080/ "})
	if err != nil || s.Type != "socks5" || s.Host != "nl.example.com" || s.Port != 1080 {
		t.Fatalf("%+v %v", s, err)
	}
	if u := (Settings{Type: "http", Host: "p.example", Port: 8080, Username: "me", Password: "pw"}).URL(); u.String() != "http://me:pw@p.example:8080" {
		t.Fatalf("url %s", u)
	}
	for _, bad := range []Settings{{Type: "ftp", Host: "x", Port: 1}, {Type: "http", Port: 80}, {Type: "http", Host: "x"}, {Type: "http", Host: "a b", Port: 1}} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	if s, err := Normalize(Settings{}); err != nil || s.URL() != nil {
		t.Fatal("no proxy")
	}
}

func TestLocal(t *testing.T) {
	for h, want := range map[string]bool{"localhost": true, "127.0.0.1": true, "192.168.1.5": true, "10.0.0.2": true, "[::1]": true,
		"jackett": true, "nas.local": true, "yggtorrent.top": false, "1.1.1.1": false, "api.themoviedb.org": false} {
		if Local(h) != want {
			t.Errorf("Local(%q) != %v", h, want)
		}
	}
}

// fakeProxy is an HTTP proxy that answers itself for plain http URLs.
func fakeProxy(t *testing.T, hits *atomic.Int32) Settings {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Host == "trace.example" {
			fmt.Fprint(w, "fl=1\nip=203.0.113.9\nloc=NL\n")
			return
		}
		fmt.Fprintf(w, "via proxy: %s", r.URL.Host)
	}))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	return Settings{Type: "http", Host: host, Port: p, Indexers: true}
}

func TestRouting(t *testing.T) {
	var hits atomic.Int32
	s := fakeProxy(t, &hits)
	Set(s)
	defer Set(Settings{})
	get := func(rt http.RoundTripper, url string) (string, error) {
		resp, err := (&http.Client{Transport: rt}).Get(url)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), nil
	}
	if body, err := get(Transport(Indexers, true), "http://tracker.example/search"); err != nil || body != "via proxy: tracker.example" {
		t.Fatalf("indexer not proxied: %q %v", body, err)
	}
	n := hits.Load()
	// excluded site and metadata (not chosen) go direct: the fake host does not resolve
	if _, err := get(Transport(Indexers, false), "http://tracker.example/search"); err == nil || hits.Load() != n {
		t.Fatal("excluded site went through the proxy")
	}
	if _, err := get(Transport(Metadata, true), "http://tmdb.example/3/movie"); err == nil || hits.Load() != n {
		t.Fatal("metadata went through the proxy")
	}
	// local addresses always direct
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "direct") }))
	defer local.Close()
	if body, err := get(Transport(Indexers, true), local.URL); err != nil || body != "direct" || hits.Load() != n {
		t.Fatalf("local address proxied: %q %v", body, err)
	}

	old := TraceURL
	TraceURL = "http://trace.example/cdn-cgi/trace"
	defer func() { TraceURL = old }()
	through, _, err := Test(context.Background(), s)
	if err != nil || through.IP != "203.0.113.9" || through.Country != "NL" {
		t.Fatalf("test: %+v %v", through, err)
	}
	if _, _, err := Test(context.Background(), Settings{Type: "http", Host: "127.0.0.1", Port: 1}); err == nil {
		t.Fatal("dead proxy answered")
	}
}
