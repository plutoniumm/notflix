// Package httpc is the outbound HTTP path for the server. Every request it
// makes carries an enforced timeout, every response is status-checked before
// a caller sees a byte, and every body is closed and length-capped.
//
// The timeout is not a courtesy: an ollama host that goes to sleep accepts the
// connection and then never answers, and a wedged aria2c still listens on its
// RPC port. A plain http.Get against either blocks forever, which is how one
// sleeping peer used to take down the whole whisper pool.
//
// Imports nothing internal, so every package can depend on it.
package httpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Cap bounds every response read. Nothing we talk to — RPC, chat completions,
// torrent search — has a legitimate reason to be bigger.
const Cap = 8 << 20

// Fallback deadline for a caller that passes none, so "no timeout" is not
// reachable through this package.
const fallback = 30 * time.Second

// Send runs req and returns its body. A non-2xx status is an error carrying a
// snippet of what came back, so an error page can never reach a parser or get
// written to disk as though it were content.
func Send(req *http.Request, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = fallback
	}

	// A nil Transport means the shared default pool, so a per-call client is
	// just a deadline, not a new set of connections.
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, Cap))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s: HTTP %d: %s", req.URL.Host, resp.StatusCode, snip(body))
	}
	if readErr != nil {
		return nil, readErr
	}

	return body, nil
}

func Get(ctx context.Context, url string, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	return Send(req, timeout)
}

func Post(ctx context.Context, url, ctype string, body []byte, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ctype)

	return Send(req, timeout)
}

func GetJSON(ctx context.Context, url string, out any, timeout time.Duration) error {
	body, err := Get(ctx, url, timeout)
	if err != nil {
		return err
	}

	return decode(body, out)
}

// PostJSON marshals in, posts it, and decodes the reply into out. A nil out
// discards the reply.
func PostJSON(ctx context.Context, url string, in, out any, timeout time.Duration) error {
	req, err := json.Marshal(in)
	if err != nil {
		return err
	}

	body, err := Post(ctx, url, "application/json", req, timeout)
	if err != nil {
		return err
	}

	return decode(body, out)
}

func decode(body []byte, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode: %w (body=%s)", err, snip(body))
	}

	return nil
}

func snip(b []byte) string {
	const n = 240
	if len(b) <= n {
		return string(b)
	}

	return string(b[:n]) + "…"
}
