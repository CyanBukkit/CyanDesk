package display

import "errors"

// Mode is the local viewer layout. It does not create an operating-system display.
type Mode string

const (
	Fit        Mode = "fit"
	Original   Mode = "original"
	Fullscreen Mode = "fullscreen"
)

var ErrOutside = errors.New("display: point is outside the remote picture")

type Rect struct {
	X int
	Y int
	W int
	H int
}

// Mapper converts a point in the local window into a pixel on the remote
// framebuffer. RemoteX/RemoteY may be negative when a monitor sits left of the
// primary display. Scale is the remote DPI relative to the viewer's CSS pixel.
type Mapper struct {
	Mode    Mode
	Window  Rect
	Remote  Rect
	RemoteW int
	RemoteH int
	Scale   float64
}

func (m Mapper) scale() float64 {
	if m.Scale <= 0 {
		return 1
	}
	return m.Scale
}

// LocalToRemote maps a window pixel to a remote framebuffer pixel.
func (m Mapper) LocalToRemote(lx, ly int) (int, int, error) {
	if m.RemoteW <= 0 || m.RemoteH <= 0 || m.Window.W <= 0 || m.Window.H <= 0 {
		return 0, 0, ErrOutside
	}
	rx := lx - m.Window.X
	ry := ly - m.Window.Y
	if rx < 0 || ry < 0 || rx >= m.Window.W || ry >= m.Window.H {
		return 0, 0, ErrOutside
	}
	var fx, fy float64
	switch m.Mode {
	case Original:
		fx = float64(rx) * m.scale()
		fy = float64(ry) * m.scale()
	case Fit, Fullscreen:
		fx = float64(rx) / float64(m.Window.W) * float64(m.RemoteW)
		fy = float64(ry) / float64(m.Window.H) * float64(m.RemoteH)
	default:
		return 0, 0, ErrOutside
	}
	x := m.Remote.X + int(fx)
	y := m.Remote.Y + int(fy)
	if int(fx) < 0 || int(fy) < 0 || int(fx) >= m.RemoteW || int(fy) >= m.RemoteH {
		return 0, 0, ErrOutside
	}
	return x, y, nil
}

// Crop splits one framebuffer into view rectangles. This is L1 only: the remote
// operating system still has the same monitors it had before.
func Crop(frame Rect, regions int) []Rect {
	if regions <= 1 || frame.W <= 0 {
		return []Rect{frame}
	}
	out := make([]Rect, regions)
	base := frame.W / regions
	extra := frame.W % regions
	x := frame.X
	for i := 0; i < regions; i++ {
		w := base
		if i < extra {
			w++
		}
		out[i] = Rect{X: x, Y: frame.Y, W: w, H: frame.H}
		x += w
	}
	return out
}
