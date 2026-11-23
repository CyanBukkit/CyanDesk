package display

import "testing"

func TestFitMapsNegativeOriginAndDPI(t *testing.T) {
	m := Mapper{
		Mode:    Fit,
		Window:  Rect{X: 0, Y: 0, W: 200, H: 100},
		Remote:  Rect{X: -1920, Y: 0, W: 1920, H: 1080},
		RemoteW: 1920,
		RemoteH: 1080,
		Scale:   2,
	}
	x, y, err := m.LocalToRemote(0, 0)
	if err != nil || x != -1920 || y != 0 {
		t.Fatalf("origin map x=%d y=%d err=%v", x, y, err)
	}
	x, y, err = m.LocalToRemote(100, 50)
	if err != nil {
		t.Fatal(err)
	}
	if x != -1920+960 || y != 540 {
		t.Fatalf("center map x=%d y=%d", x, y)
	}
	orig := m
	orig.Mode = Original
	orig.Remote = Rect{X: 0, Y: 0, W: 100, H: 100}
	orig.RemoteW = 400
	orig.RemoteH = 400
	x, y, err = orig.LocalToRemote(10, 4)
	if err != nil || x != 20 || y != 8 {
		t.Fatalf("original dpi x=%d y=%d err=%v", x, y, err)
	}
}

func TestCropDoesNotChangePixelCount(t *testing.T) {
	parts := Crop(Rect{X: 10, Y: 5, W: 100, H: 40}, 3)
	if len(parts) != 3 {
		t.Fatal(len(parts))
	}
	sum := 0
	for _, p := range parts {
		sum += p.W
		if p.Y != 5 || p.H != 40 {
			t.Fatalf("%+v", p)
		}
	}
	if sum != 100 {
		t.Fatal(sum)
	}
}
