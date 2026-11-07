package app

import (
	"fmt"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/record"
)

// Key is one soft-key label with its state.
type Key struct {
	Label string
	On    bool
	Off   bool // greyed: does nothing right now
}

// Param is one encoder readout under the screen.
type Param struct{ Name, Value string }

// View is everything the screen draws, copied under the lock.
type Frame struct {
	Mode      Mode
	Top       [8]Key
	Bot       [8]Key
	Params    [8]Param
	Status    string
	Peaks     [][2]float32 // min, max per column
	ViewStart int
	ViewSpan  int
	Sel       audio.Range
	Playhead  int
	Playing   bool
	Rate      int
	Frames    int
	Slices    [][2]int
	Cur       int
	RecState  record.State
	RecLevel  float64
	Shift     bool
	Info      string
	Mgr       *MgrFrame // set while the Sample Manager is open
}

// MgrRow is one visible line of the Sample Manager list.
type MgrRow struct {
	Name, Detail string
	Dir, Marked  bool // Marked: this is the copied file
	Bad          bool
	Playing      bool
}

// MgrFrame is the Sample Manager window.
type MgrFrame struct {
	Dir     string
	Rows    []MgrRow
	Cursor  int // index into Rows
	Index   int // 1-based position in the whole list
	Total   int
	Confirm bool
	Scroll  bool // more rows above or below
}

func fmtT(frames, rate int) string {
	if rate == 0 {
		return "0.000"
	}
	return fmt.Sprintf("%.3f", float64(frames)/float64(rate))
}

// Snapshot builds the frame to draw. cols is the waveform width in pixels.
func (a *App) Snapshot(cols int) Frame {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := Frame{Mode: a.Mode, ViewStart: a.View.Start, ViewSpan: a.View.Span,
		Sel: a.Sel.Clamp(a.frames()), Rate: a.rate(), Frames: a.frames(), Cur: a.Cur,
		RecState: a.Rec.State(), RecLevel: a.Rec.Level(), Shift: a.shift, Status: a.msg}
	f.Playing = a.Eng.Playing()
	if p, ok := a.Eng.Position(); ok && f.Playing {
		f.Playhead = p
	} else {
		f.Playhead = -1
	}
	for _, s := range a.Slice {
		f.Slices = append(f.Slices, [2]int{s.Start, s.End})
	}
	f.Peaks = a.peaks(cols)
	for i := 0; i < 5; i++ {
		f.Top[i] = Key{Label: modeNames[i], On: Mode(i) == a.Mode}
	}
	f.Top[5] = Key{Label: "Play", On: f.Playing}
	f.Top[6] = Key{Label: "Loop", On: a.S.Loop}
	f.Top[7] = Key{Label: "LIVE"}
	a.labels(&f)
	if a.mgr != nil {
		f.Mgr = a.mgrFrame()
	}
	if a.asProg != "" {
		f.Status = a.asProg
	}
	return f
}

// peaks is the min and max of each screen column of the visible range.
func (a *App) peaks(cols int) [][2]float32 {
	out := make([][2]float32, cols)
	n := a.frames()
	if n == 0 || cols == 0 {
		return out
	}
	m := a.Doc.Buf.Ch
	for x := 0; x < cols; x++ {
		s := a.View.Start + x*a.View.Span/cols
		e := a.View.Start + (x+1)*a.View.Span/cols
		if e <= s {
			e = s + 1
		}
		e = min(e, n)
		if s >= n {
			break
		}
		lo, hi := float32(1), float32(-1)
		step := max((e-s)/64, 1) // sample the column, not every frame
		for i := s; i < e; i += step {
			var v float32
			for _, c := range m {
				v += c[i]
			}
			v /= float32(len(m))
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		out[x] = [2]float32{lo, hi}
	}
	return out
}

func (a *App) labels(f *Frame) {
	s := &a.S
	p := &f.Params
	set := func(i int, n, v string) { p[i] = Param{n, v} }
	b := &f.Bot
	if a.mgr != nil {
		a.mgrLabels(f)
		return
	}
	switch a.Mode {
	case ModeRecord:
		st := a.Rec.State()
		if a.Page == 0 {
			set(0, "Thresh", fmt.Sprintf("%.0fdB", s.ThresholdDB))
			set(1, "PreRoll", fmt.Sprintf("%.2fs", s.PreRollSec))
			set(2, "Monitor", map[bool]string{true: "On", false: "Off"}[s.Monitor])
			*b = [8]Key{
				{Label: map[record.State]string{record.Idle: "Arm", record.Armed: "Start", record.Recording: "Stop"}[st], On: st != record.Idle},
				{Label: "Cancel", Off: st == record.Idle},
				{Label: "AutoGo", On: s.AutoTrigger},
				{Label: "Monitor", On: s.Monitor},
				{Label: map[bool]string{true: "Mono", false: "Stereo"}[s.Mono]},
				{Label: "Auto>"},
				{Label: "Load"},
				{Label: "Clear", Off: st != record.Idle},
			}
		} else {
			c := s.Auto
			set(0, "Low", itoa(c.LowNote))
			set(1, "High", itoa(c.HighNote))
			set(2, "Stride", itoa(c.Stride))
			set(3, "Layers", itoa(c.Layers))
			set(4, "Hold", fmt.Sprintf("%.2fs", c.HoldSec))
			set(5, "Release", fmt.Sprintf("%.2fs", c.ReleaseSec))
			set(6, "MIDI ch", itoa(c.Channel+1))
			*b = [8]Key{{Label: "Sample", On: a.asCtx != nil}, {Label: "Abort", Off: a.asCtx == nil},
				{Label: "Monitor", On: s.Monitor}, {}, {}, {}, {}, {Label: "Back"}}
		}
	case ModeTrim:
		set(0, "Zoom", itoa(a.View.Span))
		set(1, "Scroll", fmtT(a.View.Start, a.rate()))
		set(2, "Start", fmtT(f.Sel.Start, a.rate()))
		set(3, "End", fmtT(f.Sel.End, a.rate()))
		set(4, "Silence", fmt.Sprintf("%.0fdB", s.TrimDB))
		set(5, "ZC dist", itoa(s.SnapDist))
		first := "Crop"
		if a.shift {
			first = "ZeroX"
		}
		*b = [8]Key{{Label: first}, {Label: "Silence"}, {Label: "AutoTrm"}, {Label: "Reverse"},
			{Label: "Copy"}, {Label: "Cut"}, {Label: "Paste"}, {Label: "Insert"}}
	case ModeChop:
		set(0, "Zoom", itoa(a.View.Span))
		set(1, "Scroll", fmtT(a.View.Start, a.rate()))
		set(2, "Start", fmtT(f.Sel.Start, a.rate()))
		set(3, "End", fmtT(f.Sel.End, a.rate()))
		set(4, "Sens", fmt.Sprintf("%.0f%%", s.Sens*100))
		if s.How == SliceEqual {
			set(5, "Divide", itoa(EqualNs[s.EqualN]))
		} else {
			set(5, "Grid", "1/"+itoa(GridDivs[s.GridDiv]))
		}
		if a.Cur < len(a.Slice) {
			sl := a.Slice[a.Cur]
			set(6, "SlcSta", fmtT(sl.Start, a.rate()))
			set(7, "SlcEnd", fmtT(sl.End, a.rate()))
		}
		ch := "Assign"
		if a.Cur < len(a.Slice) && a.Slice[a.Cur].Choke > 0 {
			ch = "Choke" + itoa(a.Slice[a.Cur].Choke)
		}
		*b = [8]Key{{Label: "Transnt", On: s.How == SliceTransient}, {Label: "Grid", On: s.How == SliceGrid},
			{Label: "Equal", On: s.How == SliceEqual}, {Label: "Manual", On: s.How == SliceManual},
			{Label: "Clear"}, {Label: "16 Vel", On: s.VelPads}, {Label: "Nudge"}, {Label: ch}}
	case ModeProcess:
		set(0, "Zoom", itoa(a.View.Span))
		set(1, "Scroll", fmtT(a.View.Start, a.rate()))
		set(2, "Start", fmtT(f.Sel.Start, a.rate()))
		set(3, "End", fmtT(f.Sel.End, a.rate()))
		set(4, "Gain", fmt.Sprintf("%+.1fdB", s.GainDB))
		set(5, "Target", fmt.Sprintf("%.1fdB", s.NormDB))
		set(6, "Pitch", fmt.Sprintf("%+.2fst", s.Semitones))
		set(7, "Stretch", fmt.Sprintf("x%.2f", s.Ratio))
		if a.Page == 0 {
			*b = [8]Key{{Label: "Normal"}, {Label: "Gain"}, {Label: "FadeIn"}, {Label: "FadeOut"},
				{Label: "Reverse"}, {Label: map[bool]string{false: "Peak", true: "RMS"}[s.NormMode == 1]},
				{Label: s.Curve.String()}, {Label: "Time>"}}
		} else {
			*b = [8]Key{{Label: "Pitch", Off: s.PitchLive}, {Label: "Stretch"},
				{Label: map[bool]string{true: "Live", false: "Destr"}[s.PitchLive], On: s.PitchLive},
				{Label: s.StretchM.String()}, {}, {}, {}, {Label: "Back"}}
		}
	case ModeExport:
		set(4, "Root", itoa(s.RootKey))
		set(5, "XFade", itoa(s.Crossfade))
		set(6, "Pitch", fmt.Sprintf("%+.2fst", s.Semitones))
		*b = [8]Key{{Label: "Simpler"}, {Label: "Drum Rk", Off: len(a.Slice) == 0}, {Label: "WAV"},
			{Label: "Sampler", Off: len(a.Zones) == 0}, {Label: "All"}, {Label: "Name+"}, {Label: "Name-"}, {}}
		f.Info = s.Name
	}
	if a.Mode == ModeRecord && f.Info == "" {
		f.Info = ""
	}
}

// LEDs is what the pads and buttons should show.
type LEDs struct {
	Pads [64]uint8
	Btn  map[byte]uint8
}

func col(name string) uint8 {
	c, _ := push3.ColorByName(name)
	return c
}

// LEDs returns the wanted LED state. main sends only what changed.
func (a *App) LEDs() LEDs {
	a.mu.Lock()
	defer a.mu.Unlock()
	l := LEDs{Btn: map[byte]uint8{}}
	for i := 0; i < 8; i++ {
		on := uint8(col("dgray"))
		if Mode(i) == a.Mode && i <= int(ModeExport) {
			on = col("white")
		} else if i <= int(ModeExport) {
			on = col("gray_mid")
		}
		l.Btn[push3.CCScreenTop1+byte(i)] = on
		l.Btn[push3.CCScreenBot1+byte(i)] = col("dgray")
	}
	l.Btn[push3.CCScreenTop6] = col("green_vv")
	l.Btn[push3.CCScreenTop8] = col("orange")
	for i := 0; i < 8; i++ {
		b := push3.CCScreenBot1 + byte(i)
		if k := a.botKeyLit(i); k {
			l.Btn[b] = col("white")
		}
	}
	// Undo and Select and Delete and Duplicate: lit when they can act
	l.Btn[push3.CCUndo] = lit(a.Doc.CanUndo() || a.Doc.CanRedo())
	l.Btn[push3.CCSelect] = lit(len(a.Doc.Clipboard) > 0)
	l.Btn[push3.CCDuplicate] = lit(a.Sel.Len() > 0)
	l.Btn[push3.CCDelete] = lit(a.Sel.Len() > 0 || a.Mode == ModeChop)
	l.Btn[push3.CCRecord] = map[record.State]uint8{record.Idle: col("dk_red"), record.Armed: col("amber"), record.Recording: col("red")}[a.Rec.State()]
	l.Btn[push3.CCPlay] = lit(a.Eng.Playing())
	if a.Mode == ModeChop {
		g := col("sky")
		for i, sl := range a.Slice {
			if a.S.VelPads {
				break
			}
			if i >= 64 {
				break
			}
			c := g
			if sl.Choke > 0 {
				c = col("violet")
			}
			if i == a.Cur {
				c = col("white")
			}
			l.Pads[i] = c
		}
		if a.S.VelPads {
			for i := 0; i < 16; i++ {
				l.Pads[i] = [...]uint8{col("dk_pine"), col("green_vv")}[(i/4)%2]
			}
		}
	} else {
		for i := 0; i < 64; i++ {
			l.Pads[i] = col("dgray")
		}
	}
	return l
}

func lit(b bool) uint8 {
	if b {
		return col("white")
	}
	return col("dgray")
}

func (a *App) botKeyLit(i int) bool {
	var f Frame
	a.labels(&f)
	k := f.Bot[i]
	return k.On && !k.Off
}

// LEDSink sends one LED value. main provides it.
type LEDSink interface {
	CC(cc, v byte)
	Pad(note, v byte)
}

// SetLED plugs in the LED output.
func (a *App) SetLED(s LEDSink) {
	a.mu.Lock()
	a.led = s
	a.ledCC = map[byte]uint8{}
	a.ledPad = [64]int16{}
	a.mu.Unlock()
}

// ResetLEDs forgets what was sent and switches everything off, for when the
// app gets the surface: Live's own LEDs are still showing.
func (a *App) ResetLEDs() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.led == nil {
		return
	}
	for _, cc := range push3.LEDButtons {
		a.led.CC(cc, 0)
	}
	for i := 0; i < 64; i++ {
		a.led.Pad(push3.PadNoteMin+byte(i), 0)
	}
	a.ledCC = map[byte]uint8{}
	a.ledPad = [64]int16{}
}

// PaintLEDs sends the LEDs that changed since the last call.
func (a *App) PaintLEDs() {
	want := a.LEDs()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.led == nil {
		return
	}
	for cc, v := range want.Btn {
		if old, ok := a.ledCC[cc]; !ok || old != v {
			a.led.CC(cc, v)
			a.ledCC[cc] = v
		}
	}
	for i, v := range want.Pads {
		if a.ledPad[i] != int16(v)+1 {
			a.led.Pad(push3.PadNoteMin+byte(i), v)
			a.ledPad[i] = int16(v) + 1
		}
	}
}
