package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBdecode(t *testing.T) {
	cases := []struct {
		in   string
		want any
		bad  bool
	}{
		{in: "i42e", want: int64(42)},
		{in: "i-7e", want: int64(-7)},
		{in: "4:spam", want: "spam"},
		{in: "0:", want: ""},
		{in: "le", want: []any{}},
		{in: "de", want: map[string]any{}},
		{in: "l4:spami3ee", want: []any{"spam", int64(3)}},
		{in: "d3:cowi1ee", want: map[string]any{"cow": int64(1)}},
		// Truncation at every type must error, not panic: one corrupt
		// .torrent can't take the handler down.
		{in: "", bad: true},
		{in: "i42", bad: true},
		{in: "5:spam", bad: true},
		{in: "l4:spam", bad: true},
		{in: "d3:cow", bad: true},
		{in: "x", bad: true},
		{in: "-1:a", bad: true},
	}

	for _, c := range cases {
		got, err := bdecode([]byte(c.in))
		if c.bad {
			if err == nil {
				t.Errorf("bdecode(%q) = %#v, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("bdecode(%q): %v", c.in, err)
			continue
		}
		if g, w := jsonish(got), jsonish(c.want); g != w {
			t.Errorf("bdecode(%q) = %s, want %s", c.in, g, w)
		}
	}
}

func jsonish(v any) string {
	b, _ := json.Marshal(v)

	return string(b)
}

func TestTorrentMeta(t *testing.T) {
	dir := t.TempDir()
	multi := "d4:infod5:filesld6:lengthi100e4:pathl2:S15:a.mkveed6:lengthi200e4:pathl5:b.mkveee4:name4:packee"
	single := "d4:infod6:lengthi7e4:name9:movie.mp4ee"

	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}

		return p
	}

	name, files, err := torrentMeta(write("m.torrent", multi))
	if err != nil {
		t.Fatal(err)
	}
	if name != "pack" || len(files) != 2 {
		t.Fatalf("multi: name=%q files=%#v", name, files)
	}
	if files[0].Rel != "S1/a.mkv" || files[0].Name != "a.mkv" || files[0].Size != 100 {
		t.Errorf("multi first = %#v", files[0])
	}

	name, files, err = torrentMeta(write("s.torrent", single))
	if err != nil {
		t.Fatal(err)
	}
	if name != "movie.mp4" || len(files) != 1 || files[0].Size != 7 {
		t.Errorf("single: name=%q files=%#v", name, files)
	}

	if _, _, err := torrentMeta(write("bad.torrent", "d4:infod")); err == nil {
		t.Error("truncated torrent: want error")
	}
}

// The rename join is the whole reason History works: the download name and
// the on-disk name are different strings for the same file.
func TestNormKeyJoinsRenames(t *testing.T) {
	pairs := [][2]string{
		{
			"The.Orville.S02E11.Lasting.Impressions.1080p.WEBRip.AC3.x264-LESS.mkv",
			"The Orville S02E11 Lasting Impressions LESS.mp4",
		},
		{
			"Freaky.Friday.2003.1080p.BluRay.x264-[YTS.AM].mkv",
			"Freaky Friday 2003.mp4",
		},
	}
	for _, p := range pairs {
		if a, b := normKey(p[0]), normKey(p[1]); a != b {
			t.Errorf("normKey mismatch:\n %s -> %q\n %s -> %q", p[0], a, p[1], b)
		}
	}

	// Two episodes of one show must NOT collapse: the digits carry the identity.
	if normKey("Show.S01E01.1080p.mkv") == normKey("Show.S01E02.1080p.mkv") {
		t.Error("episodes collapsed onto one key")
	}
}

// The join itself, hermetically: a torrent whose file was renamed on disk, a
// pack entry that never landed, and a ledger entry for a file that is gone.
func TestHistoryJoins(t *testing.T) {
	root := t.TempDir()
	tor := "d4:infod5:filesld6:lengthi11e4:pathl38:Show.S01E01.1080p.WEBRip.x264-LESS.mkveed6:lengthi22e4:pathl38:Show.S01E02.1080p.WEBRip.x264-LESS.mkveee4:name8:Show.S01ee"
	if err := os.WriteFile(filepath.Join(root, "abc123.torrent"), []byte(tor), 0644); err != nil {
		t.Fatal(err)
	}
	// E01 landed and was renamed + remuxed by the pipeline; E02 never did.
	if err := os.WriteFile(filepath.Join(root, "Show S01E01 LESS.mp4"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	PipelineMark(filepath.Join(root, "Show S01E01 LESS.mp4"), "convert", StepDone)
	PipelineMark(filepath.Join(root, "Deleted.S09E09.mp4"), "thumb", StepDone)

	rows, st := History(&Library{Roots: []string{root}})
	if st.Torrents != 1 || st.TorrentsBad != 0 || st.DiskFiles != 1 {
		t.Fatalf("stats = %+v", st)
	}

	byName := map[string]HistRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}

	e1 := byName["Show S01E01 LESS.mp4"]
	if !e1.OnDisk || e1.DlAt == 0 || e1.Infohash != "abc123" || e1.Steps["convert"] != "done" {
		t.Errorf("renamed file did not join its torrent/ledger: %+v", e1)
	}
	if _, dup := byName["Show.S01E01.1080p.WEBRip.x264-LESS.mkv"]; dup {
		t.Error("the renamed file produced two rows")
	}

	e2 := byName["Show.S01E02.1080p.WEBRip.x264-LESS.mkv"]
	if e2.OnDisk || e2.DlAt == 0 || e2.Size != 22 {
		t.Errorf("downloaded-but-absent row wrong: %+v", e2)
	}
	// No watch log here: the watch fields stay zero so the UI prints a dash.
	if e2.LastAt != 0 || e2.Sessions != 0 {
		t.Errorf("invented a watch: %+v", e2)
	}

	gone := byName["Deleted.S09E09.mp4"]
	if gone.OnDisk || gone.Steps["thumb"] != "done" {
		t.Errorf("ledger-only row missing: %+v", gone)
	}
}
