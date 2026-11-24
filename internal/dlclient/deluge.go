package dlclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"cinearr/internal/i18n"
)

// deluge talks to the Deluge web JSON-RPC API (port of Radarr's
// DelugeProxy).
type deluge struct {
	s    Settings
	http *http.Client
	url  string

	mu       sync.Mutex
	id       int
	authed   bool
	prepared bool
	label    string // label to set ("" = Label plugin unavailable)
}

func newDeluge(s Settings) *deluge {
	base := strings.TrimRight(s.URL, "/")
	base = strings.TrimSuffix(base, "/json")
	return &deluge{s: s, http: httpClient(), url: base + "/json"}
}

func (c *deluge) Name() string { return "Deluge" }

type delugeError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// raw performs one JSON-RPC call.
func (c *deluge) raw(ctx context.Context, method string, params []any) (json.RawMessage, *delugeError, error) {
	if params == nil {
		params = []any{}
	}
	c.mu.Lock()
	c.id++
	id := c.id
	c.mu.Unlock()
	body, err := json.Marshal(map[string]any{"method": method, "params": params, "id": id})
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, unreachable("Deluge", c.url, err)
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	resp.Body.Close()
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return nil, nil, redirected("Deluge", resp)
	case resp.StatusCode == http.StatusNotFound:
		return nil, nil, i18n.Errorf("Deluge: %s not found, check the address of the web interface (port 8112 by default)", c.url)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, nil, fmt.Errorf("%s: HTTP %d %s", "Deluge", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *delugeError    `json:"error"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, nil, i18n.Errorf("Deluge: unreadable answer (%v), check the address", err)
	}
	return r.Result, r.Error, nil
}

// login authenticates and connects the web interface to a daemon.
func (c *deluge) login(ctx context.Context) error {
	c.mu.Lock()
	done := c.authed
	c.mu.Unlock()
	if done {
		return nil
	}
	res, rerr, err := c.raw(ctx, "auth.login", []any{c.s.Password})
	if err != nil {
		return err
	}
	var ok bool
	if rerr != nil || json.Unmarshal(res, &ok) != nil || !ok {
		return i18n.New("Deluge refused the password of the web interface")
	}
	if err := c.connectDaemon(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	c.authed = true
	c.mu.Unlock()
	return nil
}

func (c *deluge) connectDaemon(ctx context.Context) error {
	res, rerr, err := c.raw(ctx, "web.connected", nil)
	if err != nil {
		return err
	}
	var connected bool
	if rerr == nil && json.Unmarshal(res, &connected) == nil && connected {
		return nil
	}
	res, rerr, err = c.raw(ctx, "web.get_hosts", nil)
	if err != nil {
		return err
	}
	var hosts [][]any
	if rerr == nil {
		_ = json.Unmarshal(res, &hosts)
	}
	// the hosts are [id, address, port, …]: prefer the local daemon
	var pick any
	for _, h := range hosts {
		if len(h) > 1 {
			if addr, _ := h[1].(string); addr == "127.0.0.1" || addr == "localhost" {
				pick = h[0]
				break
			}
		}
	}
	if pick == nil && len(hosts) > 0 && len(hosts[0]) > 0 {
		pick = hosts[0][0]
	}
	if pick == nil {
		return i18n.New("Deluge: the web interface is not connected to any daemon; open it in a browser and connect it in the connection manager")
	}
	if _, rerr, err = c.raw(ctx, "web.connect", []any{pick}); err != nil {
		return err
	} else if rerr != nil {
		return i18n.Errorf("Deluge: cannot connect to the daemon (%s)", rerr.Message)
	}
	return nil
}

// call performs a call, logging in again when the session expired.
func (c *deluge) call(ctx context.Context, method string, params []any, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.login(ctx); err != nil {
			return err
		}
		res, rerr, err := c.raw(ctx, method, params)
		if err != nil {
			return err
		}
		if rerr != nil {
			if (rerr.Code == 1 || rerr.Code == 2) && attempt == 0 { // not authenticated / timeout
				c.mu.Lock()
				c.authed = false
				c.mu.Unlock()
				continue
			}
			return fmt.Errorf("%s: %s", "Deluge", rerr.Message)
		}
		if out != nil && len(res) > 0 && string(res) != "null" {
			return json.Unmarshal(res, out)
		}
		return nil
	}
	return i18n.New("Deluge: session refused")
}

func (c *deluge) Test(ctx context.Context) (TestResult, error) {
	var methods []string
	if err := c.call(ctx, "system.listMethods", nil, &methods); err != nil {
		return TestResult{}, err
	}
	has := map[string]bool{}
	labelPlugin := false
	for _, m := range methods {
		has[m] = true
		if strings.HasPrefix(m, "label.") {
			labelPlugin = true
		}
	}
	var version string
	if has["daemon.get_version"] {
		_ = c.call(ctx, "daemon.get_version", nil, &version)
	} else {
		_ = c.call(ctx, "daemon.info", nil, &version)
	}
	res := TestResult{Version: version}
	label := strings.ToLower(c.s.Category)
	switch {
	case label == "":
	case !labelPlugin:
		res.Warning = i18n.T("“Label” plugin disabled: torrents will not be labelled (enable it in Preferences › Plugins)")
		label = ""
	default:
		var labels []string
		_ = c.call(ctx, "label.get_labels", nil, &labels)
		found := false
		for _, l := range labels {
			if l == label {
				found = true
			}
		}
		if !found {
			if err := c.call(ctx, "label.add", []any{label}, nil); err != nil {
				res.Warning = i18n.Tf("label not created: %v", err)
				label = ""
			}
		}
	}
	c.mu.Lock()
	c.label = label
	c.prepared = true
	c.mu.Unlock()
	return res, nil
}

func (c *deluge) prepare(ctx context.Context) error {
	c.mu.Lock()
	done := c.prepared
	c.mu.Unlock()
	if done {
		return nil
	}
	_, err := c.Test(ctx)
	return err
}

func (c *deluge) Add(ctx context.Context, r AddRequest) error {
	if err := c.prepare(ctx); err != nil {
		return err
	}
	opts := map[string]any{"add_paused": false, "remove_at_ratio": false}
	if c.s.SavePath != "" {
		opts["download_location"] = c.s.SavePath
	}
	var hash string
	var err error
	if len(r.File) > 0 {
		err = c.call(ctx, "core.add_torrent_file", []any{r.FileName, base64.StdEncoding.EncodeToString(r.File), opts}, &hash)
	} else {
		err = c.call(ctx, "core.add_torrent_magnet", []any{r.Magnet, opts}, &hash)
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			return i18n.New("Deluge: this torrent is already there")
		}
		return err
	}
	if hash == "" {
		hash = r.Hash
	}
	hash = strings.ToLower(hash)
	c.mu.Lock()
	label := c.label
	c.mu.Unlock()
	if label != "" && hash != "" {
		_ = c.call(ctx, "label.set_torrent", []any{hash, label}, nil)
	}
	switch {
	case c.s.RatioLimit > 0:
		_ = c.call(ctx, "core.set_torrent_options", []any{[]string{hash}, map[string]any{"stop_ratio": c.s.RatioLimit, "stop_at_ratio": true}}, nil)
	case c.s.RatioLimit == -1:
		_ = c.call(ctx, "core.set_torrent_options", []any{[]string{hash}, map[string]any{"stop_at_ratio": false}}, nil)
	}
	return nil
}

type delugeTorrent struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	State         string  `json:"state"`
	Progress      float64 `json:"progress"` // percent
	ETA           float64 `json:"eta"`
	Message       string  `json:"message"`
	IsFinished    bool    `json:"is_finished"`
	SavePath      string  `json:"save_path"`
	TotalSize     int64   `json:"total_size"`
	TotalDone     int64   `json:"total_done"`
	Ratio         float64 `json:"ratio"`
	DownloadRate  float64 `json:"download_payload_rate"`
	NumSeeds      int     `json:"num_seeds"`
	IsAutoManaged bool    `json:"is_auto_managed"`
	StopAtRatio   bool    `json:"stop_at_ratio"`
	StopRatio     float64 `json:"stop_ratio"`
}

var delugeFields = []string{"hash", "name", "state", "progress", "eta", "message", "is_finished", "save_path", "total_size", "total_done",
	"ratio", "download_payload_rate", "num_seeds", "is_auto_managed", "stop_at_ratio", "stop_ratio"}

func (c *deluge) Torrents(ctx context.Context, hashes []string) ([]Torrent, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	var out map[string]delugeTorrent
	if err := c.call(ctx, "core.get_torrents_status", []any{map[string]any{"id": lower(hashes)}, delugeFields}, &out); err != nil {
		return nil, err
	}
	want := upper(hashes)
	list := make([]Torrent, 0, len(out))
	for h, t := range out {
		if t.Hash == "" {
			t.Hash = h
		}
		if !want[strings.ToUpper(t.Hash)] {
			continue
		}
		list = append(list, t.normalise())
	}
	return list, nil
}

// normalise maps a Deluge torrent like Radarr's Deluge.GetItems.
func (t delugeTorrent) normalise() Torrent {
	r := Torrent{
		Hash: strings.ToUpper(t.Hash), Name: t.Name, Size: t.TotalSize, Progress: t.Progress / 100, DlSpeed: int64(t.DownloadRate),
		ETA: int64(t.ETA), Seeds: t.NumSeeds, ContentPath: joinRemote(t.SavePath, t.Name), State: t.State,
	}
	if t.State == "Error" {
		r.Message = t.Message
	}
	switch {
	case t.State == "Error":
		r.Status = "error"
	case t.TotalSize == 0:
		r.Status = "metadata"
	case t.IsFinished && t.State != "Checking":
		r.Status, r.Completed = "completed", true
	case t.State == "Queued" || t.State == "Checking" || t.State == "Allocating" || t.State == "Moving":
		r.Status = "queued"
	case t.State == "Paused":
		r.Status = "paused"
	case t.DownloadRate == 0 && t.NumSeeds == 0:
		r.Status = "stalled"
	default:
		r.Status = "downloading"
	}
	if r.ETA <= 0 && !r.Completed {
		r.ETA = -1
	}
	r.SeedingDone = r.Completed && t.IsAutoManaged && t.StopAtRatio && t.Ratio >= t.StopRatio && t.State == "Paused"
	return r
}

func (c *deluge) Files(ctx context.Context, hash string) ([]File, error) {
	var out struct {
		Files []struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	if err := c.call(ctx, "core.get_torrent_status", []any{strings.ToLower(hash), []string{"files"}}, &out); err != nil {
		return nil, err
	}
	files := make([]File, 0, len(out.Files))
	for _, f := range out.Files {
		files = append(files, File{Path: f.Path, Size: f.Size})
	}
	return files, nil
}

func (c *deluge) Remove(ctx context.Context, hash string, deleteData bool) error {
	return c.call(ctx, "core.remove_torrent", []any{strings.ToLower(hash), deleteData}, nil)
}
