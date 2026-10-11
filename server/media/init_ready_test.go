package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func box(typ string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b[0:4], uint32(len(b)))
	copy(b[4:8], typ)
	copy(b[8:], payload)
	return b
}

// HLSInit served a 0-byte init under Cache-Control: immutable because the old
// existence-only wait returned the moment ffmpeg created the file.
func TestInitComplete(t *testing.T) {
	ftyp := box("ftyp", []byte("isom0000"))
	moov := box("moov", make([]byte, 64))
	full := append(append([]byte{}, ftyp...), moov...)

	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"empty file (the bug)", nil, false},
		{"ftyp only, moov not started", ftyp, false},
		{"moov header only", append(append([]byte{}, ftyp...), moov[:8]...), false},
		{"moov truncated mid-box", full[:len(full)-8], false},
		{"complete init", full, true},
		{"trailing partial box header", append(append([]byte{}, full...), 0x00, 0x00), false},
		{"declared size smaller than a header", box("moov", nil)[:4], false},
	}
	for _, tc := range cases {
		if got := initComplete(tc.in); got != tc.want {
			t.Errorf("%s: initComplete = %v, want %v", tc.name, got, tc.want)
		}
	}

	// A real init off disk must pass, or the check is too strict to ever fire.
	for _, p := range []string{
		"../../cache/1217390/d4-av1-copy/src/v/init.m4s",
		"../../cache/0248684/d4-av1-copy/src/v/init.m4s",
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if !initComplete(b) {
			t.Errorf("real init %s rejected", filepath.Base(p))
		}
		return
	}
	t.Log("no cached init on disk to cross-check")
}

// The bug was never in initComplete — that was correct from the start. It was
// that ensureInit's fast path asked os.Stat whether the init EXISTED, so on a
// cache hit nothing called initComplete at all, and a file ffmpeg had created
// but not yet filled went out with Cache-Control: max-age=31536000.
// This pins the distinction the fast path now has to make.
func TestExistenceIsNotReadiness(t *testing.T) {
	dir := t.TempDir()
	ftyp := box("ftyp", []byte("isom0000"))

	for _, tc := range []struct {
		name  string
		bytes []byte
	}{
		{"0-byte init, as ffmpeg leaves it on create", nil},
		{"header written, moov still pending", ftyp},
	} {
		p := filepath.Join(dir, "init.m4s")
		if err := os.WriteFile(p, tc.bytes, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: os.Stat must succeed — that is the whole trap", tc.name)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if initComplete(b) {
			t.Errorf("%s: accepted as ready (%d bytes)", tc.name, len(b))
		}
	}
}
