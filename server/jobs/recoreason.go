package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"notflix/server/x/atom"
	"notflix/server/x/httpc"
)

// A one-line "why this" from the chat model. Strictly a garnish: the row
// renders from the similarity alone, so an unreachable or slow host leaves
// every reason empty and nothing else changes. Answers are cached by
// (pick, anchor) so the same pair is never paid for twice.
const reasonPath = "./cache/reco.json"

// One request for the whole row, and a deadline short enough that a host busy
// pulling a 12 GB model just doesn't answer this time.
const reasonTimeout = 90 * time.Second

var reasonMu sync.Mutex

func loadReasons() map[string]string {
	m := map[string]string{}
	if err := atom.LoadJSON(reasonPath, &m); err != nil || m == nil {
		return map[string]string{}
	}

	return m
}

func reasonKey(r Rec) string {
	return r.Title + "\x00" + r.Because
}

// reasons fills in Reason for each pick, from cache where possible and one
// batched chat call for the rest. Every failure path leaves the reasons empty
// and returns.
func reasons(ctx context.Context, items []Rec) {
	if len(items) == 0 {
		return
	}

	reasonMu.Lock()
	defer reasonMu.Unlock()

	cache := loadReasons()

	var missing []int
	for i := range items {
		if r, ok := cache[reasonKey(items[i])]; ok {
			items[i].Reason = r

			continue
		}

		missing = append(missing, i)
	}

	if len(missing) == 0 {
		return
	}

	type pair struct {
		Pick   string `json:"pick"`
		Anchor string `json:"anchor"`
	}

	pairs := make([]pair, len(missing))
	for j, i := range missing {
		pairs[j] = pair{Pick: items[i].Title, Anchor: items[i].Because}
	}

	in, err := json.Marshal(pairs)
	if err != nil {
		return
	}

	prompt := "For each pair, write one short sentence (max 12 words) explaining why someone who " +
		"watched `anchor` might enjoy `pick`. No preamble, no titles repeated verbatim if avoidable. " +
		"Return a JSON object {\"out\": [\"...\", ...]} with exactly " +
		strconv.Itoa(len(pairs)) + " strings, same order.\nInput: " + string(in)

	out, err := askOllama(ctx, prompt, reasonTimeout)
	if err != nil {
		log.Printf("[reco] reasons unavailable: %v", err)

		return
	}

	lines, err := parseTranslation(out, len(pairs))
	if err != nil {
		log.Printf("[reco] reasons unparsable: %v", err)

		return
	}

	for j, i := range missing {
		line := strings.TrimSpace(lines[j])
		if line == "" {
			continue
		}

		items[i].Reason = line
		cache[reasonKey(items[i])] = line
	}

	if err := atom.SaveJSON(reasonPath, cache); err != nil {
		log.Printf("[reco] reason cache: %v", err)
	}
}

// askOllama is one JSON-mode completion against the configured text model.
func askOllama(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
	req := map[string]any{
		"model":      ollamaModel(),
		"stream":     false,
		"format":     "json",
		"keep_alive": ollamaKeepAlive,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	var resp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := httpc.PostJSON(ctx, ollamaHost()+"/api/chat", req, &resp, timeout); err != nil {
		return "", err
	}

	if resp.Error != "" {
		return "", errors.New(resp.Error)
	}

	return resp.Message.Content, nil
}
