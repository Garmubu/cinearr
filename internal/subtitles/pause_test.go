package subtitles

import (
	"context"
	"errors"
	"testing"

	"cinearr/internal/backoff"
)

type failing struct{ calls int }

func (f *failing) Name() string { return "Failing" }
func (f *failing) Search(context.Context, Video, []string) ([]Candidate, error) {
	f.calls++
	return nil, errors.New("Failing: quota reached")
}
func (f *failing) Download(context.Context, Candidate) ([]byte, error) { return nil, errors.New("no") }

func TestProviderPause(t *testing.T) {
	defer Pauses.OK("Failing")
	f := &failing{}
	auto := backoff.Automatic(context.Background())
	_, errs := SearchAll(auto, []Provider{f}, Video{Title: "X"}, []string{"en"}, false)
	if len(errs) != 1 || f.calls != 1 {
		t.Fatalf("first search: %v", errs)
	}
	_, errs = SearchAll(auto, []Provider{f}, Video{Title: "X"}, []string{"en"}, false)
	if len(errs) != 1 || f.calls != 1 {
		t.Fatalf("paused provider queried: %d %v", f.calls, errs)
	}
	_, _ = SearchAll(context.Background(), []Provider{f}, Video{Title: "X"}, []string{"en"}, false)
	if f.calls != 2 {
		t.Fatal("the user's search must try a paused provider")
	}
}
