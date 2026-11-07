// Package monitor plays the live input to the audio bus so you hear what the
// recorder hears. Capture feeds Push, audio loop pulls Render.
package monitor

import "sync"

const (
	maxFrames  = 4800 // cap on buffered input (0.1 s at 48k): old audio is dropped, latency stays low
	keepFrames = 960  // when over the cap, keep this much (0.02 s)
)

// Monitor is a small stereo FIFO with a linear resampler on the way out.
type Monitor struct {
	mu     sync.Mutex
	on     bool
	inRate int
	l, r   []float32
	pos    float64 // fractional read position into l/r
}

// New makes a monitor for input at inRate.
func New(inRate int) *Monitor { return &Monitor{inRate: inRate} }

// On tells if the monitor is audible.
func (m *Monitor) On() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.on }

// SetOn switches it. Switching clears the buffer so old audio never plays.
func (m *Monitor) SetOn(on bool) {
	m.mu.Lock()
	m.on = on
	m.l, m.r, m.pos = m.l[:0], m.r[:0], 0
	m.mu.Unlock()
}

// Push adds captured audio. One channel is copied to both sides.
func (m *Monitor) Push(block [][]float32) {
	if len(block) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.on {
		return
	}
	l := block[0]
	r := l
	if len(block) > 1 {
		r = block[1]
	}
	m.l = append(m.l, l...)
	m.r = append(m.r, r...)
	if n := len(m.l); n > maxFrames {
		drop := n - keepFrames
		m.l = append(m.l[:0], m.l[drop:]...)
		m.r = append(m.r[:0], m.r[drop:]...)
		m.pos = 0
	}
}

// Render adds the monitor to out (interleaved stereo at outRate). It adds
// nothing when off or when no audio is waiting.
func (m *Monitor) Render(out []float32, outRate int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.on || len(m.l) < 2 || outRate <= 0 {
		return
	}
	step := float64(m.inRate) / float64(outRate)
	for i := 0; i+1 < len(out); i += 2 {
		k := int(m.pos)
		if k+1 >= len(m.l) {
			break
		}
		f := float32(m.pos - float64(k))
		out[i] += m.l[k] + (m.l[k+1]-m.l[k])*f
		out[i+1] += m.r[k] + (m.r[k+1]-m.r[k])*f
		m.pos += step
	}
	// drop what was read
	k := int(m.pos)
	if k > len(m.l) {
		k = len(m.l)
	}
	if k > 0 {
		m.l = append(m.l[:0], m.l[k:]...)
		m.r = append(m.r[:0], m.r[k:]...)
		m.pos -= float64(k)
	}
}
