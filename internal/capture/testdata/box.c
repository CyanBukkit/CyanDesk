#include <X11/Xlib.h>

int main(void) {
	Display *d = XOpenDisplay(NULL);
	if (!d) {
		return 1;
	}
	int s = DefaultScreen(d);
	Window w = XCreateSimpleWindow(d, RootWindow(d, s), 0, 0, 80, 80, 0, BlackPixel(d, s), 0x0000ff);
	XSelectInput(d, w, ButtonPressMask | ExposureMask);
	XMapWindow(d, w);
	XFlush(d);
	for (;;) {
		XEvent e;
		XNextEvent(d, &e);
		if (e.type == ButtonPress) {
			XSetWindowBackground(d, w, 0x00ff00);
			XClearWindow(d, w);
			XFlush(d);
		}
	}
}
