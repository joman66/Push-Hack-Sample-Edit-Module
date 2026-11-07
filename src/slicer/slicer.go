// Package slicer finds slice points: by transients, by a musical grid, or
// in equal parts. It also estimates tempo from the onsets.
package slicer

import (
	"math"
	"sort"
)

// MaxSlices is the number of pads on the grid.
const MaxSlices = 64

// Slice is one region with its drum-rack assignments.
type Slice struct {
	Start, End int
	Choke      int // 0 = none, 1-8 = mute group
	VelLo      int // velocity range, 1-127
	VelHi      int
}

const (
	hop = 256
	win = 512
)

// envelope is the high-frequency energy per hop, which rises on a hit.
func envelope(x []float32) []float64 {
	if len(x) < win {
		return nil
	}
	nf := (len(x)-win)/hop + 1
	e := make([]float64, nf)
	for i := range e {
		var s float64
		base := i * hop
		for j := 1; j < win; j++ {
			d := float64(x[base+j] - x[base+j-1])
			s += d * d
		}
		e[i] = s / win
	}
	return e
}

// Transients returns slice start positions (always including 0), sorted.
// sens is 0..1: higher finds more. minGap is the shortest slice, in frames.
func Transients(x []float32, rate int, sens float64, minGap int) []int {
	out := []int{0}
	e := envelope(x)
	if len(e) < 3 {
		return out
	}
	sens = math.Max(0, math.Min(1, sens))
	ratio := 1.5 + (1-sens)*8
	const lookback = 16
	var peak float64
	for _, v := range e {
		peak = math.Max(peak, v)
	}
	floor := peak * 1e-4
	if minGap < hop {
		minGap = hop
	}
	last := -minGap
	for i := 1; i < len(e)-1; i++ {
		lo := max(0, i-lookback)
		var mean float64
		for _, v := range e[lo:i] {
			mean += v
		}
		mean /= float64(i - lo)
		if e[i] > e[i-1] && e[i] >= e[i+1] && e[i] > mean*ratio && e[i] > floor {
			// find the rising edge: walk back to the hop where energy started
			j := i
			for j > lo && e[j-1] < e[j] && e[j-1] > mean*ratio*0.25 {
				j--
			}
			pos := refine(x, j*hop)
			if pos-last >= minGap && pos > 0 {
				out = append(out, pos)
				last = pos
			}
		}
	}
	return out
}

// refine finds the first sample in the frame that clearly rises.
func refine(x []float32, from int) int {
	end := min(from+win+hop, len(x))
	var pk float32
	for _, v := range x[from:end] {
		if v < 0 {
			v = -v
		}
		if v > pk {
			pk = v
		}
	}
	for i := from; i < end; i++ {
		v := x[i]
		if v < 0 {
			v = -v
		}
		if v >= pk*0.2 {
			return i
		}
	}
	return from
}

// Tempo estimates BPM from onset spacing, folded into 70..180. It returns 0
// when the sample is too short or has no clear pulse.
func Tempo(x []float32, rate int) float64 {
	e := envelope(x)
	if len(e) < 64 {
		return 0
	}
	d := make([]float64, len(e))
	for i := 1; i < len(e); i++ {
		d[i] = math.Max(0, e[i]-e[i-1])
	}
	fps := float64(rate) / hop
	lagMin := int(fps * 60 / 180)
	lagMax := int(fps * 60 / 70)
	if lagMax >= len(d)/2 {
		lagMax = len(d)/2 - 1
	}
	if lagMax <= lagMin {
		return 0
	}
	best, bestV := 0, 0.0
	ac := make([]float64, lagMax+2)
	var zero float64
	for _, v := range d {
		zero += v * v
	}
	if zero == 0 {
		return 0
	}
	for lag := lagMin - 1; lag <= lagMax+1; lag++ {
		var s float64
		for i := lag; i < len(d); i++ {
			s += d[i] * d[i-lag]
		}
		ac[lag] = s
		if lag >= lagMin && lag <= lagMax && s > bestV {
			best, bestV = lag, s
		}
	}
	if best == 0 || bestV < zero*0.1 {
		return 0
	}
	// parabolic refinement
	l := float64(best)
	a, b, c := ac[best-1], ac[best], ac[best+1]
	if den := a - 2*b + c; den != 0 {
		l += 0.5 * (a - c) / den
	}
	return 60 * fps / l
}

// Grid returns slice starts every division of a whole note: div 8 = 1/8
// notes. anchor is the first grid line (0 or the first transient).
func Grid(frames, rate int, bpm float64, div int, anchor int) []int {
	if bpm <= 0 || div <= 0 {
		return []int{0}
	}
	step := float64(rate) * 60 / bpm * 4 / float64(div)
	var out []int
	if anchor > 0 {
		// keep the part before the anchor as one slice
		out = append(out, 0)
	}
	for p := float64(anchor); int(p) < frames && len(out) < MaxSlices; p += step {
		out = append(out, int(math.Round(p)))
	}
	return dedupe(out)
}

// Equal divides the sample into n equal parts.
func Equal(frames, n int) []int {
	n = max(1, min(n, MaxSlices))
	out := make([]int, n)
	for i := range out {
		out[i] = i * frames / n
	}
	return dedupe(out)
}

func dedupe(m []int) []int {
	sort.Ints(m)
	out := m[:0]
	for i, v := range m {
		if i == 0 || v != m[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// FromMarkers builds slices from sorted start markers; each runs to the
// next marker and the last to frames. At most MaxSlices are kept.
func FromMarkers(m []int, frames int) []Slice {
	m = dedupe(append([]int(nil), m...))
	if len(m) > MaxSlices {
		m = m[:MaxSlices]
	}
	out := make([]Slice, 0, len(m))
	for i, s := range m {
		end := frames
		if i+1 < len(m) {
			end = m[i+1]
		}
		if end > s {
			out = append(out, Slice{Start: s, End: end, VelLo: 1, VelHi: 127})
		}
	}
	return out
}

// Markers is the reverse: the start of each slice.
func Markers(s []Slice) []int {
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = v.Start
	}
	return out
}

// AddMarker inserts a marker (a manual tap) keeping slices sorted. It
// returns false if one is already within minGap frames or the pad grid is full.
func AddMarker(s []Slice, pos, frames, minGap int) ([]Slice, bool) {
	m := Markers(s)
	for _, v := range m {
		if abs(v-pos) < minGap {
			return s, false
		}
	}
	if len(m) >= MaxSlices {
		return s, false
	}
	old := map[int]Slice{}
	for _, v := range s {
		old[v.Start] = v
	}
	n := FromMarkers(append(m, pos), frames)
	for i := range n {
		if o, ok := old[n[i].Start]; ok {
			n[i].Choke, n[i].VelLo, n[i].VelHi = o.Choke, o.VelLo, o.VelHi
		}
	}
	return n, true
}

// MinLen is the shortest a slice may be made by editing.
const MinLen = 16

// SetStart moves slice i's start, keeping it inside the sample and short of its end.
func SetStart(s []Slice, i, pos int) {
	if i < 0 || i >= len(s) {
		return
	}
	s[i].Start = max(0, min(pos, s[i].End-MinLen))
}

// SetEnd moves slice i's end.
func SetEnd(s []Slice, i, pos, frames int) {
	if i < 0 || i >= len(s) {
		return
	}
	s[i].End = max(s[i].Start+MinLen, min(pos, frames))
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
