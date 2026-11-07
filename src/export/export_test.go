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

// val returns the Value of the first element tag that follows from in s.
func val(t *testing.T, s, from, tag string) string {
	t.Helper()
	i := strings.Index(s, from)
	if i < 0 {
		t.Fatalf("missing %q", from)
	}
	k := strings.Index(s[i:], "<"+tag+" Value=\"")
	if k < 0 {
		t.Fatalf("no <%s> after %q", tag, from)
	}
	r := s[i+k+len(tag)+9:]
	return r[:strings.Index(r, "\"")]
}

func noPlaceholders(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, "{{") {
		t.Fatal("placeholder left in the output")
	}
}

func TestSimpler(t *testing.T) {
	z := zone()
	z.Path = UserLibrary + "Samples/Imported/SampleEdit/a & b.wav"
	z.ModTime = 1794085945
	x := Simpler(z)
	wellFormed(t, x)
	s := string(x)
	noPlaceholders(t, s)
	if !strings.HasPrefix(s, "<?xml") || !strings.Contains(s, "\n\t<OriginalSimpler>") || strings.Contains(s, "InstrumentGroupDevicePreset") {
		t.Fatal("root must be OriginalSimpler directly under Ableton, as Live writes it")
	}
	for _, want := range []string{
		`<RootKey Value="60" />`, `<SampleEnd Value="47999" />`, `<DefaultDuration Value="48000" />`,
		`<RelativePathType Value="6" />`, `<RelativePath Value="Samples/Imported/SampleEdit/a &amp; b.wav" />`,
		`query:UserLibrary#Samples:Imported:SampleEdit:a &amp; b.wav`, `Kick &#34;1&#34;`,
		`<LastModDate Value="1794085945" />`, `<OriginalFileSize Value="288044" />`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	// the sustain loop: frames 1000-20000, mode 1 (forward), crossfade 64
	sl := s[strings.Index(s, "<SustainLoop>"):]
	sl = sl[:strings.Index(sl, "</SustainLoop>")]
	for _, w := range []string{`<Start Value="1000" />`, `<End Value="20000" />`, `<Mode Value="1" />`, `<Crossfade Value="64" />`} {
		if !strings.Contains(sl, w) {
			t.Fatalf("loop: missing %q in %s", w, sl)
		}
	}
	if v := val(t, s, "<TransposeKey>", "Manual"); v != "3" {
		t.Fatalf("transpose key %s", v)
	}
	if v := val(t, s, "<TransposeFine>", "Manual"); v != "50" {
		t.Fatalf("transpose fine %s", v)
	}
	if v := val(t, s, "<IsSimpler", "PlaybackMode"); v != "0" {
		t.Fatalf("playback mode %s", v)
	}
}

func TestSimplerOutsideUserLibrary(t *testing.T) {
	x := string(Simpler(zone())) // /data/Music/a & b/s.wav
	wellFormed(t, []byte(x))
	if !strings.Contains(x, `<RelativePathType Value="0" />`) || !strings.Contains(x, `<Path Value="/data/Music/a &amp; b/s.wav" />`) ||
		!strings.Contains(x, `<BrowserContentPath Value="" />`) {
		t.Fatal("a sample outside the User Library keeps only its absolute path")
	}
}

func TestSamplerZones(t *testing.T) {
	a, b := zone(), zone()
	a.KeyLo, a.KeyHi, a.VelLo, a.VelHi, a.Root = 0, 50, 1, 64, 48
	b.KeyLo, b.KeyHi, b.VelLo, b.VelHi, b.Root, b.Loop = 51, 127, 65, 127, 54, nil
	x := Sampler([]Zone{a, b})
	wellFormed(t, x)
	s := string(x)
	noPlaceholders(t, s)
	if strings.Count(s, "<MultiSamplePart ") != 2 || !strings.Contains(s, "\n\t<MultiSampler>") {
		t.Fatal("two parts in a MultiSampler expected")
	}
	if !strings.Contains(s, "<Min Value=\"51\" />\n\t\t\t\t\t\t\t<Max Value=\"127\" />") || !strings.Contains(s, `<Min Value="65" />`) {
		t.Fatal("ranges")
	}
	// the template came from a factory preset; its sound must be reset
	for tag, want := range map[string]string{"<TransposeKey>": "0", "<KeyZoneShift>": "0"} {
		if v := val(t, s, tag, "Manual"); v != want {
			t.Fatalf("%s = %s, want %s", tag, v, want)
		}
	}
	if strings.Contains(s, "Arp 5th") || strings.Contains(s, "Core Library") {
		t.Fatal("factory preset leftovers")
	}
	if v := val(t, s, "<SubOsc>", "Manual"); v != "false" {
		t.Fatalf("sub oscillator %s", v)
	}
}

func TestDrumRackPads(t *testing.T) {
	var pads []Pad
	for i := 0; i < 3; i++ {
		z := zone()
		z.Loop = nil
		z.Name = "Kit " + string(rune('A'+i))
		z.Start, z.End = i*1000, i*1000+900
		pads = append(pads, Pad{Zone: z, Note: 36 + i, Choke: i})
	}
	x := DrumRack(pads)
	wellFormed(t, x)
	s := string(x)
	noPlaceholders(t, s)
	if !strings.Contains(s, "\n\t\t<GroupDevicePreset>") && !strings.Contains(s, "<GroupDevicePreset>") {
		t.Fatal("GroupDevicePreset root")
	}
	if strings.Count(s, "<DrumBranchPreset ") != 3 || strings.Count(s, "<OriginalSimpler ") != 3 {
		t.Fatal("3 branches, each with a Simpler")
	}
	for _, want := range []string{`<ReceivingNote Value="92" />`, `<ReceivingNote Value="91" />`, `<ReceivingNote Value="90" />`,
		`<ChokeGroup Value="2" />`, `<Name Value="Kit B" />`, `<SampleStart Value="1000" />`, `<SampleEnd Value="1899" />`,
		`<DrumBranchPreset Id="2">`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Count(s, `<PlaybackMode Value="1" />`) != 3 {
		t.Fatal("pads play One-Shot")
	}
	seeds := map[string]bool{}
	for _, part := range strings.Split(s, "<RoundRobinRandomSeed Value=\"")[1:] {
		seeds[part[:strings.Index(part, "\"")]] = true
	}
	if len(seeds) != 3 {
		t.Fatalf("each pad needs its own seed: %v", seeds)
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
