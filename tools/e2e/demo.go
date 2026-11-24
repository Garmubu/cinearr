package main

// Demonstration data for the screenshots of the README (E2E_DEMO=1): open
// films of the Blender Foundation (Creative Commons), public-domain
// classics and a series released under Creative Commons, so that the
// pictures only show works anyone may share. The posters are drawn by
// tools/e2e/screenshots.py.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"cinearr/internal/torrent"
)

type demoFilm struct {
	id       int
	title    string
	date     string
	vote     float64
	runtime  int
	genres   []string
	overview string
	director string
	cast     [][2]string // actor, role
	imdb     string
}

var demoFilms = []demoFilm{
	{45745, "Sintel", "2010-09-27", 7.4, 15, []string{"Animation", "Fantasy"}, "A lonely young woman searches far and wide for the baby dragon she once rescued and raised.", "Colin Levy", [][2]string{{"Halina Reijn", "Sintel"}, {"Thom Hoffman", "Shaman"}}, "tt1727587"},
	{10378, "Big Buck Bunny", "2008-05-30", 6.4, 10, []string{"Animation", "Comedy"}, "A gentle giant rabbit decides to teach a lesson to the three rodents who keep bullying the forest.", "Sacha Goedegebure", nil, "tt1254207"},
	{133701, "Tears of Steel", "2012-09-26", 5.9, 12, []string{"Science Fiction"}, "In a future Amsterdam, scientists try to save the world by reliving a painful break-up.", "Ian Hubert", [][2]string{{"Derek de Lint", "Old Thom"}, {"Sergio Hasselbaink", "Barley"}, {"Rogier Schippers", "Thom"}, {"Vanja Rukavina", "Celia"}}, "tt2285752"},
	{9761, "Elephants Dream", "2006-03-24", 5.8, 11, []string{"Animation", "Science Fiction"}, "Two men explore a strange mechanical world whose rules only one of them seems to understand.", "Bassam Kurdali", [][2]string{{"Tygo Gernandt", "Proog"}, {"Cas Jansen", "Emo"}}, "tt0807840"},
	{358332, "Cosmos Laundromat", "2015-08-10", 6.9, 12, []string{"Animation", "Fantasy"}, "A weary sheep named Franck meets a mysterious salesman who offers him the gift of a lifetime.", "Mathieu Auvray", nil, "tt4957236"},
	{10331, "Night of the Living Dead", "1968-10-04", 7.5, 96, []string{"Horror"}, "Strangers trapped in a lonely farmhouse fight through the night against the dead who rise from their graves.", "George A. Romero", [][2]string{{"Duane Jones", "Ben"}, {"Judith O'Dea", "Barbra"}, {"Karl Hardman", "Harry"}}, "tt0063350"},
	{653, "Nosferatu", "1922-03-04", 7.7, 94, []string{"Horror", "Fantasy"}, "An estate agent travels to the Carpathians to sell a house to the strange Count Orlok.", "F. W. Murnau", [][2]string{{"Max Schreck", "Count Orlok"}, {"Gustav von Wangenheim", "Hutter"}, {"Greta Schröder", "Ellen"}}, "tt0013442"},
	{961, "The General", "1926-12-31", 8.0, 79, []string{"Comedy", "Adventure"}, "A train engineer chases the spies who stole his beloved locomotive deep into enemy territory.", "Buster Keaton", [][2]string{{"Buster Keaton", "Johnnie Gray"}, {"Marion Mack", "Annabelle Lee"}}, "tt0017925"},
}

// Pioneer One (2010), a series made to be shared on BitTorrent (CC BY-NC-SA).
const demoSeriesID = 33838

func demoMovie(f demoFilm) map[string]any {
	return map[string]any{"id": f.id, "title": f.title, "original_title": f.title, "original_language": "en", "overview": f.overview,
		"release_date": f.date, "vote_average": f.vote, "poster_path": fmt.Sprintf("/demo/poster-%d.png", f.id),
		"backdrop_path": fmt.Sprintf("/demo/backdrop-%d.png", f.id)}
}

func person(i int, name, role, job string) map[string]any {
	return map[string]any{"id": i, "name": name, "character": role, "job": job, "profile_path": fmt.Sprintf("/demo/person-%d.png", i)}
}

func demoTMDB() http.Handler {
	mux := http.NewServeMux()
	var list []map[string]any
	for _, f := range demoFilms {
		list = append(list, demoMovie(f))
	}
	movies := func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": list, "page": 1, "total_pages": 1})
	}
	mux.HandleFunc("/trending/movie/week", movies)
	mux.HandleFunc("/movie/popular", movies)
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("query"))
		out := []map[string]any{}
		for _, m := range list {
			if strings.Contains(strings.ToLower(m["title"].(string)), q) {
				out = append(out, m)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": out, "page": 1, "total_pages": 1})
	})
	mux.HandleFunc("/movie/", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(strings.Split(strings.TrimPrefix(r.URL.Path, "/movie/"), "/")[0])
		for i, f := range demoFilms {
			if f.id != id {
				continue
			}
			d := demoMovie(f)
			d["imdb_id"], d["runtime"] = f.imdb, f.runtime
			var genres []map[string]string
			for _, g := range f.genres {
				genres = append(genres, map[string]string{"name": g})
			}
			d["genres"] = genres
			var cast []map[string]any
			for j, c := range f.cast {
				cast = append(cast, person(100*i+j+1, c[0], c[1], ""))
			}
			d["credits"] = map[string]any{"cast": cast, "crew": []map[string]any{person(100*i+50, f.director, "", "Director")}}
			json.NewEncoder(w).Encode(d)
			return
		}
		http.NotFound(w, r)
	})

	series := map[string]any{"id": demoSeriesID, "name": "Pioneer One", "original_name": "Pioneer One", "original_language": "en",
		"overview":       "A capsule falls from the sky over Canada. Inside, US agents find a young man who claims to be the son of Soviet cosmonauts.",
		"first_air_date": "2010-06-16", "vote_average": 6.7, "poster_path": "/demo/poster-33838.png", "backdrop_path": "/demo/backdrop-33838.png"}
	tv := func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{series}, "page": 1, "total_pages": 1})
	}
	mux.HandleFunc("/trending/tv/week", tv)
	mux.HandleFunc("/tv/popular", tv)
	mux.HandleFunc("/search/tv", tv)
	mux.HandleFunc(fmt.Sprintf("/tv/%d", demoSeriesID), func(w http.ResponseWriter, r *http.Request) {
		d := map[string]any{}
		for k, v := range series {
			d[k] = v
		}
		d["seasons"] = []map[string]any{{"season_number": 1, "name": "Season 1", "episode_count": 6}}
		d["status"], d["episode_run_time"], d["number_of_seasons"], d["number_of_episodes"] = "Ended", []int{30}, 1, 6
		d["networks"] = []map[string]string{{"name": "VODO"}}
		d["genres"] = []map[string]string{{"name": "Drama"}, {"name": "Sci-Fi & Fantasy"}}
		d["created_by"] = []map[string]string{{"name": "Josh Bernhard"}, {"name": "Bracey Smith"}}
		d["external_ids"] = map[string]any{"imdb_id": "tt1748166", "tvdb_id": 170551}
		json.NewEncoder(w).Encode(d)
	})
	mux.HandleFunc(fmt.Sprintf("/tv/%d/season/", demoSeriesID), func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/credits") {
			json.NewEncoder(w).Encode(map[string]any{
				"cast": []map[string]any{person(901, "James Rich", "Tom Taylor", ""), person(902, "Alexander Stoyanov", "Yuri Kurilenko", "")},
				"crew": []map[string]any{person(903, "Bracey Smith", "", "Director"), person(904, "Josh Bernhard", "", "Writer")}})
			return
		}
		var eps []map[string]any
		for e := 1; e <= 6; e++ {
			eps = append(eps, map[string]any{"episode_number": e, "season_number": 1, "name": fmt.Sprintf("Episode %d", e),
				"air_date": fmt.Sprintf("2010-%02d-15", 5+e), "overview": "The investigation into the capsule goes on.", "runtime": 30})
		}
		json.NewEncoder(w).Encode(map[string]any{"season_number": 1, "name": "Season 1", "episodes": eps})
	})
	return mux
}

// demoReleases are the releases of the demo tracker: good ones and traps
// (executable, abnormally small file) to show the decisions.
func demoReleases() (films, series []fakeRelease) {
	for _, f := range demoFilms {
		name := strings.NewReplacer(" ", ".", "'", "").Replace(f.title) + "." + f.date[:4]
		gb := func(per90 float64) (string, int64) {
			b := per90 * float64(f.runtime) / 90 * (1 << 30)
			return fmt.Sprintf("%.1f GB", b/(1<<30)), int64(b)
		}
		s1, b1 := gb(8.5)
		s2, b2 := gb(4.2)
		s3, b3 := gb(32)
		films = append(films,
			fakeRelease{name + ".1080p.BluRay.x264-OPEN", s1, "420", []torrent.File{{Path: name + ".1080p.BluRay.x264-OPEN.mkv", Size: b1}}},
			fakeRelease{name + ".720p.WEB-DL.x264-OPEN", s2, "180", []torrent.File{{Path: name + ".720p.WEB-DL.x264-OPEN.mkv", Size: b2}}},
			fakeRelease{name + ".2160p.UHD.BluRay.x265-OPEN", s3, "35", []torrent.File{{Path: name + ".2160p.UHD.BluRay.x265-OPEN.mkv", Size: b3}}},
			fakeRelease{name + ".1080p.BluRay.x264-FREE", "120 MB", "960", []torrent.File{{Path: name + ".1080p.mkv.exe", Size: 3 << 20}}},
			fakeRelease{name + ".HDCAM.x264-CAM", "700 MB", "60", []torrent.File{{Path: name + ".HDCAM.avi", Size: 700 << 20}}},
		)
	}
	all := []int{1, 2, 3, 4, 5, 6}
	ep := func(group string, eps ...int) []torrent.File {
		var out []torrent.File
		for _, e := range eps {
			out = append(out, torrent.File{Path: fmt.Sprintf("Pioneer.One.S01E%02d.720p.WEB.x264-%s.mkv", e, group), Size: 900 << 20})
		}
		return out
	}
	for _, e := range all {
		series = append(series, fakeRelease{fmt.Sprintf("Pioneer.One.S01E%02d.720p.WEB.x264-VODO", e), "900 MB", strconv.Itoa(80 - 9*e), ep("VODO", e)})
	}
	series = append(series, fakeRelease{"Pioneer.One.S01.720p.WEB.x264-VODO", "5.3 GB", "55", ep("VODO", all...)})
	return films, series
}
