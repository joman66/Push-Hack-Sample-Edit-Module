package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/audiobus"
	"github.com/federico-pepe/push-hack-sample-edit/src/monitor"
	"github.com/federico-pepe/push-hack-sample-edit/src/record"
)

// The input of the bus, as the loopback module fills it, reaches the recorder.
func TestCaptureFromBusInput(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "abus")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	s := audiobus.NewServer(dir)
	s.SetFormat(audiobus.Format{Rate: 44100, Period: 16, Channels: 4})
	if err := s.Listen(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	bus := audiobus.OpenWithInputs(audiobus.SocketPath(dir), "SampleEdit", -1, []string{inputName})
	defer bus.Close()
	rec := record.New(record.Config{Rate: 48000, Channels: 2, TmpDir: t.TempDir()})
	src := &sourceCtl{bus: bus, rec: rec, mon: monitor.New(48000)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 300 && !bus.Connected(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if err := src.on(ctx); err != nil {
		t.Fatal(err)
	}
	defer src.off()
	if rec.Rate() != 44100 {
		t.Fatalf("recorder rate %d, want the bus rate", rec.Rate())
	}
	if err := rec.Start(); err != nil {
		t.Fatal(err)
	}
	in := s.Inputs()[0]
	blk := make([]float32, 16*2)
	for i := 0; i < 16; i++ {
		blk[2*i], blk[2*i+1] = 0.5, -0.25
	}
	for i := 0; i < 20; i++ {
		for !in.Write(blk) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	time.Sleep(100 * time.Millisecond)
	b, err := rec.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if b.Frames() != 320 || b.Ch[0][100] != 0.5 || b.Ch[1][100] != -0.25 {
		t.Fatalf("frames %d l %v r %v", b.Frames(), b.Ch[0][100], b.Ch[1][100])
	}
}

// With no loopback service there is no input, and the error says what to do.
func TestCaptureWithoutBus(t *testing.T) {
	bus := audiobus.OpenWithInputs("/tmp/none-such.sock", "SampleEdit", -1, []string{inputName})
	defer bus.Close()
	src := &sourceCtl{bus: bus, rec: record.New(record.Config{TmpDir: t.TempDir()}), mon: monitor.New(44100)}
	if err := src.on(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}
