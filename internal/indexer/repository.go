// Package indexer manages indexer instances (Cardigann definitions and
// generic Torznab feeds) and runs searches across them.
package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cinearr/internal/i18n"
	"cinearr/internal/indexer/cardigann"
	"cinearr/internal/proxy"
)

// Definitions are served by the Prowlarr project (same source as Prowlarr
// itself: IndexerDefinitionUpdateService, DEFINITION_VERSION = 12).
const (
	DefinitionsServer  = "https://indexers.prowlarr.com"
	DefinitionsBranch  = "master"
	DefinitionsVersion = 12
)

// Meta describes an available definition (CardigannMetaDefinition).
type Meta struct {
	ID          string                    `json:"id"`
	File        string                    `json:"file,omitempty"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Type        string                    `json:"type"`
	Language    string                    `json:"language"`
	Encoding    string                    `json:"encoding,omitempty"`
	Links       []string                  `json:"links"`
	Settings    []cardigann.SettingsField `json:"settings,omitempty"`
	Custom      bool                      `json:"custom,omitempty"`
}

// Repository downloads and caches definitions in dir.
type Repository struct {
	dir    string
	client *http.Client
	mu     sync.Mutex
	list   []Meta
	listAt time.Time
}

// NewRepository creates a repository caching into dir/definitions.
func NewRepository(dataDir string) *Repository {
	dir := filepath.Join(dataDir, "definitions")
	_ = os.MkdirAll(filepath.Join(dir, "custom"), 0o755)
	return &Repository{dir: dir, client: proxy.Client(proxy.Metadata, http.Client{Timeout: 30 * time.Second})}
}

// CustomDir is where users can drop their own .yml definitions.
func (r *Repository) CustomDir() string { return filepath.Join(r.dir, "custom") }

func (r *Repository) baseURL() string {
	return fmt.Sprintf("%s/%s/%d", DefinitionsServer, DefinitionsBranch, DefinitionsVersion)
}

func (r *Repository) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Cinearr")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, os.ErrNotExist
	}
	if resp.StatusCode != http.StatusOK {
		return nil, i18n.Errorf("definition server: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// List returns the available definitions (remote list, cached on disk,
// plus custom definitions).
func (r *Repository) List(ctx context.Context) ([]Meta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.list != nil && time.Since(r.listAt) < 6*time.Hour {
		return r.list, nil
	}
	cache := filepath.Join(r.dir, "list.json")
	var list []Meta
	data, err := r.get(ctx, r.baseURL())
	if err == nil {
		if jerr := json.Unmarshal(data, &list); jerr == nil {
			_ = os.WriteFile(cache, data, 0o644)
		} else {
			err = jerr
		}
	}
	if err != nil {
		cached, cerr := os.ReadFile(cache)
		if cerr != nil {
			return r.withCustom(nil), i18n.Errorf("list of indexers unavailable: %w", err)
		}
		_ = json.Unmarshal(cached, &list)
	}
	list = r.withCustom(list)
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	r.list, r.listAt = list, time.Now()
	return list, nil
}

func (r *Repository) withCustom(list []Meta) []Meta {
	files, _ := filepath.Glob(filepath.Join(r.CustomDir(), "*.yml"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		def, err := cardigann.ParseDefinition(data)
		if err != nil {
			continue
		}
		m := Meta{ID: def.ID, Name: def.Name + " (perso)", Description: def.Description, Type: def.Type,
			Language: def.Language, Links: def.Links, Settings: def.Settings, Custom: true}
		replaced := false
		for i := range list {
			if list[i].ID == def.ID {
				list[i] = m
				replaced = true
			}
		}
		if !replaced {
			list = append(list, m)
		}
	}
	return list
}

// Get returns a definition: custom file, fresh cache, remote, stale cache.
func (r *Repository) Get(ctx context.Context, id string) (*cardigann.Definition, error) {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return nil, i18n.New("invalid definition id")
	}
	if data, err := os.ReadFile(filepath.Join(r.CustomDir(), id+".yml")); err == nil {
		return cardigann.ParseDefinition(data)
	}
	cache := filepath.Join(r.dir, id+".yml")
	if st, err := os.Stat(cache); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		if data, err := os.ReadFile(cache); err == nil {
			if d, err := cardigann.ParseDefinition(data); err == nil {
				return d, nil
			}
		}
	}
	data, err := r.get(ctx, r.baseURL()+"/"+id)
	if err != nil {
		if cached, cerr := os.ReadFile(cache); cerr == nil {
			return cardigann.ParseDefinition(cached)
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, i18n.Errorf("the definition %q does not exist", id)
		}
		return nil, err
	}
	def, err := cardigann.ParseDefinition(data)
	if err != nil {
		return nil, i18n.Errorf("unreadable definition %s: %w", id, err)
	}
	_ = os.WriteFile(cache, data, 0o644)
	return def, nil
}
