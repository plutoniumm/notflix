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
	"time"
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
	// aya-expanse:8b — Cohere's purpose-built multilingual model. On
	// FLORES-200 it beats Gemma 2 9B, Llama 3.1 8B, Qwen 2.5 7B in its
	// weight class (Aya Expanse paper, arXiv:2412.04261). ~5GB on disk,
	// ~80 tok/s on Apple Silicon, ~12s for a 50-cue Hindi→English batch
	// in JSON mode → roughly 6 min to translate a 90-min movie's subs,
	// which is well under playback rate. JSON-mode adherence: 4–5 / 5 in
	// local tests; the parseTranslation fallback handles the rare miss.
	return "aya-expanse:8b"
}

// ollamaKeepAlive rides on every chat request so a model loaded for one run
// is still resident for the next — a cold load of the default model takes on
// the order of a minute, dwarfing the actual generation.
const ollamaKeepAlive = "15m"

// ollamaLoaded checks /api/ps — the models currently resident in memory — so
// callers can distinguish a cold load from an instant warm call.
func ollamaLoaded(host, model string) bool {
	resp, err := http.Get(host + "/api/ps")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var ps struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&ps) != nil {
		return false
	}
	for _, m := range ps.Models {
		if m.Name == model || m.Model == model {
			return true
		}
	}
	return false
}

// ollamaWarm loads the model without generating: a chat call with no messages
// returns done_reason "load" once the weights are resident. Also refreshes
// keep_alive on an already-loaded model, so it's safe to call eagerly.
func ollamaWarm(host, model string) error {
	reqBody, _ := json.Marshal(map[string]any{
		"model":      model,
		"messages":   []any{},
		"keep_alive": ollamaKeepAlive,
	})
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Post(host+"/api/chat", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama load HTTP %d: %s", resp.StatusCode, sample(body, 240))
	}
	return nil
}

// ollamaHasModel checks /api/tags once before the batch loop. The flood we used
// to get (one 404 per batch, ~9 per file × hundreds of files) came from a
// configured model that simply wasn't pulled on the host. Failing fast here
// logs the available models once and lets the caller save the raw transcript.
func ollamaHasModel(host, model string) bool {
	resp, err := http.Get(host + "/api/tags")
	if err != nil {
		log.Printf("[ollama] /api/tags unreachable: %v", err)
		return false
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		log.Printf("[ollama] /api/tags parse error: %v", err)
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

	// Bail before the loop if the model is missing — otherwise every batch
	// 404s identically and we save the raw (untranslated) transcript anyway.
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

		// format:"json" forces the model to emit syntactically valid JSON —
		// strictly required to dodge the "unexpected end of JSON input" flood
		// we used to get from instruction-tuned models that sometimes wrap the
		// array in prose or markdown. The prompt asks for an object wrapper
		// (`{"out":[...]}`) because some backends only accept object roots in
		// format=json mode.
		reqBody, _ := json.Marshal(map[string]any{
			"model":      model,
			"stream":     false,
			"format":     "json",
			"keep_alive": ollamaKeepAlive,
			"messages": []map[string]string{
				{"role": "user", "content": prompt +
					`\nReturn a single JSON object with key "out" mapping to the array.`},
			},
		})

		resp, err := http.Post(host+"/api/chat", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			log.Printf("[ollama] batch %d request error: %v — using original", i/batchSize, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("[ollama] batch %d HTTP %d — using original. body=%s",
				i/batchSize, resp.StatusCode, sample(body, 240))
			continue
		}

		var chatResp struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &chatResp); err != nil {
			log.Printf("[ollama] batch %d outer parse error: %v — body=%s",
				i/batchSize, err, sample(body, 240))
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

// parseTranslation accepts whatever the model produced and pulls out exactly
// `want` lines. Tries in order: {"out":[...]} (format=json happy path) → bare
// JSON array → line-by-line. Returns an error only if none yield the right
// count.
func parseTranslation(content string, want int) ([]string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	// 1. Object wrapper from format=json.
	var obj struct {
		Out []string `json:"out"`
	}
	if err := json.Unmarshal([]byte(content), &obj); err == nil && len(obj.Out) == want {
		return obj.Out, nil
	}

	// 2. Bare JSON array (in case the model ignored the wrapper instruction).
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

	// 3. Line-by-line fallback for models that emit numbered lists or
	// plain text. Strip "1.", "1)", "- ", "* " prefixes; collect first
	// `want` non-empty lines.
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
	// "• foo" / "- foo" / "* foo"
	for _, p := range []string{"• ", "- ", "* "} {
		if strings.HasPrefix(s, p) {
			return strings.TrimSpace(s[len(p):])
		}
	}
	// "1. foo" / "1) foo" / "12. foo"
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
