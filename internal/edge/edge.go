package edge

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Handler serves the admin files and reverse-proxies the public API.
// It does not forward the internal ticket API.
func Handler(upstream, adminDir string) (http.Handler, error) {
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(r *http.Request) {
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		r.Host = target.Host
		r.Header.Del("X-Forwarded-For")
		r.Header.Del("X-Forwarded-Host")
		r.Header.Del("X-Forwarded-Proto")
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			r.Header.Set("X-Forwarded-For", host)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/internal") {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/downloads/") || strings.HasPrefix(r.URL.Path, "/health/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if adminDir == "" {
			http.NotFound(w, r)
			return
		}
		rel := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
		targetPath := filepath.Join(adminDir, rel)
		root := filepath.Clean(adminDir)
		if targetPath != root && !strings.HasPrefix(targetPath, root+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(targetPath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, targetPath)
			return
		}
		http.ServeFile(w, r, filepath.Join(adminDir, "index.html"))
	}), nil
}
