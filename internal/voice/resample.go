package voice

import (
	"encoding/binary"
	"errors"
	"math"
)

// Down24to16 resamples PCM16 mono little-endian from 24 kHz to 16 kHz (3:2) through a
// windowed-sinc low-pass at about 7 kHz, so content above the new Nyquist does not alias.
// shortcut: stateless, so each call's edges repeat the edge sample instead of seeing the
// neighbouring chunk; carry history across calls if per-frame calls give audible clicks.
func Down24to16(pcm []byte) ([]byte, error) {
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, errors.New("voice: PCM16 input must be a non-empty even number of bytes")
	}
	in := len(pcm) / 2
	at := func(k int) float64 {
		k = min(max(k, 0), in-1)
		return float64(int16(binary.LittleEndian.Uint16(pcm[2*k:])))
	}
	n := (2*in + 2) / 3 // output sample j sits at input position 1.5 j
	out := make([]byte, 2*n)
	for j := range n {
		k0, ph := 3*j/2, j%2 // phase 1 sits half a sample after k0
		var y float64
		for i, w := range firPhases[ph] {
			y += w * at(k0-firHalf+1+i)
		}
		binary.LittleEndian.PutUint16(out[2*j:], uint16(int16(math.Round(min(max(y, -32768), 32767)))))
	}
	return out, nil
}

const (
	firHalf   = 16   // taps on each side of the output position
	firCutoff = 0.29 // cycles per input sample: about 7 kHz at 24 kHz
)

// Down48to24 resamples PCM16 mono little-endian from 48 kHz to 24 kHz (2:1): a symmetric
// windowed-sinc low-pass at about 11 kHz, then every second sample.
// shortcut: stateless like Down24to16, each call's edges repeat the edge sample; per-chunk calls
// on MOSS-Live's ~100 ms chunks are fine, carry history across calls if chunk edges click.
func Down48to24(pcm []byte) ([]byte, error) {
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, errors.New("voice: PCM16 input must be a non-empty even number of bytes")
	}
	in := len(pcm) / 2
	at := func(k int) float64 {
		k = min(max(k, 0), in-1)
		return float64(int16(binary.LittleEndian.Uint16(pcm[2*k:])))
	}
	n := (in + 1) / 2 // output sample j sits at input sample 2 j
	out := make([]byte, 2*n)
	for j := range n {
		y := halfband[firHalf] * at(2*j)
		for d := 1; d <= firHalf; d++ {
			y += halfband[firHalf+d] * (at(2*j-d) + at(2*j+d))
		}
		binary.LittleEndian.PutUint16(out[2*j:], uint16(int16(math.Round(min(max(y, -32768), 32767)))))
	}
	return out, nil
}

// halfband[firHalf+d] weighs input samples at distance d from the output position.
var halfband = func() [2*firHalf + 1]float64 {
	const cutoff = 0.23 // cycles per input sample: about 11 kHz at 48 kHz
	var h [2*firHalf + 1]float64
	var sum float64
	for i := range h {
		d := float64(i - firHalf)
		x := 2 * cutoff * d
		sinc := 1.0
		if x != 0 {
			sinc = math.Sin(math.Pi*x) / (math.Pi * x)
		}
		h[i] = sinc * 0.5 * (1 + math.Cos(math.Pi*d/(firHalf+1))) // Hann
		sum += h[i]
	}
	for i := range h {
		h[i] /= sum // unity gain at DC
	}
	return h
}()

// firPhases[p][i] weighs input sample k0-firHalf+1+i for an output at k0 + p/2.
var firPhases = func() [2][2 * firHalf]float64 {
	var ph [2][2 * firHalf]float64
	for p := range 2 {
		var sum float64
		for i := range 2 * firHalf {
			d := float64(firHalf-1-i) + float64(p)/2 // output position minus input position
			x := 2 * firCutoff * d
			sinc := 1.0
			if x != 0 {
				sinc = math.Sin(math.Pi*x) / (math.Pi * x)
			}
			w := 0.5 * (1 + math.Cos(math.Pi*d/firHalf)) // Hann
			ph[p][i] = sinc * w
			sum += ph[p][i]
		}
		for i := range ph[p] {
			ph[p][i] /= sum // unity gain at DC
		}
	}
	return ph
}()

// Downsampler is Down24to16 with history across calls: feed it the 20 ms frames as they arrive and
// each output sample sees its true neighbours, so there are no clicks at frame edges. Output lags
// the input by firHalf samples (0.7 ms). Zero value is ready to use; not safe for concurrent use.
type Downsampler struct {
	buf  []float64 // input samples not yet dropped, buf[0] is absolute input index base
	base int
	next int // next output index j, at input position 1.5 j
}

// Write appends 24 kHz PCM16 and returns whatever 16 kHz PCM16 can be produced so far.
func (d *Downsampler) Write(pcm []byte) ([]byte, error) {
	if len(pcm)%2 != 0 {
		return nil, errors.New("voice: PCM16 input must be an even number of bytes")
	}
	for k := 0; k+1 < len(pcm); k += 2 {
		d.buf = append(d.buf, float64(int16(binary.LittleEndian.Uint16(pcm[k:]))))
	}
	at := func(k int) float64 { // before the first sample the signal is silence
		if k < d.base {
			return 0
		}
		return d.buf[k-d.base]
	}
	var out []byte
	for {
		k0, ph := 3*d.next/2, d.next%2
		if k0+firHalf >= d.base+len(d.buf) {
			break
		}
		var y float64
		for i, w := range firPhases[ph] {
			y += w * at(k0-firHalf+1+i)
		}
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(math.Round(min(max(y, -32768), 32767)))))
		d.next++
	}
	if keep := 3*d.next/2 - firHalf + 1; keep > d.base { // samples no future output needs
		drop := min(keep-d.base, len(d.buf))
		d.buf = append(d.buf[:0], d.buf[drop:]...)
		d.base += drop
	}
	return out, nil
}
