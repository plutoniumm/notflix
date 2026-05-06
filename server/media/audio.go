package media

import (
	"context"
	"net/http"

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

	tracks := []AudioTrack{}
	streams, _ := library.Prober.Streams(context.Background(), path)
	i := 0
	for _, s := range streams {
		if s.CodecType != "audio" {
			continue
		}
		tracks = append(tracks, AudioTrack{
			Track:    i,
			Language: s.Tags["language"],
			Codec:    s.CodecName,
			Channels: s.Channels,
		})
		i++
	}

	c.JSON(http.StatusOK, tracks)
}
