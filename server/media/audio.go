package media

import (
	"encoding/json"
	"net/http"
	"os/exec"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

type AudioTrack struct {
	Track    int    `json:"track"`
	Language string `json:"language"`
	Codec    string `json:"codec"`
	Channels int    `json:"channels"`
}

func AudioInfo(c *gin.Context, lib *library.Library) {
	raw := c.Query("file")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	path, ok := lib.FindVid(raw)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "a",
		"-show_entries", "stream=codec_name,channels:stream_tags=language",
		"-of", "json",
		path,
	)
	out, _ := cmd.Output()

	var tracks []AudioTrack
	var probe struct {
		Streams []struct {
			CodecName string            `json:"codec_name"`
			Channels  int               `json:"channels"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &probe) == nil {
		for i, s := range probe.Streams {
			lang := ""
			if s.Tags != nil {
				lang = s.Tags["language"]
			}
			tracks = append(tracks, AudioTrack{
				Track:    i,
				Language: lang,
				Codec:    s.CodecName,
				Channels: s.Channels,
			})
		}
	}

	if tracks == nil {
		tracks = []AudioTrack{}
	}
	c.JSON(http.StatusOK, tracks)
}
