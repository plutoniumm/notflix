package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"notflix/server/library"
)

// Classifies real titles against the live vision model. The verdict only picks
// which upscaler shader runs, so a miss is wasted work rather than a bug — but
// it has to be right often enough to be worth asking.
//
//	ANIM_LIVE="/path/a.mp4:anime,/path/b.mp4:live" go test ./server/media/ -run AnimLive -v
func TestAnimLive(t *testing.T) {
	spec := os.Getenv("ANIM_LIVE")
	if spec == "" {
		t.Skip("set ANIM_LIVE=<path:anime|live,...>")
	}
	// Sprite sheets and the KV store are addressed relative to the repo root,
	// and go test runs with the package dir as cwd.
	t.Chdir("../..")

	for _, pair := range strings.Split(spec, ",") {
		i := strings.LastIndex(pair, ":")
		if i < 0 {
			t.Fatalf("bad spec %q", pair)
		}
		path, want := pair[:i], pair[i+1:] == "anime"

		key := library.Hash(filepath.Base(path))
		if !HasSprite(key) {
			t.Logf("SKIP (no sprite sheet): %s", filepath.Base(path))

			continue
		}

		// Force a fresh decision rather than reading a stored mark.
		library.KVDelete(animKey(path))
		if err := DetectAnimation(path, key); err != nil {
			t.Errorf("%s: %v", filepath.Base(path), err)

			continue
		}

		got := IsAnimation(path)
		label := map[bool]string{true: "animation", false: "live action"}
		if got != want {
			t.Errorf("%s: got %s, want %s", filepath.Base(path), label[got], label[want])

			continue
		}
		t.Logf("OK %-58s → %s", filepath.Base(path), label[got])
	}
}
