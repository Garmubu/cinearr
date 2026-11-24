package parser

import (
	"strings"

	"cinearr/internal/rx"
)

// Port of LanguageParser.ParseLanguages (Radarr). Languages are reported by
// their English name, as in Radarr ("French", "English", "Original", ...).

var languageRegex = rx.MustCompile(`(?:\W|_|^)(?<english>\beng\b)|
	(?<italian>\b(?:ita|italian)\b)|
	(?<german>(?:swiss)?german\b|videomann|ger[. ]dub|\bger\b)|
	(?<flemish>flemish)|
	(?<bulgarian>bgaudio)|
	(?<romanian>rodubbed)|
	(?<brazilian>\b(dublado|pt-BR)\b)|
	(?<greek>greek)|
	(?<french>\b(?:FR|VO|VF|VFF|VFQ|VFI|VF2|TRUEFRENCH|FRENCH|FRE|FRA)\b)|
	(?<russian>\b(?:rus|ru)\b)|
	(?<hungarian>\b(?:HUNDUB|HUN)\b)|
	(?<hebrew>\b(?:HebDub|HebDubbed)\b)|
	(?<polish>\b(?:PL\W?DUB|DUB\W?PL|LEK\W?PL|PL\W?LEK)\b)|
	(?<chinese>\[(?:CH[ST]|BIG5|GB)\]|简|繁|字幕)|
	(?<ukrainian>(?:(?:\dx)?UKR))|
	(?<spanish>\b(?:español|castellano)\b)|
	(?<catalan>\b(?:catalan?|catalán|català)\b)|
	(?<latvian>\b(?:lat|lav|lv)\b)|
	(?<telugu>\btel\b)|
	(?<vietnamese>\bVIE\b)|
	(?<japanese>\bJAP\b)|
	(?<korean>\bKOR\b)|
	(?<urdu>\burdu\b)|
	(?<romansh>\b(?:romansh|rumantsch|romansch)\b)|
	(?<mongolian>\b(?:mongolian|khalkha)\b)|
	(?<georgian>\b(?:georgian|geo|ka|kat)\b)|
	(?<irish>\b(?:irish|gaeilge)\b)|
	(?<original>\b(?:orig|original)\b)`, icx)

var caseSensitiveLanguageRegex = rx.MustCompile(`(?:(?i)(?<!SUB[\W|_|^]))(?:(?<english>\bEN\b)|
	(?<lithuanian>\bLT\b)|
	(?<czech>\bCZ\b)|
	(?<polish>\bPL\b)|
	(?<bulgarian>\bBG\b)|
	(?<slovak>\bSK\b)|
	(?<german>\bDE\b)|
	(?<spanish>\b(?<!DTS[._ -])ES\b))(?:(?i)(?![\W|_|^]SUB))`, rx.IgnorePatternWhitespace)

var (
	germanDualLanguageRegex  = rx.MustCompile(`(?<!WEB[-_. ]?)\bDL\b`, ic)
	germanMultiLanguageRegex = rx.MustCompile(`\bML\b`, ic)
)

var containsLanguages = []struct{ word, lang string }{
	{"english", "English"}, {"spanish", "Spanish"}, {"danish", "Danish"}, {"dutch", "Dutch"},
	{"japanese", "Japanese"}, {"icelandic", "Icelandic"}, {"mandarin", "Chinese"}, {"cantonese", "Chinese"},
	{"chinese", "Chinese"}, {"korean", "Korean"}, {"russian", "Russian"}, {"romanian", "Romanian"},
	{"hindi", "Hindi"}, {"arabic", "Arabic"}, {"thai", "Thai"}, {"bulgarian", "Bulgarian"},
	{"polish", "Polish"}, {"vietnamese", "Vietnamese"}, {"swedish", "Swedish"}, {"norwegian", "Norwegian"},
	{"finnish", "Finnish"}, {"turkish", "Turkish"}, {"portuguese", "Portuguese"}, {"brazilian", "Portuguese (Brazil)"},
	{"hungarian", "Hungarian"}, {"hebrew", "Hebrew"}, {"ukrainian", "Ukrainian"}, {"persian", "Persian"},
	{"bengali", "Bengali"}, {"slovak", "Slovak"}, {"latvian", "Latvian"}, {"latino", "Spanish (Latino)"},
	{"tamil", "Tamil"}, {"telugu", "Telugu"}, {"malayalam", "Malayalam"}, {"kannada", "Kannada"},
	{"albanian", "Albanian"}, {"afrikaans", "Afrikaans"}, {"marathi", "Marathi"}, {"tagalog", "Tagalog"},
}

var caseSensitiveGroups = []struct{ group, lang string }{
	{"english", "English"}, {"lithuanian", "Lithuanian"}, {"czech", "Czech"}, {"polish", "Polish"},
	{"bulgarian", "Bulgarian"}, {"slovak", "Slovak"}, {"spanish", "Spanish"}, {"german", "German"},
}

var regexGroups = []struct{ group, lang string }{
	{"english", "English"}, {"italian", "Italian"}, {"german", "German"}, {"flemish", "Flemish"},
	{"greek", "Greek"}, {"french", "French"}, {"russian", "Russian"}, {"bulgarian", "Bulgarian"},
	{"brazilian", "Portuguese (Brazil)"}, {"dutch", "Dutch"}, {"hungarian", "Hungarian"}, {"hebrew", "Hebrew"},
	{"polish", "Polish"}, {"chinese", "Chinese"}, {"spanish", "Spanish"}, {"catalan", "Catalan"},
	{"ukrainian", "Ukrainian"}, {"latvian", "Latvian"}, {"romanian", "Romanian"}, {"telugu", "Telugu"},
	{"vietnamese", "Vietnamese"}, {"japanese", "Japanese"}, {"korean", "Korean"}, {"urdu", "Urdu"},
	{"romansh", "Romansh"}, {"mongolian", "Mongolian"}, {"georgian", "Georgian"}, {"irish", "Irish"},
	{"original", "Original"},
}

// ParseLanguages returns the languages found in a release title, or
// ["Unknown"].
func ParseLanguages(title string) []string {
	lower := strings.ToLower(title)
	var langs []string
	for _, c := range containsLanguages {
		if strings.Contains(lower, c.word) {
			langs = append(langs, c.lang)
		}
	}
	for _, m := range caseSensitiveLanguageRegex.FindAllStringMatch(title) {
		for _, g := range caseSensitiveGroups {
			if m.Success(g.group) {
				langs = append(langs, g.lang)
			}
		}
	}
	for _, m := range languageRegex.FindAllStringMatch(title) {
		for _, g := range regexGroups {
			if m.Success(g.group) {
				langs = append(langs, g.lang)
			}
		}
	}
	if len(langs) == 0 {
		langs = append(langs, "Unknown")
	}
	if len(langs) == 1 && langs[0] == "German" {
		if germanDualLanguageRegex.MatchString(title) {
			langs = append(langs, "Original")
		} else if germanMultiLanguageRegex.MatchString(title) {
			langs = append(langs, "Original", "English")
		}
	}
	return distinct(langs)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func distinct(list []string) []string {
	var out []string
	for _, v := range list {
		if !contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
