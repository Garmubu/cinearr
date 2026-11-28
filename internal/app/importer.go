package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cinearr/internal/decision"
	"cinearr/internal/diag"
	"cinearr/internal/dlclient"
	"cinearr/internal/i18n"
	"cinearr/internal/model"
	"cinearr/internal/parser"
	"cinearr/internal/quality"
)

// Import of finished downloads into the library (Radarr's
// DownloadedMovieImportService and Sonarr's episode import).

// video is a video file found in a download.
type video struct {
	path string
	size int64
}

// locate returns the local path of a finished download, or a message
// explaining why its files cannot be reached.
func (a *App) locate(t dlclient.Torrent) (string, string) {
	client := a.Downloader().Name()
	if t.ContentPath == "" {
		return "", i18n.Tf("%s does not give the location of the files", client)
	}
	content := a.mapPath(t.ContentPath)
	if _, err := os.Stat(content); err != nil {
		return "", i18n.Tf("files not found (%s): check the download folder of %s or the path mappings", content, client)
	}
	return content, ""
}

// scanDownload checks every file of a download and lists its videos,
// samples smaller than sampleMax left out. A dangerous or executable file,
// a file whose extension the user refused, an archive or the absence of
// video refuses the release (problem is set).
func scanDownload(content string, sampleMax int64, rejected []string) ([]video, string) {
	var files []string
	if st, err := os.Stat(content); err == nil && st.IsDir() {
		_ = filepath.WalkDir(content, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				files = append(files, p)
			}
			return nil
		})
	} else {
		files = []string{content}
	}
	var videos []video
	archive := false
	for _, f := range files {
		switch quality.FileRisk(f, rejected) {
		case "dangerous":
			return nil, i18n.Tf("potentially dangerous file: %s", filepath.Base(f))
		case "executable":
			return nil, i18n.Tf("executable file: %s", filepath.Base(f))
		case "rejected":
			return nil, i18n.Tf("file with an extension refused in the settings: %s", filepath.Base(f))
		case "archive":
			archive = true
		}
		base := strings.ToLower(filepath.Base(f))
		if strings.HasPrefix(base, "._") || !quality.IsMediaExtension(filepath.Ext(base)) {
			continue
		}
		fi, err := os.Stat(f)
		if err != nil || (strings.Contains(base, "sample") && fi.Size() < sampleMax) {
			continue
		}
		videos = append(videos, video{f, fi.Size()})
	}
	switch {
	case len(videos) > 0:
		return videos, ""
	case archive:
		return nil, i18n.T("the content is an archive to extract (not supported)")
	}
	return nil, i18n.T("no video file found")
}

// replaceFile carries the subtitles of the previous file of a film or an
// episode over to the new one, then sends the previous file to the
// recycle bin.
func (a *App) replaceFile(old *model.MovieFile, dest string, subs []model.SubtitleFile) []model.SubtitleFile {
	if old == nil || old.Path == dest {
		return subs
	}
	var kept []model.SubtitleFile
	oldBase := strings.TrimSuffix(old.Path, filepath.Ext(old.Path))
	newBase := strings.TrimSuffix(dest, filepath.Ext(dest))
	for _, sub := range subs {
		np := newBase + strings.TrimPrefix(sub.Path, oldBase)
		if err := os.Rename(sub.Path, np); err == nil {
			sub.Path = np
			kept = append(kept, sub)
		}
	}
	a.recycle(old.Path)
	return kept
}

func removeIfEmpty(dir string) {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

// ---------------------------------------------------------------------------
// Films

// importDownload hardlinks (copies / moves) the video into
// "<films folder>/Title (Year)/" after checking the download again.
func (a *App) importDownload(ctx context.Context, id int, t dlclient.Torrent) {
	m, ok := a.Store.Movie(id)
	if !ok || m.Download == nil {
		return
	}
	s := a.Store.Settings()
	fail := func(msg string, block bool) {
		if block {
			_ = a.Downloader().Remove(ctx, t.Hash, true)
			a.failDownload(m, msg, true)
			return
		}
		a.Store.UpdateMovie(id, func(mv *model.Movie) {
			mv.Status = model.StatusFailed
			mv.StatusMessage = i18n.Tf("Import failed: %s", msg)
		})
		a.history(id, "failed", "Import of %s failed: %s", m.Title, msg)
	}
	if s.LibraryPath == "" {
		fail(i18n.T("library folder not configured"), false)
		return
	}
	content, msg := a.locate(t)
	if msg != "" {
		fail(msg, false)
		return
	}
	videos, problem := scanDownload(content, 400<<20, s.RejectedExtensions)
	if problem != "" {
		fail(problem, true)
		return
	}
	main := videos[0]
	for _, v := range videos[1:] {
		if v.size > main.size {
			main = v
		}
	}

	parsed := parser.ParseMovieTitle(m.Download.ReleaseTitle, false)
	q := parser.ParseQuality(m.Download.ReleaseTitle).Quality
	if q.ID == quality.Unknown.ID {
		q = parser.ParseQuality(filepath.Base(main.path)).Quality
	}
	folder := cleanFileName(fmt.Sprintf("%s (%d)", m.Title, m.Year))
	name := folder
	if parsed != nil && parsed.Edition != "" {
		name += " {edition-" + cleanFileName(parsed.Edition) + "}"
	}
	if q.ID != quality.Unknown.ID {
		name += " [" + q.Name + "]"
	}
	dest := filepath.Join(s.LibraryPath, folder, name+strings.ToLower(filepath.Ext(main.path)))
	hash, err := a.place(main.path, dest, m.File, t.Hash)
	if err != nil {
		fail(err.Error(), false)
		return
	}
	if s.ImportMode == "move" {
		_ = a.Downloader().Remove(ctx, t.Hash, false)
	}
	if m.File != nil && m.File.Path != dest {
		a.history(id, "upgraded", "Previous file replaced (%s → %s)", m.File.QualityName, q.Name)
	}
	subs := a.replaceFile(m.File, dest, m.Subtitles)

	langs, group := []string{}, ""
	if parsed != nil {
		langs = decision.EffectiveLanguages(m.Download.ReleaseTitle, parsed.Languages, s.Profile, m.OriginalLanguage)
		group = parsed.ReleaseGroup
	}
	a.Store.UpdateMovie(id, func(mv *model.Movie) {
		mv.File = &model.MovieFile{Path: dest, Size: main.size, QualityID: q.ID, QualityName: q.Name,
			ReleaseTitle: mv.Download.ReleaseTitle, ReleaseGroup: group, Languages: langs, Imported: time.Now(), TorrentHash: hash}
		mv.Download = nil
		mv.Status = model.StatusAvailable
		mv.StatusMessage = ""
		mv.Subtitles = subs
	})
	a.history(id, "imported", "%s imported into the library (%s)", m.Title, q.Name)
	if s.Subtitles.Auto {
		diag.Go("automatic subtitles", func() {
			c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := a.AutoSubtitles(c, id); err != nil {
				a.Log.Printf("automatic subtitles (%d): %v", id, err)
			}
		})
	}
}

// place transfers src to dest with the import mode of the settings and
// returns the hash of the torrent still seeding ("" when the file was
// moved: the torrent is then removed from the client).
func (a *App) place(src, dest string, current *model.MovieFile, hash string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if current != nil && current.Path == dest {
		a.recycle(dest) // the new release has the same name as the old one
	}
	s := a.Store.Settings()
	mode := s.ImportMode
	if err := transfer(src, dest, mode, int64(s.MinFreeSpaceMB)<<20); err != nil {
		return "", err
	}
	if mode == "move" {
		return "", nil
	}
	return hash, nil
}

// transfer hardlinks (falling back to copy), copies or moves a file. A
// copy must leave minFree bytes free on the destination disk.
func transfer(src, dst, mode string, minFree int64) error {
	switch mode {
	case "move":
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
		if err := copyFile(src, dst, minFree); err != nil {
			return err
		}
		return os.Remove(src)
	case "copy":
		return copyFile(src, dst, minFree)
	default: // a hard link keeps seeding without using twice the space
		if err := os.Link(src, dst); err == nil {
			return nil
		}
		return copyFile(src, dst, minFree)
	}
}

// enoughSpace checks, before a copy, that the destination disk keeps
// minFree bytes once the file is written (Radarr's and Sonarr's free
// space check at import). An unknown free space does not block.
func enoughSpace(dst string, size, minFree int64) error {
	free := freeSpace(filepath.Dir(dst))
	if free <= 0 || free-size >= minFree {
		return nil
	}
	return i18n.Errorf("not enough free space in %s: %s needed and %s kept free, %s available",
		filepath.Dir(dst), decision.HumanSize(size), decision.HumanSize(minFree), decision.HumanSize(free))
}

// usableSpace is the free space of a library folder minus the space to
// keep free (0 = unknown, as freeSpace).
func usableSpace(path string, minFreeMB int) int64 {
	free := freeSpace(path)
	if free <= 0 {
		return 0
	}
	return max(free-int64(minFreeMB)<<20, 1)
}

func copyFile(src, dst string, minFree int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if st, err := in.Stat(); err == nil {
		if err := enoughSpace(dst, st.Size(), minFree); err != nil {
			return err
		}
	}
	tmp := dst + ".partial"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// ---------------------------------------------------------------------------
// Series

// leadingNumber reads "05 - Title.mkv" in season packs whose files carry no
// SxxEyy.
var leadingNumber = regexp.MustCompile(`^(?:E|Ep|Episode)?[ ._-]?(\d{1,3})(?:[ ._-]|$)`)

// mapEpisodeFiles assigns the videos of a download to the episodes it was
// grabbed for.
func mapEpisodeFiles(videos []video, d model.SeriesDownload) map[model.EpisodeKey]string {
	out := map[model.EpisodeKey]string{}
	wanted := map[model.EpisodeKey]bool{}
	seasons := map[int]bool{}
	for _, k := range d.Episodes {
		wanted[k] = true
		seasons[k.Season] = true
	}
	for _, v := range videos {
		base := filepath.Base(v.path)
		pe := parser.ParseEpisodeTitle(base)
		if pe == nil || len(pe.Episodes) == 0 {
			// the folder may carry the numbers ("S02E05/file.mkv")
			pe = parser.ParseEpisodeTitle(filepath.Base(filepath.Dir(v.path)))
		}
		if pe != nil && len(pe.Episodes) > 0 {
			for _, e := range pe.Episodes {
				if k := (model.EpisodeKey{Season: pe.Season, Episode: e}); wanted[k] {
					out[k] = v.path
				}
			}
			continue
		}
		if len(seasons) != 1 {
			continue
		}
		if m := leadingNumber.FindStringSubmatch(parser.RemoveFileExtension(base)); m != nil {
			n, _ := strconv.Atoi(m[1])
			for s := range seasons {
				if k := (model.EpisodeKey{Season: s, Episode: n}); wanted[k] {
					out[k] = v.path
				}
			}
		}
	}
	// a single-episode release with one unnamed video
	if rel := parser.ParseEpisodeTitle(d.ReleaseTitle); len(out) == 0 && len(videos) == 1 && len(d.Episodes) == 1 && (rel == nil || !rel.FullSeason) {
		out[d.Episodes[0]] = videos[0].path
	}
	return out
}

// importSeriesDownload hardlinks (copies / moves) the episodes into
// "<series folder>/Title (Year)/Season 02/".
func (a *App) importSeriesDownload(ctx context.Context, id int, hash string, t dlclient.Torrent) {
	s, ok := a.Store.Series(id)
	if !ok || s.DownloadByHash(hash) == nil {
		return
	}
	dl := *s.DownloadByHash(hash)
	st := a.Store.Settings()
	fail := func(msg string, block bool) {
		if block {
			_ = a.Downloader().Remove(ctx, t.Hash, true)
			a.failSeriesDownload(id, dl, msg, true)
			return
		}
		a.Store.UpdateSeries(id, func(x *model.Series) {
			if d := x.DownloadByHash(hash); d != nil {
				d.Importing = false
			}
			for _, k := range dl.Episodes {
				if e := x.Episode(k.Season, k.Episode); e != nil && strings.EqualFold(e.DownloadHash, hash) {
					e.Status = model.StatusFailed
					e.StatusMessage = i18n.Tf("Import failed: %s", msg)
				}
			}
		})
		a.seriesHistory(id, "failed", "Import of %s failed: %s", s.Title+" "+dl.Label, msg)
	}
	if st.SeriesPath == "" {
		fail(i18n.T("series folder not configured (Settings › Library)"), false)
		return
	}
	content, msg := a.locate(t)
	if msg != "" {
		fail(msg, false)
		return
	}
	videos, problem := scanDownload(content, 150<<20, a.Store.Settings().RejectedExtensions)
	if problem != "" {
		fail(problem, true)
		return
	}
	mapped := mapEpisodeFiles(videos, dl)
	if len(mapped) == 0 {
		fail(i18n.T("no file matches the selected episodes"), true)
		return
	}

	relQ := parser.ParseQuality(dl.ReleaseTitle).Quality
	langs, group := []string{}, ""
	if pe := parser.ParseEpisodeTitle(dl.ReleaseTitle); pe != nil {
		langs = decision.EffectiveLanguages(dl.ReleaseTitle, pe.Languages, st.Profile, s.OriginalLanguage)
		group = pe.ReleaseGroup
	}
	seriesDir := filepath.Join(st.SeriesPath, cleanFileName(fmt.Sprintf("%s (%d)", s.Title, s.Year)))

	// a file holding several episodes (S02E01E02) is imported once
	byFile := map[string][]model.EpisodeKey{}
	for k, v := range mapped {
		byFile[v] = append(byFile[v], k)
	}
	paths := make([]string, 0, len(byFile))
	for v := range byFile {
		paths = append(paths, v)
	}
	sort.Strings(paths)
	type done struct {
		file model.MovieFile
		subs []model.SubtitleFile
	}
	results := map[model.EpisodeKey]done{}
	var errs []string
	for _, src := range paths {
		keys := byFile[src]
		sort.Slice(keys, func(i, j int) bool { return keys[i].Episode < keys[j].Episode })
		first := s.Episode(keys[0].Season, keys[0].Episode)
		if first == nil {
			continue
		}
		q := relQ
		if q.ID == quality.Unknown.ID {
			q = parser.ParseQuality(filepath.Base(src)).Quality
		}
		// an existing better file is kept
		if first.File != nil && indexOf(st.Profile.Allowed, first.File.QualityID) > indexOf(st.Profile.Allowed, q.ID) {
			continue
		}
		name := fmt.Sprintf("%s - %s", s.Title, episodeLabel(keys))
		if first.Title != "" && len(keys) == 1 {
			name += " - " + first.Title
		}
		name = cleanFileName(name)
		if q.ID != quality.Unknown.ID {
			name += " [" + q.Name + "]"
		}
		dest := filepath.Join(seriesDir, seasonFolder(keys[0].Season), name+strings.ToLower(filepath.Ext(src)))
		seed, err := a.place(src, dest, first.File, t.Hash)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		var size int64
		if fi, err := os.Stat(dest); err == nil {
			size = fi.Size()
		}
		f := model.MovieFile{Path: dest, Size: size, QualityID: q.ID, QualityName: q.Name, ReleaseTitle: dl.ReleaseTitle,
			ReleaseGroup: group, Languages: langs, Imported: time.Now(), TorrentHash: seed}
		subs := a.replaceFile(first.File, dest, first.Subtitles)
		for _, k := range keys {
			results[k] = done{f, subs}
		}
	}
	if len(results) == 0 {
		msg := i18n.T("no episode could be imported")
		if len(errs) > 0 {
			msg = errs[0]
		}
		fail(msg, false)
		return
	}
	if st.ImportMode == "move" {
		_ = a.Downloader().Remove(ctx, t.Hash, false)
	}
	notFound := i18n.T("Not found in the download")
	a.Store.UpdateSeries(id, func(x *model.Series) {
		x.Downloads = withoutDownload(x.Downloads, hash)
		for _, k := range dl.Episodes {
			e := x.Episode(k.Season, k.Episode)
			if e == nil {
				continue
			}
			if r, ok := results[k]; ok {
				f := r.file
				e.File, e.Subtitles = &f, r.subs
				e.Status, e.StatusMessage, e.DownloadHash = model.StatusAvailable, "", ""
				continue
			}
			if strings.EqualFold(e.DownloadHash, hash) {
				e.DownloadHash = ""
				if e.File != nil {
					e.Status = model.StatusAvailable
				} else {
					e.Status, e.StatusMessage = model.StatusFailed, notFound
				}
			}
		}
	})
	if len(results) == 1 {
		a.seriesHistory(id, "imported", "%s %s imported into the library (%s)", s.Title, dl.Label, relQ.Name)
	} else {
		a.seriesHistory(id, "imported", "%s %s: %d episodes imported (%s)", s.Title, dl.Label, len(results), relQ.Name)
	}
	if st.Subtitles.Auto {
		keys := make([]model.EpisodeKey, 0, len(results))
		for k := range results {
			keys = append(keys, k)
		}
		diag.Go("automatic episode subtitles", func() {
			for _, k := range keys {
				c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				if err := a.AutoEpisodeSubtitles(c, id, k.Season, k.Episode); err != nil {
					a.Log.Printf("automatic subtitles (series %d %v): %v", id, k, err)
				}
				cancel()
			}
		})
	}
}

func seasonFolder(n int) string {
	if n == 0 {
		return "Specials"
	}
	return fmt.Sprintf("Season %02d", n)
}

func indexOf(list []int, v int) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}
