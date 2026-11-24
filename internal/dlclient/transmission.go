package dlclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"cinearr/internal/i18n"
)

// transmission talks to the Transmission RPC (port of Radarr's
// TransmissionProxy).
type transmission struct {
	s    Settings
	http *http.Client
	rpc  string

	mu          sync.Mutex
	session     string
	prepared    bool
	version     string
	labels      bool   // Transmission 4+: labels instead of a sub-folder
	downloadDir string // folder given to torrent-add ("" = client default)
}

func newTransmission(s Settings) *transmission {
	base := strings.TrimRight(s.URL, "/")
	for _, suffix := range []string{"/rpc", "/web"} {
		base = strings.TrimSuffix(base, suffix)
	}
	if i := strings.Index(base, "://"); i >= 0 && !strings.Contains(base[i+3:], "/") {
		base += "/transmission"
	}
	return &transmission{s: s, http: httpClient(), rpc: base + "/rpc"}
}

func (c *transmission) Name() string { return "Transmission" }

type trResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

// call sends an RPC request, renewing the session id on 409.
func (c *transmission) call(ctx context.Context, method string, args any, out any) error {
	payload := map[string]any{"method": method}
	if args != nil {
		payload["arguments"] = args
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpc, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if c.s.Username != "" || c.s.Password != "" {
			req.SetBasicAuth(c.s.Username, c.s.Password)
		}
		c.mu.Lock()
		if c.session != "" {
			req.Header.Set("X-Transmission-Session-Id", c.session)
		}
		c.mu.Unlock()
		resp, err := c.http.Do(req)
		if err != nil {
			return unreachable("Transmission", c.rpc, err)
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusConflict:
			sid := resp.Header.Get("X-Transmission-Session-Id")
			if sid == "" {
				return i18n.New("Transmission did not return a session id")
			}
			c.mu.Lock()
			c.session = sid
			c.mu.Unlock()
			continue
		case resp.StatusCode == http.StatusUnauthorized:
			return i18n.New("Transmission refused the username or the password")
		case resp.StatusCode == http.StatusForbidden:
			return i18n.New("Transmission refuses the connection: add the address of this computer to the whitelist (“rpc-whitelist”) or disable it")
		case resp.StatusCode >= 300 && resp.StatusCode < 400:
			return redirected("Transmission", resp)
		case resp.StatusCode == http.StatusNotFound:
			return i18n.Errorf("Transmission: %s not found, check the address (the default path is /transmission/rpc)", c.rpc)
		case resp.StatusCode < 200 || resp.StatusCode > 299:
			return fmt.Errorf("%s: HTTP %d %s", "Transmission", resp.StatusCode, strings.TrimSpace(string(data)))
		}
		var r trResponse
		if err := json.Unmarshal(data, &r); err != nil {
			return i18n.Errorf("Transmission: unreadable answer (%v), check the address", err)
		}
		if r.Result != "success" {
			return fmt.Errorf("%s: %s", "Transmission", r.Result)
		}
		if out != nil && len(r.Arguments) > 0 {
			return json.Unmarshal(r.Arguments, out)
		}
		return nil
	}
	return i18n.New("Transmission: session refused")
}

var trVersion = regexp.MustCompile(`^(\d+)\.(\d+)`)

func (c *transmission) Test(ctx context.Context) (TestResult, error) {
	var cfg struct {
		Version     string `json:"version"`
		RPCVersion  int    `json:"rpc-version"`
		DownloadDir string `json:"download-dir"`
	}
	if err := c.call(ctx, "session-get", nil, &cfg); err != nil {
		return TestResult{}, err
	}
	major, minor := 0, 0
	if m := trVersion.FindStringSubmatch(cfg.Version); m != nil {
		major, _ = strconv.Atoi(m[1])
		minor, _ = strconv.Atoi(m[2])
	}
	res := TestResult{Version: cfg.Version}
	if major > 0 && (major < 2 || (major == 2 && minor < 40)) {
		return res, i18n.Errorf("Transmission %s is too old (2.40 minimum)", cfg.Version)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version = cfg.Version
	c.labels = major >= 4
	switch {
	case c.s.SavePath != "":
		c.downloadDir = c.s.SavePath
	case !c.labels && c.s.Category != "" && cfg.DownloadDir != "":
		// Transmission < 4 has no labels: Radarr uses a sub-folder
		c.downloadDir = joinRemote(cfg.DownloadDir, c.s.Category)
	default:
		c.downloadDir = ""
	}
	c.prepared = true
	return res, nil
}

func (c *transmission) prepare(ctx context.Context) error {
	c.mu.Lock()
	done := c.prepared
	c.mu.Unlock()
	if done {
		return nil
	}
	_, err := c.Test(ctx)
	return err
}

func (c *transmission) Add(ctx context.Context, r AddRequest) error {
	if err := c.prepare(ctx); err != nil {
		return err
	}
	args := map[string]any{"paused": false}
	if len(r.File) > 0 {
		args["metainfo"] = base64.StdEncoding.EncodeToString(r.File)
	} else {
		args["filename"] = r.Magnet
	}
	c.mu.Lock()
	if c.downloadDir != "" {
		args["download-dir"] = c.downloadDir
	}
	if c.labels && c.s.Category != "" {
		args["labels"] = []string{c.s.Category}
	}
	c.mu.Unlock()
	var out struct {
		Added *struct {
			HashString string `json:"hashString"`
		} `json:"torrent-added"`
		Duplicate *struct {
			HashString string `json:"hashString"`
		} `json:"torrent-duplicate"`
	}
	if err := c.call(ctx, "torrent-add", args, &out); err != nil {
		return err
	}
	hash := r.Hash
	if out.Added != nil && out.Added.HashString != "" {
		hash = out.Added.HashString
	} else if out.Duplicate != nil && out.Duplicate.HashString != "" {
		hash = out.Duplicate.HashString
	}
	if hash == "" {
		return nil
	}
	set := map[string]any{"ids": []string{strings.ToLower(hash)}}
	switch {
	case c.s.RatioLimit > 0:
		set["seedRatioLimit"], set["seedRatioMode"] = c.s.RatioLimit, 1
	case c.s.RatioLimit == -1:
		set["seedRatioMode"] = 2
	}
	switch {
	case c.s.SeedingTimeLimit > 0:
		// Transmission has no seeding time limit: like Radarr, use the
		// idle limit set per torrent
		set["seedIdleLimit"], set["seedIdleMode"] = c.s.SeedingTimeLimit, 1
	case c.s.SeedingTimeLimit == -1:
		set["seedIdleMode"] = 2
	}
	if len(set) > 1 {
		_ = c.call(ctx, "torrent-set", set, nil)
	}
	return nil
}

type trTorrent struct {
	HashString       string  `json:"hashString"`
	Name             string  `json:"name"`
	DownloadDir      string  `json:"downloadDir"`
	TotalSize        int64   `json:"totalSize"`
	LeftUntilDone    int64   `json:"leftUntilDone"`
	IsFinished       bool    `json:"isFinished"`
	ETA              int64   `json:"eta"`
	Status           int     `json:"status"`
	Error            int     `json:"error"`
	ErrorString      string  `json:"errorString"`
	PercentDone      float64 `json:"percentDone"`
	RateDownload     int64   `json:"rateDownload"`
	PeersSendingToUs int     `json:"peersSendingToUs"`
	Files            []struct {
		Name   string `json:"name"`
		Length int64  `json:"length"`
	} `json:"files"`
}

// Transmission status values.
const (
	trStopped     = 0
	trCheckWait   = 1
	trCheck       = 2
	trQueued      = 3
	trDownloading = 4
	trSeedWait    = 5
	trSeeding     = 6
)

func (c *transmission) Torrents(ctx context.Context, hashes []string) ([]Torrent, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	args := map[string]any{
		"ids": lower(hashes),
		"fields": []string{"hashString", "name", "downloadDir", "totalSize", "leftUntilDone", "isFinished", "eta", "status",
			"error", "errorString", "percentDone", "rateDownload", "peersSendingToUs"},
	}
	var out struct {
		Torrents []trTorrent `json:"torrents"`
	}
	if err := c.call(ctx, "torrent-get", args, &out); err != nil {
		return nil, err
	}
	list := make([]Torrent, 0, len(out.Torrents))
	for _, t := range out.Torrents {
		list = append(list, t.normalise())
	}
	return list, nil
}

// normalise maps a Transmission torrent like Radarr's TransmissionBase.
func (t trTorrent) normalise() Torrent {
	r := Torrent{
		Hash: strings.ToUpper(t.HashString), Name: t.Name, Size: t.TotalSize, Progress: t.PercentDone,
		DlSpeed: t.RateDownload, ETA: t.ETA, Seeds: t.PeersSendingToUs, Message: t.ErrorString,
		// Transmission replaces ':' in names on disk
		ContentPath: joinRemote(t.DownloadDir, strings.ReplaceAll(t.Name, ":", "_")),
		State:       strconv.Itoa(t.Status),
	}
	if r.ETA < 0 {
		r.ETA = -1
	}
	switch {
	case t.Error == 3: // local error (disk, missing files); 1 and 2 are tracker messages
		r.Status = "error"
	case t.TotalSize == 0:
		r.Status = "metadata"
	case t.LeftUntilDone == 0 && (t.Status == trStopped || t.Status == trSeeding || t.Status == trSeedWait):
		r.Status, r.Completed = "completed", true
	case t.IsFinished && t.Status != trCheck && t.Status != trCheckWait:
		r.Status, r.Completed = "completed", true
	case t.Status == trQueued || t.Status == trCheck || t.Status == trCheckWait:
		r.Status = "queued"
	case t.Status == trStopped:
		r.Status = "paused"
	case t.RateDownload == 0 && t.PeersSendingToUs == 0:
		r.Status = "stalled"
	default:
		r.Status = "downloading"
	}
	// isFinished: seeding limit reached; stopped: Transmission has paused it
	r.SeedingDone = r.Completed && t.IsFinished && t.Status == trStopped
	return r
}

func (c *transmission) Files(ctx context.Context, hash string) ([]File, error) {
	args := map[string]any{"ids": []string{strings.ToLower(hash)}, "fields": []string{"hashString", "files"}}
	var out struct {
		Torrents []trTorrent `json:"torrents"`
	}
	if err := c.call(ctx, "torrent-get", args, &out); err != nil {
		return nil, err
	}
	if len(out.Torrents) == 0 {
		return nil, nil
	}
	files := make([]File, 0, len(out.Torrents[0].Files))
	for _, f := range out.Torrents[0].Files {
		files = append(files, File{Path: f.Name, Size: f.Length})
	}
	return files, nil
}

func (c *transmission) Remove(ctx context.Context, hash string, deleteData bool) error {
	return c.call(ctx, "torrent-remove", map[string]any{"ids": []string{strings.ToLower(hash)}, "delete-local-data": deleteData}, nil)
}
