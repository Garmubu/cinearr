// Package qbit is a qBittorrent Web API v2 client (port of Radarr's
// QBittorrentProxyV2, limited to what the application needs).
package qbit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"cinearr/internal/i18n"
)

// Settings to reach qBittorrent.
type Settings struct {
	URL      string `json:"url"` // http://127.0.0.1:8080
	Username string `json:"username"`
	Password string `json:"password"`
	Category string `json:"category"`
	SavePath string `json:"savePath,omitempty"`
	// Sequential download lets mpv start playing before the end.
	Sequential bool `json:"sequential"`
	// Seeding limits applied to added torrents (-2 = global, -1 = none).
	RatioLimit       float64 `json:"ratioLimit"`
	SeedingTimeLimit int     `json:"seedingTimeLimit"` // minutes
}

// Torrent is an entry of /torrents/info.
type Torrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	Size        int64   `json:"size"`
	Progress    float64 `json:"progress"`
	DlSpeed     int64   `json:"dlspeed"`
	UpSpeed     int64   `json:"upspeed"`
	ETA         int64   `json:"eta"`
	State       string  `json:"state"`
	Category    string  `json:"category"`
	SavePath    string  `json:"save_path"`
	ContentPath string  `json:"content_path"`
	Ratio       float64 `json:"ratio"`
	NumSeeds    int     `json:"num_seeds"`
	AddedOn     int64   `json:"added_on"`
	Tags        string  `json:"tags"`
}

// File is an entry of /torrents/files.
type File struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

// Client talks to qBittorrent.
type Client struct {
	s      Settings
	http   *http.Client
	mu     sync.Mutex
	authed bool
	apiVer string
}

// ErrAuth is returned when credentials are refused.
var ErrAuth error = authError{}

type authError struct{}

func (authError) Error() string { return i18n.T("qBittorrent refused the credentials") }

// New creates a client.
func New(s Settings) *Client {
	jar, _ := cookiejar.New(nil)
	if s.Category == "" {
		s.Category = "cinearr"
	}
	return &Client{s: s, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

// Settings returns the current settings.
func (c *Client) Settings() Settings { return c.s }

func (c *Client) base() string { return strings.TrimRight(c.s.URL, "/") }

func (c *Client) login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authed {
		return nil
	}
	form := url.Values{"username": {c.s.Username}, "password": {c.s.Password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.base())
	resp, err := c.http.Do(req)
	if err != nil {
		return i18n.Errorf("cannot reach qBittorrent (%s): %w", c.base(), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return i18n.New("qBittorrent temporarily banned this address after too many failed logins")
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrAuth
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return i18n.Errorf("qBittorrent: login refused (HTTP %d)", resp.StatusCode)
	}
	// success is "200 Ok." on older versions and "204 No Content" on recent
	// ones; Radarr likewise accepts any 2xx with an empty body
	if t := strings.TrimSpace(string(body)); t != "" && t != "Ok." {
		return ErrAuth
	}
	c.authed = true
	return nil
}

// do sends a request, logging in (again) when needed.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, error) {
	var payload []byte
	if body != nil {
		b, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		payload = b
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base()+path, rd)
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("Referer", c.base())
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, i18n.Errorf("cannot reach qBittorrent: %w", err)
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden && attempt == 0 {
			c.mu.Lock()
			c.authed = false
			c.mu.Unlock()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("qBittorrent %s : HTTP %d %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
		}
		return data, nil
	}
	return nil, ErrAuth
}

func (c *Client) postForm(ctx context.Context, path string, form url.Values) ([]byte, error) {
	return c.do(ctx, http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
}

// Version returns the application version (also validates the connection).
func (c *Client) Version(ctx context.Context) (string, error) {
	b, err := c.do(ctx, http.MethodGet, "/api/v2/app/version", nil, "")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (c *Client) webAPIVersion(ctx context.Context) string {
	if c.apiVer != "" {
		return c.apiVer
	}
	b, err := c.do(ctx, http.MethodGet, "/api/v2/app/webapiVersion", nil, "")
	if err == nil {
		c.apiVer = strings.TrimSpace(string(b))
	}
	return c.apiVer
}

// apiAtLeast compares dotted versions.
func apiAtLeast(v string, major, minor int) bool {
	var a, b int
	fmt.Sscanf(v, "%d.%d", &a, &b)
	return a > major || (a == major && b >= minor)
}

// EnsureCategory creates the category if missing.
func (c *Client) EnsureCategory(ctx context.Context) error {
	b, err := c.do(ctx, http.MethodGet, "/api/v2/torrents/categories", nil, "")
	if err != nil {
		return err
	}
	var cats map[string]any
	_ = json.Unmarshal(b, &cats)
	if _, ok := cats[c.s.Category]; ok {
		return nil
	}
	form := url.Values{"category": {c.s.Category}}
	if c.s.SavePath != "" {
		form.Set("savePath", c.s.SavePath)
	}
	_, err = c.postForm(ctx, "/api/v2/torrents/createCategory", form)
	return err
}

// AddOptions are per-torrent options.
type AddOptions struct {
	Tags   string
	Paused bool
}

func (c *Client) addFields(ctx context.Context, w *multipart.Writer, o AddOptions) {
	_ = w.WriteField("category", c.s.Category)
	if c.s.SavePath != "" {
		_ = w.WriteField("savepath", c.s.SavePath)
	}
	if o.Tags != "" {
		_ = w.WriteField("tags", o.Tags)
	}
	if c.s.Sequential {
		_ = w.WriteField("sequentialDownload", "true")
		_ = w.WriteField("firstLastPiecePrio", "true")
	}
	param := "paused"
	if apiAtLeast(c.webAPIVersion(ctx), 2, 11) {
		param = "stopped"
	}
	_ = w.WriteField(param, fmt.Sprint(o.Paused))
	if c.s.RatioLimit != 0 {
		_ = w.WriteField("ratioLimit", fmt.Sprint(c.s.RatioLimit))
	}
	if c.s.SeedingTimeLimit != 0 {
		_ = w.WriteField("seedingTimeLimit", fmt.Sprint(c.s.SeedingTimeLimit))
	}
}

// AddMagnet adds a magnet link.
func (c *Client) AddMagnet(ctx context.Context, magnet string, o AddOptions) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("urls", magnet)
	c.addFields(ctx, w, o)
	w.Close()
	b, err := c.do(ctx, http.MethodPost, "/api/v2/torrents/add", &buf, w.FormDataContentType())
	if err != nil {
		return err
	}
	if s := strings.TrimSpace(string(b)); s == "Fails." {
		return i18n.New("qBittorrent refused the magnet link (already there?)")
	}
	return nil
}

// AddTorrentFile uploads a .torrent file.
func (c *Client) AddTorrentFile(ctx context.Context, name string, data []byte, o AddOptions) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("torrents", name)
	if err != nil {
		return err
	}
	_, _ = fw.Write(data)
	c.addFields(ctx, w, o)
	w.Close()
	b, err := c.do(ctx, http.MethodPost, "/api/v2/torrents/add", &buf, w.FormDataContentType())
	if err != nil {
		return err
	}
	if s := strings.TrimSpace(string(b)); s == "Fails." {
		return i18n.New("qBittorrent refused the .torrent file (already there?)")
	}
	return nil
}

// Torrents lists the torrents of our category (or the given hashes).
func (c *Client) Torrents(ctx context.Context, hashes ...string) ([]Torrent, error) {
	q := url.Values{}
	if len(hashes) > 0 {
		q.Set("hashes", strings.ToLower(strings.Join(hashes, "|")))
	} else {
		q.Set("category", c.s.Category)
	}
	b, err := c.do(ctx, http.MethodGet, "/api/v2/torrents/info?"+q.Encode(), nil, "")
	if err != nil {
		return nil, err
	}
	var out []Torrent
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Files lists the files of a torrent (empty until metadata is known).
func (c *Client) Files(ctx context.Context, hash string) ([]File, error) {
	b, err := c.do(ctx, http.MethodGet, "/api/v2/torrents/files?hash="+strings.ToLower(hash), nil, "")
	if err != nil {
		return nil, err
	}
	var out []File
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes a torrent, optionally with its data.
func (c *Client) Delete(ctx context.Context, hash string, deleteFiles bool) error {
	_, err := c.postForm(ctx, "/api/v2/torrents/delete", url.Values{"hashes": {strings.ToLower(hash)}, "deleteFiles": {fmt.Sprint(deleteFiles)}})
	return err
}

// Resume starts a torrent.
func (c *Client) Resume(ctx context.Context, hash string) error {
	path := "/api/v2/torrents/resume"
	if apiAtLeast(c.webAPIVersion(ctx), 2, 11) {
		path = "/api/v2/torrents/start"
	}
	_, err := c.postForm(ctx, path, url.Values{"hashes": {strings.ToLower(hash)}})
	return err
}

// Status maps qBittorrent states like Radarr's QBittorrent.GetItems.
func Status(state string) (status string, completed bool) {
	switch state {
	case "error", "missingFiles":
		return "error", false
	case "stoppedDL", "pausedDL":
		return "paused", false
	case "queuedDL", "checkingDL", "checkingUP", "checkingResumeData", "moving":
		return "queued", false
	case "pausedUP", "stoppedUP", "uploading", "stalledUP", "queuedUP", "forcedUP":
		return "completed", true
	case "stalledDL":
		return "stalled", false
	case "metaDL", "forcedMetaDL":
		return "metadata", false
	}
	return "downloading", false
}
