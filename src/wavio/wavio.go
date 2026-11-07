// Package wavio reads WAV files and writes 24-bit WAV with sampler
// metadata: loop points (smpl), slice regions (cue + adtl), tempo (acid).
package wavio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
)

// Loop is a sustain loop in frames.
type Loop struct{ Start, End int }

// Region is a named slice region in frames.
type Region struct {
	Start, End int
	Name       string
}

// Meta is what rides along with the audio.
type Meta struct {
	Tempo   float64 // BPM, 0 = none
	Root    int     // MIDI root key, -1 = none
	Loops   []Loop
	Regions []Region
}

var le = binary.LittleEndian

// Read loads a WAV (PCM 16/24/32 or float32) into a buffer.
func Read(path string) (*audio.Buffer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Decode(f)
}

// Decode reads a WAV from r.
func Decode(r io.Reader) (*audio.Buffer, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var tag, ch, bits, rate int
	var data []byte
	for p := 12; p+8 <= len(raw); {
		id := string(raw[p : p+4])
		sz := int(le.Uint32(raw[p+4:]))
		body := raw[p+8 : min(p+8+sz, len(raw))]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, errors.New("short fmt chunk")
			}
			tag = int(le.Uint16(body))
			ch = int(le.Uint16(body[2:]))
			rate = int(le.Uint32(body[4:]))
			bits = int(le.Uint16(body[14:]))
			if tag == 0xFFFE && len(body) >= 26 {
				tag = int(le.Uint16(body[24:]))
			}
		case "data":
			data = body
		}
		p += 8 + sz + sz&1
	}
	if ch < 1 || ch > 2 {
		return nil, fmt.Errorf("%d channels: only mono and stereo are supported", ch)
	}
	bps := bits / 8
	if bps == 0 || (tag != 1 && tag != 3) {
		return nil, fmt.Errorf("unsupported WAV format tag %d, %d bits", tag, bits)
	}
	n := len(data) / (bps * ch)
	b := audio.New(rate, ch, n)
	for i := 0; i < n; i++ {
		for c := 0; c < ch; c++ {
			o := (i*ch + c) * bps
			var v float32
			switch {
			case tag == 3 && bits == 32:
				v = math.Float32frombits(le.Uint32(data[o:]))
			case tag == 1 && bits == 16:
				v = float32(int16(le.Uint16(data[o:]))) / 32768
			case tag == 1 && bits == 24:
				x := int32(data[o]) | int32(data[o+1])<<8 | int32(int8(data[o+2]))<<16
				v = float32(x) / 8388608
			case tag == 1 && bits == 32:
				v = float32(int32(le.Uint32(data[o:]))) / 2147483648
			default:
				return nil, fmt.Errorf("unsupported sample format: %d bits", bits)
			}
			b.Ch[c][i] = v
		}
	}
	return b, nil
}

func to24(v float32) int32 {
	x := math.Round(float64(v) * 8388607)
	if x > 8388607 {
		x = 8388607
	}
	if x < -8388608 {
		x = -8388608
	}
	return int32(x)
}

// Writer streams 24-bit frames to disk, so a long recording does not have
// to sit in memory. Close patches the sizes.
type Writer struct {
	f      *os.File
	ch     int
	frames int
	buf    []byte
}

// Create starts a 24-bit WAV.
func Create(path string, rate, channels int) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	copy(h[8:], "WAVEfmt ")
	le.PutUint32(h[16:], 16)
	le.PutUint16(h[20:], 1)
	le.PutUint16(h[22:], uint16(channels))
	le.PutUint32(h[24:], uint32(rate))
	le.PutUint32(h[28:], uint32(rate*channels*3))
	le.PutUint16(h[32:], uint16(channels*3))
	le.PutUint16(h[34:], 24)
	copy(h[36:], "data")
	if _, err := f.Write(h); err != nil {
		f.Close()
		return nil, err
	}
	return &Writer{f: f, ch: channels}, nil
}

// WriteFrames appends planar frames.
func (w *Writer) WriteFrames(ch [][]float32) error {
	n := len(ch[0])
	w.buf = w.buf[:0]
	for i := 0; i < n; i++ {
		for c := 0; c < w.ch; c++ {
			x := to24(ch[c][i])
			w.buf = append(w.buf, byte(x), byte(x>>8), byte(x>>16))
		}
	}
	w.frames += n
	_, err := w.f.Write(w.buf)
	return err
}

// Frames written so far.
func (w *Writer) Frames() int { return w.frames }

// Close writes the optional metadata and fixes the header sizes.
func (w *Writer) Close(m *Meta) error {
	dataBytes := w.frames * w.ch * 3
	if dataBytes&1 == 1 {
		w.f.Write([]byte{0})
	}
	if m != nil {
		rate := 0
		var h [4]byte
		w.f.ReadAt(h[:], 24)
		rate = int(le.Uint32(h[:]))
		w.f.Seek(0, io.SeekEnd)
		w.f.Write(metaChunks(m, rate, w.frames))
	}
	end, _ := w.f.Seek(0, io.SeekEnd)
	var b [4]byte
	le.PutUint32(b[:], uint32(end-8))
	w.f.WriteAt(b[:], 4)
	le.PutUint32(b[:], uint32(dataBytes))
	w.f.WriteAt(b[:], 40)
	return w.f.Close()
}

func chunk(id string, body []byte) []byte {
	out := make([]byte, 8, 8+len(body)+1)
	copy(out, id)
	le.PutUint32(out[4:], uint32(len(body)))
	out = append(out, body...)
	if len(body)&1 == 1 {
		out = append(out, 0)
	}
	return out
}

func u32(v int) []byte {
	b := make([]byte, 4)
	le.PutUint32(b, uint32(v))
	return b
}

func metaChunks(m *Meta, rate, frames int) []byte {
	var out []byte
	if len(m.Loops) > 0 || m.Root >= 0 {
		root := max(m.Root, 0)
		b := append([]byte{}, u32(0)...)          // manufacturer
		b = append(b, u32(0)...)                  // product
		b = append(b, u32(1_000_000_000/rate)...) // sample period ns
		b = append(b, u32(root)...)
		b = append(b, u32(0)...) // pitch fraction
		b = append(b, u32(0)...) // SMPTE format
		b = append(b, u32(0)...) // SMPTE offset
		b = append(b, u32(len(m.Loops))...)
		b = append(b, u32(0)...) // sampler data
		for i, l := range m.Loops {
			b = append(b, u32(i)...)
			b = append(b, u32(0)...) // forward
			b = append(b, u32(l.Start)...)
			b = append(b, u32(l.End-1)...) // smpl end is inclusive
			b = append(b, u32(0)...)
			b = append(b, u32(0)...) // infinite
		}
		out = append(out, chunk("smpl", b)...)
	}
	if len(m.Regions) > 0 {
		cue := u32(len(m.Regions))
		var adtl []byte
		adtl = append(adtl, "adtl"...)
		for i, r := range m.Regions {
			id := i + 1
			cue = append(cue, u32(id)...)
			cue = append(cue, u32(r.Start)...)
			cue = append(cue, "data"...)
			cue = append(cue, u32(0)...)
			cue = append(cue, u32(0)...)
			cue = append(cue, u32(r.Start)...)
			lab := append(u32(id), r.Name...)
			lab = append(lab, 0)
			adtl = append(adtl, chunk("labl", lab)...)
			lt := append(u32(id), u32(r.End-r.Start)...)
			lt = append(lt, "rgn "...)
			lt = append(lt, make([]byte, 8)...)
			adtl = append(adtl, chunk("ltxt", lt)...)
		}
		out = append(out, chunk("cue ", cue)...)
		out = append(out, chunk("LIST", adtl)...)
	}
	if m.Tempo > 0 {
		b := make([]byte, 24)
		flags := 0x00
		if m.Root >= 0 {
			flags |= 0x02
		}
		le.PutUint32(b[0:], uint32(flags))
		le.PutUint16(b[4:], uint16(max(m.Root, 0)))
		beats := float64(frames) / float64(rate) * m.Tempo / 60
		le.PutUint32(b[12:], uint32(math.Round(beats)))
		le.PutUint16(b[16:], 4)
		le.PutUint16(b[18:], 4)
		le.PutUint32(b[20:], math.Float32bits(float32(m.Tempo)))
		out = append(out, chunk("acid", b)...)
	}
	return out
}

// Write saves the whole buffer as 24-bit WAV with metadata.
func Write(path string, b *audio.Buffer, m *Meta) error {
	w, err := Create(path, b.Rate, len(b.Ch))
	if err != nil {
		return err
	}
	if err := w.WriteFrames(b.Ch); err != nil {
		w.f.Close()
		return err
	}
	return w.Close(m)
}

// Info is what a WAV header says, read without loading the audio.
type Info struct {
	Rate, Channels, Bits, Frames int
}

// Probe reads only the chunk headers of a WAV file.
func Probe(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	var h [12]byte
	if _, err := f.ReadAt(h[:], 0); err != nil || string(h[0:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		return Info{}, errors.New("not a WAV file")
	}
	var in Info
	haveFmt, haveData := false, false
	dataBytes := 0
	for p := int64(12); ; {
		var ch [8]byte
		if _, err := f.ReadAt(ch[:], p); err != nil {
			break
		}
		sz := int64(le.Uint32(ch[4:]))
		switch string(ch[0:4]) {
		case "fmt ":
			var b [16]byte
			if _, err := f.ReadAt(b[:], p+8); err != nil {
				return Info{}, errors.New("short fmt chunk")
			}
			in.Channels = int(le.Uint16(b[2:]))
			in.Rate = int(le.Uint32(b[4:]))
			in.Bits = int(le.Uint16(b[14:]))
			haveFmt = true
		case "data":
			dataBytes = int(sz)
			haveData = true
		}
		if haveFmt && haveData {
			break
		}
		p += 8 + sz + sz&1
	}
	if !haveFmt || !haveData || in.Channels < 1 || in.Bits < 8 {
		return Info{}, errors.New("not a usable WAV")
	}
	in.Frames = dataBytes / (in.Channels * in.Bits / 8)
	return in, nil
}
