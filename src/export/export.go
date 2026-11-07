// Package export writes Ableton Live instrument presets that point at an
// exported WAV: Simpler (one sample), Sampler (many key and velocity zones)
// and Drum Rack (one pad per slice).
//
// The XML comes from templates (tmpl/*.xml) that were cut from presets which
// a real Live 12.4.6 saved on a Push 3 (see scripts/make_preset_templates.py).
// Live refuses a preset with missing nodes, so only the parts that change are
// filled in: the sample parts, the transpose, the playback mode and, in a Drum
// Rack, the branches (name, pad note, choke group).
package export

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

//go:embed tmpl/simpler.xml
var tmplSimpler string

//go:embed tmpl/sampler.xml
var tmplSampler string

//go:embed tmpl/drumrack.xml
var tmplDrumRack string

//go:embed tmpl/drumbranch.xml
var tmplDrumBranch string

// UserLibrary is Live's User Library on a Push. A sample inside it is stored
// with a relative path, so the preset still finds it if the library moves.
const UserLibrary = "/data/Music/Ableton/User Library/"

// Loop is a sustain loop in frames of the sample.
type Loop struct{ Start, End int }

// Zone is one sample part.
type Zone struct {
	Path         string // absolute path of the WAV
	Name         string
	Frames, Rate int
	FileSize     int64
	ModTime      int64 // file modification time, unix seconds
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

func esc(s string) string { return html.EscapeString(s) }

// fileRef is the <FileRef> body for a WAV. Inside the User Library Live uses
// path type 6 and a relative path; elsewhere only the absolute path.
func fileRef(z Zone, indent string) string {
	relType, rel := 0, ""
	if r, ok := strings.CutPrefix(z.Path, UserLibrary); ok {
		relType, rel = 6, r
	}
	var b strings.Builder
	line := func(f string, a ...any) { fmt.Fprintf(&b, indent+f+"\n", a...) }
	line(`<RelativePathType Value="%d" />`, relType)
	line(`<RelativePath Value="%s" />`, esc(rel))
	line(`<Path Value="%s" />`, esc(z.Path))
	line(`<Type Value="3" />`)
	line(`<LivePackName Value="" />`)
	line(`<LivePackId Value="" />`)
	line(`<OriginalFileSize Value="%d" />`, z.FileSize)
	line(`<OriginalCrc Value="0" />`)
	line(`<SourceHint Value="" />`)
	return strings.TrimSuffix(b.String(), "\n")
}

func browserPath(z Zone) string {
	r, ok := strings.CutPrefix(z.Path, UserLibrary)
	if !ok {
		return ""
	}
	return "query:UserLibrary#" + strings.ReplaceAll(r, "/", ":")
}

func loopXML(b *strings.Builder, tag string, start, end, mode, xfade int) {
	fmt.Fprintf(b, "\t\t\t\t\t\t<%s>\n\t\t\t\t\t\t\t<Start Value=\"%d\" />\n\t\t\t\t\t\t\t<End Value=\"%d\" />\n\t\t\t\t\t\t\t<Mode Value=\"%d\" />\n\t\t\t\t\t\t\t<Crossfade Value=\"%d\" />\n\t\t\t\t\t\t\t<Detune Value=\"0\" />\n\t\t\t\t\t\t</%s>\n",
		tag, start, end, mode, xfade, tag)
}

func rangeXML(b *strings.Builder, tag string, lo, hi int) {
	fmt.Fprintf(b, "\t\t\t\t\t\t<%s>\n\t\t\t\t\t\t\t<Min Value=\"%d\" />\n\t\t\t\t\t\t\t<Max Value=\"%d\" />\n\t\t\t\t\t\t\t<CrossfadeMin Value=\"%d\" />\n\t\t\t\t\t\t\t<CrossfadeMax Value=\"%d\" />\n\t\t\t\t\t\t</%s>\n",
		tag, lo, hi, lo, hi, tag)
}

// samplePart is one <MultiSamplePart>, laid out as Live writes it. releaseMode
// is Live's default for the device: 3 in Simpler, 0 in Sampler.
func samplePart(b *strings.Builder, id int, z Zone, selected bool, releaseMode int) {
	s, e := z.window()
	last := e - 1 // Live stores the last frame, the file has Frames of them
	const t6 = "\t\t\t\t\t\t"
	fmt.Fprintf(b, "\t\t\t\t\t<MultiSamplePart Id=\"%d\" InitUpdateAreSlicesFromOnsetsEditableAfterRead=\"false\" HasImportedSlicePoints=\"false\" NeedsAnalysisData=\"false\">\n", id)
	fmt.Fprintf(b, t6+"<LomId Value=\"0\" />\n"+t6+"<Name Value=\"%s\" />\n"+t6+"<Selection Value=\"%t\" />\n"+t6+"<IsActive Value=\"true\" />\n"+t6+"<Solo Value=\"false\" />\n",
		esc(z.Name), selected)
	rangeXML(b, "KeyRange", z.KeyLo, z.KeyHi)
	rangeXML(b, "VelocityRange", max(z.VelLo, 1), min(max(z.VelHi, 1), 127))
	rangeXML(b, "SelectorRange", 0, 127)
	fmt.Fprintf(b, t6+"<RootKey Value=\"%d\" />\n"+t6+"<Detune Value=\"0\" />\n"+t6+"<TuneScale Value=\"100\" />\n"+t6+"<Panorama Value=\"0\" />\n"+t6+"<Volume Value=\"1\" />\n"+t6+"<Link Value=\"false\" />\n", z.Root)
	fmt.Fprintf(b, t6+"<SampleStart Value=\"%d\" />\n"+t6+"<SampleEnd Value=\"%d\" />\n", s, last)
	if z.Loop != nil {
		loopXML(b, "SustainLoop", z.Loop.Start, z.Loop.End, 1, z.Crossfade)
	} else {
		loopXML(b, "SustainLoop", 0, last, 0, 0)
	}
	loopXML(b, "ReleaseLoop", 0, last, releaseMode, 0)
	ref := fileRef(z, t6+"\t\t")
	b.WriteString(t6 + "<SampleRef>\n" + t6 + "\t<FileRef>\n" + ref + "\n" + t6 + "\t</FileRef>\n")
	fmt.Fprintf(b, t6+"\t<LastModDate Value=\"%d\" />\n", z.ModTime)
	b.WriteString(t6 + "\t<SourceContext>\n" + t6 + "\t\t<SourceContext Id=\"0\">\n" + t6 + "\t\t\t<OriginalFileRef>\n" + t6 + "\t\t\t\t<FileRef Id=\"0\">\n")
	b.WriteString(fileRef(z, t6+"\t\t\t\t\t") + "\n")
	fmt.Fprintf(b, t6+"\t\t\t\t</FileRef>\n"+t6+"\t\t\t</OriginalFileRef>\n"+t6+"\t\t\t<BrowserContentPath Value=\"%s\" />\n"+t6+"\t\t\t<LocalFiltersJson Value=\"\" />\n"+t6+"\t\t</SourceContext>\n"+t6+"\t</SourceContext>\n", esc(browserPath(z)))
	fmt.Fprintf(b, t6+"\t<SampleUsageHint Value=\"0\" />\n"+t6+"\t<DefaultDuration Value=\"%d\" />\n"+t6+"\t<DefaultSampleRate Value=\"%d\" />\n"+t6+"\t<SamplesToAutoWarp Value=\"0\" />\n"+t6+"</SampleRef>\n", z.Frames, z.Rate)
	// Live wants warp markers even when the sample is not warped: the whole
	// file as 4 beats, and a short tail marker, as it writes for a new sample.
	dur := float64(z.Frames) / float64(max(z.Rate, 1))
	fmt.Fprintf(b, t6+"<SlicingThreshold Value=\"100\" />\n"+t6+"<SlicingBeatGrid Value=\"4\" />\n"+t6+"<SlicingRegions Value=\"8\" />\n"+t6+"<SlicingStyle Value=\"0\" />\n")
	fmt.Fprintf(b, t6+"<SampleWarpProperties>\n"+t6+"\t<WarpMarkers>\n"+t6+"\t\t<WarpMarker Id=\"0\" SecTime=\"0\" BeatTime=\"0\" />\n"+t6+"\t\t<WarpMarker Id=\"1\" SecTime=\"%.17g\" BeatTime=\"4\" />\n"+t6+"\t\t<WarpMarker Id=\"2\" SecTime=\"%.17g\" BeatTime=\"4.03125\" />\n"+t6+"\t</WarpMarkers>\n", dur, dur*1.0078125)
	b.WriteString(warpTail)
	b.WriteString(t6 + "<InitialSlicePointsFromOnsets />\n" + t6 + "<SlicePoints />\n" + t6 + "<ManualSlicePoints />\n" + t6 + "<BeatSlicePoints />\n" + t6 + "<RegionSlicePoints />\n")
	b.WriteString(t6 + "<UseDynamicBeatSlices Value=\"true\" />\n" + t6 + "<UseDynamicRegionSlices Value=\"true\" />\n" + t6 + "<AreSlicesFromOnsetsEditable Value=\"false\" />\n")
	b.WriteString("\t\t\t\t\t</MultiSamplePart>\n")
}

// warpTail is the rest of <SampleWarpProperties>, the same in every part.
const warpTail = `							<WarpMode Value="0" />
							<GranularityTones Value="30" />
							<GranularityTexture Value="65" />
							<FluctuationTexture Value="25" />
							<ComplexProFormants Value="100" />
							<ComplexProEnvelope Value="128" />
							<TransientResolution Value="6" />
							<TransientLoopMode Value="2" />
							<TransientEnvelope Value="100" />
							<IsWarped Value="false" />
							<Onsets>
								<UserOnsets />
								<HasUserOnsets Value="false" />
							</Onsets>
							<TimeSignature>
								<TimeSignatures>
									<RemoteableTimeSignature Id="0">
										<Numerator Value="4" />
										<Denominator Value="4" />
										<Time Value="0" />
									</RemoteableTimeSignature>
								</TimeSignatures>
							</TimeSignature>
							<BeatGrid>
								<FixedNumerator Value="1" />
								<FixedDenominator Value="16" />
								<GridIntervalPixel Value="20" />
								<Ntoles Value="2" />
								<SnapToGrid Value="true" />
								<Fixed Value="false" />
							</BeatGrid>
						</SampleWarpProperties>
`

func partsXML(zones []Zone, releaseMode int) string {
	var b strings.Builder
	if len(zones) == 0 {
		return "<SampleParts />"
	}
	b.WriteString("<SampleParts>\n")
	for i, z := range zones {
		samplePart(&b, i, z, i == 0, releaseMode)
	}
	b.WriteString("\t\t\t\t</SampleParts>")
	return b.String()
}

// transpose splits semitones into Live's whole and fine (cents) parts.
func transpose(semis float64) (int, int) {
	whole := int(semis)
	return whole, int((semis - float64(whole)) * 100)
}

func fill(tmpl string, kv ...string) string {
	r := strings.NewReplacer(kv...)
	return r.Replace(tmpl)
}

func defaults(z *Zone) {
	if z.KeyHi == 0 && z.KeyLo == 0 {
		z.KeyLo, z.KeyHi = 0, 127
	}
	if z.VelHi == 0 {
		z.VelLo, z.VelHi = 1, 127
	}
}

// Simpler is one sample in Simpler. Loop, root and window come from z.
func Simpler(z Zone) []byte {
	defaults(&z)
	whole, fine := transpose(z.Transpose)
	return []byte(fill(tmplSimpler, "{{PARTS}}", partsXML([]Zone{z}, 3),
		"{{TRANSPOSE_KEY}}", fmt.Sprint(whole), "{{TRANSPOSE_FINE}}", fmt.Sprint(fine), "{{PLAYBACK}}", "0"))
}

// Sampler is many zones in one Sampler: the auto-sampler's result.
func Sampler(zones []Zone) []byte {
	zs := append([]Zone(nil), zones...)
	for i := range zs {
		defaults(&zs[i])
	}
	return []byte(fill(tmplSampler, "{{PARTS}}", partsXML(zs, 0)))
}

// DrumRack maps each pad's slice to its own pad, with choke group. Each pad is
// a Simpler in One-Shot mode, so a hit plays out when the pad is let go.
func DrumRack(pads []Pad) []byte {
	var b strings.Builder
	for i, p := range pads {
		z := p.Zone
		z.KeyLo, z.KeyHi = 0, 127
		if z.Root == 0 {
			z.Root = 60
		}
		if z.VelHi == 0 {
			z.VelLo, z.VelHi = 1, 127
		}
		whole, fine := transpose(z.Transpose)
		// Live stores the receiving note as 128 minus the note.
		b.WriteString(fill(tmplDrumBranch, "{{ID}}", fmt.Sprint(i), "{{NAME}}", esc(z.Name),
			"{{PARTS}}", partsXML([]Zone{z}, 3), "{{SEED}}", fmt.Sprint(-1659344615+i*7919),
			"{{NOTE}}", fmt.Sprint(128-p.Note), "{{CHOKE}}", fmt.Sprint(p.Choke),
			"{{PLAYBACK}}", "1", "{{TRANSPOSE_KEY}}", fmt.Sprint(whole), "{{TRANSPOSE_FINE}}", fmt.Sprint(fine)))
	}
	return []byte(fill(tmplDrumRack, "{{BRANCHES}}", b.String()))
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
