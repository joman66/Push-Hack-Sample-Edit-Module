// Package playback mixes the preview voices (slice taps, 16-velocity pads,
// audition) into stereo float blocks. It has no I/O: main feeds the blocks
// to the audio bus.
package playback

import (
	"math"
	"sync"
)

// Voice is one playing slice or selection.
type Voice struct {
	ch     [][]float32
	pos    float64
	start  int
	end    int
	step   float64
	gain   float32
	loop   bool
	group  int
	done   bool
	handle int
}

// Engine is a small voice mixer.
type Engine struct {
	mu     sync.Mutex
	voices []*Voice
	rate   int
	next   int
	last   int // last played position, for the playhead
	lastOK bool
}

// New makes an engine rendering at the card's rate.
func New(cardRate int) *Engine { return &Engine{rate: cardRate} }

// SetRate follows a card format change.
func (e *Engine) SetRate(r int) { e.mu.Lock(); e.rate = r; e.mu.Unlock() }

// Opts shape one voice.
type Opts struct {
	Start, End int // frames; End 0 = to the end
	Gain       float32
	Semitones  float64 // preview pitch (varispeed)
	Loop       bool
	Group      int // choke group; starting a voice cuts others of the group
}

// Play starts a voice on a snapshot of the audio. The slices are never
// written to by edits (every edit builds new slices), so this is safe.
func (e *Engine) Play(ch [][]float32, srcRate int, o Opts) int {
	if len(ch) == 0 || len(ch[0]) == 0 {
		return -1
	}
	end := o.End
	if end <= 0 || end > len(ch[0]) {
		end = len(ch[0])
	}
	if o.Start < 0 || o.Start >= end {
		return -1
	}
	g := o.Gain
	if g == 0 {
		g = 1
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if o.Group > 0 {
		for _, v := range e.voices {
			if v.group == o.Group {
				v.done = true
			}
		}
	}
	e.next++
	v := &Voice{ch: ch, pos: float64(o.Start), start: o.Start, end: end,
		step: float64(srcRate) / float64(e.rate) * math.Pow(2, o.Semitones/12),
		gain: g, loop: o.Loop, group: o.Group, handle: e.next}
	e.voices = append(e.voices, v)
	return v.handle
}

// Stop ends one voice.
func (e *Engine) Stop(h int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, v := range e.voices {
		if v.handle == h {
			v.done = true
		}
	}
}

// StopAll ends every voice.
func (e *Engine) StopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, v := range e.voices {
		v.done = true
	}
}

// Playing reports whether any voice is sounding.
func (e *Engine) Playing() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, v := range e.voices {
		if !v.done {
			return true
		}
	}
	return false
}

// Position is where the newest voice is, in source frames.
func (e *Engine) Position() (int, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.voices) - 1; i >= 0; i-- {
		if !e.voices[i].done {
			return int(e.voices[i].pos), true
		}
	}
	return e.last, e.lastOK
}

// Render adds the voices into out (interleaved stereo float, cleared first).
func (e *Engine) Render(out []float32) {
	for i := range out {
		out[i] = 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	frames := len(out) / 2
	for _, v := range e.voices {
		if v.done {
			continue
		}
		for i := 0; i < frames; i++ {
			if v.pos >= float64(v.end-1) {
				if v.loop {
					v.pos = float64(v.start)
				} else {
					v.done = true
					break
				}
			}
			p := int(v.pos)
			f := float32(v.pos - float64(p))
			l := lerp(v.ch[0][p], v.ch[0][p+1], f)
			r := l
			if len(v.ch) > 1 {
				r = lerp(v.ch[1][p], v.ch[1][p+1], f)
			}
			out[2*i] += l * v.gain
			out[2*i+1] += r * v.gain
			v.pos += v.step
		}
		e.last, e.lastOK = int(v.pos), true
	}
	live := e.voices[:0]
	for _, v := range e.voices {
		if !v.done {
			live = append(live, v)
		}
	}
	e.voices = live
}

func lerp(a, b, f float32) float32 { return a + (b-a)*f }
