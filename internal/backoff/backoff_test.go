package backoff

import (
	"context"
	"testing"
	"time"
)

func TestEscalation(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	b := &Book{Now: func() time.Time { return now }}
	if _, p := b.Paused("x"); p {
		t.Fatal("paused without failure")
	}
	want := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}
	for i, d := range want {
		s := b.Fail("x", "down")
		if s.Failures != i+1 || s.PausedUntil != now.Add(d) {
			t.Fatalf("failure %d: %+v", i+1, s)
		}
	}
	for i := 0; i < 20; i++ {
		b.Fail("x", "down")
	}
	if s := b.Get("x"); s.PausedUntil != now.Add(24*time.Hour) {
		t.Fatalf("cap: %+v", s)
	}
	if _, p := b.Paused("x"); !p {
		t.Fatal("not paused")
	}
	now = now.Add(25 * time.Hour)
	if _, p := b.Paused("x"); p {
		t.Fatal("still paused after the period")
	}
	b.OK("x")
	if s := b.Get("x"); s.Failures != 0 || len(b.All()) != 0 {
		t.Fatalf("not cleared: %+v", s)
	}
	if IsAutomatic(context.Background()) || !IsAutomatic(Automatic(context.Background())) {
		t.Fatal("automatic flag")
	}
}
