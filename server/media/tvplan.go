package media

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

// A TV screen must not simply be handed the source file and left to cope. A
// Fire TV stick decoded a 1080p 8-bit AV1 mp4 happily and then took the whole
// browser down on a 3840x2160 10-bit AV1 one — software-decoding 4K HDR-ish AV1
// is not something that hardware can do, and the failure is an OOM that kills
// the page rather than a media error the player could catch and fall back from.
//
// So the decision moves here, where the probe data already lives. The screen
// asks what to play and gets a URL.
const (
	// A stick's comfortable ceiling. 1088 rather than 1080 because some
	// encoders pad to the macroblock boundary and a 1082-tall master is still
	// a 1080p file in every sense that matters.
	tvMaxHeight = 1088
	// 10-bit is the other half of the Obsession crash, and independently the
	// more dangerous half: an 8-bit 4K stream merely stutters, a 10-bit one
	// hits a path the hardware decoder will not take at all.
	tvMaxBitDepth = 8
)

type tvPlan struct {
	Direct bool   `json:"direct"`
	Src    string `json:"src"`
	Type   string `json:"type"`
	// Why is for the screen's diagnostics overlay, not for logic.
	Why string `json:"why,omitempty"`
}

func TVPlan(c *gin.Context, lib *library.Library) {
	rel := c.Query("file")
	path := lib.FindFile(rel)
	if path == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})

		return
	}

	enc := strings.ReplaceAll(c.Query("file"), " ", "%20")
	ladder := tvPlan{
		Src:  "/api/hls/master?file=" + enc + "&av1=0",
		Type: "application/vnd.apple.mpegurl",
	}

	if strings.ToLower(filepath.Ext(path)) != ".mp4" {
		ladder.Why = "not an mp4"
		c.JSON(http.StatusOK, ladder)

		return
	}

	// What the SCREEN says it can decode, probed with isTypeSupported against a
	// precise av01 string. Absent means "assume not" — the conservative answer
	// is a transcode, which merely costs CPU, where the optimistic one costs a
	// dead browser.
	canAV1 := c.Query("av1") == "1"
	canP10 := c.Query("p10") == "1"

	si := getSrcInfo(path)
	switch {
	// The height cap is NOT negotiable by the client, deliberately. A TV
	// browser's canPlayType will happily claim AV1 and then die on a 4K stream,
	// because what it is really answering is "do I know this codec", not "can I
	// sustain this bitrate and resolution". And the failure is an out-of-memory
	// that takes the page down, not a media error the player could catch — so
	// this is the one limit that stays ours.
	case si.vHeight > tvMaxHeight:
		ladder.Why = "too tall to decode in software"
	case si.vBitDepth > tvMaxBitDepth && !canP10:
		ladder.Why = "10-bit, and this screen did not claim 10-bit support"
	case si.vCodec == CodecAV1 && !canAV1:
		ladder.Why = "av1, and this screen did not claim av1 support"
	}
	if ladder.Why != "" {
		c.JSON(http.StatusOK, ladder)

		return
	}

	// Within the envelope: hand over the real file. No transcode, no cache, and
	// seeking is one lseek.
	c.JSON(http.StatusOK, tvPlan{
		Direct: true,
		Src:    "/video/" + enc,
		Type:   "video/mp4",
	})
}
