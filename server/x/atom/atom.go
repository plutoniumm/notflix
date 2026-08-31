// Package atom writes files so a reader never sees a half-written one:
// content goes to a temp file in the destination directory, then a rename
// swaps it in. A crash mid-write leaves the previous file intact instead of
// a truncated one. Leaf package — imports nothing internal, so anything can
// use it.
package atom

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}

	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}

	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err == nil {
		err = os.Chmod(tmp, perm)
	}

	if err == nil {
		err = os.Rename(tmp, path)
	}

	if err != nil {
		os.Remove(tmp)

		return err
	}

	return nil
}

func SaveJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	return WriteFile(path, data, 0644)
}

// LoadJSON reports a missing file as os.ErrNotExist and a malformed one as
// the unmarshal error. Callers must keep those apart: absent means "start
// empty", malformed means "do not overwrite". A zero-length file counts as
// absent — there is nothing in it to lose.
func LoadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, v)
}
