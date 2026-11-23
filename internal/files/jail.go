package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrEscape = errors.New("files: path escapes the granted root")

// Resolve joins name onto root and rejects traversal, absolute paths, and
// symlink escapes. name uses forward slashes.
func Resolve(root, name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") {
		return "", ErrEscape
	}
	clean := filepath.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", ErrEscape
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(rootAbs, clean)
	rel, err := filepath.Rel(rootAbs, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrEscape
	}
	return full, nil
}

func List(root, name string) ([]string, error) {
	dir, err := Resolve(root, name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out, nil
}

func Read(root, name string, limit int64) ([]byte, error) {
	path, err := Resolve(root, name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("files: not a file")
	}
	if limit <= 0 || info.Size() < limit {
		limit = info.Size()
	}
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}
