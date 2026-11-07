package dsp

import (
	"math"
	"testing"
)

func sine(freq float64, rate, n int) []float32 {
	x := make([]float32, n)
	for i := range x {
		x[i] = float32(0.5 * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
	}
	return x
}

// freq estimates frequency from rising zero crossings in the middle part.
func freq(x []float32, rate int) float64 {
	a, b := len(x)/4, len(x)*3/4
	first, last, cnt := -1, -1, 0
	for i := a + 1; i < b; i++ {
		if x[i-1] <= 0 && x[i] > 0 {
			if first < 0 {
				first = i
			}
			last = i
			cnt++
		}
	}
	if cnt < 2 {
		return 0
	}
	return float64(cnt-1) * float64(rate) / float64(last-first)
}

func TestResampleKeepsToneAndLength(t *testing.T) {
	x := sine(1000, 48000, 48000)
	y := Resample(x, 0.5) // half as long: plays an octave up
	if len(y) != 24000 {
		t.Fatalf("len %d", len(y))
	}
	if f := freq(y, 48000); math.Abs(f-2000) > 10 {
		t.Fatalf("freq %v", f)
	}
}

func TestStretchLengthAndPitch(t *testing.T) {
	for _, m := range []Mode{Rhythmic, Tonal} {
		for _, r := range []float64{0.5, 1.5, 2} {
			x := sine(440, 48000, 48000)
			y := Stretch([][]float32{x}, 48000, r, m)[0]
			if want := int(math.Round(48000 * r)); len(y) != want {
				t.Fatalf("%v r=%v len %d want %d", m, r, len(y), want)
			}
			if f := freq(y, 48000); math.Abs(f-440) > 8 {
				t.Fatalf("%v r=%v pitch moved: %v", m, r, f)
			}
			var pk float32
			for _, v := range y[len(y)/4 : len(y)*3/4] {
				if v > pk {
					pk = v
				}
			}
			if pk < 0.4 || pk > 0.65 {
				t.Fatalf("%v r=%v level off: %v", m, r, pk)
			}
		}
	}
}

func TestPitchShift(t *testing.T) {
	x := sine(440, 48000, 48000)
	for _, tc := range []struct{ semi, want float64 }{{12, 880}, {-12, 220}, {7, 659.3}} {
		y := PitchShift([][]float32{x}, 48000, tc.semi, Tonal)[0]
		if len(y) != len(x) {
			t.Fatalf("length changed: %d", len(y))
		}
		if f := freq(y, 48000); math.Abs(f-tc.want) > tc.want*0.02 {
			t.Fatalf("semi %v: got %v want %v", tc.semi, f, tc.want)
		}
	}
}

func TestStereoStaysInPhase(t *testing.T) {
	x := sine(300, 48000, 24000)
	y := Stretch([][]float32{x, x}, 48000, 1.7, Tonal)
	for i := range y[0] {
		if y[0][i] != y[1][i] {
			t.Fatal("identical channels diverged")
		}
	}
}

func TestStretchIdentityAndEmpty(t *testing.T) {
	x := sine(100, 48000, 1000)
	y := Stretch([][]float32{x}, 48000, 1, Tonal)[0]
	for i := range x {
		if x[i] != y[i] {
			t.Fatal("ratio 1 should copy")
		}
	}
	if out := Stretch([][]float32{{}}, 48000, 2, Tonal); len(out[0]) != 0 {
		t.Fatal("empty in, empty out")
	}
	if len(Stretch([][]float32{sine(100, 48000, 100)}, 48000, 3, Rhythmic)[0]) != 300 {
		t.Fatal("tiny input")
	}
}
