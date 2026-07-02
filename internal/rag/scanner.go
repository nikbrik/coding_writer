package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/nikbrik/coding_writer/internal/validation"
)

type ScanOptions struct {
	WorkspaceRoot string
	MaxFileBytes  int64
}

func ScanWorkspace(opts ScanOptions) ([]Document, []string, error) {
	root, err := filepath.Abs(opts.WorkspaceRoot)
	if err != nil {
		return nil, nil, err
	}
	maxBytes := opts.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}
	var docs []Document
	var ignored []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			ignored = append(ignored, rel)
			return nil
		}
		if d.IsDir() {
			if ignoredDir(rel) {
				ignored = append(ignored, rel+"/")
				return filepath.SkipDir
			}
			return nil
		}
		if ignoredFile(rel) {
			ignored = append(ignored, rel)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxBytes {
			ignored = append(ignored, rel)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) || isLikelyBinary(data) {
			ignored = append(ignored, rel)
			return nil
		}
		text := string(data)
		if validation.HasSecret(text) {
			ignored = append(ignored, rel)
			return nil
		}
		sum := sha256.Sum256(data)
		docs = append(docs, Document{
			Source: "workspace",
			Path:   rel,
			Title:  filepath.Base(rel),
			Text:   text,
			Lines:  splitLines(text),
			SHA256: hex.EncodeToString(sum[:]),
			Size:   info.Size(),
			ModAt:  info.ModTime().UTC(),
		})
		return nil
	})
	return docs, ignored, err
}

func ignoredDir(rel string) bool {
	first := strings.Split(filepath.ToSlash(rel), "/")[0]
	switch first {
	case ".git", ".codingwriter", ".assistant", ".cache", ".pytest_cache", ".kilo", "artifacts", "Artifacts", "manual_scratch", "tmp", "temp", "build", "dist", "coverage", "__pycache__":
		return true
	default:
		return false
	}
}

func ignoredFile(rel string) bool {
	base := filepath.Base(rel)
	if strings.HasPrefix(base, ".") && base != ".gitignore" {
		return true
	}
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".md", ".go", ".txt", ".json", ".yaml", ".yml", ".toml":
		return false
	default:
		return true
	}
}

func isLikelyBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	limit := len(data)
	if limit > 8192 {
		limit = 8192
	}
	for _, b := range data[:limit] {
		if b == 0 {
			return true
		}
	}
	return false
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(text, "\n")
}
