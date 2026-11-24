// Package torrent reads .torrent files (bencode), handles magnet links and
// performs the safety checks applied before a release reaches the torrent
// client (Radarr's TorrentFileInfoReader, MagnetLinkBuilder and the
// dangerous / executable file rules).
package torrent

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"strconv"
	"strings"

	"cinearr/internal/i18n"
	"cinearr/internal/quality"
)

// ---------------------------------------------------------------------------
// Bencode

type decoder struct {
	b   []byte
	i   int
	dep int
}

var errBencode = errors.New("invalid bencode")

func (d *decoder) value() (any, error) {
	if d.i >= len(d.b) {
		return nil, errBencode
	}
	d.dep++
	defer func() { d.dep-- }()
	if d.dep > 64 {
		return nil, errBencode
	}
	switch c := d.b[d.i]; {
	case c == 'i':
		end := indexByteFrom(d.b, 'e', d.i)
		if end < 0 {
			return nil, errBencode
		}
		n, err := strconv.ParseInt(string(d.b[d.i+1:end]), 10, 64)
		if err != nil {
			return nil, errBencode
		}
		d.i = end + 1
		return n, nil
	case c == 'l':
		d.i++
		var list []any
		for d.i < len(d.b) && d.b[d.i] != 'e' {
			v, err := d.value()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		if d.i >= len(d.b) {
			return nil, errBencode
		}
		d.i++
		return list, nil
	case c == 'd':
		d.i++
		m := map[string]any{}
		for d.i < len(d.b) && d.b[d.i] != 'e' {
			k, err := d.value()
			if err != nil {
				return nil, err
			}
			ks, ok := k.([]byte)
			if !ok {
				return nil, errBencode
			}
			start := d.i
			v, err := d.value()
			if err != nil {
				return nil, err
			}
			m[string(ks)] = v
			if string(ks) == "info" {
				m["\x00info-raw"] = d.b[start:d.i]
			}
		}
		if d.i >= len(d.b) {
			return nil, errBencode
		}
		d.i++
		return m, nil
	case c >= '0' && c <= '9':
		colon := indexByteFrom(d.b, ':', d.i)
		if colon < 0 {
			return nil, errBencode
		}
		n, err := strconv.Atoi(string(d.b[d.i:colon]))
		if err != nil || n < 0 || colon+1+n > len(d.b) {
			return nil, errBencode
		}
		s := d.b[colon+1 : colon+1+n]
		d.i = colon + 1 + n
		return s, nil
	}
	return nil, errBencode
}

func indexByteFrom(b []byte, c byte, from int) int {
	for i := from; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// File is one file inside a torrent.
type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Info describes a .torrent.
type Info struct {
	InfoHash string `json:"infoHash"` // v1, upper-case hex
	Name     string `json:"name"`
	Files    []File `json:"files"`
	Size     int64  `json:"size"`
	Private  bool   `json:"private"`
	Trackers []string
}

// Parse decodes a .torrent file.
func Parse(data []byte) (*Info, error) {
	if len(data) == 0 || data[0] != 'd' {
		return nil, i18n.New("this is not a .torrent file")
	}
	d := &decoder{b: data}
	v, err := d.value()
	if err != nil {
		return nil, err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return nil, errBencode
	}
	info, ok := root["info"].(map[string]any)
	raw, _ := root["\x00info-raw"].([]byte)
	if !ok || raw == nil {
		return nil, errors.New("dictionnaire info absent")
	}
	sum := sha1.Sum(raw)
	ti := &Info{InfoHash: strings.ToUpper(hex.EncodeToString(sum[:]))}
	ti.Name = str(info["name.utf-8"])
	if ti.Name == "" {
		ti.Name = str(info["name"])
	}
	if p, ok := info["private"].(int64); ok && p == 1 {
		ti.Private = true
	}
	if files, ok := info["files"].([]any); ok {
		for _, f := range files {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			var parts []string
			pl, _ := fm["path.utf-8"].([]any)
			if pl == nil {
				pl, _ = fm["path"].([]any)
			}
			for _, p := range pl {
				parts = append(parts, str(p))
			}
			size, _ := fm["length"].(int64)
			ti.Files = append(ti.Files, File{Path: path.Join(append([]string{ti.Name}, parts...)...), Size: size})
			ti.Size += size
		}
	} else if tree, ok := info["file tree"].(map[string]any); ok { // BitTorrent v2 only
		walkTree(tree, ti.Name, ti)
	} else {
		size, _ := info["length"].(int64)
		ti.Files = []File{{Path: ti.Name, Size: size}}
		ti.Size = size
	}
	if a := str(root["announce"]); a != "" {
		ti.Trackers = append(ti.Trackers, a)
	}
	if al, ok := root["announce-list"].([]any); ok {
		for _, tier := range al {
			if tl, ok := tier.([]any); ok {
				for _, t := range tl {
					ti.Trackers = append(ti.Trackers, str(t))
				}
			}
		}
	}
	return ti, nil
}

func walkTree(tree map[string]any, prefix string, ti *Info) {
	for name, v := range tree {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if leaf, ok := m[""].(map[string]any); ok {
			size, _ := leaf["length"].(int64)
			ti.Files = append(ti.Files, File{Path: path.Join(prefix, name), Size: size})
			ti.Size += size
			continue
		}
		walkTree(m, path.Join(prefix, name), ti)
	}
}

func str(v any) string {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Magnet links

// PublicTrackers is MagnetLinkBuilder.Trackers (Prowlarr).
var PublicTrackers = []string{
	"http://tracker.opentrackr.org:1337/announce",
	"udp://tracker.auctor.tv:6969/announce",
	"udp://opentracker.i2p.rocks:6969/announce",
	"https://opentracker.i2p.rocks:443/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
	"http://tracker.openbittorrent.com:80/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://tracker.moeking.me:6969/announce",
	"udp://explodie.org:6969/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://uploads.gamecoast.net:6969/announce",
	"udp://tracker1.bt.moack.co.kr:80/announce",
	"udp://tracker.tiny-vps.com:6969/announce",
	"udp://tracker.theoks.net:6969/announce",
	"udp://tracker.skyts.net:6969/announce",
	"udp://tracker-udp.gbitt.info:80/announce",
	"udp://open.tracker.ink:6969/announce",
	"udp://movies.zsw.ca:6969/announce",
}

// Magnet is a parsed magnet link.
type Magnet struct {
	InfoHash string // upper-case hex (v1)
	Name     string
	Trackers []string
}

// NormalizeHash returns an upper-case 40-char hex hash (accepts base32).
func NormalizeHash(h string) (string, error) {
	h = strings.TrimSpace(h)
	switch len(h) {
	case 40:
		if _, err := hex.DecodeString(h); err != nil {
			return "", i18n.New("invalid hash")
		}
		return strings.ToUpper(h), nil
	case 32:
		b, err := base32.StdEncoding.DecodeString(strings.ToUpper(h))
		if err != nil {
			return "", i18n.New("invalid base32 hash")
		}
		return strings.ToUpper(hex.EncodeToString(b)), nil
	}
	return "", i18n.Errorf("hash of unexpected length (%d)", len(h))
}

// ParseMagnet validates a magnet link and extracts its v1 info hash.
func ParseMagnet(link string) (*Magnet, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Scheme != "magnet" {
		return nil, i18n.New("invalid magnet link")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, i18n.New("invalid magnet link")
	}
	m := &Magnet{Name: q.Get("dn"), Trackers: q["tr"]}
	for _, xt := range q["xt"] {
		if strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
			h, err := NormalizeHash(xt[9:])
			if err != nil {
				return nil, err
			}
			m.InfoHash = h
			break
		}
	}
	if m.InfoHash == "" {
		return nil, i18n.New("magnet link without a BitTorrent v1 hash")
	}
	return m, nil
}

// BuildMagnet ports MagnetLinkBuilder.BuildPublicMagnetLink.
func BuildMagnet(hash, title string) string {
	h, err := NormalizeHash(hash)
	if err != nil {
		h = strings.ToUpper(hash)
	}
	v := "magnet:?xt=urn:btih:" + h
	if title != "" {
		v += "&dn=" + url.QueryEscape(title)
	}
	for _, t := range PublicTrackers {
		v += "&tr=" + url.QueryEscape(t)
	}
	return v
}

// ---------------------------------------------------------------------------
// Safety checks

// Report is the result of inspecting a torrent's file list.
type Report struct {
	OK          bool     `json:"ok"`
	Problems    []string `json:"problems"`
	Warnings    []string `json:"warnings"`
	VideoFiles  int      `json:"videoFiles"`
	LargestFile string   `json:"largestFile"`
}

// Inspect applies Radarr's dangerous/executable/archive rules to a file list
// and adds checks against common fakes (no video, tiny video, video
// "sample" only, double extensions such as movie.mkv.exe). rejected are
// the extensions refused by the user (".iso").
func Inspect(files []File, rejected []string) Report {
	r := Report{OK: true}
	var largest int64
	var videoBytes, total int64
	hasArchive := false
	for _, f := range files {
		total += f.Size
		name := path.Base(f.Path)
		lower := strings.ToLower(name)
		risk := quality.FileRisk(name, rejected)
		switch risk {
		case "dangerous":
			r.Problems = append(r.Problems, i18n.Tf("potentially dangerous file: %s", name))
		case "executable":
			r.Problems = append(r.Problems, i18n.Tf("executable file: %s", name))
		case "rejected":
			r.Problems = append(r.Problems, i18n.Tf("file with an extension refused in the settings: %s", name))
		case "archive":
			hasArchive = true
			r.Warnings = append(r.Warnings, i18n.Tf("archive to extract: %s", name))
		}
		// double extension "film.mkv.exe" / "film.mp4.lnk"
		if parts := strings.Split(lower, "."); len(parts) >= 3 {
			inner := "." + parts[len(parts)-2]
			if quality.IsMediaExtension(inner) && risk != "" && risk != "archive" {
				r.Problems = append(r.Problems, i18n.Tf("misleading double extension: %s", name))
			}
		}
		if quality.IsMediaExtension(path.Ext(lower)) && !strings.Contains(lower, "sample") {
			r.VideoFiles++
			videoBytes += f.Size
			if f.Size > largest {
				largest = f.Size
				r.LargestFile = f.Path
			}
		}
	}
	if len(files) > 0 && r.VideoFiles == 0 {
		// decided on the files, not on the translated warnings
		if hasArchive {
			r.Problems = append(r.Problems, i18n.T("no video: the content is an archive (often a password trap)"))
		} else {
			r.Problems = append(r.Problems, i18n.T("no video file in the torrent"))
		}
	}
	if r.VideoFiles > 0 && largest < 50<<20 {
		r.Problems = append(r.Problems, i18n.T("the main video file is smaller than 50 MB: probably a fake"))
	}
	if total > 0 && videoBytes > 0 && float64(videoBytes)/float64(total) < 0.5 {
		r.Warnings = append(r.Warnings, i18n.T("less than half of the content is video"))
	}
	r.OK = len(r.Problems) == 0
	return r
}
