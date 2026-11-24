package diag

import (
	"io"
	"net/url"
	"regexp"
	"strings"
)

// Secrets and personal data are removed from the log, the diagnostic and
// the error messages shown in reports. The rules follow Sonarr's
// CleanseLogMessage: keys, passwords and tokens, tracker passkeys,
// credentials in URLs, user names in home folder paths and e-mail
// addresses.

var secretWords = []string{"pass", "key", "token", "cookie", "secret", "pin", "auth", "sid", "rss", "2fa", "otp", "hash",
	"user", "login", "mail", "account", "uid", "session"}

// IsSecret reports whether a parameter or setting name holds a secret or
// personal data.
func IsSecret(name string) bool {
	n := strings.ToLower(name)
	if n == "infohash" {
		return false
	}
	for _, w := range secretWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// RedactURL hides credentials and secret query parameters.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid url>"
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	q := u.Query()
	changed := false
	for k := range q {
		if IsSecret(k) {
			q.Set(k, "***")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	// passkeys embedded in paths (/rss/<32 hex>/...)
	return RedactText(passkeyInPath.ReplaceAllString(u.String(), "/***"))
}

var passkeyInPath = regexp.MustCompile(`/[0-9a-fA-F]{32,64}(?:/|$)`)

// rule replaces what a pattern matches ("${1}" keeps the name of the
// secret, the value becomes "***").
type rule struct {
	re   *regexp.Regexp
	with string
}

var rules = []rule{
	// credentials in URLs: scheme://user:password@host
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@"']+:[^/\s@"']+@`), "${1}***@"},
	// name=value pairs (query strings, cookies, form bodies)
	{regexp.MustCompile(`(?i)\b(\w*?(?:api_?key|apikey|passkey|passw(?:or)?d|pass|pwd|token|authkey|auth|torrent_pass|rsskey|secret|session|sid|uid|user(?:name)?|login|cookie))=([^&\s"';,]+)`), "${1}=***"},
	// JSON fields: "password": "…", "apiKey": "…", "username": "…"
	{regexp.MustCompile(`(?i)("[^"]*?(?:password|passwd|key|passkey|token|secret|cookie|username|user|login|authkey|sid|email|mail)"\s*:\s*)"[^"]*"`), `${1}"***"`},
	// HTTP headers
	{regexp.MustCompile(`(?i)\b(authorization|cookie|set-cookie|x-api-key|api-key)(\s*[:=]\s*)[^\r\n]+`), "${1}${2}***"},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[a-z0-9._~+/=-]{8,}`), "${1} ***"},
	// JSON web tokens (TMDB read access tokens)
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]+`), "***"},
	// tracker announce URLs: /announce/<passkey>, /<passkey>/announce
	{regexp.MustCompile(`(?i)(announce(?:\.php)?(?:/|%2f|\?passkey=|%3fpasskey%3d))[a-z0-9]{16,}`), "${1}***"},
	{regexp.MustCompile(`(?i)((?:^|[^a-z0-9%])|%2f)[a-z0-9]{16,}((?:/|%2f)announce)`), "${1}***${2}"},
	// user names in home folders: C:\Users\<name>\, /home/<name>/, /Users/<name>/
	{regexp.MustCompile(`(?i)\b([a-z]:(?:\\\\|\\|/)(?:users|documents and settings)(?:\\\\|\\|/))[^\\/"'\r\n]+`), "${1}***"},
	{regexp.MustCompile(`(/(?:home|Users)/)[^/"'\s]+`), "${1}***"},
	// e-mail addresses
	{regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@([a-z0-9-]+(?:\.[a-z0-9-]+)*\.[a-z]{2,})\b`), "***@${1}"},
}

// RedactText hides secrets and personal data that may appear in free text
// (log lines, error messages, reports).
func RedactText(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.with)
	}
	return s
}

// redactWriter cleans every log line before writing it.
type redactWriter struct{ w io.Writer }

func (r redactWriter) Write(p []byte) (int, error) {
	if _, err := r.w.Write([]byte(RedactText(string(p)))); err != nil {
		return 0, err
	}
	return len(p), nil
}
