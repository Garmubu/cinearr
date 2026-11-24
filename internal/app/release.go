package app

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"cinearr/internal/decision"
	"cinearr/internal/dlclient"
	"cinearr/internal/i18n"
	"cinearr/internal/indexer"
	"cinearr/internal/indexer/cardigann"
	"cinearr/internal/model"
	"cinearr/internal/parser"
	"cinearr/internal/torrent"
)

// Releases: fetching the .torrent or magnet, safety checks before the
// download, and the torrent client.

// testSearch is the search run by the "Test" button of an indexer.
func testSearch(ctx context.Context, s indexer.Searcher) ([]model.Release, error) {
	rels, err := s.Search(ctx, cardigann.Query{Categories: []int{2000}})
	if err == nil && len(rels) == 0 {
		// some sites return nothing without keywords
		rels, err = s.Search(ctx, cardigann.Query{Keywords: "2023", Categories: []int{2000}})
	}
	return rels, err
}

func (a *App) searcherFor(ctx context.Context, rel model.Release) (indexer.Searcher, error) {
	for _, c := range a.Store.Settings().Indexers {
		if c.ID == rel.IndexerID {
			return a.Indexer.Get(ctx, c)
		}
	}
	return nil, i18n.Errorf("indexer %q not found (deleted?)", rel.Indexer)
}

// send fetches the .torrent (or magnet) of a release, inspects its file
// list and adds it to the torrent client. blocked tells whether a hash is
// on the blocklist; refuse records a release refused for its content.
func (a *App) send(ctx context.Context, rel model.Release, tag string, blocked func(string) bool, refuse func(hash, problems string),
	warn func(format string, args ...any)) (*model.Download, error) {
	dc := a.Downloader()
	var res *cardigann.DownloadResult
	var dlErr error
	if rel.DownloadURL != "" {
		s, err := a.searcherFor(ctx, rel)
		if err != nil {
			return nil, err
		}
		res, dlErr = s.Download(ctx, rel.DownloadURL)
	}
	if (res == nil || dlErr != nil) && rel.MagnetURL != "" {
		res, dlErr = &cardigann.DownloadResult{Magnet: rel.MagnetURL}, nil
	}
	if (res == nil || dlErr != nil) && rel.InfoHash != "" && !rel.Private {
		res, dlErr = &cardigann.DownloadResult{Magnet: torrent.BuildMagnet(rel.InfoHash, rel.Title)}, nil
	}
	if dlErr != nil {
		return nil, dlErr
	}
	if res == nil {
		return nil, i18n.New("no usable link for this release")
	}

	q := parser.ParseQuality(rel.Title).Quality
	dl := &model.Download{ReleaseTitle: rel.Title, Indexer: rel.Indexer, QualityID: q.ID, QualityName: q.Name,
		Size: rel.Size, Added: time.Now(), State: "queued"}

	if len(res.Torrent) > 0 {
		info, err := torrent.Parse(res.Torrent)
		if err != nil {
			return nil, i18n.Errorf("invalid .torrent file: %w", err)
		}
		if blocked(info.InfoHash) {
			return nil, i18n.New("release already on the blocklist")
		}
		rep := torrent.Inspect(info.Files, a.Store.Settings().RejectedExtensions)
		if !rep.OK {
			problems := strings.Join(rep.Problems, " ; ")
			refuse(info.InfoHash, problems)
			return nil, i18n.Errorf("dangerous or suspicious content: %s", problems)
		}
		if rel.Size > 0 && info.Size > 0 && absDiff(rel.Size, info.Size) > rel.Size/5 {
			warn("Announced size (%s) differs from the real content (%s) for %s",
				decision.HumanSize(rel.Size), decision.HumanSize(info.Size), rel.Title)
		}
		dl.Hash = info.InfoHash
		dl.Size = info.Size
		dl.Checked = true
		name := cleanFileName(rel.Title) + ".torrent"
		if err := dc.Add(ctx, dlclient.AddRequest{File: res.Torrent, FileName: name, Hash: info.InfoHash, Tag: tag}); err != nil {
			return nil, err
		}
	} else {
		mg, err := torrent.ParseMagnet(res.Magnet)
		if err != nil {
			return nil, err
		}
		if blocked(mg.InfoHash) {
			return nil, i18n.New("release already on the blocklist")
		}
		dl.Hash = mg.InfoHash
		if err := dc.Add(ctx, dlclient.AddRequest{Magnet: res.Magnet, Hash: mg.InfoHash, Tag: tag}); err != nil {
			return nil, err
		}
	}
	return dl, nil
}

func absDiff(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}

// cleanFileName removes characters that are invalid on Windows, macOS or
// Linux file systems.
func cleanFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 32:
			return -1
		case strings.ContainsRune(`<>:"/\|?*`, r):
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, ". ")
	if s == "" {
		s = "video"
	}
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}

// mapPath converts a path reported by the torrent client to a local path.
func (a *App) mapPath(p string) string {
	for _, m := range a.Store.Settings().PathMappings {
		if m.Remote != "" && strings.HasPrefix(p, m.Remote) {
			rest := strings.TrimPrefix(p, m.Remote)
			rest = strings.TrimLeft(strings.ReplaceAll(rest, "\\", "/"), "/")
			return filepath.Join(m.Local, filepath.FromSlash(rest))
		}
	}
	return p
}
