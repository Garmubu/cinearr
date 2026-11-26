package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuiltinKey: without a key of the user's own the built-in key is used;
// a refusal of that key is reported apart, and cleared when it works again.
func TestBuiltinKey(t *testing.T) {
	valid := "builtin"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != valid {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status_code":10,"status_message":"Suspended API key"}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"id":27205,"title":"Inception","release_date":"2010-07-15"}]}`))
	}))
	defer srv.Close()
	oldBase, oldKey := APIBase, BuiltinKey
	APIBase, BuiltinKey = srv.URL, ""
	defer func() { APIBase, BuiltinKey = oldBase, oldKey; builtinRefused.Store(false) }()
	ctx := context.Background()

	if c := New("", "en-US"); c.Configured() {
		t.Fatal("configured without any key")
	}
	BuiltinKey = "builtin"
	c := New("  ", "en-US")
	if !c.Configured() || !c.Builtin() {
		t.Fatal("built-in key not used")
	}
	if res, err := c.Search(ctx, "Inception", 2010); err != nil || len(res) != 1 {
		t.Fatalf("search: %v %v", res, err)
	}

	valid = "other" // the built-in key is suspended
	_, err := New("", "en-US").Search(ctx, "Inception", 2010)
	if !errors.Is(err, ErrBuiltinRefused) || !BuiltinRefused() {
		t.Fatalf("suspended key: %v (refused %v)", err, BuiltinRefused())
	}
	if !strings.Contains(err.Error(), "Settings › General") {
		t.Fatalf("message does not lead to the settings: %v", err)
	}

	// a key of the user's own: plain message, no flag change
	builtinRefused.Store(false)
	mine := New("mine", "en-US")
	if mine.Builtin() {
		t.Fatal("own key reported as built-in")
	}
	if _, err := mine.Search(ctx, "Inception", 2010); err == nil || errors.Is(err, ErrBuiltinRefused) || BuiltinRefused() {
		t.Fatalf("own key: %v", err)
	}

	valid = "builtin"
	builtinRefused.Store(true)
	if _, err := New("", "en-US").Search(ctx, "Inception", 2011); err != nil || BuiltinRefused() {
		t.Fatalf("key accepted again: %v (refused %v)", err, BuiltinRefused())
	}
}
