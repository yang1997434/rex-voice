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

func TestDown48to24Sine(t *testing.T) {
	out, err := Down48to24(sine(1000, 48000, 9600, 10000)) // 200 ms
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2*4800 {
		t.Fatalf("len = %d, want %d", len(out), 2*4800)
	}
	f, peak := measure(out, 24000)
	if math.Abs(f-1000) > 20 || math.Abs(peak-10000) > 1000 {
		t.Fatalf("1 kHz came out as %.0f Hz, peak %.0f", f, peak)
	}
	// 18 kHz is above the 12 kHz output Nyquist: dropping samples would alias it to 6 kHz at full level.
	out, _ = Down48to24(sine(18000, 48000, 9600, 10000))
	if _, peak := measure(out, 24000); peak > 1000 {
		t.Fatalf("18 kHz not filtered: peak %.0f", peak)
	}
	for _, in := range [][]byte{nil, {1, 2, 3}} {
		if _, err := Down48to24(in); err == nil {
			t.Errorf("Down48to24(%d bytes) accepted", len(in))
		}
	}
}

func TestDown24to16BadInput(t *testing.T) {
	for _, in := range [][]byte{nil, {}, {1, 2, 3}} {
		if _, err := Down24to16(in); err == nil {
			t.Errorf("Down24to16(%d bytes) accepted", len(in))
		}
	}
}

func TestDownsamplerMatchesWholeBuffer(t *testing.T) {
	pcm := sine(1000, 24000, 24000, 10000) // 1 s
	whole, err := Down24to16(pcm)
	if err != nil {
		t.Fatal(err)
	}
	var d Downsampler
	var got []byte
	for i := 0; i < len(pcm); i += 960 { // 20 ms frames
		part, err := d.Write(pcm[i:min(i+960, len(pcm))])
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, part...)
	}
	if len(got) < len(whole)-2*firHalf {
		t.Fatalf("streamed %d bytes, whole %d", len(got), len(whole))
	}
	// Away from the very start (where Down24to16 repeats the edge and the streamer assumes silence),
	// the streamed output must equal the whole-buffer output sample for sample.
	for j := 2 * firHalf; j < len(got)/2; j++ {
		a := int16(binary.LittleEndian.Uint16(got[2*j:]))
		b := int16(binary.LittleEndian.Uint16(whole[2*j:]))
		if a != b {
			t.Fatalf("sample %d: streamed %d, whole %d", j, a, b)
		}
	}
	if _, err := d.Write([]byte{1}); err == nil {
		t.Fatal("odd length accepted")
	}
}
