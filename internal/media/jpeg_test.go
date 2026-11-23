package media

import "testing"

func TestJPEGRoundTrip(t *testing.T) {
	rgb := []byte{
		255, 0, 0, 0, 255, 0,
		0, 0, 255, 255, 255, 0,
	}
	enc, err := EncodeJPEG(rgb, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc) < 2 || enc[0] != 0xff || enc[1] != 0xd8 {
		t.Fatal("not a jpeg")
	}
	got, err := DecodeJPEG(enc, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 {
		t.Fatalf("len %d", len(got))
	}
}
