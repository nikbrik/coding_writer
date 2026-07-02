package rag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanWorkspaceIgnoresUnsafeAndFindsDocsCode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", "# Title\nhello world")
	writeFile(t, root, "internal/app.go", "package internal\n\nfunc Run() {}\n")
	writeFile(t, root, ".git/config", "secret")
	writeFile(t, root, ".codingwriter/rag/index.jsonl", "ignore")
	writeFile(t, root, "image.png", "\x00\x01")

	docs, ignored, err := ScanWorkspace(ScanOptions{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d: %#v ignored=%v", len(docs), docs, ignored)
	}
	paths := []string{docs[0].Path, docs[1].Path}
	if !contains(paths, "README.md") || !contains(paths, "internal/app.go") {
		t.Fatalf("missing expected paths: %v", paths)
	}
}

func TestScanWorkspaceSkipsSymlinkFiles(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, root, "README.md", "# Title\nhello world")
	writeFile(t, outside, "outside.md", "# Outside\nmust not be indexed")
	if err := os.Symlink(filepath.Join(outside, "outside.md"), filepath.Join(root, "linked.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	docs, ignored, err := ScanWorkspace(ScanOptions{WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "README.md" {
		t.Fatalf("symlink target should not be indexed: docs=%#v ignored=%v", docs, ignored)
	}
	if !contains(ignored, "linked.md") {
		t.Fatalf("symlink should be reported ignored: %v", ignored)
	}
}

func TestChunkersProduceRequiredMetadataAndSections(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	docs := []Document{{
		Source: "workspace",
		Path:   "README.md",
		Title:  "README.md",
		Text:   "# Intro\none two three\n## Details\nfour five six seven eight nine ten",
		Lines:  splitLines("# Intro\none two three\n## Details\nfour five six seven eight nine ten"),
		SHA256: "doc",
	}, {
		Source: "workspace",
		Path:   "internal/demo.go",
		Title:  "demo.go",
		Text:   "package demo\n\ntype Thing struct{}\n\nfunc Run() {}\n",
		Lines:  splitLines("package demo\n\ntype Thing struct{}\n\nfunc Run() {}\n"),
		SHA256: "go",
	}}
	fixed := FixedChunks(docs, 4, 1, now)
	structural := StructuralChunks(docs, 20, 2, now)
	if len(fixed) < 3 {
		t.Fatalf("expected fixed overlap chunks, got %d", len(fixed))
	}
	for _, ch := range append(fixed, structural...) {
		if ch.ChunkID == "" || ch.Source == "" || ch.Title == "" || ch.Section == "" || ch.Path == "" || ch.ContentSHA256 == "" {
			t.Fatalf("chunk missing metadata: %#v", ch)
		}
		if ch.StartLine <= 0 || ch.EndLine < ch.StartLine || ch.TokenCountEstimate <= 0 {
			t.Fatalf("chunk has invalid range/tokens: %#v", ch)
		}
	}
	if !hasSection(structural, "Intro") || !hasSection(structural, "Details") || !hasSection(structural, "Thing") || !hasSection(structural, "Run") {
		t.Fatalf("structural sections missing: %#v", sections(structural))
	}
}

func TestFixedChunksAlwaysMakesProgressWithLargeOverlap(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	doc := Document{
		Source: "workspace",
		Path:   "progress.md",
		Title:  "progress.md",
		Text:   "a b c d e f g h i\nj k\nl m n o p q r s t",
		Lines:  splitLines("a b c d e f g h i\nj k\nl m n o p q r s t"),
	}
	chunks := FixedChunks([]Document{doc}, 10, 9, now)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %#v", chunks)
	}
	lastStart := 0
	for _, ch := range chunks {
		if ch.StartLine <= lastStart {
			t.Fatalf("chunk starts should strictly progress: %#v", chunks)
		}
		lastStart = ch.StartLine
	}
}

func TestBuildIndexStoreAndSearch(t *testing.T) {
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	workspace := filepath.Join(root, "workspace")
	writeFile(t, workspace, "README.md", "# Trusted verification\ntrusted evidence validates commands\n")
	writeFile(t, workspace, "internal/controller.go", "package internal\n\nfunc RunExchange() {}\n")

	result, err := BuildIndex(context.Background(), IndexOptions{
		WorkspaceRoot: workspace,
		StorageRoot:   storageRoot,
		Model:         "fake/bge-m3",
		Provider:      "fake",
		FixedTokens:   8,
		OverlapTokens: 2,
		Now:           time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	}, FakeEmbedder{ModelID: "fake/bge-m3", Dim: 8})
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Fixed.Chunks == 0 || result.Report.Structural.Chunks == 0 {
		t.Fatalf("missing strategy chunks: %#v", result.Report)
	}
	store := Store{Root: storageRoot}
	chunks, err := store.LoadChunks(workspace, StrategyStructural)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected stored chunks")
	}
	semantic, err := store.Search(context.Background(), workspace, StrategyStructural, "trusted verification", FakeEmbedder{ModelID: "fake/bge-m3", Dim: 8}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if semantic.Mode != ModeSemantic || len(semantic.Results) == 0 {
		t.Fatalf("expected semantic results: %#v", semantic)
	}
	fallback, err := store.Search(context.Background(), workspace, StrategyStructural, "trusted verification", nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Mode != ModeLexicalFallback || !strings.Contains(fallback.Warning, "semantic unavailable") || len(fallback.Results) == 0 {
		t.Fatalf("expected lexical fallback: %#v", fallback)
	}
}

func TestStoreIsStaleWhenIndexedFileDeleted(t *testing.T) {
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	workspace := filepath.Join(root, "workspace")
	indexedPath := filepath.Join(workspace, "README.md")
	writeFile(t, workspace, "README.md", "# Trusted verification\ntrusted evidence validates commands\n")

	if _, err := BuildIndex(context.Background(), IndexOptions{
		WorkspaceRoot: workspace,
		StorageRoot:   storageRoot,
		Model:         "fake/bge-m3",
		Provider:      "fake",
		FixedTokens:   8,
		OverlapTokens: 2,
		Now:           time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC),
	}, FakeEmbedder{ModelID: "fake/bge-m3", Dim: 8}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(indexedPath); err != nil {
		t.Fatal(err)
	}
	stale, reason := (Store{Root: storageRoot}).IsStale(workspace)
	if !stale || !strings.Contains(reason, "workspace files changed") {
		t.Fatalf("deleted indexed file should make index stale: stale=%v reason=%q", stale, reason)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasSection(chunks []Chunk, section string) bool {
	for _, ch := range chunks {
		if ch.Section == section {
			return true
		}
	}
	return false
}

func sections(chunks []Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, ch.Section)
	}
	return out
}
