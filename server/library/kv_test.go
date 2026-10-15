package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func kvTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	return dir
}

// One JSON blob: a read that drops its lock before the write takes one loses
// every set but the last.
func TestKVConcurrentSet(t *testing.T) {
	kvTemp(t)

	const n = 50
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := KVSetValue(fmt.Sprintf("k%02d", i), float64(i)); err != nil {
				t.Errorf("KVSetValue: %v", err)
			}
		}()
	}
	wg.Wait()

	store := kvRead()
	if len(store) != n {
		t.Fatalf("kept %d of %d keys", len(store), n)
	}

	for i := range n {
		k := fmt.Sprintf("k%02d", i)
		if store[k] != float64(i) {
			t.Errorf("store[%q] = %v, want %v", k, store[k], float64(i))
		}
	}
}

// A truncated store must not read as empty and get overwritten — that swaps
// thousands of keys for the one being set.
func TestKVCorruptRefusesWrite(t *testing.T) {
	dir := kvTemp(t)
	path := filepath.Join(dir, "cache", "kv.json")
	corrupt := []byte(`{"a": 1, "b`)
	if err := os.WriteFile(path, corrupt, 0644); err != nil {
		t.Fatal(err)
	}

	if err := KVSetValue("c", 3.0); err == nil {
		t.Fatal("KVSetValue overwrote a corrupt store")
	}

	got, _ := os.ReadFile(path)
	if string(got) != string(corrupt) {
		t.Fatalf("store rewritten: %q", got)
	}
}

func TestKVWriteIsAtomic(t *testing.T) {
	dir := kvTemp(t)

	if len(kvRead()) != 0 {
		t.Fatal("missing store should read empty")
	}

	if err := KVSetValue("watched:a.mp4", map[string]any{"t": 12.0}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "kv.json" {
		t.Fatalf("temp file left behind: %v", entries)
	}

	data, err := os.ReadFile(filepath.Join(dir, "cache", "kv.json"))
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("store not valid JSON: %v", err)
	}
	if _, ok := m["watched:a.mp4"]; !ok {
		t.Fatalf("key missing from store: %v", m)
	}
}

func TestKVPrefix(t *testing.T) {
	kvTemp(t)

	for k, v := range map[string]any{
		"blurb:0000001": "a description",
		"blurb:0000002": "",
		"codec:0000001": "av1",
		"blurb":         "not prefixed by the colon form",
	} {
		if err := KVSetValue(k, v); err != nil {
			t.Fatal(err)
		}
	}

	got := KVPrefix("blurb:")
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %v", len(got), got)
	}
	if got["0000001"] != "a description" {
		t.Errorf("prefix not trimmed from key: %v", got)
	}
	// Presence is what marks a title done, so an empty value must survive
	// the scan even though the handler filters it out of the wire payload.
	if _, ok := got["0000002"]; !ok {
		t.Error("empty value dropped")
	}

	if n := len(KVPrefix("nothing:")); n != 0 {
		t.Errorf("want empty map, got %d", n)
	}
}

func TestWatchedKeepsClearedEntriesAsZero(t *testing.T) {
	kvTemp(t)

	body := `{
		"watched:Finished.mp4": {"t": 7000, "dur": 8000, "at": 12},
		"watched:Dismissed.mp4": null,
		"watched:Malformed.mp4": "nonsense",
		"codec:1234567": "av1"
	}`
	if err := os.WriteFile("cache/kv.json", []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	got := Watched()

	if len(got) != 3 {
		t.Fatalf("want 3 watch entries, got %d: %+v", len(got), got)
	}

	if w := got["Finished.mp4"]; w.T != 7000 || w.Dur != 8000 || w.At != 12 {
		t.Fatalf("Finished.mp4 = %+v", w)
	}

	// A dismissed card is still a title the user has seen; it comes back with
	// a zero T so it can never pass a watched threshold.
	if w, ok := got["Dismissed.mp4"]; !ok || w != (Watch{}) {
		t.Fatalf("Dismissed.mp4 = %+v (present %v)", w, ok)
	}

	if w, ok := got["Malformed.mp4"]; !ok || w != (Watch{}) {
		t.Fatalf("Malformed.mp4 = %+v (present %v)", w, ok)
	}
}

// The store is parsed once and kept in memory — 650 KB and 7000+ keys were
// being unmarshalled on EVERY get, twice per MediaCodec call. The cache is only
// safe if it still notices a file that changed underneath it, which is what
// keeps "refuse to write to a malformed store" working.
func TestKVCacheNoticesExternalRewrite(t *testing.T) {
	kvTemp(t)

	if err := KVSetValue("codec:abc", "av1"); err != nil {
		t.Fatal(err)
	}
	if got := KVGetValue("codec:abc"); got != "av1" {
		t.Fatalf("got %v, want av1", got)
	}

	// Replace the file behind the cache's back, the way a restored backup or a
	// hand edit would. Writing a DIFFERENT size guarantees the stat differs even
	// if the clock has not ticked.
	raw, err := json.Marshal(map[string]any{"codec:abc": "h264", "pad": "aaaaaaaaaaaaaaaa"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("cache", "kv.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}

	if got := KVGetValue("codec:abc"); got != "h264" {
		t.Errorf("served stale cached value %v, want h264", got)
	}

	// And a snapshot read must agree with the single-key read.
	if got := kvRead()["codec:abc"]; got != "h264" {
		t.Errorf("kvRead served stale %v, want h264", got)
	}
}

// A snapshot is handed out without the lock, so it must not alias the live map:
// a later write would otherwise mutate a map the caller is still ranging over.
func TestKVReadSnapshotIsDetached(t *testing.T) {
	kvTemp(t)

	if err := KVSetValue("a", 1.0); err != nil {
		t.Fatal(err)
	}
	snap := kvRead()
	if err := KVSetValue("b", 2.0); err != nil {
		t.Fatal(err)
	}

	if _, ok := snap["b"]; ok {
		t.Error("snapshot aliases the live store")
	}
}
