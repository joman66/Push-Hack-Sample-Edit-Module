// Package audio holds the sample buffer and every destructive edit on it.
// Each edit is one "replace a range with new data" step, so undo and redo
// store only what changed, not a copy of the whole sample.
package audio

import (
	"errors"
	"math"
)

// Buffer is planar float audio: Ch[c][frame], 1 or 2 channels.
type Buffer struct {
	Rate int
	Ch   [][]float32
}

// New makes a silent buffer.
func New(rate, channels, frames int) *Buffer {
	b := &Buffer{Rate: rate, Ch: make([][]float32, channels)}
	for i := range b.Ch {
		b.Ch[i] = make([]float32, frames)
	}
	return b
}

// Frames is the length in frames.
func (b *Buffer) Frames() int {
	if len(b.Ch) == 0 {
		return 0
	}
	return len(b.Ch[0])
}

// Clone copies the buffer.
func (b *Buffer) Clone() *Buffer {
	c := &Buffer{Rate: b.Rate, Ch: make([][]float32, len(b.Ch))}
	for i := range b.Ch {
		c.Ch[i] = append([]float32(nil), b.Ch[i]...)
	}
	return c
}

// Mono mixes all channels down to one slice (a new slice).
func (b *Buffer) Mono() []float32 {
	n := b.Frames()
	out := make([]float32, n)
	for _, c := range b.Ch {
		for i, v := range c {
			out[i] += v
		}
	}
	if len(b.Ch) > 1 {
		k := 1 / float32(len(b.Ch))
		for i := range out {
			out[i] *= k
		}
	}
	return out
}

// Range is a half-open frame range [Start, End).
type Range struct{ Start, End int }

// Len is End-Start.
func (r Range) Len() int { return r.End - r.Start }

// Clamp fits the range into [0, n] and orders it.
func (r Range) Clamp(n int) Range {
	if r.Start > r.End {
		r.Start, r.End = r.End, r.Start
	}
	if r.Start < 0 {
		r.Start = 0
	}
	if r.End > n {
		r.End = n
	}
	if r.Start > r.End {
		r.Start = r.End
	}
	return r
}

type edit struct {
	start    int
	old, new [][]float32
}

func (e edit) bytes() int {
	n := 0
	for _, c := range e.old {
		n += len(c) * 4
	}
	for _, c := range e.new {
		n += len(c) * 4
	}
	return n
}

type step []edit

func (s step) bytes() int {
	n := 0
	for _, e := range s {
		n += e.bytes()
	}
	return n
}

// MaxHistoryBytes caps the memory the undo history may use.
const MaxHistoryBytes = 96 << 20

// ErrEmpty is returned when an edit has nothing to act on.
var ErrEmpty = errors.New("nothing selected")

// Doc is a buffer plus its edit history and clipboard.
type Doc struct {
	Buf       *Buffer
	Clipboard [][]float32
	undo      []step
	redo      []step
	cur       step // edits of the step being built
	grouping  bool
}

// NewDoc wraps a buffer.
func NewDoc(b *Buffer) *Doc { return &Doc{Buf: b} }

func copyRange(ch [][]float32, s, e int) [][]float32 {
	out := make([][]float32, len(ch))
	for i := range ch {
		out[i] = append([]float32(nil), ch[i][s:e]...)
	}
	return out
}

// splice replaces [s,e) of every channel with nw (same channel count).
func (d *Doc) splice(s, e int, nw [][]float32) {
	for i := range d.Buf.Ch {
		c := d.Buf.Ch[i]
		out := make([]float32, 0, len(c)-(e-s)+len(nw[i]))
		out = append(out, c[:s]...)
		out = append(out, nw[i]...)
		out = append(out, c[e:]...)
		d.Buf.Ch[i] = out
	}
}

// replace applies one edit and records it.
func (d *Doc) replace(s, e int, nw [][]float32) {
	old := copyRange(d.Buf.Ch, s, e)
	d.splice(s, e, nw)
	d.cur = append(d.cur, edit{start: s, old: old, new: nw})
	if !d.grouping {
		d.commit()
	}
}

func (d *Doc) commit() {
	if len(d.cur) == 0 {
		return
	}
	d.undo = append(d.undo, d.cur)
	d.cur = nil
	d.redo = nil
	total := 0
	for _, s := range d.undo {
		total += s.bytes()
	}
	for total > MaxHistoryBytes && len(d.undo) > 1 {
		total -= d.undo[0].bytes()
		d.undo = d.undo[1:]
	}
}

// Group runs fn so that all its edits undo as one step.
func (d *Doc) Group(fn func()) {
	d.grouping = true
	fn()
	d.grouping = false
	d.commit()
}

// CanUndo and CanRedo tell the UI whether the Undo button does anything.
func (d *Doc) CanUndo() bool { return len(d.undo) > 0 }
func (d *Doc) CanRedo() bool { return len(d.redo) > 0 }

// Undo steps back one edit step.
func (d *Doc) Undo() bool {
	if len(d.undo) == 0 {
		return false
	}
	st := d.undo[len(d.undo)-1]
	d.undo = d.undo[:len(d.undo)-1]
	for i := len(st) - 1; i >= 0; i-- {
		e := st[i]
		d.splice(e.start, e.start+len(e.new[0]), e.old)
	}
	d.redo = append(d.redo, st)
	return true
}

// Redo steps forward again.
func (d *Doc) Redo() bool {
	if len(d.redo) == 0 {
		return false
	}
	st := d.redo[len(d.redo)-1]
	d.redo = d.redo[:len(d.redo)-1]
	for _, e := range st {
		d.splice(e.start, e.start+len(e.old[0]), e.new)
	}
	d.undo = append(d.undo, st)
	return true
}

func (d *Doc) sel(r Range) (Range, error) {
	r = r.Clamp(d.Buf.Frames())
	if r.Len() == 0 {
		return r, ErrEmpty
	}
	return r, nil
}

// Copy puts the range on the clipboard.
func (d *Doc) Copy(r Range) error {
	r, err := d.sel(r)
	if err != nil {
		return err
	}
	d.Clipboard = copyRange(d.Buf.Ch, r.Start, r.End)
	return nil
}

// Cut copies the range, then removes it and closes the gap.
func (d *Doc) Cut(r Range) error {
	r, err := d.sel(r)
	if err != nil {
		return err
	}
	d.Clipboard = copyRange(d.Buf.Ch, r.Start, r.End)
	d.replace(r.Start, r.End, make([][]float32, len(d.Buf.Ch)))
	return nil
}

func (d *Doc) clip() ([][]float32, error) {
	if len(d.Clipboard) == 0 || len(d.Clipboard[0]) == 0 {
		return nil, ErrEmpty
	}
	// Match the clipboard to the buffer's channel count.
	out := make([][]float32, len(d.Buf.Ch))
	for i := range out {
		src := d.Clipboard[min(i, len(d.Clipboard)-1)]
		out[i] = append([]float32(nil), src...)
	}
	return out, nil
}

// PasteOverwrite writes the clipboard at pos over existing audio. It grows
// the buffer only when the clip runs past the end.
func (d *Doc) PasteOverwrite(pos int) error {
	cl, err := d.clip()
	if err != nil {
		return err
	}
	n := d.Buf.Frames()
	pos = max(0, min(pos, n))
	end := min(pos+len(cl[0]), n)
	if pos+len(cl[0]) > n {
		end = n
	}
	d.replace(pos, end, cl)
	return nil
}

// PasteInsert inserts the clipboard at pos and shifts the rest right.
func (d *Doc) PasteInsert(pos int) error {
	cl, err := d.clip()
	if err != nil {
		return err
	}
	pos = max(0, min(pos, d.Buf.Frames()))
	d.replace(pos, pos, cl)
	return nil
}

// Crop deletes everything outside the range.
func (d *Doc) Crop(r Range) error {
	r, err := d.sel(r)
	if err != nil {
		return err
	}
	d.Group(func() {
		n := d.Buf.Frames()
		if r.End < n {
			d.replace(r.End, n, make([][]float32, len(d.Buf.Ch)))
		}
		if r.Start > 0 {
			d.replace(0, r.Start, make([][]float32, len(d.Buf.Ch)))
		}
	})
	return nil
}

// Delete removes the range and closes the gap (no clipboard).
func (d *Doc) Delete(r Range) error {
	r, err := d.sel(r)
	if err != nil {
		return err
	}
	d.replace(r.Start, r.End, make([][]float32, len(d.Buf.Ch)))
	return nil
}

// Map rewrites the range in place with fn, which gets a copy of each
// channel's slice and must return the same length.
func (d *Doc) Map(r Range, fn func(ch int, x []float32)) error {
	r, err := d.sel(r)
	if err != nil {
		return err
	}
	nw := copyRange(d.Buf.Ch, r.Start, r.End)
	for i := range nw {
		fn(i, nw[i])
	}
	d.replace(r.Start, r.End, nw)
	return nil
}

// Transform replaces the range with fn's result, which may be longer or
// shorter (time stretch, pitch shift).
func (d *Doc) Transform(r Range, fn func(in [][]float32) [][]float32) error {
	r, err := d.sel(r)
	if err != nil {
		return err
	}
	in := copyRange(d.Buf.Ch, r.Start, r.End)
	out := fn(in)
	if len(out) != len(in) {
		return errors.New("transform changed channel count")
	}
	d.replace(r.Start, r.End, out)
	return nil
}

// Silence zeroes the range without changing the length.
func (d *Doc) Silence(r Range) error {
	return d.Map(r, func(_ int, x []float32) {
		for i := range x {
			x[i] = 0
		}
	})
}

// Reverse flips the range backwards.
func (d *Doc) Reverse(r Range) error {
	return d.Map(r, func(_ int, x []float32) {
		for i, j := 0, len(x)-1; i < j; i, j = i+1, j-1 {
			x[i], x[j] = x[j], x[i]
		}
	})
}

// MinDB is the floor: at or below it gain means silence (-inf).
const MinDB = -96.0

// MaxGainDB is the largest boost.
const MaxGainDB = 24.0

// DBToLin converts dB to a gain factor. MinDB and below give 0.
func DBToLin(db float64) float64 {
	if db <= MinDB {
		return 0
	}
	return math.Pow(10, db/20)
}

// Gain applies a gain in dB (MinDB = -inf up to +24 dB).
func (d *Doc) Gain(r Range, db float64) error {
	db = math.Min(db, MaxGainDB)
	g := float32(DBToLin(db))
	return d.Map(r, func(_ int, x []float32) {
		for i := range x {
			x[i] *= g
		}
	})
}

// NormMode picks what Normalize measures.
type NormMode int

const (
	NormPeak NormMode = iota
	NormRMS
)

// Measure returns the peak and RMS (over all channels) of a range.
func (b *Buffer) Measure(r Range) (peak, rms float64) {
	r = r.Clamp(b.Frames())
	var sum float64
	n := 0
	for _, c := range b.Ch {
		for _, v := range c[r.Start:r.End] {
			a := math.Abs(float64(v))
			if a > peak {
				peak = a
			}
			sum += float64(v) * float64(v)
			n++
		}
	}
	if n > 0 {
		rms = math.Sqrt(sum / float64(n))
	}
	return
}

// NormResult says what Normalize did.
type NormResult struct {
	GainDB  float64
	Limited bool // gain was cut so the peak stays at or under 0 dBFS
}

// Normalize scales the range so its peak or RMS hits targetDB (0 = full
// scale). For RMS the gain is cut when it would push the peak over 0 dBFS.
func (d *Doc) Normalize(r Range, mode NormMode, targetDB float64) (NormResult, error) {
	r, err := d.sel(r)
	if err != nil {
		return NormResult{}, err
	}
	peak, rms := d.Buf.Measure(r)
	meas := peak
	if mode == NormRMS {
		meas = rms
	}
	if meas < 1e-9 {
		return NormResult{}, errors.New("silent: nothing to normalize")
	}
	target := math.Pow(10, targetDB/20)
	g := target / meas
	res := NormResult{}
	if g*peak > 1 && targetDB <= 0 {
		g = 1 / peak
		res.Limited = true
	}
	res.GainDB = 20 * math.Log10(g)
	gf := float32(g)
	err = d.Map(r, func(_ int, x []float32) {
		for i := range x {
			x[i] *= gf
		}
	})
	return res, err
}

// Curve is a fade shape.
type Curve int

const (
	CurveLinear Curve = iota
	CurveExp          // starts slow, ends fast (fade in)
	CurveLog          // starts fast, ends slow (fade in)
)

func (c Curve) String() string { return [...]string{"Lin", "Exp", "Log"}[c] }

// fadeGain is the fade-in gain at t in [0,1].
func fadeGain(c Curve, t float64) float64 {
	switch c {
	case CurveExp:
		const k = 4.0
		return (math.Exp(k*t) - 1) / (math.Exp(k) - 1)
	case CurveLog:
		const k = 9.0
		return math.Log(1+k*t) / math.Log(1+k)
	}
	return t
}

// Fade applies a fade in or out across the range.
func (d *Doc) Fade(r Range, in bool, c Curve) error {
	return d.Map(r, func(_ int, x []float32) {
		n := len(x)
		for i := range x {
			t := float64(i) / float64(max(n-1, 1))
			if !in {
				t = 1 - t
			}
			x[i] *= float32(fadeGain(c, t))
		}
	})
}

// AutoTrim cuts leading and trailing audio below thresholdDB. It returns
// the frames removed from the start and the end.
func (d *Doc) AutoTrim(thresholdDB float64) (head, tail int, err error) {
	th := float32(math.Pow(10, thresholdDB/20))
	n := d.Buf.Frames()
	first, last := -1, -1
	for i := 0; i < n && first < 0; i++ {
		for _, c := range d.Buf.Ch {
			if abs32(c[i]) > th {
				first = i
				break
			}
		}
	}
	if first < 0 {
		return 0, 0, errors.New("all audio is below the threshold")
	}
	for i := n - 1; i >= 0 && last < 0; i-- {
		for _, c := range d.Buf.Ch {
			if abs32(c[i]) > th {
				last = i + 1
				break
			}
		}
	}
	if first == 0 && last == n {
		return 0, 0, nil
	}
	if err = d.Crop(Range{first, last}); err != nil {
		return 0, 0, err
	}
	return first, n - last, nil
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// SnapZero returns the zero crossing nearest to pos within maxDist frames
// (positive-going preferred on a tie). If none is found it returns pos.
func (b *Buffer) SnapZero(pos, maxDist int) int {
	m := b.Mono()
	n := len(m)
	if n < 2 {
		return pos
	}
	pos = max(0, min(pos, n-1))
	for d := 0; d <= maxDist; d++ {
		for _, p := range [2]int{pos + d, pos - d} {
			if p < 1 || p >= n {
				continue
			}
			if (m[p-1] <= 0 && m[p] > 0) || (m[p-1] < 0 && m[p] >= 0) {
				return p
			}
		}
		for _, p := range [2]int{pos + d, pos - d} {
			if p < 1 || p >= n {
				continue
			}
			if (m[p-1] >= 0 && m[p] < 0) || (m[p-1] > 0 && m[p] <= 0) {
				return p
			}
		}
	}
	return pos
}
