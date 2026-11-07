// Package record captures audio straight to disk. It has a pre-roll ring,
// a level threshold that can start the take, and a take-length cap.
package record

import (
	"errors"
	"math"
	"os"
	"sync"

	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/wavio"
)

// State of the recorder.
type State int

const (
	Idle      State = iota
	Armed           // waiting for the threshold (or Start)
	Recording       // writing to disk
)

func (s State) String() string { return [...]string{"Idle", "Armed", "REC"}[s] }

// Config sets one recorder.
type Config struct {
	Rate        int
	Channels    int  // channels the source delivers (1 or 2)
	Mono        bool // mix down to mono when stored
	ThresholdDB float64
	PreRollSec  float64
	MaxSec      float64 // take length cap
	TmpDir      string
}

// Recorder turns a stream of blocks into one take on disk.
type Recorder struct {
	mu    sync.Mutex
	cfg   Config
	state State
	auto  bool // armed: start on threshold
	ring  [][]float32
	rpos  int
	rfill int
	w     *wavio.Writer
	path  string
	level float64 // last block peak, for the meter
	n     int
	full  bool
}

// New makes a recorder.
func New(c Config) *Recorder {
	if c.Rate == 0 {
		c.Rate = 48000
	}
	if c.Channels == 0 {
		c.Channels = 2
	}
	if c.MaxSec == 0 {
		c.MaxSec = 600
	}
	r := &Recorder{cfg: c}
	r.resetRing()
	return r
}

func (r *Recorder) storedCh() int {
	if r.cfg.Mono {
		return 1
	}
	return r.cfg.Channels
}

func (r *Recorder) resetRing() {
	n := int(r.cfg.PreRollSec * float64(r.cfg.Rate))
	r.ring = make([][]float32, r.storedCh())
	for i := range r.ring {
		r.ring[i] = make([]float32, max(n, 1))
	}
	r.rpos, r.rfill = 0, 0
}

// SetConfig changes settings while idle.
func (r *Recorder) SetConfig(c Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != Idle {
		return
	}
	if c.Rate == 0 {
		c.Rate = r.cfg.Rate
	}
	if c.Channels == 0 {
		c.Channels = r.cfg.Channels
	}
	if c.MaxSec == 0 {
		c.MaxSec = r.cfg.MaxSec
	}
	r.cfg = c
	r.resetRing()
}

// SetRate changes the sample rate while idle. The capture source sets it: the
// audio bus input has the rate of the loopback module, not ours.
func (r *Recorder) SetRate(rate int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != Idle || rate <= 0 || rate == r.cfg.Rate {
		return
	}
	r.cfg.Rate = rate
	r.resetRing()
}

// Rate is the sample rate the recorder runs at.
func (r *Recorder) Rate() int { r.mu.Lock(); defer r.mu.Unlock(); return r.cfg.Rate }

// State, Level and Frames feed the UI.
func (r *Recorder) State() State { r.mu.Lock(); defer r.mu.Unlock(); return r.state }

// Level is the peak of the last block, 0..1.
func (r *Recorder) Level() float64 { r.mu.Lock(); defer r.mu.Unlock(); return r.level }

// Frames is the length of the take so far.
func (r *Recorder) Frames() int { r.mu.Lock(); defer r.mu.Unlock(); return r.n }

// Full tells that the take hit MaxSec and stopped writing.
func (r *Recorder) Full() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.full }

// Arm waits for the threshold. With auto false it only fills the pre-roll
// ring and waits for Start.
func (r *Recorder) Arm(auto bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == Recording {
		return
	}
	r.state, r.auto = Armed, auto
	r.resetRing()
}

// Start begins writing now, with the pre-roll already in the ring.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startLocked()
}

func (r *Recorder) startLocked() error {
	if r.state == Recording {
		return nil
	}
	dir := r.cfg.TmpDir
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "take-*.wav")
	if err != nil {
		return err
	}
	f.Close()
	r.path = f.Name()
	w, err := wavio.Create(r.path, r.cfg.Rate, r.storedCh())
	if err != nil {
		return err
	}
	r.w, r.n, r.full = w, 0, false
	// write the pre-roll in time order
	if r.rfill > 0 {
		pre := make([][]float32, len(r.ring))
		size := len(r.ring[0])
		start := 0
		if r.rfill == size {
			start = r.rpos
		}
		for c := range r.ring {
			pre[c] = make([]float32, 0, r.rfill)
			pre[c] = append(pre[c], r.ring[c][start:r.rfill]...)
			if r.rfill == size {
				pre[c] = append(pre[c][:0], append(append([]float32{}, r.ring[c][r.rpos:]...), r.ring[c][:r.rpos]...)...)
			}
		}
		if err := r.w.WriteFrames(pre); err != nil {
			return err
		}
		r.n = len(pre[0])
	}
	r.state = Recording
	return nil
}

// Feed takes one block of planar audio (the source's channel count).
func (r *Recorder) Feed(block [][]float32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(block) == 0 || len(block[0]) == 0 {
		return nil
	}
	if r.cfg.Mono && len(block) > 1 {
		m := make([]float32, len(block[0]))
		for _, c := range block {
			for i, v := range c {
				m[i] += v / float32(len(block))
			}
		}
		block = [][]float32{m}
	}
	var pk float32
	for _, c := range block {
		for _, v := range c {
			if v < 0 {
				v = -v
			}
			if v > pk {
				pk = v
			}
		}
	}
	r.level = float64(pk)
	switch r.state {
	case Armed:
		if r.auto && r.level >= math.Pow(10, r.cfg.ThresholdDB/20) {
			// the block that crossed goes into the take, after the pre-roll
			if err := r.startLocked(); err != nil {
				return err
			}
			return r.writeLocked(block)
		}
		r.pushRing(block)
	case Recording:
		return r.writeLocked(block)
	}
	return nil
}

func (r *Recorder) pushRing(block [][]float32) {
	size := len(r.ring[0])
	for i := range block[0] {
		for c := range r.ring {
			r.ring[c][r.rpos] = block[min(c, len(block)-1)][i]
		}
		r.rpos = (r.rpos + 1) % size
		if r.rfill < size {
			r.rfill++
		}
	}
}

func (r *Recorder) writeLocked(block [][]float32) error {
	limit := int(r.cfg.MaxSec * float64(r.cfg.Rate))
	if r.n >= limit {
		r.full = true
		return nil
	}
	if r.n+len(block[0]) > limit {
		keep := limit - r.n
		cut := make([][]float32, len(block))
		for c := range block {
			cut[c] = block[c][:keep]
		}
		block = cut
		r.full = true
	}
	r.n += len(block[0])
	return r.w.WriteFrames(block)
}

// ErrNoTake is returned by Stop when nothing was recorded.
var ErrNoTake = errors.New("no take recorded")

// Stop ends the take and loads it from disk. The temp file is removed.
func (r *Recorder) Stop() (*audio.Buffer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != Recording {
		r.state = Idle
		return nil, ErrNoTake
	}
	r.state = Idle
	err := r.w.Close(nil)
	r.w = nil
	defer os.Remove(r.path)
	if err != nil {
		return nil, err
	}
	b, err := wavio.Read(r.path)
	if err != nil {
		return nil, err
	}
	if b.Frames() == 0 {
		return nil, ErrNoTake
	}
	return b, nil
}

// Cancel drops the take and goes idle.
func (r *Recorder) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == Recording && r.w != nil {
		r.w.Close(nil)
		os.Remove(r.path)
		r.w = nil
	}
	r.state = Idle
}
