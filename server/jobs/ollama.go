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
	// 20min, not 5. A 50-cue batch on a good model is slow by nature:
	// gemma4:12b-it-q8_0 measured 26 tok/s (47s for ten cues), so a full batch is
	// ~4min — inside the old 5min deadline with almost no margin, and a timeout
	// silently drops all 50 cues to their source language. The batch stays at 50
	// because the ASR-correction prompt needs that much surrounding context, and
	// this is a niced background sweep where wall-clock is cheap. Do NOT buy
	// margin back by switching to a weaker/faster model.
	ollamaChat = 20 * time.Minute
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
		"options":    OllamaOpts(nil),
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

// Ollama sizes the KV cache from the model's TRAINED context — Qwen3.8 reports
// n_ctx_train 262144 — multiplied by OLLAMA_NUM_PARALLEL slots (4 on this host).
// None of our calls set num_ctx, so a long-context model asks Metal for a KV
// cache far past the wired limit and the GPU dies mid-generation with
// kIOGPUCommandBufferCallbackErrorOutOfMemory (seen 2026-09-24 13:24). Bound it
// here instead. This is an OOM fix ONLY — measured, it does not change
// throughput at all (gemma4:12b: 30.0 tok/s unbounded vs 30.3 bounded).
// ponytail: one number for every call site — the longest prompt is a 50-cue
// translate batch (~4k tokens) and blurbs' first-15%-of-runtime slice; if a
// future caller needs more, give that call its own budget rather than raising
// this for everyone.
const OllamaNumCtx = 16384

// NoThink turns off chain-of-thought on models that advertise `thinking`.
// MEASURED on gemma4:12b, ten Japanese cues: 101.5s / 3021 tokens / 8589 chars
// of thinking with it on, versus 4.1s / 119 tokens with it off — 25x — and the
// two translations were equivalent ("We must hire a samurai." vs "I'm going to
// hire a samurai."; the rest identical). Reasoning earns nothing on mechanical
// cue-by-cue translation, and it was 96% of the cost. This is how translation
// gets fast WITHOUT dropping to a weaker model: a 1400-cue episode goes from
// ~4h to ~9min. Left ON for organize (classification accuracy, already
// vote-merged) and the poster vision pick, neither of which is measured.
const NoThink = false

// OllamaOpts is the options block every /api/chat and /api/generate call should
// carry. `extra` is merged over the defaults, so a caller can add temperature.
func OllamaOpts(extra map[string]any) map[string]any {
	o := map[string]any{"num_ctx": OllamaNumCtx}
	for k, v := range extra {
		o[k] = v
	}
	return o
}

// glossary may be empty; when present it is the correct spellings of names and
// terms lifted from a sibling episode's existing English sidecar (ShowGlossary).
func TranslateSegments(texts []string, srcLang string, glossary []string) ([]string, error) {
	host := ollamaHost()
	model := ollamaModel()
	result := make([]string, len(texts))
	copy(result, texts)

	if !ollamaHasModel(host, model) {
		return result, fmt.Errorf("ollama model %q unavailable", model)
	}

	// Supplying names beats asking the model to guess them — see the prompt
	// comment below for the measurement that killed the guess-from-context idea.
	names := ""
	if len(glossary) > 0 {
		names = " Names and terms in this title, already spelled correctly — prefer one of these when a line sounds like it: " +
			strings.Join(glossary, ", ") + "."
	}

	const batchSize = 50
	batches, failed := 0, 0
	for i := 0; i < len(texts); i += batchSize {
		batches++
		end := min(i+batchSize, len(texts))
		batch := texts[i:end]

		inputJSON, _ := json.Marshal(batch)
		// Do NOT add "this is ASR output, infer the mis-heard words from context"
		// here. Tried and MEASURED FLAT against City Hunter ep05's release sub:
		// content-word overlap 15.5% (this prompt) vs 15.2% (the ASR-aware one),
		// and qualitatively sometimes worse — it left これおっぱい as "This is
		// 'oppai'" where this one says "This is boobs." It recovered zero proper
		// nouns: サイバリオ stayed "Cybario" both ways (the real name is 冴羽獠 /
		// Ryo Saeba). The model cannot infer a name it has no way to know, and
		// asking it to try just loosens the paraphrase. The fix for proper nouns is
		// to SUPPLY them — a glossary lifted from a sibling episode's existing
		// English sidecar — not a prompt instruction.
		prompt := fmt.Sprintf(
			"Translate these %s subtitle lines to English. Natural dialogue, concise.%s Return ONLY a JSON array of strings, same order, no explanation.\nInput: %s",
			srcLang, names, string(inputJSON),
		)

		// format:"json" stops models wrapping the array in prose or markdown.
		// The object wrapper is asked for because some backends only accept
		// object roots in format=json mode.
		req := map[string]any{
			"model":      model,
			"stream":     false,
			"format":     "json",
			"keep_alive": ollamaKeepAlive,
			"options":    OllamaOpts(nil),
			"think":      NoThink,
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
			failed++
			continue
		}
		if chatResp.Error != "" {
			log.Printf("[ollama] batch %d server error: %s", i/batchSize, chatResp.Error)
			failed++
			continue
		}

		translated, parseErr := parseTranslation(chatResp.Message.Content, len(batch))
		if parseErr != nil {
			log.Printf("[ollama] batch %d %v — content=%s",
				i/batchSize, parseErr, sample([]byte(chatResp.Message.Content), 240))
			failed++
			continue
		}

		for j, t := range translated {
			result[i+j] = t
		}
		log.Printf("[ollama] batch %d translated %d segments", i/batchSize, len(batch))
	}

	// A mixed-language VTT is the one outcome that looks like nobody's fault, so
	// say it out loud: per-batch failures leave those 50 cues in the source
	// language and the file ships half-translated.
	if failed > 0 {
		log.Printf("[ollama] %d/%d batches failed — %d of %d cues left untranslated (%s)",
			failed, batches, failed*batchSize, len(texts), srcLang)
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

	// A JSON-shaped answer that failed BOTH strict parses is a bad batch, not
	// prose. Line-splitting pretty-printed JSON collects `{` and `"out": [` as
	// translations — 11 such cues shipped as subtitle text in Seven Samurai's
	// .whisper.vtt. Erroring here keeps the caller's untranslated originals,
	// which are wrong but readable. The line fallback below is only for models
	// that answer in plain lines.
	if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") {
		return nil, fmt.Errorf("json answer carried %d of %d lines", len(arr), want)
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
