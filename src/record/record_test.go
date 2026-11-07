package record

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"
)

func blk(n int, amp float32) [][]float32 {
	b := [][]float32{make([]float32, n), make([]float32, n)}
	for i := 0; i < n; i++ {
		b[0][i], b[1][i] = amp, -amp
	}
	return b
}

func TestAutoTriggerWithPreRoll(t *testing.T) {
	r := New(Config{Rate: 1000, Channels: 2, ThresholdDB: -20, PreRollSec: 0.1, TmpDir: t.TempDir()})
	r.Arm(true)
	// 5 quiet blocks of 50 frames: the ring keeps the last 100 frames
	for i := 0; i < 5; i++ {
		r.Feed(blk(50, 0.01))
	}
	if r.State() != Armed {
		t.Fatal("should still be armed")
	}
	r.Feed(blk(50, 0.5)) // crosses -20 dB
	if r.State() != Recording {
		t.Fatal("should be recording")
	}
	r.Feed(blk(50, 0.5))
	b, err := r.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if b.Frames() != 100+50+50 {
		t.Fatalf("frames %d, want pre-roll 100 + 2 blocks", b.Frames())
	}
	if math.Abs(float64(b.Ch[0][0])-0.01) > 1e-5 || math.Abs(float64(b.Ch[0][100])-0.5) > 1e-5 {
		t.Fatalf("order wrong: %v %v", b.Ch[0][0], b.Ch[0][100])
	}
	if r.State() != Idle {
		t.Fatal("idle after stop")
	}
}

func TestManualStartMonoAndCap(t *testing.T) {
	r := New(Config{Rate: 1000, Channels: 2, Mono: true, MaxSec: 0.2, TmpDir: t.TempDir()})
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		r.Feed(blk(50, 0.5))
	}
	if !r.Full() || r.Frames() != 200 {
		t.Fatalf("cap: full=%v frames=%d", r.Full(), r.Frames())
	}
	b, err := r.Stop()
	if err != nil || len(b.Ch) != 1 || b.Frames() != 200 {
		t.Fatalf("%v %v", err, b)
	}
	if math.Abs(float64(b.Ch[0][10])) > 1e-5 { // +0.5 and -0.5 mix to 0
		t.Fatal("mono mix")
	}
}

func TestStopWithoutTakeAndCancel(t *testing.T) {
	r := New(Config{Rate: 1000, TmpDir: t.TempDir()})
	if _, err := r.Stop(); err != ErrNoTake {
		t.Fatal("expected ErrNoTake")
	}
	r.Start()
	r.Feed(blk(10, 0.1))
	r.Cancel()
	if r.State() != Idle {
		t.Fatal("cancel")
	}
}

func TestPumpDecodesS32(t *testing.T) {
	var raw bytes.Buffer
	for i := 0; i < 100; i++ {
		binary.Write(&raw, binary.LittleEndian, int32(1<<30))  // +0.5
		binary.Write(&raw, binary.LittleEndian, int32(-1<<30)) // -0.5
	}
	r := New(Config{Rate: 1000, Channels: 2, TmpDir: t.TempDir()})
	r.Start()
	err := Pump(context.Background(), nopCloser{&raw}, 2, 32, r)
	if err == nil {
		t.Fatal("expected EOF at the end")
	}
	b, _ := r.Stop()
	if b.Frames() != 100 || math.Abs(float64(b.Ch[0][50])-0.5) > 1e-5 || math.Abs(float64(b.Ch[1][50])+0.5) > 1e-5 {
		t.Fatalf("%d %v", b.Frames(), b.Ch[0][50])
	}
}

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }
