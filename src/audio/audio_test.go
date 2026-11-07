package audio

import (
	"math"
	"testing"
)

func ramp(n int) *Buffer {
	b := New(48000, 2, n)
	for i := 0; i < n; i++ {
		b.Ch[0][i] = float32(i)
		b.Ch[1][i] = float32(-i)
	}
	return b
}

func eq(a, b *Buffer) bool {
	if a.Frames() != b.Frames() {
		return false
	}
	for c := range a.Ch {
		for i := range a.Ch[c] {
			if a.Ch[c][i] != b.Ch[c][i] {
				return false
			}
		}
	}
	return true
}

func TestCutPasteInsertOverwrite(t *testing.T) {
	d := NewDoc(ramp(10))
	if err := d.Cut(Range{2, 5}); err != nil {
		t.Fatal(err)
	}
	if d.Buf.Frames() != 7 || d.Buf.Ch[0][2] != 5 {
		t.Fatalf("cut: %v", d.Buf.Ch[0])
	}
	d.PasteInsert(1)
	want := []float32{0, 2, 3, 4, 1, 5, 6, 7, 8, 9}
	for i, v := range want {
		if d.Buf.Ch[0][i] != v {
			t.Fatalf("insert: %v", d.Buf.Ch[0])
		}
	}
	d.PasteOverwrite(8) // runs past the end: grows
	if d.Buf.Frames() != 11 || d.Buf.Ch[0][8] != 2 || d.Buf.Ch[0][10] != 4 {
		t.Fatalf("overwrite: %v", d.Buf.Ch[0])
	}
}

func TestUndoRedoRestoresEverything(t *testing.T) {
	orig := ramp(100)
	d := NewDoc(orig.Clone())
	d.Cut(Range{10, 20})
	d.PasteInsert(50)
	d.Crop(Range{5, 60})
	d.Reverse(Range{0, 10})
	d.Gain(Range{0, 30}, -6)
	d.Fade(Range{0, 20}, true, CurveExp)
	d.Silence(Range{3, 9})
	final := d.Buf.Clone()
	for d.Undo() {
	}
	if !eq(d.Buf, orig) {
		t.Fatal("undo all did not restore the original")
	}
	for d.Redo() {
	}
	if !eq(d.Buf, final) {
		t.Fatal("redo all did not restore the final state")
	}
}

func TestCropIsOneUndoStep(t *testing.T) {
	d := NewDoc(ramp(50))
	d.Crop(Range{10, 20})
	if d.Buf.Frames() != 10 || d.Buf.Ch[0][0] != 10 {
		t.Fatal("crop wrong")
	}
	d.Undo()
	if d.Buf.Frames() != 50 {
		t.Fatal("one undo should undo the whole crop")
	}
}

func TestRedoClearedByNewEdit(t *testing.T) {
	d := NewDoc(ramp(10))
	d.Reverse(Range{0, 10})
	d.Undo()
	d.Silence(Range{0, 2})
	if d.CanRedo() {
		t.Fatal("redo should clear after a new edit")
	}
}

func TestNormalizePeakAndRMS(t *testing.T) {
	b := New(48000, 1, 4)
	copy(b.Ch[0], []float32{0.25, -0.5, 0.1, 0})
	d := NewDoc(b)
	if _, err := d.Normalize(Range{0, 4}, NormPeak, 0); err != nil {
		t.Fatal(err)
	}
	p, _ := d.Buf.Measure(Range{0, 4})
	if math.Abs(p-1) > 1e-6 {
		t.Fatalf("peak %v", p)
	}
	// RMS to 0 dBFS must not clip: the gain is limited
	b2 := New(48000, 1, 4)
	copy(b2.Ch[0], []float32{0.9, 0.01, 0.01, 0.01})
	d2 := NewDoc(b2)
	res, _ := d2.Normalize(Range{0, 4}, NormRMS, 0)
	p2, _ := d2.Buf.Measure(Range{0, 4})
	if !res.Limited || p2 > 1.000001 {
		t.Fatalf("RMS normalize clipped or not flagged: %v %v", res, p2)
	}
	// target below zero
	b3 := New(48000, 1, 2)
	b3.Ch[0][0] = 0.5
	NewDoc(b3).Normalize(Range{0, 2}, NormPeak, -6)
}

func TestGainRange(t *testing.T) {
	b := New(48000, 1, 2)
	b.Ch[0][0], b.Ch[0][1] = 0.1, 0.1
	d := NewDoc(b)
	d.Gain(Range{0, 1}, 100) // clamps to +24 dB
	if math.Abs(float64(b.Ch[0][0])-0.1*15.8489) > 1e-3 {
		t.Fatalf("+24 dB clamp: %v", b.Ch[0][0])
	}
	d.Gain(Range{1, 2}, -200) // -inf
	if d.Buf.Ch[0][1] != 0 {
		t.Fatal("-inf should silence")
	}
}

func TestFadeShapes(t *testing.T) {
	for _, c := range []Curve{CurveLinear, CurveExp, CurveLog} {
		b := New(48000, 1, 101)
		for i := range b.Ch[0] {
			b.Ch[0][i] = 1
		}
		d := NewDoc(b)
		d.Fade(Range{0, 101}, true, c)
		if b.Ch[0][0] != 0 || math.Abs(float64(b.Ch[0][100])-1) > 1e-5 {
			t.Fatalf("%v fade-in ends", c)
		}
		for i := 1; i < 101; i++ {
			if b.Ch[0][i] < b.Ch[0][i-1] {
				t.Fatalf("%v fade-in not monotonic", c)
			}
		}
	}
	// exp is below linear in the middle, log above
	g := func(c Curve) float64 { return fadeGain(c, 0.5) }
	if !(g(CurveExp) < 0.5 && g(CurveLog) > 0.5) {
		t.Fatal("curve shapes wrong")
	}
	b := New(48000, 1, 11)
	for i := range b.Ch[0] {
		b.Ch[0][i] = 1
	}
	NewDoc(b).Fade(Range{0, 11}, false, CurveLinear)
	if b.Ch[0][0] != 1 || b.Ch[0][10] != 0 {
		t.Fatal("fade-out ends")
	}
}

func TestAutoTrim(t *testing.T) {
	b := New(48000, 1, 100)
	for i := 30; i < 70; i++ {
		b.Ch[0][i] = 0.5
	}
	d := NewDoc(b)
	h, tl, err := d.AutoTrim(-40)
	if err != nil || h != 30 || tl != 30 || d.Buf.Frames() != 40 {
		t.Fatalf("trim %d %d %v frames=%d", h, tl, err, d.Buf.Frames())
	}
	if _, _, err := NewDoc(New(48000, 1, 10)).AutoTrim(-40); err == nil {
		t.Fatal("silent buffer should error")
	}
}

func TestSnapZero(t *testing.T) {
	b := New(48000, 1, 200)
	for i := range b.Ch[0] {
		b.Ch[0][i] = float32(math.Sin(2 * math.Pi * float64(i) / 50))
	}
	p := b.SnapZero(37, 30)
	m := b.Mono()
	if !((m[p-1] <= 0 && m[p] > 0) || (m[p-1] >= 0 && m[p] < 0)) {
		t.Fatalf("not a zero crossing at %d", p)
	}
	if abs(p-37) > 13 {
		t.Fatalf("snapped too far: %d", p)
	}
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func TestEmptySelection(t *testing.T) {
	d := NewDoc(ramp(5))
	if d.Cut(Range{3, 3}) != ErrEmpty || d.Silence(Range{9, 9}) != ErrEmpty {
		t.Fatal("empty selection should error")
	}
	if d.PasteInsert(0) != ErrEmpty {
		t.Fatal("empty clipboard should error")
	}
}

func TestHistoryCap(t *testing.T) {
	d := NewDoc(New(48000, 2, 8_000_000))
	for i := 0; i < 20; i++ {
		d.Gain(Range{0, 8_000_000}, -1)
	}
	total := 0
	for _, s := range d.undo {
		total += s.bytes()
	}
	if total > MaxHistoryBytes+int(2*8_000_000*4*2) {
		t.Fatalf("history not capped: %d", total)
	}
}
