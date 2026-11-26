package subtitles

import (
	"context"
	"errors"
	"strings"

	"cinearr/internal/i18n"
)

// Field is a setting of a provider (API key, account…).
type Field struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"` // text, password, checkbox
	Required bool   `json:"required"`
	Hint     string `json:"hint,omitempty"`
}

// Info describes a provider of the catalog.
type Info struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Languages   []string `json:"languages"` // ISO 639-1; empty = many
	Fields      []Field  `json:"fields"`
	// SignUp tells where to get the key or account.
	SignUp string `json:"signUp,omitempty"`
	// Series: the provider also has subtitles for series episodes; Films
	// is false for providers specialised in series.
	Series bool `json:"series"`
	Films  bool `json:"films"`
	// Multiple: can be added several times (sites added by the user).
	Multiple bool `json:"multiple,omitempty"`
	// Link: not searched automatically, its search page is opened in the
	// browser.
	Link bool `json:"link,omitempty"`

	build func(cfg map[string]string) (Provider, error)
}

// Config is a provider chosen by the user.
type Config struct {
	ID       string            `json:"id"`
	Enabled  bool              `json:"enabled"`
	Settings map[string]string `json:"settings,omitempty"`
}

// Tester is implemented by providers able to check their settings.
type Tester interface {
	Test(ctx context.Context) (string, error)
}

// Catalog lists the available providers (ports of Bazarr's providers), in
// the order shown to the user, then the two kinds of sites the user can add.
func Catalog() []Info {
	builtin := BuiltinOpenSubtitlesKey != ""
	osKeyHint := "Required: on opensubtitles.com, avatar › API consumers › New consumer, then copy the API key."
	if builtin {
		osKeyHint = "Optional: Cinearr provides its own key. Enter yours if that key ever stops working."
	}
	return []Info{
		{
			ID: "opensubtitles", Name: "OpenSubtitles.com", URL: "https://www.opensubtitles.com", Films: true, Series: true,
			Description: "The largest subtitle database, every language. Matches the exact file by its hash.",
			SignUp:      "https://www.opensubtitles.com/en/consumers",
			Fields: []Field{
				{Key: "username", Label: "Username", Type: "text", Hint: "Free account: raises the daily download quota."},
				{Key: "password", Label: "Password", Type: "password"},
				{Key: "apiKey", Label: "API key", Type: "password", Required: !builtin, Hint: osKeyHint},
				{Key: "includeAI", Label: "Accept machine translations", Type: "checkbox"},
			},
			build: func(c map[string]string) (Provider, error) {
				key := strings.TrimSpace(c["apiKey"])
				builtin := key == "" && BuiltinOpenSubtitlesKey != ""
				if builtin {
					key = BuiltinOpenSubtitlesKey
				}
				if key == "" {
					return nil, i18n.New("OpenSubtitles.com requires an API key in addition to the username and password: create it for free on opensubtitles.com › API consumers › New consumer.")
				}
				return &OpenSubtitles{APIKey: key, Builtin: builtin, Username: strings.TrimSpace(c["username"]), Password: c["password"], IncludeAI: c["includeAI"] == "true"}, nil
			},
		},
		{
			ID: "subdl", Name: "SubDL", URL: "https://subdl.com", Films: true, Series: true,
			Description: "Large multilingual database (formerly Subscene).",
			SignUp:      "https://subdl.com/panel/api",
			Fields:      []Field{{Key: "apiKey", Label: "API key", Type: "password", Required: true, Hint: "Free: create an account on subdl.com, then copy the key from your panel."}},
			build: func(c map[string]string) (Provider, error) {
				key := strings.TrimSpace(c["apiKey"])
				if key == "" {
					return nil, i18n.New("SubDL: API key missing")
				}
				return &SubDL{APIKey: key}, nil
			},
		},
		{
			ID: "subsource", Name: "Subsource", URL: "https://subsource.net", Films: true,
			Description: "Multilingual community database, many recent releases.",
			SignUp:      "https://subsource.net",
			Fields:      []Field{{Key: "apiKey", Label: "API key", Type: "password", Required: true, Hint: "Free: create an account on subsource.net, then copy the API key from your profile."}},
			build: func(c map[string]string) (Provider, error) {
				key := strings.TrimSpace(c["apiKey"])
				if key == "" {
					return nil, i18n.New("Subsource: API key missing")
				}
				return &Subsource{APIKey: key}, nil
			},
		},
		{
			ID: "yifysubtitles", Name: "YIFY Subtitles", URL: "https://yifysubtitles.ch", Films: true,
			Description: "Films only, many languages. No account needed.",
			build:       func(c map[string]string) (Provider, error) { return &YIFY{}, nil },
		},
		{
			ID: "subsynchro", Name: "Subsynchro", URL: "https://www.subsynchro.com", Films: true,
			Description: "French subtitles for films. No account needed.",
			Languages:   []string{"fr"},
			build:       func(c map[string]string) (Provider, error) { return &Subsynchro{}, nil },
		},
		{
			ID: "gestdown", Name: "Addic7ed (Gestdown)", URL: "https://www.gestdown.info", Series: true,
			Description: "Series only: the Addic7ed subtitles, the reference for series, through the Gestdown API. Many languages, no account needed.",
			build:       func(c map[string]string) (Provider, error) { return &Gestdown{}, nil },
		},
		{
			ID: "tvsubtitles", Name: "TVsubtitles.net", URL: "https://www.tvsubtitles.net", Series: true,
			Description: "Series only, many languages. No account needed.",
			build:       func(c map[string]string) (Provider, error) { return &TVSubtitles{}, nil },
		},
		{
			ID: "betaseries", Name: "BetaSeries", URL: "https://www.betaseries.com", Series: true,
			Description: "Series only: French and original-language subtitles from the French series site.",
			Languages:   []string{"fr", "en"},
			SignUp:      "https://www.betaseries.com/api/",
			Fields:      []Field{{Key: "apiKey", Label: "API key", Type: "password", Required: true, Hint: "Free: create an account on betaseries.com, then request a key on betaseries.com/api."}},
			build: func(c map[string]string) (Provider, error) {
				key := strings.TrimSpace(c["apiKey"])
				if key == "" {
					return nil, i18n.New("BetaSeries: API key missing")
				}
				return &BetaSeries{Key: key}, nil
			},
		},
		{
			ID: "custom-api", Name: "Other service (OpenSubtitles-compatible API)", Films: true, Series: true, Multiple: true,
			Description: "Any service exposing the OpenSubtitles.com API (a self-hosted aggregator, a mirror…): searched automatically like the others.",
			Fields: []Field{
				{Key: "name", Label: "Name", Type: "text", Required: true},
				{Key: "url", Label: "API address", Type: "text", Required: true, Hint: "The address ending with /api/v1/, e.g. https://subtitles.example.org/api/v1/"},
				{Key: "apiKey", Label: "API key", Type: "password"},
				{Key: "username", Label: "Username", Type: "text"},
				{Key: "password", Label: "Password", Type: "password"},
			},
			build: func(c map[string]string) (Provider, error) {
				name, u := strings.TrimSpace(c["name"]), strings.TrimSpace(c["url"])
				if name == "" || !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
					return nil, i18n.Errorf("%s: name or address missing", "OpenSubtitles API")
				}
				if !strings.HasSuffix(u, "/") {
					u += "/"
				}
				return &OpenSubtitles{APIKey: strings.TrimSpace(c["apiKey"]), Username: strings.TrimSpace(c["username"]), Password: c["password"],
					baseURL: u, fixed: true, name: "custom:" + name}, nil
			},
		},
		{
			ID: "custom-link", Name: "Your own site (search page)", Films: true, Series: true, Multiple: true, Link: true,
			Description: "Any subtitle website: Cinearr opens its search page for the film or the episode; you download the subtitle there and add it with “Add a subtitle file”.",
			Fields: []Field{
				{Key: "name", Label: "Name", Type: "text", Required: true},
				{Key: "url", Label: "Search address", Type: "text", Required: true,
					Hint: "The address of the search page, with {query} where the title goes, e.g. https://example.org/search?q={query}. Also: {title}, {original}, {year}, {imdb}, {tmdb}, {season}, {episode}, {s00e00}, {lang}."},
			},
		},
	}
}

// Lookup returns a provider of the catalog.
func Lookup(id string) (Info, bool) {
	for _, p := range Catalog() {
		if p.ID == id {
			return p, true
		}
	}
	return Info{}, false
}

// Build creates the enabled providers; a provider whose settings are
// incomplete is skipped and reported.
func Build(cfgs []Config) ([]Provider, []error) {
	var out []Provider
	var errs []error
	for _, c := range cfgs {
		if !c.Enabled {
			continue
		}
		info, ok := Lookup(c.ID)
		if !ok || info.build == nil { // search links are not providers
			continue
		}
		p, err := info.build(c.Settings)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, p)
	}
	return out, errs
}

// BuildOne creates a provider from a configuration, enabled or not.
func BuildOne(c Config) (Provider, error) {
	info, ok := Lookup(c.ID)
	if !ok || info.build == nil {
		return nil, errors.New("unknown provider " + c.ID)
	}
	return info.build(c.Settings)
}
