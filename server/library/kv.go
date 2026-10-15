package library

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/x/atom"
)

const kvPath = "./cache/kv.json"

var (
	kvMu sync.RWMutex
	// The parsed store, authoritative once loaded. nil means "not loaded yet".
	//
	// Every read used to go to disk: 650 KB and 7217 keys unmarshalled per call,
	// and MediaCodec does two gets, so every HLSMaster parsed 1.3 MB of JSON
	// before it could decide which codec to serve.
	kvCache map[string]any
	// What the cache was built from. A stat per read (microseconds) against a
	// ~20 ms parse keeps the old semantics intact: a store replaced or corrupted
	// behind our back is still noticed, so "refuse to write to a malformed
	// store" survives the cache. kvSize -1 means "cached an absent file".
	kvSize int64 = -1
	kvMod  time.Time
)

// stale requires kvMu held (either mode). True when the file no longer matches
// what the cache was built from.
func stale() bool {
	fi, err := os.Stat(kvPath)
	if err != nil {
		return kvSize != -1
	}

	return fi.Size() != kvSize || !fi.ModTime().Equal(kvMod)
}

// stampLocked records what the cache now corresponds to. Requires the write lock.
func stampLocked() {
	if fi, err := os.Stat(kvPath); err == nil {
		kvSize, kvMod = fi.Size(), fi.ModTime()

		return
	}
	kvSize, kvMod = -1, time.Time{}
}

// readLocked requires the caller to hold kvMu for WRITING — it populates the
// cache. An absent file is an empty store; a present-but-malformed one is an
// error the caller must respect, and leaves the cache unset so the next call
// tries the file again rather than inventing an empty store.
func readLocked() (map[string]any, error) {
	if kvCache != nil && !stale() {
		return kvCache, nil
	}

	m := map[string]any{}
	if err := atom.LoadJSON(kvPath, &m); err != nil {
		kvCache = nil
		if errors.Is(err, os.ErrNotExist) {
			kvCache, kvSize, kvMod = map[string]any{}, -1, time.Time{}

			return kvCache, nil
		}

		return nil, err
	}

	if m == nil {
		m = map[string]any{}
	}
	kvCache = m
	stampLocked()

	return kvCache, nil
}

// kvRead returns a snapshot safe to iterate after the lock is dropped. The copy
// is shallow, which is sound because every writer REPLACES a value rather than
// mutating one in place — a reader can never observe a half-written value. The
// copy costs microseconds against the ~20 ms parse it replaces.
func kvRead() map[string]any {
	kvMu.RLock()
	if kvCache != nil && !stale() {
		out := copyStore(kvCache)
		kvMu.RUnlock()

		return out
	}
	kvMu.RUnlock()

	kvMu.Lock()
	defer kvMu.Unlock()

	m, err := readLocked()
	if err != nil {
		log.Printf("[kv] %s unreadable, serving empty: %v", kvPath, err)

		return map[string]any{}
	}

	return copyStore(m)
}

func copyStore(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}

	return out
}

// update is the ONLY write path: one exclusive lock across read, mutate and
// write. The store is one JSON blob, so RLock-read + separate Lock-write lost
// every concurrent set but the last. A malformed store aborts the write.
func update(fn func(map[string]any)) error {
	kvMu.Lock()
	defer kvMu.Unlock()

	store, err := readLocked()
	if err != nil {
		log.Printf("[kv] REFUSING to write: %s is corrupt (%v) — move it aside to start fresh", kvPath, err)

		return err
	}

	fn(store)

	if err := atom.SaveJSON(kvPath, store); err != nil {
		// Memory now holds a mutation the disk does not. Drop the cache so the
		// next reader re-reads the file and sees what actually persisted.
		kvCache = nil

		return err
	}
	// Re-stamp, or every read after a write would see a changed file and parse
	// it again — and the player saves its position every 2 seconds.
	stampLocked()

	return nil
}

// KVGetValue is the hot path — MediaCodec, skip:, poster: and blurb: lookups all
// land here — so it reads ONE key under the read lock instead of copying the
// whole store.
func KVGetValue(key string) any {
	kvMu.RLock()
	if kvCache != nil && !stale() {
		v := kvCache[key]
		kvMu.RUnlock()

		return v
	}
	kvMu.RUnlock()

	kvMu.Lock()
	defer kvMu.Unlock()

	store, err := readLocked()
	if err != nil {
		log.Printf("[kv] %s unreadable, serving empty: %v", kvPath, err)

		return nil
	}

	return store[key]
}

func KVSetValue(key string, value any) error {
	return update(func(store map[string]any) { store[key] = value })
}

// KVDelete removes a key through the same single write path, so a delete
// racing a set cannot lose the set.
func KVDelete(key string) error {
	return update(func(store map[string]any) { delete(store, key) })
}

// Watch is one `watched:<param>` progress record: T is the last saved
// position in seconds, Dur the video's duration when the player knew it
// (older records have none), At the wall clock of the save in ms.
type Watch struct {
	T   float64
	Dur float64
	At  float64
}

// Watched returns every watch record in the store, keyed by video param.
//
// A cleared entry is stored as JSON null — that is how the UI removes a
// Continue Watching card — and comes back as a zero Watch rather than being
// dropped. The title was still opened at some point, which is what a caller
// asking "has this been seen" needs to know, and a zero T keeps it below
// every watched threshold so it can never count as finished.
func Watched() map[string]Watch {
	store := kvRead()
	out := make(map[string]Watch)

	for k, v := range store {
		if !strings.HasPrefix(k, "watched:") {
			continue
		}

		w := Watch{}

		m, _ := v.(map[string]any)
		if m == nil {
			out[strings.TrimPrefix(k, "watched:")] = w

			continue
		}

		w.T, _ = m["t"].(float64)
		w.Dur, _ = m["dur"].(float64)
		w.At, _ = m["at"].(float64)
		out[strings.TrimPrefix(k, "watched:")] = w
	}

	return out
}

// DirRecency maps a library dir to the newest watched:<param>.at (ms) under
// it; drives the LRU ordering of the Home list.
func DirRecency() map[string]float64 {
	store := kvRead()
	out := make(map[string]float64)
	for k, v := range store {
		if !strings.HasPrefix(k, "watched:") {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		at, ok := m["at"].(float64)
		if !ok {
			continue
		}
		param := strings.TrimPrefix(k, "watched:")
		dir := "."
		if i := strings.LastIndex(param, "/"); i >= 0 {
			dir = param[:i]
		}
		if at > out[dir] {
			out[dir] = at
		}
	}
	return out
}

func HiddenDirs() map[string]bool {
	store := kvRead()
	out := make(map[string]bool)
	for k, v := range store {
		if !strings.HasPrefix(k, "hidden:") {
			continue
		}
		if b, ok := v.(bool); ok && b {
			out[strings.TrimPrefix(k, "hidden:")] = true
		}
	}
	return out
}

func HiddenList(c *gin.Context) {
	hidden := HiddenDirs()
	out := make([]string, 0, len(hidden))
	for k := range hidden {
		out = append(out, k)
	}
	c.JSON(http.StatusOK, out)
}

func KVGet(c *gin.Context) {
	keys := c.QueryArray("key")
	store := kvRead()

	if len(keys) == 1 {
		c.JSON(http.StatusOK, gin.H{"key": keys[0], "value": store[keys[0]]})
		return
	}

	out := make(map[string]any, len(keys))
	if len(keys) == 0 {
		c.JSON(http.StatusOK, store)
		return
	}
	for _, k := range keys {
		out[k] = store[k]
	}
	c.JSON(http.StatusOK, out)
}

func KVSet(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	var bulk []struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	ip := c.ClientIP()

	if json.Unmarshal(raw, &bulk) == nil && len(bulk) > 0 {
		err := update(func(store map[string]any) {
			for _, item := range bulk {
				store[item.Key] = item.Value
			}
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

			return
		}

		for _, item := range bulk {
			statsTee(item.Key, item.Value, ip)
		}

		c.JSON(http.StatusOK, gin.H{"ok": true, "count": len(bulk)})

		return
	}

	var single struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(raw, &single); err != nil || single.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected {key, value} or [{key, value}]"})

		return
	}

	if err := KVSetValue(single.Key, single.Value); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

		return
	}

	statsTee(single.Key, single.Value, ip)
	c.JSON(http.StatusOK, gin.H{"ok": true, "key": single.Key})
}

// KVPrefix returns every entry whose key starts with prefix, with the prefix
// trimmed off the returned keys. Read-only — it goes through kvRead, not a
// second read-modify-write path.
func KVPrefix(prefix string) map[string]any {
	out := map[string]any{}
	for k, v := range kvRead() {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = v
		}
	}

	return out
}
