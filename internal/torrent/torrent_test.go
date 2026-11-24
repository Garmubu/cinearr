package torrent

import (
	"fmt"
	"strings"
	"testing"

	"cinearr/internal/i18n"
)

func build(name string, files map[string]int64) []byte {
	var b strings.Builder
	b.WriteString("d5:filesl")
	for p, s := range files {
		fmt.Fprintf(&b, "d6:lengthi%de4:pathl%d:%see", s, len(p), p)
	}
	fmt.Fprintf(&b, "e4:name%d:%s12:piece lengthi16384e6:pieces20:%s7:privatei1ee", len(name), name, strings.Repeat("a", 20))
	return []byte("d8:announce3:x/y4:info" + b.String() + "e")
}

func TestParseAndInspect(t *testing.T) {
	data := build("Movie.2020.1080p", map[string]int64{"Movie.2020.1080p.mkv": 4 << 30, "Movie.nfo": 100})
	info, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Private || info.Size != 4<<30+100 || len(info.InfoHash) != 40 || len(info.Files) != 2 {
		t.Fatalf("info %+v", info)
	}
	if r := Inspect(info.Files, nil); !r.OK || r.VideoFiles != 1 {
		t.Fatalf("clean torrent rejected: %+v", r)
	}
	bad := []struct {
		name  string
		files []File
	}{
		{"executable", []File{{"m/Movie.2020.1080p.mkv.exe", 3 << 20}}},
		{"shortcut", []File{{"m/Movie.mkv", 4 << 30}, {"m/Play.lnk", 900}}},
		{"archive only", []File{{"m/Movie.rar", 4 << 30}}},
		{"tiny video", []File{{"m/Movie.mkv", 10 << 20}}},
		{"script", []File{{"m/Movie.mkv", 4 << 30}, {"m/codec.vbs", 900}}},
	}
	for _, c := range bad {
		if r := Inspect(c.files, nil); r.OK {
			t.Errorf("%s accepted", c.name)
		}
	}
	// extensions refused by the user (Sonarr's rejected extensions)
	withISO := []File{{"m/Movie.mkv", 4 << 30}, {"m/Bonus.iso", 700 << 20}}
	if r := Inspect(withISO, nil); !r.OK {
		t.Fatalf("iso refused without the setting: %+v", r)
	}
	if r := Inspect(withISO, []string{".iso"}); r.OK || len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "Bonus.iso") {
		t.Fatalf("refused extension accepted: %+v", r)
	}
}

func TestMagnet(t *testing.T) {
	m, err := ParseMagnet("magnet:?xt=urn:btih:c9e15763f722f23e98a29decdfae341b98d53056&dn=Test")
	if err != nil || m.InfoHash != "C9E15763F722F23E98A29DECDFAE341B98D53056" || m.Name != "Test" {
		t.Fatalf("%+v %v", m, err)
	}
	b32, err := ParseMagnet("magnet:?xt=urn:btih:ZHUFOY7XELZD5GFCTXWN7LRUDOMNKMCW")
	if err != nil || len(b32.InfoHash) != 40 {
		t.Fatalf("base32: %+v %v", b32, err)
	}
	if _, err := ParseMagnet("http://example.com"); err == nil {
		t.Fatal("http accepted as magnet")
	}
	if l := BuildMagnet("c9e15763f722f23e98a29decdfae341b98d53056", "A B"); !strings.HasPrefix(l, "magnet:?xt=urn:btih:C9E15763") {
		t.Fatal(l)
	}
}

// The archive trap is named in every language of the interface.
func TestArchiveMessageTranslated(t *testing.T) {
	defer i18n.SetLanguage("en")
	for _, lang := range []string{"en", "fr", "es", "it", "de"} {
		i18n.SetLanguage(lang)
		r := Inspect([]File{{"m/Movie.rar", 4 << 30}}, nil)
		want := i18n.T("no video: the content is an archive (often a password trap)")
		if len(r.Problems) != 1 || r.Problems[0] != want {
			t.Errorf("%s: %v", lang, r.Problems)
		}
	}
}
