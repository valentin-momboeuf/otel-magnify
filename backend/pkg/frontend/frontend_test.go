package frontend_test

import (
	"io/fs"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/frontend"
)

func TestFS_ReturnsFilesystemContainingIndexHTML(t *testing.T) {
	fsys := frontend.FS()
	if fsys == nil {
		t.Fatal("FS() returned nil")
	}

	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("open index.html: %v", err)
	}
	defer f.Close()

	info, err := fs.Stat(fsys, "index.html")
	if err != nil {
		t.Fatalf("stat index.html: %v", err)
	}
	if info.Size() == 0 {
		t.Error("index.html is empty")
	}
}
