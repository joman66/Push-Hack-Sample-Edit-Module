// Package dsp has the offline processing: resampling, WSOLA time stretch
// and pitch shift. Stdlib only, no allocation tricks: it runs on a sample
// the user is waiting for, not in a real-time callback.
package dsp

import "math"

const sincTaps = 16 // taps each side

func sinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	return math.Sin(math.Pi*x) / (math.Pi * x)
}

// Resample changes the length of in by ratio (out = in * ratio). Playing the
// result at the same rate speeds it up when ratio < 1. It uses a Hann
// windowed sinc, with the cutoff lowered when shrinking to avoid aliasing.
func Resample(in []float32, ratio float64) []float32 {
	if ratio <= 0 || len(in) == 0 {
		return nil
	}
	n := int(math.Round(float64(len(in)) * ratio))
	if n == 0 {
		return nil
	}
	out := make([]float32, n)
	step := 1 / ratio // input frames per output frame
	cut := 1.0
	if step > 1 {
		cut = 1 / step
	}
	half := float64(sincTaps) / cut
	for i := range out {
		pos := float64(i) * step
		lo := int(math.Ceil(pos - half))
		hi := int(math.Floor(pos + half))
		var acc, wsum float64
		for j := lo; j <= hi; j++ {
			d := float64(j) - pos
			w := 0.5 + 0.5*math.Cos(math.Pi*d/half)
			k := w * sinc(d*cut) * cut
			wsum += k
			if j >= 0 && j < len(in) {
				acc += k * float64(in[j])
			}
		}
		if wsum != 0 {
			acc /= wsum
		}
		out[i] = float32(acc)
	}
	return out
}

// Mode picks the stretch window set.
type Mode int

const (
	Rhythmic Mode = iota // short windows keep drum hits tight
	Tonal                // long windows keep pitched sounds smooth
)

func (m Mode) String() string { return [...]string{"Rhythm", "Tonal"}[m] }

func params(m Mode, rate int) (win, seek int) {
	scale := float64(rate) / 48000
	if m == Tonal {
		win, seek = 2048, 512
	} else {
		win, seek = 512, 128
	}
	win = int(float64(win)*scale) &^ 1
	seek = int(float64(seek) * scale)
	return
}

// MinRatio and MaxRatio bound a stretch.
const (
	MinRatio = 0.25
	MaxRatio = 4.0
)

// Stretch changes the duration of every channel by ratio (2 = twice as
// long) without changing pitch, with WSOLA. The best overlap position is
// found on the channel mix, so stereo stays phase coherent.
func Stretch(ch [][]float32, rate int, ratio float64, m Mode) [][]float32 {
	ratio = math.Max(MinRatio, math.Min(MaxRatio, ratio))
	out := make([][]float32, len(ch))
	if len(ch) == 0 || len(ch[0]) == 0 {
		return out
	}
	if math.Abs(ratio-1) < 1e-6 {
		for i := range ch {
			out[i] = append([]float32(nil), ch[i]...)
		}
		return out
	}
	n := len(ch[0])
	win, seek := params(m, rate)
	hs := win / 2 // synthesis hop
	ha := float64(hs) / ratio
	outLen := int(math.Round(float64(n) * ratio))
	mix := make([]float32, n)
	for _, c := range ch {
		for i, v := range c {
			mix[i] += v
		}
	}
	hann := make([]float64, win)
	for i := range hann {
		hann[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(win))
	}
	for c := range out {
		out[c] = make([]float32, outLen+win)
	}
	at := func(x []float32, i int) float32 {
		if i < 0 || i >= len(x) {
			return 0
		}
		return x[i]
	}
	prev := 0 // input position of the previous grain
	const dec = 4
	for k := 0; k*hs < outLen; k++ {
		nom := int(math.Round(float64(k) * ha))
		pos := nom
		if k > 0 {
			// natural continuation of the previous grain
			want := prev + hs
			best, bestScore := 0, math.Inf(-1)
			for d := -seek; d <= seek; d += 2 {
				p := nom + d
				if p < 0 {
					continue
				}
				var s, e float64
				for i := 0; i < hs; i += dec {
					a := float64(at(mix, want+i))
					b := float64(at(mix, p+i))
					s += a * b
					e += b * b
				}
				score := s / math.Sqrt(e+1e-9)
				if score > bestScore {
					bestScore, best = score, d
				}
			}
			pos = nom + best
			if pos < 0 {
				pos = 0
			}
		}
		o := k * hs
		for c := range ch {
			for i := 0; i < win; i++ {
				out[c][o+i] += float32(hann[i]) * at(ch[c], pos+i)
			}
		}
		prev = pos
	}
	for c := range out {
		out[c] = out[c][:outLen]
	}
	return out
}

// MaxSemitones bounds a pitch shift.
const MaxSemitones = 24.0

// PitchShift shifts pitch by semitones without changing duration:
// stretch by the pitch factor, then resample back to the old length.
func PitchShift(ch [][]float32, rate int, semitones float64, m Mode) [][]float32 {
	semitones = math.Max(-MaxSemitones, math.Min(MaxSemitones, semitones))
	f := math.Pow(2, semitones/12)
	out := make([][]float32, len(ch))
	if math.Abs(semitones) < 1e-9 {
		for i := range ch {
			out[i] = append([]float32(nil), ch[i]...)
		}
		return out
	}
	// The stretch is limited to 4x, so a 2-octave shift is its edge.
	st := Stretch(ch, rate, f, m)
	for i := range st {
		out[i] = Resample(st[i], 1/f)
	}
	if len(ch) > 0 {
		want := len(ch[0])
		for i := range out {
			switch {
			case len(out[i]) > want:
				out[i] = out[i][:want]
			case len(out[i]) < want:
				out[i] = append(out[i], make([]float32, want-len(out[i]))...)
			}
		}
	}
	return out
}

// ResampleRate converts a buffer from one rate to another.
func ResampleRate(ch [][]float32, from, to int) [][]float32 {
	if from == to || from <= 0 || to <= 0 {
		return ch
	}
	out := make([][]float32, len(ch))
	for i := range ch {
		out[i] = Resample(ch[i], float64(to)/float64(from))
	}
	return out
}
