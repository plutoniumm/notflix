package library

import (
	"os"
	"path/filepath"
	"testing"
)

// The real stranding: conversion renames the video to CleanName(src) + ".mp4"
// and the release's own subtitles stay on the old stem. Names taken from files
// actually found orphaned in the library.
func TestMoveSidecars_AcrossConversionRename(t *testing.T) {
	dir := t.TempDir()
	src := "Breaking.Bad.S02E01.1080p.BluRay.ReEnc-DeeJayAhmed"
	dst := CleanName(src + ".mkv")

	subs := []string{".vtt", ".eng.vtt", ".srt", ".por2.vtt", ".whisper.vtt"}
	for _, s := range subs {
		if err := os.WriteFile(filepath.Join(dir, src+s), []byte("WEBVTT\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A neighbour that must not be dragged along.
	other := filepath.Join(dir, "Some Other Film.eng.vtt")
	os.WriteFile(other, []byte("WEBVTT\n"), 0o644)

	n := MoveSidecars(filepath.Join(dir, src), filepath.Join(dir, dst))
	if n != len(subs) {
		t.Errorf("moved %d sidecars, want %d", n, len(subs))
	}
	for _, s := range subs {
		if _, err := os.Stat(filepath.Join(dir, dst+s)); err != nil {
			t.Errorf("%q did not arrive on the new stem: %v", dst+s, err)
		}
		if _, err := os.Stat(filepath.Join(dir, src+s)); err == nil {
			t.Errorf("%q still sits on the old stem", src+s)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("an unrelated sidecar was moved: %v", err)
	}
}

// filepath.Glob would return nothing with a nil error on either of these.
func TestMoveSidecars_BracketedReleaseNames(t *testing.T) {
	for _, stem := range []string{
		"Killers.Of.The.Flower.Moon.2023.1080p.WEBRip.x265.10bit.AAC5.1-[YTS.MX]",
		"[Judas] Bleach - Thousand Year Blood War - S17E41",
		"Minions.Monsters.2026.1080p.WEBRip.x264.AAC5.1-[YTS.GG - YTS.BZ]",
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, stem+".eng.vtt"), []byte("WEBVTT\n"), 0o644)

		if n := MoveSidecars(filepath.Join(dir, stem), filepath.Join(dir, "Clean Name")); n != 1 {
			t.Errorf("%q: moved %d, want 1", stem, n)
		}
		if _, err := os.Stat(filepath.Join(dir, "Clean Name.eng.vtt")); err != nil {
			t.Errorf("%q: %v", stem, err)
		}
	}
}

func TestMoveSidecars_NeverClobbersAndNoopsOnSameStem(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.eng.vtt"), []byte("old\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.eng.vtt"), []byte("keep\n"), 0o644)

	if n := MoveSidecars(filepath.Join(dir, "a"), filepath.Join(dir, "b")); n != 0 {
		t.Errorf("moved %d onto an occupied stem, want 0", n)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.eng.vtt")); string(b) != "keep\n" {
		t.Errorf("clobbered the existing sidecar: %q", b)
	}
	if n := MoveSidecars(filepath.Join(dir, "a"), filepath.Join(dir, "a")); n != 0 {
		t.Errorf("same-stem move reported %d", n)
	}
}
