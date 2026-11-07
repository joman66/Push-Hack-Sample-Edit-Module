package monitor

import "testing"

func TestOffAddsNothing(t *testing.T) {
	m := New(48000)
	m.Push([][]float32{{1, 1}, {1, 1}})
	out := make([]float32, 4)
	m.Render(out, 48000)
	for _, v := range out {
		if v != 0 {
			t.Fatal("off monitor made sound")
		}
	}
}

func TestPassThroughAndMix(t *testing.T) {
	m := New(48000)
	m.SetOn(true)
	m.Push([][]float32{{0.5, 0.5, 0.5, 0.5}, {0.25, 0.25, 0.25, 0.25}})
	out := []float32{0.1, 0.1, 0.1, 0.1}
	m.Render(out, 48000)
	if out[0] != 0.6 || out[1] != 0.35 {
		t.Fatalf("%v", out)
	}
}

func TestMonoToBoth(t *testing.T) {
	m := New(48000)
	m.SetOn(true)
	m.Push([][]float32{{0.5, 0.5, 0.5}})
	out := make([]float32, 2)
	m.Render(out, 48000)
	if out[0] != 0.5 || out[1] != 0.5 {
		t.Fatalf("%v", out)
	}
}

func TestResample(t *testing.T) {
	m := New(24000) // half the output rate: each input frame lasts two output frames
	m.SetOn(true)
	m.Push([][]float32{{0, 1, 0, 1, 0}, {0, 1, 0, 1, 0}})
	out := make([]float32, 8)
	m.Render(out, 48000)
	want := []float32{0, 0.5, 1, 0.5}
	for i, w := range want {
		if out[2*i] != w {
			t.Fatalf("frame %d: %v want %v", i, out[2*i], w)
		}
	}
}

func TestLatencyCap(t *testing.T) {
	m := New(48000)
	m.SetOn(true)
	big := make([]float32, 20000)
	m.Push([][]float32{big, big})
	m.mu.Lock()
	n := len(m.l)
	m.mu.Unlock()
	if n > maxFrames {
		t.Fatalf("buffered %d", n)
	}
}

func TestSetOnClears(t *testing.T) {
	m := New(48000)
	m.SetOn(true)
	m.Push([][]float32{{1, 1, 1}, {1, 1, 1}})
	m.SetOn(false)
	m.SetOn(true)
	out := make([]float32, 2)
	m.Render(out, 48000)
	if out[0] != 0 {
		t.Fatal("old audio played")
	}
}
