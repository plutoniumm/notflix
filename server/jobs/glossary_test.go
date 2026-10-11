package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProperNounsUsesMidSentencePosition(t *testing.T) {
	// "Ryo" and "Kaori" appear mid-sentence; "Wait"/"Listen" only ever open one,
	// so position alone separates names from grammar — no dictionary.
	text := strings.Join([]string{
		"Wait, that's Ryo over there.",
		"Listen to me, Kaori.",
		"Wait here. Ryo will handle it.",
		"Kaori, are you hurt?",
		"Listen! Something's wrong.",
		"Once is not enough, Saeko.", // single appearance -> excluded
	}, "\n")

	got := properNouns(text, 24)
	joined := strings.Join(got, ",")

	for _, want := range []string{"Ryo", "Kaori"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in %v", want, got)
		}
	}
	for _, bad := range []string{"Wait", "Listen", "Once", "Saeko", "Something"} {
		if strings.Contains(joined, bad) {
			t.Errorf("did not expect %q in %v", bad, got)
		}
	}
	if len(got) > 0 && got[0] != "Ryo" && got[0] != "Kaori" {
		t.Errorf("most frequent name should rank first, got %v", got)
	}
}

func TestShowGlossaryReadsASiblingNotItself(t *testing.T) {
	dir := t.TempDir()
	write := func(n, body string) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Bracketed names are the case filepath.Glob silently returns nothing for.
	write("Show [1987] - 05.mp4", "x")
	write("Show [1987] - 05.vtt", "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nOnly Mine appears here twice. Mine again.\n")
	write("Show [1987] - 06.vtt", "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nSo Ryo said hi. Then Ryo left.\n")
	write("Show [1987] - 07.vtt", "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nこれは日本語です。日本語。\n")

	got := strings.Join(ShowGlossary(filepath.Join(dir, "Show [1987] - 05.mp4")), ",")
	if !strings.Contains(got, "Ryo") {
		t.Errorf("expected the sibling's names, got %q", got)
	}
	if strings.Contains(got, "Mine") {
		t.Errorf("must not read the file's own sidecar, got %q", got)
	}

	// No sibling at all -> empty, and translation proceeds without a glossary.
	only := t.TempDir()
	if err := os.WriteFile(filepath.Join(only, "a.mp4"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if g := ShowGlossary(filepath.Join(only, "a.mp4")); len(g) != 0 {
		t.Errorf("expected no glossary, got %v", g)
	}
}
