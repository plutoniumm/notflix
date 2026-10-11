# research

The owner rejected HCI-widget research ("a better horse") and asked for research about humans — what feels natural and what we naturalise fast — so every finding below is perception or cognition, not an interface guideline.

### Perceptual causality (launching effect)
Michotte (1946) showed that when A contacts B and B departs, observers *see* causation rather than infer it; the impression is strongest around a 30-40ms delay, weakens past 70ms, and a registered replication found it absent by 154ms.
**Built:** `--motion-fast: 130ms` in `public/assets/global.css` is the hard budget for input echo — past that window the interface reads as processing a request instead of as you having done something.
[source](https://pmc.ncbi.nlm.nih.gov/articles/PMC12434928/) · [source](https://perception.yale.edu/papers/00-Scholl-Tremoulet-TICS.pdf)

### 1/f statistics and fractal preference
Taylor et al. found aesthetic preference peaks at fractal dimension D ≈ 1.3-1.5 across natural, mathematical and Pollock fractals, and at 1/f amplitude-spectrum slopes of 1.25-1.5; the stress-reduction claims built on this are much weaker, and Bies et al. (2016) is a contrary result.
**Built:** `src/core/noise.ts` generates 1/f (Voss-McCartney) noise driving the ambient background field at `body::after` in `public/assets/global.css`, replacing a 34-second sine keyframe that got recognised as a loop and stopped being an environment.
[source](http://staff.pccu.edu.tw/~tdl/resonancebetweenartandnature.pdf) · [source](https://www.nature.com/articles/s41599-020-00648-y)

### Biological motion
Johansson (1973) showed that a small number of point-lights on the major joints is enough to read a human action, within roughly 200ms of onset — what makes motion read as alive is its shape, not its presence.
**Built:** live elements breathe on an asymmetric 40/60 inhale/exhale curve (`breath()` in `src/core/noise.ts`) rather than a symmetric pulse, which reads as a metronome.
[source](https://en.wikipedia.org/wiki/Biological_motion)

### Tool embodiment / body schema
Iriki, Tanaka & Iwamura (1996) found that macaques using a rake had parietal visuo-tactile neurons whose visual receptive fields extended along the tool — the tool absorbed into the body schema; Holmes (2012) re-analysed this and found the peripersonal-space extension contested, with small effect sizes.
**Built:** the ambient field takes its colour from the artwork under the pointer (`src/core/room.svelte.ts`), so the environment is a consequence of the user's own movement instead of an animation playing at them.
[source](https://iris.unito.it/retrieve/e27ce434-ae4b-2581-e053-d805fe0acbaa/Berti2021_Article_FromEmbodyingToolToEmbodyingAl.pdf) · [source](https://link.springer.com/article/10.3758/s13414-020-02099-8)

### Peripheral awareness
Weiser & Brown (1996), "The Coming Age of Calm Technology", located the periphery as where we are attuned without attending; Pousman & Stasko (AVI 2006) give the dimensions for designing there — information capacity, notification level, representational fidelity, aesthetic emphasis.
**Built:** the whole page's ambient field breathes while the media pipeline is working and goes faintly red when something needs a person, so machine state is legible from any page with no progress bar on it — you open Manage only once it already matters.
[source](https://faculty.cc.gatech.edu/~stasko/papers/avi06.pdf)

### Subitising
Kaufman, Lord, Reese & Volkmann (1949) named subitising: counts of about 1-4 are apprehended instantly and without counting, while larger sets must be estimated or counted.
**Built:** `src/library/ProcessStatus.svelte` replaced per-phase progress bars with unit fields, one mark per file — a bar at 60% could be 3 files or 300, a field of marks carries magnitude and progress in the same mark.
[source](https://www.jstor.org/stable/1418556)

### Graphical perception ranking
Cleveland & McGill (1984) ranked judgement accuracy: position on a common scale beats length, which beats area, which beats colour saturation.
**Built:** same file — counts get aligned unit marks, and only a single file's conversion percentage (a genuine continuous fraction) keeps a bar.
[source](https://vis.pku.edu.cn/course/visclass_f25/material/Graphical_perception.pdf)

### Common fate
Wertheimer (1923) established common fate: elements that move together are perceived as one group.
**Built:** a batch of unit marks lights as one staggered sweep (`transition-delay: calc(var(--i) * 9ms)` in `src/library/ProcessStatus.svelte`) so a finished batch reads as a single event, not ten separate blinks.
[source](https://psychclassics.yorku.ca/Wertheimer/Forms/forms.htm)

### Object permanence / spatiotemporal continuity
Spelke (1990), "Principles of object perception", shows that from infancy we expect objects to travel continuous paths and not wink in and out of existence.
**Built:** identity changes move rather than cross-fade — view transitions with a shared `view-transition-name` per artwork (`morph()` in `src/components/art.ts`), because a fade-out plus fade-in reads as two objects while the same element moving reads as one.
[source](https://onlinelibrary.wiley.com/doi/10.1207/s15516709cog1401_3)

### What this does not cover
None of this was measured on this system: the citations justify the design choices, but no user study was run here.
With one user, the OS `prefers-reduced-motion` setting is deliberately ignored at the owner's instruction.
