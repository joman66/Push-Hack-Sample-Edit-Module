package playback

import (
	"math"
	"testing"
)

func ramp(n int) [][]float32 {
	x := make([]float32, n)
	for i := range x {
		x[i] = float32(i)
	}
	return [][]float32{x}
}

func TestPlayOnceAndFinish(t *testing.T) {
	e := New(48000)
	e.Play(ramp(100), 48000, Opts{})
	out := make([]float32, 2*40)
	e.Render(out)
	if out[2*10] != 10 || out[2*10+1] != 10 { // mono is copied to both sides
		t.Fatalf("%v", out[18:24])
	}
	e.Render(out)
	e.Render(out)
	if e.Playing() {
		t.Fatal("should have finished")
	}
}

func TestRateAndPitch(t *testing.T) {
	e := New(48000)
	e.Play(ramp(1000), 24000, Opts{}) // half rate source: step 0.5
	out := make([]float32, 2*10)
	e.Render(out)
	if math.Abs(float64(out[2*4])-2) > 1e-4 {
		t.Fatalf("step: %v", out[8])
	}
	e2 := New(48000)
	e2.Play(ramp(1000), 48000, Opts{Semitones: 12})
	e2.Render(out)
	if math.Abs(float64(out[2*4])-8) > 1e-3 {
		t.Fatalf("octave up: %v", out[8])
	}
}

func TestLoopStopAndGain(t *testing.T) {
	e := New(48000)
	h := e.Play(ramp(20), 48000, Opts{Loop: true, Gain: 0.5})
	out := make([]float32, 2*100)
	e.Render(out)
	if !e.Playing() {
		t.Fatal("loop should keep playing")
	}
	if out[2*5] != 2.5 {
		t.Fatalf("gain: %v", out[10])
	}
	e.Stop(h)
	e.Render(out)
	if e.Playing() {
		t.Fatal("stop")
	}
}

func TestChokeGroup(t *testing.T) {
	e := New(48000)
	e.Play(ramp(10000), 48000, Opts{Group: 2})
	e.Play(ramp(10000), 48000, Opts{Group: 2, Start: 5000})
	out := make([]float32, 2*4)
	e.Render(out)
	if out[0] != 5000 { // only the second voice sounds
		t.Fatalf("choke failed: %v", out[0])
	}
}

func TestBadArgs(t *testing.T) {
	e := New(48000)
	if e.Play(nil, 48000, Opts{}) != -1 || e.Play(ramp(10), 48000, Opts{Start: 50}) != -1 {
		t.Fatal("bad args should not start a voice")
	}
	e.Play(ramp(10), 48000, Opts{Start: 2, End: 6})
	if p, ok := e.Position(); !ok || p != 2 {
		t.Fatal("position")
	}
}

// A document edit can swap or shorten a channel slice while a voice plays.
// The voice must not read past the audio it was given (it crashed the app).
func TestVoiceSurvivesShorterChannel(t *testing.T) {
	e := New(48000)
	ch := [][]float32{make([]float32, 1000), make([]float32, 600)} // right is shorter
	h := e.Play(ch, 48000, Opts{})
	if h < 0 {
		t.Fatal("no voice")
	}
	ch[0] = ch[0][:100] // the edit swaps the document's slice
	out := make([]float32, 2*256)
	for i := 0; i < 10; i++ {
		e.Render(out) // must not panic
	}
	if e.Playing() {
		t.Fatal("the voice should have ended")
	}
}

func TestStepFastNeverReadsPastEnd(t *testing.T) {
	e := New(48000)
	ch := [][]float32{make([]float32, 500)}
	e.Play(ch, 48000, Opts{Semitones: 24, Loop: true})
	out := make([]float32, 2*256)
	for i := 0; i < 50; i++ {
		e.Render(out)
	}
}
