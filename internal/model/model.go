// Package model holds the types shared by the services.
package model

import (
	"strings"
	"time"
)

// Release is a search result returned by an indexer (ReleaseInfo/TorrentInfo).
type Release struct {
	GUID                 string    `json:"guid"`
	Title                string    `json:"title"`
	Indexer              string    `json:"indexer"`
	IndexerID            string    `json:"indexerId"`
	DownloadURL          string    `json:"downloadUrl,omitempty"`
	MagnetURL            string    `json:"magnetUrl,omitempty"`
	InfoHash             string    `json:"infoHash,omitempty"`
	InfoURL              string    `json:"infoUrl,omitempty"`
	Size                 int64     `json:"size"`
	Seeders              int       `json:"seeders"`
	Peers                int       `json:"peers"`
	Grabs                int       `json:"grabs"`
	Files                int       `json:"files,omitempty"`
	PublishDate          time.Time `json:"publishDate"`
	Categories           []int     `json:"categories,omitempty"`
	ImdbID               string    `json:"imdbId,omitempty"`
	TmdbID               int       `json:"tmdbId,omitempty"`
	DownloadVolumeFactor float64   `json:"downloadVolumeFactor"`
	UploadVolumeFactor   float64   `json:"uploadVolumeFactor"`
	MinimumRatio         float64   `json:"minimumRatio,omitempty"`
	MinimumSeedTime      int64     `json:"minimumSeedTime,omitempty"`
	Private              bool      `json:"private"`
	IndexerPriority      int       `json:"indexerPriority"`
}

// MovieStatus values.
const (
	StatusWanted      = "wanted"
	StatusDownloading = "downloading"
	StatusImporting   = "importing"
	StatusAvailable   = "available"
	StatusFailed      = "failed"
)

// Movie is a film added to the library.
type Movie struct {
	TmdbID            int            `json:"tmdbId"`
	ImdbID            string         `json:"imdbId,omitempty"`
	Title             string         `json:"title"`
	OriginalTitle     string         `json:"originalTitle,omitempty"`
	EnglishTitle      string         `json:"englishTitle,omitempty"`
	OriginalLanguage  string         `json:"originalLanguage,omitempty"`
	AlternativeTitles []string       `json:"alternativeTitles,omitempty"`
	Year              int            `json:"year"`
	Runtime           int            `json:"runtime"`
	Overview          string         `json:"overview,omitempty"`
	PosterPath        string         `json:"posterPath,omitempty"`
	BackdropPath      string         `json:"backdropPath,omitempty"`
	Genres            []string       `json:"genres,omitempty"`
	Rating            float64        `json:"rating,omitempty"`
	Status            string         `json:"status"`
	StatusMessage     string         `json:"statusMessage,omitempty"`
	Added             time.Time      `json:"added"`
	File              *MovieFile     `json:"file,omitempty"`
	Download          *Download      `json:"download,omitempty"`
	Subtitles         []SubtitleFile `json:"subtitles,omitempty"`
}

// MovieFile is the imported video file.
type MovieFile struct {
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	QualityID    int       `json:"qualityId"`
	QualityName  string    `json:"qualityName"`
	ReleaseTitle string    `json:"releaseTitle"`
	ReleaseGroup string    `json:"releaseGroup,omitempty"`
	Languages    []string  `json:"languages,omitempty"`
	Imported     time.Time `json:"imported"`
	TorrentHash  string    `json:"torrentHash,omitempty"` // still seeding in qBittorrent
}

// Download tracks a release sent to the torrent client.
type Download struct {
	Hash         string    `json:"hash"`
	ReleaseTitle string    `json:"releaseTitle"`
	Indexer      string    `json:"indexer"`
	QualityID    int       `json:"qualityId"`
	QualityName  string    `json:"qualityName"`
	Size         int64     `json:"size"`
	Added        time.Time `json:"added"`
	Progress     float64   `json:"progress"`
	State        string    `json:"state"`
	ETA          int64     `json:"eta"`
	DlSpeed      int64     `json:"dlSpeed"`
	Seeds        int       `json:"seeds"`
	Checked      bool      `json:"checked"` // file list verified for dangerous content
}

// SubtitleFile is a subtitle saved next to the movie file.
type SubtitleFile struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Provider string `json:"provider"`
	Score    int    `json:"score"`
	HI       bool   `json:"hi,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
}

// BlocklistEntry prevents a release from being grabbed again.
type BlocklistEntry struct {
	TmdbID   int       `json:"tmdbId"`
	SeriesID int       `json:"seriesId,omitempty"` // TMDB id of the series (TmdbID is then 0)
	Title    string    `json:"title"`
	Hash     string    `json:"hash,omitempty"`
	Indexer  string    `json:"indexer,omitempty"`
	Reason   string    `json:"reason"`
	Date     time.Time `json:"date"`
}

// HistoryEvent is an entry of the activity log.
type HistoryEvent struct {
	Date     time.Time `json:"date"`
	TmdbID   int       `json:"tmdbId,omitempty"`
	SeriesID int       `json:"seriesId,omitempty"`
	Kind     string    `json:"kind"` // grabbed, imported, failed, subtitle, deleted, warning
	Message  string    `json:"message"`
}

// Series is a TV series added to the library (Sonarr's Series).
type Series struct {
	TmdbID            int              `json:"tmdbId"`
	TvdbID            int              `json:"tvdbId,omitempty"`
	ImdbID            string           `json:"imdbId,omitempty"`
	Title             string           `json:"title"`
	OriginalTitle     string           `json:"originalTitle,omitempty"`
	EnglishTitle      string           `json:"englishTitle,omitempty"`
	OriginalLanguage  string           `json:"originalLanguage,omitempty"`
	AlternativeTitles []string         `json:"alternativeTitles,omitempty"`
	Year              int              `json:"year"`
	Runtime           int              `json:"runtime"` // typical episode length (minutes)
	Overview          string           `json:"overview,omitempty"`
	PosterPath        string           `json:"posterPath,omitempty"`
	BackdropPath      string           `json:"backdropPath,omitempty"`
	Genres            []string         `json:"genres,omitempty"`
	Rating            float64          `json:"rating,omitempty"`
	Network           string           `json:"network,omitempty"`
	State             string           `json:"state,omitempty"` // TMDB status: Returning Series, Ended…
	Added             time.Time        `json:"added"`
	Refreshed         time.Time        `json:"refreshed"`
	Seasons           []Season         `json:"seasons"`
	Downloads         []SeriesDownload `json:"downloads"`
}

// Season of a series.
type Season struct {
	Number     int       `json:"number"`
	Name       string    `json:"name,omitempty"`
	Overview   string    `json:"overview,omitempty"`
	PosterPath string    `json:"posterPath,omitempty"`
	AirDate    string    `json:"airDate,omitempty"`
	Episodes   []Episode `json:"episodes"`
}

// Episode of a season. Status is "" while nothing was asked for it, then
// wanted, downloading, importing, available or failed.
type Episode struct {
	Season        int            `json:"season"`
	Number        int            `json:"number"`
	Title         string         `json:"title"`
	Overview      string         `json:"overview,omitempty"`
	AirDate       string         `json:"airDate,omitempty"`
	Runtime       int            `json:"runtime,omitempty"`
	Status        string         `json:"status,omitempty"`
	StatusMessage string         `json:"statusMessage,omitempty"`
	DownloadHash  string         `json:"downloadHash,omitempty"`
	File          *MovieFile     `json:"file,omitempty"`
	Subtitles     []SubtitleFile `json:"subtitles,omitempty"`
}

// Aired reports whether the episode has been broadcast (air date today or
// earlier).
func (e Episode) Aired(now time.Time) bool {
	if len(e.AirDate) < 10 {
		return false
	}
	d, err := time.Parse("2006-01-02", e.AirDate[:10])
	return err == nil && !d.After(now)
}

// EpisodeKey identifies an episode.
type EpisodeKey struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

// SeriesDownload is a release sent to the torrent client for one or more
// episodes (a season pack covers a whole season).
type SeriesDownload struct {
	Download
	Episodes  []EpisodeKey `json:"episodes"`
	Label     string       `json:"label"` // S02E01, S02E01-E03, S02…
	Importing bool         `json:"importing,omitempty"`
}

// Episode returns a pointer to an episode of the series, or nil.
func (s *Series) Episode(season, number int) *Episode {
	for i := range s.Seasons {
		if s.Seasons[i].Number != season {
			continue
		}
		for j := range s.Seasons[i].Episodes {
			if s.Seasons[i].Episodes[j].Number == number {
				return &s.Seasons[i].Episodes[j]
			}
		}
	}
	return nil
}

// SeasonByNumber returns a pointer to a season, or nil.
func (s *Series) SeasonByNumber(n int) *Season {
	for i := range s.Seasons {
		if s.Seasons[i].Number == n {
			return &s.Seasons[i]
		}
	}
	return nil
}

// DownloadByHash returns a pointer to a download of the series, or nil.
func (s *Series) DownloadByHash(hash string) *SeriesDownload {
	for i := range s.Downloads {
		if strings.EqualFold(s.Downloads[i].Hash, hash) {
			return &s.Downloads[i]
		}
	}
	return nil
}
