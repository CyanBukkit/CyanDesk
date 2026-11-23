package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("yes"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../etc/passwd", "/etc/passwd", `..\ok.txt`, "..", "ok.txt/../../etc/passwd"} {
		if _, err := Resolve(root, name); err != ErrEscape {
			t.Fatalf("%s err=%v", name, err)
		}
	}
	got, err := Read(root, "ok.txt", 100)
	if err != nil || string(got) != "yes" {
		t.Fatalf("read %q err=%v", got, err)
	}
}
