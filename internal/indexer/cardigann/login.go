package cardigann

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"cinearr/internal/dom"
	"cinearr/internal/i18n"
	"cinearr/internal/torrent"
)

// ensureLogin ports DoLogin for the methods that do not need a human
// (post, form, cookie, get, oneurl). Captchas are reported as errors.
func (ix *Indexer) ensureLogin(ctx context.Context) error {
	login := ix.Def.Login
	if login == nil || ix.loggedIn {
		return nil
	}
	vars := ix.baseVars()
	hdr, err := ix.headers(firstHeaders(login.Headers, ix.Def.Search.Headers), vars)
	if err != nil {
		return err
	}
	hdr.Set("Referer", ix.SiteLink())
	tpl := func(s string) (string, error) { return ApplyTemplate(s, vars, nil) }

	switch login.Method {
	case "cookie":
		if strings.TrimSpace(ix.Settings["cookie"]) == "" {
			return i18n.Errorf("%s: enter the session cookie in the settings of the indexer", ix.Name)
		}
	case "post":
		form := url.Values{}
		for _, in := range login.Inputs {
			v, err := tpl(in.Value)
			if err != nil {
				return err
			}
			form.Set(in.Key, v)
		}
		p, err := tpl(login.Path)
		if err != nil {
			return err
		}
		u, err := ix.resolve(p, "")
		if err != nil {
			return err
		}
		if len(login.Cookies) > 0 {
			hdr.Set("Cookie", strings.Join(login.Cookies, "; "))
		}
		resp, body, err := ix.do(ctx, http.MethodPost, u, form, hdr, true)
		if err != nil {
			return err
		}
		if err := ix.loginErrors(resp, body, login.Error); err != nil {
			return err
		}
	case "get", "oneurl":
		p, err := tpl(login.Path)
		if err != nil {
			return err
		}
		if login.Method == "get" {
			q := url.Values{}
			for _, in := range login.Inputs {
				v, err := tpl(in.Value)
				if err != nil {
					return err
				}
				q.Add(in.Key, v)
			}
			if len(q) > 0 {
				p += "?" + q.Encode()
			}
		} else {
			one, _ := login.Inputs.Get("oneurl")
			o, err := tpl(one)
			if err != nil {
				return err
			}
			p += o
		}
		u, err := ix.resolve(p, "")
		if err != nil {
			return err
		}
		resp, body, err := ix.do(ctx, http.MethodGet, u, nil, hdr, true)
		if err != nil {
			return err
		}
		if err := ix.loginErrors(resp, body, login.Error); err != nil {
			return err
		}
	case "form":
		if err := ix.formLogin(ctx, vars, hdr); err != nil {
			return err
		}
	case "":
		return nil
	default:
		return i18n.Errorf("%s: login method %q not supported", ix.Name, login.Method)
	}

	// login.test: the page must contain the selector once logged in
	if login.Test != nil && login.Test.Path != "" {
		p, err := tpl(login.Test.Path)
		if err != nil {
			return err
		}
		u, err := ix.resolve(p, "")
		if err != nil {
			return err
		}
		resp, body, err := ix.do(ctx, http.MethodGet, u, nil, hdr, true)
		if err != nil {
			return err
		}
		if ix.loginNeeded(resp, body) {
			return i18n.Errorf("%s: login failed (check the credentials or the cookie)", ix.Name)
		}
	}
	ix.loggedIn = true
	return nil
}

func firstHeaders(a, b HeaderMap) HeaderMap {
	if a != nil {
		return a
	}
	return b
}

func (ix *Indexer) formLogin(ctx context.Context, vars Vars, hdr http.Header) error {
	login := ix.Def.Login
	if login.Captcha != nil {
		return i18n.Errorf("%s: this site asks for a captcha at login; use the “cookie” method if the definition offers it", ix.Name)
	}
	p, err := ApplyTemplate(login.Path, vars, nil)
	if err != nil {
		return err
	}
	loginURL, err := ix.resolve(p, "")
	if err != nil {
		return err
	}
	_, body, err := ix.do(ctx, http.MethodGet, loginURL, nil, hdr, true)
	if err != nil {
		return err
	}
	doc := dom.Parse(string(body))
	formSel := login.Form
	if formSel == "" {
		formSel = "form"
	}
	form, err := dom.QuerySelector(doc, formSel)
	if err != nil || form == nil {
		return i18n.Errorf("%s: login form not found (%s)", ix.Name, formSel)
	}
	pairs := url.Values{}
	inputs, _ := dom.QuerySelectorAll(form, "input")
	for _, in := range inputs {
		name, ok := in.Attr("name")
		if !ok {
			continue
		}
		if _, dis := in.Attr("disabled"); dis {
			continue
		}
		typ, _ := in.Attr("type")
		typ = strings.ToLower(typ)
		if typ == "checkbox" || typ == "radio" {
			if _, checked := in.Attr("checked"); !checked {
				continue
			}
		}
		v, _ := in.Attr("value")
		pairs.Set(name, v)
	}
	submit, _ := form.Attr("action")
	if login.Submitpath != "" {
		submit = login.Submitpath
	}
	for _, in := range login.Inputs {
		v, err := ApplyTemplate(in.Value, vars, nil)
		if err != nil {
			return err
		}
		key := in.Key
		if login.Selectors {
			el, _ := dom.QuerySelector(doc, in.Key)
			if el == nil {
				return i18n.Errorf("%s: login field not found (%s)", ix.Name, in.Key)
			}
			key, _ = el.Attr("name")
		}
		pairs.Set(key, v)
	}
	for k, sb := range login.Selectorinputs {
		v, err := ix.handleSelector(sb, doc, vars, !sb.Optional)
		if err != nil {
			return err
		}
		if sb.Optional && v == "" {
			continue
		}
		pairs.Set(k, v)
	}
	q := url.Values{}
	for k, sb := range login.Getselectorinputs {
		v, err := ix.handleSelector(sb, doc, vars, !sb.Optional)
		if err != nil {
			return err
		}
		if sb.Optional && v == "" {
			continue
		}
		q.Set(k, v)
	}
	if len(q) > 0 {
		submit += "?" + q.Encode()
	}
	submitURL, err := ix.resolve(submit, loginURL)
	if err != nil {
		return err
	}
	hdr = hdr.Clone()
	hdr.Set("Referer", loginURL)
	resp, rbody, err := ix.do(ctx, http.MethodPost, submitURL, pairs, hdr, true)
	if err != nil {
		return err
	}
	return ix.loginErrors(resp, rbody, login.Error)
}

func (ix *Indexer) loginErrors(resp *http.Response, body []byte, blocks []ErrorBlock) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return i18n.Errorf("%s: credentials refused (HTTP 401)", ix.Name)
	}
	if len(blocks) == 0 {
		return nil
	}
	return ix.checkErrors(dom.Parse(string(body)), blocks, ix.baseVars())
}

// loginNeeded ports CheckIfLoginIsNeeded.
func (ix *Indexer) loginNeeded(resp *http.Response, body []byte) bool {
	login := ix.Def.Login
	if login == nil {
		return false
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return true
	}
	if resp.StatusCode >= 400 {
		return true
	}
	if login.Test != nil && login.Test.Selector != "" {
		ct := resp.Header.Get("Content-Type")
		if ct == "" || strings.Contains(ct, "text/html") {
			n, err := dom.QuerySelectorAll(dom.Parse(string(body)), login.Test.Selector)
			if err == nil && len(n) == 0 {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Download

// DownloadResult is either a .torrent payload or a magnet link.
type DownloadResult struct {
	Torrent []byte
	Magnet  string
}

// Download resolves a release link (download block, details page selectors,
// infohash) and fetches the .torrent file or returns the magnet.
func (ix *Indexer) Download(ctx context.Context, link string) (*DownloadResult, error) {
	if strings.HasPrefix(link, "magnet:") {
		return &DownloadResult{Magnet: link}, nil
	}
	if err := ix.ensureLogin(ctx); err != nil {
		return nil, err
	}
	vars := ix.baseVars()
	if u, err := url.Parse(link); err == nil {
		vars[".DownloadUri.AbsoluteUri"] = u.String()
		vars[".DownloadUri.AbsolutePath"] = u.Path
		vars[".DownloadUri.Scheme"] = u.Scheme
		vars[".DownloadUri.Host"] = u.Hostname()
		vars[".DownloadUri.Port"] = u.Port()
		vars[".DownloadUri.PathAndQuery"] = u.RequestURI()
		vars[".DownloadUri.Query"] = "?" + u.RawQuery
		for k, v := range u.Query() {
			if len(v) > 0 {
				vars[".DownloadUri.Query."+k] = v[0]
			}
		}
	}
	var dlHeaders HeaderMap
	if ix.Def.Download != nil {
		dlHeaders = ix.Def.Download.Headers
	}
	hdr, err := ix.headers(firstHeaders(dlHeaders, ix.Def.Search.Headers), vars)
	if err != nil {
		return nil, err
	}
	method := http.MethodGet

	if dl := ix.Def.Download; dl != nil {
		if strings.EqualFold(dl.Method, "post") {
			method = http.MethodPost
		}
		var page []byte
		fetchPage := func() error {
			if page != nil {
				return nil
			}
			_, b, err := ix.do(ctx, http.MethodGet, link, nil, hdr, true)
			if err != nil {
				return err
			}
			page = b
			return nil
		}
		if dl.Before != nil {
			beforePath := dl.Before.Path
			if dl.Before.Pathselector != nil {
				if err := fetchPage(); err != nil {
					return nil, err
				}
				v, err := ix.matchSelector(page, *dl.Before.Pathselector, vars)
				if err != nil {
					return nil, err
				}
				beforePath = v
			}
			bp, err := ApplyTemplate(beforePath, vars, nil)
			if err != nil {
				return nil, err
			}
			bu, err := ix.resolve(bp, link)
			if err != nil {
				return nil, err
			}
			form := url.Values{}
			for _, in := range dl.Before.Inputs {
				v, err := ApplyTemplate(in.Value, vars, nil)
				if err != nil {
					return nil, err
				}
				form.Set(in.Key, v)
			}
			bm := http.MethodGet
			if strings.EqualFold(dl.Before.Method, "post") {
				bm = http.MethodPost
			} else if len(form) > 0 {
				bu += "?" + form.Encode()
				form = nil
			}
			_, b, err := ix.do(ctx, bm, bu, form, hdr, true)
			if err != nil {
				return nil, err
			}
			page = b
		}
		if ih := dl.Infohash; ih != nil && ih.Hash != nil && ih.Title != nil {
			if !ih.Usebeforeresponse || dl.Before == nil {
				page = nil
			}
			if err := fetchPage(); err != nil {
				return nil, err
			}
			hash, err := ix.matchSelector(page, *ih.Hash, vars)
			if err != nil || hash == "" {
				return nil, i18n.Errorf("%s: hash not found on the page", ix.Name)
			}
			title, _ := ix.matchSelector(page, *ih.Title, vars)
			return &DownloadResult{Magnet: torrent.BuildMagnet(hash, title)}, nil
		}
		if len(dl.Selectors) > 0 {
			for _, sel := range dl.Selectors {
				if !sel.Usebeforeresponse || dl.Before == nil {
					page = nil
				}
				if err := fetchPage(); err != nil {
					return nil, err
				}
				href, err := ix.matchSelector(page, sel, vars)
				if err != nil || href == "" {
					continue
				}
				target, err := ix.resolve(href, link)
				if err != nil {
					continue
				}
				if strings.HasPrefix(target, "magnet:") {
					return &DownloadResult{Magnet: target}, nil
				}
				res, err := ix.fetchTorrent(ctx, method, target, hdr)
				if err != nil {
					continue
				}
				return res, nil
			}
			return nil, i18n.Errorf("%s: no download link found on the page", ix.Name)
		}
	}
	return ix.fetchTorrent(ctx, method, link, hdr)
}

func (ix *Indexer) fetchTorrent(ctx context.Context, method, link string, hdr http.Header) (*DownloadResult, error) {
	resp, body, err := ix.doRaw(ctx, method, link, hdr)
	if err != nil {
		return nil, err
	}
	if loc := resp.Header.Get("Location"); loc != "" && strings.HasPrefix(loc, "magnet:") {
		return &DownloadResult{Magnet: loc}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, i18n.Errorf("%s: download refused (HTTP %d)", ix.Name, resp.StatusCode)
	}
	if len(body) == 0 || body[0] != 'd' {
		if strings.HasPrefix(string(body), "magnet:") {
			return &DownloadResult{Magnet: strings.TrimSpace(string(body))}, nil
		}
		return nil, i18n.New("the file received is not a valid .torrent (HTML page or unexpected content)")
	}
	return &DownloadResult{Torrent: body}, nil
}

// doRaw is like do but never transcodes the body and follows redirects
// except to magnet links.
func (ix *Indexer) doRaw(ctx context.Context, method, link string, hdr http.Header) (*http.Response, []byte, error) {
	for i := 0; i < 8; i++ {
		resp, body, err := ix.request(ctx, method, link, nil, hdr, false, true)
		if err != nil {
			return nil, nil, err
		}
		loc := resp.Header.Get("Location")
		if resp.StatusCode >= 300 && resp.StatusCode < 400 && loc != "" {
			if strings.HasPrefix(loc, "magnet:") {
				return resp, nil, nil
			}
			next, err := ix.resolve(loc, link)
			if err != nil {
				return nil, nil, err
			}
			link = next
			method = http.MethodGet
			continue
		}
		return resp, body, nil
	}
	return nil, nil, errors.New("too many redirects")
}

func (ix *Indexer) matchSelector(page []byte, sel SelectorField, vars Vars) (string, error) {
	s, err := ApplyTemplate(sel.Selector, vars, nil)
	if err != nil {
		return "", err
	}
	el, err := dom.QuerySelector(dom.Parse(string(page)), s)
	if err != nil {
		return "", err
	}
	if el == nil {
		return "", nil
	}
	var v string
	if sel.Attribute != "" {
		a, ok := el.Attr(sel.Attribute)
		if !ok {
			return "", fmt.Errorf("attribut %q absent", sel.Attribute)
		}
		v = a
	} else {
		v = el.TextContent()
	}
	return ApplyFilters(v, sel.Filters, vars)
}

// ---------------------------------------------------------------------------
// Encodings (UTF-8 and the single-byte Western encodings; others are read as
// UTF-8 with replacement characters).

var cp1252 = [32]rune{
	0x20AC, 0x81, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x8D, 0x017D, 0x8F,
	0x90, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x9D, 0x017E, 0x0178,
}

func decodeCharset(b []byte, enc string) []byte {
	e := strings.ToLower(strings.ReplaceAll(enc, "_", "-"))
	switch e {
	case "binary", "", "utf-8", "utf8":
		return b
	case "iso-8859-1", "latin1", "windows-1252", "cp1252", "iso-8859-15":
		if utf8.Valid(b) {
			return b
		}
		var sb strings.Builder
		for _, c := range b {
			switch {
			case c < 0x80:
				sb.WriteByte(c)
			case c < 0xA0 && e != "iso-8859-1":
				sb.WriteRune(cp1252[c-0x80])
			default:
				sb.WriteRune(rune(c))
			}
		}
		return []byte(sb.String())
	}
	return b
}

func encodeQueryComponent(s, enc string) string {
	e := strings.ToLower(enc)
	if e == "iso-8859-1" || e == "windows-1252" {
		var b strings.Builder
		for _, r := range s {
			if r < 0x80 {
				b.WriteString(url.QueryEscape(string(r)))
			} else if r < 0x100 {
				fmt.Fprintf(&b, "%%%02X", r)
			} else {
				b.WriteString(url.QueryEscape(string(r)))
			}
		}
		return b.String()
	}
	return url.QueryEscape(s)
}
