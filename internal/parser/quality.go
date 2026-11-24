package parser

import (
	"path/filepath"
	"strconv"
	"strings"

	"cinearr/internal/quality"
	"cinearr/internal/rx"
)

// Port of NzbDrone.Core.Parser.QualityParser (Radarr). The regular
// expressions are kept verbatim.

// Revision mirrors Radarr's Revision.
type Revision struct {
	Version  int  `json:"version"`
	Real     int  `json:"real"`
	IsRepack bool `json:"isRepack"`
}

// QualityModel is the parsed quality plus its revision.
type QualityModel struct {
	Quality  quality.Quality `json:"quality"`
	Revision Revision        `json:"revision"`
}

const ic = rx.IgnoreCase
const icx = rx.IgnoreCase | rx.IgnorePatternWhitespace

var (
	sourceRegex = rx.MustCompile(`\b(?:
		(?<bluray>M?Blu[-_. ]?Ray|HD[-_. ]?DVD|BD(?!$)|UHD2?BD|BDISO|BDMux|BD25|BD50|BR[-_. ]?DISK)|
		(?<webdl>WEB[-_. ]?DL(?:mux)?|AmazonHD|AmazonSD|iTunesHD|MaxdomeHD|NetflixU?HD|WebHD|HBOMaxHD|DisneyHD|[. ]WEB[. ](?:[xh][ .]?26[45]|AVC|HEVC|DDP?5[. ]1)|[. ](?-i:WEB)$|(?:\d{3,4}0p)[-. ](?:Hybrid[-_. ]?)?WEB[-. ]|[-. ]WEB[-. ]\d{3,4}0p|\b\s\/\sWEB\s\/\s\b|(?:AMZN|NF|DP)[. -]WEB[. -](?!Rip))|
		(?<webrip>WebRip|Web-Rip|WEBMux)|
		(?<hdtv>HDTV)|
		(?<bdrip>BDRip|BDLight|HD[-_. ]?DVDRip|UHDBDRip)|
		(?<brrip>BRRip)|
		(?<dvdr>\d?x?M?DVD-?[R59])|
		(?<dvd>DVD(?!-R)|DVDRip|xvidvd)|
		(?<dsr>WS[-_. ]DSR|DSR)|
		(?<regional>R[0-9]{1}|REGIONAL)|
		(?<scr>SCR|SCREENER|DVDSCR|DVDSCREENER)|
		(?<ts>TS[-_. ]|TELESYNCH?|HD-TS|HDTS|PDVD|TSRip|HDTSRip)|
		(?<tc>TC|TELECINE|HD-TC|HDTC)|
		(?<cam>CAMRIP|(?:NEW)?CAM|HD-?CAM(?:Rip)?|HQCAM)|
		(?<wp>WORKPRINT|WP)|
		(?<pdtv>PDTV)|
		(?<sdtv>SDTV)|
		(?<tvrip>TVRip)
		)(?:\b|$|[ .])`, icx)

	rawHDRegex   = rx.MustCompile(`\b(?<rawhd>RawHD|Raw[-_. ]HD)\b`, ic)
	mpeg2Regex   = rx.MustCompile(`\b(?<mpeg2>MPEG[-_. ]?2)\b`, 0)
	brDiskRegex  = rx.MustCompile(`^(?!.*\b((?<!HD[._ -]|HD)DVD|BDRip|720p|MKV|XviD|WMV|d3g|(BD)?REMUX|^(?=.*1080p)(?=.*HEVC)|[xh][-_. ]?26[45]|German.*[DM]L|((?<=\d{4}).*German.*([DM]L)?)(?=.*\b(AVC|HEVC|VC[-_. ]?1|MVC|MPEG[-_. ]?2)\b))\b)(((?=.*\b(Blu[-_. ]?ray|BD|HD[-_. ]?DVD)\b)(?=.*\b(AVC|HEVC|VC[-_. ]?1|MVC|MPEG[-_. ]?2|BDMV|ISO)\b))|^((?=.*\b(((?=.*\b((.*_)?COMPLETE.*|Dis[ck])\b)(?=.*(Blu[-_. ]?ray|HD[-_. ]?DVD)))|3D[-_. ]?BD|BR[-_. ]?DISK|Full[-_. ]?Blu[-_. ]?ray|^((?=.*((BD|UHD)[-_. ]?(25|50|66|100|ISO)))))))).*`, ic)
	properRegex  = rx.MustCompile(`\b(?<proper>proper)\b`, ic)
	repackRegex  = rx.MustCompile(`\b(?<repack>repack\d?|rerip\d?)\b`, ic)
	versionRegex = rx.MustCompile(`\d[-._ ]?v(?<version>\d)[-._ ]|\[v(?<version>\d)\]|repack(?<version>\d)|rerip(?<version>\d)`, ic)
	realRegex    = rx.MustCompile(`\b(?<real>REAL)\b`, 0)

	resolutionRegex            = rx.MustCompile(`\b(?:(?<R360p>360p)|(?<R480p>480p|480i|640x480|848x480)|(?<R540p>540p)|(?<R576p>576p)|(?<R720p>720p|1280x720|960p)|(?<R1080p>1080p|1920x1080|1440p|FHD|1080i|4kto1080p)|(?<R2160p>2160p|3840x2160|4k[-_. ](?:UHD|HEVC|BD|H\.?265)|(?:UHD|HEVC|BD|H\.?265)[-_. ]4k))\b`, ic)
	alternativeResolutionRegex = rx.MustCompile(`\b(?<R2160p>UHD)\b|(?<R2160p>\[4K\])`, ic)
	codecRegex                 = rx.MustCompile(`\b(?:(?<x264>x264)|(?<h264>h264)|(?<xvidhd>XvidHD)|(?<xvid>X-?vid)|(?<divx>divx))\b`, ic)
	otherSourceRegex           = rx.MustCompile(`(?<hdtv>HD[-_. ]TV)|(?<sdtv>SD[-_. ]TV)`, ic)
	animeBlurayRegex           = rx.MustCompile(`bd(?:720|1080|2160)|(?<=[-_. (\[])bd(?=[-_. )\]])`, ic)
	animeWebDlRegex            = rx.MustCompile(`\[WEB\]|[\[\(]WEB[ .]`, ic)
	highDefPdtvRegex           = rx.MustCompile(`hr[-_. ]ws`, ic)
	remuxRegex                 = rx.MustCompile(`(?:[_. \[]|\d{4}p-|\bHybrid-)(?<remux>(?:(BD|UHD)[-_. ]?)?Remux)\b|(?<remux>(?:(BD|UHD)[-_. ]?)?Remux[_. ]\d{4}p)`, ic)
	germanRemuxRegex           = rx.MustCompile(`((?<=\d{4}).*German.*([DM]L)?)(?=.*\b(AVC|HEVC|VC[_. -]?1|MVC|MPEG[_. -]?2))(?=.*Blu-?ray)`, ic)
)

// Resolution values (Resolution enum).
const (
	resUnknown = 0
	r360p      = 360
	r480p      = 480
	r540p      = 540
	r576p      = 576
	r720p      = 720
	r1080p     = 1080
	r2160p     = 2160
)

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// ParseQuality ports QualityParser.ParseQuality.
func ParseQuality(name string) QualityModel {
	if strings.TrimSpace(name) == "" {
		return QualityModel{Quality: quality.Unknown}
	}
	name = strings.TrimSpace(name)
	result := ParseQualityName(name)
	if result.Quality.ID == quality.Unknown.ID && !strings.ContainsAny(name, "<>|\"\x00") {
		result.Quality = quality.ForExtension(filepath.Ext(name))
	}
	return result
}

// ParseQualityName ports QualityParser.ParseQualityName.
func ParseQualityName(name string) QualityModel {
	normalized := strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	result := parseQualityModifiers(name, normalized)

	sourceMatch := sourceRegex.LastMatch(normalized)
	resolution := parseResolution(normalized)
	codec := codecRegex.FindStringMatch(normalized)
	codecHas := func(g string) bool { return codec != nil && codec.Success(g) }
	remuxMatch := remuxRegex.MatchString(normalized) || germanRemuxRegex.MatchString(normalized)
	brDiskMatch := brDiskRegex.MatchString(normalized)

	set := func(q quality.Quality) QualityModel { result.Quality = q; return result }

	if rawHDRegex.MatchString(normalized) && !brDiskMatch {
		return set(quality.RAWHD)
	}

	if sourceMatch != nil {
		g := sourceMatch.Success
		switch {
		case g("bluray"):
			if brDiskMatch {
				return set(quality.BRDISK)
			}
			if codecHas("xvid") || codecHas("divx") {
				return set(quality.Bluray480p)
			}
			switch resolution {
			case r2160p:
				if remuxMatch {
					return set(quality.Remux2160p)
				}
				return set(quality.Bluray2160p)
			case r1080p:
				if remuxMatch {
					return set(quality.Remux1080p)
				}
				return set(quality.Bluray1080p)
			case r720p:
				return set(quality.Bluray720p)
			case r576p:
				return set(quality.Bluray576p)
			case r360p, r480p, r540p:
				return set(quality.Bluray480p)
			}
			if remuxMatch && resolution != r720p {
				return set(quality.Remux1080p)
			}
			return set(quality.Bluray720p)
		case g("webdl"):
			switch resolution {
			case r2160p:
				return set(quality.WEBDL2160p)
			case r1080p:
				return set(quality.WEBDL1080p)
			case r720p:
				return set(quality.WEBDL720p)
			}
			if strings.Contains(name, "[WEBDL]") {
				return set(quality.WEBDL720p)
			}
			return set(quality.WEBDL480p)
		case g("webrip"):
			switch resolution {
			case r2160p:
				return set(quality.WEBRip2160p)
			case r1080p:
				return set(quality.WEBRip1080p)
			case r720p:
				return set(quality.WEBRip720p)
			}
			return set(quality.WEBRip480p)
		case g("scr"):
			return set(quality.DVDSCR)
		case g("cam"):
			return set(quality.CAM)
		case g("ts"):
			q := quality.TELESYNC
			q.Resolution = resolution
			return set(q)
		case g("tc"):
			return set(quality.TELECINE)
		case g("wp"):
			return set(quality.WORKPRINT)
		case g("regional"):
			return set(quality.REGIONAL)
		case g("hdtv"):
			if mpeg2Regex.MatchString(normalized) {
				return set(quality.RAWHD)
			}
			switch resolution {
			case r2160p:
				return set(quality.HDTV2160p)
			case r1080p:
				return set(quality.HDTV1080p)
			case r720p:
				return set(quality.HDTV720p)
			}
			if strings.Contains(name, "[HDTV]") {
				return set(quality.HDTV720p)
			}
			return set(quality.SDTV)
		case g("bdrip") || g("brrip"):
			switch resolution {
			case r720p:
				return set(quality.Bluray720p)
			case r1080p:
				return set(quality.Bluray1080p)
			case r2160p:
				return set(quality.Bluray2160p)
			case r576p:
				return set(quality.Bluray576p)
			}
			return set(quality.Bluray480p)
		case g("dvdr"):
			return set(quality.DVDR)
		case g("dvd"):
			return set(quality.DVD)
		case g("pdtv") || g("sdtv") || g("dsr") || g("tvrip"):
			if resolution == r1080p || containsFold(normalized, "1080p") {
				return set(quality.HDTV1080p)
			}
			if resolution == r720p || containsFold(normalized, "720p") {
				return set(quality.HDTV720p)
			}
			if highDefPdtvRegex.MatchString(normalized) {
				return set(quality.HDTV720p)
			}
			return set(quality.SDTV)
		}
	}

	if sourceMatch == nil && remuxMatch && resolution != resUnknown {
		switch resolution {
		case r480p:
			return set(quality.Bluray480p)
		case r720p:
			return set(quality.Bluray720p)
		case r2160p:
			return set(quality.Remux2160p)
		case r1080p:
			return set(quality.Remux1080p)
		}
	}

	if animeBlurayRegex.MatchString(normalized) {
		if resolution == r360p || resolution == r480p || resolution == r540p || resolution == r576p || containsFold(normalized, "480p") {
			return set(quality.DVD)
		}
		if resolution == r1080p || containsFold(normalized, "1080p") {
			if remuxMatch {
				return set(quality.Remux1080p)
			}
			return set(quality.Bluray1080p)
		}
		if resolution == r2160p || containsFold(normalized, "2160p") {
			if remuxMatch {
				return set(quality.Remux2160p)
			}
			return set(quality.Bluray2160p)
		}
		if remuxMatch && resolution != r720p {
			return set(quality.Remux1080p)
		}
		return set(quality.Bluray720p)
	}

	if animeWebDlRegex.MatchString(normalized) {
		if resolution == r360p || resolution == r480p || resolution == r540p || resolution == r576p || containsFold(normalized, "480p") {
			return set(quality.WEBDL480p)
		}
		if resolution == r1080p || containsFold(normalized, "1080p") {
			return set(quality.WEBDL1080p)
		}
		if resolution == r2160p || containsFold(normalized, "2160p") {
			return set(quality.WEBDL2160p)
		}
		return set(quality.WEBDL720p)
	}

	if resolution != resUnknown {
		source := quality.SourceUnknown
		mod := quality.ModNone
		if remuxMatch {
			source = quality.SourceBluray
			mod = quality.ModRemux
		} else if q := quality.ForExtension(filepath.Ext(name)); q.ID != quality.Unknown.ID {
			source = q.Source
		}
		pick := func(fallback quality.Quality, res int) QualityModel {
			if source == quality.SourceUnknown {
				return set(fallback)
			}
			return set(quality.FindBySourceAndResolution(source, res, mod))
		}
		switch resolution {
		case r2160p:
			return pick(quality.HDTV2160p, 2160)
		case r1080p:
			return pick(quality.HDTV1080p, 1080)
		case r720p:
			return pick(quality.HDTV720p, 720)
		case r360p, r480p, r540p, r576p:
			return pick(quality.SDTV, 480)
		}
	}

	if codecHas("x264") {
		return set(quality.SDTV)
	}

	if containsFold(normalized, "848x480") {
		if strings.Contains(normalized, "dvd") {
			return set(quality.DVD)
		} else if containsFold(normalized, "bluray") {
			return set(quality.Bluray480p)
		}
		return set(quality.SDTV)
	}
	if containsFold(normalized, "1280x720") {
		if containsFold(normalized, "bluray") {
			return set(quality.Bluray720p)
		}
		return set(quality.HDTV720p)
	}
	if containsFold(normalized, "1920x1080") {
		if containsFold(normalized, "bluray") {
			return set(quality.Bluray1080p)
		}
		return set(quality.HDTV1080p)
	}
	if containsFold(normalized, "bluray720p") {
		return set(quality.Bluray720p)
	}
	if containsFold(normalized, "bluray1080p") {
		return set(quality.Bluray1080p)
	}
	if containsFold(normalized, "bluray2160p") {
		return set(quality.Bluray2160p)
	}

	if m := otherSourceRegex.FindStringMatch(normalized); m != nil {
		if m.Success("sdtv") {
			return set(quality.SDTV)
		}
		if m.Success("hdtv") {
			return set(quality.HDTV720p)
		}
	}
	return result
}

func parseResolution(name string) int {
	m := resolutionRegex.FindStringMatch(name)
	alt := alternativeResolutionRegex.FindStringMatch(name)
	if m == nil && alt == nil {
		return resUnknown
	}
	if m != nil {
		switch {
		case m.Success("R360p"):
			return r360p
		case m.Success("R480p"):
			return r480p
		case m.Success("R540p"):
			return r540p
		case m.Success("R576p"):
			return r576p
		case m.Success("R720p"):
			return r720p
		case m.Success("R1080p"):
			return r1080p
		case m.Success("R2160p"):
			return r2160p
		}
	}
	if alt != nil && alt.Success("R2160p") {
		return r2160p
	}
	return resUnknown
}

func parseQualityModifiers(name, normalized string) QualityModel {
	result := QualityModel{Quality: quality.Unknown, Revision: Revision{Version: 1}}
	vm := versionRegex.FindStringMatch(normalized)
	version := func() int {
		v, _ := strconv.Atoi(vm.Value("version"))
		return v
	}
	if vm != nil {
		result.Revision.Version = version()
	}
	if properRegex.MatchString(normalized) {
		if vm != nil {
			result.Revision.Version = version() + 1
		} else {
			result.Revision.Version = 2
		}
	}
	if repackRegex.MatchString(normalized) {
		if vm != nil {
			result.Revision.Version = version() + 1
		} else {
			result.Revision.Version = 2
		}
		result.Revision.IsRepack = true
	}
	if n := len(realRegex.FindAllStringMatch(name)); n > 0 {
		result.Revision.Real = n
	}
	return result
}
