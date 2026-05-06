package media

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const osUA = "notflix v1.0.0"

// OpenSubtitlesClient wraps auth + search + download against the OS REST API.
// Token is cached in-memory for ~23h and refreshed on demand.
type OpenSubtitlesClient struct {
	apiKey string
	user   string
	pass   string

	mu     sync.Mutex
	tokVal string
	tokExp time.Time
}

func NewOpenSubtitlesClient() *OpenSubtitlesClient {
	return &OpenSubtitlesClient{
		apiKey: os.Getenv("OPENSUBTITLES_API_KEY"),
		user:   os.Getenv("OPENSUBTITLES_USER"),
		pass:   os.Getenv("OPENSUBTITLES_PASS"),
	}
}

var osClient = NewOpenSubtitlesClient()

func (c *OpenSubtitlesClient) Enabled() bool { return c.apiKey != "" }

// Search runs the OS subtitles query (raw urlencoded params, e.g. "moviehash=...").
// Returns an empty slice on any failure; logs go to stdout.
func (c *OpenSubtitlesClient) Search(params string, byHash bool) []SubResult {
	if c.apiKey == "" {
		return nil
	}

	req, err := http.NewRequest("GET", "https://api.opensubtitles.com/api/v1/subtitles?"+params, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", osUA)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[subs] osSearch request error: %v", err)
		return nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("[subs] osSearch status=%d body=%s params=%q", resp.StatusCode, string(data), params)
		return nil
	}

	var payload struct {
		Data []struct {
			Attributes struct {
				Language      string `json:"language"`
				Release       string `json:"release"`
				DownloadCount int    `json:"download_count"`
				Files         []struct {
					FileID int `json:"file_id"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("[subs] osSearch parse error: %v body=%s", err, string(data))
		return nil
	}

	out := make([]SubResult, 0, len(payload.Data))
	for _, item := range payload.Data {
		a := item.Attributes
		fid := 0
		if len(a.Files) > 0 {
			fid = a.Files[0].FileID
		}
		out = append(out, SubResult{
			Provider:      "os",
			FileID:        fid,
			Language:      a.Language,
			Release:       a.Release,
			DownloadCount: a.DownloadCount,
			HashMatch:     byHash,
		})
	}
	return out
}

// token returns a cached login token, or empty string if creds are missing or
// login fails. Tokens are cached for 23 hours.
func (c *OpenSubtitlesClient) token() string {
	if c.user == "" || c.pass == "" {
		return ""
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tokVal != "" && time.Now().Before(c.tokExp) {
		return c.tokVal
	}

	body, _ := json.Marshal(map[string]string{"username": c.user, "password": c.pass})
	req, err := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/login", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", osUA)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[subs] login request failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("[subs] login failed: status=%d body=%s", resp.StatusCode, string(data))
		return ""
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		log.Printf("[subs] login parse error: %v", err)
		return ""
	}

	log.Printf("[subs] login ok, token acquired")
	c.tokVal = result.Token
	c.tokExp = time.Now().Add(23 * time.Hour)
	return c.tokVal
}

// Download asks OS for a download URL for the given file_id, retrying up to 3
// times on 429/503. Returns (link, filename, "") on success, or
// ("", "", message) describing the failure.
func (c *OpenSubtitlesClient) Download(fileID int) (link, filename, errMsg string) {
	tok := c.token()
	rb, _ := json.Marshal(map[string]int{"file_id": fileID})

	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		req, err := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/download", bytes.NewReader(rb))
		if err != nil {
			return "", "", "request error"
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Api-Key", c.apiKey)
		}
		req.Header.Set("User-Agent", osUA)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[subs] download request error (attempt %d): %v", attempt+1, err)
			errMsg = "download request failed"
			continue
		}

		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("[subs] OS download attempt=%d status=%d body=%s", attempt+1, resp.StatusCode, string(data))

		if resp.StatusCode == http.StatusOK {
			var dl struct {
				Link     string `json:"link"`
				FileName string `json:"file_name"`
			}
			if err := json.Unmarshal(data, &dl); err != nil || dl.Link == "" {
				return "", "", "invalid download response"
			}
			return dl.Link, dl.FileName, ""
		}

		var osErr struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		}
		errMsg = fmt.Sprintf("OpenSubtitles error (HTTP %d)", resp.StatusCode)
		if json.Unmarshal(data, &osErr) == nil && osErr.Message != "" {
			errMsg = osErr.Message
			if len(osErr.Errors) > 0 {
				errMsg += ": " + strings.Join(osErr.Errors, "; ")
			}
		}

		if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusTooManyRequests {
			break
		}
	}

	return "", "", errMsg
}
