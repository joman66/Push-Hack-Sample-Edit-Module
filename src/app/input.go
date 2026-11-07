package app

import (
	"context"
	"math"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/autosample"
	"github.com/federico-pepe/push-hack-sample-edit/src/dsp"
	"github.com/federico-pepe/push-hack-sample-edit/src/record"
	"github.com/federico-pepe/push-hack-sample-edit/src/slicer"
)

// CC handles one control change from Push: a button (value 0 = release),
// or an encoder (relative delta).
func (a *App) CC(cc, val byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if push3.IsEncoderCC(cc) {
		if a.mgr != nil {
			a.mgrEncoder(cc, push3.DecodeRel(val))
			return
		}
		a.encoder(cc, push3.DecodeRel(val))
		return
	}
	if cc == push3.CCShift {
		a.shift = val > 0
		a.dirty = true
		return
	}
	if val == 0 {
		return // only presses do anything below
	}
	switch {
	case cc >= push3.CCScreenTop1 && cc <= push3.CCScreenTop8:
		if a.mgr != nil {
			a.mgrTop(int(cc - push3.CCScreenTop1))
		} else {
			a.topKey(int(cc - push3.CCScreenTop1))
		}
	case cc >= push3.CCScreenBot1 && cc <= push3.CCScreenBot8:
		if a.mgr != nil {
			a.mgrBot(int(cc - push3.CCScreenBot1))
		} else {
			a.botKey(int(cc - push3.CCScreenBot1))
		}
	default:
		if a.mgr != nil {
			a.mgrButton(cc)
		} else {
			a.button(cc)
		}
	}
}

func (a *App) topKey(i int) {
	switch {
	case i <= int(ModeExport):
		a.Mode, a.Page = Mode(i), 0
		a.dirty = true
	case i == 5:
		a.audition(false)
	case i == 6:
		a.S.Loop = !a.S.Loop
		a.say("Loop %s", onOff(a.S.Loop))
	case i == 7:
		if a.H.Leave != nil {
			a.H.Leave()
		}
	}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (a *App) button(cc byte) {
	switch cc {
	case push3.CCUndo:
		if a.shift {
			if !a.Doc.Redo() {
				a.say("Nothing to redo")
			} else {
				a.afterEdit("Redo")
			}
		} else if !a.Doc.Undo() {
			a.say("Nothing to undo")
		} else {
			a.afterEdit("Undo")
		}
	case push3.CCDuplicate:
		if a.shift {
			a.copySel()
		}
	case push3.CCDelete:
		if a.shift {
			a.cutSel()
		} else if a.Mode == ModeChop {
			a.deleteSlice()
		}
	case push3.CCSelect:
		if a.shift {
			a.pasteInsert()
		} else {
			a.pasteOverwrite()
		}
	case push3.CCPlay:
		a.audition(false)
	case push3.CCRecord:
		a.Mode = ModeRecord
		a.recToggle()
	case push3.CCPageLeft, push3.CCPageRight:
		if a.Mode == ModeRecord || a.Mode == ModeProcess {
			a.Page = 1 - a.Page
			a.dirty = true
		}
	case push3.CCDPadLeft:
		a.stepSlice(-1)
	case push3.CCDPadRight:
		a.stepSlice(1)
	}
}

func (a *App) afterEdit(what string) {
	a.clampSel()
	a.clampView()
	a.touch()
	a.say("%s", what)
}

func (a *App) clampSel() {
	n := a.frames()
	a.Sel = a.Sel.Clamp(n)
	for i := range a.Slice {
		a.Slice[i].End = min(a.Slice[i].End, n)
		a.Slice[i].Start = min(a.Slice[i].Start, a.Slice[i].End)
	}
}

// ---- clipboard ------------------------------------------------------------

func (a *App) copySel() {
	if r, ok := a.needSel(); ok {
		if a.Doc.Copy(r) == nil {
			a.say("Copied %d frames", r.Len())
		}
	}
}

func (a *App) cutSel() {
	if r, ok := a.needSel(); ok {
		if a.Doc.Cut(r) == nil {
			a.Sel = audio.Range{Start: r.Start, End: r.Start}
			a.afterEdit("Cut")
		}
	}
}

func (a *App) pasteOverwrite() {
	if a.Doc.PasteOverwrite(a.cursor()) != nil {
		a.say("Clipboard empty")
		return
	}
	a.afterEdit("Pasted over")
}

func (a *App) pasteInsert() {
	if a.Doc.PasteInsert(a.cursor()) != nil {
		a.say("Clipboard empty")
		return
	}
	a.afterEdit("Inserted")
}

// ---- encoders -------------------------------------------------------------

func (a *App) encoder(cc byte, d int) {
	if d == 0 {
		return
	}
	fine := a.shift
	n := -1
	if cc >= push3.CCEncoder1 && cc <= push3.CCEncoder8 {
		n = int(cc - push3.CCEncoder1)
	}
	if cc == push3.CCJogWheel {
		if a.Mode == ModeChop {
			a.stepSlice(d)
		} else {
			a.scroll(d, fine)
		}
		return
	}
	if n < 0 {
		return
	}
	// Record has no waveform to move around, so its encoders are settings.
	if a.Mode == ModeRecord {
		a.recordEncoder(n, d, fine)
		return
	}
	switch n {
	case 0:
		a.zoom(d, fine)
	case 1:
		a.scroll(d, fine)
	case 2:
		a.moveSel(false, d, fine)
	case 3:
		a.moveSel(true, d, fine)
	default:
		a.modeEncoder(n, d, fine)
	}
}

func scale(d int, fine bool, coarse, fn float64) float64 {
	if fine {
		return float64(d) * fn
	}
	return float64(d) * coarse
}

func clampf(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func (a *App) recordEncoder(n, d int, fine bool) {
	s := &a.S
	if a.Page == 0 {
		switch n {
		case 0:
			s.ThresholdDB = clampf(s.ThresholdDB+scale(d, fine, 2, 0.5), -90, 0)
		case 1:
			s.PreRollSec = clampf(s.PreRollSec+scale(d, fine, 0.05, 0.01), 0, 2)
		case 2: // turn right for monitor on, left for off
			if on := d > 0; on != s.Monitor {
				a.setMonitor(on)
			}
		}
		a.syncRecorder()
	} else {
		c := &s.Auto
		switch n {
		case 0:
			c.LowNote += d
		case 1:
			c.HighNote += d
		case 2:
			c.Stride += d
		case 3:
			c.Layers += d
		case 4:
			c.HoldSec += scale(d, fine, 0.25, 0.05)
		case 5:
			c.ReleaseSec += scale(d, fine, 0.25, 0.05)
		case 6:
			c.Channel += d
		}
		c.Clamp()
	}
	a.dirty = true
}

func (a *App) modeEncoder(n, d int, fine bool) {
	s := &a.S
	switch a.Mode {
	case ModeTrim:
		if n == 4 {
			s.TrimDB = clampf(s.TrimDB+scale(d, fine, 2, 0.5), -96, -6)
		}
		if n == 5 {
			s.SnapDist = max(8, min(4000, s.SnapDist+d*map[bool]int{true: 8, false: 64}[fine]))
		}
	case ModeChop:
		switch n {
		case 4:
			s.Sens = clampf(s.Sens+scale(d, fine, 0.05, 0.01), 0, 1)
		case 5:
			if s.How == SliceEqual {
				s.EqualN = max(0, min(len(EqualNs)-1, s.EqualN+d))
			} else {
				s.GridDiv = max(0, min(len(GridDivs)-1, s.GridDiv+d))
			}
		case 6:
			if a.Cur < len(a.Slice) {
				step := a.pxFrames() * 2
				if fine {
					step = 1
				}
				slicer.SetStart(a.Slice, a.Cur, a.Slice[a.Cur].Start+d*step)
				a.touch()
			}
		case 7:
			if a.Cur < len(a.Slice) {
				step := a.pxFrames() * 2
				if fine {
					step = 1
				}
				slicer.SetEnd(a.Slice, a.Cur, a.Slice[a.Cur].End+d*step, a.frames())
				a.touch()
			}
		}
	case ModeProcess:
		switch n {
		case 4:
			s.GainDB = clampf(s.GainDB+scale(d, fine, 1, 0.1), audio.MinDB, audio.MaxGainDB)
		case 5:
			s.NormDB = clampf(s.NormDB+scale(d, fine, 1, 0.1), -60, 0)
		case 6:
			s.Semitones = clampf(s.Semitones+scale(d, fine, 1, 0.05), -dsp.MaxSemitones, dsp.MaxSemitones)
		case 7:
			s.Ratio = clampf(s.Ratio*math.Pow(1.02, scale(d, fine, 5, 1)), dsp.MinRatio, dsp.MaxRatio)
		}
	case ModeExport:
		switch n {
		case 4:
			s.RootKey = max(0, min(127, s.RootKey+d))
		case 5:
			s.Crossfade = max(0, min(4096, s.Crossfade+d*map[bool]int{true: 1, false: 16}[fine]))
		case 6:
			s.Semitones = clampf(s.Semitones+scale(d, fine, 1, 0.05), -dsp.MaxSemitones, dsp.MaxSemitones)
		}
	}
	a.dirty = true
}

// ---- lower soft-keys --------------------------------------------------------

func (a *App) botKey(i int) {
	switch a.Mode {
	case ModeRecord:
		a.botRecord(i)
	case ModeTrim:
		a.botTrim(i)
	case ModeChop:
		a.botChop(i)
	case ModeProcess:
		a.botProcess(i)
	case ModeExport:
		a.botExport(i)
	}
	a.dirty = true
}

// ---- monitor ----------------------------------------------------------------

func (a *App) toggleMonitor() { a.setMonitor(!a.S.Monitor) }

func (a *App) setMonitor(on bool) {
	if a.H.Monitor == nil {
		a.say("Monitor: no audio out")
		return
	}
	if err := a.H.Monitor(on); err != nil {
		a.S.Monitor = false
		a.say("Monitor: %s", asciiErr(err))
		return
	}
	a.S.Monitor = on
	a.say("Monitor %s", onOff(on))
}

// MonitorStop turns the monitor off. Call it when the app loses the surface.
func (a *App) MonitorStop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.S.Monitor && a.H.Monitor != nil {
		a.H.Monitor(false)
	}
	a.S.Monitor = false
	a.dirty = true
}

func (a *App) botRecord(i int) {
	if a.Page == 1 {
		switch i {
		case 0:
			a.startAutoSample()
		case 1:
			if a.asCtx != nil {
				a.asCtx()
			}
		case 2:
			a.toggleMonitor()
		case 7:
			a.Page = 0
		}
		return
	}
	switch i {
	case 0:
		a.recToggle()
	case 1:
		if a.Rec.State() != record.Idle {
			a.recStop(false)
		}
	case 2:
		a.S.AutoTrigger = !a.S.AutoTrigger
		a.say("Auto-start %s", onOff(a.S.AutoTrigger))
	case 3:
		a.toggleMonitor()
	case 4:
		a.S.Mono = !a.S.Mono
		a.syncRecorder()
		a.say("%s", map[bool]string{true: "Mono", false: "Stereo"}[a.S.Mono])
	case 5:
		a.Page = 1
	case 6:
		a.openManager()
	case 7:
		if a.Rec.State() == record.Idle {
			a.load(audio.New(48000, 2, 0))
			a.say("Cleared")
		}
	}
}

func (a *App) botTrim(i int) {
	switch i {
	case 0:
		if a.shift {
			a.snap()
			return
		}
		if r, ok := a.needSel(); ok && a.Doc.Crop(r) == nil {
			a.Sel = audio.Range{}
			a.View.Start = 0
			a.View.Span = a.frames()
			a.afterEdit("Cropped")
		}
	case 1:
		if r, ok := a.needSel(); ok && a.Doc.Silence(r) == nil {
			a.afterEdit("Silenced")
		}
	case 2:
		h, t, err := a.Doc.AutoTrim(a.S.TrimDB)
		if err != nil {
			a.say("%s", asciiErr(err))
			return
		}
		a.Sel = audio.Range{}
		a.View.Start, a.View.Span = 0, a.frames()
		a.afterEdit("Trimmed " + itoa(h+t))
	case 3:
		if r := a.selOrAll(); a.Doc.Reverse(r) == nil {
			a.afterEdit("Reversed")
		}
	case 4:
		a.copySel()
	case 5:
		a.cutSel()
	case 6:
		a.pasteOverwrite()
	case 7:
		a.pasteInsert()
	}
}

func itoa(n int) string { return string(appendInt(nil, n)) }

func appendInt(b []byte, n int) []byte {
	if n == 0 {
		return append(b, '0')
	}
	var t [20]byte
	i := len(t)
	for n > 0 {
		i--
		t[i] = byte('0' + n%10)
		n /= 10
	}
	return append(b, t[i:]...)
}

// snap moves both selection ends to the nearest zero crossing.
func (a *App) snap() {
	r := a.Sel.Clamp(a.frames())
	if r.Len() == 0 {
		a.say("Set a selection first")
		return
	}
	a.Sel.Start = a.Doc.Buf.SnapZero(r.Start, a.S.SnapDist)
	a.Sel.End = a.Doc.Buf.SnapZero(r.End, a.S.SnapDist)
	a.touch()
	a.say("Snapped to zero")
}

func (a *App) botProcess(i int) {
	s := &a.S
	r := a.selOrAll()
	if a.Page == 0 {
		switch i {
		case 0:
			res, err := a.Doc.Normalize(r, s.NormMode, s.NormDB)
			if err != nil {
				a.say("%s", asciiErr(err))
				return
			}
			msg := "Normalized"
			if res.Limited {
				msg += " (limited)"
			}
			a.afterEdit(msg)
		case 1:
			if a.Doc.Gain(r, s.GainDB) == nil {
				a.afterEdit("Gain applied")
			}
		case 2:
			if a.Doc.Fade(r, true, s.Curve) == nil {
				a.afterEdit("Fade in")
			}
		case 3:
			if a.Doc.Fade(r, false, s.Curve) == nil {
				a.afterEdit("Fade out")
			}
		case 4:
			if a.Doc.Reverse(r) == nil {
				a.afterEdit("Reversed")
			}
		case 5:
			s.NormMode = 1 - s.NormMode
			a.say("Normalize %s", map[audio.NormMode]string{audio.NormPeak: "peak", audio.NormRMS: "RMS"}[s.NormMode])
		case 6:
			s.Curve = (s.Curve + 1) % 3
			a.say("Curve %s", s.Curve)
		case 7:
			a.Page = 1
		}
		return
	}
	switch i {
	case 0:
		if s.PitchLive {
			a.say("Pitch is live: %+.1f st", s.Semitones)
			return
		}
		a.busy("Pitch")
		rate := a.rate()
		semi, m := s.Semitones, s.StretchM
		if a.Doc.Transform(r, func(in [][]float32) [][]float32 { return dsp.PitchShift(in, rate, semi, m) }) == nil {
			s.Semitones = 0
			a.afterEdit("Pitch shifted")
		}
	case 1:
		rate := a.rate()
		ratio, m := s.Ratio, s.StretchM
		a.busy("Stretch")
		if a.Doc.Transform(r, func(in [][]float32) [][]float32 { return dsp.Stretch(in, rate, ratio, m) }) == nil {
			// the selection now covers the stretched audio
			a.Sel = audio.Range{Start: r.Start, End: r.Start + int(math.Round(float64(r.Len())*clampf(ratio, dsp.MinRatio, dsp.MaxRatio)))}
			a.View.Start, a.View.Span = 0, max(a.frames(), 64)
			s.Ratio = 1
			a.afterEdit("Stretched")
		}
	case 2:
		s.PitchLive = !s.PitchLive
		a.say("Pitch %s", map[bool]string{true: "live (preview/export)", false: "destructive"}[s.PitchLive])
	case 3:
		s.StretchM = 1 - s.StretchM
		a.say("Stretch %s", s.StretchM)
	case 7:
		a.Page = 0
	}
}

// busy puts a message up before a slow edit. The edit runs on the caller's
// goroutine, so the screen shows it on the next frame after.
func (a *App) busy(what string) { a.say("%s...", what) }

// ---- chop -----------------------------------------------------------------

func (a *App) botChop(i int) {
	n := a.frames()
	if n == 0 {
		return
	}
	s := &a.S
	switch i {
	case 0:
		a.setSlices(slicer.Transients(a.Doc.Buf.Mono(), a.rate(), s.Sens, a.rate()/20), SliceTransient)
	case 1:
		bpm := a.bpm()
		if bpm <= 0 {
			a.say("No tempo found")
			return
		}
		anchor := 0
		if t := slicer.Transients(a.Doc.Buf.Mono(), a.rate(), s.Sens, a.rate()/20); len(t) > 1 {
			anchor = t[1]
		}
		a.setSlices(slicer.Grid(n, a.rate(), bpm, GridDivs[s.GridDiv], anchor), SliceGrid)
	case 2:
		a.setSlices(slicer.Equal(n, EqualNs[s.EqualN]), SliceEqual)
	case 3:
		s.How = SliceManual
		a.Slice, a.Cur = slicer.FromMarkers([]int{0}, n), 0
		a.touch()
		a.audition(false)
		a.say("Play, tap pads to mark")
	case 4:
		a.Slice, a.Cur = nil, 0
		a.touch()
	case 5:
		s.VelPads = !s.VelPads
		a.say("16 velocities %s", onOff(s.VelPads))
	case 6:
		if a.Cur < len(a.Slice) {
			a.nudge(a.Cur)
		}
	case 7:
		if a.Cur < len(a.Slice) {
			a.Slice[a.Cur].Choke = (a.Slice[a.Cur].Choke + 1) % 9
			a.say("Choke %d", a.Slice[a.Cur].Choke)
		}
	}
}

func (a *App) setSlices(m []int, how SliceHow) {
	a.S.How = how
	a.Slice, a.Cur = slicer.FromMarkers(m, a.frames()), 0
	a.touch()
	a.say("%d slices", len(a.Slice))
}

// nudge moves a slice's start and end to the nearest zero crossings.
func (a *App) nudge(i int) {
	sl := &a.Slice[i]
	sl.Start = a.Doc.Buf.SnapZero(sl.Start, a.S.SnapDist)
	sl.End = max(sl.Start+slicer.MinLen, a.Doc.Buf.SnapZero(sl.End, a.S.SnapDist))
	a.touch()
	a.say("Nudged to zero")
}

func (a *App) stepSlice(d int) {
	if len(a.Slice) == 0 || a.Mode != ModeChop {
		return
	}
	a.Cur = max(0, min(len(a.Slice)-1, a.Cur+d))
	a.dirty = true
}

func (a *App) deleteSlice() {
	if a.Cur >= len(a.Slice) || len(a.Slice) < 2 {
		return
	}
	// merge into the previous slice (or the next for the first one)
	if a.Cur > 0 {
		a.Slice[a.Cur-1].End = a.Slice[a.Cur].End
	} else {
		a.Slice[1].Start = a.Slice[0].Start
	}
	a.Slice = append(a.Slice[:a.Cur], a.Slice[a.Cur+1:]...)
	a.Cur = min(a.Cur, len(a.Slice)-1)
	a.touch()
}

// ---- export -----------------------------------------------------------------

func (a *App) botExport(i int) {
	if a.H.Save == nil {
		a.say("No library folder")
		return
	}
	switch i {
	case 0:
		a.exportSimpler()
	case 1:
		a.exportDrumRack()
	case 2:
		a.exportWAV()
	case 3:
		a.exportSampler()
	case 4:
		if a.exportSimpler() {
			if len(a.Slice) > 0 {
				a.exportDrumRack()
			}
		}
	case 5:
		a.bumpName(1)
	case 6:
		a.bumpName(-1)
	}
}

func (a *App) bumpName(d int) {
	n := 0
	prefix := "sample-"
	if len(a.S.Name) > len(prefix) {
		for _, c := range a.S.Name[len(prefix):] {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
	}
	n = max(1, n+d)
	a.S.Name = prefix + pad3(n)
	a.say("Name %s", a.S.Name)
}

func pad3(n int) string {
	s := itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

// loopRange is the loop to write: the selection, when Loop is on.
func (a *App) loopRange() *Loop {
	if !a.S.Loop {
		return nil
	}
	r := a.Sel.Clamp(a.frames())
	if r.Len() < 2 {
		return nil
	}
	return &r
}

// exported returns the audio to write: the editor's, with live pitch left
// to the preset's transpose.
func (a *App) wav() (string, bool) {
	if a.frames() == 0 {
		a.say("Nothing to export")
		return "", false
	}
	tempo := a.bpm()
	p, err := a.H.Save.SaveWAV(a.S.Name, a.Doc.Buf, tempo, a.S.RootKey, a.loopRange(), a.Slice)
	if err != nil {
		a.say("WAV: %s", asciiErr(err))
		return "", false
	}
	return p, true
}

func (a *App) exportWAV() {
	if p, ok := a.wav(); ok {
		a.say("Saved %s", baseName(p))
	}
}

func (a *App) exportSimpler() bool {
	p, ok := a.wav()
	if !ok {
		return false
	}
	tr := 0.0
	if a.S.PitchLive {
		tr = a.S.Semitones
	}
	pre, err := a.H.Save.SaveSimpler(a.S.Name, p, a.Doc.Buf, a.S.RootKey, a.loopRange(), a.S.Crossfade, tr)
	if err != nil {
		a.say("Simpler: %s", asciiErr(err))
		return false
	}
	a.say("Saved %s", baseName(pre))
	return true
}

func (a *App) exportDrumRack() {
	if len(a.Slice) == 0 {
		a.say("Slice first (Chop)")
		return
	}
	p, ok := a.wav()
	if !ok {
		return
	}
	pre, err := a.H.Save.SaveDrumRack(a.S.Name, p, a.Doc.Buf, a.Slice)
	if err != nil {
		a.say("Drum Rack: %s", asciiErr(err))
		return
	}
	a.say("Saved %s", baseName(pre))
}

func (a *App) exportSampler() {
	if len(a.Zones) == 0 {
		a.say("Auto-sample first")
		return
	}
	pre, err := a.H.Save.SaveSampler(a.S.Name, a.Zones)
	if err != nil {
		a.say("Sampler: %s", asciiErr(err))
		return
	}
	a.say("Saved %s", baseName(pre))
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// ---- auto-sample --------------------------------------------------------------

func (a *App) startAutoSample() {
	if a.H.AutoSample == nil {
		a.say("No MIDI output")
		return
	}
	if a.asCtx != nil {
		a.say("Already sampling")
		return
	}
	cfg := a.S.Auto
	cfg.Clamp()
	ctx, cancel := context.WithCancel(context.Background())
	a.asCtx = cancel
	a.say("Auto-sampling %d hits", len(autosample.Plan(cfg)))
	go func() {
		zones, err := a.H.AutoSample(ctx, cfg, func(i, n int, h autosample.Hit) {
			a.mu.Lock()
			a.asProg = "AS " + itoa(i+1) + "/" + itoa(n) + " n" + itoa(h.Note)
			a.dirty = true
			a.mu.Unlock()
		})
		a.mu.Lock()
		defer a.mu.Unlock()
		a.asCtx, a.asProg = nil, ""
		if len(zones) > 0 {
			a.Zones = zones
			a.load(zones[0].Audio.Clone())
			a.S.RootKey = zones[0].Root
		}
		if err != nil {
			a.say("Sampling stopped: %s", asciiErr(err))
		} else {
			a.say("Sampled %d zones", len(zones))
		}
	}()
}

// ---- pads ----------------------------------------------------------------------

// Pad handles a pad press (vel > 0) or release.
func (a *App) Pad(note, vel byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !push3.IsPadNote(note) || vel == 0 {
		return
	}
	if a.mgr != nil {
		a.mgrPreview() // any pad previews the selected sample
		return
	}
	i := padIndex(note)
	gain := float32(vel) / 127
	if a.Mode != ModeChop {
		a.Eng.StopAll()
		a.playRange(a.selOrAll(), gain, false, 0)
		return
	}
	if a.S.How == SliceManual && a.Eng.Playing() {
		pos := a.cursor()
		pos = a.Doc.Buf.SnapZero(pos, 256)
		if sl, ok := slicer.AddMarker(a.Slice, pos, a.frames(), a.rate()/50); ok {
			a.Slice = sl
			a.Cur = idxAt(sl, pos)
			a.touch()
		}
		return
	}
	if a.S.VelPads {
		if a.Cur >= len(a.Slice) {
			return
		}
		g := float32(i+1) / 16
		if i >= 16 {
			return
		}
		sl := a.Slice[a.Cur]
		a.Eng.StopAll()
		a.playRange(audio.Range{Start: sl.Start, End: sl.End}, g, false, sl.Choke)
		return
	}
	if i >= len(a.Slice) {
		return
	}
	a.Cur = i
	sl := a.Slice[i]
	a.playRange(audio.Range{Start: sl.Start, End: sl.End}, gain, false, sl.Choke)
}

func idxAt(s []slicer.Slice, pos int) int {
	for i, v := range s {
		if v.Start == pos {
			return i
		}
	}
	return 0
}
