package media

import "testing"

func TestH264RoundTripKeepsTheRedFrame(t *testing.T) {
	const w, h = 64, 64
	rgb := make([]byte, w*h*3)
	for i := 0; i < w*h; i++ {
		rgb[i*3] = 220
		rgb[i*3+1] = 10
		rgb[i*3+2] = 10
	}
	annex, err := EncodeH264(rgb, w, h)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeH264(annex, w, h)
	if err != nil {
		t.Fatal(err)
	}
	var r, g, b int
	n := w * h
	for i := 0; i < n; i++ {
		r += int(got[i*3])
		g += int(got[i*3+1])
		b += int(got[i*3+2])
	}
	if r/n < 150 || g/n > 80 || b/n > 80 {
		t.Fatalf("decoded color r=%d g=%d b=%d", r/n, g/n, b/n)
	}
}

func TestOpusEncoderProducesAPacket(t *testing.T) {
	b, err := EncodeOpus()
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:4]) != "OggS" {
		t.Fatalf("not ogg: %q", b[:4])
	}
}
