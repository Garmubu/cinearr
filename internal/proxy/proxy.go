// Package proxy sends some of Cinearr's requests through an HTTP or SOCKS5
// proxy, as Radarr's and Prowlarr's proxy settings do. Most VPN services
// provide one: the searches on the indexers (and, if chosen, TMDB and the
// subtitle providers) then leave from the VPN while the rest of the
// computer does not. The host names are resolved by the proxy, so sites
// blocked by the DNS of the internet provider are reached too. Local
// addresses (the torrent client, Jackett or Prowlarr on the network) are
// always reached directly.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"cinearr/internal/diag"
	"cinearr/internal/i18n"
)

// Settings describe the proxy (persisted in the settings).
type Settings struct {
	Type     string `json:"type"` // "" (none), "http", "socks5"
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// Indexers sends the searches and the .torrent downloads through the
	// proxy (sites can still be excluded one by one).
	Indexers bool `json:"indexers"`
	// Metadata sends TMDB, the subtitle providers and the indexer
	// definitions through the proxy too.
	Metadata bool `json:"metadata"`
}

// Defaults: no proxy; once one is set, the indexers use it.
func Defaults() Settings { return Settings{Indexers: true} }

// Kind is the family of a request.
type Kind int

const (
	// Indexers are the searches and .torrent downloads.
	Indexers Kind = iota
	// Metadata is TMDB, the subtitle providers and the definitions.
	Metadata
)

// Normalize checks and cleans the settings typed by the user.
func Normalize(s Settings) (Settings, error) {
	s.Type = strings.ToLower(strings.TrimSpace(s.Type))
	if s.Type == "socks" || s.Type == "socks5h" {
		s.Type = "socks5"
	}
	if s.Type == "https" {
		s.Type = "http"
	}
	if s.Type == "" {
		return s, nil
	}
	if s.Type != "http" && s.Type != "socks5" {
		return s, i18n.Errorf("unknown proxy type: %s", s.Type)
	}
	h := strings.TrimSpace(s.Host)
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	h = strings.TrimRight(h, "/")
	if host, port, err := net.SplitHostPort(h); err == nil && s.Port == 0 {
		if p, err := strconv.Atoi(port); err == nil {
			h, s.Port = host, p
		}
	}
	if h == "" || strings.ContainsAny(h, "/@ ") {
		return s, i18n.New("proxy address missing or invalid (for example nl.socks.example.com)")
	}
	if s.Port <= 0 || s.Port > 65535 {
		return s, i18n.New("proxy port missing or invalid (1 to 65535)")
	}
	s.Host = h
	return s, nil
}

// URL returns the proxy URL with its credentials (nil without proxy).
func (s Settings) URL() *url.URL {
	if s.Type == "" || s.Host == "" {
		return nil
	}
	u := &url.URL{Scheme: s.Type, Host: net.JoinHostPort(s.Host, strconv.Itoa(s.Port))}
	if s.Username != "" {
		u.User = url.UserPassword(s.Username, s.Password)
	}
	return u
}

// applies reports whether a kind of request goes through the proxy.
func (s Settings) applies(k Kind) bool {
	return s.URL() != nil && (k == Indexers && s.Indexers || k == Metadata && s.Metadata)
}

var (
	mu      sync.RWMutex
	current Settings
	proxied *http.Transport // transport through current, rebuilt on change
	// direct is the standard transport (the one http.DefaultTransport was
	// before diag wrapped it).
	direct = standard()
)

func standard() *http.Transport {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		return t.Clone()
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true, MaxIdleConns: 100,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second}
}

// Set installs the proxy settings (at startup and when they are saved).
func Set(s Settings) {
	mu.Lock()
	defer mu.Unlock()
	if s == current && proxied != nil {
		return
	}
	current = s
	if proxied != nil {
		proxied.CloseIdleConnections()
	}
	proxied = Through(s)
	if u := s.URL(); u != nil {
		diag.Printf("[proxy] %s %s for %s", s.Type, u.Host, scope(s))
	}
}

func scope(s Settings) string {
	var p []string
	if s.Indexers {
		p = append(p, "the indexers")
	}
	if s.Metadata {
		p = append(p, "TMDB and subtitles")
	}
	if len(p) == 0 {
		return "nothing"
	}
	return strings.Join(p, ", ")
}

// Through returns a transport that sends everything through the proxy of
// s (the standard transport without proxy).
func Through(s Settings) *http.Transport {
	t := direct.Clone()
	if u := s.URL(); u != nil {
		t.Proxy = http.ProxyURL(u)
	}
	return t
}

// Local reports a host that is reached directly: names without a dot
// ("jackett", "nas"), ".local" names, loopback and private addresses.
func Local(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".lan") || strings.HasSuffix(h, ".home.arpa") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	return !strings.Contains(h, ".")
}

// switcher picks, for every request, the proxy or the direct route.
type switcher struct {
	kind  Kind
	allow bool // false for a site excluded from the proxy
}

func (sw switcher) RoundTrip(req *http.Request) (*http.Response, error) {
	mu.RLock()
	s, t := current, proxied
	mu.RUnlock()
	if sw.allow && t != nil && s.applies(sw.kind) && !Local(req.URL.Hostname()) {
		resp, err := t.RoundTrip(req)
		if err != nil {
			err = fmt.Errorf("%w (%s)", err, i18n.Tf("through the proxy %s", s.Host))
		}
		return resp, err
	}
	return direct.RoundTrip(req)
}

// Transport returns the transport of a kind of request; allow is false for
// a site the user excluded from the proxy. Requests are logged like the
// others (diag).
func Transport(k Kind, allow bool) http.RoundTripper {
	return &diag.Transport{Base: switcher{kind: k, allow: allow}}
}

// Client returns an http.Client for a kind of request.
func Client(k Kind, c http.Client) *http.Client {
	c.Transport = Transport(k, true)
	return &c
}

// TraceURL answers with the address and the country of the caller
// (Cloudflare's trace page: "ip=…", "loc=…").
var TraceURL = "https://www.cloudflare.com/cdn-cgi/trace"

// Exit is the public address seen by the sites, and its country.
type Exit struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
}

func trace(ctx context.Context, rt http.RoundTripper) (Exit, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, TraceURL, nil)
	if err != nil {
		return Exit{}, err
	}
	resp, err := (&http.Client{Transport: rt, Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return Exit{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Exit{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	var e Exit
	for _, l := range strings.Split(string(b), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(l), "=")
		switch k {
		case "ip":
			e.IP = v
		case "loc":
			e.Country = v
		}
	}
	if e.IP == "" {
		return e, errors.New("unexpected answer")
	}
	return e, nil
}

// Test goes out through the proxy of s and directly, and returns the
// address seen by the sites in both cases.
func Test(ctx context.Context, s Settings) (through, without Exit, err error) {
	if s.URL() == nil {
		return through, without, i18n.New("no proxy configured")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		without, _ = trace(ctx, direct)
	}()
	through, err = trace(ctx, Through(s))
	wg.Wait()
	if err != nil {
		return through, without, i18n.Errorf("the proxy %s does not answer: %v", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)), err)
	}
	return through, without, nil
}
