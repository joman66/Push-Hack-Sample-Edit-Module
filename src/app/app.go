// Package app is the SampleEdit controller. It turns Push presses, encoder
// turns and pad taps into edits, recording, slicing and export, and holds
// every setting. It does no drawing and opens no devices: main plugs those
// in through Hooks, so the whole thing runs in tests.
package app

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/autosample"
	"github.com/federico-pepe/push-hack-sample-edit/src/dsp"
	"github.com/federico-pepe/push-hack-sample-edit/src/playback"
	"github.com/federico-pepe/push-hack-sample-edit/src/record"
	"github.com/federico-pepe/push-hack-sample-edit/src/slicer"
)

// Mode is the page chosen with the upper soft-keys.
type Mode int

const (
	ModeRecord Mode = iota
	ModeTrim
	ModeChop
	ModeProcess
	ModeExport
)

var modeNames = [...]string{"Record", "Trim", "Chop", "Process", "Export"}

func (m Mode) String() string { return modeNames[m] }

// SliceHow is how Chop makes slices.
type SliceHow int

const (
	SliceTransient SliceHow = iota
	SliceGrid
	SliceEqual
	SliceManual
)

// Settings are the user's values. Plain data, so a test can read them.
type Settings struct {
	// record
	ThresholdDB float64
	PreRollSec  float64
	AutoTrigger bool
	Mono        bool
	Source      int
	Monitor     bool // the live input is audible on the audio bus
	Auto        autosample.Config
	// trim
	TrimDB   float64
	SnapDist int // frames searched for a zero crossing
	// chop
	Sens    float64
	GridDiv int // index into GridDivs
	EqualN  int // index into EqualNs
	How     SliceHow
	VelPads bool
	// process
	GainDB    float64
	NormDB    float64
	NormMode  audio.NormMode
	Curve     audio.Curve
	Semitones float64
	Ratio     float64
	StretchM  dsp.Mode
	PitchLive bool // pitch is a playback setting, not applied to the audio
	// export
	RootKey   int
	Crossfade int
	Loop      bool
	Name      string
}

// GridDivs are the grid choices (note value as a fraction of a whole note).
var GridDivs = []int{8, 16, 32}

// EqualNs are the equal division counts.
var EqualNs = []int{4, 8, 16, 32, 64}

// DefaultSettings is the first-run state.
func DefaultSettings() Settings {
	return Settings{
		ThresholdDB: -40, PreRollSec: 0.25, AutoTrigger: true,
		Auto:   autosample.Default(),
		TrimDB: -60, SnapDist: 400,
		Sens: 0.5, GridDiv: 1, EqualN: 1,
		GainDB: 0, NormDB: 0, Ratio: 1,
		RootKey: 60, Crossfade: 0, Name: "sample-001",
	}
}

// Hooks are the things main provides.
type Hooks struct {
	LiveTempo   func() float64                  // 0 when unknown
	Leave       func()                          // give the surface back to Live
	SourceNames []string                        // input choices, for the Source key
	SourceOn    func(source int) error          // start capturing
	SourceOff   func()                          // stop capturing
	Monitor     func(on bool, source int) error // hear the input live; nil if there is no audio out
	AutoSample  func(ctx context.Context, c autosample.Config, p autosample.Progress) ([]autosample.Zone, error)
	Save        Saver
	Now         func() time.Time
}

// Saver writes files. It is an interface so tests need no disk.
type Saver interface {
	SaveWAV(name string, b *audio.Buffer, tempo float64, root int, loop *Loop, slices []slicer.Slice) (path string, err error)
	SaveSimpler(name, wavPath string, b *audio.Buffer, root int, loop *Loop, crossfade int, transpose float64) (string, error)
	SaveDrumRack(name, wavPath string, b *audio.Buffer, slices []slicer.Slice) (string, error)
	SaveSampler(name string, zones []autosample.Zone) (string, error)
	Load(name string) (*audio.Buffer, error)
}

// Loop is a loop range in frames.
type Loop = audio.Range

// App is the controller.
type App struct {
	mu sync.Mutex
	H  Hooks

	Doc   *audio.Doc
	Rec   *record.Recorder
	Eng   *playback.Engine
	S     Settings
	Mode  Mode
	Page  int // sub-page: Record 0/1, Process 0/1
	Sel   audio.Range
	View  struct{ Start, Span int }
	Slice []slicer.Slice
	Cur   int // selected slice
	Zones []autosample.Zone

	shift   bool
	msg     string
	msgEnd  time.Time
	dirty   bool
	ver     int // bumps when audio, view or slices change
	tempo   float64
	asCtx   context.CancelFunc
	asProg  string
	voice   int
	exports int
	led     LEDSink
	ledCC   map[byte]uint8
	ledPad  [64]int16
}

// New makes an app around a recorder and a playback engine.
func New(h Hooks, rec *record.Recorder, eng *playback.Engine) *App {
	if h.Now == nil {
		h.Now = time.Now
	}
	a := &App{H: h, Rec: rec, Eng: eng, S: DefaultSettings(), Mode: ModeRecord}
	a.Doc = audio.NewDoc(audio.New(48000, 2, 0))
	a.View.Span = 48000
	a.dirty = true
	a.syncRecorder()
	return a
}

// Say shows a short ASCII message on the screen's status line.
func (a *App) say(f string, args ...any) {
	a.msg = fmt.Sprintf(f, args...)
	a.msgEnd = a.H.Now().Add(3 * time.Second)
	a.dirty = true
}

func (a *App) touch() { a.ver++; a.dirty = true }

// Load puts a buffer in the editor and resets the view.
func (a *App) Load(b *audio.Buffer) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.load(b)
}

func (a *App) load(b *audio.Buffer) {
	a.Eng.StopAll()
	a.Doc = audio.NewDoc(b)
	a.Sel = audio.Range{}
	a.Slice, a.Cur = nil, 0
	a.View.Start, a.View.Span = 0, max(b.Frames(), 64)
	a.tempo = 0
	a.touch()
}

func (a *App) frames() int { return a.Doc.Buf.Frames() }
func (a *App) rate() int   { return a.Doc.Buf.Rate }

// selOrAll is the selection, or the whole sample when none is set.
func (a *App) selOrAll() audio.Range {
	r := a.Sel.Clamp(a.frames())
	if r.Len() == 0 {
		return audio.Range{Start: 0, End: a.frames()}
	}
	return r
}

func (a *App) needSel() (audio.Range, bool) {
	r := a.Sel.Clamp(a.frames())
	if r.Len() == 0 {
		a.say("Set a selection (Enc3/Enc4)")
		return r, false
	}
	return r, true
}

// cursor is the playhead while playing, else the selection start.
func (a *App) cursor() int {
	if p, ok := a.Eng.Position(); ok && a.Eng.Playing() {
		return p
	}
	return a.Sel.Clamp(a.frames()).Start
}

// ---- clamps and views --------------------------------------------------

func (a *App) clampView() {
	n := max(a.frames(), 64)
	a.View.Span = max(64, min(a.View.Span, n))
	a.View.Start = max(0, min(a.View.Start, n-a.View.Span))
}

func (a *App) zoom(delta int, fine bool) {
	base := 1.18
	if fine {
		base = 1.03
	}
	centre := a.View.Start + a.View.Span/2
	a.View.Span = int(float64(a.View.Span) * math.Pow(base, float64(-delta)))
	a.View.Start = centre - a.View.Span/2
	a.clampView()
	a.touch()
}

func (a *App) scroll(delta int, fine bool) {
	step := a.View.Span / 40
	if fine {
		step = max(a.View.Span/800, 1)
	}
	a.View.Start += delta * max(step, 1)
	a.clampView()
	a.touch()
}

// pxFrames is how many frames one screen pixel covers at this zoom.
func (a *App) pxFrames() int { return max(a.View.Span/960, 1) }

func (a *App) moveSel(end bool, delta int, fine bool) {
	n := a.frames()
	if n == 0 {
		return
	}
	step := a.pxFrames() * 2
	if fine {
		step = 1 // sample-accurate
	}
	if a.Sel.End == 0 && a.Sel.Start == 0 {
		a.Sel = audio.Range{Start: a.View.Start, End: a.View.Start + a.View.Span}
	}
	if end {
		a.Sel.End = max(0, min(a.Sel.End+delta*step, n))
	} else {
		a.Sel.Start = max(0, min(a.Sel.Start+delta*step, n))
	}
	if a.Sel.Start > a.Sel.End {
		if end {
			a.Sel.End = a.Sel.Start
		} else {
			a.Sel.Start = a.Sel.End
		}
	}
	a.touch()
}

// ---- recorder glue -----------------------------------------------------

func (a *App) syncRecorder() {
	a.Rec.SetConfig(record.Config{
		Rate: a.Rec.Rate(), ThresholdDB: a.S.ThresholdDB, PreRollSec: a.S.PreRollSec,
		Mono: a.S.Mono,
	})
}

func (a *App) recToggle() {
	switch a.Rec.State() {
	case record.Idle:
		a.syncRecorder()
		if a.H.SourceOn != nil {
			if err := a.H.SourceOn(a.S.Source); err != nil {
				a.say("Input: %s", asciiErr(err))
				return
			}
		}
		a.Rec.Arm(a.S.AutoTrigger)
		a.say("Armed")
	case record.Armed:
		if err := a.Rec.Start(); err != nil {
			a.say("Rec: %s", asciiErr(err))
			return
		}
		a.say("Recording")
	case record.Recording:
		a.recStop(true)
	}
	a.dirty = true
}

func (a *App) recStop(keep bool) {
	if a.H.SourceOff != nil {
		defer a.H.SourceOff()
	}
	if !keep {
		a.Rec.Cancel()
		a.say("Cancelled")
		return
	}
	b, err := a.Rec.Stop()
	if err != nil {
		a.say("No take")
		return
	}
	a.load(b)
	a.say("Took %.2fs", float64(b.Frames())/float64(b.Rate))
}

// ---- time ---------------------------------------------------------------

// Tick runs a few times a second. It reports whether the screen changed.
func (a *App) Tick() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.msg != "" && a.H.Now().After(a.msgEnd) {
		a.msg = ""
		a.dirty = true
	}
	st := a.Rec.State()
	if st == record.Recording || st == record.Armed || a.Eng.Playing() {
		a.dirty = true
	}
	if st == record.Recording && a.Rec.Full() {
		a.recStop(true)
		a.say("Max length reached")
	}
	d := a.dirty
	a.dirty = false
	return d
}

// ---- helpers ------------------------------------------------------------

func asciiErr(err error) string {
	s := []rune(err.Error())
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 32 && r < 127 {
			out = append(out, r)
		}
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return string(out)
}

func (a *App) audition(loop bool) {
	if a.Eng.Playing() {
		a.Eng.StopAll()
		a.dirty = true
		return
	}
	r := a.selOrAll()
	a.playRange(r, 1, loop || a.S.Loop, 0)
}

func (a *App) playRange(r audio.Range, gain float32, loop bool, group int) {
	semis := 0.0
	if a.S.PitchLive {
		semis = a.S.Semitones
	}
	a.voice = a.Eng.Play(a.Doc.Buf.Ch, a.rate(), playback.Opts{
		Start: r.Start, End: r.End, Gain: gain, Semitones: semis, Loop: loop, Group: group})
	a.dirty = true
}

// BPM is Live's tempo, else the one found in the sample, else 0.
func (a *App) bpm() float64 {
	if a.H.LiveTempo != nil {
		if t := a.H.LiveTempo(); t > 0 {
			return t
		}
	}
	if a.tempo == 0 {
		a.tempo = slicer.Tempo(a.Doc.Buf.Mono(), a.rate())
	}
	return a.tempo
}

func padIndex(note byte) int {
	c, r := push3.PadCoord(note)
	return r*8 + c
}
