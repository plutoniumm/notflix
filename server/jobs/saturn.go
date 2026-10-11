package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"notflix/server/x/httpc"
)

// Saturn is the house LLM gateway. It is NOT ollama: one POST /llm/chat answers
// a single {response,done}, and it takes an OpenAI-style response_format, so the
// shape of the answer is enforced by a grammar rather than requested in prose.
// That is the whole reason organize lives here — a schema makes a malformed plan
// unrepresentable, where ollama's loose `format:"json"` still needed brace
// slicing and still lost whole runs to stray prose.
//
// The fleet assigns models by size and deliberately hides the host, but organize
// pins one id: the plan is vote-merged across runs, so a model swapped underneath
// would make two runs disagree for a reason that isn't the library.
const (
	saturnHostDefault = "https://saturn.god.manav.ch"
	organizeModelID   = "llama/qwen3.8-flash-next-uncensored-q4-k-s"
)

func saturnHost() string {
	if h := strings.TrimRight(os.Getenv("SATURN_HOST"), "/"); h != "" {
		return h
	}
	return saturnHostDefault
}

func organizeModel() string {
	if m := os.Getenv("ORGANIZE_MODEL"); m != "" {
		return m
	}
	return organizeModelID
}

// jsonSchema wraps a bare JSON Schema in the response_format envelope saturn
// forwards to llama.cpp. strict:true is what turns the schema into a grammar
// instead of a hint.
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

// saturnChat returns the model's raw text. A schema is optional; when present the
// reply is guaranteed to parse as that schema.
func saturnChat(ctx context.Context, system, user string, format map[string]any, timeout time.Duration) (string, error) {
	req := map[string]any{
		"model":       organizeModel(),
		"temperature": 0,
		"messages":    []map[string]string{{"role": "user", "content": user}},
	}
	if system != "" {
		req["system"] = system
	}
	if format != nil {
		req["response_format"] = format
	}

	var resp struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
		Error    string `json:"error"`
	}
	if err := httpc.PostJSON(ctx, saturnHost()+"/llm/chat", req, &resp, timeout); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("saturn: %s", resp.Error)
	}
	if strings.TrimSpace(resp.Response) == "" {
		return "", fmt.Errorf("saturn returned an empty response")
	}
	return resp.Response, nil
}

// saturnUp reports whether the gateway is reachable and carries the pinned model,
// so the UI can say which of the two is wrong instead of timing out per run.
func saturnUp(ctx context.Context, timeout time.Duration) error {
	var reg json.RawMessage
	if err := httpc.GetJSON(ctx, saturnHost()+"/llm/registry", &reg, timeout); err != nil {
		return err
	}
	if !strings.Contains(string(reg), organizeModel()) {
		return fmt.Errorf("model %q not in saturn's catalogue", organizeModel())
	}
	return nil
}

// saturnWarm pays the cold-load minute before the user asks for a plan.
// /llm/llamacpp/load is explicit warmup and returns once the server answers
// /health, so it is bounded by the request rather than by the checkpoint size.
func saturnWarm(ctx context.Context) error {
	body := map[string]string{"id": organizeModel()}
	var out json.RawMessage
	return httpc.PostJSON(ctx, saturnHost()+"/llm/llamacpp/load", body, &out, 5*time.Minute)
}
