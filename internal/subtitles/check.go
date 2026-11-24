package subtitles

import (
	"bytes"
	"regexp"

	"cinearr/internal/i18n"
)

// Bazarr parses every subtitle before keeping it: a web page (captcha,
// login, error), an empty file or a file without any timed line is not a
// subtitle. Check does the same for the text formats Cinearr keeps and
// gives the extension that matches the content (a provider may send an
// ASS file as ".srt").

var (
	srtTiming = regexp.MustCompile(`(?m)^[ \t]*\d{1,2}:\d{2}:\d{2}[,.]\d{1,3}[ \t]*-->[ \t]*\d{1,2}:\d{2}:\d{2}[,.]\d{1,3}`)
	vttTiming = regexp.MustCompile(`(?m)^[ \t]*(?:\d{1,2}:)?\d{2}:\d{2}\.\d{3}[ \t]*-->[ \t]*(?:\d{1,2}:)?\d{2}:\d{2}\.\d{3}`)
	assEvent  = regexp.MustCompile(`(?mi)^[ \t]*Dialogue:[ \t]*`)
	microDVD  = regexp.MustCompile(`(?m)^[ \t]*\{\d+\}\{\d*\}`)
	subViewer = regexp.MustCompile(`(?m)^[ \t]*\d{2}:\d{2}:\d{2}\.\d{2},\d{2}:\d{2}:\d{2}\.\d{2}`)
	webPage   = regexp.MustCompile(`(?i)<!doctype html|<html[\s>]|<head[\s>]|<body[\s>]`)
)

// Check verifies that data (UTF-8) is a usable subtitle and returns the
// extension of its format. ext is the extension announced by the source,
// kept when it matches the content.
func Check(data []byte, ext string) (string, error) {
	text := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	if len(text) == 0 {
		return "", i18n.New("empty subtitle file")
	}
	if bytes.HasPrefix(data, []byte{0x00, 0x00, 0x01, 0xBA}) {
		return "", i18n.New("image subtitles (VobSub) without their .idx file cannot be used")
	}
	head := text
	if len(head) > 4096 {
		head = head[:4096]
	}
	if webPage.Match(head) {
		return "", i18n.New("the file is a web page, not a subtitle (the site may ask for a captcha or a login)")
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return "", i18n.New("not a subtitle: binary content")
	}
	switch {
	case bytes.HasPrefix(text, []byte("WEBVTT")) && vttTiming.Match(text):
		return ".vtt", nil
	case assEvent.Match(text) && (bytes.Contains(bytes.ToLower(text), []byte("[events]")) || bytes.Contains(bytes.ToLower(text), []byte("[script info]"))):
		if ext == ".ass" || ext == ".ssa" {
			return ext, nil
		}
		if bytes.Contains(bytes.ToLower(text), []byte("[v4+ styles]")) {
			return ".ass", nil
		}
		return ".ssa", nil
	case srtTiming.Match(text):
		return ".srt", nil
	case microDVD.Match(text) || subViewer.Match(text):
		return ".sub", nil
	}
	return "", i18n.New("not a subtitle: no timed line found")
}
