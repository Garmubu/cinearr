package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cinearr/internal/backoff"
)

// An indexer that keeps failing is skipped by automatic searches, still
// tried by the user's searches, and resumed by a success.
func TestIndexerPause(t *testing.T) {
	var calls atomic.Int32
	var down atomic.Bool
	down.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if down.Load() {
			http.Error(w, "maintenance", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel></channel></rss>`))
	}))
	defer srv.Close()
	m := NewManager(nil)
	cfgs := []Config{{ID: "t", Kind: "torznab", Name: "Test", Enabled: true, BaseURL: srv.URL, APIKey: "k"}}
	auto := backoff.Automatic(context.Background())
	q := MovieQuery{Title: "Inception", Year: 2010}

	_, res := m.SearchMovie(auto, cfgs, q)
	if len(res) != 1 || res[0].Error == "" || res[0].Paused {
		t.Fatalf("first search: %+v", res)
	}
	n := calls.Load()
	_, res = m.SearchMovie(auto, cfgs, q)
	if len(res) != 1 || !res[0].Paused || calls.Load() != n {
		t.Fatalf("paused indexer queried: %+v", res)
	}
	down.Store(false)
	_, res = m.SearchMovie(context.Background(), cfgs, q) // the user's search
	if len(res) != 1 || res[0].Error != "" || calls.Load() == n {
		t.Fatalf("manual search: %+v", res)
	}
	if _, paused := m.Status.Paused("t"); paused {
		t.Fatal("still paused after a success")
	}
}
