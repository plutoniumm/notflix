package jobs

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"os"
	"strings"
	"time"

	"notflix/server/x/httpc"
)

// Saturn is the house LLM gateway, reached through its OpenAI-compatible
// /v1/chat/completions. Organize uses whatever model saturn's chat lane resolves
// to right now (GET /agent/model — the owner's pick, healed if it has left the
// catalogue), so it follows the fleet instead of pinning an id that vanishes:
// the old pinned qwen id, and the /llm/chat route it was called through, both
// disappeared and took organize down with them.
//
// The model is resolved ONCE per plan and held for every run, because the plan
// is vote-merged and a model swapped between runs would make them disagree for
// a reason that isn't the library.
const saturnHostDefault = "https://saturn.god.manav.ch"

func SaturnHost() string {
	if h := strings.TrimRight(os.Getenv("SATURN_HOST"), "/"); h != "" {
		return h
	}
	return saturnHostDefault
}

// organizeModel doubles as the up-probe: an answer means the gateway is reachable.
func organizeModel(ctx context.Context, timeout time.Duration) (string, error) {
	if m := os.Getenv("ORGANIZE_MODEL"); m != "" {
		return m, nil
	}
	var resp struct {
		Model string `json:"model"`
	}
	if err := httpc.GetJSON(ctx, SaturnHost()+"/agent/model", &resp, timeout); err != nil {
		return "", err
	}
	if resp.Model == "" {
		return "", fmt.Errorf("saturn has no default model")
	}
	return resp.Model, nil
}

// jsonSchema wraps a bare JSON Schema in the response_format envelope. A local
// llama.cpp backend turns strict:true into a grammar; a hosted one (DeepSeek on
// novita, measured) ignores it entirely, so the prompt must spell the shape out
// too and parseOrganize still slices to the braces.
func jsonSchema(name string, schema map[string]any) map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   name,
			"strict": true,
			"schema": schema,
		},
	}
}

// saturnChat returns the model's reply text. Reasoning models answer their
// trace in a separate reasoning_content, so content is the answer alone.
func saturnChat(ctx context.Context, model, system, user string, format map[string]any, timeout time.Duration) (string, error) {
	msgs := []map[string]string{{"role": "user", "content": user}}
	if system != "" {
		msgs = append([]map[string]string{{"role": "system", "content": system}}, msgs...)
	}
	req := map[string]any{"model": model, "temperature": 0, "messages": msgs}
	if format != nil {
		req["response_format"] = format
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := httpc.PostJSON(ctx, SaturnHost()+"/v1/chat/completions", req, &resp, timeout); err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("saturn returned an empty response")
	}
	return resp.Choices[0].Message.Content, nil
}

// saturnSearch is saturn's raw web search (no LLM): the top n results as
// "title: snippet" lines, highlight tags stripped and trimmed for a prompt.
func saturnSearch(ctx context.Context, q string, n int) ([]string, error) {
	var res []struct{ Title, Content string }
	if err := httpc.GetJSON(ctx, SaturnHost()+"/search/web?q="+url.QueryEscape(q), &res, 20*time.Second); err != nil {
		return nil, err
	}
	var out []string
	for _, r := range res[:min(n, len(res))] {
		line := []rune(html.UnescapeString(tagRe.ReplaceAllString(r.Title+": "+r.Content, "")))
		out = append(out, string(line[:min(220, len(line))]))
	}
	return out, nil
}
