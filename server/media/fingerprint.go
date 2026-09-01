package media

import (
	"math"
	"math/bits"
)

// Haitsma-Kalker audio fingerprint: low-rate mono PCM → Hann-windowed frames
// → 33 log-spaced band energies → 32 bits per frame from the sign of the
// band-energy delta. Robust to the lossy re-encodes episodes go through.

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
	fpStrong   = 0.75 // a span must score like ≥minSec matching at this rate, so a short perfect match can't pad itself out
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

// fft is in-place radix-2 Cooley-Tukey; len(re) must be a power of two.
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

// Fingerprint returns one uint32 per frame hop. Frame i has no predecessor at
// offset 0: it covers samples from (i+1)*fpHop, timestamped by fpTime(i).
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

// densestSpan maximizes score (good frame +1-fpDensity, bad -fpDensity) over
// spans ≥ minLen — under a plain min-density criterion a strong core
// subsidizes garbage tails.
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

// bestRun returns the best span across segments split at gap-length dropouts.
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

// matchFingerprints slides b over a and returns the best-scoring common run in
// a's timeline. Compare alignments by score, not run length: an off-by-one-hop
// alignment matches loosely and yields a longer but sloppier run.
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
