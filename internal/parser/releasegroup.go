package parser

import (
	"strconv"
	"strings"

	"cinearr/internal/rx"
)

// Port of ReleaseGroupParser (Radarr).

var (
	releaseGroupRegex               = rx.MustCompile(`-(?<releasegroup>[a-z0-9]+(?<part2>-[a-z0-9]+)?(?!.+?(?:480p|576p|720p|1080p|2160p)))(?<!(?:WEB-(DL|Rip)|Blu-Ray|480p|576p|720p|1080p|2160p|DTS-HD|DTS-X|DTS-MA|DTS-ES|-ES|-EN|-CAT|-ENG|-JAP|-GER|-FRA|-FRE|-ITA|-HDRip|\d{1,2}-bit|[ ._]\d{4}-\d{2}|-\d{2}|tmdb(id)?-(?<tmdbid>\d+)|(?<imdbid>tt\d{7,8}))(?:\k<part2>)?)(?:\b|[-._ ]|$)|[-._ ]\[(?<releasegroup>[a-z0-9]+)\]$`, ic)
	invalidReleaseGroupRegex        = rx.MustCompile(`^([se]\d+|[0-9a-f]{8})$`, ic)
	animeReleaseGroupRegex          = rx.MustCompile(`^(?:\[(?<subgroup>(?!\s).+?(?<!\s))\](?:_|-|\s|\.)?)`, ic)
	exceptionReleaseGroupRegexExact = rx.MustCompile(`\b(?<releasegroup>KRaLiMaRKo|E\.N\.D|D\-Z0N3|Koten_Gars|BluDragon|ZØNEHD|HQMUX|VARYG|YIFY|YTS(.(MX|LT|AG))?|TMd|Eml HDTeam|LMain|DarQ|BEN THE MEN|TAoE|QxR|126811)\b`, ic)
	exceptionReleaseGroupRegex      = rx.MustCompile(`(?<=[._ \[])(?<releasegroup>(Silence|afm72|Panda|Ghost|MONOLITH|Tigole|Joy|ImE|UTR|t3nzin|Anime Time|Project Angel|Hakata Ramen|HONE|GiLG|Vyndros|SEV|Garshasp|Kappa|Natty|RCVR|SAMPA|YOGI|r00t|EDGE2020|RZeroX|FreetheFish|Anna|Bandi|Qman|theincognito|HDO|DusIctv|DHD|CtrlHD|-ZR-|ADC|XZVN|RH|Kametsu|Celdra)(?=\]|\)))`, ic)
	cleanReleaseGroupRegex          = rx.MustCompile(`(-(RP|1|NZBGeek|Obfuscated|Obfuscation|Scrambled|sample|Pre|postbot|xpost|Rakuv[a-z0-9]*|WhiteRev|BUYMORE|AsRequested|AlternativeToRequested|GEROV|Z0iDS3N|Chamele0n|4P|4Planet|AlteZachen|RePACKPOST))+$`, ic)
)

// ParseReleaseGroup returns the release group of a title, or "".
func ParseReleaseGroup(title string) string {
	title = strings.TrimSpace(title)
	title = RemoveFileExtension(title)
	title = websitePrefixRegex.ReplaceAllString(title, "")
	title = cleanTorrentSuffixRegex.ReplaceAllString(title, "")

	if m := animeReleaseGroupRegex.FindStringMatch(title); m != nil {
		return m.Value("subgroup")
	}
	title = cleanReleaseGroupRegex.ReplaceAllString(title, "")

	if m := exceptionReleaseGroupRegexExact.LastMatch(title); m != nil {
		return m.Value("releasegroup")
	}
	if m := exceptionReleaseGroupRegex.LastMatch(title); m != nil {
		return m.Value("releasegroup")
	}
	if m := releaseGroupRegex.LastMatch(title); m != nil {
		group := m.Value("releasegroup")
		if _, err := strconv.Atoi(group); err == nil {
			return ""
		}
		if invalidReleaseGroupRegex.MatchString(group) {
			return ""
		}
		return group
	}
	return ""
}
