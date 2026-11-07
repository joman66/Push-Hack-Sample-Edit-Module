package library

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/autosample"
	"github.com/federico-pepe/push-hack-sample-edit/src/slicer"
)

func lib(t *testing.T) *Library {
	d := t.TempDir()
	return &Library{D: Dirs{Samples: d + "/s", Simpler: d + "/si", Sampler: d + "/sa", Drum: d + "/d", KeepXML: true}}
}

func tone(rate, n int) *audio.Buffer {
	b := audio.New(rate, 1, n)
	for i := range b.Ch[0] {
		b.Ch[0][i] = float32(0.5 * math.Sin(float64(i)/10))
	}
	return b
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	l := lib(t)
	p, err := l.SaveWAV("my/sample", tone(48000, 4800), 120, 60, &audio.Range{Start: 100, End: 900}, []slicer.Slice{{Start: 0, End: 2000}, {Start: 2000, End: 4800}})
	if err != nil || !strings.HasSuffix(p, "my_sample.wav") {
		t.Fatalf("%v %v", p, err)
	}
	b, err := l.Load("my/sample")
	if err != nil || b.Frames() != 4800 || b.Rate != 48000 {
		t.Fatalf("%v %v", err, b)
	}
}

func TestResamplesTo48kAndScalesMarkers(t *testing.T) {
	l := lib(t)
	p, _ := l.SaveWAV("x", tone(24000, 2400), 0, -1, nil, nil)
	b, _ := l.Load("x")
	if b.Rate != 48000 || b.Frames() != 4800 {
		t.Fatalf("rate %d frames %d", b.Rate, b.Frames())
	}
	_ = p
	pre, err := l.SaveSimpler("x", p, tone(24000, 2400), 60, &audio.Range{Start: 100, End: 200}, 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(strings.TrimSuffix(pre, ".adv") + ".xml")
	if !strings.Contains(string(raw), "<Start Value=\"200\" />\n\t\t\t\t\t\t\t<End Value=\"400\" />") {
		t.Fatal("loop points should be scaled to 48k")
	}
}

func TestDrumRackAndSampler(t *testing.T) {
	l := lib(t)
	b := tone(48000, 9600)
	p, _ := l.SaveWAV("kit", b, 0, -1, nil, nil)
	sl := []slicer.Slice{{Start: 0, End: 4000, Choke: 1, VelLo: 1, VelHi: 127}, {Start: 4000, End: 9600, VelLo: 1, VelHi: 127}}
	pre, err := l.SaveDrumRack("kit", p, b, sl)
	if err != nil || filepath.Ext(pre) != ".adg" {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(strings.TrimSuffix(pre, ".adg") + ".xml")
	if strings.Count(string(raw), "<DrumBranchPreset ") != 2 || !strings.Contains(string(raw), `<ChokeGroup Value="1" />`) {
		t.Fatal("drum rack content")
	}
	zs := []autosample.Zone{
		{Audio: tone(48000, 4800), Root: 48, KeyLo: 0, KeyHi: 50, VelLo: 1, VelHi: 127, Loop: &autosample.Loop{Start: 100, End: 3000}},
		{Audio: tone(48000, 4800), Root: 54, KeyLo: 51, KeyHi: 127, VelLo: 1, VelHi: 127},
	}
	sp, err := l.SaveSampler("synth", zs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(l.D.Samples, "synth_n048_v127.wav")); err != nil {
		t.Fatal("zone wav missing")
	}
	x, _ := os.ReadFile(strings.TrimSuffix(sp, ".adv") + ".xml")
	if strings.Count(string(x), "<MultiSamplePart ") != 2 {
		t.Fatal("sampler parts")
	}
}

func TestBrowseRoot(t *testing.T) {
	d := DefaultDirs()
	if BrowseRoot(d) != UserSamples {
		t.Fatalf("default should be the User Library samples: %s", BrowseRoot(d))
	}
	d.Samples = "/mnt/usb/samples"
	if BrowseRoot(d) != "/mnt/usb/samples" {
		t.Fatal("outside the User Library: just that folder")
	}
	d.Browse = "/data/Music/"
	if BrowseRoot(d) != "/data/Music" {
		t.Fatal("explicit root")
	}
}
