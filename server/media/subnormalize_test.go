package media

import (
	"strings"
	"testing"
)

// Regression: martinlindhe/subtitles chokes on a UTF-8 BOM at the header line
// (`strconv.Atoi: parsing "<BOM>1"`).
func TestParseSRT_StripsBOM(t *testing.T) {
	const bom = "\ufeff"
	body := "1\n00:00:01,000 --> 00:00:02,000\nhello\n"

	if _, err := parseSRT(bom + body); err != nil {
		t.Errorf("parseSRT must accept BOM-prefixed SRT, got %v", err)
	}
	if _, err := parseSRT(body); err != nil {
		t.Errorf("parseSRT must accept plain SRT, got %v", err)
	}
}

// Regression (2026-05-20): a NUL-padded `.srt` reached strconv.Atoi, whose
// NumError echoed the raw NULs into the log (1000s of rows). Bail early.
func TestParseSRT_RejectsBinaryWithTidyError(t *testing.T) {
	nullPad := strings.Repeat("\x00", 4096) + "anything"
	_, err := parseSRT(nullPad)
	if err == nil {
		t.Fatal("parseSRT must reject NUL-padded input")
	}
	msg := err.Error()
	if strings.ContainsRune(msg, 0) {
		t.Errorf("error must not contain raw NULs (would flood the log), got %q", msg)
	}
	if len(msg) > 200 {
		t.Errorf("error must be tidy/short, got %d chars: %q", len(msg), msg)
	}
	if !strings.Contains(msg, "not an SRT") {
		t.Errorf("error must explain why, got %q", msg)
	}

	if _, err := parseVTT(nullPad); err == nil || strings.ContainsRune(err.Error(), 0) {
		t.Errorf("parseVTT must reject binary with a NUL-free error, got %v", err)
	}
}

// Three Pirates .srt sidecars in this library are UTF-16 LE; parseSRT rejected
// them on the 0xff BOM byte as "likely binary" and the subtitle never existed.
func TestParseSRTUTF16(t *testing.T) {
	body := "1\r\n00:00:05,000 --> 00:00:15,000\r\nHello world\r\n\r\n"

	enc := func(bom []byte, le bool) string {
		out := append([]byte{}, bom...)
		for _, r := range body {
			if le {
				out = append(out, byte(r), byte(r>>8))
			} else {
				out = append(out, byte(r>>8), byte(r))
			}
		}
		return string(out)
	}

	for _, tc := range []struct {
		name string
		in   string
	}{
		{"utf-16 le bom", enc([]byte{0xff, 0xfe}, true)},
		{"utf-16 be bom", enc([]byte{0xfe, 0xff}, false)},
		{"utf-8 bom still works", "\ufeff" + body},
		{"plain utf-8 still works", body},
	} {
		got, err := parseSRT(tc.in)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if len(got.Captions) != 1 || len(got.Captions[0].Text) == 0 ||
			got.Captions[0].Text[0] != "Hello world" {
			t.Errorf("%s: got %+v", tc.name, got.Captions)
		}
	}

	// A genuinely binary sidecar must still be refused, not decoded into mojibake.
	if _, err := parseSRT("\x00\x01\x02\x03binary"); err == nil {
		t.Error("binary input accepted")
	}
}

// The bug these pin: a provider payload that failed to parse used to be written
// to disk verbatim as the sidecar, so an API error object rendered on screen as
// raw JSON and nothing retried, because the file now existed.
func TestAsVTTRejectsNonSubtitles(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"opensubtitles error object", `{"status":406,"message":"download limit reached"}`, "JSON"},
		{"json array", `[{"id":1}]`, "JSON"},
		{"html error page", "<!DOCTYPE html><html><body>403 Forbidden</body></html>", "markup"},
		{"zip archive", "PK\x03\x04\x14\x00\x00\x00", "zip"},
		{"empty", "   \n\n ", "empty"},
		{"prose with no timings", "just some text that is not a subtitle at all", "unrecognised"},
	}

	for _, tc := range cases {
		got, err := AsVTT([]byte(tc.in))
		if err == nil {
			t.Errorf("%s: accepted, returned %q", tc.name, got)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.want)
		}
		if got != "" {
			t.Errorf("%s: returned content alongside an error: %q", tc.name, got)
		}
	}
}

func TestAsVTTAcceptsRealSubtitles(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"srt", "1\n00:00:01,000 --> 00:00:02,000\nHello world\n"},
		{"srt with bom", "\xef\xbb\xbf1\n00:00:01,000 --> 00:00:02,000\nHello world\n"},
		{"vtt", "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello world\n"},
		{"vtt without hours", "WEBVTT\n\n00:01.000 --> 00:02.000\nHello world\n"},
	}

	for _, tc := range cases {
		got, err := AsVTT([]byte(tc.in))
		if err != nil {
			t.Errorf("%s: rejected: %v", tc.name, err)
			continue
		}
		if !strings.HasPrefix(got, "WEBVTT") {
			t.Errorf("%s: output is not VTT: %.40q", tc.name, got)
		}
		if !cueTiming.MatchString(got) {
			t.Errorf("%s: output carries no cue timing: %.80q", tc.name, got)
		}
	}
}

// An SRT index line is a cue identifier, never cue text — leaking it is the
// "subtitles are just numbers" symptom.
func TestAsVTTDropsSRTIndexLines(t *testing.T) {
	got, err := AsVTT([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n\n2\n00:00:03,000 --> 00:00:04,000\nWorld\n"))
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}

	for _, line := range strings.Split(got, "\n") {
		if s := strings.TrimSpace(line); s == "1" || s == "2" {
			t.Errorf("index line survived into the cue body:\n%s", got)
			break
		}
	}
}
