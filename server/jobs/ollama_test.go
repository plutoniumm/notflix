package jobs

import (
	"strings"
	"testing"
)

// Regression (2026-05-20): every Ollama batch failed with "unexpected end of
// JSON input" because the model's content was non-JSON (or empty). The fix
// uses format=json + an {"out":[…]} object wrapper, with a bare-array and a
// line-by-line fallback. These cases lock those three paths and a couple of
// real-world failure modes we want to keep tolerating.
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
		"1abc":       "1abc", // no marker char after digits → unchanged
	}
	for in, want := range cases {
		if got := stripListMarker(in); got != want {
			t.Errorf("stripListMarker(%q) = %q, want %q", in, got, want)
		}
	}
}
