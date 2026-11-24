// Package i18n translates the texts of the interface and of the server
// messages. English is the source language: every text is written in
// English in the code and looked up, as is, in the catalog of the chosen
// language (locales/<code>.json). A missing translation falls back to
// English, so a partial catalog never breaks anything.
package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

//go:embed locales/*.json
var files embed.FS

// Language is a supported interface language.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"` // in the language itself
	// TMDB is the matching language for the film pages.
	TMDB string `json:"tmdb"`
	// Release is the name used by release titles ("French"), for the
	// preferred languages of the quality profile.
	Release string `json:"release"`
	// Audio lists the codes given to the player for the audio track.
	Audio string `json:"audio"`
}

// Languages lists the supported languages; English is the default.
var Languages = []Language{
	{Code: "en", Name: "English", TMDB: "en-US", Release: "English", Audio: "eng,en"},
	{Code: "fr", Name: "Français", TMDB: "fr-FR", Release: "French", Audio: "fre,fra,fr,eng,en"},
	{Code: "es", Name: "Español", TMDB: "es-ES", Release: "Spanish", Audio: "spa,es,eng,en"},
	{Code: "it", Name: "Italiano", TMDB: "it-IT", Release: "Italian", Audio: "ita,it,eng,en"},
	{Code: "de", Name: "Deutsch", TMDB: "de-DE", Release: "German", Audio: "ger,deu,de,eng,en"},
}

// Lookup returns a supported language (English when unknown).
func Lookup(code string) Language {
	code = strings.ToLower(code)
	if i := strings.IndexAny(code, "-_"); i > 0 {
		code = code[:i]
	}
	for _, l := range Languages {
		if l.Code == code {
			return l
		}
	}
	return Languages[0]
}

var (
	mu       sync.RWMutex
	current  = "en"
	catalogs = map[string]map[string]string{}
)

func init() {
	for _, l := range Languages[1:] {
		b, err := files.ReadFile("locales/" + l.Code + ".json")
		if err != nil {
			continue
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err == nil {
			catalogs[l.Code] = m
		}
	}
}

// SetLanguage selects the language of the server messages.
func SetLanguage(code string) {
	mu.Lock()
	current = Lookup(code).Code
	mu.Unlock()
}

// Catalog returns the translations of a language (empty for English).
func Catalog(code string) map[string]string {
	if m := catalogs[Lookup(code).Code]; m != nil {
		return m
	}
	return map[string]string{}
}

// T translates an English text into the selected language.
func T(s string) string {
	mu.RLock()
	lang := current
	mu.RUnlock()
	if lang == "en" {
		return s
	}
	if t, ok := catalogs[lang][s]; ok && t != "" {
		return t
	}
	return s
}

// Tf translates a format string, then formats it.
func Tf(format string, args ...any) string { return fmt.Sprintf(T(format), args...) }

// New returns a translated error.
func New(s string) error { return errors.New(T(s)) }

// Errorf returns a translated, formatted error (%w is supported).
func Errorf(format string, args ...any) error { return fmt.Errorf(T(format), args...) }
