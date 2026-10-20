package tv

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Routes registers the bus endpoints. `play` is everything the screen needs to
// fetch media itself; it is registered by the caller, not here, because the
// media handlers live in another package.
func (b *Bus) Routes(r gin.IRoutes) {
	r.GET("/bus/events", b.events)
	r.POST("/bus/cmd", b.cmd)
	r.POST("/bus/state", b.postState)
	r.GET("/bus/state", b.getState)
}

// events is the screen's connection. SSE rather than a websocket: the traffic
// is one-way and tiny, and an EventSource reconnects on its own, which is the
// whole failure story for a TV that sleeps.
func (b *Bus) events(c *gin.Context) {
	ch, release := b.Subscribe()
	defer release()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	// gzip would buffer this into silence; the allowlist already excludes it,
	// and this header stops any intermediary doing the same.
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "no streaming")

		return
	}

	io.WriteString(c.Writer, "retry: 2000\n\n")
	flusher.Flush()

	// A proxy or a dozing TV will drop an idle connection; a comment every 20s
	// keeps it warm and costs nothing.
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()

	gone := c.Request.Context().Done()
	for {
		select {
		case raw, open := <-ch:
			if !open {
				return
			}
			io.WriteString(c.Writer, "data: ")
			c.Writer.Write(raw)
			io.WriteString(c.Writer, "\n\n")
			flusher.Flush()
		case <-tick.C:
			io.WriteString(c.Writer, ": ping\n\n")
			flusher.Flush()
		case <-gone:
			return
		}
	}
}

func (b *Bus) cmd(c *gin.Context) {
	var in Cmd
	if err := c.ShouldBindJSON(&in); err != nil || in.Cmd == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad command"})

		return
	}

	sent := b.Send(in)
	if sent == 0 {
		log.Printf("[tv] %q with no screen listening", in.Cmd)
	}

	c.JSON(http.StatusOK, gin.H{"screens": sent})
}

func (b *Bus) postState(c *gin.Context) {
	var s State
	if err := c.ShouldBindJSON(&s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad state"})

		return
	}
	b.SetState(s)
	c.Status(http.StatusNoContent)
}

// getState long-polls when given ?rev=N: it holds until the screen reports
// something newer, so the remote's scrubber tracks without a busy poll. Caps
// out at 25s and answers anyway, which keeps it inside every proxy's idle
// timeout.
func (b *Bus) getState(c *gin.Context) {
	want := c.Query("rev")
	if want != "" {
		var rev uint64
		if err := json.Unmarshal([]byte(want), &rev); err == nil {
			deadline := time.After(25 * time.Second)
			for b.Rev() == rev {
				select {
				case <-time.After(200 * time.Millisecond):
				case <-deadline:
					goto answer
				case <-c.Request.Context().Done():
					return
				}
			}
		}
	}

answer:
	c.JSON(http.StatusOK, gin.H{"rev": b.Rev(), "state": b.State()})
}
