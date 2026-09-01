package jobs

import (
	"os"
	"path/filepath"
	"testing"

	"notflix/server/library"
)

func TestBlurbLive(t *testing.T) {
	vid := os.Getenv("BLURB_LIVE")
	if vid == "" {
		t.Skip("set BLURB_LIVE=<path to mp4>")
	}

	sub := subFor(vid)
	if sub == "" {
		t.Fatal("no sidecar")
	}
	t.Logf("sidecar: %s", filepath.Base(sub))

	raw, err := os.ReadFile(sub)
	if err != nil {
		t.Fatal(err)
	}

	cues := parseCues(string(raw))
	text := opening(cues)
	t.Logf("cues=%d runtime=%.0fs opening=%d chars", len(cues), cues[len(cues)-1].End, len(text))
	t.Logf("last opening line: %q", text[max(0, len(text)-120):])

	content, err := blurbChat(blurbPrompt(library.CleanName(filepath.Base(vid)), text))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("raw: %s", content)

	out, err := parseBlurb(content)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("BLURB: %s", out)
}
