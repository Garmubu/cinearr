package indexer

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cinearr/internal/i18n"
	"cinearr/internal/indexer/cardigann"
	"cinearr/internal/model"
	"cinearr/internal/torrent"
)

// Torznab is a generic Torznab feed (Jackett, Prowlarr, sites exposing the
// API). Port of Radarr's Torznab indexer, reduced to movie search.
type Torznab struct {
	transport http.RoundTripper // proxy or direct (nil: default)
	ID        string
	Name      string
	BaseURL   string // e.g. http://host:9117/api/v2.0/indexers/xxx/results/torznab
	APIKey    string
	Priority  int
	client    *http.Client
	caps      *torznabCaps
}

type torznabCaps struct {
	movieSearch bool
	imdb        bool
	tmdb        bool
	tvSearch    bool
	tvImdb      bool
	tvTvdb      bool
	tvTmdb      bool
	tvSeason    bool
	tvEp        bool
}

// NewTorznab creates a Torznab client.
func NewTorznab(id, name, baseURL, apiKey string) *Torznab {
	return &Torznab{ID: id, Name: name, BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey,
		client: &http.Client{Timeout: 45 * time.Second}}
}

// SetTransport sets the route of the requests (proxy or direct).
func (t *Torznab) SetTransport(rt http.RoundTripper) {
	t.transport = rt
	t.client.Transport = rt
}

func (t *Torznab) endpoint(params url.Values) string {
	u := t.BaseURL
	if !strings.HasSuffix(u, "/api") {
		u += "/api"
	}
	if t.APIKey != "" {
		params.Set("apikey", t.APIKey)
	}
	return u + "?" + params.Encode()
}

func (t *Torznab) fetch(ctx context.Context, params url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.endpoint(params), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Cinearr")
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", t.Name, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func (t *Torznab) loadCaps(ctx context.Context) *torznabCaps {
	if t.caps != nil {
		return t.caps
	}
	c := &torznabCaps{}
	data, err := t.fetch(ctx, url.Values{"t": {"caps"}})
	if err == nil {
		var doc struct {
			Searching struct {
				MovieSearch struct {
					Available       string `xml:"available,attr"`
					SupportedParams string `xml:"supportedParams,attr"`
				} `xml:"movie-search"`
				TVSearch struct {
					Available       string `xml:"available,attr"`
					SupportedParams string `xml:"supportedParams,attr"`
				} `xml:"tv-search"`
			} `xml:"searching"`
		}
		if xml.Unmarshal(data, &doc) == nil {
			ms := doc.Searching.MovieSearch
			c.movieSearch = ms.Available == "yes"
			params := "," + strings.ToLower(strings.ReplaceAll(ms.SupportedParams, " ", "")) + ","
			c.imdb = strings.Contains(params, ",imdbid,")
			c.tmdb = strings.Contains(params, ",tmdbid,")
			ts := doc.Searching.TVSearch
			c.tvSearch = ts.Available == "yes"
			tp := "," + strings.ToLower(strings.ReplaceAll(ts.SupportedParams, " ", "")) + ","
			if ts.SupportedParams == "" {
				tp = ",q,season,ep,"
			}
			c.tvImdb = strings.Contains(tp, ",imdbid,")
			c.tvTvdb = strings.Contains(tp, ",tvdbid,")
			c.tvTmdb = strings.Contains(tp, ",tmdbid,")
			c.tvSeason = strings.Contains(tp, ",season,")
			c.tvEp = strings.Contains(tp, ",ep,")
		}
	}
	t.caps = c
	return c
}

// Search implements Searcher.
func (t *Torznab) Search(ctx context.Context, q cardigann.Query) ([]model.Release, error) {
	caps := t.loadCaps(ctx)
	params := url.Values{"extended": {"1"}, "limit": {"100"}}
	if q.TV {
		return t.searchTV(ctx, caps, q, params)
	}
	switch {
	case caps.movieSearch && caps.imdb && q.ImdbID != "":
		params.Set("t", "movie")
		params.Set("imdbid", strings.TrimPrefix(q.ImdbID, "tt"))
	case caps.movieSearch && caps.tmdb && q.TmdbID > 0:
		params.Set("t", "movie")
		params.Set("tmdbid", strconv.Itoa(q.TmdbID))
	default:
		if q.Keywords == "" {
			return nil, nil
		}
		params.Set("t", "search")
		params.Set("q", q.Keywords)
	}
	if len(q.Categories) > 0 {
		var cs []string
		for _, c := range q.Categories {
			cs = append(cs, strconv.Itoa(c))
		}
		params.Set("cat", strings.Join(cs, ","))
	}
	data, err := t.fetch(ctx, params)
	if err != nil {
		return nil, err
	}
	return t.parse(data)
}

// searchTV runs a series search (t=tvsearch with season and episode, or a
// keyword search "Title S02E05" when the feed has no series search).
func (t *Torznab) searchTV(ctx context.Context, caps *torznabCaps, q cardigann.Query, params url.Values) ([]model.Release, error) {
	if caps.tvSearch && caps.tvSeason {
		params.Set("t", "tvsearch")
		switch {
		case caps.tvImdb && q.ImdbID != "":
			params.Set("imdbid", strings.TrimPrefix(q.ImdbID, "tt"))
		case caps.tvTvdb && q.TvdbID > 0:
			params.Set("tvdbid", strconv.Itoa(q.TvdbID))
		case caps.tvTmdb && q.TmdbID > 0:
			params.Set("tmdbid", strconv.Itoa(q.TmdbID))
		default:
			if q.Keywords == "" {
				return nil, nil
			}
			params.Set("q", q.Keywords)
		}
		if q.Season > 0 {
			params.Set("season", strconv.Itoa(q.Season))
		}
		if q.Episode > 0 && caps.tvEp {
			params.Set("ep", strconv.Itoa(q.Episode))
		}
	} else {
		if q.Keywords == "" {
			return nil, nil
		}
		params.Set("t", "search")
		params.Set("q", strings.TrimSpace(q.Keywords+" "+q.EpisodeString()))
	}
	if len(q.Categories) > 0 {
		var cs []string
		for _, c := range q.Categories {
			cs = append(cs, strconv.Itoa(c))
		}
		params.Set("cat", strings.Join(cs, ","))
	}
	data, err := t.fetch(ctx, params)
	if err != nil {
		return nil, err
	}
	return t.parse(data)
}

type rssDoc struct {
	Error *struct {
		Description string `xml:"description,attr"`
	} `xml:"error"`
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title     string `xml:"title"`
	GUID      string `xml:"guid"`
	Link      string `xml:"link"`
	Comments  string `xml:"comments"`
	PubDate   string `xml:"pubDate"`
	Size      int64  `xml:"size"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
	} `xml:"enclosure"`
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

func (t *Torznab) parse(data []byte) ([]model.Release, error) {
	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, i18n.Errorf("%s: invalid Torznab feed", t.Name)
	}
	if doc.Error != nil {
		return nil, fmt.Errorf("%s: %s", t.Name, doc.Error.Description)
	}
	var out []model.Release
	for _, it := range doc.Channel.Items {
		r := model.Release{Title: it.Title, GUID: it.GUID, InfoURL: it.Comments, Indexer: t.Name, IndexerID: t.ID,
			IndexerPriority: t.Priority, DownloadVolumeFactor: 1, UploadVolumeFactor: 1, Size: it.Size}
		link := it.Enclosure.URL
		if link == "" {
			link = it.Link
		}
		if strings.HasPrefix(link, "magnet:") {
			r.MagnetURL = link
		} else {
			r.DownloadURL = link
		}
		if r.Size == 0 {
			r.Size = it.Enclosure.Length
		}
		if d, err := time.Parse(time.RFC1123Z, it.PubDate); err == nil {
			r.PublishDate = d
		} else if d, err := time.Parse(time.RFC1123, it.PubDate); err == nil {
			r.PublishDate = d
		}
		peers := -1
		for _, a := range it.Attrs {
			switch strings.ToLower(a.Name) {
			case "seeders":
				r.Seeders, _ = strconv.Atoi(a.Value)
			case "peers":
				peers, _ = strconv.Atoi(a.Value)
			case "infohash":
				r.InfoHash = strings.ToUpper(a.Value)
			case "magneturl":
				r.MagnetURL = a.Value
			case "size":
				if r.Size == 0 {
					r.Size, _ = strconv.ParseInt(a.Value, 10, 64)
				}
			case "imdb", "imdbid":
				if n, err := strconv.Atoi(strings.TrimPrefix(a.Value, "tt")); err == nil && n > 0 {
					r.ImdbID = fmt.Sprintf("tt%07d", n)
				}
			case "tmdbid":
				r.TmdbID, _ = strconv.Atoi(a.Value)
			case "downloadvolumefactor":
				r.DownloadVolumeFactor, _ = strconv.ParseFloat(a.Value, 64)
			case "uploadvolumefactor":
				r.UploadVolumeFactor, _ = strconv.ParseFloat(a.Value, 64)
			case "grabs":
				r.Grabs, _ = strconv.Atoi(a.Value)
			case "category":
				if c, err := strconv.Atoi(a.Value); err == nil {
					r.Categories = append(r.Categories, c)
				}
			}
		}
		r.Peers = peers
		if peers < 0 {
			r.Peers = r.Seeders
		}
		if r.InfoHash == "" && r.MagnetURL != "" {
			if m, err := torrent.ParseMagnet(r.MagnetURL); err == nil {
				r.InfoHash = m.InfoHash
			}
		}
		if r.GUID == "" {
			r.GUID = link
		}
		out = append(out, r)
	}
	return out, nil
}

// Download implements Searcher.
func (t *Torznab) Download(ctx context.Context, link string) (*cardigann.DownloadResult, error) {
	if strings.HasPrefix(link, "magnet:") {
		return &cardigann.DownloadResult{Magnet: link}, nil
	}
	noRedirect := &http.Client{Timeout: 60 * time.Second, Transport: t.transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if strings.HasPrefix(r.URL.String(), "magnet:") || len(via) > 8 {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Cinearr")
	resp, err := noRedirect.Do(req)
	if err != nil {
		var u *url.Error
		if errors.As(err, &u) && strings.HasPrefix(u.URL, "magnet:") {
			return &cardigann.DownloadResult{Magnet: u.URL}, nil
		}
		return nil, err
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("Location"); strings.HasPrefix(loc, "magnet:") {
		return &cardigann.DownloadResult{Magnet: loc}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, i18n.Errorf("%s: download refused (HTTP %d)", t.Name, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || body[0] != 'd' {
		return nil, i18n.Errorf("%s: the file received is not a .torrent", t.Name)
	}
	return &cardigann.DownloadResult{Torrent: body}, nil
}
