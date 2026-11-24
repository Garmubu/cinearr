package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cinearr/internal/model"
)

// Recycle bin (Radarr's and Sonarr's "Recycling Bin"): the files Cinearr
// deletes from the library (removal with the files, replacement by a
// better release) are moved to a hidden folder at the root of the films
// or series folder, one subfolder per day, and erased after
// RecycleDays days. The move stays on the same disk, so it is instant,
// and the library scan does not read hidden folders.

// RecycleDir is the name of the recycle bin in a library folder.
const RecycleDir = ".cinearr-recycle"

// recycle moves files to the recycle bin of their library folder, or
// deletes them when the bin is disabled (0 days) or out of reach.
func (a *App) recycle(paths ...string) {
	s := a.Store.Settings()
	day := time.Now().Format("2006-01-02")
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		root := ""
		for _, r := range []string{s.LibraryPath, s.SeriesPath} {
			if r != "" && within(p, r) && !samePath(p, r) {
				root = r
			}
		}
		if s.RecycleDays > 0 && root != "" {
			rel, err := filepath.Rel(root, p)
			if err == nil {
				dest := freeName(filepath.Join(root, RecycleDir, day, rel))
				if err = os.MkdirAll(filepath.Dir(dest), 0o755); err == nil {
					if err = os.Rename(p, dest); err == nil {
						continue
					}
				}
			}
			a.Log.Printf("[recycle] %s could not be moved to the recycle bin (%v): deleted", p, err)
		}
		_ = os.Remove(p)
	}
}

// freeName adds " (2)", " (3)"… to a file name already taken.
func freeName(p string) string {
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	ext := filepath.Ext(p)
	base := p[:len(p)-len(ext)]
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Lstat(c); err != nil {
			return c
		}
	}
}

// cleanRecycle erases the days of the recycle bins older than
// RecycleDays (all of them when the bin is disabled).
func (a *App) cleanRecycle() {
	s := a.Store.Settings()
	limit := time.Now().AddDate(0, 0, -s.RecycleDays)
	for _, r := range []string{s.LibraryPath, s.SeriesPath} {
		if r == "" {
			continue
		}
		bin := filepath.Join(r, RecycleDir)
		entries, err := os.ReadDir(bin)
		if err != nil {
			continue
		}
		for _, e := range entries {
			day, err := time.ParseInLocation("2006-01-02", e.Name(), time.Local)
			if err != nil || !e.IsDir() || (s.RecycleDays > 0 && day.AddDate(0, 0, 1).After(limit)) {
				continue
			}
			if err := os.RemoveAll(filepath.Join(bin, e.Name())); err != nil {
				a.Log.Printf("[recycle] %v", err)
			}
		}
		removeIfEmpty(bin)
	}
}

// removeFile sends a video and its subtitles to the recycle bin, and
// deletes its folder once empty.
func (a *App) removeFile(f *model.MovieFile, subs []model.SubtitleFile) {
	if f == nil {
		return
	}
	paths := []string{f.Path}
	for _, s := range subs {
		paths = append(paths, s.Path)
	}
	a.recycle(paths...)
	removeIfEmpty(filepath.Dir(f.Path))
}
