package process

import "github.com/nikbrik/coding_writer/internal/app"

// StagePolicy is the trusted per-stage contract.
type StagePolicy struct {
	Stage            app.TaskStage
	Role             string
	AllowedActions   []ActionKind
	ForbiddenActions []ActionKind
	OutputSchema     string
	Permissions      PermissionSet
}

func (p StagePolicy) Allows(kind ActionKind) bool {
	for _, forbidden := range p.ForbiddenActions {
		if forbidden == kind {
			return false
		}
	}
	for _, allowed := range p.AllowedActions {
		if allowed == kind {
			return true
		}
	}
	return false
}
