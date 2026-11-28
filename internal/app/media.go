package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cinearr/internal/backoff"
	"cinearr/internal/desktop"
	"cinearr/internal/i18n"
	"cinearr/internal/model"
	"cinearr/internal/parser"
	"cinearr/internal/player"
	"cinearr/internal/quality"
	"cinearr/internal/subtitles"
)

// Subtitles (Bazarr) and playback, for films and episodes.

func (a *App) movieVideo(id int) (subtitles.Video, error) {
	m, ok := a.Store.Movie(id)
	if !ok {
		return subtitles.Video{}, i18n.New("film not found")
	}
	if m.File == nil {
		return subtitles.Video{}, i18n.New("the film has no file yet")
	}
	v := subtitles.Video{Path: m.File.Path, ImdbID: m.ImdbID, TmdbID: m.TmdbID, Title: m.Title, Year: m.Year,
		ReleaseName: m.File.ReleaseTitle, AltTitles: []string{m.Title, m.EnglishTitle}}
	if v.ReleaseName == "" {
		v.ReleaseName = filepath.Base(m.File.Path)
	}
	if m.OriginalTitle != "" {
		v.Title = m.OriginalTitle
	}
	hashVideo(&v)
	return v, nil
}

func (a *App) episode(id, season, number int) (model.Series, model.Episode, error) {
	s, ok := a.Store.Series(id)
	if !ok {
		return s, model.Episode{}, i18n.New("series not found")
	}
	e := s.Episode(season, number)
	if e == nil {
		return s, model.Episode{}, i18n.New("episode not found")
	}
	return s, *e, nil
}

func (a *App) episodeVideo(id, season, number int) (model.Series, subtitles.Video, error) {
	s, e, err := a.episode(id, season, number)
	if err != nil {
		return s, subtitles.Video{}, err
	}
	if e.File == nil {
		return s, subtitles.Video{}, i18n.New("the episode has no file yet")
	}
	v := subtitles.Video{Path: e.File.Path, ImdbID: s.ImdbID, TmdbID: s.TmdbID, Title: s.Title, Year: s.Year,
		ReleaseName: e.File.ReleaseTitle, AltTitles: []string{s.Title, s.EnglishTitle}, Season: e.Season, Episode: e.Number,
		TvdbID: s.TvdbID}
	// the release of a season pack does not name the episode: the file does
	if pe := parser.ParseEpisodeTitle(v.ReleaseName); pe == nil || len(pe.Episodes) == 0 {
		v.ReleaseName = filepath.Base(e.File.Path)
	}
	if s.OriginalTitle != "" {
		v.Title = s.OriginalTitle
	}
	hashVideo(&v)
	return s, v, nil
}

// hashVideo sets the OpenSubtitles hash of the video, when it can be read.
func hashVideo(v *subtitles.Video) {
	if h, size, err := subtitles.Hash(v.Path); err == nil {
		v.Hash, v.Size = h, size
	}
}

// searchSubtitles queries every configured provider.
func (a *App) searchSubtitles(ctx context.Context, v subtitles.Video) ([]subtitles.Candidate, []string, error) {
	provs := a.SubtitleProviders()
	st := a.Store.Settings().Subtitles
	if len(provs) == 0 {
		return nil, nil, errors.New(SubtitleSetupProblem(st))
	}
	cs, errs := subtitles.SearchAll(ctx, provs, v, st.Languages, st.HearingImpaired)
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	if v.IsEpisode() && len(cs) == 0 && len(msgs) == 0 {
		series := false
		for _, p := range provs {
			if sc, ok := p.(interface{ SupportsEpisodes() bool }); ok && sc.SupportsEpisodes() {
				series = true
			}
		}
		if !series {
			msgs = append(msgs, i18n.T("None of your subtitle providers has series: add Addic7ed (Gestdown), OpenSubtitles.com or another provider marked “Series” in Settings › Subtitles."))
		}
	}
	return cs, msgs, nil
}

// saveSubtitle downloads a candidate and writes it next to the video.
func (a *App) saveSubtitle(ctx context.Context, c subtitles.Candidate, v subtitles.Video) (model.SubtitleFile, error) {
	p := subtitles.Find(a.SubtitleProviders(), c.Provider)
	if p == nil {
		return model.SubtitleFile{}, i18n.Errorf("provider %q not configured", c.Provider)
	}
	if err := subtitles.Skipped(ctx, c.Provider); err != nil {
		return model.SubtitleFile{}, err
	}
	data, err := p.Download(ctx, subtitles.WithRef(c))
	if err != nil {
		subtitles.Failed(ctx, c.Provider, err)
		return model.SubtitleFile{}, err
	}
	sf, err := writeSubtitle(v, data, "", c.Language, c.HI, c.Forced)
	if err != nil {
		return sf, err
	}
	sf.Provider, sf.Score = c.Provider, c.Percent
	return sf, nil
}

// subtitleExts are the subtitle formats kept.
var subtitleExts = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".vtt": true, ".sub": true}

// writeSubtitle saves a subtitle (or the right one of a zip archive) as
// "<video>.<lang>[.hi|.forced].<ext>", converted to UTF-8 and checked.
// name is the original file name when known.
func writeSubtitle(v subtitles.Video, data []byte, name, lang string, hi, forced bool) (model.SubtitleFile, error) {
	content, ext, err := subtitles.ExtractSubtitleFor(data, v.Season, v.Episode)
	if err != nil {
		return model.SubtitleFile{}, err
	}
	if e := strings.ToLower(filepath.Ext(name)); subtitleExts[e] {
		ext = e
	}
	if !subtitleExts[ext] {
		return model.SubtitleFile{}, i18n.New("not a subtitle file (.srt, .ass, .ssa, .vtt, .sub or a .zip containing one)")
	}
	// converted to UTF-8, then checked: a web page or a file without any
	// timed line is refused, and the extension follows the real format
	content = subtitles.ToUTF8(content)
	if ext, err = subtitles.Check(content, ext); err != nil {
		return model.SubtitleFile{}, err
	}
	if lang, err = subtitles.NormalizeLanguage(lang); err != nil {
		return model.SubtitleFile{}, err
	}
	path := subtitles.SavePath(v.Path, lang, hi, forced, ext)
	if filepath.Dir(path) != filepath.Dir(v.Path) {
		return model.SubtitleFile{}, i18n.Errorf("invalid subtitle file name: %s", filepath.Base(path))
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return model.SubtitleFile{}, err
	}
	return model.SubtitleFile{Path: path, Language: lang, HI: hi, Forced: forced}, nil
}

// ImportSubtitle adds a subtitle file chosen by the user (downloaded from
// any site) to a film.
func (a *App) ImportSubtitle(id int, data []byte, name, lang string) error {
	v, err := a.movieVideo(id)
	if err != nil {
		return err
	}
	sf, err := writeSubtitle(v, data, name, lang, false, false)
	if err != nil {
		return err
	}
	sf.Provider = "manual"
	a.Store.UpdateMovie(id, func(m *model.Movie) { m.Subtitles = withSubtitle(m.Subtitles, sf) })
	a.history(id, "subtitle", "%s subtitles added from a file (%s)", strings.ToUpper(sf.Language), filepath.Base(name))
	return nil
}

// ImportEpisodeSubtitle adds a subtitle file chosen by the user to an
// episode.
func (a *App) ImportEpisodeSubtitle(id, season, number int, data []byte, name, lang string) error {
	s, v, err := a.episodeVideo(id, season, number)
	if err != nil {
		return err
	}
	sf, err := writeSubtitle(v, data, name, lang, false, false)
	if err != nil {
		return err
	}
	sf.Provider = "manual"
	a.Store.UpdateSeries(id, func(x *model.Series) {
		if e := x.Episode(season, number); e != nil {
			e.Subtitles = withSubtitle(e.Subtitles, sf)
		}
	})
	a.seriesHistory(id, "subtitle", "%s %s: %s subtitles added from a file (%s)", s.Title,
		episodeLabel([]model.EpisodeKey{{Season: season, Episode: number}}), strings.ToUpper(sf.Language), filepath.Base(name))
	return nil
}

// subtitleLang is the first wanted subtitle language.
func (a *App) subtitleLang() string {
	if l := a.Store.Settings().Subtitles.Languages; len(l) > 0 {
		return l[0]
	}
	return ""
}

// MovieSubtitleLinks returns the search pages of the sites added by the
// user, for a film.
func (a *App) MovieSubtitleLinks(id int) []subtitles.Link {
	m, ok := a.Store.Movie(id)
	if !ok {
		return []subtitles.Link{}
	}
	return subtitles.Links(a.Store.Settings().Subtitles.Providers, subtitles.LinkParams{
		Title: m.Title, Original: m.OriginalTitle, Query: fmt.Sprintf("%s %d", m.Title, m.Year), Year: m.Year,
		ImdbID: m.ImdbID, TmdbID: m.TmdbID, Lang: a.subtitleLang()})
}

// EpisodeSubtitleLinks returns the search pages of the sites added by the
// user, for an episode.
func (a *App) EpisodeSubtitleLinks(id, season, number int) []subtitles.Link {
	s, ok := a.Store.Series(id)
	if !ok {
		return []subtitles.Link{}
	}
	return subtitles.Links(a.Store.Settings().Subtitles.Providers, subtitles.LinkParams{
		Title: s.Title, Original: s.OriginalTitle, Query: fmt.Sprintf("%s S%02dE%02d", s.Title, season, number), Year: s.Year,
		ImdbID: s.ImdbID, TmdbID: s.TmdbID, TvdbID: s.TvdbID, Season: season, Episode: number, Lang: a.subtitleLang()})
}

// withSubtitle adds sf to list, replacing a subtitle saved at the same path.
func withSubtitle(list []model.SubtitleFile, sf model.SubtitleFile) []model.SubtitleFile {
	out := make([]model.SubtitleFile, 0, len(list)+1)
	for _, s := range list {
		if s.Path != sf.Path {
			out = append(out, s)
		}
	}
	return append(out, sf)
}

// autoSubtitles downloads the best candidate for each wanted language still
// missing (Bazarr's "wanted" logic, score threshold included).
func (a *App) autoSubtitles(have []model.SubtitleFile, cands []subtitles.Candidate, save func(subtitles.Candidate) error) {
	st := a.Store.Settings().Subtitles
	got := map[string]bool{}
	for _, s := range have {
		if !s.Forced {
			got[s.Language] = true
		}
	}
	for _, lang := range st.Languages {
		lang = strings.ToLower(lang)
		if got[lang] {
			continue
		}
		for _, c := range cands {
			if c.Language != lang || c.Forced || c.Percent < st.MinPercent {
				continue
			}
			if err := save(c); err != nil {
				a.Log.Printf("subtitle download: %v", err)
				continue
			}
			break
		}
	}
}

// SearchSubtitles lists subtitle candidates for a film.
func (a *App) SearchSubtitles(ctx context.Context, id int) ([]subtitles.Candidate, []string, error) {
	v, err := a.movieVideo(id)
	if err != nil {
		return nil, nil, err
	}
	return a.searchSubtitles(ctx, v)
}

// DownloadSubtitle saves a candidate next to the film.
func (a *App) DownloadSubtitle(ctx context.Context, id int, c subtitles.Candidate) error {
	v, err := a.movieVideo(id)
	if err != nil {
		return err
	}
	sf, err := a.saveSubtitle(ctx, c, v)
	if err != nil {
		return err
	}
	a.Store.UpdateMovie(id, func(m *model.Movie) { m.Subtitles = withSubtitle(m.Subtitles, sf) })
	a.history(id, "subtitle", "%s subtitles downloaded (%s, %d %%)", strings.ToUpper(sf.Language), c.Provider, c.Percent)
	return nil
}

// AutoSubtitles fetches the missing subtitles of a film.
func (a *App) AutoSubtitles(ctx context.Context, id int) error {
	m, ok := a.Store.Movie(id)
	if !ok || m.File == nil || len(a.SubtitleProviders()) == 0 {
		return nil
	}
	ctx = backoff.Automatic(ctx) // providers that keep failing are left alone
	cands, _, err := a.SearchSubtitles(ctx, id)
	if err != nil {
		return err
	}
	a.autoSubtitles(m.Subtitles, cands, func(c subtitles.Candidate) error { return a.DownloadSubtitle(ctx, id, c) })
	return nil
}

// SearchEpisodeSubtitles lists subtitle candidates for an episode.
func (a *App) SearchEpisodeSubtitles(ctx context.Context, id, season, number int) ([]subtitles.Candidate, []string, error) {
	_, v, err := a.episodeVideo(id, season, number)
	if err != nil {
		return nil, nil, err
	}
	return a.searchSubtitles(ctx, v)
}

// DownloadEpisodeSubtitle saves a candidate next to the episode.
func (a *App) DownloadEpisodeSubtitle(ctx context.Context, id, season, number int, c subtitles.Candidate) error {
	s, v, err := a.episodeVideo(id, season, number)
	if err != nil {
		return err
	}
	sf, err := a.saveSubtitle(ctx, c, v)
	if err != nil {
		return err
	}
	a.Store.UpdateSeries(id, func(x *model.Series) {
		if e := x.Episode(season, number); e != nil {
			e.Subtitles = withSubtitle(e.Subtitles, sf)
		}
	})
	a.seriesHistory(id, "subtitle", "%s %s: %s subtitles downloaded (%s, %d %%)", s.Title,
		episodeLabel([]model.EpisodeKey{{Season: season, Episode: number}}), strings.ToUpper(sf.Language), c.Provider, c.Percent)
	return nil
}

// AutoEpisodeSubtitles fetches the missing subtitles of an episode.
func (a *App) AutoEpisodeSubtitles(ctx context.Context, id, season, number int) error {
	_, e, err := a.episode(id, season, number)
	if err != nil || e.File == nil || len(a.SubtitleProviders()) == 0 {
		return err
	}
	ctx = backoff.Automatic(ctx)
	cands, _, err := a.SearchEpisodeSubtitles(ctx, id, season, number)
	if err != nil {
		return err
	}
	a.autoSubtitles(e.Subtitles, cands, func(c subtitles.Candidate) error { return a.DownloadEpisodeSubtitle(ctx, id, season, number, c) })
	return nil
}

// ---------------------------------------------------------------------------
// Playback

// SubtitleChoice is the subtitle chosen before the launch: Set is false to
// keep the default (every subtitle file, the player picks by language);
// otherwise Index is a subtitle of the film or episode, or -1 for none.
type SubtitleChoice struct {
	Set   bool
	Index int
}

// ParseSubtitleChoice reads the "sub" parameter: empty for the default,
// "none", or the index of a subtitle.
func ParseSubtitleChoice(v string) (SubtitleChoice, error) {
	switch v {
	case "":
		return SubtitleChoice{}, nil
	case "none":
		return SubtitleChoice{Set: true, Index: -1}, nil
	}
	i, err := strconv.Atoi(v)
	if err != nil || i < 0 {
		return SubtitleChoice{}, i18n.New("subtitle not found")
	}
	return SubtitleChoice{Set: true, Index: i}, nil
}

// Play opens the film (or the file being downloaded) in the chosen player.
func (a *App) Play(id int, whileDownloading bool, choice SubtitleChoice) error {
	m, ok := a.Store.Movie(id)
	if !ok {
		return i18n.New("film not found")
	}
	file, subs := "", []string(nil)
	switch {
	case m.File != nil && !whileDownloading:
		file, subs = m.File.Path, subtitlePaths(m.Subtitles)
	case m.Download != nil:
		content, err := a.liveContent(m.Download.Hash)
		if err != nil {
			return err
		}
		file = largestVideo(content)
	}
	if err := a.launch(file, subs, fmt.Sprintf("%s (%d)", m.Title, m.Year), choice); err != nil {
		return err
	}
	a.history(id, "played", "Playing %s", m.Title)
	return nil
}

// PlayEpisode opens an episode (or the file being downloaded) in the
// player.
func (a *App) PlayEpisode(id, season, number int, whileDownloading bool, choice SubtitleChoice) error {
	s, e, err := a.episode(id, season, number)
	if err != nil {
		return err
	}
	file, subs := "", []string(nil)
	switch {
	case e.File != nil && !whileDownloading:
		file, subs = e.File.Path, subtitlePaths(e.Subtitles)
	case e.DownloadHash != "":
		content, err := a.liveContent(e.DownloadHash)
		if err != nil {
			return err
		}
		file = episodeFile(content, season, number)
	}
	label := episodeLabel([]model.EpisodeKey{{Season: season, Episode: number}})
	title := s.Title + " - " + label
	if e.Title != "" {
		title += " - " + e.Title
	}
	if err := a.launch(file, subs, title, choice); err != nil {
		return err
	}
	a.seriesHistory(id, "played", "Playing %s", s.Title+" "+label)
	return nil
}

func subtitlePaths(subs []model.SubtitleFile) []string {
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		out = append(out, s.Path)
	}
	return out
}

// liveContent returns the local path of a torrent being downloaded.
func (a *App) liveContent(hash string) (string, error) {
	dc := a.Downloader()
	ts, err := dc.Torrents(context.Background(), []string{hash})
	if err != nil || len(ts) == 0 {
		return "", i18n.Errorf("download not found in %s", dc.Name())
	}
	return a.mapPath(ts[0].ContentPath), nil
}

// launch starts the player with the settings (audio and subtitle
// languages, full screen, resume) and the subtitle chosen by the user, if
// any (it only applies to the subtitles of the library: not while
// downloading).
func (a *App) launch(file string, subs []string, title string, choice SubtitleChoice) error {
	if file == "" {
		return i18n.New("no file to play")
	}
	if _, err := os.Stat(file); err != nil {
		return i18n.Errorf("file not found: %s", file)
	}
	st := a.Store.Settings()
	exe, err := player.Find(st.Player.Kind, st.Player.Path, a.Store.Dir())
	if err != nil {
		return err
	}
	o := player.Options{
		Title: title, Subtitles: subs, Fullscreen: st.Player.Fullscreen, AudioLang: st.Player.AudioLang,
		SubLang: strings.Join(st.Subtitles.Languages, ","), Resume: st.Player.Resume, DataDir: a.Store.Dir(), ExtraArgs: st.Player.ExtraArgs,
	}
	if choice.Set {
		if choice.Index >= len(subs) {
			return i18n.New("subtitle not found")
		}
		o.ChooseSubtitle = true
		if choice.Index >= 0 {
			o.Subtitle = subs[choice.Index]
		}
	}
	cmd, err := player.Play(exe, file, o)
	if err != nil {
		return err
	}
	// started by a click of the user: its window comes to the front
	desktop.BringToFront(cmd.Process.Pid)
	return nil
}

// videosIn lists the videos of a file or folder.
func videosIn(p string) []video {
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		return []video{{p, st.Size()}}
	}
	var out []video
	_ = filepath.WalkDir(p, func(f string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !quality.IsMediaExtension(filepath.Ext(f)) {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			out = append(out, video{f, fi.Size()})
		}
		return nil
	})
	return out
}

// largestVideo returns the main video of a download.
func largestVideo(p string) string {
	best := video{}
	for _, v := range videosIn(p) {
		if v.size > best.size {
			best = v
		}
	}
	return best.path
}

// episodeFile finds the video of one episode in a download (the largest
// video when no file is named after the episode).
func episodeFile(p string, season, number int) string {
	for _, v := range videosIn(p) {
		if pe := parser.ParseEpisodeTitle(filepath.Base(v.path)); pe != nil && pe.Covers(season, number) && !pe.FullSeason {
			return v.path
		}
	}
	return largestVideo(p)
}
