package jobs

import (
	"strings"
	"testing"
)

// Regression (2026-05-20): non-JSON model content broke every batch. Fix =
// format=json + {"out":[…]} wrapper, with bare-array and line-by-line fallbacks.
func TestParseTranslation(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
		wantErr bool
	}{
		{
			name:    "object wrapper (happy path)",
			content: `{"out":["hello","world"]}`,
			want:    []string{"hello", "world"},
		},
		{
			name:    "bare array fallback",
			content: `["hello","world"]`,
			want:    []string{"hello", "world"},
		},
		{
			name:    "array wrapped in prose",
			content: "Sure! Here it is:\n[\"hello\",\"world\"]\nLet me know.",
			want:    []string{"hello", "world"},
		},
		{
			// Shipped 11 `{` / `"out": [` cues into Seven Samurai's .whisper.vtt:
			// pretty JSON one line short of `want`, line-split into "translations".
			name:    "pretty json short of want must error, not line-split",
			content: "{\n  \"out\": [\n    \"hello\"\n  ]\n}",
			wantErr: true,
		},
		{
			name:    "numbered list fallback",
			content: "1. hello\n2. world",
			want:    []string{"hello", "world"},
		},
		{
			name:    "bulleted list fallback",
			content: "- hello\n- world",
			want:    []string{"hello", "world"},
		},
		{
			name:    "paren-numbered list",
			content: "1) hello\n2) world\n3) extra-ignored",
			want:    []string{"hello", "world"},
		},
		{
			name:    "plain lines fallback",
			content: "hello\nworld",
			want:    []string{"hello", "world"},
		},
		{
			name:    "skips blank lines",
			content: "\n\nhello\n\nworld\n",
			want:    []string{"hello", "world"},
		},
		{
			name:    "empty content errors out",
			content: "",
			wantErr: true,
		},
		{
			name:    "too few lines errors out",
			content: "only one",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTranslation(tc.content, 2)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStripListMarker(t *testing.T) {
	cases := map[string]string{
		"1. hello":   "hello",
		"12) world":  "world",
		"- a":        "a",
		"* b":        "b",
		"• c":        "c",
		"plain text": "plain text",
		"1abc":       "1abc",
	}
	for in, want := range cases {
		if got := stripListMarker(in); got != want {
			t.Errorf("stripListMarker(%q) = %q, want %q", in, got, want)
		}
	}
}

// Regression (2026-10-15): gemma4 answered 49 or 51 lines for 6 of 30 batches
// on The Roundup: Punishment. The strict `len == want` check discarded all 50
// cues of each, so 300 perfectly good translations were thrown away and 20% of
// the file shipped in Korean. Indices make the count irrelevant.
func TestParseTranslationKeyed(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
		got     []string
		wantErr bool
	}{
		{
			name:    "index-keyed object",
			content: `{"out":{"0":"hello","1":"world"}}`,
			want:    2,
			got:     []string{"hello", "world"},
		},
		{
			name:    "out of order keys land by index, not arrival",
			content: `{"out":{"2":"three","0":"one","1":"two"}}`,
			want:    3,
			got:     []string{"one", "two", "three"},
		},
		{
			// The 49-of-50 case: one line short must cost that one cue only, and
			// must NOT shift every later line onto the wrong timestamp.
			name:    "missing key leaves an empty slot in place",
			content: `{"out":{"0":"one","2":"three"}}`,
			want:    3,
			got:     []string{"one", "", "three"},
		},
		{
			// The 51-of-50 case.
			name:    "extra key past the batch is ignored",
			content: `{"out":{"0":"one","1":"two","2":"spurious"}}`,
			want:    2,
			got:     []string{"one", "two"},
		},
		{
			// gemma4 drops the "out" envelope on about a third of batches.
			name:    "bare index map without the out envelope",
			content: `{"0":"hello","1":"world"}`,
			want:    2,
			got:     []string{"hello", "world"},
		},
		{
			name:    "non-numeric keys alone error out",
			content: `{"out":{"line one":"hello"}}`,
			want:    2,
			wantErr: true,
		},
		{
			name:    "bare map with no numeric keys errors out",
			content: `{"error":"model is loading"}`,
			want:    2,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTranslation(tc.content, tc.want)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got, "|") != strings.Join(tc.got, "|") {
				t.Errorf("got %q, want %q", got, tc.got)
			}
		})
	}
}
