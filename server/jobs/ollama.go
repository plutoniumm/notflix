package jobs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
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
	return "qwen2.5:7b"
}

func TranslateSegments(texts []string, srcLang string) ([]string, error) {
	host := ollamaHost()
	model := ollamaModel()
	result := make([]string, len(texts))
	copy(result, texts)

	const batchSize = 50
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		inputJSON, _ := json.Marshal(batch)
		prompt := fmt.Sprintf(
			"Translate these %s subtitle lines to English. Natural dialogue, concise. Return ONLY a JSON array of strings, same order, no explanation.\nInput: %s",
			srcLang, string(inputJSON),
		)

		reqBody, _ := json.Marshal(map[string]any{
			"model":  model,
			"stream": false,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
		})

		resp, err := http.Post(host+"/api/chat", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			log.Printf("[ollama] batch %d request error: %v — using original", i/batchSize, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var chatResp struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(body, &chatResp); err != nil {
			log.Printf("[ollama] batch %d parse error: %v — using original", i/batchSize, err)
			continue
		}

		content := strings.TrimSpace(chatResp.Message.Content) // strip markdown code fences if present
		if idx := strings.Index(content, "["); idx != -1 {
			content = content[idx:]
		}
		if idx := strings.LastIndex(content, "]"); idx != -1 {
			content = content[:idx+1]
		}

		var translated []string
		if err := json.Unmarshal([]byte(content), &translated); err != nil {
			log.Printf("[ollama] batch %d JSON decode error: %v — using original", i/batchSize, err)
			continue
		}
		if len(translated) != len(batch) {
			log.Printf("[ollama] batch %d length mismatch (%d vs %d) — using original", i/batchSize, len(translated), len(batch))
			continue
		}

		for j, t := range translated {
			result[i+j] = t
		}
		log.Printf("[ollama] batch %d translated %d segments", i/batchSize, len(batch))
	}

	return result, nil
}
