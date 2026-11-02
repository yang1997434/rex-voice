package voice

import (
	"encoding/binary"
	"math"
	"testing"
)

func sine(freq, rate float64, n int, amp float64) []byte {
	b := make([]byte, 2*n)
	for i := range n {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(int16(amp*math.Sin(2*math.Pi*freq*float64(i)/rate))))
	}
	return b
}

// measure returns the dominant frequency (from zero crossings) and peak amplitude, ignoring the edges.
func measure(pcm []byte, rate float64) (freq, peak float64) {
	s := make([]float64, len(pcm)/2)
	for i := range s {
		s[i] = float64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
	}
	s = s[len(s)/10 : len(s)*9/10]
	cross := 0
	for i := 1; i < len(s); i++ {
		if (s[i-1] < 0) != (s[i] < 0) {
			cross++
		}
		peak = max(peak, math.Abs(s[i]))
	}
	return float64(cross) / 2 / (float64(len(s)) / rate), peak
}

func TestDown24to16Sine(t *testing.T) {
	in := sine(1000, 24000, 4800, 10000) // 200 ms
	out, err := Down24to16(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2*3200 {
		t.Fatalf("len = %d, want %d", len(out), 2*3200)
	}
	f, peak := measure(out, 16000)
	if math.Abs(f-1000) > 20 || math.Abs(peak-10000) > 1000 {
		t.Fatalf("1 kHz came out as %.0f Hz, peak %.0f", f, peak)
	}
	// 10 kHz is above the 8 kHz output Nyquist: dropping samples would alias it to 6 kHz at full level.
	out, _ = Down24to16(sine(10000, 24000, 4800, 10000))
	if _, peak := measure(out, 16000); peak > 1000 {
		t.Fatalf("10 kHz not filtered: peak %.0f", peak)
	}
}

func TestDown24to16BadInput(t *testing.T) {
	for _, in := range [][]byte{nil, {}, {1, 2, 3}} {
		if _, err := Down24to16(in); err == nil {
			t.Errorf("Down24to16(%d bytes) accepted", len(in))
		}
	}
}
