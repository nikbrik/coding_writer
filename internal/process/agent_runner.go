package process

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/providers"
)

type AgentRole string

const (
	AgentRolePromptImprover           AgentRole = "prompt_improver"
	AgentRolePlanOrchestrator         AgentRole = "planning_orchestrator"
	AgentRoleRequirementsSpecialist   AgentRole = "requirements_specialist"
	AgentRoleCodeResearchSpecialist   AgentRole = "code_research_specialist"
	AgentRoleArchitectureSpecialist   AgentRole = "architecture_specialist"
	AgentRoleTestValidationSpecialist AgentRole = "test_validation_specialist"
	AgentRoleRiskRegressionSpecialist AgentRole = "risk_regression_specialist"
	AgentRoleExecutor                 AgentRole = "executor"
	AgentRoleReviewer                 AgentRole = "reviewer"
	AgentRoleFinalizer                AgentRole = "finalizer"
)

type Microtask struct {
	ID          string
	Role        AgentRole
	Stage       app.TaskStage
	ActionKind  ActionKind
	Instruction string
	PlanItem    string
}

type AgentRunInput struct {
	SessionID       string
	Task            *app.TaskState
	Microtask       Microtask
	UserInput       string
	TrustedEvidence []string
}

type AgentRunResult struct {
	MicrotaskID string
	Role        AgentRole
	Raw         string
	Model       string
}

type AgentRunner struct {
	Provider providers.LLMProvider
	Model    string
	Factory  *StagePromptFactory
}

func (r *AgentRunner) Run(ctx context.Context, input AgentRunInput) (AgentRunResult, error) {
	if r == nil || r.Provider == nil {
		return AgentRunResult{}, app.NewError(app.CategoryInternal, "missing_agent_runner", "agent runner provider is required", nil)
	}
	if strings.TrimSpace(input.Microtask.ID) == "" {
		input.Microtask.ID = app.NewID("microtask")
	}
	system := r.systemPrompt(input.Microtask)
	payload, err := json.Marshal(map[string]any{
		"task":             microtaskTaskSummary(input.Task),
		"user_input":       input.UserInput,
		"instruction":      input.Microtask.Instruction,
		"plan_item":        input.Microtask.PlanItem,
		"trusted_evidence": input.TrustedEvidence,
	})
	if err != nil {
		return AgentRunResult{}, app.NewError(app.CategoryInternal, "agent_payload_encode", err.Error(), err)
	}
	res, err := r.Provider.Complete(ctx, providers.CompletionRequest{
		Purpose:  providers.PurposeChat,
		Model:    r.Model,
		JSONMode: input.Microtask.ActionKind != ActionAnswerQuestion,
		Messages: []app.ChatMessage{
			{ID: app.NewID("msg"), Role: app.RoleSystem, Content: system, CreatedAt: time.Now().UTC()},
			{ID: app.NewID("msg"), Role: app.RoleUser, Content: string(payload), CreatedAt: time.Now().UTC()},
		},
	})
	if err != nil {
		return AgentRunResult{}, err
	}
	return AgentRunResult{
		MicrotaskID: input.Microtask.ID,
		Role:        input.Microtask.Role,
		Raw:         res.Message.Content,
		Model:       res.Model,
	}, nil
}

func microtaskTaskSummary(task *app.TaskState) map[string]any {
	if task == nil {
		return nil
	}
	return map[string]any{
		"id":                  task.ID,
		"stage":               task.Stage,
		"objective":           task.Objective,
		"current_step":        task.CurrentStep,
		"approved_plan_id":    task.ApprovedPlanID,
		"acceptance_criteria": task.AcceptanceCriteria,
		"plan":                task.Plan,
		"microtasks":          task.Microtasks,
		"completed_steps":     task.CompletedSteps,
		"validation_status":   task.ValidationStatus,
		"validation_evidence": task.ValidationEvidence,
	}
}

func (r *AgentRunner) systemPrompt(task Microtask) string {
	if r.Factory == nil {
		r.Factory = NewStagePromptFactory(NewStagePolicyRegistry())
	}
	var parts []string
	parts = append(parts, r.Factory.BaseSystemPrompt(), r.Factory.ProcessContractPrompt())
	if task.Stage != "" {
		if stagePrompt, err := r.Factory.StagePrompt(task.Stage, task.ActionKind); err == nil {
			parts = append(parts, stagePrompt)
		}
	}
	parts = append(parts, "Agent role: "+string(task.Role)+".")
	if strings.TrimSpace(task.Instruction) != "" {
		parts = append(parts, "Microtask instruction: "+task.Instruction)
	}
	return strings.Join(parts, "\n")
}
