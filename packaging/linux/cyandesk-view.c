#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

static int read_full(void *dst, size_t n) {
	unsigned char *p = dst;
	size_t got = 0;
	while (got < n) {
		ssize_t r = read(0, p + got, n - got);
		if (r <= 0) {
			return -1;
		}
		got += (size_t)r;
	}
	return 0;
}

int main(int argc, char **argv) {
	if (argc != 3) {
		fprintf(stderr, "usage: cyandesk-view WIDTH HEIGHT\n");
		return 2;
	}
	int w = atoi(argv[1]);
	int h = atoi(argv[2]);
	if (w <= 0 || h <= 0 || w > 4096 || h > 4096) {
		return 2;
	}
	Display *d = XOpenDisplay(NULL);
	if (!d) {
		fprintf(stderr, "cyandesk-view: no display\n");
		return 1;
	}
	int s = DefaultScreen(d);
	Window win = XCreateSimpleWindow(d, RootWindow(d, s), 0, 0, w, h, 0, BlackPixel(d, s), BlackPixel(d, s));
	XStoreName(d, win, "CyanDesk remote");
	XSelectInput(d, win, ButtonPressMask | KeyPressMask | KeyReleaseMask);
	XMapWindow(d, win);
	XFlush(d);
	XImage *img = XCreateImage(d, DefaultVisual(d, s), (unsigned)DefaultDepth(d, s), ZPixmap, 0, NULL, w, h, 32, 0);
	if (!img) {
		return 1;
	}
	img->data = calloc((size_t)img->bytes_per_line, (size_t)h);
	if (!img->data) {
		return 1;
	}
	unsigned char *rgb = malloc((size_t)w * (size_t)h * 3);
	if (!rgb) {
		return 1;
	}
	GC gc = XCreateGC(d, win, 0, NULL);
	for (;;) {
		unsigned char hdr[4];
		if (read_full(hdr, 4) < 0) {
			break;
		}
		uint32_t n = ((uint32_t)hdr[0] << 24) | ((uint32_t)hdr[1] << 16) | ((uint32_t)hdr[2] << 8) | hdr[3];
		if (n != (uint32_t)w * (uint32_t)h * 3) {
			break;
		}
		if (read_full(rgb, n) < 0) {
			break;
		}
		int bpp = img->bits_per_pixel / 8;
		if (bpp < 3) {
			bpp = 4;
		}
		for (int y = 0; y < h; y++) {
			unsigned char *row = (unsigned char *)img->data + (size_t)y * (size_t)img->bytes_per_line;
			for (int x = 0; x < w; x++) {
				unsigned char *p = rgb + ((y * w + x) * 3);
				unsigned char *dst = row + (size_t)x * (size_t)bpp;
				dst[0] = p[2];
				dst[1] = p[1];
				dst[2] = p[0];
			}
		}
		XPutImage(d, win, gc, img, 0, 0, 0, 0, (unsigned)w, (unsigned)h);
		while (XPending(d)) {
			XEvent ev;
			XNextEvent(d, &ev);
			if (ev.type == ButtonPress) {
				printf("click %d %d\n", ev.xbutton.x, ev.xbutton.y);
				fflush(stdout);
			} else if (ev.type == KeyPress || ev.type == KeyRelease) {
				printf("%s %u\n", ev.type == KeyPress ? "keydown" : "keyup", (unsigned)ev.xkey.keycode);
				fflush(stdout);
			}
		}
		XFlush(d);
	}
	return 0;
}
