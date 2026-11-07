package export

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wellFormed(t *testing.T, b []byte) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("not well-formed XML: %v", err)
		}
	}
}

func zone() Zone {
	return Zone{Path: "/data/Music/a & b/s.wav", Name: `Kick "1"`, Frames: 48000, Rate: 48000, FileSize: 288044,
		Root: 60, Loop: &Loop{1000, 20000}, Crossfade: 64, Transpose: 3.5}
}

func TestSimpler(t *testing.T) {
	x := Simpler(zone())
	wellFormed(t, x)
	s := string(x)
	for _, want := range []string{`<RootKey Value="60"`, `<Start Value="1000" /><End Value="20000" /><Mode Value="1" /><Crossfade Value="64"`,
		`a &amp; b`, `Kick &#34;1&#34;`, `<Manual Value="3" />`, `<Manual Value="50" />`, `<Max Value="127"`, `OriginalSimpler`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestSamplerZones(t *testing.T) {
	a, b := zone(), zone()
	a.KeyLo, a.KeyHi, a.VelLo, a.VelHi, a.Root = 0, 50, 1, 64, 48
	b.KeyLo, b.KeyHi, b.VelLo, b.VelHi, b.Root, b.Loop = 51, 127, 65, 127, 54, nil
	x := Sampler([]Zone{a, b})
	wellFormed(t, x)
	s := string(x)
	if strings.Count(s, "<MultiSamplePart ") != 2 || !strings.Contains(s, "MultiSampler") {
		t.Fatal("two parts expected")
	}
	if !strings.Contains(s, `<KeyRange><Min Value="51" /><Max Value="127" />`) || !strings.Contains(s, `<VelocityRange><Min Value="65" />`) {
		t.Fatal("ranges")
	}
	if !strings.Contains(s, `<Mode Value="0" />`) {
		t.Fatal("unlooped part should have loop mode 0")
	}
}

func TestDrumRackPads(t *testing.T) {
	var pads []Pad
	for i := 0; i < 3; i++ {
		z := zone()
		z.Loop = nil
		z.Start, z.End = i*1000, i*1000+900
		pads = append(pads, Pad{Zone: z, Note: 36 + i, Choke: i})
	}
	x := DrumRack(pads)
	wellFormed(t, x)
	s := string(x)
	if strings.Count(s, "<DrumBranch ") != 3 {
		t.Fatal("3 branches")
	}
	if !strings.Contains(s, `<ReceivingNote Value="92" />`) || !strings.Contains(s, `<ChokeGroup Value="2" />`) {
		t.Fatal("pad note or choke")
	}
	if !strings.Contains(s, `<SampleStart Value="1000" /><SampleEnd Value="1900" />`) {
		t.Fatal("slice window")
	}
}

func TestWritePreset(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "x.adv")
	x := Simpler(zone())
	if err := WritePreset(p, x, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	back, _ := io.ReadAll(zr)
	if !bytes.Equal(back, x) {
		t.Fatal("gzip round trip")
	}
	if _, err := os.Stat(strings.TrimSuffix(p, ".adv") + ".xml"); err != nil {
		t.Fatal("xml copy missing")
	}
}

func TestSafeName(t *testing.T) {
	if SafeName("a/b:c*") != "a_b_c_" || SafeName("...") != "sample" || SafeName("My Kit 1") != "My Kit 1" {
		t.Fatal(SafeName("a/b:c*"))
	}
}
