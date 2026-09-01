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
