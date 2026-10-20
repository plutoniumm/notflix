package media

import (
	"path/filepath"
	"strings"
	"testing"

	"notflix/server/library"
)

func TestCompatRoundTrip(t *testing.T) {
	chdirTemp(t)

	if IsCompat("Movie.2020.mkv") {
		t.Fatal("unmarked file reads as compat")
	}

	if err := SetCompat("Movie.2020.mkv", true); err != nil {
		t.Fatal(err)
	}
	if !IsCompat("Movie.2020.mkv") {
		t.Fatal("mark did not round-trip")
	}
}

// Presence means set, so clearing has to remove the key — a stored "0" would
// still show up in /api/compat.
func TestCompatClearDeletesKey(t *testing.T) {
	chdirTemp(t)

	if err := SetCompat("a.mkv", true); err != nil {
		t.Fatal(err)
	}
	if err := SetCompat("a.mkv", false); err != nil {
		t.Fatal(err)
	}

	if IsCompat("a.mkv") {
		t.Fatal("cleared mark still reads as compat")
	}
	if _, ok := library.KVPrefix(compatPrefix)[library.Hash("a.mkv")]; ok {
		t.Fatal("clearing wrote a value instead of deleting the key")
	}
}

// CleanAll/Organize move files between dirs after the mark is stamped; a
// dir-qualified key would orphan and the file would silently convert to AV1.
func TestCompatSurvivesMove(t *testing.T) {
	chdirTemp(t)

	if err := SetCompat(filepath.Join("Downloads", "Show S01E01.mkv"), true); err != nil {
		t.Fatal(err)
	}

	moved := filepath.Join("Shows", "Show", "Season 1", "Show S01E01.mkv")
	if !IsCompat(moved) {
		t.Fatal("mark orphaned by a move between dirs")
	}
}

func TestCompatUnmarkedFileIsFalse(t *testing.T) {
	chdirTemp(t)

	if err := SetCompat("marked.mkv", true); err != nil {
		t.Fatal(err)
	}
	if IsCompat("other.mkv") {
		t.Fatal("unmarked file reads as compat")
	}
}

// The whole point: an AV1 source is normally copied, and compat must re-encode
// it instead.
func TestCompatBeatsAV1Copy(t *testing.T) {
	chdirTemp(t)

	if got := strings.Join(videoArgs("x.mkv", "av1"), " "); got != "-c:v copy" {
		t.Fatalf("unmarked av1 source: got %q, want a copy", got)
	}

	if err := SetCompat("x.mkv", true); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(videoArgs("x.mkv", "av1"), " ")
	if strings.Contains(got, "copy") {
		t.Fatalf("compat copied AV1: %q", got)
	}
	if !strings.Contains(got, pickH264()) {
		t.Fatalf("compat did not pick the h264 encoder: %q", got)
	}
	if !strings.Contains(got, "-pix_fmt yuv420p") {
		t.Fatalf("compat must force 8-bit: %q", got)
	}
}

// A marked non-AV1 source must not fall through to the AV1 encoder either.
func TestCompatOverridesAV1Encode(t *testing.T) {
	chdirTemp(t)

	if err := SetCompat("y.mkv", true); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(videoArgs("y.mkv", "h264"), " ")
	if strings.Contains(got, "av1") {
		t.Fatalf("compat produced AV1 args: %q", got)
	}
}
