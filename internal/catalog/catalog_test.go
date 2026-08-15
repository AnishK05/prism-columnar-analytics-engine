package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTableFiles(t *testing.T) {
	dir := t.TempDir()
	table := filepath.Join(dir, "events")
	if err := os.MkdirAll(table, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"part-0001.parquet", "part-0000.parquet"} {
		p := filepath.Join(table, name)
		if err := os.WriteFile(p, []byte("not-a-real-parquet"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := TableFiles(dir, "events")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	if filepath.Base(files[0]) != "part-0000.parquet" {
		t.Fatalf("files not sorted: %v", files)
	}
}

func TestTableFilesMissing(t *testing.T) {
	_, err := TableFiles(t.TempDir(), "nope")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveDataDir(t *testing.T) {
	if got := ResolveDataDir("C:\\data"); got != "C:\\data" {
		t.Fatalf("flag should win: %q", got)
	}
	t.Setenv("PRISM_DATA_DIR", filepath.Join("custom", "tables"))
	if got := DefaultDataDir(); got != filepath.Join("custom", "tables") {
		t.Fatalf("env data dir: %q", got)
	}
}
