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
