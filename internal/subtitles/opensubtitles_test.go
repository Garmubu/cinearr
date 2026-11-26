package subtitles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenSubtitlesTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Api-Key") != "good" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/infos/formats":
			_, _ = w.Write([]byte(`{"data":{"output_formats":["srt"]}}`))
		case "/login":
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["password"] != "pw" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"token":"tok","status":200}`))
		case "/infos/user":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"level":"Sub leecher","allowed_downloads":20,"remaining_downloads":18,"vip":false}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	o := &OpenSubtitles{APIKey: "good", Username: "garm", Password: "pw", baseURL: srv.URL + "/"}
	msg, err := o.Test(ctx)
	if err != nil || !strings.Contains(msg, "18 downloads left out of 20") {
		t.Fatalf("test = %q %v", msg, err)
	}
	if _, err := (&OpenSubtitles{APIKey: "bad", baseURL: srv.URL + "/"}).Test(ctx); err == nil || !strings.Contains(err.Error(), "API key refused") {
		t.Fatalf("bad key: %v", err)
	}
	if _, err := (&OpenSubtitles{APIKey: "good", Username: "garm", Password: "x", baseURL: srv.URL + "/"}).Test(ctx); err == nil || !strings.Contains(err.Error(), "password refused") {
		t.Fatalf("bad password: %v", err)
	}
	if _, err := (&OpenSubtitles{Username: "garm", Password: "pw"}).Test(ctx); err == nil {
		t.Fatal("missing key accepted")
	}
}

// TestOpenSubtitlesBuiltinKey: a refused built-in key is reported as such
// (not as a refused account), and cleared once the key is accepted again.
func TestOpenSubtitlesBuiltinKey(t *testing.T) {
	valid := "builtin"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Api-Key") != valid {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/infos/formats":
			_, _ = w.Write([]byte(`{"data":{}}`))
		case "/login":
			_, _ = w.Write([]byte(`{"token":"tok"}`))
		case "/subtitles":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer builtinRefused.Store(false)
	ctx := context.Background()
	o := &OpenSubtitles{APIKey: "builtin", Builtin: true, Username: "garm", Password: "pw", baseURL: srv.URL + "/"}
	if _, err := o.Search(ctx, Video{Title: "Inception", ImdbID: "tt1375666"}, []string{"en"}); err != nil || BuiltinKeyRefused() {
		t.Fatalf("search: %v (refused %v)", err, BuiltinKeyRefused())
	}
	valid = "another" // the built-in key is revoked
	_, err := o.Search(ctx, Video{Title: "Inception", ImdbID: "tt1375666"}, []string{"en"})
	if !errors.Is(err, ErrBuiltinKeyRefused) || !BuiltinKeyRefused() {
		t.Fatalf("revoked key: %v (refused %v)", err, BuiltinKeyRefused())
	}
	if !strings.Contains(err.Error(), "Settings › Subtitles") {
		t.Fatalf("message does not lead to the settings: %v", err)
	}
	// a key of the user's own is reported as such, without the flag
	builtinRefused.Store(false)
	if _, err := (&OpenSubtitles{APIKey: "mine", baseURL: srv.URL + "/"}).Test(ctx); !errors.Is(err, errOSKey) || BuiltinKeyRefused() {
		t.Fatalf("own key: %v", err)
	}
	valid = "builtin"
	builtinRefused.Store(true)
	if _, err := o.Search(ctx, Video{Title: "Inception", ImdbID: "tt1375666"}, []string{"en"}); err != nil || BuiltinKeyRefused() {
		t.Fatalf("key accepted again: %v (refused %v)", err, BuiltinKeyRefused())
	}
}
