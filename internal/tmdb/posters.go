package tmdb

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Posters are shown in the original language of the film or series, not
// in the language of the interface: TMDB returns the localised poster
// (translated title) with the language of the pages, so the poster is
// taken from the images of the title. Titles, summaries and genres stay in
// the language of the interface.

type imageList struct {
	Posters []struct {
		FilePath string  `json:"file_path"`
		Language *string `json:"iso_639_1"` // null: poster without text
		Votes    float64 `json:"vote_average"`
		Count    int     `json:"vote_count"`
	} `json:"posters"`
}

// posterMemo remembers the poster chosen for each title for the life of
// the process (posters rarely change), on top of the 30-minute cache.
type posterMemo struct {
	mu sync.Mutex
	m  map[string]string
}

func (p *posterMemo) get(k string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.m[k]
	return v, ok
}

func (p *posterMemo) put(k, v string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.m == nil || len(p.m) > 5000 {
		p.m = map[string]string{}
	}
	p.m[k] = v
}

func (p *posterMemo) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m = nil
}

var posters posterMemo

// interfaceLanguage is the ISO 639-1 code of the language of the pages
// ("fr" for "fr-FR").
func (c *Client) interfaceLanguage() string {
	l, _, _ := strings.Cut(c.language, "-")
	return strings.ToLower(l)
}

// OriginalPoster returns the poster of a film (tv false) or a series in its
// original language (ISO 639-1), else a poster without text, else current.
// When the original language is the one of the pages, current is already
// the right poster.
func (c *Client) OriginalPoster(ctx context.Context, tv bool, id int, original, current string) string {
	if original == "" || strings.EqualFold(original, c.interfaceLanguage()) {
		return current
	}
	if p, err := c.LookupOriginalPoster(ctx, tv, id, original); err == nil && p != "" {
		return p
	}
	return current
}

// LookupOriginalPoster returns the best poster of a title in its original
// language, else one without text, or "" when TMDB has neither.
func (c *Client) LookupOriginalPoster(ctx context.Context, tv bool, id int, original string) (string, error) {
	original = strings.ToLower(strings.TrimSpace(original))
	if id == 0 || original == "" || !c.Configured() {
		return "", nil
	}
	kind := "movie"
	if tv {
		kind = "tv"
	}
	key := kind + ":" + strconv.Itoa(id)
	if p, ok := posters.get(key); ok {
		return p, nil
	}
	var imgs imageList
	q := url.Values{"language": {original}, "include_image_language": {original + ",null"}}
	if err := c.get(ctx, "/"+kind+"/"+strconv.Itoa(id)+"/images", q, &imgs); err != nil {
		return "", err // not remembered: tried again next time
	}
	best, bestScore := "", -1.0
	for _, p := range imgs.Posters {
		score := p.Votes + float64(p.Count)/1000
		switch {
		case p.Language != nil && strings.EqualFold(*p.Language, original):
			score += 100 // the original language first
		case p.Language == nil:
			// a poster without text comes next
		default:
			continue
		}
		if p.FilePath != "" && score > bestScore {
			best, bestScore = p.FilePath, score
		}
	}
	posters.put(key, best)
	return best, nil
}

// OriginalPosters sets the poster of every film or series of a list to its
// original-language poster, a few requests at a time.
func (c *Client) OriginalPosters(ctx context.Context, tv bool, list []Summary) {
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i := range list {
		s := &list[i]
		if s.OriginalLanguage == "" || strings.EqualFold(s.OriginalLanguage, c.interfaceLanguage()) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			s.PosterPath = c.OriginalPoster(ctx, tv, s.ID, s.OriginalLanguage, s.PosterPath)
		}()
	}
	wg.Wait()
}
