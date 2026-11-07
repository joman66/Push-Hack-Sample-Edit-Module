// Package autosample plans and runs an MPC-style auto-sampling pass over a
// MIDI-connected synth, and finds sustain loops in each take.
package autosample

import (
	"context"
	"errors"
	"math"

	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
)

// Config is what the user sets on the Auto-sample page.
type Config struct {
	Channel    int     // MIDI channel 0-15
	LowNote    int     // first note
	HighNote   int     // last note
	Stride     int     // sample every Nth note (1 = all)
	Layers     int     // velocity layers, 1-8
	HoldSec    float64 // note held
	ReleaseSec float64 // recorded after note off
	PreRollSec float64 // recorded before note on
	FindLoops  bool
}

// Default is a sensible starting point.
func Default() Config {
	return Config{LowNote: 36, HighNote: 84, Stride: 3, Layers: 1, HoldSec: 2, ReleaseSec: 1, PreRollSec: 0.05, FindLoops: true}
}

// Clamp forces every field into its legal range.
func (c *Config) Clamp() {
	c.Channel = max(0, min(15, c.Channel))
	c.LowNote = max(0, min(127, c.LowNote))
	c.HighNote = max(c.LowNote, min(127, c.HighNote))
	c.Stride = max(1, min(24, c.Stride))
	c.Layers = max(1, min(8, c.Layers))
	c.HoldSec = math.Max(0.1, math.Min(30, c.HoldSec))
	c.ReleaseSec = math.Max(0, math.Min(10, c.ReleaseSec))
	c.PreRollSec = math.Max(0, math.Min(1, c.PreRollSec))
}

// Hit is one note and velocity to play.
type Hit struct{ Note, Layer, Vel int }

// Velocities returns the velocity that plays each layer.
func Velocities(layers int) []int {
	layers = max(1, min(8, layers))
	v := make([]int, layers)
	for i := range v {
		v[i] = int(math.Round(127 * float64(i+1) / float64(layers)))
	}
	return v
}

// Plan lists every hit, lowest note first, softest layer first.
func Plan(c Config) []Hit {
	c.Clamp()
	var out []Hit
	vels := Velocities(c.Layers)
	for n := c.LowNote; n <= c.HighNote; n += c.Stride {
		for l, v := range vels {
			out = append(out, Hit{Note: n, Layer: l, Vel: v})
		}
	}
	return out
}

// Loop is a sustain loop in frames of a take.
type Loop struct{ Start, End int }

// Zone is one sampled note and layer, with the range it covers.
type Zone struct {
	Audio        *audio.Buffer
	Root         int
	KeyLo, KeyHi int
	VelLo, VelHi int
	Loop         *Loop
}

// Take is what the recorder returns for one hit.
type Take struct {
	Audio    *audio.Buffer
	On, Off  int // frame of note on and note off
	NoteName string
}

// Player plays one hit on the synth and records it. Real use talks to
// MIDI and the recorder; tests use a fake.
type Player interface {
	Play(ctx context.Context, ch, note, vel int, hold, release, preroll float64) (Take, error)
}

// Progress is called before each hit (i of n).
type Progress func(i, n int, h Hit)

// Run plays the whole plan and returns the zones with key and velocity
// ranges that tile the keyboard with no gaps.
func Run(ctx context.Context, c Config, p Player, prog Progress) ([]Zone, error) {
	c.Clamp()
	plan := Plan(c)
	if len(plan) == 0 {
		return nil, errors.New("nothing to sample")
	}
	zones := make([]Zone, 0, len(plan))
	for i, h := range plan {
		if err := ctx.Err(); err != nil {
			return zones, err
		}
		if prog != nil {
			prog(i, len(plan), h)
		}
		t, err := p.Play(ctx, c.Channel, h.Note, h.Vel, c.HoldSec, c.ReleaseSec, c.PreRollSec)
		if err != nil {
			return zones, err
		}
		z := Zone{Audio: t.Audio, Root: h.Note}
		if c.FindLoops {
			if l, ok := FindLoop(t.Audio.Mono(), t.Audio.Rate, t.On, t.Off); ok {
				z.Loop = &l
			}
		}
		zones = append(zones, z)
	}
	assignRanges(zones, plan, c)
	return zones, nil
}

// assignRanges turns roots and layers into key and velocity ranges.
func assignRanges(z []Zone, plan []Hit, c Config) {
	vels := Velocities(c.Layers)
	// roots in order, without repeats
	var roots []int
	for _, h := range plan {
		if len(roots) == 0 || roots[len(roots)-1] != h.Note {
			roots = append(roots, h.Note)
		}
	}
	for i := range z {
		h := plan[i]
		ri := 0
		for k, r := range roots {
			if r == h.Note {
				ri = k
			}
		}
		lo, hi := h.Note, h.Note
		if ri == 0 {
			lo = 0
		} else {
			lo = (roots[ri-1]+h.Note)/2 + 1
		}
		if ri == len(roots)-1 {
			hi = 127
		} else {
			hi = (h.Note + roots[ri+1]) / 2
		}
		z[i].KeyLo, z[i].KeyHi = lo, hi
		vlo := 1
		if h.Layer > 0 {
			vlo = vels[h.Layer-1] + 1
		}
		z[i].VelLo, z[i].VelHi = vlo, vels[h.Layer]
	}
}

// FindLoop looks for a sustain loop between note on and note off. The loop
// runs between two rising zero crossings a whole number of pitch periods
// apart, and is picked where the waveform before the two ends matches best.
// It returns false for noise, drums or anything without a steady pitch.
func FindLoop(x []float32, rate, on, off int) (Loop, bool) {
	if off > len(x) {
		off = len(x)
	}
	// look at the steady middle-to-late part of the held note
	span := off - on
	if on < 0 || span < rate/5 {
		return Loop{}, false
	}
	a := on + span/3
	b := off - span/20
	if b-a < rate/10 {
		return Loop{}, false
	}
	period := estimatePeriod(x[a:b], rate)
	if period <= 0 {
		return Loop{}, false
	}
	// the loop must be at least ~60 ms and a few periods long
	minLen := max(int(0.06*float64(rate)), 3*int(period))
	k := int(math.Ceil(float64(minLen) / period))
	end := lastRising(x, b)
	if end < 0 {
		return Loop{}, false
	}
	loopLen := float64(k) * period
	if float64(end)-loopLen < float64(a) {
		return Loop{}, false
	}
	target := end - int(math.Round(loopLen))
	// search start near target over +- half a period for the best match
	win := int(math.Round(period)) * 2
	best, bestErr := -1, math.Inf(1)
	for s := target - int(period/2) - 1; s <= target+int(period/2)+1; s++ {
		if s < a || s >= len(x) || s < 1 || !(x[s-1] <= 0 && x[s] > 0) {
			continue
		}
		var e float64
		cnt := 0
		for i := 1; i <= win; i++ {
			if s-i < 0 || end-i < 0 {
				break
			}
			d := float64(x[s-i] - x[end-i])
			e += d * d
			cnt++
		}
		if cnt < win {
			continue
		}
		if e < bestErr {
			bestErr, best = e, s
		}
	}
	if best < 0 || best >= end {
		return Loop{}, false
	}
	// reject if the two ends do not match: not a steady tone
	var energy float64
	for i := 1; i <= win; i++ {
		energy += float64(x[end-i]) * float64(x[end-i])
	}
	if energy <= 0 || bestErr/energy > 0.15 {
		return Loop{}, false
	}
	return Loop{Start: best, End: end}, true
}

// lastRising is the last rising zero crossing at or before pos.
func lastRising(x []float32, pos int) int {
	for i := min(pos, len(x)-1); i > 0; i-- {
		if x[i-1] <= 0 && x[i] > 0 {
			return i
		}
	}
	return -1
}

// estimatePeriod finds the pitch period in frames by autocorrelation, with
// parabolic refinement. It returns 0 if the signal is not clearly periodic.
func estimatePeriod(x []float32, rate int) float64 {
	lagMin := max(rate/2000, 2)
	lagMax := min(rate/30, len(x)/3)
	if lagMax <= lagMin+2 {
		return 0
	}
	n := min(len(x), 4*lagMax)
	x = x[:n]
	var e0 float64
	for _, v := range x[:n-lagMax-1] {
		e0 += float64(v) * float64(v)
	}
	if e0 < 1e-9 {
		return 0
	}
	ac := make([]float64, lagMax+2)
	for lag := lagMin - 1; lag <= lagMax+1; lag++ {
		var s, e float64
		for i := 0; i < n-lagMax-1; i++ {
			s += float64(x[i]) * float64(x[i+lag])
			e += float64(x[i+lag]) * float64(x[i+lag])
		}
		ac[lag] = s / math.Sqrt(e0*e+1e-12)
	}
	// first strong local maximum, so we get the period not a multiple
	var maxAC float64
	for lag := lagMin; lag <= lagMax; lag++ {
		maxAC = math.Max(maxAC, ac[lag])
	}
	if maxAC < 0.8 {
		return 0
	}
	for lag := lagMin; lag <= lagMax; lag++ {
		if ac[lag] >= 0.9*maxAC && ac[lag] >= ac[lag-1] && ac[lag] >= ac[lag+1] {
			l := float64(lag)
			a, b, c := ac[lag-1], ac[lag], ac[lag+1]
			if den := a - 2*b + c; den != 0 {
				l += 0.5 * (a - c) / den
			}
			return l
		}
	}
	return 0
}
