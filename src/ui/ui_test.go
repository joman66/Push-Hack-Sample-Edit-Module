package ui

import (
	"testing"

	"github.com/federico-pepe/push-hack-sample-edit/src/app"
)

func TestDrawDoesNotPanicAndPaintsWave(t *testing.T) {
	img := New()
	f := app.Frame{Mode: app.ModeTrim, ViewStart: 0, ViewSpan: 960, Frames: 960, Rate: 48000, Playhead: 100,
		Slices: [][2]int{{0, 400}, {400, 960}}, Cur: 1, Status: "Hello"}
	f.Sel.Start, f.Sel.End = 100, 300
	f.Peaks = make([][2]float32, 960)
	for i := range f.Peaks {
		f.Peaks[i] = [2]float32{-0.5, 0.5}
	}
	Draw(img, f)
	if img.NRGBAAt(500, waveY+waveH/2).A == 0 {
		t.Fatal("waveform not drawn")
	}
	for m := app.ModeRecord; m <= app.ModeExport; m++ {
		f.Mode = m
		Draw(img, f)
	}
	f.Frames, f.Peaks = 0, make([][2]float32, 960)
	Draw(img, f)
}

func TestDrawManager(t *testing.T) {
	img := New()
	f := app.Frame{Mode: app.ModeRecord}
	f.Mgr = &app.MgrFrame{Dir: "/data/Music/Samples", Index: 2, Total: 9, Cursor: 1, Scroll: true,
		Rows: []app.MgrRow{{Name: "Drums", Dir: true}, {Name: "kick.wav", Detail: "1.0s 48k S 600KB", Marked: true},
			{Name: "snare.wav", Detail: "0.5s 48k M 300KB", Playing: true}, {Name: "bad.wav", Detail: "unreadable", Bad: true}}}
	Draw(img, f)
	if img.NRGBAAt(300, waveY+13+13+5).A == 0 { // cursor row highlight
		t.Fatal("cursor row not painted")
	}
	f.Mgr.Confirm = true
	Draw(img, f)
	f.Mgr = &app.MgrFrame{Dir: "/x"}
	Draw(img, f) // empty folder
}
