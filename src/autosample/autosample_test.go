package autosample

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
)

func TestPlan(t *testing.T) {
	c := Config{LowNote: 36, HighNote: 48, Stride: 3, Layers: 2}
	p := Plan(c)
	if len(p) != 10 { // notes 36,39,42,45,48 x 2 layers
		t.Fatalf("%d hits", len(p))
	}
	if p[0].Note != 36 || p[0].Vel != 64 || p[1].Vel != 127 {
		t.Fatalf("%v", p[:2])
	}
	if v := Velocities(8); len(v) != 8 || v[7] != 127 || v[0] != 16 {
		t.Fatalf("%v", v)
	}
	c = Config{LowNote: 90, HighNote: 10, Stride: 0, Layers: 99}
	c.Clamp()
	if c.HighNote < c.LowNote || c.Stride != 1 || c.Layers != 8 {
		t.Fatalf("clamp %+v", c)
	}
}

type fakeSynth struct {
	rate  int
	calls int
	fail  int
}

func (f *fakeSynth) Play(_ context.Context, ch, note, vel int, hold, rel, pre float64) (Take, error) {
	f.calls++
	if f.fail > 0 && f.calls == f.fail {
		return Take{}, errors.New("midi gone")
	}
	freq := 440 * math.Pow(2, float64(note-69)/12)
	n := int((pre + hold + rel) * float64(f.rate))
	b := audio.New(f.rate, 1, n)
	on := int(pre * float64(f.rate))
	off := on + int(hold*float64(f.rate))
	for i := on; i < n; i++ {
		env := 1.0
		if i > off {
			env = math.Exp(-float64(i-off) / 2000)
		}
		b.Ch[0][i] = float32(float64(vel) / 127 * 0.8 * env * math.Sin(2*math.Pi*freq*float64(i)/float64(f.rate)))
	}
	return Take{Audio: b, On: on, Off: off}, nil
}

func TestRunTilesKeyboardAndVelocity(t *testing.T) {
	c := Config{LowNote: 48, HighNote: 60, Stride: 6, Layers: 3, HoldSec: 0.5, ReleaseSec: 0.1, PreRollSec: 0.02, FindLoops: true}
	zs, err := Run(context.Background(), c, &fakeSynth{rate: 48000}, nil)
	if err != nil || len(zs) != 9 {
		t.Fatalf("%v %d", err, len(zs))
	}
	// key ranges: 48 covers 0..., 54, 60 covers ...127, no gaps
	if zs[0].KeyLo != 0 || zs[8].KeyHi != 127 {
		t.Fatalf("edges %d %d", zs[0].KeyLo, zs[8].KeyHi)
	}
	if zs[3].KeyLo != zs[0].KeyHi+1 || zs[6].KeyLo != zs[3].KeyHi+1 {
		t.Fatalf("gap: %+v %+v %+v", zs[0], zs[3], zs[6])
	}
	// velocity ranges tile 1..127 per key
	if zs[0].VelLo != 1 || zs[1].VelLo != zs[0].VelHi+1 || zs[2].VelHi != 127 {
		t.Fatalf("vel %+v %+v %+v", zs[0], zs[1], zs[2])
	}
	if zs[4].Root != 54 {
		t.Fatal("root")
	}
}

func TestRunStopsOnErrorAndCancel(t *testing.T) {
	c := Config{LowNote: 48, HighNote: 60, Stride: 1, Layers: 1, HoldSec: 0.1, ReleaseSec: 0, FindLoops: false}
	zs, err := Run(context.Background(), c, &fakeSynth{rate: 8000, fail: 3}, nil)
	if err == nil || len(zs) != 2 {
		t.Fatalf("%v %d", err, len(zs))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, c, &fakeSynth{rate: 8000}, nil); err == nil {
		t.Fatal("cancelled context should stop")
	}
}

func TestFindLoopOnSteadyTone(t *testing.T) {
	rate := 48000
	for _, freq := range []float64{110, 220.5, 440, 1000} {
		n := rate * 2
		x := make([]float32, n)
		for i := range x {
			x[i] = float32(0.6*math.Sin(2*math.Pi*freq*float64(i)/float64(rate)) + 0.2*math.Sin(4*math.Pi*freq*float64(i)/float64(rate)))
		}
		l, ok := FindLoop(x, rate, 0, rate*3/2)
		if !ok {
			t.Fatalf("%v Hz: no loop", freq)
		}
		if !(x[l.Start-1] <= 0 && x[l.Start] > 0) || !(x[l.End-1] <= 0 && x[l.End] > 0) {
			t.Fatalf("%v Hz: ends are not rising zero crossings", freq)
		}
		period := float64(rate) / freq
		k := float64(l.End-l.Start) / period
		if math.Abs(k-math.Round(k)) > 0.05 {
			t.Fatalf("%v Hz: loop is %.3f periods", freq, k)
		}
		if l.End-l.Start < rate/20 {
			t.Fatalf("%v Hz: loop too short", freq)
		}
		// the jump must be seamless: signal just before End matches just before Start
		var e float64
		for i := 1; i < 64; i++ {
			d := float64(x[l.End-i] - x[l.Start-i])
			e += d * d
		}
		if e/64 > 1e-3 {
			t.Fatalf("%v Hz: seam error %v", freq, e/64)
		}
	}
}

func TestFindLoopRejectsNoiseAndShort(t *testing.T) {
	rate := 48000
	x := make([]float32, rate*2)
	seed := uint32(7)
	for i := range x {
		seed = seed*1664525 + 1013904223
		x[i] = float32(int32(seed)>>8) / float32(1<<23)
	}
	if _, ok := FindLoop(x, rate, 0, rate*3/2); ok {
		t.Fatal("noise must not loop")
	}
	if _, ok := FindLoop(x[:1000], rate, 0, 900); ok {
		t.Fatal("too short")
	}
	if _, ok := FindLoop(x, rate, -1, 10); ok {
		t.Fatal("bad on")
	}
}
