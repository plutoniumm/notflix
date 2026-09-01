package jobs

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"notflix/server/library"
)

// A real round trip against the configured ollama host. Off by default —
// the host is a tailnet peer that sleeps, and a test suite must not depend on
// it. Run with RECO_LIVE=1 go test ./server/jobs/ -run Live -v to check the
// dimension the current EMBED_MODEL actually returns and to eyeball the
// nearest neighbours it produces for real release names.
func TestLiveEmbedding(t *testing.T) {
	if os.Getenv("RECO_LIVE") == "" {
		t.Skip("set RECO_LIVE=1 to hit the real ollama host")
	}

	rels := []string{
		"Udta.Punjab.2016.1080p.BluRay.x264.Hindi.AAC-ETRG.mp4",
		"The.Kerala.Story.2023.1080p.WEBRip.Hin.DDP.5.1.ESub.mp4",
		"Drishyam 3 ESub.mp4",
		"The.Sopranos.S01.1080p.BluRay.x265-RARBG/The.Sopranos.S01E01.mp4",
		"Breaking Bad Season 3 Complete 720p.BRrip.Sujaidr/Breaking Bad S03E01.mp4",
		"Goodfellas.1990.1080p.BluRay.x264.mp4",
		"[Judas] Bleach 280-308 [BD 1080p][HEVC x265 10bit]/Bleach 281.mp4",
		"[Judas] JoJo no Kimyou na Bouken (Seasons 1-4)/JoJo S01E09.mp4",
		"Transformers.The.Last.Knight.2017.1080p.BluRay.x264-[YTS.AG].mp4",
		"Transformers.Age.of.Extinction.2014.1080p.mp4",
		"Seven.Samurai.1954.CRITERION.1080p.BluRay.x264.anoXmous_.mp4",
		"2000s/Gladiator.2000.1080p.BluRay.x264.mp4",
	}

	model := embedModel()
	store := newVecs(model)

	want := map[string]string{}
	for _, rel := range rels {
		want[library.Hash(rel)] = titleOf(rel)
	}

	start := time.Now()
	n := store.fill(context.Background(), want)
	took := time.Since(start)

	if n == 0 {
		t.Fatalf("%s embedded nothing — host asleep or model missing", model)
	}

	t.Logf("model=%s dim=%d embedded=%d in %v (%v/title)", model, store.Dim, n, took, took/time.Duration(n))

	byTitle := map[string]vec{}
	for k, tx := range want {
		if v, ok := store.get(k, tx); ok {
			byTitle[tx] = v
		}
	}

	centered := center(byTitle)
	for tx, v := range centered {
		if len(v) != store.Dim {
			t.Fatalf("%q has dim %d, store says %d", tx, len(v), store.Dim)
		}
	}

	titles := make([]string, 0, len(centered))
	for tx := range centered {
		titles = append(titles, tx)
	}
	sort.Strings(titles)

	for _, a := range titles {
		type hit struct {
			t string
			s float64
		}

		var hits []hit
		for _, b := range titles {
			if a == b {
				continue
			}

			hits = append(hits, hit{b, cosine(centered[a], centered[b])})
		}
		sort.Slice(hits, func(i, j int) bool { return hits[i].s > hits[j].s })

		t.Logf("%-42s → %-42s %.3f | %-42s %.3f", a, hits[0].t, hits[0].s, hits[1].t, hits[1].s)
	}
}
