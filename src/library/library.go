// Package library saves WAVs and Live presets into the user's folders on
// Push's SSD, and loads a WAV back.
package library

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/federico-pepe/push-hack-sample-edit/src/app"
	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
	"github.com/federico-pepe/push-hack-sample-edit/src/autosample"
	"github.com/federico-pepe/push-hack-sample-edit/src/dsp"
	"github.com/federico-pepe/push-hack-sample-edit/src/export"
	"github.com/federico-pepe/push-hack-sample-edit/src/slicer"
	"github.com/federico-pepe/push-hack-sample-edit/src/wavio"
)

// ExportRate is the rate every WAV is written at.
const ExportRate = 48000

// Dirs are where files go.
type Dirs struct {
	Samples string // WAVs
	Simpler string // Simpler presets (.adv)
	Sampler string // Sampler presets (.adv)
	Drum    string // Drum Rack presets (.adg)
	KeepXML bool   // leave a plain .xml next to each preset
}

// DefaultDirs are inside Live's User Library on Push.
func DefaultDirs() Dirs {
	ul := "/data/Music/Ableton/User Library"
	return Dirs{
		Samples: filepath.Join(ul, "Samples", "Imported", "SampleEdit"),
		Simpler: filepath.Join(ul, "Presets", "Instruments", "Simpler", "SampleEdit"),
		Sampler: filepath.Join(ul, "Presets", "Instruments", "Sampler", "SampleEdit"),
		Drum:    filepath.Join(ul, "Presets", "Instruments", "Drum Rack", "SampleEdit"),
	}
}

// Library implements app.Saver.
type Library struct{ D Dirs }

var _ app.Saver = (*Library)(nil)

// at48 returns the audio at 48 kHz and the factor positions were scaled by.
func at48(b *audio.Buffer) (*audio.Buffer, float64) {
	if b.Rate == ExportRate {
		return b, 1
	}
	return &audio.Buffer{Rate: ExportRate, Ch: dsp.ResampleRate(b.Ch, b.Rate, ExportRate)}, float64(ExportRate) / float64(b.Rate)
}

func sc(v int, f float64) int { return int(float64(v)*f + 0.5) }

// SaveWAV writes 24-bit / 48 kHz WAV with tempo, root key, loop and one
// region per slice.
func (l *Library) SaveWAV(name string, b *audio.Buffer, tempo float64, root int, loop *app.Loop, sl []slicer.Slice) (string, error) {
	name = export.SafeName(name)
	out, f := at48(b)
	m := &wavio.Meta{Tempo: tempo, Root: root}
	if loop != nil {
		m.Loops = []wavio.Loop{{Start: sc(loop.Start, f), End: sc(loop.End, f)}}
	}
	for i, s := range sl {
		m.Regions = append(m.Regions, wavio.Region{Start: sc(s.Start, f), End: sc(s.End, f), Name: fmt.Sprintf("Slice %d", i+1)})
	}
	p := filepath.Join(l.D.Samples, name+".wav")
	if err := wavio.Write(p, out, m); err != nil {
		return "", err
	}
	return p, nil
}

func zoneFor(path, name string, b *audio.Buffer) export.Zone {
	var size int64
	if st, err := os.Stat(path); err == nil {
		size = st.Size()
	}
	return export.Zone{Path: path, Name: name, Frames: sc(b.Frames(), float64(ExportRate)/float64(b.Rate)),
		Rate: ExportRate, FileSize: size, KeyLo: 0, KeyHi: 127, VelLo: 1, VelHi: 127}
}

// SaveSimpler writes a Simpler preset for the WAV.
func (l *Library) SaveSimpler(name, wav string, b *audio.Buffer, root int, loop *app.Loop, xfade int, transpose float64) (string, error) {
	name = export.SafeName(name)
	z := zoneFor(wav, name, b)
	z.Root, z.Crossfade, z.Transpose = root, xfade, transpose
	f := float64(ExportRate) / float64(b.Rate)
	if loop != nil {
		z.Loop = &export.Loop{Start: sc(loop.Start, f), End: sc(loop.End, f)}
	}
	p := filepath.Join(l.D.Simpler, name+".adv")
	return p, export.WritePreset(p, export.Simpler(z), l.D.KeepXML)
}

// SaveDrumRack writes a Drum Rack preset with one pad per slice.
func (l *Library) SaveDrumRack(name, wav string, b *audio.Buffer, sl []slicer.Slice) (string, error) {
	name = export.SafeName(name)
	f := float64(ExportRate) / float64(b.Rate)
	var pads []export.Pad
	for i, s := range sl {
		if i >= slicer.MaxSlices {
			break
		}
		z := zoneFor(wav, fmt.Sprintf("%s %d", name, i+1), b)
		z.Start, z.End = sc(s.Start, f), sc(s.End, f)
		z.VelLo, z.VelHi = max(s.VelLo, 1), max(s.VelHi, 1)
		z.Root = 60
		pads = append(pads, export.Pad{Zone: z, Note: 36 + i, Choke: s.Choke})
	}
	p := filepath.Join(l.D.Drum, name+".adg")
	return p, export.WritePreset(p, export.DrumRack(pads), l.D.KeepXML)
}

// SaveSampler writes every auto-sampled zone as a WAV and one Sampler preset.
func (l *Library) SaveSampler(name string, zones []autosample.Zone) (string, error) {
	name = export.SafeName(name)
	var zs []export.Zone
	for _, z := range zones {
		wavName := fmt.Sprintf("%s_n%03d_v%03d", name, z.Root, z.VelHi)
		p, err := l.SaveWAV(wavName, z.Audio, 0, z.Root, nil, nil)
		if err != nil {
			return "", err
		}
		ez := zoneFor(p, wavName, z.Audio)
		ez.Root, ez.KeyLo, ez.KeyHi, ez.VelLo, ez.VelHi = z.Root, z.KeyLo, z.KeyHi, z.VelLo, z.VelHi
		if z.Loop != nil {
			f := float64(ExportRate) / float64(z.Audio.Rate)
			ez.Loop = &export.Loop{Start: sc(z.Loop.Start, f), End: sc(z.Loop.End, f)}
			ez.Crossfade = 64
		}
		zs = append(zs, ez)
	}
	p := filepath.Join(l.D.Sampler, name+".adv")
	return p, export.WritePreset(p, export.Sampler(zs), l.D.KeepXML)
}

// Load reads <Samples>/<name>.wav.
func (l *Library) Load(name string) (*audio.Buffer, error) {
	return wavio.Read(filepath.Join(l.D.Samples, export.SafeName(name)+".wav"))
}
