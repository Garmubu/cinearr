// Package cardigann ports Prowlarr's Cardigann engine: it runs the YAML
// indexer definitions published by the Prowlarr project (hundreds of public
// and private torrent sites) without site-specific code.
package cardigann

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"cinearr/internal/yaml"
)

// KV is an ordered string pair.
type KV struct {
	Key, Value string
}

// OrderedStrings is an ordered map[string]string (inputs, case, options).
type OrderedStrings []KV

// UnmarshalYAMLNode implements yaml.Unmarshaler.
func (o *OrderedStrings) UnmarshalYAMLNode(node any) error {
	m, ok := node.(*yaml.Map)
	if !ok {
		if node == nil {
			return nil
		}
		return fmt.Errorf("expected mapping")
	}
	for i, k := range m.Keys {
		v := ""
		if s, ok := m.Vals[i].(string); ok {
			v = s
		}
		*o = append(*o, KV{k, v})
	}
	return nil
}

// Get returns the value for key.
func (o OrderedStrings) Get(key string) (string, bool) {
	for _, kv := range o {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return "", false
}

// FieldList keeps search.fields in order, duplicates included.
type FieldList []FieldEntry

// FieldEntry is one search field.
type FieldEntry struct {
	Key   string
	Block SelectorBlock
}

// UnmarshalYAMLNode implements yaml.Unmarshaler.
func (f *FieldList) UnmarshalYAMLNode(node any) error {
	m, ok := node.(*yaml.Map)
	if !ok {
		return fmt.Errorf("fields: expected mapping")
	}
	for i, k := range m.Keys {
		var sb SelectorBlock
		if err := yaml.Decode(m.Vals[i], &sb); err != nil {
			return fmt.Errorf("field %s: %w", k, err)
		}
		*f = append(*f, FieldEntry{k, sb})
	}
	return nil
}

// HeaderMap accepts "name: value" or "name: [value]".
type HeaderMap map[string][]string

// UnmarshalYAMLNode implements yaml.Unmarshaler.
func (h *HeaderMap) UnmarshalYAMLNode(node any) error {
	m, ok := node.(*yaml.Map)
	if !ok {
		return nil
	}
	*h = HeaderMap{}
	for i, k := range m.Keys {
		switch v := m.Vals[i].(type) {
		case string:
			(*h)[k] = []string{v}
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					(*h)[k] = append((*h)[k], s)
				}
			}
		}
	}
	return nil
}

// Definition mirrors CardigannDefinition.
type Definition struct {
	ID              string
	Settings        []SettingsField
	Name            string
	Description     string
	Type            string
	Language        string
	Encoding        string
	RequestDelay    float64
	Links           []string
	Legacylinks     []string
	Followredirect  bool
	TestLinkTorrent *bool `yaml:"testlinktorrent"`
	Caps            CapabilitiesBlock
	Login           *LoginBlock
	Search          SearchBlock
	Download        *DownloadBlock
}

// SettingsField is a user setting declared by the definition.
type SettingsField struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Label    string         `json:"label"`
	Default  string         `json:"default"`
	Defaults []string       `json:"defaults,omitempty"`
	Options  OrderedStrings `json:"options,omitempty"`
}

// CategoryMapping maps a tracker category to a Newznab category.
type CategoryMapping struct {
	ID      string
	Cat     string
	Desc    string
	Default bool
}

// CapabilitiesBlock mirrors CapabilitiesBlock.
type CapabilitiesBlock struct {
	Categories       OrderedStrings
	Categorymappings []CategoryMapping
	Modes            map[string][]string
	Allowrawsearch   bool
}

// CaptchaBlock describes a login captcha (not supported interactively).
type CaptchaBlock struct {
	Type     string
	Selector string
	Input    string
}

// LoginBlock mirrors LoginBlock.
type LoginBlock struct {
	Path              string
	Submitpath        string
	Cookies           []string
	Method            string
	Form              string
	Selectors         bool
	Inputs            OrderedStrings
	Selectorinputs    map[string]SelectorBlock
	Getselectorinputs map[string]SelectorBlock
	Error             []ErrorBlock
	Test              *PageTestBlock
	Captcha           *CaptchaBlock
	Headers           HeaderMap
}

// ErrorBlock mirrors ErrorBlock.
type ErrorBlock struct {
	Path     string
	Selector string
	Message  *SelectorBlock
}

// SelectorBlock mirrors SelectorBlock.
type SelectorBlock struct {
	Selector  string
	Optional  bool
	Default   string
	Text      *string
	Attribute string
	Remove    string
	Filters   []FilterBlock
	Case      OrderedStrings
}

// FilterBlock mirrors FilterBlock; Args is a string or a list.
type FilterBlock struct {
	Name string
	Args any
}

// ArgString returns Args as a string (or "").
func (f FilterBlock) ArgString() string {
	switch v := f.Args.(type) {
	case string:
		return v
	case []any:
		if len(v) > 0 {
			s, _ := v[0].(string)
			return s
		}
	}
	return ""
}

// ArgList returns Args as a list of strings.
func (f FilterBlock) ArgList() []string {
	switch v := f.Args.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, len(v))
		for i, x := range v {
			s, _ := x.(string)
			out[i] = s
		}
		return out
	}
	return nil
}

// PageTestBlock mirrors PageTestBlock.
type PageTestBlock struct {
	Path     string
	Selector string
}

// SearchBlock mirrors SearchBlock.
type SearchBlock struct {
	Path                 string
	Paths                []SearchPathBlock
	Headers              HeaderMap
	Keywordsfilters      []FilterBlock
	AllowEmptyInputs     bool
	Inputs               OrderedStrings
	Error                []ErrorBlock
	Preprocessingfilters []FilterBlock
	Rows                 RowsBlock
	Fields               FieldList
}

// RowsBlock mirrors RowsBlock.
type RowsBlock struct {
	Selector                        string
	Optional                        bool
	Attribute                       string
	Remove                          string
	Filters                         []FilterBlock
	After                           int
	Dateheaders                     *SelectorBlock
	Count                           *SelectorBlock
	Multiple                        bool
	MissingAttributeEqualsNoResults bool
}

// SearchPathBlock mirrors SearchPathBlock.
type SearchPathBlock struct {
	Path           string
	Method         string
	Inputs         OrderedStrings
	Queryseparator string
	Categories     []string
	Inheritinputs  *bool
	Followredirect bool
	Response       *ResponseBlock
}

// ResponseBlock mirrors ResponseBlock.
type ResponseBlock struct {
	Type             string
	NoResultsMessage string
}

// RequestBlock mirrors RequestBlock.
type RequestBlock struct {
	Path           string
	Method         string
	Inputs         OrderedStrings
	Queryseparator string
}

// SelectorField mirrors SelectorField.
type SelectorField struct {
	Selector          string
	Attribute         string
	Usebeforeresponse bool
	Filters           []FilterBlock
}

// BeforeBlock mirrors BeforeBlock.
type BeforeBlock struct {
	Path         string
	Method       string
	Inputs       OrderedStrings
	Pathselector *SelectorField
}

// InfohashBlock mirrors InfohashBlock.
type InfohashBlock struct {
	Hash              *SelectorField
	Title             *SelectorField
	Usebeforeresponse bool
}

// DownloadBlock mirrors DownloadBlock.
type DownloadBlock struct {
	Selectors []SelectorField
	Method    string
	Before    *BeforeBlock
	Infohash  *InfohashBlock
	Headers   HeaderMap
}

// ParseDefinition decodes a YAML definition and applies Prowlarr's cleanup
// (default credentials settings, default encoding, single path).
func ParseDefinition(data []byte) (*Definition, error) {
	var d Definition
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	if d.ID == "" {
		return nil, fmt.Errorf("definition has no id")
	}
	if len(d.Links) == 0 {
		return nil, fmt.Errorf("definition %s has no links", d.ID)
	}
	if d.Settings == nil {
		d.Settings = []SettingsField{
			{Name: "username", Label: "Username", Type: "text"},
			{Name: "password", Label: "Password", Type: "password"},
		}
	}
	if d.Encoding == "" {
		d.Encoding = "UTF-8"
	}
	if d.Search.Path != "" && len(d.Search.Paths) == 0 {
		d.Search.Paths = []SearchPathBlock{{Path: d.Search.Path}}
	}
	for i, l := range d.Links {
		if !strings.HasSuffix(l, "/") {
			d.Links[i] = l + "/"
		}
	}
	return &d, nil
}

// SupportsImdbMovieSearch reports whether movie-search accepts imdbid.
func (d *Definition) SupportsImdbMovieSearch() bool {
	for _, m := range d.Caps.Modes["movie-search"] {
		if m == "imdbid" {
			return true
		}
	}
	return false
}

// SupportsTVSearch reports whether the site has a series search mode and
// which ids it accepts (imdbid, tvdbid, tmdbid).
func (d *Definition) SupportsTVSearch(param string) bool {
	for _, m := range d.Caps.Modes["tv-search"] {
		if m == param {
			return true
		}
	}
	return false
}

// UnmarshalJSON keeps the key order of a JSON object.
func (o *OrderedStrings) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		if tok == nil {
			return nil
		}
		return fmt.Errorf("expected object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var v any
		if err := dec.Decode(&v); err != nil {
			return err
		}
		s, _ := v.(string)
		if s == "" && v != nil {
			s = fmt.Sprint(v)
		}
		*o = append(*o, KV{fmt.Sprint(kt), s})
	}
	return nil
}

// MarshalJSON writes the pairs as an object (order preserved).
func (o OrderedStrings) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(kv.Key)
		v, _ := json.Marshal(kv.Value)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
