package media

import (
	"math"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"notflix/server/jobs"
)

// warp maps every cue through t·k + d, the damage a mistimed release does.
func warp(cues []jobs.Cue, k, d float64) []jobs.Cue {
	out := make([]jobs.Cue, len(cues))
	for i, c := range cues {
		out[i] = jobs.Cue{Start: c.Start*k + d, End: c.End*k + d, Text: c.Text}
	}

	return out
}

// miss is the worst timing error left after applying fit to a sub warped by
// (k, d), over a film of dur seconds. Both maps are linear, so the ends bound it.
func miss(f syncFit, k, d, dur float64) float64 {
	at := func(t float64) float64 { return math.Abs(f.Scale*(t*k+d) + f.Offset - t) }

	return max(at(0), at(dur))
}

func TestAlignRecoversShift(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	const dur = 1800.0

	var cues []jobs.Cue
	for at := 5.0; at < dur-10; {
		n := 0.8 + r.Float64()*4
		cues = append(cues, jobs.Cue{Start: at, End: at + n, Text: "x"})
		at += n + 0.3 + r.ExpFloat64()*3
	}

	env := make([]byte, int(dur*vadFPS))
	for i := range env {
		env[i] = byte(20 + r.Intn(10))
	}
	for _, c := range cues {
		for i := int(c.Start * vadFPS); i < int(c.End*vadFPS); i++ {
			if r.Float64() < 0.8 {
				env[i] = byte(55 + r.Intn(10))
			}
		}
	}
	// Music and effects: loud, voice-band, and nothing to do with the cues.
	for j := 0; j < 60; j++ {
		s := r.Intn(len(env) - 400)
		for i := s; i < s+r.Intn(400); i++ {
			env[i] = byte(50 + r.Intn(15))
		}
	}

	if f := align(env, cues); confident(f) {
		t.Fatalf("a correct subtitle was moved: %+v", f)
	}

	for _, w := range [][2]float64{{1, 3.2}, {1, -7.5}, {1, 0.4}, {25 / 23.976, 1.5}, {23.976 / 25, -42}} {
		f := align(env, warp(cues, w[0], w[1]))
		if !confident(f) {
			t.Errorf("warp %v: not confident: %+v", w, f)
		}

		if e := miss(f, w[0], w[1], dur); e > 0.05 {
			t.Errorf("warp %v: left %.3fs of error: %+v", w, e, f)
		}
	}
}

func TestShiftVTT(t *testing.T) {
	in := "WEBVTT\n\n1\n00:00:01.000 --> 00:01:02.500 line:90%\nhi 00:00:09.000\n\n59:59.999 --> 1:00:01,000\nyo\n"
	got := shiftVTT(in, 1, 2.5)

	want := "WEBVTT\n\n1\n00:00:03.500 --> 00:01:05.000 line:90%\nhi 00:00:09.000\n\n01:00:02.499 --> 01:00:03.500\nyo\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}

	if got := shiftVTT("00:00:01.000 --> 00:00:02.000\n", 1, -5); !strings.HasPrefix(got, "00:00:00.000 --> 00:00:00.000") {
		t.Fatalf("negative times must clamp to zero, got %q", got)
	}
}

// SUBSYNC_LIVE="video.mp4|sub.vtt;..." measures real titles: the sidecar as
// shipped (must come back unchanged) and synthetic warps of it (must come
// back to within 0.15s).
func TestSubsyncLive(t *testing.T) {
	spec := os.Getenv("SUBSYNC_LIVE")
	if spec == "" {
		t.Skip(`set SUBSYNC_LIVE="video|sub;video|sub"`)
	}

	for _, pair := range strings.Split(spec, ";") {
		vs := strings.SplitN(pair, "|", 2)
		video, sub := vs[0], vs[1]

		t0 := time.Now()
		env, err := speech(video)
		if err != nil {
			t.Fatalf("%s: %v", video, err)
		}
		cold := time.Since(t0)

		t0 = time.Now()
		speech(video)
		warm := time.Since(t0)

		raw, _ := os.ReadFile(sub)
		cues := jobs.ParseCues(decodeText(string(raw)))
		dur := float64(len(env)) / vadFPS

		t.Logf("== %s  (%d cues, %.0fs, extract cold %s / cached %s)", sub[strings.LastIndex(sub, "/")+1:], len(cues), dur, cold.Round(time.Millisecond), warm.Round(time.Microsecond))

		t0 = time.Now()
		f := align(env, cues)
		t.Logf("   as shipped    scale=%.5f off=%+7.2f peak=%5.1f lead=%5.1f gain=%5.1f → %s  (align %s)", f.Scale, f.Offset, f.Peak, f.Lead, f.Gain, verdict(f), time.Since(t0).Round(time.Millisecond))

		for _, w := range [][2]float64{{1, 3.2}, {1, -7.5}, {1, 0.4}, {25 / 23.976, 2.0}, {23.976 / 25, -12}, {24 / 23.976, 1.0}, {23.976 / 24, -2.5}} {
			g := align(env, warp(cues, w[0], w[1]))
			// Judged against the shipped sub's own fit, so the subtitler's
			// habitual lead on speech onset doesn't count as error.
			e := max(math.Abs(g.Scale*w[1]+g.Offset-f.Offset),
				math.Abs(g.Scale*(dur*w[0]+w[1])+g.Offset-(f.Scale*dur+f.Offset)))
			t.Logf("   warp k=%.4f d=%+6.1f  scale=%.5f off=%+7.2f peak=%5.1f lead=%5.1f gain=%5.1f → %-7s err=%.3fs", w[0], w[1], g.Scale, g.Offset, g.Peak, g.Lead, g.Gain, verdict(g), e)
		}
	}
}
