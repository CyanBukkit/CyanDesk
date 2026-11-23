package media

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
)

// Preset is a quality hint. These are the design defaults, not measured guarantees.
type Preset struct {
	Name    string
	Height  int
	FPS     int
	Bitrate string
}

func Presets() []Preset {
	return []Preset{
		{Name: "省流", Height: 720, FPS: 30, Bitrate: "3M"},
		{Name: "均衡", Height: 1080, FPS: 30, Bitrate: "8M"},
		{Name: "高清", Height: 1080, FPS: 60, Bitrate: "20M"},
		{Name: "超清", Height: 1440, FPS: 60, Bitrate: "60M"},
	}
}

// EncodeH264 encodes one RGB24 frame with libx264. w and h must be even.
func EncodeH264(rgb []byte, w, h int) ([]byte, error) {
	if len(rgb) != w*h*3 {
		return nil, errors.New("media: rgb size does not match the frame")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, err
	}
	cmd := exec.Command("ffmpeg", "-loglevel", "error",
		"-f", "rawvideo", "-pix_fmt", "rgb24", "-s", fmt.Sprintf("%dx%d", w, h), "-r", "10", "-i", "pipe:0",
		"-frames:v", "1", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-f", "h264", "pipe:1")
	cmd.Stdin = bytes.NewReader(rgb)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("media: encode: %w: %s", err, errb.String())
	}
	if out.Len() == 0 {
		return nil, errors.New("media: encoder produced no bytes")
	}
	return out.Bytes(), nil
}

// DecodeH264 decodes an annex-b elementary stream back to RGB24.
func DecodeH264(annex []byte, w, h int) ([]byte, error) {
	cmd := exec.Command("ffmpeg", "-loglevel", "error",
		"-f", "h264", "-i", "pipe:0", "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	cmd.Stdin = bytes.NewReader(annex)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("media: decode: %w: %s", err, errb.String())
	}
	want := w * h * 3
	if out.Len() < want {
		return nil, fmt.Errorf("media: decoded %d bytes, want %d", out.Len(), want)
	}
	return out.Bytes()[:want], nil
}

// EncodeOpus encodes a short 48 kHz stereo silence-free sine as Ogg Opus.
func EncodeOpus() ([]byte, error) {
	cmd := exec.Command("ffmpeg", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=0.2",
		"-c:a", "libopus", "-f", "ogg", "pipe:1")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("media: opus: %w: %s", err, errb.String())
	}
	if out.Len() < 32 {
		return nil, errors.New("media: opus output too small")
	}
	return out.Bytes(), nil
}
