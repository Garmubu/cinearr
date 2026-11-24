package cardigann

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cinearr/internal/diag"
	"cinearr/internal/dom"
	"cinearr/internal/i18n"
	"cinearr/internal/model"
	"cinearr/internal/torrent"
)

// Category is a Newznab category.
type Category struct {
	ID   int
	Name string
}

// CategoryByName resolves "Movies/HD" to its id.
func CategoryByName(name string) (int, bool) {
	for _, c := range standardCategories {
		if strings.EqualFold(c.Name, name) {
			return c.ID, true
		}
	}
	return 0, false
}

type catMapping struct {
	tracker string
	newznab int
	desc    string
}

// Query is a search request.
type Query struct {
	Keywords   string
	ImdbID     string // tt1234567
	TmdbID     int
	Year       int
	Categories []int
	// series search (Prowlarr's TvSearchCriteria)
	TV      bool
	TvdbID  int
	Season  int
	Episode int // 0 with Season set: the whole season
}

// EpisodeString is Prowlarr's EpisodeSearchString: S02E05, or S02 for a
// season.
func (q Query) EpisodeString() string {
	if !q.TV || q.Season <= 0 {
		return ""
	}
	if q.Episode > 0 {
		return fmt.Sprintf("S%02dE%02d", q.Season, q.Episode)
	}
	return fmt.Sprintf("S%02d", q.Season)
}

// Indexer runs one Cardigann definition with user settings.
type Indexer struct {
	ID       string // instance id (user side)
	Name     string
	Def      *Definition
	Settings map[string]string
	BaseURL  string
	Priority int

	client      *http.Client
	noRedirect  *http.Client
	mu          sync.Mutex
	lastRequest time.Time
	delay       time.Duration
	cats        []catMapping
	defaultCats []string
	loggedIn    bool
	userAgent   string
}

// UserAgent sent to trackers (a common browser string, as Prowlarr does).
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// SetTransport sets the route of the requests (proxy or direct).
func (ix *Indexer) SetTransport(rt http.RoundTripper) {
	ix.client.Transport = rt
	ix.noRedirect.Transport = rt
}

// New creates an indexer instance.
func New(id string, def *Definition, settings map[string]string, baseURL string) *Indexer {
	jar, _ := cookiejar.New(nil)
	ix := &Indexer{ID: id, Name: def.Name, Def: def, Settings: settings, BaseURL: baseURL, userAgent: UserAgent}
	if ix.Settings == nil {
		ix.Settings = map[string]string{}
	}
	ix.client = &http.Client{Jar: jar, Timeout: 45 * time.Second}
	ix.noRedirect = &http.Client{Jar: jar, Timeout: 45 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	ix.delay = time.Duration(def.RequestDelay * float64(time.Second))
	// the route (proxy or direct) is set by the manager with SetTransport
	if ix.delay < 500*time.Millisecond {
		ix.delay = 500 * time.Millisecond
	}
	for _, kv := range def.Caps.Categories {
		if id, ok := CategoryByName(kv.Value); ok {
			ix.cats = append(ix.cats, catMapping{tracker: kv.Key, newznab: id})
		}
	}
	for _, cm := range def.Caps.Categorymappings {
		id, ok := 0, true
		if cm.Cat != "" {
			id, ok = CategoryByName(cm.Cat)
		}
		if !ok {
			continue
		}
		ix.cats = append(ix.cats, catMapping{tracker: cm.ID, newznab: id, desc: cm.Desc})
		if cm.Default {
			ix.defaultCats = append(ix.defaultCats, cm.ID)
		}
	}
	return ix
}

// SiteLink returns the base URL in use (ResolveSiteLink).
func (ix *Indexer) SiteLink() string {
	if ix.BaseURL != "" {
		for _, l := range ix.Def.Legacylinks {
			if strings.TrimRight(l, "/") == strings.TrimRight(ix.BaseURL, "/") {
				return ix.Def.Links[0]
			}
		}
		u := ix.BaseURL
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		return u
	}
	return ix.Def.Links[0]
}

func (ix *Indexer) resolve(path string, base string) (string, error) {
	if base == "" {
		base = ix.SiteLink()
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	return b.ResolveReference(r).String(), nil
}

// baseVars ports GetBaseTemplateVariables.
func (ix *Indexer) baseVars() Vars {
	v := Vars{
		".Config.sitelink": ix.SiteLink(),
		".True":            "True",
		".False":           nil,
		".Today.Year":      strconv.Itoa(time.Now().Year()),
	}
	for _, s := range ix.Def.Settings {
		name := ".Config." + s.Name
		val, has := ix.Settings[s.Name]
		switch s.Type {
		case "text", "password":
			if !has {
				val = s.Default
			}
			v[name] = val
		case "checkbox":
			if !has {
				val = s.Default
			}
			if b, _ := strconv.ParseBool(val); b {
				v[name] = ".True"
			} else {
				v[name] = nil
			}
		case "select":
			if !has || val == "" {
				val = s.Default
			}
			if _, ok := s.Options.Get(val); !ok && len(s.Options) > 0 {
				keys := make([]string, len(s.Options))
				for i, kv := range s.Options {
					keys[i] = kv.Key
				}
				sort.Strings(keys)
				if idx, err := strconv.Atoi(val); err == nil && idx >= 0 && idx < len(keys) {
					val = keys[idx]
				} else {
					val = keys[0]
				}
			}
			v[name] = val
		case "multi-select":
			var list []string
			if has && val != "" {
				list = strings.Split(val, ",")
			} else {
				list = s.Defaults
			}
			v[name] = list
		}
	}
	return v
}

func (ix *Indexer) headers(h HeaderMap, vars Vars) (http.Header, error) {
	out := http.Header{}
	for k, vals := range h {
		if len(vals) == 0 {
			continue
		}
		s, err := ApplyTemplate(vals[0], vars, nil)
		if err != nil {
			return nil, err
		}
		out.Set(k, s)
	}
	return out, nil
}

// do performs an HTTP request with rate limiting and the indexer cookies.
func (ix *Indexer) do(ctx context.Context, method, rawURL string, form url.Values, hdr http.Header, follow bool) (*http.Response, []byte, error) {
	return ix.request(ctx, method, rawURL, form, hdr, follow, false)
}

func (ix *Indexer) request(ctx context.Context, method, rawURL string, form url.Values, hdr http.Header, follow, raw bool) (*http.Response, []byte, error) {
	ix.mu.Lock()
	wait := time.Until(ix.lastRequest.Add(ix.delay))
	ix.lastRequest = time.Now().Add(max(wait, 0))
	ix.mu.Unlock()
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	var body io.Reader
	if method == http.MethodPost && form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", ix.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "fr-FR,fr;q=0.9,en;q=0.8")
	if method == http.MethodPost && form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	if c := ix.Settings["cookie"]; c != "" && ix.Def.Login != nil && ix.Def.Login.Method == "cookie" {
		req.Header.Set("Cookie", c)
	}
	client := ix.noRedirect
	if follow || ix.Def.Followredirect {
		client = ix.client
	}
	resp, err := client.Do(req)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, nil, i18n.Errorf("%s: address %s not found. The site may be blocked by your internet provider (DNS blocking) or this mirror no longer exists: choose another address in the settings of the indexer, or use a public DNS (1.1.1.1, 9.9.9.9)", ix.Name, req.URL.Hostname())
		}
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, nil, err
	}
	if raw {
		return resp, data, nil
	}
	return resp, decodeCharset(data, ix.Def.Encoding), nil
}

// ---------------------------------------------------------------------------
// Search

func (ix *Indexer) mapCategories(query []int) []string {
	if len(query) == 0 {
		return nil
	}
	expanded := map[int]bool{}
	for _, q := range query {
		expanded[q] = true
		if q < 100000 && q%1000 == 0 {
			for _, c := range standardCategories {
				if c.ID/1000 == q/1000 {
					expanded[c.ID] = true
				}
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range ix.cats {
		if expanded[m.newznab] && !seen[m.tracker] {
			out = append(out, m.tracker)
			seen[m.tracker] = true
		}
	}
	return out
}

func (ix *Indexer) trackerCatToNewznab(v string, byDesc bool) []int {
	var out []int
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		for _, m := range ix.cats {
			key := m.tracker
			if byDesc {
				key = m.desc
			}
			if key != "" && strings.EqualFold(key, part) && !containsInt(out, m.newznab) {
				out = append(out, m.newznab)
			}
		}
	}
	return out
}

func containsInt(l []int, v int) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

type searchRequest struct {
	url    string
	method string
	form   url.Values
	vars   Vars
	path   SearchPathBlock
	header http.Header
}

func (ix *Indexer) queryVars(q Query) Vars {
	v := ix.baseVars()
	for _, k := range []string{"Type", "Q", "Limit", "Offset", "Extended", "APIKey", "Genre", "Movie", "Year", "IMDBID",
		"IMDBIDShort", "TMDBID", "Series", "Ep", "Season", "TVDBID", "TVRageID", "TVMazeID", "TraktID", "DoubanID",
		"Episode", "Album", "Artist", "Label", "Track", "Author", "Title", "Publisher"} {
		v[".Query."+k] = nil
	}
	v[".Query.Type"] = "movie"
	if q.TV {
		v[".Query.Type"] = "tvsearch"
		if q.Season > 0 {
			v[".Query.Season"] = strconv.Itoa(q.Season)
		}
		if q.Episode > 0 {
			v[".Query.Ep"] = strconv.Itoa(q.Episode)
		}
		if q.TvdbID > 0 {
			v[".Query.TVDBID"] = strconv.Itoa(q.TvdbID)
		}
		if es := q.EpisodeString(); es != "" {
			v[".Query.Episode"] = es
		}
	}
	if q.Keywords != "" {
		v[".Query.Q"] = q.Keywords
	}
	if q.Year > 0 && q.Keywords == "" {
		v[".Query.Year"] = strconv.Itoa(q.Year)
	}
	if q.ImdbID != "" {
		v[".Query.IMDBID"] = q.ImdbID
		v[".Query.IMDBIDShort"] = strings.TrimPrefix(q.ImdbID, "tt")
	}
	if q.TmdbID > 0 {
		v[".Query.TMDBID"] = strconv.Itoa(q.TmdbID)
	}
	return v
}

func (ix *Indexer) buildRequests(q Query) ([]searchRequest, error) {
	search := ix.Def.Search
	vars := ix.queryVars(q)
	mapped := ix.mapCategories(q.Categories)
	if len(mapped) == 0 {
		mapped = ix.defaultCats
	}
	var tokens []string
	for _, k := range []string{"Q", "Series", "Movie", "Year"} {
		if s, _ := vars[".Query."+k].(string); strings.TrimSpace(s) != "" {
			tokens = append(tokens, s)
		}
	}
	if s, _ := vars[".Query.Episode"].(string); s != "" {
		tokens = append(tokens, s)
	}
	vars[".Query.Keywords"] = strings.Join(tokens, " ")
	kw, err := ApplyFilters(vars.str(".Query.Keywords"), search.Keywordsfilters, vars)
	if err != nil {
		return nil, err
	}
	vars[".Keywords"] = kw

	var reqs []searchRequest
	seen := map[string]bool{}
	for _, sp := range search.Paths {
		pv := vars.clone()
		pv[".Categories"] = mapped
		if len(sp.Categories) > 0 && len(mapped) > 0 {
			negate := sp.Categories[0] == "!"
			var inter, except []string
			for _, c := range mapped {
				if containsStr(sp.Categories, c) {
					inter = append(inter, c)
				} else {
					except = append(except, c)
				}
			}
			has := len(inter) > 0
			if negate {
				has = !has
			}
			if !has {
				continue
			}
			if negate {
				pv[".Categories"] = except
			} else {
				pv[".Categories"] = inter
			}
		}
		p, err := ApplyTemplate(sp.Path, pv, url.QueryEscape)
		if err != nil {
			return nil, err
		}
		p = strings.ReplaceAll(p, "+", "%20")
		searchURL, err := ix.resolve(p, "")
		if err != nil {
			return nil, err
		}
		method := http.MethodGet
		if strings.EqualFold(sp.Method, "post") {
			method = http.MethodPost
		}
		var pairs [][2]string
		inputsList := []OrderedStrings{}
		if sp.Inheritinputs == nil || *sp.Inheritinputs {
			inputsList = append(inputsList, search.Inputs)
		}
		inputsList = append(inputsList, sp.Inputs)
		for _, inputs := range inputsList {
			for _, in := range inputs {
				if in.Key == "$raw" {
					raw, err := ApplyTemplate(in.Value, pv, url.QueryEscape)
					if err != nil {
						return nil, err
					}
					for _, part := range strings.Split(raw, "&") {
						kv := strings.SplitN(part, "=", 2)
						if kv[0] == "" {
							continue
						}
						val := ""
						if len(kv) == 2 {
							val = kv[1]
						}
						k, _ := url.QueryUnescape(kv[0])
						vv, _ := url.QueryUnescape(val)
						pairs = append(pairs, [2]string{k, vv})
					}
					continue
				}
				val, err := ApplyTemplate(in.Value, pv, nil)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(val) != "" || search.AllowEmptyInputs {
					pairs = append(pairs, [2]string{in.Key, val})
				}
			}
		}
		var form url.Values
		if method == http.MethodGet && len(pairs) > 0 {
			sep := sp.Queryseparator
			if sep == "" {
				sep = "&"
			}
			var parts []string
			for _, kv := range pairs {
				parts = append(parts, encodeQueryComponent(kv[0], ix.Def.Encoding)+"="+encodeQueryComponent(kv[1], ix.Def.Encoding))
			}
			if strings.Contains(searchURL, "?") {
				searchURL += "&" + strings.Join(parts, sep)
			} else {
				searchURL += "?" + strings.Join(parts, sep)
			}
		} else if method == http.MethodPost {
			form = url.Values{}
			for _, kv := range pairs {
				form.Add(kv[0], kv[1])
			}
		}
		if method == http.MethodGet && seen[searchURL] {
			continue
		}
		seen[searchURL] = true
		hdr, err := ix.headers(search.Headers, pv)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, searchRequest{url: searchURL, method: method, form: form, vars: pv, path: sp, header: hdr})
	}
	return reqs, nil
}

// Search runs a query and returns parsed releases.
func (ix *Indexer) Search(ctx context.Context, q Query) ([]model.Release, error) {
	if err := ix.ensureLogin(ctx); err != nil {
		return nil, err
	}
	reqs, err := ix.buildRequests(q)
	if err != nil {
		return nil, err
	}
	var all []model.Release
	var firstErr error
	for _, r := range reqs {
		resp, body, err := ix.do(ctx, r.method, r.url, r.form, r.header, r.path.Followredirect)
		if err != nil {
			firstErr = err
			continue
		}
		if ix.loginNeeded(resp, body) {
			ix.loggedIn = false
			if err := ix.ensureLogin(ctx); err != nil {
				return nil, err
			}
			resp, body, err = ix.do(ctx, r.method, r.url, r.form, r.header, r.path.Followredirect)
			if err != nil {
				firstErr = err
				continue
			}
		}
		rels, err := ix.parseResponse(resp, body, r)
		if err != nil {
			diag.Debugf("[%s] unusable answer from %s: %v (start: %.300q)", ix.Name, diag.RedactURL(r.url), err, string(body))
			firstErr = err
			continue
		}
		diag.Debugf("[%s] %d results from %s", ix.Name, len(rels), diag.RedactURL(r.url))
		all = append(all, rels...)
	}
	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	for i := range all {
		all[i].Indexer = ix.Name
		all[i].IndexerID = ix.ID
		all[i].IndexerPriority = ix.Priority
		all[i].Private = ix.Def.Type == "private"
	}
	return all, nil
}

func (ix *Indexer) checkErrors(doc *dom.Node, blocks []ErrorBlock, vars Vars) error {
	for _, e := range blocks {
		if e.Selector == "" {
			continue
		}
		n, err := dom.QuerySelector(doc, e.Selector)
		if err != nil || n == nil {
			continue
		}
		msg := strings.TrimSpace(n.TextContent())
		if e.Message != nil {
			if m, err := ix.handleSelector(*e.Message, doc, vars, false); err == nil && m != "" {
				msg = m
			}
		}
		return i18n.Errorf("error returned by %s: %s", ix.Name, msg)
	}
	return nil
}

func (ix *Indexer) parseResponse(resp *http.Response, body []byte, r searchRequest) ([]model.Release, error) {
	if loc := resp.Header.Get("Location"); resp.StatusCode >= 300 && resp.StatusCode < 400 && loc != "" {
		if strings.Contains(strings.ToLower(loc), "login") {
			return nil, i18n.Errorf("%s: redirected to the login page (session expired or invalid credentials)", ix.Name)
		}
		return nil, i18n.Errorf("%s: unexpected redirect to %s", ix.Name, loc)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, i18n.Errorf("%s: HTTP answer %d", ix.Name, resp.StatusCode)
	}
	content := string(body)
	vars := r.vars
	search := ix.Def.Search
	var releases []model.Release

	if r.path.Response != nil && strings.EqualFold(r.path.Response.Type, "json") {
		if nr := r.path.Response.NoResultsMessage; nr != "" && strings.Contains(content, nr) {
			return nil, nil
		}
		var parsed any
		dec := json.NewDecoder(strings.NewReader(content))
		dec.UseNumber()
		if err := dec.Decode(&parsed); err != nil {
			return nil, i18n.Errorf("%s: invalid JSON answer", ix.Name)
		}
		if search.Rows.Count != nil {
			if cv, err := ix.handleJSONSelector(*search.Rows.Count, parsed, vars, true); err == nil {
				if n, err := strconv.Atoi(cv); err == nil && n < 1 {
					return nil, nil
				}
			}
		}
		rows, err := ix.jsonRows(parsed, search.Rows.Selector, vars)
		if err != nil {
			if search.Rows.MissingAttributeEqualsNoResults {
				return nil, nil
			}
			return nil, err
		}
		for _, row := range rows {
			selObj := row
			if search.Rows.Attribute != "" {
				v, ok := jsonSelect(row, search.Rows.Attribute)
				if !ok {
					if search.Rows.MissingAttributeEqualsNoResults {
						continue
					}
					continue
				}
				selObj = v
			}
			var mulRows []any
			if search.Rows.Multiple {
				switch x := selObj.(type) {
				case []any:
					mulRows = x
				case map[string]any:
					keys := make([]string, 0, len(x))
					for k := range x {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						mulRows = append(mulRows, x[k])
					}
				}
			} else {
				mulRows = []any{selObj}
			}
			for _, mr := range mulRows {
				rel, ok, err := ix.parseFields(vars, r.url, func(f SelectorBlock, required bool) (string, error) {
					parent := mr
					if strings.HasPrefix(f.Selector, "..") {
						parent = row
					}
					return ix.handleJSONSelector(f, parent, vars, required)
				})
				if err != nil {
					return nil, err
				}
				if ok {
					releases = append(releases, rel)
				}
			}
		}
		return ix.finish(releases), nil
	}

	if search.Preprocessingfilters != nil {
		c, err := ApplyFilters(content, search.Preprocessingfilters, vars)
		if err != nil {
			return nil, err
		}
		content = c
	}
	var doc *dom.Node
	if r.path.Response != nil && strings.EqualFold(r.path.Response.Type, "xml") {
		doc = dom.ParseXML(content)
	} else {
		doc = dom.Parse(content)
	}
	if err := ix.checkErrors(doc, search.Error, vars); err != nil {
		return nil, err
	}
	rowSel, err := ApplyTemplate(search.Rows.Selector, vars, nil)
	if err != nil {
		return nil, err
	}
	rows, err := dom.QuerySelectorAll(doc, rowSel)
	if err != nil {
		return nil, err
	}
	if after := search.Rows.After; after > 0 {
		var merged []*dom.Node
		for i := 0; i < len(rows); i++ {
			cur := rows[i]
			for j := 0; j < after && i+1+j < len(rows); j++ {
				next := rows[i+1+j]
				cur.AppendChildren(append([]*dom.Node(nil), next.Children...))
			}
			merged = append(merged, cur)
			i += after
		}
		rows = merged
	}
	skipped, firstSkip := 0, ""
	defer func() {
		if skipped > 0 {
			diag.Printf("[%s] %d result row(s) skipped out of %d: %s", ix.Name, skipped, len(rows), firstSkip)
		}
	}()
	for _, row := range rows {
		rel, ok, err := ix.parseFields(vars, r.url, func(f SelectorBlock, required bool) (string, error) {
			return ix.handleSelector(f, row, vars, required)
		})
		if err != nil {
			// Prowlarr logs and skips rows that fail to parse
			skipped++
			if firstSkip == "" {
				firstSkip = err.Error()
			}
			diag.Debugf("[%s] row skipped: %v", ix.Name, err)
			continue
		}
		if !ok {
			continue
		}
		if rel.PublishDate.IsZero() && search.Rows.Dateheaders != nil {
			prev := row.PreviousElementSibling()
			if prev == nil && row.ParentElement() != nil {
				prev = row.ParentElement().PreviousElementSibling()
			}
			for prev != nil {
				if v, err := ix.handleSelector(*search.Rows.Dateheaders, prev, vars, true); err == nil {
					if t, err := FromUnknown(v, ""); err == nil {
						rel.PublishDate = t
					}
					break
				}
				cur := prev
				prev = cur.PreviousElementSibling()
				if prev == nil && cur.ParentElement() != nil {
					prev = cur.ParentElement().PreviousElementSibling()
				}
			}
		}
		releases = append(releases, rel)
	}
	return ix.finish(releases), nil
}

func (ix *Indexer) finish(rels []model.Release) []model.Release {
	for i := range rels {
		r := &rels[i]
		if r.MagnetURL == "" && r.InfoHash != "" && ix.Def.Type != "private" {
			r.MagnetURL = torrent.BuildMagnet(r.InfoHash, r.Title)
		}
		if r.MagnetURL != "" && r.InfoHash == "" {
			if m, err := torrent.ParseMagnet(r.MagnetURL); err == nil {
				r.InfoHash = m.InfoHash
			}
		}
		if r.GUID == "" {
			r.GUID = firstNonEmpty(r.DownloadURL, r.MagnetURL, r.InfoURL, r.Title)
		}
	}
	return rels
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

var optionalFields = map[string]bool{"imdb": true, "imdbid": true, "tmdbid": true, "rageid": true, "tvdbid": true,
	"tvmazeid": true, "traktid": true, "doubanid": true, "poster": true, "banner": true, "description": true, "genre": true}

// parseFields ports the field loop of CardigannParser.
func (ix *Indexer) parseFields(vars Vars, searchURL string, get func(SelectorBlock, bool) (string, error)) (model.Release, bool, error) {
	var rel model.Release
	rel.DownloadVolumeFactor, rel.UploadVolumeFactor = 1, 1
	peersSet := false
	for _, field := range ix.Def.Search.Fields {
		parts := strings.Split(field.Key, "|")
		name := parts[0]
		mods := parts[1:]
		key := ".Result." + name
		optional := optionalFields[field.Key] || field.Block.Optional || containsStr(mods, "optional")
		value, err := get(field.Block, !optional)
		if err == nil && optional && strings.TrimSpace(value) == "" {
			def, derr := ApplyTemplate(field.Block.Default, vars, nil)
			if derr != nil || strings.TrimSpace(def) == "" {
				vars[key] = nil
				continue
			}
			value = def
		}
		if err != nil {
			if _, exists := vars[key]; !exists || optional {
				vars[key] = nil
			}
			if optional {
				continue
			}
			return rel, false, fmt.Errorf("field %s: %w", field.Key, err)
		}
		out, err := ix.applyField(&rel, name, value, mods, searchURL, &peersSet)
		if err != nil {
			if optional {
				vars[key] = nil
				continue
			}
			return rel, false, fmt.Errorf("field %s: %w", field.Key, err)
		}
		vars[key] = out
	}
	// row filters (andmatch...) are informational in Prowlarr
	if rel.Title == "" {
		return rel, false, nil
	}
	return rel, true, nil
}

func (ix *Indexer) applyField(r *model.Release, name, value string, mods []string, searchURL string, peersSet *bool) (string, error) {
	switch name {
	case "download":
		if value == "" {
			r.DownloadURL = ""
			return "", nil
		}
		if strings.HasPrefix(value, "magnet:") {
			r.MagnetURL = value
		} else {
			u, err := ix.resolve(value, searchURL)
			if err != nil {
				return "", err
			}
			r.DownloadURL = u
			value = u
		}
		r.GUID = value
	case "magnet":
		r.MagnetURL = value
	case "infohash":
		r.InfoHash = value
	case "details":
		u, err := ix.resolve(value, searchURL)
		if err != nil {
			return "", err
		}
		r.InfoURL = u
		value = u
	case "comments":
		if r.InfoURL == "" {
			if u, err := ix.resolve(value, searchURL); err == nil {
				r.InfoURL = u
			}
		}
	case "title":
		if containsStr(mods, "append") {
			r.Title += value
		} else {
			r.Title = value
		}
		value = r.Title
	case "category", "categorydesc":
		cats := ix.trackerCatToNewznab(value, name == "categorydesc")
		if len(cats) > 0 {
			if r.Categories == nil || containsStr(mods, "noappend") {
				r.Categories = cats
			} else {
				for _, c := range cats {
					if !containsInt(r.Categories, c) {
						r.Categories = append(r.Categories, c)
					}
				}
			}
		}
		parts := make([]string, len(r.Categories))
		for i, c := range r.Categories {
			parts[i] = strconv.Itoa(c)
		}
		value = strings.Join(parts, ",")
	case "size":
		r.Size = GetBytes(value)
		value = strconv.FormatInt(r.Size, 10)
	case "leechers":
		l := CoerceInt(value)
		if l >= 5000000 {
			l = 0
		}
		r.Peers += l
		*peersSet = true
		value = strconv.Itoa(l)
	case "seeders":
		s := CoerceInt(value)
		if s >= 5000000 {
			s = 0
		}
		r.Seeders = s
		r.Peers += s
		*peersSet = true
		value = strconv.Itoa(s)
	case "date":
		// an unreadable date must not drop the release (the date only
		// affects the displayed age)
		t, err := FromUnknown(value, "")
		if err != nil {
			diag.Debugf("[%s] unreadable date %q", ix.Name, value)
			return "", nil
		}
		r.PublishDate = t
		value = t.Format(time.RFC1123Z)
	case "files":
		r.Files = CoerceInt(value)
		value = strconv.Itoa(r.Files)
	case "grabs":
		r.Grabs = CoerceInt(value)
		value = strconv.Itoa(r.Grabs)
	case "downloadvolumefactor":
		r.DownloadVolumeFactor = CoerceDouble(value)
	case "uploadvolumefactor":
		r.UploadVolumeFactor = CoerceDouble(value)
	case "minimumratio":
		r.MinimumRatio = CoerceDouble(value)
	case "minimumseedtime":
		r.MinimumSeedTime = LongFromString(value)
	case "imdb", "imdbid":
		if n := LongFromString(value); n > 0 {
			r.ImdbID = fmt.Sprintf("tt%07d", n)
			value = strconv.FormatInt(n, 10)
		}
	case "tmdbid":
		r.TmdbID = int(LongFromString(value))
		value = strconv.Itoa(r.TmdbID)
	}
	return value, nil
}

// handleSelector ports CardigannBase.HandleSelector.
func (ix *Indexer) handleSelector(sel SelectorBlock, node *dom.Node, vars Vars, required bool) (string, error) {
	if sel.Text != nil {
		t, err := ApplyTemplate(*sel.Text, vars, nil)
		if err != nil {
			return "", err
		}
		return ApplyFilters(t, sel.Filters, vars)
	}
	selection := node
	if sel.Selector != "" {
		s, err := ApplyTemplate(sel.Selector, vars, nil)
		if err != nil {
			return "", err
		}
		root := node
		if strings.HasPrefix(s, ":root") {
			s = strings.TrimSpace(strings.TrimPrefix(s, ":root"))
			root = node.Root()
		}
		compiled, err := dom.Compile(s)
		if err != nil {
			return "", err
		}
		if compiled.Match(node) && root == node {
			selection = node
		} else {
			selection = compiled.Query(root)
		}
		if selection == nil {
			if required {
				return "", fmt.Errorf("selector %q matches nothing", s)
			}
			return "", nil
		}
	}
	if sel.Remove != "" {
		if rm, err := dom.QuerySelectorAll(selection, sel.Remove); err == nil {
			for _, n := range rm {
				n.Remove()
			}
		}
	}
	var value string
	switch {
	case len(sel.Case) > 0:
		found := false
		for _, c := range sel.Case {
			ok, err := dom.Matches(selection, c.Key)
			if err != nil {
				return "", err
			}
			if !ok {
				n, _ := dom.QuerySelector(selection, c.Key)
				ok = n != nil
			}
			if ok {
				v, err := ApplyTemplate(c.Value, vars, nil)
				if err != nil {
					return "", err
				}
				value = v
				found = true
				break
			}
		}
		if !found {
			if required {
				return "", errors.New("no case of the selector matches")
			}
			return "", nil
		}
	case sel.Attribute != "":
		v, ok := selection.Attr(sel.Attribute)
		if !ok {
			if required {
				return "", fmt.Errorf("attribut %q absent", sel.Attribute)
			}
			return "", nil
		}
		value = v
	default:
		value = selection.TextContent()
	}
	return ApplyFilters(strings.TrimSpace(value), sel.Filters, vars)
}

// handleJSONSelector ports CardigannBase.HandleJsonSelector.
func (ix *Indexer) handleJSONSelector(sel SelectorBlock, parent any, vars Vars, required bool) (string, error) {
	if sel.Text != nil {
		t, err := ApplyTemplate(*sel.Text, vars, nil)
		if err != nil {
			return "", err
		}
		return ApplyFilters(t, sel.Filters, vars)
	}
	var value string
	has := false
	if sel.Selector != "" {
		s, err := ApplyTemplate(strings.TrimLeft(sel.Selector, "."), vars, nil)
		if err != nil {
			return "", err
		}
		path, ok := jsonFieldSelector(parent, s)
		var tok any
		if ok {
			tok, ok = jsonSelect(parent, path)
		}
		if !ok {
			if required {
				return "", fmt.Errorf("JSON selector %q matches nothing", s)
			}
			return "", nil
		}
		value = jsonString(tok)
		has = true
	}
	if len(sel.Case) > 0 {
		found := false
		for _, c := range sel.Case {
			if (has && value == c.Key) || c.Key == "*" {
				v, err := ApplyTemplate(c.Value, vars, nil)
				if err != nil {
					return "", err
				}
				value, found = v, true
				break
			}
		}
		if !found {
			if required {
				return "", errors.New("no JSON case matches")
			}
			return "", nil
		}
	}
	return ApplyFilters(strings.TrimSpace(value), sel.Filters, vars)
}

// jsonRows ports JsonParseRowsSelector.
func (ix *Indexer) jsonRows(parsed any, rowSelector string, vars Vars) ([]any, error) {
	rs, err := ApplyTemplate(rowSelector, vars, nil)
	if err != nil {
		return nil, err
	}
	base := strings.SplitN(rs, ":", 2)[0]
	tok, ok := jsonSelect(parsed, base)
	if !ok {
		return nil, fmt.Errorf("JSON row selector %q not found", base)
	}
	arr, ok := tok.([]any)
	if !ok {
		if tok == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("JSON row selector %q: not an array", base)
	}
	filter := rs[len(base):]
	var out []any
	for _, row := range arr {
		if _, ok := jsonFieldSelector(row, filter); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// jsonFieldSelector ports JsonParseFieldSelector (":has(x)", ":not(x)",
// ":contains(x)" filters).
func jsonFieldSelector(obj any, sel string) (string, bool) {
	base := strings.SplitN(sel, ":", 2)[0]
	cur := obj
	if strings.TrimSpace(base) != "" {
		v, ok := jsonSelect(obj, base)
		if !ok {
			return "", false
		}
		cur = v
	}
	for _, f := range parseJSONFilters(sel[len(base):]) {
		switch f[0] {
		case "has":
			if _, ok := jsonFieldSelector(cur, f[1]); !ok {
				return "", false
			}
		case "not":
			if _, ok := jsonFieldSelector(cur, f[1]); ok {
				return "", false
			}
		case "contains":
			b, _ := json.Marshal(cur)
			if !strings.Contains(string(b), f[1]) && !strings.Contains(jsonString(cur), f[1]) {
				return "", false
			}
		}
	}
	return base, true
}

func parseJSONFilters(s string) [][2]string {
	var out [][2]string
	for len(s) > 0 && s[0] == ':' {
		open := strings.IndexByte(s, '(')
		if open < 0 {
			break
		}
		name := s[1:open]
		depth := 0
		end := -1
		for i := open; i < len(s); i++ {
			if s[i] == '(' {
				depth++
			} else if s[i] == ')' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}
		if end < 0 {
			break
		}
		out = append(out, [2]string{name, s[open+1 : end]})
		s = s[end+1:]
	}
	return out
}
