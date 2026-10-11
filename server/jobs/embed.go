package jobs

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"notflix/server/x/atom"
	"notflix/server/x/httpc"
)

// Title vectors live in their own file, NOT in the KV store. The store is one
// whole-file JSON blob rewritten under an exclusive lock on every set, and a
// watch ping is a set — parking a few thousand floats per title there would
// make every progress save rewrite megabytes. This file is written only when
// the catalogue gains titles.
//
// The encoding is raw float32, not JSON numbers: a 3584-dim vector is 14 KB
// binary against ~45 KB as text, and the whole store stays a single mmap-able
// read. Records key on library.Hash(basename) exactly like codec:/skip: — a
// dir-qualified key orphans the moment CleanAll or Organize moves the file.
const vecPath = "./cache/embed.bin"

// Two models' vectors are different spaces; cosine between them is noise that
// reads as "the recommendations are just bad" rather than as an error. The
// model name and the dimension are stamped in the header and a mismatch drops
// the whole store rather than mixing.
var vecMagic = [4]byte{'N', 'F', 'E', '1'}

// A sane ceiling on dimensionality, so a corrupt header can't ask for a
// gigabyte-sized allocation before the length checks catch it.
const vecMaxDim = 16384

const embedDefault = "nomic-embed-text:latest"

func embedModel() string {
	if m := os.Getenv("EMBED_MODEL"); m != "" {
		return m
	}

	return embedDefault
}

// Embedding a batch on a 12B embedder is far slower than on a 137M one, and
// the host is a tailnet peer that may be asleep, so the deadline is generous
// but always present.
const embedTimeout = 4 * time.Minute

// One request carries at most this many titles. httpc caps a response at 8 MB
// and a 3584-dim vector is ~45 KB of JSON, so a whole-library batch would be
// silently truncated into a decode error.
const embedBatch = 24

type vec []float32

type embRec struct {
	Text string
	V    vec
}

// vecs is the on-disk vector cache plus the identity of the model that
// produced it. Zero value is an empty store for the current model.
type vecs struct {
	mu    sync.Mutex
	Model string
	Dim   int
	Rec   map[string]embRec
}

func newVecs(model string) *vecs {
	return &vecs{Model: model, Rec: map[string]embRec{}}
}

func (s *vecs) get(key, text string) (vec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.Rec[key]
	// A cleaning change rewrites the title, which makes the cached vector an
	// answer to a question nobody asked any more.
	if !ok || r.Text != text || len(r.V) != s.Dim {
		return nil, false
	}

	return r.V, true
}

func (s *vecs) put(key, text string, v vec) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Dim == 0 {
		s.Dim = len(v)
	}

	if len(v) != s.Dim {
		return
	}

	s.Rec[key] = embRec{Text: text, V: v}
}

func (s *vecs) encode() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	buf := make([]byte, 0, 16+len(s.Model)+len(s.Rec)*(8+s.Dim*4))
	buf = append(buf, vecMagic[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(s.Dim))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(s.Rec)))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(s.Model)))
	buf = append(buf, s.Model...)

	for k, r := range s.Rec {
		buf = binary.LittleEndian.AppendUint16(buf, uint16(len(k)))
		buf = append(buf, k...)
		buf = binary.LittleEndian.AppendUint16(buf, uint16(len(r.Text)))
		buf = append(buf, r.Text...)

		for _, f := range r.V {
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(f))
		}
	}

	return buf
}

func (s *vecs) save(path string) error {
	return atom.WriteFile(path, s.encode(), 0644)
}

// cursor reads the flat encoding with a bounds check at every step, so a
// truncated or corrupt file returns an error instead of panicking on a slice.
type cursor struct {
	b []byte
	i int
}

func (c *cursor) take(n int) ([]byte, error) {
	if n < 0 || c.i+n > len(c.b) {
		return nil, errors.New("short read")
	}

	out := c.b[c.i : c.i+n]
	c.i += n

	return out, nil
}

func (c *cursor) u16() (int, error) {
	b, err := c.take(2)
	if err != nil {
		return 0, err
	}

	return int(binary.LittleEndian.Uint16(b)), nil
}

func (c *cursor) u32() (int, error) {
	b, err := c.take(4)
	if err != nil {
		return 0, err
	}

	return int(binary.LittleEndian.Uint32(b)), nil
}

// loadVecs returns a store for model. Anything it cannot vouch for — missing
// file, wrong magic, a different model, an implausible dimension, a truncated
// tail — comes back as an empty store for the current model, because
// re-embedding is cheap and a wrong vector is invisible.
func loadVecs(path, model string) *vecs {
	raw, err := os.ReadFile(path)
	if err != nil {
		return newVecs(model)
	}

	c := &cursor{b: raw}
	magic, err := c.take(4)
	if err != nil || [4]byte(magic) != vecMagic {
		return newVecs(model)
	}

	dim, err := c.u32()
	if err != nil || dim <= 0 || dim > vecMaxDim {
		return newVecs(model)
	}

	count, err := c.u32()
	if err != nil {
		return newVecs(model)
	}

	mlen, err := c.u16()
	if err != nil {
		return newVecs(model)
	}

	mb, err := c.take(mlen)
	if err != nil || string(mb) != model {
		if err == nil {
			log.Printf("[reco] embed model changed %q → %q; dropping %d cached vectors", string(mb), model, count)
		}

		return newVecs(model)
	}

	s := newVecs(model)
	s.Dim = dim

	for range count {
		klen, err := c.u16()
		if err != nil {
			return newVecs(model)
		}

		kb, err := c.take(klen)
		if err != nil {
			return newVecs(model)
		}

		tlen, err := c.u16()
		if err != nil {
			return newVecs(model)
		}

		tb, err := c.take(tlen)
		if err != nil {
			return newVecs(model)
		}

		vb, err := c.take(dim * 4)
		if err != nil {
			return newVecs(model)
		}

		v := make(vec, dim)
		for i := range v {
			v[i] = math.Float32frombits(binary.LittleEndian.Uint32(vb[i*4:]))
		}

		s.Rec[string(kb)] = embRec{Text: string(tb), V: v}
	}

	return s
}

// embedOnce asks ollama for one vector per input, in order.
func embedOnce(ctx context.Context, model string, in []string) ([]vec, error) {
	req := map[string]any{
		"model":      model,
		"input":      in,
		"keep_alive": ollamaKeepAlive,
	}

	var resp struct {
		Embeddings []vec  `json:"embeddings"`
		Error      string `json:"error"`
	}
	if err := httpc.PostJSON(ctx, ollamaHost()+"/api/embed", req, &resp, embedTimeout); err != nil {
		return nil, err
	}

	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	if len(resp.Embeddings) != len(in) {
		return nil, fmt.Errorf("embed: asked %d, got %d", len(in), len(resp.Embeddings))
	}

	return resp.Embeddings, nil
}

// fill embeds every (key, text) pair missing from the store, in batches.
// It returns the number newly embedded; a failing batch is logged and skipped
// so one bad title — or a host that fell asleep mid-run — costs its batch and
// not the run.
func (s *vecs) fill(ctx context.Context, want map[string]string) int {
	type job struct{ key, text string }

	var todo []job
	for k, t := range want {
		if t == "" {
			continue
		}

		if _, ok := s.get(k, t); !ok {
			todo = append(todo, job{k, t})
		}
	}

	if len(todo) == 0 {
		return 0
	}

	model := s.Model
	done := 0

	for i := 0; i < len(todo); i += embedBatch {
		if ctx.Err() != nil {
			break
		}

		end := min(i+embedBatch, len(todo))
		batch := todo[i:end]

		texts := make([]string, len(batch))
		for j, b := range batch {
			texts[j] = b.text
		}

		got, err := embedOnce(ctx, model, texts)
		if err != nil {
			log.Printf("[reco] embed batch %d: %v", i/embedBatch, err)

			continue
		}

		for j, v := range got {
			// The first vector back defines the dimension for this model.
			// Anything of a different length is from a different space and
			// must never reach a cosine.
			if len(v) == 0 || (s.Dim != 0 && len(v) != s.Dim) {
				log.Printf("[reco] %s returned dim %d, store is %d — dropping cache", model, len(v), s.Dim)
				s.reset()

				return done
			}

			s.put(batch[j].key, batch[j].text, v)
			done++
		}
	}

	return done
}

func (s *vecs) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Dim = 0
	s.Rec = map[string]embRec{}
}

// unit copies v scaled to length 1. A zero vector stays zero — cosine against
// it is 0, which is what "no signal" should score.
func unit(v vec) vec {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}

	if sum == 0 {
		return append(vec(nil), v...)
	}

	inv := float32(1 / math.Sqrt(sum))
	out := make(vec, len(v))
	for i, x := range v {
		out[i] = x * inv
	}

	return out
}

// cosine of two unit vectors. Differing lengths score 0 rather than panicking
// or silently comparing a prefix: a truncated dot product produces a plausible
// number, which is the worst possible failure here.
func cosine(a, b vec) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}

	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}

	return sum
}

// centerVecs subtracts the catalogue's mean vector and renormalises. Embeddings of
// short titles all share a large "this is a video title" component: raw cosine
// over this library sits at a median of 0.55 with everything crushed into
// 0.45–0.9, so no threshold separates a real match from a coincidence.
// Removing the mean spreads the same ranking over roughly -0.35 to 0.75.
func centerVecs(in map[string]vec) map[string]vec {
	out := make(map[string]vec, len(in))
	if len(in) == 0 {
		return out
	}

	// The reference dimension must be the MAJORITY one, never whichever key Go
	// hands back first: map order is random, so one stray vector from an old
	// model's space could define dim and every good vector was then dropped as
	// ragged — a 1-in-3 flake in TestCenterEmptyAndRagged, and in production a
	// silent purge of the whole store in favour of the stray.
	counts := map[int]int{}
	for _, v := range in {
		counts[len(v)]++
	}

	dim, best := 0, 0
	for d, c := range counts {
		// Ties break toward the longer space so the choice is deterministic
		// regardless of map order.
		if c > best || (c == best && d > dim) {
			dim, best = d, c
		}
	}

	mean := make([]float64, dim)
	n := 0

	for _, v := range in {
		if len(v) != dim {
			continue
		}

		for i, x := range v {
			mean[i] += float64(x)
		}
		n++
	}

	if n == 0 {
		return out
	}

	for i := range mean {
		mean[i] /= float64(n)
	}

	for k, v := range in {
		if len(v) != dim {
			continue
		}

		c := make(vec, dim)
		for i, x := range v {
			c[i] = x - float32(mean[i])
		}

		out[k] = unit(c)
	}

	return out
}

func embedReady() bool {
	return strings.TrimSpace(ollamaHost()) != ""
}
