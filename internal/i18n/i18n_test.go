package i18n

import (
	"regexp"
	"sort"
	"testing"
)

var verb = regexp.MustCompile(`%[-+#0]*[0-9]*(\.[0-9]+)?[a-zA-Z%]`)

// Every translation keeps the format verbs of its English text, in order.
func TestCatalogVerbs(t *testing.T) {
	for _, l := range Languages[1:] {
		for k, v := range Catalog(l.Code) {
			a, b := verb.FindAllString(k, -1), verb.FindAllString(v, -1)
			if len(a) != len(b) {
				t.Errorf("%s: %q -> %q: verbs %v vs %v", l.Code, k, v, a, b)
				continue
			}
			for i := range a {
				if a[i] != b[i] {
					t.Errorf("%s: %q -> %q: verbs %v vs %v", l.Code, k, v, a, b)
				}
			}
		}
	}
}

// Placeholders of the interface ({name}) are kept too.
func TestCatalogPlaceholders(t *testing.T) {
	ph := regexp.MustCompile(`\{[a-zA-Z0-9]+\}`)
	for _, l := range Languages[1:] {
		for k, v := range Catalog(l.Code) {
			a, b := ph.FindAllString(k, -1), ph.FindAllString(v, -1)
			sort.Strings(a)
			sort.Strings(b)
			if len(a) != len(b) {
				t.Errorf("%s: %q -> %q: placeholders %v vs %v", l.Code, k, v, a, b)
				continue
			}
			for i := range a {
				if a[i] != b[i] {
					t.Errorf("%s: %q -> %q: placeholders %v vs %v", l.Code, k, v, a, b)
				}
			}
		}
	}
}

func TestFallback(t *testing.T) {
	catalogs["fr"]["Hello %s"] = "Bonjour %s"
	defer delete(catalogs["fr"], "Hello %s")
	SetLanguage("fr-FR")
	defer SetLanguage("en")
	if got := Tf("Hello %s", "Garm"); got != "Bonjour Garm" {
		t.Fatalf("got %q", got)
	}
	if got := T("Untranslated"); got != "Untranslated" {
		t.Fatalf("got %q", got)
	}
}
