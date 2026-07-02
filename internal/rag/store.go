package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/storage"
)

type Store struct {
	Root string
}

func WorkspaceID(root string) string {
	abs, _ := filepath.Abs(root)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

func (s Store) IndexDir(workspaceRoot string) (string, error) {
	return storage.SafeJoin(s.Root, "rag", WorkspaceID(workspaceRoot))
}

func (s Store) WriteIndex(ctx context.Context, opts IndexOptions, docs []Document, fixed []Chunk, structural []Chunk, fixedEmb []Embedding, structuralEmb []Embedding, report Report) (Manifest, error) {
	opts = normalizeOptions(opts)
	dir, err := s.IndexDir(opts.WorkspaceRoot)
	if err != nil {
		return Manifest{}, err
	}
	if err := storage.EnsureDir(dir); err != nil {
		return Manifest{}, err
	}
	if err := storage.RewriteJSONL(filepath.Join(dir, "chunks.fixed.jsonl"), fixed); err != nil {
		return Manifest{}, err
	}
	if err := storage.RewriteJSONL(filepath.Join(dir, "chunks.structural.jsonl"), structural); err != nil {
		return Manifest{}, err
	}
	if err := storage.RewriteJSONL(filepath.Join(dir, "embeddings.fixed.jsonl"), fixedEmb); err != nil {
		return Manifest{}, err
	}
	if err := storage.RewriteJSONL(filepath.Join(dir, "embeddings.structural.jsonl"), structuralEmb); err != nil {
		return Manifest{}, err
	}
	dim := 0
	if len(structuralEmb) > 0 {
		dim = structuralEmb[0].Dimension
	} else if len(fixedEmb) > 0 {
		dim = fixedEmb[0].Dimension
	}
	files := make([]FileRecord, 0, len(docs))
	for _, doc := range docs {
		files = append(files, FileRecord{Path: doc.Path, SHA256: doc.SHA256, SizeBytes: doc.Size, ModAt: doc.ModAt})
	}
	manifest := Manifest{
		Version:            1,
		WorkspaceRoot:      opts.WorkspaceRoot,
		WorkspaceID:        WorkspaceID(opts.WorkspaceRoot),
		EmbeddingProvider:  opts.Provider,
		EmbeddingModel:     opts.Model,
		EmbeddingDimension: dim,
		DefaultStrategy:    DefaultStrategy,
		IndexedAt:          opts.Now,
		Documents:          len(docs),
		Strategies: map[string]StrategyRecord{
			StrategyFixed:      {Chunks: len(fixed), Path: "chunks.fixed.jsonl", EmbeddingsPath: "embeddings.fixed.jsonl"},
			StrategyStructural: {Chunks: len(structural), Path: "chunks.structural.jsonl", EmbeddingsPath: "embeddings.structural.jsonl"},
		},
		Files: files,
	}
	if err := storage.AtomicWriteJSON(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		return Manifest{}, err
	}
	if err := storage.AtomicWriteJSON(filepath.Join(dir, "report.json"), report); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (s Store) LoadManifest(workspaceRoot string) (Manifest, error) {
	dir, err := s.IndexDir(workspaceRoot)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	err = storage.ReadJSON(filepath.Join(dir, "manifest.json"), &manifest)
	return manifest, err
}

func (s Store) LoadReport(workspaceRoot string) (Report, error) {
	dir, err := s.IndexDir(workspaceRoot)
	if err != nil {
		return Report{}, err
	}
	var report Report
	err = storage.ReadJSON(filepath.Join(dir, "report.json"), &report)
	return report, err
}

func (s Store) LoadChunks(workspaceRoot, strategy string) ([]Chunk, error) {
	dir, err := s.IndexDir(workspaceRoot)
	if err != nil {
		return nil, err
	}
	if strategy == "" {
		strategy = DefaultStrategy
	}
	return storage.ReadJSONL[Chunk](filepath.Join(dir, "chunks."+strategy+".jsonl"))
}

func (s Store) LoadEmbeddings(workspaceRoot, strategy string) ([]Embedding, error) {
	dir, err := s.IndexDir(workspaceRoot)
	if err != nil {
		return nil, err
	}
	if strategy == "" {
		strategy = DefaultStrategy
	}
	return storage.ReadJSONL[Embedding](filepath.Join(dir, "embeddings."+strategy+".jsonl"))
}

func (s Store) DeleteIndex(workspaceRoot string) error {
	dir, err := s.IndexDir(workspaceRoot)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(dir)
}

func (s Store) IsStale(workspaceRoot string) (bool, string) {
	manifest, err := s.LoadManifest(workspaceRoot)
	if err != nil {
		return false, ""
	}
	byPath := map[string]FileRecord{}
	for _, f := range manifest.Files {
		byPath[f.Path] = f
	}
	docs, _, err := ScanWorkspace(ScanOptions{WorkspaceRoot: workspaceRoot})
	if err != nil {
		return false, ""
	}
	changed := 0
	currentPaths := map[string]bool{}
	for _, doc := range docs {
		currentPaths[doc.Path] = true
		if old, ok := byPath[doc.Path]; !ok || old.SHA256 != doc.SHA256 {
			changed++
		}
	}
	for path := range byPath {
		if !currentPaths[path] {
			changed++
		}
	}
	if changed > 0 {
		return true, strings.TrimSpace(strings.Join([]string{itoa(changed), "workspace files changed after indexing"}, " "))
	}
	return false, ""
}

func (s Store) Search(ctx context.Context, workspaceRoot, strategy, query string, embedder Embedder, topK int) (RetrievalContext, error) {
	if topK <= 0 {
		topK = DefaultSearchTopK
	}
	start := nowMillis()
	chunks, err := s.LoadChunks(workspaceRoot, strategy)
	if err != nil || len(chunks) == 0 {
		return RetrievalContext{Mode: ModeSkipped, Strategy: strategy, Warning: "index missing"}, err
	}
	if embedder == nil {
		return lexical(query, chunks, strategy, "semantic unavailable: embedding model missing", topK, start), nil
	}
	embeddings, err := s.LoadEmbeddings(workspaceRoot, strategy)
	if err != nil || len(embeddings) == 0 {
		return lexical(query, chunks, strategy, "semantic unavailable: embeddings missing", topK, start), nil
	}
	queryVecs, err := embedder.Embed(ctx, []string{query})
	if err != nil || len(queryVecs) == 0 {
		return lexical(query, chunks, strategy, "semantic unavailable: "+errString(err), topK, start), nil
	}
	byID := map[string]Chunk{}
	for _, ch := range chunks {
		byID[ch.ChunkID] = ch
	}
	results := make([]SearchResult, 0, len(embeddings))
	for _, emb := range embeddings {
		ch, ok := byID[emb.ChunkID]
		if !ok {
			continue
		}
		results = append(results, SearchResult{Chunk: ch, Score: Cosine(queryVecs[0], emb.Vector)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > topK {
		results = results[:topK]
	}
	return RetrievalContext{Mode: ModeSemantic, Strategy: strategy, Model: embedder.Model(), DurationMS: nowMillis() - start, Results: results}, nil
}

func lexical(query string, chunks []Chunk, strategy, warning string, topK int, start int64) RetrievalContext {
	terms := strings.Fields(strings.ToLower(query))
	var results []SearchResult
	for _, ch := range chunks {
		text := strings.ToLower(ch.Text)
		score := 0.0
		for _, term := range terms {
			if strings.Contains(text, term) {
				score++
			}
		}
		if score > 0 {
			results = append(results, SearchResult{Chunk: ch, Score: score / float64(len(terms)+1)})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > topK {
		results = results[:topK]
	}
	return RetrievalContext{Mode: ModeLexicalFallback, Strategy: strategy, Warning: warning, DurationMS: nowMillis() - start, Results: results}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func errString(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

func nowMillis() int64 { return time.Now().UnixNano() / int64(time.Millisecond) }
