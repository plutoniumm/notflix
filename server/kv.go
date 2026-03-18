package server

import (
	"encoding/json"
	"os"
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

// GET /kv/get?key=foo  or  GET /kv/get?key=foo&key=bar  (multi)
func KVGet(c *gin.Context) {
	keys := c.QueryArray("key")
	store := kvRead()

	if len(keys) == 1 {
		c.JSON(200, gin.H{"key": keys[0], "value": store[keys[0]]})
		return
	}

	out := make(map[string]any, len(keys))
	if len(keys) == 0 {
		// return everything
		c.JSON(200, store)
		return
	}
	for _, k := range keys {
		out[k] = store[k]
	}
	c.JSON(200, out)
}

// POST /kv/set  body: {"key":"foo","value":"bar"}  or  {"key":"foo","value":{...}}
// Also supports bulk: [{"key":"a","value":1}, ...]
func KVSet(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		c.JSON(400, gin.H{"error": "cannot read body"})
		return
	}

	store := kvRead()

	// Try bulk array first
	var bulk []struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if json.Unmarshal(raw, &bulk) == nil && len(bulk) > 0 {
		for _, item := range bulk {
			store[item.Key] = item.Value
		}
		if err := kvWrite(store); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true, "count": len(bulk)})
		return
	}

	// Single object
	var single struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(raw, &single); err != nil || single.Key == "" {
		c.JSON(400, gin.H{"error": "expected {key, value} or [{key, value}]"})
		return
	}

	store[single.Key] = single.Value
	if err := kvWrite(store); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "key": single.Key})
}
