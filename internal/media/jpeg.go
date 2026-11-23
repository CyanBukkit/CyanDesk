package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
)

// EncodeJPEG encodes one RGB24 frame with the Go standard library.
// It does not call ffmpeg. Windows builds use this so capture does not
// depend on an X11 or ffmpeg binary. Quality is a size choice, not a
// latency measurement.
func EncodeJPEG(rgb []byte, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 || len(rgb) != w*h*3 {
		return nil, errors.New("media: rgb size does not match the frame")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			img.SetRGBA(x, y, color.RGBA{R: rgb[i], G: rgb[i+1], B: rgb[i+2], A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeJPEG returns RGB24. The decoded size must match the frame header.
func DecodeJPEG(data []byte, w, h int) ([]byte, error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Dx() != w || b.Dy() != h {
		return nil, errors.New("media: jpeg size does not match the frame header")
	}
	out := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*w + x) * 3
			out[i] = byte(r >> 8)
			out[i+1] = byte(g >> 8)
			out[i+2] = byte(bl >> 8)
		}
	}
	return out, nil
}
