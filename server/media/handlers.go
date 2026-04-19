package media

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

const imgDir = "./images"

func VideoServe(c *gin.Context, lib *library.Library) {
	fname := c.Param("filename")
	root, _, ok := lib.FindRoot(fname)
	if !ok {
		c.String(http.StatusNotFound, "Video not found")
		return
	}

	VideoPlayer(c, root)
}

func VideoHeadServe(c *gin.Context, lib *library.Library) {
	fname := c.Param("filename")
	root, _, ok := lib.FindRoot(fname)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}

	VideoHead(c, root)
}

func Thumbnail(c *gin.Context, lib *library.Library) {
	path := filepath.Join(imgDir, c.Param("filename"))
	if _, err := os.Stat(path); err != nil {
		go RegenerateThumbnails(lib, 60*time.Second)
		c.Status(http.StatusNotFound)
		return
	}

	c.File(path)
}

func Conversions(c *gin.Context) {
	c.JSON(http.StatusOK, GetProgress())
}

func ProcessStart(c *gin.Context, lib *library.Library) {
	if IsProcessing() {
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": "already running"})
		return
	}

	go ProcessAll(lib)
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": "started"})
}
