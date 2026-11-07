package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/autosample"
	"github.com/federico-pepe/push-hack-sample-edit/src/playback"
	"github.com/federico-pepe/push-hack-sample-edit/src/record"
	"github.com/federico-pepe/push-hack-sample-edit/src/slicer"
)

type saved struct {
	wav, simpler, rack, sampler int
	loop                        *Loop
	slices                      int
	root                        int
	transpose                   float64
}

func (s *saved) SaveWAV(n string, b *audio.Buffer, t float64, root int, l *Loop, sl []slicer.Slice) (string, error) {
	s.wav++
	s.loop, s.slices, s.root = l, len(sl), root
	return "/lib/" + n + ".wav", nil
}
func (s *saved) SaveSimpler(n, w string, b *audio.Buffer, root int, l *Loop, xf int, tr float64) (string, error) {
	s.simpler++
	s.transpose = tr
	return "/p/" + n + ".adv", nil
}
func (s *saved) SaveDrumRack(n, w string, b *audio.Buffer, sl []slicer.Slice) (string, error) {
	s.rack++
	return "/p/" + n + ".adg", nil
}
func (s *saved) SaveSampler(n string, z []autosample.Zone) (string, error) {
	s.sampler++
	return "/p/" + n + ".adv", nil
}
func (s *saved) Load(n string) (*audio.Buffer, error) { return nil, errors.New("none") }

func newApp(t *testing.T) (*App, *saved) {
	t.Helper()
	sv := &saved{}
	rec := record.New(record.Config{Rate: 48000, Channels: 2, TmpDir: t.TempDir()})
	a := New(Hooks{Save: sv, LiveTempo: func() float64 { return 0 }, SourceOn: func() error { return nil }, SourceOff: func() {}}, rec, playback.New(48000))
	return a, sv
}

func tone(n int) *audio.Buffer {
	b := audio.New(48000, 2, n)
	for i := range b.Ch[0] {
		v := float32(0.5 * math.Sin(float64(i)/20))
		b.Ch[0][i], b.Ch[1][i] = v, v
	}
	return b
}

func press(a *App, cc byte) { a.CC(cc, 127); a.CC(cc, 0) }
func turn(a *App, enc byte, d int) {
	v := byte(d)
	if d < 0 {
		v = byte(128 + d)
	}
	a.CC(enc, v)
}
func shiftPress(a *App, cc byte) { a.CC(push3.CCShift, 127); press(a, cc); a.CC(push3.CCShift, 0) }
func top(a *App, i int)          { press(a, push3.CCScreenTop1+byte(i)) }
func bot(a *App, i int)          { press(a, push3.CCScreenBot1+byte(i)) }

func TestModeKeys(t *testing.T) {
	a, _ := newApp(t)
	for i, m := range []Mode{ModeRecord, ModeTrim, ModeChop, ModeProcess, ModeExport} {
		top(a, i)
		if a.Mode != m {
			t.Fatalf("top %d -> %v", i, a.Mode)
		}
	}
	left := false
	a.H.Leave = func() { left = true }
	top(a, 7)
	if !left {
		t.Fatal("LIVE key")
	}
}

func TestSelectionCoarseAndFine(t *testing.T) {
	a, _ := newApp(t)
	a.Load(tone(96000))
	top(a, 1)
	turn(a, push3.CCEncoder3, 3)
	coarse := a.Sel.Start
	if coarse <= 3 {
		t.Fatalf("coarse should move several frames per click: %d", coarse)
	}
	a.CC(push3.CCShift, 127)
	turn(a, push3.CCEncoder3, 3)
	a.CC(push3.CCShift, 0)
	if a.Sel.Start != coarse+3 {
		t.Fatalf("fine = 1 frame per click: %d", a.Sel.Start-coarse)
	}
	turn(a, push3.CCEncoder4, 10)
	if a.Sel.End <= a.Sel.Start {
		t.Fatal("end must stay after start")
	}
	// zoom and scroll
	before := a.View.Span
	turn(a, push3.CCEncoder1, 2)
	if a.View.Span >= before {
		t.Fatal("zoom in should shrink the span")
	}
	s0 := a.View.Start
	turn(a, push3.CCEncoder2, 4)
	if a.View.Start <= s0 {
		t.Fatal("scroll")
	}
}

func TestClipboardAndUndoButtons(t *testing.T) {
	a, _ := newApp(t)
	a.Load(tone(1000))
	a.Sel = audio.Range{Start: 100, End: 300}
	shiftPress(a, push3.CCDuplicate) // copy
	if len(a.Doc.Clipboard[0]) != 200 || a.frames() != 1000 {
		t.Fatal("copy")
	}
	shiftPress(a, push3.CCDelete) // cut
	if a.frames() != 800 {
		t.Fatalf("cut: %d", a.frames())
	}
	a.Sel = audio.Range{Start: 50, End: 50}
	press(a, push3.CCSelect) // paste overwrite at cursor
	if a.frames() != 800 {
		t.Fatalf("overwrite should not grow: %d", a.frames())
	}
	shiftPress(a, push3.CCSelect) // insert
	if a.frames() != 1000 {
		t.Fatalf("insert: %d", a.frames())
	}
	press(a, push3.CCUndo)
	if a.frames() != 800 {
		t.Fatal("undo insert")
	}
	shiftPress(a, push3.CCUndo) // redo
	if a.frames() != 1000 {
		t.Fatal("redo")
	}
}

func TestTrimActions(t *testing.T) {
	a, _ := newApp(t)
	a.Load(tone(5000))
	top(a, 1)
	bot(a, 0) // crop with no selection: refuses
	if a.frames() != 5000 {
		t.Fatal("crop needs a selection")
	}
	a.Sel = audio.Range{Start: 1000, End: 3000}
	bot(a, 0)
	if a.frames() != 2000 {
		t.Fatalf("crop: %d", a.frames())
	}
	a.Sel = audio.Range{Start: 100, End: 200}
	bot(a, 1) // silence
	if a.Doc.Buf.Ch[0][150] != 0 || a.frames() != 2000 {
		t.Fatal("silence")
	}
	bot(a, 3) // reverse: whole when no selection... selection exists here
	// zero-crossing snap on Shift + lower key 1
	a.Sel = audio.Range{Start: 301, End: 901}
	a.CC(push3.CCShift, 127)
	bot(a, 0)
	a.CC(push3.CCShift, 0)
	m := a.Doc.Buf.Mono()
	for _, p := range []int{a.Sel.Start, a.Sel.End} {
		if !(m[p-1] <= 0 && m[p] > 0 || m[p-1] >= 0 && m[p] < 0) {
			t.Fatalf("pos %d is not a zero crossing", p)
		}
	}
	// auto trim
	b := audio.New(48000, 1, 1000)
	for i := 300; i < 600; i++ {
		b.Ch[0][i] = 0.5
	}
	a.Load(b)
	bot(a, 2)
	if a.frames() != 300 {
		t.Fatalf("autotrim: %d", a.frames())
	}
}

func TestProcessPages(t *testing.T) {
	a, _ := newApp(t)
	a.Load(tone(8000))
	top(a, 3)
	bot(a, 0) // normalize peak to 0 dB
	pk, _ := a.Doc.Buf.Measure(audio.Range{End: 8000})
	if math.Abs(pk-1) > 1e-4 {
		t.Fatalf("normalize: %v", pk)
	}
	turn(a, push3.CCEncoder5, -6) // gain -6 dB
	bot(a, 1)
	pk2, _ := a.Doc.Buf.Measure(audio.Range{End: 8000})
	if math.Abs(pk2-0.5012) > 0.01 {
		t.Fatalf("gain: %v", pk2)
	}
	bot(a, 2) // fade in
	if a.Doc.Buf.Ch[0][0] != 0 {
		t.Fatal("fade in")
	}
	bot(a, 6) // curve cycles
	if a.S.Curve != audio.CurveExp {
		t.Fatal("curve")
	}
	bot(a, 5)
	if a.S.NormMode != audio.NormRMS {
		t.Fatal("norm mode")
	}
	// page 2: stretch and destructive pitch
	press(a, push3.CCPageRight)
	if a.Page != 1 {
		t.Fatal("page")
	}
	a.S.Ratio = 2
	bot(a, 1)
	if a.frames() != 16000 {
		t.Fatalf("stretch: %d", a.frames())
	}
	a.S.Semitones = 12
	bot(a, 0)
	if a.frames() != 16000 {
		t.Fatalf("pitch should keep length: %d", a.frames())
	}
	// live pitch is not applied to audio
	a.S.PitchLive, a.S.Semitones = true, 5
	n := a.frames()
	bot(a, 0)
	if a.frames() != n || a.S.Semitones != 5 {
		t.Fatal("live pitch must not touch audio")
	}
	// one undo reverts the pitch/stretch steps
	if !a.Doc.CanUndo() {
		t.Fatal("undo history")
	}
}

func clickAudio() *audio.Buffer {
	b := audio.New(48000, 1, 48000)
	seed := uint32(3)
	for _, p := range []int{2000, 14000, 26000, 38000} {
		for i := 0; i < 2400; i++ {
			seed = seed*1664525 + 1013904223
			b.Ch[0][p+i] = float32(int32(seed)>>8) / float32(1<<23) * float32(math.Exp(-float64(i)/500)) * 0.8
		}
	}
	return b
}

func TestChopAndPads(t *testing.T) {
	a, _ := newApp(t)
	a.Load(clickAudio())
	top(a, 2)
	bot(a, 0) // transients
	if len(a.Slice) != 5 {
		t.Fatalf("transient slices: %d", len(a.Slice))
	}
	bot(a, 2) // equal division, default index 1 = 8
	if len(a.Slice) != 8 {
		t.Fatalf("equal: %d", len(a.Slice))
	}
	turn(a, push3.CCEncoder6, 1)
	bot(a, 2)
	if len(a.Slice) != 16 {
		t.Fatalf("equal 16: %d", len(a.Slice))
	}
	// pad tap selects and previews that slice
	a.Pad(push3.PadNote(2, 0), 100)
	if a.Cur != 2 || !a.Eng.Playing() {
		t.Fatal("pad tap")
	}
	a.Eng.StopAll()
	// grid needs a tempo
	bot(a, 1)
	// choke assign cycles
	a.Cur = 1
	bot(a, 7)
	if a.Slice[1].Choke != 1 {
		t.Fatal("choke assign")
	}
	// 16 velocity preview: one slice across 16 pads
	bot(a, 5)
	a.Pad(push3.PadNote(3, 1), 90) // pad index 11 -> gain 12/16
	if !a.Eng.Playing() {
		t.Fatal("vel pad")
	}
	a.Eng.StopAll()
	bot(a, 5)
	// slice edit by encoders
	s0 := a.Slice[a.Cur].Start
	turn(a, push3.CCEncoder7, 3)
	if a.Slice[a.Cur].Start == s0 {
		t.Fatal("slice start edit")
	}
	bot(a, 6) // nudge
	// delete merges
	n := len(a.Slice)
	press(a, push3.CCDelete)
	if len(a.Slice) != n-1 {
		t.Fatal("delete slice")
	}
	// d-pad moves selection
	a.Cur = 3
	press(a, push3.CCDPadRight)
	if a.Cur != 4 {
		t.Fatal("dpad")
	}
}

func TestManualSlicing(t *testing.T) {
	a, _ := newApp(t)
	a.Load(clickAudio())
	top(a, 2)
	bot(a, 3) // manual: starts playback
	if !a.Eng.Playing() {
		t.Fatal("manual should audition")
	}
	// render a bit so the playhead moves
	out := make([]float32, 2*20000)
	a.Eng.Render(out)
	a.Pad(push3.PadNote(0, 0), 100)
	if len(a.Slice) != 2 {
		t.Fatalf("a tap should drop one marker: %d", len(a.Slice))
	}
}

func TestExportRoutes(t *testing.T) {
	a, sv := newApp(t)
	top(a, 4)
	bot(a, 2)
	if sv.wav != 0 {
		t.Fatal("nothing to export yet")
	}
	a.Load(clickAudio())
	a.S.Loop = true
	a.Sel = audio.Range{Start: 1000, End: 9000}
	bot(a, 2)
	if sv.wav != 1 || sv.loop == nil || sv.loop.Start != 1000 {
		t.Fatalf("wav %+v", sv)
	}
	bot(a, 0)
	if sv.simpler != 1 {
		t.Fatal("simpler")
	}
	bot(a, 1)
	if sv.rack != 0 {
		t.Fatal("drum rack needs slices")
	}
	top(a, 2)
	bot(a, 2)
	top(a, 4)
	bot(a, 1)
	if sv.rack != 1 || sv.slices == 0 {
		t.Fatal("drum rack")
	}
	a.S.PitchLive, a.S.Semitones = true, 3
	bot(a, 0)
	if sv.transpose != 3 {
		t.Fatal("live pitch becomes the preset transpose")
	}
	bot(a, 5)
	if a.S.Name != "sample-002" {
		t.Fatalf("name %s", a.S.Name)
	}
	bot(a, 3)
	if sv.sampler != 0 {
		t.Fatal("sampler needs zones")
	}
}

func TestRecordFlow(t *testing.T) {
	a, _ := newApp(t)
	a.S.AutoTrigger = true
	bot(a, 0) // arm
	if a.Rec.State() != record.Armed {
		t.Fatal("armed")
	}
	quiet := [][]float32{make([]float32, 480), make([]float32, 480)}
	loud := [][]float32{make([]float32, 480), make([]float32, 480)}
	for i := range loud[0] {
		loud[0][i], loud[1][i] = 0.5, 0.5
	}
	a.Rec.Feed(quiet)
	a.Rec.Feed(loud)
	a.Rec.Feed(loud)
	if a.Rec.State() != record.Recording {
		t.Fatal("threshold should start the take")
	}
	bot(a, 0) // stop
	if a.frames() == 0 || a.Rec.State() != record.Idle {
		t.Fatalf("take not loaded: %d", a.frames())
	}
	// cancel
	bot(a, 0)
	bot(a, 1)
	if a.Rec.State() != record.Idle {
		t.Fatal("cancel")
	}
	// settings
	turn(a, push3.CCEncoder1, 5)
	if a.S.ThresholdDB <= -40 {
		t.Fatal("threshold encoder")
	}
	bot(a, 4)
	if !a.S.Mono {
		t.Fatal("mono toggle")
	}
}

func TestAutoSamplePage(t *testing.T) {
	a, _ := newApp(t)
	done := make(chan struct{})
	a.H.AutoSample = func(ctx context.Context, c autosample.Config, p autosample.Progress) ([]autosample.Zone, error) {
		defer close(done)
		p(0, 1, autosample.Hit{Note: 60})
		return []autosample.Zone{{Audio: tone(1000), Root: 60, KeyHi: 127, VelLo: 1, VelHi: 127}}, nil
	}
	press(a, push3.CCPageRight)
	if a.Page != 1 {
		t.Fatal("auto page")
	}
	turn(a, push3.CCEncoder3, 2) // stride
	if a.S.Auto.Stride != 5 {
		t.Fatalf("stride %d", a.S.Auto.Stride)
	}
	bot(a, 0)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hook not called")
	}
	time.Sleep(50 * time.Millisecond)
	a.mu.Lock()
	z, n := len(a.Zones), a.frames()
	a.mu.Unlock()
	if z != 1 || n != 1000 {
		t.Fatalf("zones %d frames %d", z, n)
	}
}

func TestSnapshotAndLEDs(t *testing.T) {
	a, _ := newApp(t)
	a.Load(clickAudio())
	for m := 0; m <= int(ModeExport); m++ {
		top(a, m)
		for p := 0; p < 2; p++ {
			a.Page = p
			f := a.Snapshot(960)
			if len(f.Peaks) != 960 {
				t.Fatal("peaks")
			}
			for _, k := range f.Bot {
				for _, r := range k.Label {
					if r > 126 {
						t.Fatalf("non-ASCII label %q", k.Label)
					}
				}
			}
			l := a.LEDs()
			if len(l.Btn) == 0 {
				t.Fatal("leds")
			}
		}
	}
	// peaks show the clicks
	a.Mode, a.Page = ModeTrim, 0
	f := a.Snapshot(960)
	var hi float32
	for _, p := range f.Peaks {
		if p[1] > hi {
			hi = p[1]
		}
	}
	if hi < 0.1 {
		t.Fatal("waveform empty")
	}
}

func TestTickClearsMessage(t *testing.T) {
	a, _ := newApp(t)
	now := time.Now()
	a.H.Now = func() time.Time { return now }
	a.say("hello")
	if !a.Tick() {
		t.Fatal("dirty after message")
	}
	now = now.Add(5 * time.Second)
	a.Tick()
	if a.msg != "" {
		t.Fatal("message should expire")
	}
}

func TestMonitorToggle(t *testing.T) {
	a, _ := newApp(t)
	var calls []string
	a.H.Monitor = func(on bool) error {
		calls = append(calls, fmt.Sprintf("%v", on))
		return nil
	}
	a.Mode = ModeRecord
	bot(a, 3) // Monitor key
	if !a.S.Monitor || len(calls) != 1 || calls[0] != "true" {
		t.Fatalf("Monitor key should switch it on: %v", calls)
	}
	a.Page = 1
	bot(a, 2) // page 2 key
	if a.S.Monitor || calls[len(calls)-1] != "false" {
		t.Fatalf("monitor %v calls %v", a.S.Monitor, calls)
	}
	a.Page = 0
	turn(a, push3.CCEncoder3, 1) // encoder 3 right: on
	if !a.S.Monitor {
		t.Fatal("encoder should switch it on")
	}
	a.MonitorStop()
	if a.S.Monitor || calls[len(calls)-1] != "false" {
		t.Fatalf("MonitorStop: %v %v", a.S.Monitor, calls)
	}
}

func TestMonitorErrorStaysOff(t *testing.T) {
	a, _ := newApp(t)
	a.H.Monitor = func(bool) error { return errors.New("audio bus not running") }
	a.Mode = ModeRecord
	a.Page = 1
	bot(a, 2)
	if a.S.Monitor {
		t.Fatal("must stay off when the hook fails")
	}
	a.H.Monitor = nil
	bot(a, 2) // no hook: say so, no crash
}
