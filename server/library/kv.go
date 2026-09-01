package library

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"notflix/server/x/atom"
)

const kvPath = "./cache/kv.json"

var kvMu sync.RWMutex

// readLocked requires the caller to hold kvMu. An absent file is an empty
// store; a present-but-malformed one is an error the caller must respect.
func readLocked() (map[string]any, error) {
	m := map[string]any{}
	if err := atom.LoadJSON(kvPath, &m); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}

		return nil, err
	}

	if m == nil {
		m = map[string]any{}
	}

	return m, nil
}

func kvRead() map[string]any {
	kvMu.RLock()
	defer kvMu.RUnlock()

	m, err := readLocked()
	if err != nil {
		log.Printf("[kv] %s unreadable, serving empty: %v", kvPath, err)

		return map[string]any{}
	}

	return m
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

	return atom.SaveJSON(kvPath, store)
}

func KVGetValue(key string) any {
	return kvRead()[key]
}

func KVSetValue(key string, value any) error {
	return update(func(store map[string]any) { store[key] = value })
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
