// Package backoff pauses the sources that keep failing (indexers, subtitle
// providers), as Radarr's and Sonarr's provider status does: after each
// failure in a row, the automatic searches leave the source alone for a
// longer time (1 minute, 5, 15, 30, 1 hour, 3, 6, 12, then 24 hours).
// A site that is down is not hammered, and private trackers or quotas
// (OpenSubtitles) do not hold the account responsible for too many
// requests. Searches started by the user still try every source; a
// success clears the pause.
package backoff

import (
	"context"
	"sync"
	"time"
)

// Periods are Radarr's EscalationBackOff periods.
var Periods = []time.Duration{0, time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour}

// Status is the failure state of a source.
type Status struct {
	Failures    int       `json:"failures"`              // failures in a row
	PausedUntil time.Time `json:"pausedUntil,omitempty"` // automatic searches skip it until then
	LastError   string    `json:"lastError,omitempty"`
}

// Paused reports whether automatic searches skip the source at now.
func (s Status) Paused(now time.Time) bool { return now.Before(s.PausedUntil) }

// Book keeps the status of the sources by id.
type Book struct {
	mu sync.Mutex
	m  map[string]Status
	// Now is the clock (tests).
	Now func() time.Time
}

func (b *Book) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Get returns the status of a source.
func (b *Book) Get(id string) Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m[id]
}

// Paused reports whether a source is paused now.
func (b *Book) Paused(id string) (Status, bool) {
	s := b.Get(id)
	return s, s.Paused(b.now())
}

// Fail records a failure and returns the new status.
func (b *Book) Fail(id, msg string) Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[string]Status{}
	}
	s := b.m[id]
	s.Failures++
	s.LastError = msg
	s.PausedUntil = b.now().Add(Periods[min(s.Failures, len(Periods)-1)])
	b.m[id] = s
	return s
}

// OK clears the failures of a source.
func (b *Book) OK(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.m, id)
}

// All returns the sources that failed.
func (b *Book) All() map[string]Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]Status, len(b.m))
	for k, v := range b.m {
		out[k] = v
	}
	return out
}

type autoKey struct{}

// Automatic marks a context as an automatic search: paused sources are
// skipped.
func Automatic(ctx context.Context) context.Context { return context.WithValue(ctx, autoKey{}, true) }

// IsAutomatic reports whether ctx comes from an automatic search.
func IsAutomatic(ctx context.Context) bool { v, _ := ctx.Value(autoKey{}).(bool); return v }
