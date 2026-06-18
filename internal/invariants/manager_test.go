package invariants

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestManagerDefaultsStoredSeparatelyAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	if err := m.EnsureDefaults(); err != nil {
		t.Fatal(err)
	}
	if err := m.EnsureDefaults(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "invariants", "project.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "stack.go") || strings.Contains(path, "sessions") {
		t.Fatalf("bad invariant storage: path=%s data=%s", path, data)
	}
	items, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, inv := range items {
		seen[inv.ID]++
	}
	if seen["stack.go"] != 1 {
		t.Fatalf("defaults not idempotent: %+v", seen)
	}
}

func TestManagerAddListAndSecretBlocked(t *testing.T) {
	m := NewManager(t.TempDir())
	added, err := m.Add(context.Background(), app.Invariant{ID: "custom.rule", Scope: "project", Kind: "business", Content: "Do not mention beta", Severity: "block", ForbiddenTerms: []string{"beta"}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == added.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("add/list mismatch: %+v", items)
	}
	_, err = m.Add(context.Background(), app.Invariant{ID: "secret.rule", Scope: "project", Kind: "security", Content: "OPENROUTER_API_KEY=sk-secretsecret", Severity: "block"})
	if err == nil || app.AsError(err).Code != "secret_blocked" {
		t.Fatalf("want secret_blocked, got %v", err)
	}
	_, err = m.Add(context.Background(), app.Invariant{ID: "large.rule", Scope: "project", Kind: "business", Content: strings.Repeat("x", MaxContentLength+1), Severity: "block"})
	if err == nil || app.AsError(err).Code != "invariant_too_large" {
		t.Fatalf("want invariant_too_large, got %v", err)
	}
	_, err = m.Add(context.Background(), app.Invariant{ID: "required.rule", Scope: "project", Kind: "business", Content: "Must include beta", Severity: "block", RequiredTerms: []string{"beta"}})
	if err == nil || app.AsError(err).Code != "unsupported_invariant_matcher" {
		t.Fatalf("want unsupported_invariant_matcher, got %v", err)
	}
}

func TestManagerConflictReturnsInvariantIDAndEvidence(t *testing.T) {
	m := NewManager(t.TempDir())
	if _, err := m.Add(context.Background(), app.Invariant{ID: "custom.stack", Scope: "project", Kind: "architecture", Content: "Go only", Severity: "block", ForbiddenTerms: []string{"only custom evidence"}}); err != nil {
		t.Fatal(err)
	}
	violations, err := m.CheckInput(context.Background(), "this has only custom evidence")
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 1 || violations[0].InvariantID != "custom.stack" || violations[0].Evidence != "only custom evidence" {
		t.Fatalf("bad violation: %+v", violations)
	}
	if violations[0].Direction != "input" {
		t.Fatalf("missing direction: %+v", violations)
	}
	if err := Error(violations); err == nil || !strings.Contains(app.AsError(err).Message, "custom.stack") || !strings.Contains(app.AsError(err).Message, "only custom evidence") || len(app.AsError(err).Violations) != 1 {
		t.Fatalf("bad invariant error: %v", err)
	}
}
