package qbit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Recent qBittorrent versions answer 204 No Content instead of "200 Ok.".
func TestNoContentResponses(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusNoContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v2/auth/login":
				http.SetCookie(w, &http.Cookie{Name: "SID", Value: "x"})
				if code == http.StatusOK {
					io.WriteString(w, "Ok.")
				} else {
					w.WriteHeader(code)
				}
			case "/api/v2/app/version":
				io.WriteString(w, "v5.1.2")
			case "/api/v2/app/webapiVersion":
				io.WriteString(w, "2.11.4")
			case "/api/v2/torrents/categories":
				io.WriteString(w, "{}")
			default:
				w.WriteHeader(code)
			}
		}))
		c := New(Settings{URL: srv.URL, Username: "u", Password: "p"})
		ctx := context.Background()
		if v, err := c.Version(ctx); err != nil || v != "v5.1.2" {
			t.Fatalf("HTTP %d: version %q %v", code, v, err)
		}
		if err := c.EnsureCategory(ctx); err != nil {
			t.Fatalf("HTTP %d: category %v", code, err)
		}
		if err := c.AddMagnet(ctx, "magnet:?xt=urn:btih:C9E15763F722F23E98A29DECDFAE341B98D53056", AddOptions{}); err != nil {
			t.Fatalf("HTTP %d: add %v", code, err)
		}
		srv.Close()
	}
	// wrong credentials still fail
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "Fails.") }))
	defer srv.Close()
	if _, err := New(Settings{URL: srv.URL}).Version(context.Background()); !errors.Is(err, ErrAuth) {
		t.Fatalf("bad credentials: %v", err)
	}
}
