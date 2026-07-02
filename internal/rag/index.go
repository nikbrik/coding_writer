package rag

import (
	"context"
	"fmt"
	"time"
)

type IndexResult struct {
	Manifest Manifest `json:"manifest"`
	Report   Report   `json:"report"`
	Ignored  []string `json:"ignored"`
}

func BuildIndex(ctx context.Context, opts IndexOptions, embedder Embedder) (IndexResult, error) {
	opts = normalizeOptions(opts)
	if opts.WorkspaceRoot == "" {
		return IndexResult{}, fmt.Errorf("workspace root is required")
	}
	if opts.StorageRoot == "" {
		return IndexResult{}, fmt.Errorf("storage root is required")
	}
	if embedder == nil {
		return IndexResult{}, fmt.Errorf("embedder is required")
	}
	docs, ignored, err := ScanWorkspace(ScanOptions{WorkspaceRoot: opts.WorkspaceRoot, MaxFileBytes: opts.MaxFileBytes})
	if err != nil {
		return IndexResult{}, err
	}
	if len(docs) == 0 {
		return IndexResult{}, fmt.Errorf("no indexable workspace documents found")
	}
	fixed := FixedChunks(docs, opts.FixedTokens, opts.OverlapTokens, opts.Now)
	structural := StructuralChunks(docs, opts.FixedTokens, opts.OverlapTokens, opts.Now)
	fixedEmb, err := embedChunks(ctx, fixed, embedder, opts.Now)
	if err != nil {
		return IndexResult{}, err
	}
	structuralEmb, err := embedChunks(ctx, structural, embedder, opts.Now)
	if err != nil {
		return IndexResult{}, err
	}
	report := BuildReport(docs, fixed, structural, opts.OverlapTokens)
	store := Store{Root: opts.StorageRoot}
	manifest, err := store.WriteIndex(ctx, opts, docs, fixed, structural, fixedEmb, structuralEmb, report)
	if err != nil {
		return IndexResult{}, err
	}
	return IndexResult{Manifest: manifest, Report: report, Ignored: ignored}, nil
}

func embedChunks(ctx context.Context, chunks []Chunk, embedder Embedder, now time.Time) ([]Embedding, error) {
	texts := make([]string, 0, len(chunks))
	for _, ch := range chunks {
		texts = append(texts, ch.Text)
	}
	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(chunks) {
		return nil, fmt.Errorf("embedding count mismatch: %d vectors for %d chunks", len(vectors), len(chunks))
	}
	out := make([]Embedding, 0, len(chunks))
	dim := 0
	for i, vec := range vectors {
		if dim == 0 {
			dim = len(vec)
		}
		if len(vec) != dim {
			return nil, fmt.Errorf("embedding dimension mismatch")
		}
		out = append(out, Embedding{
			ChunkID:       chunks[i].ChunkID,
			Model:         embedder.Model(),
			Provider:      embedder.Provider(),
			Dimension:     len(vec),
			Vector:        vec,
			ContentSHA256: chunks[i].ContentSHA256,
			CreatedAt:     now,
		})
	}
	return out, nil
}

func BuildReport(docs []Document, fixed, structural []Chunk, overlap int) Report {
	var textBytes int64
	for _, doc := range docs {
		textBytes += int64(len(doc.Text))
	}
	return Report{
		Corpus: CorpusReport{
			Documents:              len(docs),
			TextBytes:              textBytes,
			PageEquivalentEstimate: int(textBytes / 1800),
		},
		Fixed:      summarizeStrategy(fixed, overlap, 0),
		Structural: summarizeStrategy(structural, 0, countSections(structural)),
		Summary: []string{
			"structural keeps headings and functions together",
			"fixed produces more uniform chunk sizes",
		},
	}
}

func summarizeStrategy(chunks []Chunk, overlap, sections int) StrategyReport {
	report := StrategyReport{Chunks: len(chunks), OverlapTokens: overlap, Sections: sections}
	if len(chunks) == 0 {
		return report
	}
	files := map[string]bool{}
	min := chunks[0].TokenCountEstimate
	max := chunks[0].TokenCountEstimate
	total := 0
	for _, ch := range chunks {
		files[ch.Path] = true
		tokens := ch.TokenCountEstimate
		total += tokens
		if tokens < min {
			min = tokens
		}
		if tokens > max {
			max = tokens
		}
	}
	report.AvgTokens = float64(total) / float64(len(chunks))
	report.MinTokens = min
	report.MaxTokens = max
	report.FilesCovered = len(files)
	return report
}

func countSections(chunks []Chunk) int {
	seen := map[string]bool{}
	for _, ch := range chunks {
		seen[ch.Path+"#"+ch.Section] = true
	}
	return len(seen)
}
