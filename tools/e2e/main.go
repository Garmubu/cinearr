// Command e2e runs Cinearr against local fakes (TMDB, a Cardigann tracker,
// qBittorrent, Transmission) so the whole flow can be exercised without network access:
// setup, indexer, search, safety checks, grab, download, import, playback.
//
//	go run ./tools/e2e -addr 127.0.0.1:7980
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cinearr/internal/api"
	"cinearr/internal/app"
	"cinearr/internal/diag"
	"cinearr/internal/store"
	"cinearr/internal/tmdb"
	"cinearr/internal/torrent"
	"cinearr/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7980", "UI address")
	root := flag.String("root", filepath.Join(os.TempDir(), "cinearr-e2e"), "work folder")
	flag.Parse()
	_ = os.RemoveAll(*root)
	dataDir := filepath.Join(*root, "data")
	dlDir := filepath.Join(*root, "downloads")
	_ = os.MkdirAll(filepath.Join(dataDir, "definitions", "custom"), 0o755)
	_ = os.MkdirAll(dlDir, 0o755)

	tmdbURL := serve(fakeTMDB())
	trackerURL := serve(fakeTracker())
	qbitURL := serve(newFakeQbit(dlDir).handler())
	trURL := serve(newFakeTransmission(dlDir).handler())
	tmdb.APIBase = tmdbURL

	def := strings.ReplaceAll(definition, "TRACKER", trackerURL)
	_ = os.WriteFile(filepath.Join(dataDir, "definitions", "custom", "demotracker.yml"), []byte(def), 0o644)

	st, err := store.Open(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	logger, _ := diag.Setup(dataDir, true) // the log stays open until the process ends
	a := app.New(st, logger)
	ctx := context.Background()
	go a.Run(ctx)
	srv := &api.Server{App: a, UI: web.Files()}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	info, _ := json.Marshal(map[string]string{"qbit": qbitURL, "transmission": trURL, "tracker": trackerURL, "library": filepath.Join(*root, "library"),
		"series": filepath.Join(*root, "series"), "data": dataDir})
	fmt.Println("E2E", string(info))
	log.Fatal(http.Serve(ln, srv.Handler()))
}

func serve(h http.Handler) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go http.Serve(ln, h)
	return "http://" + ln.Addr().String()
}

// ---------------------------------------------------------------- TMDB fake

func fakeTMDB() http.Handler {
	inception := map[string]any{"id": 27205, "title": "Inception", "original_title": "Inception", "original_language": "en",
		"overview":     "Dom Cobb est un voleur expérimenté dans l'art périlleux de l'extraction : sa spécialité consiste à s'approprier les secrets les plus précieux d'un individu, enfouis au plus profond de son subconscient, pendant qu'il rêve.",
		"release_date": "2010-07-15", "vote_average": 8.4, "poster_path": "", "backdrop_path": ""}
	others := []map[string]any{inception,
		{"id": 157336, "title": "Interstellar", "original_title": "Interstellar", "release_date": "2014-11-05", "vote_average": 8.4, "overview": "Une équipe d'explorateurs voyage à travers un trou de ver."},
		{"id": 603, "title": "Matrix", "original_title": "The Matrix", "release_date": "1999-03-30", "vote_average": 8.2, "overview": "Programmeur le jour, hacker la nuit."},
		{"id": 129, "title": "Le Voyage de Chihiro", "original_title": "千と千尋の神隠し", "release_date": "2001-07-20", "vote_average": 8.5},
		{"id": 680, "title": "Pulp Fiction", "original_title": "Pulp Fiction", "release_date": "1994-09-10", "vote_average": 8.5},
		{"id": 496243, "title": "Parasite", "original_title": "기생충", "release_date": "2019-05-30", "vote_average": 8.5},
		// not released yet: must not appear in the trending list
		{"id": 1003596, "title": "Avengers : Doomsday", "original_title": "Avengers: Doomsday", "release_date": "2099-12-16", "vote_average": 0},
	}
	mux := http.NewServeMux()
	list := func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": others, "page": 1, "total_pages": 1})
	}
	mux.HandleFunc("/trending/movie/week", list)
	mux.HandleFunc("/movie/popular", list)
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("query"))
		var out []map[string]any
		for _, m := range others {
			if strings.Contains(strings.ToLower(m["title"].(string)), q) {
				out = append(out, m)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": out, "page": 1, "total_pages": 1})
	})
	// series: Twin Peaks (3 seasons) and an unaired series
	twin := map[string]any{"id": 1920, "name": "Twin Peaks", "original_name": "Twin Peaks", "original_language": "en",
		"overview": "The body of Laura Palmer is found wrapped in plastic.", "first_air_date": "1990-04-08", "vote_average": 8.3}
	tvList := []map[string]any{twin,
		{"id": 1399, "name": "Game of Thrones", "original_name": "Game of Thrones", "first_air_date": "2011-04-17", "vote_average": 8.5},
		{"id": 999001, "name": "Future Show", "original_name": "Future Show", "first_air_date": "2099-01-01"},
	}
	seasonEps := map[int]int{0: 1, 1: 8, 2: 22, 3: 18}
	tvl := func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": tvList, "page": 1, "total_pages": 1})
	}
	mux.HandleFunc("/trending/tv/week", tvl)
	mux.HandleFunc("/tv/popular", tvl)
	mux.HandleFunc("/search/tv", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("query"))
		var out []map[string]any
		for _, m := range tvList {
			if strings.Contains(strings.ToLower(m["name"].(string)), q) {
				out = append(out, m)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": out, "page": 1, "total_pages": 1})
	})
	mux.HandleFunc("/tv/1920", func(w http.ResponseWriter, r *http.Request) {
		d := map[string]any{}
		for k, v := range twin {
			d[k] = v
		}
		var seasons []map[string]any
		for n := 0; n <= 3; n++ {
			seasons = append(seasons, map[string]any{"season_number": n, "name": fmt.Sprintf("Season %d", n), "episode_count": seasonEps[n]})
		}
		d["seasons"] = seasons
		d["status"] = "Ended"
		d["episode_run_time"] = []int{47}
		d["number_of_seasons"] = 3
		d["number_of_episodes"] = 48
		d["networks"] = []map[string]string{{"name": "ABC"}}
		d["genres"] = []map[string]string{{"name": "Drama"}, {"name": "Mystery"}}
		d["created_by"] = []map[string]string{{"name": "David Lynch"}, {"name": "Mark Frost"}}
		d["external_ids"] = map[string]any{"imdb_id": "tt0098936", "tvdb_id": 70533}
		json.NewEncoder(w).Encode(d)
	})
	mux.HandleFunc("/tv/1920/season/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/credits") {
			json.NewEncoder(w).Encode(map[string]any{
				"cast":        []map[string]any{{"id": 1, "name": "Kyle MacLachlan", "character": "Dale Cooper"}, {"id": 2, "name": "Sheryl Lee", "character": "Laura Palmer"}},
				"guest_stars": []map[string]any{{"id": 3, "name": "David Lynch", "character": "Gordon Cole"}},
				"crew":        []map[string]any{{"id": 3, "name": "David Lynch", "job": "Director"}, {"id": 4, "name": "Mark Frost", "job": "Writer"}}})
			return
		}
		var n int
		fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/tv/1920/season/"), "%d", &n)
		var eps []map[string]any
		for e := 1; e <= seasonEps[n]; e++ {
			air := fmt.Sprintf("%d-10-%02d", 1989+n, e)
			if n == 3 {
				air = fmt.Sprintf("2017-05-%02d", e)
			}
			eps = append(eps, map[string]any{"episode_number": e, "season_number": n, "name": fmt.Sprintf("Episode %d", e+8*(n-1)), "air_date": air,
				"overview": "Agent Cooper investigates.", "runtime": 47})
		}
		json.NewEncoder(w).Encode(map[string]any{"season_number": n, "name": fmt.Sprintf("Season %d", n), "episodes": eps})
	})
	mux.HandleFunc("/movie/", func(w http.ResponseWriter, r *http.Request) {
		d := map[string]any{}
		for k, v := range inception {
			d[k] = v
		}
		d["imdb_id"] = "tt1375666"
		d["runtime"] = 148
		d["genres"] = []map[string]string{{"name": "Action"}, {"name": "Science-Fiction"}, {"name": "Aventure"}}
		d["alternative_titles"] = map[string]any{"titles": []map[string]string{{"title": "El origen", "iso_3166_1": "AR"}}}
		d["credits"] = map[string]any{"cast": []map[string]string{{"name": "Leonardo DiCaprio", "character": "Cobb"}, {"name": "Marion Cotillard", "character": "Mal"}, {"name": "Elliot Page", "character": "Ariadne"}},
			"crew": []map[string]string{{"name": "Christopher Nolan", "job": "Director"}}}
		json.NewEncoder(w).Encode(d)
	})
	return mux
}

// ------------------------------------------------------------- tracker fake

const definition = `---
id: demotracker
name: Demo Tracker
description: "Demonstration tracker (end-to-end tests)"
language: fr-FR
type: public
encoding: UTF-8
links:
  - TRACKER/
caps:
  categorymappings:
    - {id: 1, cat: Movies/HD, desc: "Films HD"}
    - {id: 2, cat: TV/HD, desc: "Series HD"}
  modes:
    search: [q]
    movie-search: [q, imdbid]
    tv-search: [q, season, ep]
settings: []
search:
  paths:
    - path: "search"
  inputs:
    imdb: "{{ .Query.IMDBID }}"
    q: "{{ .Keywords }}"
  rows:
    selector: table.results > tbody > tr
  fields:
    category:
      text: 1
    title:
      selector: td.title a
    details:
      selector: td.title a
      attribute: href
    download:
      selector: td.dl a
      attribute: href
    size:
      selector: td.size
    seeders:
      selector: td.seed
    leechers:
      selector: td.leech
    date:
      selector: td.date
`

type fakeRelease struct {
	title, size, seed string
	files             []torrent.File
}

var releases = []fakeRelease{
	{"Inception.2010.MULTi.1080p.BluRay.x264-NEWGRP", "9.5 GB", "512", []torrent.File{{Path: "Inception.2010.MULTi.1080p.BluRay.x264-NEWGRP.mkv.exe", Size: 2 << 20}, {Path: "README.txt", Size: 1024}}},
	{"Inception.2010.MULTi.1080p.BluRay.x264-QTZ", "8.7 GB", "230", []torrent.File{{Path: "Inception.2010.MULTi.1080p.BluRay.x264-QTZ.mkv", Size: 8 << 30}, {Path: "Inception.2010.MULTi.1080p.BluRay.x264-QTZ.nfo", Size: 4096}}},
	{"Inception.2010.TRUEFRENCH.720p.WEB-DL.x264-FRATERNiTY", "3.1 GB", "88", []torrent.File{{Path: "Inception.mkv", Size: 3 << 30}}},
	{"Inception.2010.1080p.BluRay.x264-SPARKS", "300 MB", "1200", []torrent.File{{Path: "Inception.mkv", Size: 300 << 20}}},
	{"Inception.2010.HDCAM.x264-FAKE", "1.4 GB", "40", []torrent.File{{Path: "Inception.HDCAM.avi", Size: 1 << 30}}},
	{"Inception.2010.2160p.UHD.BluRay.REMUX.HDR.HEVC.Atmos-FGT", "63.2 GB", "15", []torrent.File{{Path: "Inception.mkv", Size: 63 << 30}}},
	{"Shutter.Island.2010.1080p.BluRay.x264-AMIABLE", "8.0 GB", "300", []torrent.File{{Path: "Shutter.Island.mkv", Size: 8 << 30}}},
}

func episodeFiles(name string, eps ...int) []torrent.File {
	var out []torrent.File
	size := int64(2 << 30)
	if len(eps) > 1 {
		size = 550 << 20
	}
	for _, e := range eps {
		out = append(out, torrent.File{Path: strings.Replace(name, "S02", fmt.Sprintf("S02E%02d", e), 1) + ".mkv", Size: size})
	}
	return out
}

var tvReleases = func() []fakeRelease {
	pack := make([]int, 22)
	for i := range pack {
		pack[i] = i + 1
	}
	return []fakeRelease{
		{"Twin.Peaks.S02E01.1080p.BluRay.x264-SHORTBREHD", "2.1 GB", "60", episodeFiles("Twin.Peaks.S02.1080p.BluRay.x264-SHORTBREHD", 1)},
		{"Twin.Peaks.S02E02.1080p.BluRay.x264-SHORTBREHD", "2.1 GB", "58", episodeFiles("Twin.Peaks.S02.1080p.BluRay.x264-SHORTBREHD", 2)},
		{"Twin.Peaks.S02E03.1080p.BluRay.x264-SHORTBREHD", "2.1 GB", "55", episodeFiles("Twin.Peaks.S02.1080p.BluRay.x264-SHORTBREHD", 3)},
		{"Twin.Peaks.S02E02.1080p.WEB.x264-EVIL", "2.0 GB", "900", []torrent.File{{Path: "Twin.Peaks.S02E02.mkv.exe", Size: 3 << 20}}},
		{"Twin.Peaks.S02.1080p.BluRay.x264-SHORTBREHD", "11.8 GB", "40", episodeFiles("Twin.Peaks.S02.1080p.BluRay.x264-SHORTBREHD", pack...)},
		{"Twin.Peaks.S02E01.720p.HDTV.x264-LOL", "900 MB", "12", episodeFiles("Twin.Peaks.S02.720p.HDTV.x264-LOL", 1)},
	}
}()

// matches emulates a tracker search: every word of the query appears in
// the release name.
func matches(title, q string) bool {
	t := strings.ToLower(strings.NewReplacer(".", " ", "-", " ").Replace(title))
	for _, w := range strings.Fields(strings.ToLower(q)) {
		if !strings.Contains(t, w) {
			return false
		}
	}
	return true
}

func bencodeTorrent(name string, files []torrent.File) []byte {
	var info strings.Builder
	info.WriteString("d5:filesl")
	for _, f := range files {
		fmt.Fprintf(&info, "d6:lengthi%de4:pathl%d:%see", f.Size, len(f.Path), f.Path)
	}
	fmt.Fprintf(&info, "e4:name%d:%s12:piece lengthi262144e6:pieces20:%se", len(name), name, strings.Repeat("x", 20))
	ann := "http://tracker/announce"
	return []byte(fmt.Sprintf("d8:announce%d:%s4:info%se", len(ann), ann, info.String()))
}

func fakeTracker() http.Handler {
	mux := http.NewServeMux()
	all := append(append([]fakeRelease{}, releases...), tvReleases...)
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		var b strings.Builder
		b.WriteString(`<html><body><table class="results"><tr><th>Nom</th></tr>`)
		for i, rl := range all {
			tv := i >= len(releases)
			if (q == "" && tv) || (q != "" && !matches(rl.title, q)) {
				continue
			}
			fmt.Fprintf(&b, `<tr><td class="title"><a href="/t/%d">%s</a></td><td class="dl"><a href="/dl/%d.torrent">DL</a></td><td class="size">%s</td><td class="seed">%s</td><td class="leech">3</td><td class="date">%d hours ago</td></tr>`,
				i, rl.title, i, rl.size, rl.seed, 2+i*5)
		}
		b.WriteString(`</table></body></html>`)
		io.WriteString(w, b.String())
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		var i int
		fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/dl/"), "%d.torrent", &i)
		if i < 0 || i >= len(all) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		w.Write(bencodeTorrent(all[i].title, all[i].files))
	})
	return mux
}

// ---------------------------------------------------------- qBittorrent fake

type fakeTorrent struct {
	Hash, Name, Category, State, ContentPath, SavePath string
	Size                                               int64
	Progress                                           float64
	polls                                              int
	files                                              []torrent.File
}

type fakeQbit struct {
	dir string
	mu  sync.Mutex
	ts  map[string]*fakeTorrent
}

func newFakeQbit(dir string) *fakeQbit { return &fakeQbit{dir: dir, ts: map[string]*fakeTorrent{}} }

func (q *fakeQbit) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "x", Path: "/"})
		io.WriteString(w, "Ok.")
	})
	mux.HandleFunc("/api/v2/app/version", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "v5.0.4") })
	mux.HandleFunc("/api/v2/app/webapiVersion", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "2.11.2") })
	mux.HandleFunc("/api/v2/torrents/categories", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{}") })
	mux.HandleFunc("/api/v2/torrents/createCategory", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/api/v2/torrents/delete", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		q.mu.Lock()
		delete(q.ts, strings.ToLower(r.FormValue("hashes")))
		q.mu.Unlock()
	})
	mux.HandleFunc("/api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f, _, err := r.FormFile("torrents")
		if err != nil {
			io.WriteString(w, "Fails.")
			return
		}
		data, _ := io.ReadAll(f)
		info, err := torrent.Parse(data)
		if err != nil {
			io.WriteString(w, "Fails.")
			return
		}
		h := strings.ToLower(info.InfoHash)
		content := filepath.Join(q.dir, info.Name)
		_ = os.MkdirAll(content, 0o755)
		for _, tf := range info.Files {
			p := filepath.Join(q.dir, filepath.FromSlash(tf.Path))
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			fh, _ := os.Create(p)
			fh.Truncate(tf.Size) // sparse
			fh.Close()
		}
		q.mu.Lock()
		q.ts[h] = &fakeTorrent{Hash: h, Name: info.Name, Category: r.FormValue("category"), State: "downloading",
			ContentPath: content, SavePath: q.dir, Size: info.Size, files: info.Files}
		q.mu.Unlock()
		io.WriteString(w, "Ok.")
	})
	mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		q.mu.Lock()
		defer q.mu.Unlock()
		var out []map[string]any
		for _, t := range q.ts {
			t.polls++
			t.Progress = float64(t.polls) / 6
			if t.Progress >= 1 {
				t.Progress, t.State = 1, "uploading"
			}
			out = append(out, map[string]any{"hash": t.Hash, "name": t.Name, "size": t.Size, "progress": t.Progress, "dlspeed": 12 << 20,
				"eta": int64((1 - t.Progress) * 600), "state": t.State, "category": t.Category, "save_path": t.SavePath,
				"content_path": t.ContentPath, "num_seeds": 42})
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/api/v2/torrents/files", func(w http.ResponseWriter, r *http.Request) {
		q.mu.Lock()
		defer q.mu.Unlock()
		t := q.ts[strings.ToLower(r.URL.Query().Get("hash"))]
		var out []map[string]any
		if t != nil {
			for _, f := range t.files {
				out = append(out, map[string]any{"name": f.Path, "size": f.Size})
			}
		}
		json.NewEncoder(w).Encode(out)
	})
	return mux
}

// ---------------------------------------------------------------- Transmission fake

type fakeTransmission struct {
	dir string
	mu  sync.Mutex
	ts  map[string]*fakeTorrent
}

func newFakeTransmission(dir string) *fakeTransmission {
	return &fakeTransmission{dir: dir, ts: map[string]*fakeTorrent{}}
}

func (f *fakeTransmission) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transmission/rpc" {
			http.NotFound(w, r)
			return
		}
		if u, p, _ := r.BasicAuth(); u != "admin" || p != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Transmission-Session-Id") != "e2e" {
			w.Header().Set("X-Transmission-Session-Id", "e2e")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var req struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reply := func(args any) { json.NewEncoder(w).Encode(map[string]any{"result": "success", "arguments": args}) }
		ids := func() map[string]bool {
			m := map[string]bool{}
			list, _ := req.Arguments["ids"].([]any)
			for _, x := range list {
				if s, ok := x.(string); ok {
					m[strings.ToLower(s)] = true
				}
			}
			return m
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		switch req.Method {
		case "session-get":
			reply(map[string]any{"version": "4.0.6 (38c164933e)", "rpc-version": 17, "download-dir": f.dir})
		case "torrent-add":
			meta, _ := req.Arguments["metainfo"].(string)
			data, err := base64.StdEncoding.DecodeString(meta)
			if err != nil {
				json.NewEncoder(w).Encode(map[string]any{"result": "invalid or corrupt torrent file"})
				return
			}
			info, err := torrent.Parse(data)
			if err != nil {
				json.NewEncoder(w).Encode(map[string]any{"result": "invalid or corrupt torrent file"})
				return
			}
			h := strings.ToLower(info.InfoHash)
			for _, tf := range info.Files {
				p := filepath.Join(f.dir, filepath.FromSlash(tf.Path))
				_ = os.MkdirAll(filepath.Dir(p), 0o755)
				fh, _ := os.Create(p)
				fh.Truncate(tf.Size)
				fh.Close()
			}
			f.ts[h] = &fakeTorrent{Hash: h, Name: info.Name, Size: info.Size, files: info.Files, SavePath: f.dir}
			reply(map[string]any{"torrent-added": map[string]any{"hashString": h, "id": len(f.ts), "name": info.Name}})
		case "torrent-set":
			reply(map[string]any{})
		case "torrent-remove":
			for h := range ids() {
				delete(f.ts, h)
			}
			reply(map[string]any{})
		case "torrent-get":
			want := ids()
			var out []map[string]any
			for h, t := range f.ts {
				if !want[h] {
					continue
				}
				fields, _ := req.Arguments["fields"].([]any)
				if len(fields) == 2 {
					var files []map[string]any
					for _, x := range t.files {
						files = append(files, map[string]any{"name": x.Path, "length": x.Size, "bytesCompleted": 0})
					}
					out = append(out, map[string]any{"hashString": h, "files": files})
					continue
				}
				t.polls++
				t.Progress = float64(t.polls) / 6
				status, left := 4, int64(float64(t.Size)*(1-t.Progress))
				if t.Progress >= 1 {
					t.Progress, status, left = 1, 6, 0
				}
				out = append(out, map[string]any{"hashString": h, "name": t.Name, "downloadDir": t.SavePath, "totalSize": t.Size,
					"leftUntilDone": left, "isFinished": false, "eta": int64((1 - t.Progress) * 600), "status": status, "error": 0,
					"errorString": "", "percentDone": t.Progress, "rateDownload": 12 << 20, "peersSendingToUs": 17})
			}
			reply(map[string]any{"torrents": out})
		default:
			json.NewEncoder(w).Encode(map[string]any{"result": "method name not recognized"})
		}
	})
}
