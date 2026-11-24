package dlclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"cinearr/internal/i18n"
)

// utorrent talks to the µTorrent Web UI API (port of Radarr's
// UTorrentProxy).
type utorrent struct {
	s    Settings
	http *http.Client
	base string // …/gui

	mu       sync.Mutex
	token    string
	prepared bool
}

func newUTorrent(s Settings) *utorrent {
	base := strings.TrimRight(s.URL, "/")
	base = strings.TrimSuffix(base, "/gui")
	return &utorrent{s: s, http: httpClient(), base: base + "/gui"}
}

func (c *utorrent) Name() string { return "µTorrent" }

var utToken = regexp.MustCompile(`<div[^>]*id=['"]token['"][^>]*>([^<]+)</div>`)

func (c *utorrent) auth(ctx context.Context) (string, error) {
	c.mu.Lock()
	tok := c.token
	c.mu.Unlock()
	if tok != "" {
		return tok, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/token.html", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.s.Username, c.s.Password)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", unreachable("µTorrent", c.base, err)
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return "", i18n.New("µTorrent refused the username or the password")
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return "", redirected("µTorrent", resp)
	case resp.StatusCode == http.StatusNotFound:
		return "", i18n.Errorf("µTorrent: %s/token.html not found, check that the web interface is enabled and the address", c.base)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return "", fmt.Errorf("µTorrent: HTTP %d", resp.StatusCode)
	}
	m := utToken.FindSubmatch(data)
	if m == nil {
		return "", i18n.New("µTorrent: web interface token not found, check the address")
	}
	tok = strings.TrimSpace(string(m[1]))
	c.mu.Lock()
	c.token = tok
	c.mu.Unlock()
	return tok, nil
}

// pairs keeps query parameters in order: µTorrent reads repeated s / v
// pairs positionally.
type pairs [][2]string

func (p pairs) encode() string {
	var b strings.Builder
	for i, kv := range p {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(kv[0]))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(kv[1]))
	}
	return b.String()
}

// request calls the API; upload, when set, is posted as torrent_file.
func (c *utorrent) request(ctx context.Context, q pairs, upload []byte, name string, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.auth(ctx)
		if err != nil {
			return err
		}
		target := c.base + "/?" + pairs{{"token", tok}}.encode() + "&" + q.encode()
		var req *http.Request
		if upload != nil {
			var buf bytes.Buffer
			w := multipart.NewWriter(&buf)
			fw, err := w.CreateFormFile("torrent_file", name)
			if err != nil {
				return err
			}
			_, _ = fw.Write(upload)
			w.Close()
			req, err = http.NewRequestWithContext(ctx, http.MethodPost, target, &buf)
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", w.FormDataContentType())
		} else {
			req, err = http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if err != nil {
				return err
			}
		}
		req.SetBasicAuth(c.s.Username, c.s.Password)
		req.Header.Set("Cache-Control", "no-cache")
		resp, err := c.http.Do(req)
		if err != nil {
			return unreachable("µTorrent", c.base, err)
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized) && attempt == 0 {
			// expired token
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("µTorrent: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
		}
		var head struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &head) == nil && head.Error != "" {
			return fmt.Errorf("µTorrent: %s", head.Error)
		}
		if out != nil {
			if err := json.Unmarshal(data, out); err != nil {
				return i18n.Errorf("µTorrent: unreadable answer (%v)", err)
			}
		}
		return nil
	}
	return i18n.New("µTorrent: session refused")
}

func (c *utorrent) Test(ctx context.Context) (TestResult, error) {
	var out struct {
		Build int `json:"build"`
	}
	if err := c.request(ctx, pairs{{"action", "getsettings"}}, nil, "", &out); err != nil {
		return TestResult{}, err
	}
	c.mu.Lock()
	c.prepared = true
	c.mu.Unlock()
	return TestResult{Version: "build " + strconv.Itoa(out.Build)}, nil
}

func (c *utorrent) prepare(ctx context.Context) error {
	c.mu.Lock()
	done := c.prepared
	c.mu.Unlock()
	if done {
		return nil
	}
	_, err := c.Test(ctx)
	return err
}

func (c *utorrent) Add(ctx context.Context, r AddRequest) error {
	if err := c.prepare(ctx); err != nil {
		return err
	}
	var err error
	if len(r.File) > 0 {
		err = c.request(ctx, pairs{{"action", "add-file"}, {"path", ""}}, r.File, r.FileName, nil)
	} else {
		err = c.request(ctx, pairs{{"action", "add-url"}, {"s", r.Magnet}}, nil, "", nil)
	}
	if err != nil || r.Hash == "" {
		return err
	}
	hash := strings.ToUpper(r.Hash)
	if c.s.Category != "" {
		_ = c.request(ctx, pairs{{"action", "setprops"}, {"hash", hash}, {"s", "label"}, {"v", c.s.Category}}, nil, "", nil)
	}
	if c.s.RatioLimit > 0 || c.s.SeedingTimeLimit > 0 {
		q := pairs{{"action", "setprops"}, {"hash", hash}, {"s", "seed_override"}, {"v", "1"}}
		if c.s.RatioLimit > 0 {
			q = append(q, [2]string{"s", "seed_ratio"}, [2]string{"v", strconv.Itoa(int(c.s.RatioLimit * 1000))})
		}
		if c.s.SeedingTimeLimit > 0 {
			q = append(q, [2]string{"s", "seed_time"}, [2]string{"v", strconv.Itoa(c.s.SeedingTimeLimit * 60)})
		}
		_ = c.request(ctx, q, nil, "", nil)
	}
	return nil
}

// µTorrent status flags.
const (
	utStarted = 1
	utChecked = 8
	utError   = 16
	utPaused  = 32
	utQueued  = 64
	utLoaded  = 128
)

type utTorrent struct {
	Hash           string
	Status         int
	Name           string
	Size           int64
	Progress       int // per mille
	DownSpeed      int64
	ETA            int64
	SeedsConnected int
	Remaining      int64
	StatusMessage  string
	SavePath       string
}

// parseUTorrent reads one row of list=1 (an array of mixed values).
func parseUTorrent(row []json.RawMessage) (utTorrent, bool) {
	var t utTorrent
	if len(row) < 19 {
		return t, false
	}
	str := func(i int) string {
		var s string
		if i < len(row) {
			_ = json.Unmarshal(row[i], &s)
		}
		return s
	}
	num := func(i int) int64 {
		var f float64
		if i < len(row) {
			_ = json.Unmarshal(row[i], &f)
		}
		return int64(f)
	}
	t.Hash = strings.ToUpper(str(0))
	t.Status = int(num(1))
	t.Name = str(2)
	t.Size = num(3)
	t.Progress = int(num(4))
	t.DownSpeed = num(9)
	t.ETA = num(10)
	t.SeedsConnected = int(num(14))
	t.Remaining = num(18)
	t.StatusMessage = str(21)
	t.SavePath = str(26)
	return t, t.Hash != ""
}

// normalise maps a µTorrent torrent like Radarr's UTorrent.GetItems.
func (t utTorrent) normalise() Torrent {
	r := Torrent{
		Hash: t.Hash, Name: t.Name, Size: t.Size, Progress: float64(t.Progress) / 1000, DlSpeed: t.DownSpeed,
		ETA: t.ETA, Seeds: t.SeedsConnected, State: t.StatusMessage,
	}
	if t.SavePath != "" {
		if baseName(t.SavePath) == t.Name {
			r.ContentPath = t.SavePath
		} else {
			r.ContentPath = joinRemote(t.SavePath, t.Name)
		}
	}
	has := func(f int) bool { return t.Status&f != 0 }
	switch {
	case has(utError):
		r.Status, r.Message = "error", t.StatusMessage
	case t.Size == 0:
		r.Status = "metadata"
	case has(utLoaded) && has(utChecked) && t.Remaining == 0 && t.Progress == 1000:
		r.Status, r.Completed = "completed", true
	case has(utPaused):
		r.Status = "paused"
	case has(utStarted) && t.DownSpeed == 0 && t.SeedsConnected == 0:
		r.Status = "stalled"
	case has(utStarted):
		r.Status = "downloading"
	default:
		r.Status = "queued"
	}
	if r.ETA <= 0 && !r.Completed {
		r.ETA = -1
	}
	// neither queued nor started: seeding goal reached and stopped
	r.SeedingDone = r.Completed && !has(utQueued) && !has(utStarted)
	return r
}

func (c *utorrent) Torrents(ctx context.Context, hashes []string) ([]Torrent, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	var out struct {
		Torrents [][]json.RawMessage `json:"torrents"`
	}
	if err := c.request(ctx, pairs{{"list", "1"}}, nil, "", &out); err != nil {
		return nil, err
	}
	want := upper(hashes)
	var list []Torrent
	for _, row := range out.Torrents {
		t, ok := parseUTorrent(row)
		if ok && want[t.Hash] {
			list = append(list, t.normalise())
		}
	}
	return list, nil
}

func (c *utorrent) Files(ctx context.Context, hash string) ([]File, error) {
	var out struct {
		Files []json.RawMessage `json:"files"`
	}
	if err := c.request(ctx, pairs{{"action", "getfiles"}, {"hash", strings.ToUpper(hash)}}, nil, "", &out); err != nil {
		return nil, err
	}
	// "files": ["HASH", [[name, size, downloaded, priority, …], …]]
	if len(out.Files) < 2 {
		return nil, nil
	}
	var rows [][]json.RawMessage
	if err := json.Unmarshal(out.Files[1], &rows); err != nil {
		return nil, nil
	}
	var files []File
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		var name string
		var size float64
		_ = json.Unmarshal(row[0], &name)
		_ = json.Unmarshal(row[1], &size)
		files = append(files, File{Path: name, Size: int64(size)})
	}
	return files, nil
}

func (c *utorrent) Remove(ctx context.Context, hash string, deleteData bool) error {
	action := "remove"
	if deleteData {
		action = "removedata"
	}
	return c.request(ctx, pairs{{"action", action}, {"hash", strings.ToUpper(hash)}}, nil, "", nil)
}
