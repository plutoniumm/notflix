// 1/f noise and a respiration curve — the two shapes the ambient room moves on.
// Kept rune-free so `node --test` can import it directly.

const OCT = 6;

// Voss-McCartney: octave k is re-rolled every 2^k ticks, so the summed series
// has power ~1/f without an FFT or a ring buffer. Natural light, wind and
// water all carry 1/f amplitude spectra; a sine loop does not, and the eye
// identifies a loop within about two cycles.
export class Pink {
  private rows: number[];
  private t = 0;
  // A plain field, not a constructor parameter property: node --test strips
  // types only and refuses that syntax.
  private rand: () => number;

  constructor(rand: () => number = Math.random) {
    this.rand = rand;
    this.rows = Array.from({ length: OCT }, () => rand() * 2 - 1);
  }

  next(): number {
    this.t++;
    for (let k = 0; k < OCT; k++) {
      if (this.t % (1 << k)) continue;
      this.rows[k] = this.rand() * 2 - 1;
    }

    let s = 0;
    for (let k = 0; k < OCT; k++) s += this.rows[k];

    return s / OCT;
  }
}

// Resting human respiration. Asymmetric on purpose: a symmetric in/out reads
// as a metronome, 40/60 reads as something alive.
export const BREATH_S = 3.6;
const IN = 0.4;

export function breath(t: number): number {
  const p = (((t / BREATH_S) % 1) + 1) % 1;
  const ramp = p < IN ? p / IN : 1 - (p - IN) / (1 - IN);

  return (1 - Math.cos(ramp * Math.PI)) / 2;
}

export const lerp = (a: number, b: number, k: number) => a + (b - a) * k;
