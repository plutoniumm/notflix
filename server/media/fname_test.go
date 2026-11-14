package media

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// A name with '+' or '..' inside it is a name, not an escape or a traversal.
func TestGetfname(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := map[string]bool{
		"/JoJo (Seasons 1-4 + OVAs)/JoJo S01E08.mp4": true,
		"/Astro Boy/Retro..mp4":                      true,
		"/a/../../etc/passwd.mp4":                    false,
		"/clip.mkv":                                  false,
	}
	for name, ok := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Params = gin.Params{{Key: "filename", Value: name}}
		got, err := getfname(c)
		if (err == nil) != ok || (ok && got != name) {
			t.Errorf("getfname(%q) = %q, %v", name, got, err)
		}
	}

	if _, err := resolve("/srv/vid", "../vidEvil/x.mp4"); err == nil {
		t.Error("resolve let a sibling of the base through")
	}
}
