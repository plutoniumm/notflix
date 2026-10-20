package media

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

// Compat mode is a per-download switch: convert to H.264 + AAC instead of AV1
// so a TV decodes the file natively off /video. It's a free downgrade — the web
// player already plays MP4 — and it is decided at DOWNLOAD time: nothing
// re-encodes an existing title and no second copy is kept.
//
// Mark is `compat:<basename hash>` like codec:/skip:/poster:/blurb:, so it
// survives the moves CleanAll and Organize make — a dir-qualified key would
// silently orphan and the file would convert to AV1 anyway. Presence means on;
// clearing DELETES the key.
const compatPrefix = "compat:"

// Roughly matches the AV1 path's crf 32 in perceived quality; same value the
// share clips use.
const compatCRF = 21

func compatKey(file string) string {
	return compatPrefix + library.Hash(filepath.Base(file))
}

func IsCompat(path string) bool {
	return library.KVGetValue(compatKey(path)) != nil
}

func SetCompat(name string, on bool) error {
	if !on {
		return library.KVDelete(compatKey(name))
	}

	return library.KVSetValue(compatKey(name), true)
}

// carryCompat moves the mark onto the converted name. /api/compat is joined
// against the basename hash the video list carries, and conversion renames
// `X.mkv` to `CleanName(X).mp4`, so the mark has to follow or the badge
// vanishes off the one file that really is H.264 now.
func carryCompat(from, to string) {
	if compatKey(from) == compatKey(to) {
		return
	}

	if err := SetCompat(to, true); err != nil {
		return
	}
	SetCompat(from, false)
}

// CompatSet flips the mark for a batch of names. A rel path works as well as a
// bare basename — only the basename is ever keyed.
func CompatSet(c *gin.Context) {
	var body struct {
		Names []string `json:"names"`
		On    bool     `json:"on"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

		return
	}

	n := 0
	for _, name := range body.Names {
		if name == "" {
			continue
		}
		if err := SetCompat(name, body.On); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

			return
		}
		n++
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "n": n})
}

// CompatList mirrors BlurbList: keyed by library.Hash(basename), which is the
// `key` the video list already carries, so the frontend joins the two without
// a second lookup.
func CompatList(c *gin.Context) {
	out := map[string]bool{}
	for hash := range library.KVPrefix(compatPrefix) {
		out[hash] = true
	}

	c.JSON(http.StatusOK, out)
}
