#include <X11/Xlib.h>
#include <unistd.h>

int main(void) {
	Display *d = XOpenDisplay(NULL);
	if (!d) {
		return 1;
	}
	int s = DefaultScreen(d);
	Window w = XCreateSimpleWindow(d, RootWindow(d, s), 0, 0, 320, 240, 0, BlackPixel(d, s), 0xff0000);
	XSelectInput(d, w, ExposureMask | ButtonPressMask);
	XMapWindow(d, w);
	XFlush(d);
	unsigned long cycle[] = {0xff0000, 0x00aa00, 0x0033ff};
	int i = 0;
	int clicked = 0;
	for (;;) {
		while (XPending(d)) {
			XEvent e;
			XNextEvent(d, &e);
			if (e.type == ButtonPress) {
				clicked = 1;
			}
		}
		unsigned long color = clicked ? 0xffff00 : cycle[i % 3];
		i++;
		XSetWindowBackground(d, w, color);
		XClearWindow(d, w);
		XFlush(d);
		usleep(300000);
	}
}
