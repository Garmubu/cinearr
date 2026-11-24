// Package dlclient drives the torrent client chosen by the user:
// qBittorrent, Transmission, Deluge or µTorrent (ports of Radarr's
// download clients, limited to what Cinearr needs).
//
// Cinearr identifies its torrents by info-hash, which it knows before
// sending them (computed from the .torrent file or read from the magnet
// link): categories and labels are only a convenience for the user.
package dlclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"cinearr/internal/i18n"
)

// Supported clients.
const (
	QBittorrent  = "qbittorrent"
	Transmission = "transmission"
	Deluge       = "deluge"
	UTorrent     = "utorrent"
)

// Settings to reach the client.
type Settings struct {
	Kind     string `json:"kind"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	// Category (qBittorrent), label (Transmission 4+, Deluge with the Label
	// plugin, µTorrent) or sub-folder (older Transmission).
	Category string `json:"category"`
	SavePath string `json:"savePath,omitempty"`
	// Sequential download lets the player start before the end
	// (qBittorrent only).
	Sequential bool `json:"sequential"`
	// Seeding limits applied to added torrents (-2 = client setting,
	// -1 = unlimited).
	RatioLimit       float64 `json:"ratioLimit"`
	SeedingTimeLimit int     `json:"seedingTimeLimit"` // minutes
}

// Info describes a client for the interface.
type Info struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DefaultURL string `json:"defaultUrl"`
	Username   bool   `json:"username"`   // asks for a user name
	SavePath   bool   `json:"savePath"`   // honours a download folder
	Sequential bool   `json:"sequential"` // sequential download option
	TimeLimit  bool   `json:"timeLimit"`  // seeding time limit
	Help       string `json:"help"`
}

// Clients lists the supported clients, most used first.
var Clients = []Info{
	{ID: QBittorrent, Name: "qBittorrent", DefaultURL: "http://127.0.0.1:8080", Username: true, SavePath: true, Sequential: true, TimeLimit: true,
		Help: "In qBittorrent: Tools › Options › Web UI, tick “Web User Interface (Remote control)”, note the port and set a username and a password."},
	{ID: Transmission, Name: "Transmission", DefaultURL: "http://127.0.0.1:9091", Username: true, SavePath: true, TimeLimit: true,
		Help: "In Transmission: Edit › Preferences › Remote, tick “Allow remote access” (port 9091). A username and a password are optional."},
	{ID: Deluge, Name: "Deluge", DefaultURL: "http://127.0.0.1:8112", SavePath: true,
		Help: "In Deluge: Preferences › Plugins, enable “WebUi” (port 8112). The default password of the web interface is “deluge”. Also enable the “Label” plugin so that torrents are labelled."},
	{ID: UTorrent, Name: "µTorrent", DefaultURL: "http://127.0.0.1:8080", Username: true, TimeLimit: true,
		Help: "In µTorrent: Options › Preferences › Advanced › Web UI, tick “Enable Web UI”, set a username and a password, and tick “Alternative listening port” (8080). The download folder is set in µTorrent."},
}

// Lookup returns the description of a client (qBittorrent by default).
func Lookup(kind string) Info {
	for _, c := range Clients {
		if c.ID == kind {
			return c
		}
	}
	return Clients[0]
}

// Name returns the display name of a client kind.
func Name(kind string) string { return Lookup(kind).Name }

// Torrent is the state of a torrent, normalised across clients.
type Torrent struct {
	Hash     string // upper case
	Name     string
	Size     int64
	Progress float64 // 0..1
	DlSpeed  int64
	ETA      int64 // seconds, < 0 when unknown
	Seeds    int
	// Status is one of downloading, stalled, metadata, queued, paused,
	// completed, error.
	Status    string
	Completed bool
	// SeedingDone: completed, seeding limit reached and stopped, so the
	// torrent can be removed.
	SeedingDone bool
	// ContentPath is the file or folder of the content, as seen by the
	// client (path mappings are applied by the caller).
	ContentPath string
	Message     string // error reported by the client
	State       string // raw state, for the log
}

// File is a file of a torrent.
type File struct {
	Path string
	Size int64
}

// AddRequest is a torrent to send: a magnet link or a .torrent file.
type AddRequest struct {
	Magnet   string
	File     []byte
	FileName string
	Hash     string // info-hash, known in advance
	Tag      string // per-film tag (qBittorrent)
}

// TestResult is returned by Test.
type TestResult struct {
	Version string `json:"version"`
	Warning string `json:"warning,omitempty"`
}

// Client is a torrent client.
type Client interface {
	// Name is the display name ("qBittorrent", "Transmission"…).
	Name() string
	// Test checks the connection and prepares the category or label.
	Test(ctx context.Context) (TestResult, error)
	// Add sends a torrent.
	Add(ctx context.Context, r AddRequest) error
	// Torrents returns the state of the given torrents (absent ones are
	// simply missing from the result).
	Torrents(ctx context.Context, hashes []string) ([]Torrent, error)
	// Files lists the files of a torrent (empty until metadata is known).
	Files(ctx context.Context, hash string) ([]File, error)
	// Remove deletes a torrent, optionally with its data.
	Remove(ctx context.Context, hash string, deleteData bool) error
}

// New returns the client described by the settings.
func New(s Settings) Client {
	s.URL = strings.TrimSpace(s.URL)
	if s.URL != "" && !strings.Contains(s.URL, "://") {
		s.URL = "http://" + s.URL
	}
	if s.Category == "" {
		s.Category = "cinearr"
	}
	switch s.Kind {
	case Transmission:
		return newTransmission(s)
	case Deluge:
		return newDeluge(s)
	case UTorrent:
		return newUTorrent(s)
	default:
		return newQBit(s)
	}
}

// httpClient keeps cookies and refuses redirects (a redirect usually means
// a wrong address, e.g. the web page instead of the API).
func httpClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// unreachable wraps a connection error with the client name.
func unreachable(name, base string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return i18n.Errorf("%s does not answer (%s): %w", name, base, err)
	}
	return i18n.Errorf("cannot reach %s (%s): check that it is running and that its web interface is enabled (%v)", name, base, err)
}

// redirected explains a redirect answer.
func redirected(name string, resp *http.Response) error {
	return i18n.Errorf("%s: the address redirects to %s, check the address and the port", name, resp.Header.Get("Location"))
}

// joinRemote joins a folder and a name with the separator used by the
// client's system.
func joinRemote(dir, name string) string {
	if dir == "" {
		return name
	}
	sep := "/"
	if strings.Contains(dir, `\`) && !strings.Contains(dir, "/") {
		sep = `\`
	}
	return strings.TrimRight(dir, `/\`) + sep + name
}

// baseName returns the last element of a path in either syntax.
func baseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func upper(hashes []string) map[string]bool {
	m := make(map[string]bool, len(hashes))
	for _, h := range hashes {
		m[strings.ToUpper(h)] = true
	}
	return m
}

func lower(hashes []string) []string {
	out := make([]string, len(hashes))
	for i, h := range hashes {
		out[i] = strings.ToLower(h)
	}
	return out
}
