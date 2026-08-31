package httpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The whole point of the package: a bad status is an error, never bytes the
// caller can mistake for content (the "write the 404 page to disk" bug).
func TestStatusIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html>nope</html>"))
	}))
	defer srv.Close()

	body, err := Get(context.Background(), srv.URL, time.Second)
	if err == nil {
		t.Fatal("404 must be an error")
	}
	if body != nil {
		t.Fatalf("body leaked on error status: %q", body)
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error lost status or body: %v", err)
	}

	var out map[string]string
	if err := GetJSON(context.Background(), srv.URL, &out, time.Second); err == nil {
		t.Fatal("GetJSON must fail on 404 instead of parsing the error page")
	}
}

// A peer that accepts the connection and never answers is exactly the
// Tailscale-asleep case; the call must come back on its own deadline.
func TestTimeoutIsEnforced(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	start := time.Now()
	if _, err := Get(context.Background(), srv.URL, 150*time.Millisecond); err == nil {
		t.Fatal("hung server must produce an error")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("timeout not enforced: waited %v", el)
	}
}

func TestContextCancels(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if _, err := Get(ctx, srv.URL, time.Minute); err == nil {
		t.Fatal("cancelled context must produce an error")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("context ignored: waited %v", el)
	}
}

func TestPostJSONRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var in map[string]string
		if err := readJSON(r, &in); err != nil {
			t.Errorf("server decode: %v", err)
		}
		if in["q"] != "frieren" {
			t.Errorf("payload not sent: %v", in)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var out struct {
		OK bool `json:"ok"`
	}
	if err := PostJSON(context.Background(), srv.URL, map[string]string{"q": "frieren"}, &out, time.Second); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatal("reply not decoded")
	}

	if err := PostJSON(context.Background(), srv.URL, map[string]string{"q": "frieren"}, nil, time.Second); err != nil {
		t.Fatalf("nil out should discard the reply: %v", err)
	}
}

func TestBodyIsCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 1<<16)
		for range (Cap / len(chunk)) + 4 {
			w.Write([]byte(chunk))
		}
	}))
	defer srv.Close()

	body, err := Get(context.Background(), srv.URL, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != Cap {
		t.Fatalf("body = %d bytes, want the %d cap", len(body), Cap)
	}
}

func TestNoTimeoutFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if _, err := Get(context.Background(), srv.URL, 0); err != nil {
		t.Fatalf("zero timeout must fall back, not fail: %v", err)
	}
}

func readJSON(r *http.Request, out any) error {
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, out)
}
