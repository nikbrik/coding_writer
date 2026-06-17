package tasks

import (
	"encoding/json"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/validation"
)

type RenderedState struct {
	app.TaskState
	AllowedNextStages []app.TaskStage `json:"allowed_next_stages"`
	PausedWarning     string          `json:"paused_warning,omitempty"`
}

func Render(state app.TaskState) (string, error) {
	rendered := RenderedState{TaskState: state, AllowedNextStages: AllowedNext(state.Stage)}
	if state.Status == app.TaskStatusPaused {
		rendered.PausedWarning = "task paused; do not continue execution until /task resume"
	}
	data, err := json.MarshalIndent(rendered, "", "  ")
	if err != nil {
		return "", err
	}
	return `<context_block id="task.current" type="task_state" source="storage" trust="untrusted">` + "\n" + validation.EscapeUntrusted(string(data)) + "\n</context_block>", nil
}
