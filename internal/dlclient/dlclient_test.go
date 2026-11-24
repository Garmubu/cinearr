package dlclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const hashA = "0123456789ABCDEF0123456789ABCDEF01234567"

// ---------------------------------------------------------------- Transmission

type fakeTransmission struct {
	mu      sync.Mutex
	version string
	added   map[string]any
	set     map[string]any
	removed map[string]any
	calls   []string
}

func (f *fakeTransmission) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transmission/rpc" {
			http.NotFound(w, r)
			return
		}
		if u, p, _ := r.BasicAuth(); u != "user" || p != "pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Transmission-Session-Id") != "sid-1" {
			w.Header().Set("X-Transmission-Session-Id", "sid-1")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var req struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad body: %v", err)
		}
		f.mu.Lock()
		f.calls = append(f.calls, req.Method)
		f.mu.Unlock()
		reply := func(args any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "success", "arguments": args})
		}
		switch req.Method {
		case "session-get":
			reply(map[string]any{"version": f.version, "rpc-version": 17, "download-dir": "/data/downloads"})
		case "torrent-add":
			f.mu.Lock()
			f.added = req.Arguments
			f.mu.Unlock()
			reply(map[string]any{"torrent-added": map[string]any{"hashString": strings.ToLower(hashA), "id": 1, "name": "x"}})
		case "torrent-set":
			f.mu.Lock()
			f.set = req.Arguments
			f.mu.Unlock()
			reply(map[string]any{})
		case "torrent-remove":
			f.mu.Lock()
			f.removed = req.Arguments
			f.mu.Unlock()
			reply(map[string]any{})
		case "torrent-get":
			fields, _ := req.Arguments["fields"].([]any)
			if len(fields) == 2 { // files
				reply(map[string]any{"torrents": []any{map[string]any{"hashString": strings.ToLower(hashA),
					"files": []any{map[string]any{"name": "Film/Film.mkv", "length": 1000, "bytesCompleted": 0}}}}})
				return
			}
			reply(map[string]any{"torrents": []any{map[string]any{
				"hashString": strings.ToLower(hashA), "name": "Film: The Movie", "downloadDir": "/data/downloads/cinearr",
				"totalSize": 5000, "leftUntilDone": 0, "isFinished": false, "eta": -1, "status": 6, "error": 0, "errorString": "",
				"percentDone": 1.0, "rateDownload": 0, "peersSendingToUs": 0}}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "method name not recognized"})
		}
	})
}

func TestTransmission(t *testing.T) {
	for _, tc := range []struct {
		version   string
		wantLabel bool
		wantDir   string
	}{
		{"4.0.5 (a6fe2a64aa)", true, ""},
		{"3.00 (bb6b5a062e)", false, "/data/downloads/cinearr"},
	} {
		f := &fakeTransmission{version: tc.version}
		srv := httptest.NewServer(f.handler(t))
		c := New(Settings{Kind: Transmission, URL: srv.URL, Username: "user", Password: "pass", Category: "cinearr", RatioLimit: 2, SeedingTimeLimit: 60})
		ctx := context.Background()
		res, err := c.Test(ctx)
		if err != nil || res.Version != tc.version {
			t.Fatalf("%s: test: %v %+v", tc.version, err, res)
		}
		torrentFile := []byte("d4:infod4:name1:xee")
		if err := c.Add(ctx, AddRequest{File: torrentFile, FileName: "x.torrent", Hash: hashA}); err != nil {
			t.Fatalf("add: %v", err)
		}
		if got, _ := f.added["metainfo"].(string); got != base64.StdEncoding.EncodeToString(torrentFile) {
			t.Errorf("metainfo = %q", got)
		}
		_, hasLabels := f.added["labels"]
		if hasLabels != tc.wantLabel {
			t.Errorf("%s: labels sent = %v", tc.version, hasLabels)
		}
		if dir, _ := f.added["download-dir"].(string); dir != tc.wantDir {
			t.Errorf("%s: download-dir = %q, want %q", tc.version, dir, tc.wantDir)
		}
		if f.set["seedRatioLimit"] != 2.0 || f.set["seedIdleLimit"] != 60.0 {
			t.Errorf("seeding config = %v", f.set)
		}
		list, err := c.Torrents(ctx, []string{hashA})
		if err != nil || len(list) != 1 {
			t.Fatalf("torrents: %v %v", err, list)
		}
		tr := list[0]
		if tr.Hash != hashA || !tr.Completed || tr.Status != "completed" || tr.ContentPath != "/data/downloads/cinearr/Film_ The Movie" {
			t.Errorf("torrent = %+v", tr)
		}
		files, err := c.Files(ctx, hashA)
		if err != nil || len(files) != 1 || files[0].Path != "Film/Film.mkv" {
			t.Errorf("files = %v %v", files, err)
		}
		if err := c.Remove(ctx, hashA, true); err != nil || f.removed["delete-local-data"] != true {
			t.Errorf("remove: %v %v", err, f.removed)
		}
		srv.Close()
	}
}

func TestTransmissionAuth(t *testing.T) {
	f := &fakeTransmission{version: "4.0.0"}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	_, err := New(Settings{Kind: Transmission, URL: srv.URL + "/transmission/web/", Username: "user", Password: "bad"}).Test(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err = %v", err)
	}
}

func TestTransmissionStatus(t *testing.T) {
	cases := []struct {
		t    trTorrent
		want string
		done bool
	}{
		{trTorrent{TotalSize: 0, Status: trDownloading}, "metadata", false},
		{trTorrent{TotalSize: 10, LeftUntilDone: 5, Status: trDownloading, RateDownload: 10}, "downloading", false},
		{trTorrent{TotalSize: 10, LeftUntilDone: 5, Status: trDownloading}, "stalled", false},
		{trTorrent{TotalSize: 10, LeftUntilDone: 5, Status: trStopped}, "paused", false},
		{trTorrent{TotalSize: 10, LeftUntilDone: 5, Status: trQueued}, "queued", false},
		{trTorrent{TotalSize: 10, LeftUntilDone: 0, Status: trStopped, IsFinished: true}, "completed", true},
		{trTorrent{TotalSize: 10, LeftUntilDone: 5, Status: trDownloading, Error: 2, ErrorString: "tracker"}, "stalled", false},
		{trTorrent{TotalSize: 10, LeftUntilDone: 5, Status: trStopped, Error: 3, ErrorString: "No data found"}, "error", false},
	}
	for i, c := range cases {
		got := c.t.normalise()
		if got.Status != c.want || got.SeedingDone != c.done {
			t.Errorf("%d: status %q done %v, want %q %v", i, got.Status, got.SeedingDone, c.want, c.done)
		}
	}
}

// ---------------------------------------------------------------- Deluge

func TestDeluge(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var addOpts map[string]any
	var label []any
	connected := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
			ID     int    `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		calls = append(calls, req.Method)
		mu.Unlock()
		reply := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "result": v, "error": nil}) }
		if req.Method != "auth.login" {
			if c, err := r.Cookie("_session_id"); err != nil || c.Value != "abc" {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "result": nil, "error": map[string]any{"message": "Not authenticated", "code": 1}})
				return
			}
		}
		switch req.Method {
		case "auth.login":
			if req.Params[0] == "deluge" {
				http.SetCookie(w, &http.Cookie{Name: "_session_id", Value: "abc", Path: "/"})
				reply(true)
			} else {
				reply(false)
			}
		case "web.connected":
			reply(connected)
		case "web.get_hosts":
			reply([]any{[]any{"host-1", "127.0.0.1", 58846, "localclient"}})
		case "web.connect":
			connected = req.Params[0] == "host-1"
			reply([]any{})
		case "system.listMethods":
			reply([]string{"daemon.get_version", "core.add_torrent_magnet", "label.add", "label.get_labels", "label.set_torrent"})
		case "daemon.get_version":
			reply("2.1.1")
		case "label.get_labels":
			reply([]string{"tv"})
		case "label.add":
			reply(nil)
		case "core.add_torrent_magnet":
			addOpts, _ = req.Params[1].(map[string]any)
			reply(strings.ToLower(hashA))
		case "label.set_torrent":
			label = req.Params
			reply(nil)
		case "core.set_torrent_options":
			reply(nil)
		case "core.get_torrents_status":
			reply(map[string]any{strings.ToLower(hashA): map[string]any{"hash": strings.ToLower(hashA), "name": "Film", "state": "Downloading",
				"progress": 42.0, "eta": 120, "save_path": `D:\Torrents`, "total_size": 1000, "download_payload_rate": 512.0, "num_seeds": 3}})
		case "core.get_torrent_status":
			reply(map[string]any{"files": []any{map[string]any{"index": 0, "path": "Film/Film.mkv", "size": 1000}}})
		case "core.remove_torrent":
			reply(true)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": req.ID, "result": nil, "error": map[string]any{"message": "Unknown method", "code": 2}})
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if _, err := New(Settings{Kind: Deluge, URL: srv.URL, Password: "bad"}).Test(ctx); err == nil {
		t.Fatal("wrong password accepted")
	}
	c := New(Settings{Kind: Deluge, URL: srv.URL + "/", Password: "deluge", Category: "Cinearr", SavePath: "/dl", RatioLimit: 1.5})
	res, err := c.Test(ctx)
	if err != nil || res.Version != "2.1.1" || res.Warning != "" {
		t.Fatalf("test: %v %+v", err, res)
	}
	if !connected {
		t.Error("daemon not connected")
	}
	if err := c.Add(ctx, AddRequest{Magnet: "magnet:?xt=urn:btih:" + hashA, Hash: hashA}); err != nil {
		t.Fatal(err)
	}
	if addOpts["download_location"] != "/dl" || len(label) != 2 || label[1] != "cinearr" {
		t.Errorf("opts %v label %v", addOpts, label)
	}
	list, err := c.Torrents(ctx, []string{hashA})
	if err != nil || len(list) != 1 {
		t.Fatalf("torrents: %v %v", err, list)
	}
	if tr := list[0]; tr.Status != "downloading" || tr.Progress != 0.42 || tr.ContentPath != `D:\Torrents\Film` || tr.ETA != 120 {
		t.Errorf("torrent = %+v", tr)
	}
	files, err := c.Files(ctx, hashA)
	if err != nil || len(files) != 1 || files[0].Size != 1000 {
		t.Errorf("files = %v %v", files, err)
	}
	if err := c.Remove(ctx, hashA, false); err != nil {
		t.Error(err)
	}
}

// ---------------------------------------------------------------- µTorrent

func TestUTorrent(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	var uploaded []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "admin" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/gui/token.html" {
			http.SetCookie(w, &http.Cookie{Name: "GUID", Value: "g1", Path: "/"})
			_, _ = io.WriteString(w, "<html><div id='token' style='display:none;'>TOK123</div></html>")
			return
		}
		if r.URL.Path != "/gui/" || r.URL.Query().Get("token") != "TOK123" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		q := r.URL.Query()
		switch {
		case q.Get("action") == "getsettings":
			_, _ = io.WriteString(w, `{"build":46302,"settings":[]}`)
		case q.Get("action") == "add-file":
			f, _, err := r.FormFile("torrent_file")
			if err == nil {
				uploaded, _ = io.ReadAll(f)
			}
			_, _ = io.WriteString(w, `{"build":46302}`)
		case q.Get("list") == "1":
			_, _ = io.WriteString(w, `{"build":46302,"label":[],"torrents":[
				["`+hashA+`",201,"Film",1000,1000,1000,500,500,0,0,0,"cinearr",0,0,0,0,65536,1,0,"","","Seeding","",1700000000,1700000100,"","C:\\Downloads\\Film",0,""],
				["FFFF56789ABCDEF0123456789ABCDEF01234567",137,"Other",1000,200,200,0,0,0,0,30,"",0,0,0,0,0,2,800,"","","Downloading","",0,0,"","C:\\Downloads",0,""]],
				"torrentc":"123"}`)
		case q.Get("action") == "getfiles":
			_, _ = io.WriteString(w, `{"build":46302,"files":["`+hashA+`",[["Film.mkv",1000,1000,2,0,1,true,0,0,0,0,0,0]]]}`)
		default:
			_, _ = io.WriteString(w, `{"build":46302}`)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if _, err := New(Settings{Kind: UTorrent, URL: srv.URL, Username: "admin", Password: "x"}).Test(ctx); err == nil {
		t.Fatal("wrong password accepted")
	}
	c := New(Settings{Kind: UTorrent, URL: srv.URL + "/gui/", Username: "admin", Password: "secret", Category: "cinearr", RatioLimit: 1})
	if res, err := c.Test(ctx); err != nil || res.Version != "build 46302" {
		t.Fatalf("test: %v %+v", err, res)
	}
	if err := c.Add(ctx, AddRequest{File: []byte("d4:infod4:name1:xee"), FileName: "x.torrent", Hash: hashA}); err != nil {
		t.Fatal(err)
	}
	if string(uploaded) != "d4:infod4:name1:xee" {
		t.Errorf("uploaded = %q", uploaded)
	}
	joined := strings.Join(queries, "\n")
	if !strings.Contains(joined, "action=setprops&hash="+hashA+"&s=label&v=cinearr") ||
		!strings.Contains(joined, "s=seed_override&v=1&s=seed_ratio&v=1000") {
		t.Errorf("queries:\n%s", joined)
	}
	list, err := c.Torrents(ctx, []string{strings.ToLower(hashA)})
	if err != nil || len(list) != 1 {
		t.Fatalf("torrents: %v %v", err, list)
	}
	// status 201 = loaded + checked + started: completed, still seeding
	if tr := list[0]; !tr.Completed || tr.SeedingDone || tr.ContentPath != `C:\Downloads\Film` {
		t.Errorf("torrent = %+v", tr)
	}
	files, err := c.Files(ctx, hashA)
	if err != nil || len(files) != 1 || files[0].Path != "Film.mkv" {
		t.Errorf("files = %v %v", files, err)
	}
	if err := c.Remove(ctx, hashA, true); err != nil || !strings.Contains(strings.Join(queries, "\n"), "action=removedata") {
		t.Errorf("remove: %v", err)
	}
}

func TestJoinRemote(t *testing.T) {
	for _, c := range [][3]string{
		{"/data/dl/", "Film", "/data/dl/Film"},
		{`D:\Torrents\`, "Film", `D:\Torrents\Film`},
		{"", "Film", "Film"},
	} {
		if got := joinRemote(c[0], c[1]); got != c[2] {
			t.Errorf("joinRemote(%q, %q) = %q", c[0], c[1], got)
		}
	}
}
