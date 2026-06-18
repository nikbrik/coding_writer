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
	messages, err := NewBuilder().Build(BuildInput{Profile: profile, Task: &task, Memory: app.MemoryBundle{Work: []app.MemoryRecord{{Layer: app.LayerWork, Content: "work"}}, Long: []app.MemoryRecord{{Layer: app.LayerLong, Content: "long"}}, Short: []app.MemoryRecord{{Layer: app.LayerShort, Content: "short"}}}, Query: "query"})
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
		"Invariants",
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
	if strings.Contains(rendered, "Role: implementer") {
		t.Fatalf("answer_question prompt should not include execution role body:\n%s", rendered)
	}
	if strings.Contains(rendered, "Return output using the execution schema") {
		t.Fatalf("answer_question prompt must not request execution schema:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Selected action is answer_question") {
		t.Fatalf("missing answer_question stage guidance:\n%s", rendered)
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
