package subtitles

import "testing"

func TestCheck(t *testing.T) {
	ok := []struct{ name, data, ext, want string }{
		{"srt", "1\r\n00:00:01,000 --> 00:00:02,500\r\nBonjour\r\n", ".srt", ".srt"},
		{"srt with BOM", "\xEF\xBB\xBF1\n00:00:01,000 --> 00:00:02,500\nHi\n", ".srt", ".srt"},
		{"vtt", "WEBVTT\n\n00:01.000 --> 00:02.000\nHi\n", ".srt", ".vtt"},
		{"ass sent as srt", "[Script Info]\nScriptType: v4.00+\n[V4+ Styles]\n[Events]\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,Hi\n", ".srt", ".ass"},
		{"ssa", "[Script Info]\n[Events]\nDialogue: Marked=0,0:00:01.00,0:00:02.00,Default,,0,0,0,,Hi\n", ".ssa", ".ssa"},
		{"microdvd", "{1}{1}25.000\n{100}{200}Hi\n", ".sub", ".sub"},
	}
	for _, c := range ok {
		got, err := Check([]byte(c.data), c.ext)
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v", c.name, got, err)
		}
	}
	bad := map[string]string{
		"empty":    "  \n ",
		"web page": "<!DOCTYPE html><html><head><title>Just a moment...</title></head><body>captcha</body></html>",
		"text":     "This subtitle was removed at the request of the copyright holder.",
		"binary":   "\x00\x00\x01\xBA\x44\x00",
		"zeros":    "1\n00:00:01,000 --> 00:00:02,000\n\x00\x00",
	}
	for name, data := range bad {
		if ext, err := Check([]byte(data), ".srt"); err == nil {
			t.Errorf("%s accepted as %s", name, ext)
		}
	}
}

func TestNormalizeLanguage(t *testing.T) {
	for in, want := range map[string]string{"FR": "fr", " pt_BR ": "pt-br", "zh-Hans": "zh-hans", "fre": "fre", "": "und"} {
		if got, err := NormalizeLanguage(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"../../x", "fr/../../x", `..\x`, "fr.srt", "french", "f", "fr-", "fr x"} {
		if got, err := NormalizeLanguage(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
}
