package library

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

const kvPath = "./cache/kv.json"

var kvMu sync.RWMutex

func kvRead() map[string]any {
	kvMu.RLock()
	defer kvMu.RUnlock()

	data, err := os.ReadFile(kvPath)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func kvWrite(m map[string]any) error {
	kvMu.Lock()
	defer kvMu.Unlock()

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(kvPath, data, 0644)
}

// KVGetValue is the in-process counterpart to KVGet. Returns nil if the key
// is absent; caller decides what to do with the type.
func KVGetValue(key string) any {
	return kvRead()[key]
}

// KVSetValue is the in-process counterpart to KVSet. Reads-modifies-writes
// the whole store under the kvMu lock chain; safe under concurrent callers.
func KVSetValue(key string, value any) error {
	store := kvRead()
	store[key] = value
	return kvWrite(store)
}

// DirRecency maps a library dir to the most-recent `watched:<param>.at`
// timestamp (ms) seen for any video under it. Dirs with no watch history are
// absent. Drives the LRU ordering of the Home video list.
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

	store := kvRead()

	var bulk []struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if json.Unmarshal(raw, &bulk) == nil && len(bulk) > 0 {
		for _, item := range bulk {
			store[item.Key] = item.Value
		}
		if err := kvWrite(store); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
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

	store[single.Key] = single.Value
	if err := kvWrite(store); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "key": single.Key})
}
