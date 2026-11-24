// Package quality holds the quality catalogue ported from Radarr
// (NzbDrone.Core.Qualities): identifiers, sources, resolutions, weights and
// default size limits.
package quality

import (
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"cinearr/internal/i18n"
)

// Source of a release (QualitySource).
type Source int

const (
	SourceUnknown Source = iota
	SourceCAM
	SourceTelesync
	SourceTelecine
	SourceWorkprint
	SourceDVD
	SourceTV
	SourceWEBDL
	SourceWEBRip
	SourceBluray
)

// Modifier (Modifier enum).
type Modifier int

const (
	ModNone Modifier = iota
	ModRegional
	ModScreener
	ModRawHD
	ModBRDisk
	ModRemux
)

// Quality is one entry of the catalogue.
type Quality struct {
	ID         int      `json:"id"`
	Name       string   `json:"name"`
	Source     Source   `json:"source"`
	Resolution int      `json:"resolution"`
	Modifier   Modifier `json:"modifier"`
}

var (
	Unknown     = Quality{0, "Unknown", SourceUnknown, 0, ModNone}
	WORKPRINT   = Quality{24, "WORKPRINT", SourceWorkprint, 0, ModNone}
	CAM         = Quality{25, "CAM", SourceCAM, 0, ModNone}
	TELESYNC    = Quality{26, "TELESYNC", SourceTelesync, 0, ModNone}
	TELECINE    = Quality{27, "TELECINE", SourceTelecine, 0, ModNone}
	DVDSCR      = Quality{28, "DVDSCR", SourceDVD, 480, ModScreener}
	REGIONAL    = Quality{29, "REGIONAL", SourceDVD, 480, ModRegional}
	SDTV        = Quality{1, "SDTV", SourceTV, 480, ModNone}
	DVD         = Quality{2, "DVD", SourceDVD, 0, ModNone}
	DVDR        = Quality{23, "DVD-R", SourceDVD, 480, ModRemux}
	HDTV720p    = Quality{4, "HDTV-720p", SourceTV, 720, ModNone}
	HDTV1080p   = Quality{9, "HDTV-1080p", SourceTV, 1080, ModNone}
	HDTV2160p   = Quality{16, "HDTV-2160p", SourceTV, 2160, ModNone}
	WEBDL480p   = Quality{8, "WEBDL-480p", SourceWEBDL, 480, ModNone}
	WEBDL720p   = Quality{5, "WEBDL-720p", SourceWEBDL, 720, ModNone}
	WEBDL1080p  = Quality{3, "WEBDL-1080p", SourceWEBDL, 1080, ModNone}
	WEBDL2160p  = Quality{18, "WEBDL-2160p", SourceWEBDL, 2160, ModNone}
	Bluray480p  = Quality{20, "Bluray-480p", SourceBluray, 480, ModNone}
	Bluray576p  = Quality{21, "Bluray-576p", SourceBluray, 576, ModNone}
	Bluray720p  = Quality{6, "Bluray-720p", SourceBluray, 720, ModNone}
	Bluray1080p = Quality{7, "Bluray-1080p", SourceBluray, 1080, ModNone}
	Bluray2160p = Quality{19, "Bluray-2160p", SourceBluray, 2160, ModNone}
	Remux1080p  = Quality{30, "Remux-1080p", SourceBluray, 1080, ModRemux}
	Remux2160p  = Quality{31, "Remux-2160p", SourceBluray, 2160, ModRemux}
	BRDISK      = Quality{22, "BR-DISK", SourceBluray, 1080, ModBRDisk}
	RAWHD       = Quality{10, "Raw-HD", SourceTV, 1080, ModRawHD}
	WEBRip480p  = Quality{12, "WEBRip-480p", SourceWEBRip, 480, ModNone}
	WEBRip720p  = Quality{14, "WEBRip-720p", SourceWEBRip, 720, ModNone}
	WEBRip1080p = Quality{15, "WEBRip-1080p", SourceWEBRip, 1080, ModNone}
	WEBRip2160p = Quality{17, "WEBRip-2160p", SourceWEBRip, 2160, ModNone}
)

// Definition carries the ranking weight and default size limits (MB per
// minute of runtime, as in Radarr). Max 0 means unlimited.
type Definition struct {
	Quality   Quality `json:"quality"`
	Weight    int     `json:"weight"`
	Group     string  `json:"group,omitempty"`
	MinSize   float64 `json:"minSize"`
	MaxSize   float64 `json:"maxSize"`
	Preferred float64 `json:"preferredSize"`
}

// Definitions is the default catalogue, ordered by weight (Radarr defaults).
var Definitions = []Definition{
	{Unknown, 1, "", 0, 100, 95},
	{WORKPRINT, 2, "", 0, 100, 95},
	{CAM, 3, "", 0, 100, 95},
	{TELESYNC, 4, "", 0, 100, 95},
	{TELECINE, 5, "", 0, 100, 95},
	{REGIONAL, 6, "", 0, 100, 95},
	{DVDSCR, 7, "", 0, 100, 95},
	{SDTV, 8, "", 0, 100, 95},
	{DVD, 9, "", 0, 100, 95},
	{DVDR, 10, "", 0, 100, 95},
	{WEBDL480p, 11, "WEB 480p", 0, 100, 95},
	{WEBRip480p, 11, "WEB 480p", 0, 100, 95},
	{Bluray480p, 12, "", 0, 100, 95},
	{Bluray576p, 13, "", 0, 100, 95},
	{HDTV720p, 14, "", 0, 100, 95},
	{WEBDL720p, 15, "WEB 720p", 0, 100, 95},
	{WEBRip720p, 15, "WEB 720p", 0, 100, 95},
	{Bluray720p, 16, "", 0, 100, 95},
	{HDTV1080p, 17, "", 0, 100, 95},
	{WEBDL1080p, 18, "WEB 1080p", 0, 100, 95},
	{WEBRip1080p, 18, "WEB 1080p", 0, 100, 95},
	{Bluray1080p, 19, "", 0, 0, 0},
	{Remux1080p, 20, "", 0, 0, 0},
	{HDTV2160p, 21, "", 0, 0, 0},
	{WEBDL2160p, 22, "WEB 2160p", 0, 0, 0},
	{WEBRip2160p, 22, "WEB 2160p", 0, 0, 0},
	{Bluray2160p, 23, "", 0, 0, 0},
	{Remux2160p, 24, "", 0, 0, 0},
	{BRDISK, 25, "", 0, 0, 0},
	{RAWHD, 26, "", 0, 0, 0},
}

// All lists every quality.
func All() []Quality {
	out := make([]Quality, 0, len(Definitions))
	for _, d := range Definitions {
		out = append(out, d.Quality)
	}
	return out
}

// Weight returns the ranking weight of q.
func Weight(q Quality) int {
	for _, d := range Definitions {
		if d.Quality.ID == q.ID {
			return d.Weight
		}
	}
	return 0
}

// DefinitionFor returns the definition of q.
func DefinitionFor(q Quality) Definition {
	for _, d := range Definitions {
		if d.Quality.ID == q.ID {
			return d
		}
	}
	return Definitions[0]
}

// FindBySourceAndResolution ports QualityFinder.FindBySourceAndResolution.
func FindBySourceAndResolution(source Source, resolution int, mod Modifier) Quality {
	all := All()
	for _, q := range all {
		if q.Source == source && q.Resolution == resolution && q.Modifier == mod {
			return q
		}
	}
	var unknownRes []Quality
	for _, q := range all {
		if q.Source == source && q.Resolution == 0 && q.Modifier == mod && q.ID != Unknown.ID {
			unknownRes = append(unknownRes, q)
		}
	}
	if len(unknownRes) == 1 {
		return unknownRes[0]
	}
	for _, q := range unknownRes {
		if q.Source >= source {
			return q
		}
	}
	var matching []Quality
	for _, q := range all {
		if q.Modifier == mod && q.Resolution == resolution {
			matching = append(matching, q)
		}
	}
	sort.SliceStable(matching, func(i, j int) bool { return matching[i].Source < matching[j].Source })
	for _, q := range matching {
		if q.Source >= source {
			return q
		}
	}
	return Unknown
}

// mediaExtensions is MediaFileExtensions._fileExtensions.
var mediaExtensions = map[string]Quality{
	".webm": Unknown, ".m4v": SDTV, ".3gp": SDTV, ".nsv": SDTV, ".ty": SDTV, ".strm": SDTV,
	".rm": SDTV, ".rmvb": SDTV, ".m3u": SDTV, ".ifo": SDTV, ".mov": SDTV, ".qt": SDTV,
	".divx": SDTV, ".xvid": SDTV, ".bivx": SDTV, ".nrg": SDTV, ".pva": SDTV, ".wmv": SDTV,
	".asf": SDTV, ".asx": SDTV, ".ogm": SDTV, ".ogv": SDTV, ".m2v": SDTV, ".avi": SDTV,
	".bin": SDTV, ".dat": SDTV, ".dvr-ms": SDTV, ".mpg": SDTV, ".mpeg": SDTV, ".mp4": SDTV,
	".avc": SDTV, ".vp3": SDTV, ".svq3": SDTV, ".nuv": SDTV, ".viv": SDTV, ".dv": SDTV,
	".fli": SDTV, ".flv": SDTV, ".wpl": SDTV, ".img": DVD, ".iso": DVD, ".vob": DVD,
	".mkv": WEBDL720p, ".mk3d": WEBDL720p, ".ts": SDTV, ".wtv": SDTV, ".m2ts": Bluray720p,
}

// IsMediaExtension reports whether ext (with dot) is a known video extension.
func IsMediaExtension(ext string) bool {
	_, ok := mediaExtensions[strings.ToLower(ext)]
	return ok
}

// ForExtension returns the quality implied by a file extension.
func ForExtension(ext string) Quality {
	if q, ok := mediaExtensions[strings.ToLower(ext)]; ok {
		return q
	}
	return Unknown
}

// File-type safety lists from Radarr's FileExtensions.
var (
	DangerousExtensions  = set(".arj", ".lnk", ".lzh", ".ps1", ".scr", ".vbs", ".zipx")
	ExecutableExtensions = set(".bat", ".cmd", ".exe", ".sh")
	ArchiveExtensions    = set(".7z", ".bz2", ".gz", ".r00", ".rar", ".tar.bz2", ".tar.gz", ".tar", ".tb2", ".tbz2", ".tgz", ".zip")
)

// Extra extensions treated as unsafe in addition to Radarr's lists: they are
// never part of a film release and are commonly used to deliver malware.
var ExtraUnsafeExtensions = set(".com", ".pif", ".msi", ".js", ".jse", ".wsf", ".hta", ".cpl", ".jar", ".dll", ".apk", ".dmg", ".app", ".ps2", ".reg")

func set(v ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		m[s] = true
	}
	return m
}

// FileRisk classifies a file name: "dangerous", "executable", "archive" or "".
func FileRisk(name string, rejected []string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case DangerousExtensions[ext] || ExtraUnsafeExtensions[ext]:
		return "dangerous"
	case ExecutableExtensions[ext]:
		return "executable"
	case slices.Contains(rejected, ext):
		return "rejected"
	case ArchiveExtensions[ext]:
		return "archive"
	}
	return ""
}

var extensionSyntax = regexp.MustCompile(`^\.[a-z0-9][a-z0-9_-]{0,15}$`)

// NormalizeExtensions cleans a list of extensions typed by the user
// ("ISO", "*.img", ".wmv") into ".iso", ".img", ".wmv".
func NormalizeExtensions(list []string) ([]string, error) {
	out := []string{}
	for _, e := range list {
		e = strings.ToLower(strings.TrimSpace(e))
		e = "." + strings.TrimLeft(e, "*.")
		if e == "." {
			continue
		}
		if !extensionSyntax.MatchString(e) {
			return nil, i18n.Errorf("invalid file extension: %s", e)
		}
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out, nil
}
