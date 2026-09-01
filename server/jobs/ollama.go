package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"notflix/server/x/httpc"
)

func ollamaHost() string {
	if h := os.Getenv("OLLAMA_HOST"); h != "" {
		return strings.TrimRight(h, "/")
	}
	return "http://100.117.92.56:11434"
}

func ollamaModel() string {
	if m := os.Getenv("OLLAMA_MODEL"); m != "" {
		return m
	}
	// Multilingual model in this weight class; ~80 tok/s, comfortably under
	// playback rate for a feature's subs.
	return "aya-expanse:8b"
}

// Keeps a loaded model resident between runs; a cold load costs ~a minute.
const ollamaKeepAlive = "15m"

// The ollama host is a Tailscale peer that can be asleep: it accepts the
// connection and never answers, so every call needs a deadline.
const (
	ollamaQuick = 10 * time.Second
	ollamaChat  = 5 * time.Minute
)

// /api/ps lists the models resident in memory — cold load vs warm call.
func ollamaLoaded(host, model string) bool {
	var ps struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if httpc.GetJSON(context.Background(), host+"/api/ps", &ps, ollamaQuick) != nil {
		return false
	}

	for _, m := range ps.Models {
		if m.Name == model || m.Model == model {
			return true
		}
	}
	return false
}

// A chat with no messages loads the weights and refreshes keep_alive, so this
// is safe to call eagerly.
func ollamaWarm(host, model string) error {
	req := map[string]any{
		"model":      model,
		"messages":   []any{},
		"keep_alive": ollamaKeepAlive,
	}
	if err := httpc.PostJSON(context.Background(), host+"/api/chat", req, nil, ollamaChat); err != nil {
		return fmt.Errorf("ollama load: %w", err)
	}

	return nil
}

// One check before the batch loop: a model that isn't pulled on the host 404s
// once per batch otherwise.
func ollamaHasModel(host, model string) bool {
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := httpc.GetJSON(context.Background(), host+"/api/tags", &tags, ollamaQuick); err != nil {
		log.Printf("[ollama] /api/tags: %v", err)
		return false
	}

	have := make([]string, len(tags.Models))
	for i, m := range tags.Models {
		have[i] = m.Name
		if m.Name == model {
			return true
		}
	}
	log.Printf("[ollama] model %q not on host; available: %v — set OLLAMA_MODEL", model, have)
	return false
}

func TranslateSegments(texts []string, srcLang string) ([]string, error) {
	host := ollamaHost()
	model := ollamaModel()
	result := make([]string, len(texts))
	copy(result, texts)

	if !ollamaHasModel(host, model) {
		return result, fmt.Errorf("ollama model %q unavailable", model)
	}

	const batchSize = 50
	for i := 0; i < len(texts); i += batchSize {
		end := min(i+batchSize, len(texts))
		batch := texts[i:end]

		inputJSON, _ := json.Marshal(batch)
		prompt := fmt.Sprintf(
			"Translate these %s subtitle lines to English. Natural dialogue, concise. Return ONLY a JSON array of strings, same order, no explanation.\nInput: %s",
			srcLang, string(inputJSON),
		)

		// format:"json" stops models wrapping the array in prose or markdown.
		// The object wrapper is asked for because some backends only accept
		// object roots in format=json mode.
		req := map[string]any{
			"model":      model,
			"stream":     false,
			"format":     "json",
			"keep_alive": ollamaKeepAlive,
			"messages": []map[string]string{
				{"role": "user", "content": prompt +
					`\nReturn a single JSON object with key "out" mapping to the array.`},
			},
		}

		var chatResp struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Error string `json:"error"`
		}
		if err := httpc.PostJSON(context.Background(), host+"/api/chat", req, &chatResp, ollamaChat); err != nil {
			log.Printf("[ollama] batch %d: %v — using original", i/batchSize, err)
			continue
		}
		if chatResp.Error != "" {
			log.Printf("[ollama] batch %d server error: %s", i/batchSize, chatResp.Error)
			continue
		}

		translated, parseErr := parseTranslation(chatResp.Message.Content, len(batch))
		if parseErr != nil {
			log.Printf("[ollama] batch %d %v — content=%s",
				i/batchSize, parseErr, sample([]byte(chatResp.Message.Content), 240))
			continue
		}

		for j, t := range translated {
			result[i+j] = t
		}
		log.Printf("[ollama] batch %d translated %d segments", i/batchSize, len(batch))
	}

	return result, nil
}

// Pulls exactly `want` lines out of whatever the model produced: {"out":[...]}
// → bare JSON array → line-by-line.
func parseTranslation(content string, want int) ([]string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	var obj struct {
		Out []string `json:"out"`
	}
	if err := json.Unmarshal([]byte(content), &obj); err == nil && len(obj.Out) == want {
		return obj.Out, nil
	}

	trimmed := content
	if idx := strings.Index(trimmed, "["); idx != -1 {
		trimmed = trimmed[idx:]
	}
	if idx := strings.LastIndex(trimmed, "]"); idx != -1 {
		trimmed = trimmed[:idx+1]
	}
	var arr []string
	if err := json.Unmarshal([]byte(trimmed), &arr); err == nil && len(arr) == want {
		return arr, nil
	}

	var lines []string
	for raw := range strings.SplitSeq(content, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" {
			continue
		}
		l = stripListMarker(l)
		if l == "" {
			continue
		}
		lines = append(lines, l)
		if len(lines) == want {
			break
		}
	}
	if len(lines) == want {
		return lines, nil
	}

	return nil, fmt.Errorf("could not extract %d translations (got %d via json, %d via lines)",
		want, len(arr), len(lines))
}

func stripListMarker(s string) string {
	for _, p := range []string{"• ", "- ", "* "} {
		if strings.HasPrefix(s, p) {
			return strings.TrimSpace(s[len(p):])
		}
	}
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end > 0 && end < len(s) && (s[end] == '.' || s[end] == ')') {
		return strings.TrimSpace(s[end+1:])
	}
	return s
}

func sample(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
