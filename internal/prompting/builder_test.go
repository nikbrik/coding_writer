package prompting

import (
	"strings"
	"testing"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/profiles"
)

func TestPromptBuilderOrderAndUntrustedTags(t *testing.T) {
	profile := profiles.DefaultProfiles(time.Now().UTC())[0]
	task := app.TaskState{ID: "task_test", Title: "t", Stage: app.StageExecution, Status: app.TaskStatusPaused, CurrentStep: "step", ExpectedAction: app.ExpectedLLMResponse}
	messages, err := NewBuilder().Build(BuildInput{Profile: profile, Task: &task, Memory: app.MemoryBundle{Work: []app.MemoryRecord{{Layer: app.LayerWork, Content: "work"}}, Long: []app.MemoryRecord{{Layer: app.LayerLong, Content: "long"}}, Short: []app.MemoryRecord{{Layer: app.LayerShort, Content: "short"}}}, Invariants: []app.Invariant{{ID: "stack.go", Scope: "project", Kind: "architecture", Content: "Use Go", Severity: "block"}}, Query: "query"})
	if err != nil {
		t.Fatal(err)
	}
	rendered := RenderMessages(messages)
	order := []string{
		"minimal CLI code assistant",
		"Security and memory policy",
		"Process rules",
		"Current stage: execution",
		"Tool and side-effect policy",
		`id="profile.active"`,
		"Invariant policy",
		`id="invariants.active"`,
		`id="task.current"`,
		`id="memory.working"`,
		`id="memory.long"`,
		`id="memory.short"`,
		"query",
	}
	last := -1
	for _, needle := range order {
		idx := strings.Index(rendered, needle)
		if idx <= last {
			t.Fatalf("bad order for %q in prompt:\n%s", needle, rendered)
		}
		last = idx
	}
	if !strings.Contains(rendered, `trust="untrusted"`) || !strings.Contains(rendered, "task paused") {
		t.Fatalf("missing untrusted tags or paused warning:\n%s", rendered)
	}
	if !strings.Contains(rendered, "stack.go") || !strings.Contains(rendered, "Use Go") {
		t.Fatalf("missing active invariant text:\n%s", rendered)
	}
	if strings.Contains(rendered, "Role: implementer") {
		t.Fatalf("answer_question prompt should not include execution role body:\n%s", rendered)
	}
	if strings.Contains(rendered, "Return output using the execution schema") {
		t.Fatalf("answer_question prompt must not request execution schema:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Selected action is answer_question") {
		t.Fatalf("missing answer_question stage guidance:\n%s", rendered)
	}
	for _, msg := range messages {
		if strings.Contains(msg.Content, `id="profile.active"`) || strings.Contains(msg.Content, `id="task.current"`) || strings.Contains(msg.Content, `id="memory.`) {
			if msg.Role != app.RoleUser {
				t.Fatalf("untrusted context must use user role, got %s for %q", msg.Role, msg.Content)
			}
		}
	}
}

func TestPromptBuilderIncludesRAGContextBeforeMemory(t *testing.T) {
	profile := profiles.DefaultProfiles(time.Now().UTC())[0]
	messages, err := NewBuilder().Build(BuildInput{
		Profile: profile,
		RAG: app.RAGContext{
			Mode:     "semantic",
			Strategy: "structural",
			Model:    "bge-m3",
			Chunks: []app.RAGChunk{{
				ChunkID: "chunk_1",
				Source:  "workspace",
				Path:    "RAG.md",
				Title:   "RAG.md",
				Section: "Embeddings",
				Score:   0.91,
				Text:    "Embedding text with <unsafe> markup.",
			}},
		},
		Memory: app.MemoryBundle{Short: []app.MemoryRecord{{Layer: app.LayerShort, Content: "short memory"}}},
		Query:  "query",
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered := RenderMessages(messages)
	ragIdx := strings.Index(rendered, `id="rag.workspace"`)
	memIdx := strings.Index(rendered, `id="memory.short"`)
	if ragIdx < 0 || memIdx < 0 || ragIdx > memIdx {
		t.Fatalf("RAG context should render before memory:\n%s", rendered)
	}
	for _, want := range []string{`trust="untrusted"`, `"chunk_id": "chunk_1"`, `"source": "workspace"`, `"title": "RAG.md"`, `"section": "Embeddings"`, `\u003cunsafe\u003e`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RAG prompt missing %q:\n%s", want, rendered)
		}
	}
}

func TestPromptBuilderNilFactoryUsesDefault(t *testing.T) {
	profile := profiles.DefaultProfiles(time.Now().UTC())[0]
	b := &Builder{}
	messages, err := b.Build(BuildInput{Profile: profile, Query: "query"})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 || !strings.Contains(RenderMessages(messages), "minimal CLI code assistant") {
		t.Fatalf("missing default base prompt")
	}
}
