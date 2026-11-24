package cardigann

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"cinearr/internal/parser"
	"cinearr/internal/rx"
)

// ApplyFilters ports CardigannBase.ApplyFilters.
func ApplyFilters(data string, filters []FilterBlock, vars Vars) (string, error) {
	for _, f := range filters {
		switch f.Name {
		case "querystring":
			data = argumentFromQueryString(data, f.ArgString())
		case "timeparse", "dateparse":
			layout := f.ArgString()
			if t, err := parseGoLayout(data, layout); err == nil {
				data = t.Format(time.RFC1123Z)
			}
		case "regexp":
			re, err := rx.Cached(f.ArgString(), 0)
			if err != nil {
				return "", err
			}
			if m := re.FindStringMatch(data); m != nil {
				data = m.GroupNum(1).Value
			} else {
				data = ""
			}
		case "re_replace":
			a := f.ArgList()
			if len(a) < 2 {
				return "", fmt.Errorf("re_replace needs 2 arguments")
			}
			re, err := rx.Cached(a[0], 0)
			if err != nil {
				return "", err
			}
			repl, err := ApplyTemplate(a[1], vars, nil)
			if err != nil {
				return "", err
			}
			data = re.ReplaceAllString(data, repl)
		case "split":
			a := f.ArgList()
			if len(a) < 2 || a[0] == "" {
				return "", fmt.Errorf("split needs 2 arguments")
			}
			parts := strings.Split(data, string([]rune(a[0])[0]))
			pos, err := strconv.Atoi(strings.TrimSpace(a[1]))
			if err != nil {
				return "", err
			}
			if pos < 0 {
				pos += len(parts)
			}
			if pos < 0 || pos >= len(parts) {
				return "", fmt.Errorf("split index out of range")
			}
			data = parts[pos]
		case "replace":
			a := f.ArgList()
			if len(a) < 2 {
				return "", fmt.Errorf("replace needs 2 arguments")
			}
			to, err := ApplyTemplate(a[1], vars, nil)
			if err != nil {
				return "", err
			}
			if a[0] != "" {
				data = strings.ReplaceAll(data, a[0], to)
			}
		case "trim":
			if cut := f.ArgString(); cut != "" {
				data = strings.Trim(data, string([]rune(cut)[0]))
			} else {
				data = strings.TrimSpace(data)
			}
		case "prepend":
			s, err := ApplyTemplate(f.ArgString(), vars, nil)
			if err != nil {
				return "", err
			}
			data = s + data
		case "append":
			s, err := ApplyTemplate(f.ArgString(), vars, nil)
			if err != nil {
				return "", err
			}
			data += s
		case "tolower":
			data = strings.ToLower(data)
		case "toupper":
			data = strings.ToUpper(data)
		case "base64decode":
			s := strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimSpace(data))
			s = strings.TrimRight(s, "=")
			b, err := base64.RawStdEncoding.DecodeString(s)
			if err != nil {
				return "", err
			}
			data = string(b)
		case "base64encode":
			data = base64.RawURLEncoding.EncodeToString([]byte(data))
		case "urldecode":
			if s, err := url.QueryUnescape(data); err == nil {
				data = s
			}
		case "urlencode":
			data = url.QueryEscape(data)
		case "htmldecode":
			data = html.UnescapeString(data)
		case "htmlencode":
			data = html.EscapeString(data)
		case "timeago", "reltime":
			t, err := FromTimeAgo(data)
			if err != nil {
				return "", err
			}
			data = t.Format(time.RFC1123Z)
		case "fuzzytime":
			t, err := FromUnknown(data, f.ArgString())
			if err != nil {
				return "", err
			}
			data = t.Format(time.RFC1123Z)
		case "validfilename":
			data = strings.Map(func(r rune) rune {
				if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
					return '_'
				}
				return r
			}, data)
		case "diacritics":
			if f.ArgString() != "replace" {
				return "", fmt.Errorf("unsupported diacritics filter argument")
			}
			data = parser.RemoveAccent(data)
		case "jsonjoinarray":
			a := f.ArgList()
			if len(a) < 2 {
				return "", fmt.Errorf("jsonjoinarray needs 2 arguments")
			}
			var obj any
			if err := json.Unmarshal([]byte(data), &obj); err != nil {
				return "", err
			}
			tok, ok := jsonSelect(obj, strings.TrimPrefix(a[0], "$."))
			if !ok {
				return "", fmt.Errorf("jsonjoinarray: path %q not found", a[0])
			}
			var parts []string
			if arr, ok := tok.([]any); ok {
				for _, x := range arr {
					parts = append(parts, jsonString(x))
				}
			}
			data = strings.Join(parts, a[1])
		case "hexdump":
			_ = hex.EncodeToString([]byte(data)) // debugging aid only
		case "strdump":
		case "validate":
			delims := func(r rune) bool { return strings.ContainsRune(",  /)(.;[]\"|:", r) }
			valid := strings.FieldsFunc(strings.ToLower(f.ArgString()), delims)
			have := strings.FieldsFunc(strings.ToLower(data), delims)
			var out []string
			for _, v := range valid {
				for _, h := range have {
					if strings.TrimSpace(v) == strings.TrimSpace(h) && !containsStr(out, v) {
						out = append(out, v)
					}
				}
			}
			data = strings.Join(out, ", ")
		}
	}
	return data, nil
}

func containsStr(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func argumentFromQueryString(u, arg string) string {
	i := strings.IndexByte(u, '?')
	if i < 0 {
		return ""
	}
	qs := u[i+1:]
	if h := strings.IndexByte(qs, '#'); h >= 0 {
		qs = qs[:h]
	}
	v, err := url.ParseQuery(qs)
	if err != nil {
		return ""
	}
	return v.Get(arg)
}

// ---------------------------------------------------------------------------
// Numbers and sizes (ParseUtil)

func normalizeNumber(s string, isInt bool) string {
	var b strings.Builder
	for _, c := range s {
		if unicode.IsDigit(c) || c == '.' || c == ',' {
			b.WriteRune(c)
		}
	}
	v := strings.TrimSpace(b.String())
	if isInt {
		if strings.Contains(v, ",") && strings.Contains(v, ".") {
			return v
		}
		if v == "" {
			return "0"
		}
		// "1.234" or "1,234" are thousands separators for integers
		return strings.NewReplacer(".", "", ",", "").Replace(v)
	}
	if v == "" {
		return "0"
	}
	v = strings.ReplaceAll(v, ",", ".")
	if strings.Count(v, ".") > 1 {
		last := strings.LastIndex(v, ".")
		v = strings.ReplaceAll(v[:last], ".", "") + v[last:]
	}
	return v
}

// CoerceDouble parses a loosely formatted decimal.
func CoerceDouble(s string) float64 {
	f, _ := strconv.ParseFloat(normalizeNumber(s, false), 64)
	return f
}

// CoerceInt parses a loosely formatted integer.
func CoerceInt(s string) int {
	v := normalizeNumber(s, true)
	if strings.Contains(v, ",") && strings.Contains(v, ".") {
		v = strings.NewReplacer(",", "", ".", "").Replace(v)
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return int(n)
}

// LongFromString extracts the first run of digits.
func LongFromString(s string) int64 {
	var b strings.Builder
	for _, c := range s {
		if c < '0' || c > '9' {
			if b.Len() > 0 {
				break
			}
			continue
		}
		b.WriteRune(c)
	}
	n, _ := strconv.ParseInt(b.String(), 10, 64)
	return n
}

// GetBytes ports ParseUtil.GetBytes ("1.4 GB", "700 MiB", "1,2 Go").
func GetBytes(s string) int64 {
	var unit strings.Builder
	for _, c := range s {
		if unicode.IsLetter(c) {
			unit.WriteRune(c)
		}
	}
	val := CoerceDouble(s)
	u := strings.ToLower(strings.ReplaceAll(unit.String(), "i", ""))
	// French units (Ko, Mo, Go, To)
	u = strings.NewReplacer("ko", "kb", "mo", "mb", "go", "gb", "to", "tb").Replace(u)
	switch {
	case strings.Contains(u, "kb"):
		return int64(val * 1024)
	case strings.Contains(u, "mb"):
		return int64(val * 1024 * 1024)
	case strings.Contains(u, "gb"):
		return int64(val * 1024 * 1024 * 1024)
	case strings.Contains(u, "tb"):
		return int64(val * 1024 * 1024 * 1024 * 1024)
	}
	return int64(math.Round(val))
}

// ---------------------------------------------------------------------------
// Dates (DateTimeUtil)

var (
	timeAgoRe     = regexp.MustCompile(`(?i)\bago`)
	todayRe       = regexp.MustCompile(`(?i)\btoday(?:[\s,]+(?:at)?\s*|[\s,]*|$)`)
	tomorrowRe    = regexp.MustCompile(`(?i)\btomorrow(?:[\s,]+(?:at)?\s*|[\s,]*|$)`)
	yesterdayRe   = regexp.MustCompile(`(?i)\byesterday(?:[\s,]+(?:at)?\s*|[\s,]*|$)`)
	daysOfWeekRe  = regexp.MustCompile(`(?i)\b(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+at\s+`)
	missingYearRe = regexp.MustCompile(`^(\d{1,2}-\d{1,2})(\s|$)`)
	missingYear2  = regexp.MustCompile(`^(\d{1,2}\s+\w{3})\s+(\d{1,2}\:\d{1,2}.*)$`)
	timeAgoPart   = regexp.MustCompile(`\s*?([\d\.]+)\s*?([^\d\s\.]+)\s*?`)
)

// FromTimeAgo parses "2 hours 1 day ago".
func FromTimeAgo(s string) (time.Time, error) {
	s = strings.ToLower(s)
	if strings.Contains(s, "now") {
		return time.Now(), nil
	}
	s = strings.NewReplacer(",", "", "ago", "", "and", "").Replace(s)
	var d time.Duration
	for _, m := range timeAgoPart.FindAllStringSubmatch(s, -1) {
		v := CoerceDouble(m[1])
		unit := m[2]
		switch {
		case strings.Contains(unit, "sec") || unit == "s":
			d += time.Duration(v * float64(time.Second))
		case strings.Contains(unit, "min") || unit == "m":
			d += time.Duration(v * float64(time.Minute))
		case strings.Contains(unit, "hour") || strings.Contains(unit, "hr") || unit == "h":
			d += time.Duration(v * float64(time.Hour))
		case strings.Contains(unit, "day") || unit == "d":
			d += time.Duration(v * 24 * float64(time.Hour))
		case strings.Contains(unit, "week") || strings.Contains(unit, "wk") || unit == "w":
			d += time.Duration(v * 7 * 24 * float64(time.Hour))
		case strings.Contains(unit, "month") || unit == "mo":
			d += time.Duration(v * 30 * 24 * float64(time.Hour))
		case strings.Contains(unit, "year") || unit == "y":
			d += time.Duration(v * 365 * 24 * float64(time.Hour))
		default:
			return time.Time{}, fmt.Errorf("timeago: unknown unit %q", unit)
		}
	}
	return time.Now().Add(-d), nil
}

func parseTimeOfDay(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, l := range []string{"15:04:05", "15:04", "3:04 PM", "3:04PM", "3:04 pm", "3:04pm", "3:04:05 PM"} {
		if t, err := time.Parse(l, strings.ToUpper(s)); err == nil {
			return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second
		}
	}
	return 0
}

func today() time.Time {
	n := time.Now().UTC()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// FromUnknown ports DateTimeUtil.FromUnknown.
func FromUnknown(s, format string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC1123Z, s); err == nil {
		return t, nil
	}
	if s != "" && strings.Trim(s, "0123456789") == "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Unix(n, 0), nil
		}
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "now") {
		return time.Now().UTC(), nil
	}
	if timeAgoRe.MatchString(s) {
		return FromTimeAgo(s)
	}
	if m := todayRe.FindString(s); m != "" {
		return today().Add(parseTimeOfDay(strings.Replace(s, m, "", 1))), nil
	}
	if m := yesterdayRe.FindString(s); m != "" {
		return today().Add(parseTimeOfDay(strings.Replace(s, m, "", 1)) - 24*time.Hour), nil
	}
	if m := tomorrowRe.FindString(s); m != "" {
		return today().Add(parseTimeOfDay(strings.Replace(s, m, "", 1)) + 24*time.Hour), nil
	}
	if m := daysOfWeekRe.FindStringSubmatch(s); m != nil {
		dt := today().Add(parseTimeOfDay(strings.Replace(s, m[0], "", 1)))
		want := map[string]time.Weekday{"monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
			"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday, "sunday": time.Sunday}[strings.ToLower(m[1])]
		for dt.Weekday() != want {
			dt = dt.Add(-24 * time.Hour)
		}
		return dt, nil
	}
	if m := missingYearRe.FindStringSubmatch(s); m != nil {
		s = strings.Replace(s, m[1], fmt.Sprintf("%d-%s", time.Now().Year(), m[1]), 1)
	}
	if m := missingYear2.FindStringSubmatch(s); m != nil {
		s = fmt.Sprintf("%s %d %s", m[1], time.Now().Year(), m[2])
	}
	return fromFuzzy(s, format)
}

var fuzzyLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC3339Nano, time.RFC822Z, time.RFC822, time.ANSIC,
	"2006-01-02T15:04:05", "2006-01-02T15:04:05.000", "2006-01-02 15:04:05", "2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05 MST", "2006-01-02 15:04", "2006-01-02", "2006-1-2 15:04:05", "2006-1-2 15:04", "2006-1-2",
	"2006/01/02 15:04:05", "2006/01/02 15:04", "2006/01/02", "2006.01.02 15:04", "2006.01.02",
	"Jan 2 2006", "Jan 2, 2006", "Jan 2 2006 15:04", "Jan 2, 2006 15:04", "Jan 02 2006", "January 2, 2006", "January 2 2006",
	"2 Jan 2006", "02 Jan 2006", "2 Jan 2006 15:04", "02 Jan 2006 15:04", "02 Jan 2006 15:04:05", "2 January 2006",
	"2 January 2006 15:04", "02-Jan-2006", "2-Jan-2006", "02-Jan-06", "Mon Jan 2 15:04:05 2006", "Mon, 02 Jan 2006",
	"Monday, January 2, 2006", "Monday 2 January 2006", "Jan 2 15:04", "2 Jan 15:04",
}

var usLayouts = []string{"01/02/2006 15:04:05", "01/02/2006 15:04", "01/02/2006", "1/2/2006 15:04", "1/2/2006", "01-02-2006 15:04", "01-02-2006", "01/02/06"}
var ukLayouts = []string{"02/01/2006 15:04:05", "02/01/2006 15:04", "02/01/2006", "2/1/2006 15:04", "2/1/2006", "02-01-2006 15:04", "02-01-2006", "02.01.2006 15:04:05", "02.01.2006 15:04", "02.01.2006", "02/01/06"}

func fromFuzzy(s, format string) (time.Time, error) {
	s = strings.Join(strings.Fields(s), " ")
	layouts := append([]string{}, fuzzyLayouts...)
	if format == "UK" {
		layouts = append(layouts, ukLayouts...)
		layouts = append(layouts, usLayouts...)
	} else {
		layouts = append(layouts, usLayouts...)
		layouts = append(layouts, ukLayouts...)
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			if t.Year() == 0 {
				t = t.AddDate(time.Now().Year(), 0, 0)
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse date %q", s)
}

// parseGoLayout ports ParseDateTimeGoLang: the layouts are Go layouts, so
// time.Parse handles them natively.
func parseGoLayout(s, layout string) (time.Time, error) {
	return time.Parse(layout, strings.TrimSpace(s))
}

// ---------------------------------------------------------------------------
// JSON selection (a subset of Json.NET SelectToken)

func jsonSelect(obj any, path string) (any, bool) {
	path = strings.TrimSpace(strings.TrimPrefix(path, "$"))
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return obj, true
	}
	cur := obj
	for _, seg := range splitJSONPath(path) {
		switch {
		case strings.HasPrefix(seg, "["):
			inner := strings.Trim(seg, "[]")
			if strings.HasPrefix(inner, "'") || strings.HasPrefix(inner, `"`) {
				m, ok := cur.(map[string]any)
				if !ok {
					return nil, false
				}
				v, ok := m[strings.Trim(inner, `'"`)]
				if !ok {
					return nil, false
				}
				cur = v
				continue
			}
			idx, err := strconv.Atoi(inner)
			arr, ok := cur.([]any)
			if err != nil || !ok {
				return nil, false
			}
			if idx < 0 {
				idx += len(arr)
			}
			if idx < 0 || idx >= len(arr) {
				return nil, false
			}
			cur = arr[idx]
		default:
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			v, ok := m[seg]
			if !ok {
				return nil, false
			}
			cur = v
		}
	}
	return cur, true
}

func splitJSONPath(p string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '.':
			flush()
		case '[':
			flush()
			end := strings.IndexByte(p[i:], ']')
			if end < 0 {
				b.WriteString(p[i:])
				i = len(p)
				continue
			}
			out = append(out, p[i:i+end+1])
			i += end
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return out
}

func jsonString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case json.Number:
		return x.String()
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = jsonString(e)
		}
		return strings.Join(parts, ",")
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
