// Package export writes Ableton Live instrument presets that point at an
// exported WAV: Simpler (one sample), Sampler (many key and velocity zones)
// and Drum Rack (one pad per slice).
//
// The XML follows the Live 12 preset layout as far as it is known, and
// carries the nodes that matter (sample ref, key and velocity ranges, root
// key, loop, choke group, pad note). It has NOT been loaded in a real Live
// yet. Check it on a Push first, as CLAUDE.md asks of any new file format.
package export

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// Loop is a sustain loop in frames of the sample.
type Loop struct{ Start, End int }

// Zone is one sample part.
type Zone struct {
	Path         string // absolute path of the WAV
	Name         string
	Frames, Rate int
	FileSize     int64
	Root         int
	KeyLo, KeyHi int
	VelLo, VelHi int
	Start, End   int // played window in frames; End 0 = whole file
	Loop         *Loop
	Crossfade    int     // loop crossfade in frames
	Transpose    float64 // semitones, non-destructive pitch
}

func (z Zone) window() (int, int) {
	e := z.End
	if e <= 0 || e > z.Frames {
		e = z.Frames
	}
	return z.Start, e
}

// Pad is one Drum Rack pad.
type Pad struct {
	Zone
	Note  int // the pad's input note (36 = first pad)
	Choke int // 0 = none, 1-16
}

const header = `<?xml version="1.0" encoding="UTF-8"?>
<Ableton MajorVersion="5" MinorVersion="12.0_12402" SchemaChangeCount="5" Creator="Ableton Live 12.4.6" Revision="">
`

func esc(s string) string { return html.EscapeString(s) }

func rng(b *strings.Builder, tag string, lo, hi int) {
	fmt.Fprintf(b, "<%s><Min Value=\"%d\" /><Max Value=\"%d\" /><CrossfadeMin Value=\"%d\" /><CrossfadeMax Value=\"%d\" /></%s>\n", tag, lo, hi, lo, hi, tag)
}

func loopXML(b *strings.Builder, tag string, z Zone) {
	_, e := z.window()
	if z.Loop != nil {
		fmt.Fprintf(b, "<%s><Start Value=\"%d\" /><End Value=\"%d\" /><Mode Value=\"1\" /><Crossfade Value=\"%d\" /><Detune Value=\"0\" /></%s>\n",
			tag, z.Loop.Start, z.Loop.End, z.Crossfade, tag)
		return
	}
	fmt.Fprintf(b, "<%s><Start Value=\"0\" /><End Value=\"%d\" /><Mode Value=\"0\" /><Crossfade Value=\"0\" /><Detune Value=\"0\" /></%s>\n", tag, e, tag)
}

func samplePart(b *strings.Builder, id int, z Zone) {
	s, e := z.window()
	fmt.Fprintf(b, "<MultiSamplePart Id=\"%d\">\n", id)
	b.WriteString("<LomId Value=\"0\" /><Selection Value=\"true\" /><IsActive Value=\"true\" /><Solo Value=\"false\" />\n")
	fmt.Fprintf(b, "<Name Value=\"%s\" />\n", esc(z.Name))
	rng(b, "KeyRange", z.KeyLo, z.KeyHi)
	rng(b, "VelocityRange", max(z.VelLo, 1), min(max(z.VelHi, 1), 127))
	rng(b, "SelectorRange", 0, 127)
	fmt.Fprintf(b, "<RootKey Value=\"%d\" /><Detune Value=\"0\" /><TuneScale Value=\"100\" /><Panorama Value=\"0\" /><Volume Value=\"1\" /><Link Value=\"false\" />\n", z.Root)
	fmt.Fprintf(b, "<SampleStart Value=\"%d\" /><SampleEnd Value=\"%d\" />\n", s, e)
	loopXML(b, "SustainLoop", z)
	b.WriteString("<ReleaseLoop><Start Value=\"0\" /><End Value=\"0\" /><Mode Value=\"3\" /><Crossfade Value=\"0\" /><Detune Value=\"0\" /></ReleaseLoop>\n")
	b.WriteString("<SampleRef><FileRef>\n")
	fmt.Fprintf(b, "<HasRelativePath Value=\"false\" /><RelativePathType Value=\"0\" /><RelativePath />\n")
	fmt.Fprintf(b, "<Name Value=\"%s\" /><Type Value=\"1\" /><Path Value=\"%s\" />\n", esc(filepath.Base(z.Path)), esc(z.Path))
	fmt.Fprintf(b, "<LivePackName Value=\"\" /><LivePackId Value=\"\" /><OriginalFileSize Value=\"%d\" /><OriginalCrc Value=\"0\" />\n", z.FileSize)
	b.WriteString("</FileRef>\n")
	fmt.Fprintf(b, "<LastModDate Value=\"0\" /><SourceContext /><SampleUsageHint Value=\"0\" /><DefaultDuration Value=\"%d\" /><DefaultSampleRate Value=\"%d\" />\n", z.Frames, z.Rate)
	b.WriteString("</SampleRef>\n</MultiSamplePart>\n")
}

func pitch(b *strings.Builder, semis float64) {
	whole := int(semis)
	fine := int((semis - float64(whole)) * 100)
	fmt.Fprintf(b, "<Pitch><TransposeKey><Manual Value=\"%d\" /></TransposeKey><TransposeFine><Manual Value=\"%d\" /></TransposeFine></Pitch>\n", whole, fine)
}

func simplerDevice(b *strings.Builder, id int, zones []Zone, multi bool, classic bool) {
	tag := "OriginalSimpler"
	if multi {
		tag = "MultiSampler"
	}
	fmt.Fprintf(b, "<%s Id=\"%d\">\n", tag, id)
	b.WriteString("<LomId Value=\"0\" /><IsExpanded Value=\"true\" /><On><LomId Value=\"0\" /><Manual Value=\"true\" /></On>\n")
	b.WriteString("<Player><MultiSampleMap><SampleParts>\n")
	for i, z := range zones {
		samplePart(b, i, z)
	}
	b.WriteString("</SampleParts></MultiSampleMap>\n</Player>\n")
	if len(zones) > 0 {
		pitch(b, zones[0].Transpose)
	}
	if !multi {
		mode := 0
		if classic {
			mode = 0
		}
		fmt.Fprintf(b, "<Globals><PlaybackMode Value=\"%d\" /></Globals>\n", mode)
	}
	fmt.Fprintf(b, "</%s>\n", tag)
}

func wrap(inner func(b *strings.Builder)) []byte {
	var b strings.Builder
	b.WriteString(header)
	inner(&b)
	b.WriteString("</Ableton>\n")
	return []byte(b.String())
}

// Simpler is one sample in Simpler. Loop, root and window come from z.
func Simpler(z Zone) []byte {
	if z.KeyHi == 0 && z.KeyLo == 0 {
		z.KeyLo, z.KeyHi = 0, 127
	}
	if z.VelHi == 0 {
		z.VelLo, z.VelHi = 1, 127
	}
	return wrap(func(b *strings.Builder) {
		b.WriteString("<InstrumentGroupDevicePreset><Device>\n")
		simplerDevice(b, 0, []Zone{z}, false, true)
		b.WriteString("</Device></InstrumentGroupDevicePreset>\n")
	})
}

// Sampler is many zones in one Sampler: the auto-sampler's result.
func Sampler(zones []Zone) []byte {
	return wrap(func(b *strings.Builder) {
		b.WriteString("<InstrumentGroupDevicePreset><Device>\n")
		simplerDevice(b, 0, zones, true, false)
		b.WriteString("</Device></InstrumentGroupDevicePreset>\n")
	})
}

// DrumRack maps each pad's slice to its own pad, with choke group.
func DrumRack(pads []Pad) []byte {
	return wrap(func(b *strings.Builder) {
		b.WriteString("<GroupDevicePreset><Device>\n<DrumGroupDevice Id=\"0\">\n")
		b.WriteString("<LomId Value=\"0\" /><IsExpanded Value=\"true\" /><On><LomId Value=\"0\" /><Manual Value=\"true\" /></On>\n<Branches>\n")
		for i, p := range pads {
			z := p.Zone
			z.KeyLo, z.KeyHi = 0, 127
			if z.Root == 0 {
				z.Root = 60
			}
			if z.VelHi == 0 {
				z.VelLo, z.VelHi = 1, 127
			}
			fmt.Fprintf(b, "<DrumBranch Id=\"%d\">\n<LomId Value=\"0\" /><Name Value=\"%s\" /><IsSelected Value=\"%t\" />\n", i, esc(z.Name), i == 0)
			// Live stores the receiving note as 128 minus the note.
			fmt.Fprintf(b, "<ZoneSettings><ReceivingNote Value=\"%d\" /><SendingNote Value=\"60\" /><ChokeGroup Value=\"%d\" /></ZoneSettings>\n", 128-p.Note, p.Choke)
			b.WriteString("<DeviceChain><Devices>\n")
			simplerDevice(b, 0, []Zone{z}, false, false)
			b.WriteString("</Devices></DeviceChain>\n</DrumBranch>\n")
		}
		b.WriteString("</Branches>\n</DrumGroupDevice>\n</Device></GroupDevicePreset>\n")
	})
}

// WritePreset gzips the XML into path (.adv or .adg). With keepXML it also
// leaves a plain .xml copy next to it.
func WritePreset(path string, xml []byte, keepXML bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(xml); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if keepXML {
		return os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+".xml", xml, 0o644)
	}
	return nil
}

// SafeName makes a string safe as a file name.
func SafeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == ' ', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), " .")
	if out == "" {
		return "sample"
	}
	return out
}
