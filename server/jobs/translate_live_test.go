package jobs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var hangul = regexp.MustCompile(`[\x{ac00}-\x{d7a3}]`)

// Feeds the real cues that batch 12 of The Roundup: Punishment failed on
// straight back through TranslateSegments. Those 50 lines translated fine the
// first time — the model answered 51 of 50 and the parser binned all of them —
// so this asserts the only thing that was ever broken: that a batch survives
// the model miscounting.
//
//	TRANSLATE_LIVE=/path/to/file.whisper.vtt go test ./server/jobs/ -run Live -v
func TestTranslateLive(t *testing.T) {
	path := os.Getenv("TRANSLATE_LIVE")
	if path == "" {
		t.Skip("set TRANSLATE_LIVE=<path to a .vtt with untranslated cues>")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var src []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "-->") || line == "WEBVTT" {
			continue
		}
		if hangul.MatchString(line) {
			src = append(src, line)
		}
		if len(src) == 50 {
			break
		}
	}
	if len(src) < 10 {
		t.Skipf("only %d untranslated cues in %s", len(src), path)
	}

	out, err := TranslateSegments(src, "ko", nil)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}

	left := 0
	for i, s := range out {
		if hangul.MatchString(s) {
			left++
			t.Logf("untranslated [%d] %q", i, s)
			continue
		}
		if i < 5 {
			t.Logf("%q → %q", src[i], s)
		}
	}

	// Before the index-keyed format a single miscount left all 50 in Korean.
	if left > len(src)/10 {
		t.Errorf("%d of %d cues still Korean", left, len(src))
	}
	t.Logf("translated %d of %d", len(src)-left, len(src))
}

// Repairs one real sidecar in place and reports what it fixed. This is also the
// way to run the pass on a single file instead of the whole library:
//
//	RETRANSLATE_LIVE=/path/to/file.whisper.vtt go test ./server/jobs/ -run Retranslate -v
func TestRetranslateLive(t *testing.T) {
	path := os.Getenv("RETRANSLATE_LIVE")
	if path == "" {
		t.Skip("set RETRANSLATE_LIVE=<path to a .whisper.vtt>")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := UntranslatedCues(string(raw))
	t.Logf("%d untranslated cues before", before)

	fixed, err := RetranslateVTT(path)
	if err != nil {
		t.Fatalf("retranslate: %v", err)
	}

	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after := UntranslatedCues(string(raw))
	t.Logf("fixed %d, %d untranslated cues left", fixed, after)

	if before > 0 && after >= before {
		t.Errorf("no improvement: %d → %d", before, after)
	}
}
