package storage

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSafeJoinAndValidateIDRejectUnsafeInput(t *testing.T) {
	for _, id := range []string{"../x", "a/b", "a\\b", "%2e%2e", ""} {
		if err := ValidateID(id); err == nil {
			t.Fatalf("unsafe id accepted: %q", id)
		}
	}
	root := t.TempDir()
	if _, err := SafeJoin(root, "..", "x"); err == nil {
		t.Fatal("unsafe path accepted")
	}
	joined, err := SafeJoin(root, "sessions", "session_1", "short_term.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(joined, root) {
		t.Fatalf("path escaped root: %s", joined)
	}
}

func TestAtomicWriteJSONRejectsSymlinkTargetAndBrokenJSONTyped(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte(`{"ok":true}`), FileMode); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := AtomicWriteJSON(link, map[string]bool{"ok": true}); err == nil {
		t.Fatal("symlink target write accepted")
	}
	broken := filepath.Join(root, "broken.json")
	if err := os.WriteFile(broken, []byte(`{bad json`), FileMode); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := ReadJSON(broken, &out); err == nil || !strings.Contains(err.Error(), "broken_json") {
		t.Fatalf("want broken_json error, got %v", err)
	}
}

func TestEnsureDirRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, DirMode); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "linkdir")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := EnsureDir(linkDir); err == nil {
		t.Fatal("symlink directory accepted")
	}
}

func TestConcurrentJSONLAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.jsonl")
	const count = 20
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = AppendJSONL(path, map[string]int{"n": i})
		}(i)
	}
	wg.Wait()
	records, err := ReadJSONL[map[string]int](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != count {
		t.Fatalf("want %d records, got %d", count, len(records))
	}
}

func TestReadJSONLHandlesLargeRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.jsonl")
	large := strings.Repeat("x", 128*1024)
	if err := AppendJSONL(path, map[string]string{"content": large}); err != nil {
		t.Fatal(err)
	}
	records, err := ReadJSONL[map[string]string](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["content"] != large {
		t.Fatalf("large record did not round-trip: %d", len(records))
	}
}
