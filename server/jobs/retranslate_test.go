package jobs

import "testing"

func TestIsCueText(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"WEBVTT", false},
		{"", false},
		{"   ", false},
		{"00:04:24.790 --> 00:04:25.910", false},
		{"00:04.790 --> 00:05.910", false},
		{"12", false}, // cue identifier
		{"NOTE a comment", false},
		{"STYLE", false},
		{"Yes, I went to the market.", true},
		{"출발해!", true},
		{"Episode 12", true}, // digits with words is dialogue
	}

	for _, tc := range cases {
		if got := isCueText(tc.line); got != tc.want {
			t.Errorf("isCueText(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestUntranslatedCues(t *testing.T) {
	vtt := "WEBVTT\n\n" +
		"00:00:01.000 --> 00:00:02.000\nHello there.\n\n" +
		"00:00:03.000 --> 00:00:04.000\n출발해!\n\n" +
		"00:00:05.000 --> 00:00:06.000\nこっちだ\n\n"

	if got := UntranslatedCues(vtt); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
	// A timing line full of digits must never count, or every cue in the file
	// would look untranslated.
	if got := UntranslatedCues("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nAll English.\n"); got != 0 {
		t.Errorf("clean file reported %d untranslated", got)
	}
}

func TestLangOf(t *testing.T) {
	cases := map[string]string{
		"출발해!": "Korean",
		"こっちだ": "Japanese",
		// Kana before Han: Japanese mixes both, so Han-first would call it Chinese.
		"行くぞ":          "Japanese",
		"我们走吧":         "Chinese",
		"चलो चलें":     "Hindi",
		"Пошли":        "Russian",
		"ยินดี":        "Thai",
		"nothing here": "foreign",
	}

	for in, want := range cases {
		if got := langOf(in); got != want {
			t.Errorf("langOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestForeignRunes(t *testing.T) {
	if foreignRunes("Hello, world!") != 0 {
		t.Error("plain English counted as foreign")
	}
	// Latin-script languages are deliberately not flagged: readable as-is, and
	// accent-sniffing false-positives on English loanwords.
	if foreignRunes("Déjà vu, señor — naïve café") != 0 {
		t.Error("accented Latin counted as foreign")
	}
	if foreignRunes("출발해") != 3 {
		t.Errorf("got %d Hangul runes, want 3", foreignRunes("출발해"))
	}
}
