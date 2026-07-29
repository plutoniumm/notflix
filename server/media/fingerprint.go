package media

import (
	"math"
	"math/bits"
)

// Audio fingerprinting for intro/outro detection, Haitsma-Kalker style:
// low-rate mono PCM is cut into overlapping Hann-windowed frames, each frame's
// spectrum is folded into 33 log-spaced band energies, and each frame becomes
// 32 bits — the sign of the band-energy delta along both frequency and time.
// The bits are robust to the lossy re-encodes episodes go through, so the
// same opening theme in two episodes matches with low bit error even when the
// files came from different rips.

const (
	fpRate  = 5512
	fpWin   = 1024
	fpHop   = 256 // 46ms — dense enough that two rips' arbitrary sub-hop offsets still align
	fpBands = 33
	fpMinHz = 250.0
	fpMaxHz = 2600.0
	fpGoodBits = 13   // ≤13/32 bits differ → frames considered matching (random ≈ 16)
	fpGapSec   = 2.0  // tolerated dropout inside a run (dialogue over the theme)
	fpDensity  = 0.55 // per-frame score pivot: good frames earn 1-this, bad ones lose this
	fpStrong   = 0.75 // a span must score like ≥minSec of audio matching at this rate —
	// otherwise a shorter-but-perfect stray match (one shared sentence) could
	// pad itself past the minimum length with garbage frames
)

var (
	fpFPS = float64(fpRate) / float64(fpHop)

	fpWindow = func() []float64 {
		w := make([]float64, fpWin)
		for i := range w {
			w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fpWin-1))
		}
		return w
	}()

	// fpBinBand maps FFT bin → band index (-1 = outside the analyzed range).
	fpBinBand = func() []int {
		m := make([]int, fpWin/2)
		ratio := fpMaxHz / fpMinHz
		for i := range m {
			f := float64(i) * fpRate / fpWin
			if f < fpMinHz || f >= fpMaxHz {
				m[i] = -1
				continue
			}
			m[i] = int(math.Log(f/fpMinHz) / math.Log(ratio) * fpBands)
			if m[i] >= fpBands {
				m[i] = fpBands - 1
			}
		}
		return m
	}()
)

// fft is an in-place iterative radix-2 Cooley-Tukey transform. len(re) must
// be a power of two.
func fft(re, im []float64) {
	n := len(re)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j |= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		ang := -2 * math.Pi / float64(size)
		wr, wi := math.Cos(ang), math.Sin(ang)
		for start := 0; start < n; start += size {
			cr, ci := 1.0, 0.0
			for k := start; k < start+size/2; k++ {
				m := k + size/2
				tr := re[m]*cr - im[m]*ci
				ti := re[m]*ci + im[m]*cr
				re[m], im[m] = re[k]-tr, im[k]-ti
				re[k], im[k] = re[k]+tr, im[k]+ti
				cr, ci = cr*wr-ci*wi, cr*wi+ci*wr
			}
		}
	}
}

func bandEnergies(pcm []int16, offset int) []float64 {
	re := make([]float64, fpWin)
	im := make([]float64, fpWin)
	for i := 0; i < fpWin; i++ {
		re[i] = float64(pcm[offset+i]) * fpWindow[i]
	}
	fft(re, im)

	e := make([]float64, fpBands)
	for i := 1; i < fpWin/2; i++ {
		b := fpBinBand[i]
		if b >= 0 {
			e[b] += re[i]*re[i] + im[i]*im[i]
		}
	}
	return e
}

// Fingerprint converts PCM to one uint32 per frame hop. Frame t's bit b is
// set when the (frequency, time) double-delta of band energy is positive —
// the first frame has no predecessor, so frame i covers samples starting at
// (i+1)*fpHop and its timestamp is fpTime(i).
func Fingerprint(pcm []int16) []uint32 {
	nFrames := (len(pcm) - fpWin) / fpHop
	if nFrames < 2 {
		return nil
	}

	prev := bandEnergies(pcm, 0)
	out := make([]uint32, 0, nFrames-1)
	for t := 1; t < nFrames; t++ {
		cur := bandEnergies(pcm, t*fpHop)
		var w uint32
		for b := 0; b < fpBands-1; b++ {
			if (cur[b]-cur[b+1])-(prev[b]-prev[b+1]) > 0 {
				w |= 1 << b
			}
		}
		out = append(out, w)
		prev = cur
	}
	return out
}

func fpTime(frame int) float64 {
	return float64(frame+1) * fpHop / fpRate
}

// densestSpan finds the maximum-score subarray of length ≥ minLen, where a
// good frame scores 1-fpDensity and a bad one -fpDensity. Matching audio
// (good rate well above fpDensity) accumulates score with length while
// non-matching stretches bleed it, so the optimal bounds hug the real signal
// instead of letting a strong core subsidize garbage tails — the failure
// mode of a plain min-density-over-the-span criterion.
func densestSpan(good []bool, minLen int) (start, end int, score float64) {
	n := len(good)
	if n < minLen {
		return
	}
	pre := make([]float64, n+1)
	for i, g := range good {
		v := -fpDensity
		if g {
			v = 1 - fpDensity
		}
		pre[i+1] = pre[i] + v
	}

	minVal, minPos := pre[0], 0
	for r := minLen; r <= n; r++ {
		if pre[r-minLen] < minVal {
			minVal, minPos = pre[r-minLen], r-minLen
		}
		if sc := pre[r] - minVal; sc > score {
			score, start, end = sc, minPos, r
		}
	}

	for start < end && !good[start] {
		start++
	}
	for end > start && !good[end-1] {
		end--
	}
	if end-start < minLen {
		return 0, 0, 0
	}
	return start, end, score
}

// bestRun scores the dense stretches of matching frames in `good`. The slice
// is split into segments at dropouts longer than gap frames, and the best
// span of any segment wins. Returns score 0 when nothing qualifies.
func bestRun(good []bool, gap, minLen int) (start, end int, score float64) {
	segStart, lastGood := -1, -1
	flush := func(s, e int) {
		if s < 0 || e-s < minLen {
			return
		}
		if bs, be, sc := densestSpan(good[s:e], minLen); sc > score {
			start, end, score = s+bs, s+be, sc
		}
	}
	for i, g := range good {
		if !g {
			continue
		}
		if segStart >= 0 && i-lastGood > gap {
			flush(segStart, lastGood+1)
			segStart = i
		}
		if segStart < 0 {
			segStart = i
		}
		lastGood = i
	}
	flush(segStart, lastGood+1)
	return
}

// matchFingerprints slides b against a over every alignment and returns the
// best-scoring common audio run, as half-open frame bounds in a's timeline.
// Comparing alignments by score (not run length) matters: an off-by-one-hop
// alignment still matches loosely and can produce a longer-but-sloppier run
// than the true alignment.
func matchFingerprints(a, b []uint32, minSec float64) (start, end int, ok bool) {
	minLen := int(minSec * fpFPS)
	gap := int(fpGapSec * fpFPS)
	if len(a) < minLen || len(b) < minLen {
		return 0, 0, false
	}

	best := (fpStrong - fpDensity) * float64(minLen)
	minGood := int(fpStrong * float64(minLen))
	good := make([]bool, len(a))
	for d := -(len(b) - minLen); d <= len(a)-minLen; d++ {
		lo := max(0, d)
		hi := min(len(a), len(b)+d)
		good = good[:hi-lo]
		goodCount := 0
		for i := lo; i < hi; i++ {
			g := bits.OnesCount32(a[i]^b[i-d]) <= fpGoodBits
			good[i-lo] = g
			if g {
				goodCount++
			}
		}
		if goodCount < minGood {
			continue
		}
		if s, e, sc := bestRun(good, gap, minLen); sc > best {
			start, end, best, ok = s+lo, e+lo, sc, true
		}
	}
	return
}
