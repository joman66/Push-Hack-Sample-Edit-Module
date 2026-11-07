package slicer

import (
	"math"
	"testing"
)

// clicks makes decaying noise bursts at the given frame positions.
func clicks(n, rate int, at []int) []float32 {
	x := make([]float32, n)
	seed := uint32(1)
	for _, p := range at {
		for i := 0; i < 2400 && p+i < n; i++ {
			seed = seed*1664525 + 1013904223
			r := float32(int32(seed)>>8) / float32(1<<23)
			x[p+i] += r * float32(math.Exp(-float64(i)/500)) * 0.8
		}
	}
	return x
}

func TestTransientsFindsHits(t *testing.T) {
	at := []int{4000, 20000, 36000, 52000, 68000}
	x := clicks(90000, 48000, at)
	got := Transients(x, 48000, 0.6, 2400)
	if got[0] != 0 {
		t.Fatal("must start at 0")
	}
	if len(got) != len(at)+1 {
		t.Fatalf("got %v", got)
	}
	for i, p := range at {
		if d := got[i+1] - p; d < -800 || d > 800 {
			t.Fatalf("hit %d at %d, found %d", i, p, got[i+1])
		}
	}
}

func TestTransientsSilenceAndSensitivity(t *testing.T) {
	if got := Transients(make([]float32, 50000), 48000, 1, 0); len(got) != 1 {
		t.Fatalf("silence: %v", got)
	}
	at := []int{4000, 12000, 20000}
	x := clicks(40000, 48000, at)
	for i := range x {
		x[i] += 0.002 * float32(math.Sin(float64(i)))
	}
	hi := len(Transients(x, 48000, 1, 2400))
	lo := len(Transients(x, 48000, 0, 2400))
	if lo > hi {
		t.Fatalf("lower sensitivity found more: %d > %d", lo, hi)
	}
}

func TestTempo(t *testing.T) {
	rate := 48000
	beat := rate / 2 // 120 BPM
	var at []int
	for i := 0; i < 16; i++ {
		at = append(at, 1000+i*beat)
	}
	x := clicks(1000+17*beat, rate, at)
	if bpm := Tempo(x, rate); math.Abs(bpm-120) > 2 {
		t.Fatalf("bpm %v", bpm)
	}
	if Tempo(make([]float32, 100000), rate) != 0 {
		t.Fatal("silence should give 0")
	}
}

func TestGridAndEqual(t *testing.T) {
	g := Grid(96000, 48000, 120, 8, 0) // 1/8 at 120 = 12000 frames
	if len(g) != 8 || g[1] != 12000 {
		t.Fatalf("grid %v", g)
	}
	g = Grid(96000, 48000, 120, 4, 6000)
	if g[0] != 0 || g[1] != 6000 {
		t.Fatalf("anchored grid %v", g)
	}
	if len(Grid(48000*600, 48000, 120, 32, 0)) != MaxSlices {
		t.Fatal("grid must cap at 64")
	}
	e := Equal(1000, 4)
	if len(e) != 4 || e[3] != 750 {
		t.Fatalf("equal %v", e)
	}
	if len(Equal(1000, 500)) != MaxSlices {
		t.Fatal("equal caps at 64")
	}
	if len(Grid(1000, 48000, 0, 8, 0)) != 1 {
		t.Fatal("no bpm")
	}
}

func TestMarkersAndEdit(t *testing.T) {
	s := FromMarkers([]int{0, 100, 300}, 1000)
	if len(s) != 3 || s[2].End != 1000 || s[0].End != 100 {
		t.Fatalf("%v", s)
	}
	s[1].Choke = 3
	s, ok := AddMarker(s, 200, 1000, 50)
	if !ok || len(s) != 4 || s[2].Start != 200 {
		t.Fatalf("add: %v", s)
	}
	if s[1].Choke != 3 {
		t.Fatal("choke lost on add")
	}
	if _, ok := AddMarker(s, 210, 1000, 50); ok {
		t.Fatal("too close should refuse")
	}
	SetStart(s, 1, 5000)
	if s[1].Start != s[1].End-MinLen {
		t.Fatalf("start clamp: %v", s[1])
	}
	SetEnd(s, 1, -5, 1000)
	if s[1].End != s[1].Start+MinLen {
		t.Fatalf("end clamp: %v", s[1])
	}
	SetEnd(s, 99, 1, 1000) // out of range is ignored
	full := make([]int, 70)
	for i := range full {
		full[i] = i * 10
	}
	if len(FromMarkers(full, 1000)) != MaxSlices {
		t.Fatal("cap")
	}
}
