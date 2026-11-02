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
