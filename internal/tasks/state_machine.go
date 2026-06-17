package tasks

import "github.com/nikbrik/coding_writer/internal/app"

var AllowedTransitions = map[app.TaskStage][]app.TaskStage{
	app.StagePlanning:   {app.StageExecution},
	app.StageExecution:  {app.StageValidation, app.StagePlanning},
	app.StageValidation: {app.StageExecution, app.StageDone},
	app.StageDone:       {},
}

func IsAllowed(from, to app.TaskStage) bool {
	for _, allowed := range AllowedTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

func ValidStage(stage app.TaskStage) bool {
	_, ok := AllowedTransitions[stage]
	return ok
}

func ValidExpectedAction(action app.ExpectedAction) bool {
	switch action {
	case app.ExpectedUserInput, app.ExpectedLLMResponse, app.ExpectedUserConfirmation, app.ExpectedNone:
		return true
	default:
		return false
	}
}

func AllowedNext(stage app.TaskStage) []app.TaskStage {
	out := append([]app.TaskStage(nil), AllowedTransitions[stage]...)
	return out
}
