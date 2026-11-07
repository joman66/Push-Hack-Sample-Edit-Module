package wavio

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/federico-pepe/push-hack-sample-edit/src/audio"
)

func tone(n int) *audio.Buffer {
	b := audio.New(48000, 2, n)
	for i := 0; i < n; i++ {
		b.Ch[0][i] = float32(0.7 * math.Sin(float64(i)/10))
		b.Ch[1][i] = float32(-0.3 * math.Sin(float64(i)/7))
	}
	return b
}

func TestRoundTrip24Bit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.wav")
	b := tone(1000)
	if err := Write(p, b, nil); err != nil {
		t.Fatal(err)
	}
	r, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Rate != 48000 || len(r.Ch) != 2 || r.Frames() != 1000 {
		t.Fatalf("%d %d %d", r.Rate, len(r.Ch), r.Frames())
	}
	for c := range b.Ch {
		for i := range b.Ch[c] {
			if math.Abs(float64(b.Ch[c][i]-r.Ch[c][i])) > 2e-7 {
				t.Fatalf("sample %d/%d: %v vs %v", c, i, b.Ch[c][i], r.Ch[c][i])
			}
		}
	}
}

func TestMetadataChunksAndValidRiff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.wav")
	m := &Meta{Tempo: 120, Root: 60, Loops: []Loop{{100, 400}},
		Regions: []Region{{0, 500, "Slice 1"}, {500, 1000, "Slice 2"}}}
	if err := Write(p, tone(1001), m); err != nil { // odd frame count: padding byte
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if string(raw[:4]) != "RIFF" || int(le.Uint32(raw[4:]))+8 != len(raw) {
		t.Fatalf("RIFF size %d vs file %d", le.Uint32(raw[4:])+8, len(raw))
	}
	for _, id := range []string{"smpl", "cue ", "adtl", "labl", "ltxt", "acid", "data"} {
		if !bytes.Contains(raw, []byte(id)) {
			t.Fatalf("missing %s", id)
		}
	}
	// the file must still decode, skipping unknown chunks
	r, err := Read(p)
	if err != nil || r.Frames() != 1001 {
		t.Fatalf("decode with metadata: %v %v", err, r)
	}
	// loop end in smpl is inclusive: End-1
	i := bytes.Index(raw, []byte("smpl"))
	body := raw[i+8:]
	if le.Uint32(body[28:]) != 1 || le.Uint32(body[44:]) != 100 || le.Uint32(body[48:]) != 399 {
		t.Fatalf("smpl loop wrong: %v", body[28:56])
	}
}

func TestStreamingWriter(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.wav")
	w, err := Create(p, 48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		w.WriteFrames([][]float32{{0.5, -0.5, 0.25}})
	}
	if w.Frames() != 9 {
		t.Fatal("frame count")
	}
	if err := w.Close(nil); err != nil {
		t.Fatal(err)
	}
	r, err := Read(p)
	if err != nil || r.Frames() != 9 || math.Abs(float64(r.Ch[0][1])+0.5) > 1e-6 {
		t.Fatalf("%v %v", err, r)
	}
}

func TestReadFormatsAndErrors(t *testing.T) {
	if _, err := Decode(bytes.NewReader([]byte("nope"))); err == nil {
		t.Fatal("garbage should fail")
	}
	// 16-bit mono
	h := make([]byte, 44)
	copy(h, "RIFF")
	copy(h[8:], "WAVEfmt ")
	le.PutUint32(h[16:], 16)
	le.PutUint16(h[20:], 1)
	le.PutUint16(h[22:], 1)
	le.PutUint32(h[24:], 44100)
	le.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	le.PutUint32(h[40:], 4)
	h = append(h, 0x00, 0x40, 0x00, 0xC0)
	b, err := Decode(bytes.NewReader(h))
	if err != nil || b.Rate != 44100 || b.Ch[0][0] != 0.5 || b.Ch[0][1] != -0.5 {
		t.Fatalf("%v %v", err, b)
	}
	le.PutUint16(h[22:], 6)
	if _, err := Decode(bytes.NewReader(h)); err == nil {
		t.Fatal("6 channels should fail")
	}
}
