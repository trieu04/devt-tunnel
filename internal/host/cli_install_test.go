package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinkTargetResolvesRelativeLinks(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "dtt")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink("dtt", link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if got := linkTarget(link); got != bin {
		t.Fatalf("got %q, want %q", got, bin)
	}
	if linkTarget(filepath.Join(dir, "missing")) != "" {
		t.Fatal("missing link should have no target")
	}
}

func TestDirOnPath(t *testing.T) {
	t.Setenv("PATH", "/a"+string(os.PathListSeparator)+"/b/c/")
	if !dirOnPath("/b/c") || dirOnPath("/x") {
		t.Fatal("PATH matching is wrong")
	}
}
