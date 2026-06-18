package process

import (
	"context"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/profiles"
	"github.com/nikbrik/coding_writer/internal/providers"
	"github.com/nikbrik/coding_writer/internal/tasks"
	"github.com/nikbrik/coding_writer/internal/validation"
)

// ProcessController owns the deterministic chat exchange flow.
type ProcessController struct {
	Tasks           *tasks.Manager
	Profiles        *profiles.Manager
	Memory          *memory.Manager
	Proposals       *memory.ProposalStore
	Classifier      *memory.Classifier
	Provider        providers.LLMProvider
	Model           string
	MemoryModel     string
	Builder         PromptBuilder
	PolicyRegistry  *StagePolicyRegistry
	TransitionGate  *TransitionGate
	RetryController *RetryController
	AuditStore      *AuditStore
}

// ExchangeInput controls a single process-controlled exchange.
type ExchangeInput struct {
	SessionID              string
	Input                  string
	RenderOnly             bool
	ActionKind             ActionKind
	AutoApproveTransitions bool
	TrustedEvidence        []string
	RequireMemoryProposal  bool
}

// RunExchange executes the gated process loop.
func (c *ProcessController) RunExchange(ctx context.Context, input ExchangeInput) (*ExchangeResult, error) {
	if c == nil {
		return nil, app.NewError(app.CategoryInternal, "missing_process_controller", "process controller is required", nil)
	}
	sessionID := input.SessionID
	if sessionID == "" {
		sessionID = app.NewID("session")
	}
	if validation.HasSecret(input.Input) {
		_ = c.saveAudit(sessionID, nil, "", ActionAnswerQuestion, "rejected", []string{"secret-like input blocked"}, "", "", c.Model)
		return nil, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like input cannot be sent to provider", nil)
	}
	if c.Tasks == nil {
		return nil, app.NewError(app.CategoryInternal, "missing_task_manager", "task manager is required", nil)
	}
	if c.Profiles == nil {
		return nil, app.NewError(app.CategoryInternal, "missing_profile_manager", "profile manager is required", nil)
	}
	if c.Builder == nil {
		return nil, app.NewError(app.CategoryInternal, "missing_prompt_builder", "prompt builder is required", nil)
	}

	preflight, err := c.resolveProcessState(input)
	if err != nil {
		task, stage, action := c.bestEffortState(input)
		_ = c.saveAudit(sessionID, task, stage, action, "rejected", []string{app.AsError(err).Message}, "", "", c.Model)
		return nil, err
	}
	taskPtr := preflight.Task
	stage := preflight.Stage
	action := preflight.Action

	profile, err := c.Profiles.Active()
	if err != nil {
		return nil, err
	}

	// Profile and memory are intentionally loaded only after local hard gates pass.
	// This keeps paused/done/forbidden decisions provider-independent.

	if c.Memory == nil {
		return nil, app.NewError(app.CategoryInternal, "missing_memory_manager", "memory manager is required", nil)
	}
	if !input.RenderOnly {
		if c.Classifier == nil {
			return nil, app.NewError(app.CategoryInternal, "missing_classifier", "memory classifier is required", nil)
		}
		if c.Proposals == nil {
			return nil, app.NewError(app.CategoryInternal, "missing_proposal_store", "memory proposal store is required", nil)
		}
		if taskPtr != nil && action != ActionAnswerQuestion && c.TransitionGate == nil {
			return nil, app.NewError(app.CategoryInternal, "missing_transition_gate", "transition gate is required", nil)
		}
	}

	promptTask := taskPtr
	promptStage := stage
	promptTaskID := taskID(taskPtr)
	if taskPtr != nil && taskPtr.Status == app.TaskStatusPaused && action == ActionAnswerQuestion {
		promptTask = nil
		promptStage = ""
		promptTaskID = ""
	}

	bundle, err := c.Memory.SelectForPrompt(ctx, sessionID, promptTaskID, profile.ID)
	if err != nil {
		return nil, err
	}

	messages, err := c.Builder.Build(PromptBuildInput{
		Profile:    profile,
		Task:       promptTask,
		Memory:     bundle,
		Query:      input.Input,
		Stage:      promptStage,
		ActionKind: action,
	})
	if err != nil {
		return nil, err
	}

	rendered := renderMessages(messages)
	result := &ExchangeResult{
		Model:          "",
		Messages:       messages,
		RenderedPrompt: rendered,
	}

	if input.RenderOnly {
		return result, nil
	}

	if c.Provider == nil {
		return nil, app.NewError(app.CategoryProvider, "missing_provider", "provider is required", nil)
	}
	if c.Model == "" {
		return result, app.NewError(app.CategoryProvider, "missing_model", "active model is required", nil)
	}
	if c.RetryController == nil {
		c.RetryController = NewRetryController()
	}

	var lastRaw string
	var parsed ParsedResponse
	var parseErr error
	var validatorErrors []string
	attempt := 0
	maxRetries := c.RetryController.MaxRetries

	for {
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "provider_call", nil, "", "", c.Model, auditRetry(attempt)); auditErr != nil {
			return result, auditErr
		}
		res, err := c.Provider.Complete(ctx, providers.CompletionRequest{
			Purpose:  providers.PurposeChat,
			Model:    c.Model,
			Messages: messages,
			JSONMode: RequiresSchema(action),
		})
		if err != nil {
			if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "rejected", nil, "", "", c.Model, auditError(err)); auditErr != nil {
				result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
			}
			return result, err
		}
		lastRaw = res.Message.Content
		result.Model = res.Model

		parsed, parseErr = Parse(stage, action, lastRaw)
		if parseErr == nil {
			parsed.TrustedEvidence = append([]string(nil), input.TrustedEvidence...)
			validatorErrors = RunValidators(parsed)
			if len(validatorErrors) == 0 || !shouldRetryValidatorErrors(validatorErrors) || attempt >= maxRetries {
				break
			}
			if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "retried", validatorErrors, "", "", result.Model, auditRetry(attempt+1)); auditErr != nil {
				result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
			}
			attempt++
			messages = append(messages, app.ChatMessage{ID: app.NewID("msg"), Role: app.RoleSystem, Content: c.RetryController.CorrectionPrompt(validatorErrors), CreatedAt: time.Now().UTC()})
			continue
		}
		if !c.RetryController.ShouldRetry(parseErr) || attempt >= maxRetries {
			break
		}
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "retried", []string{app.AsError(parseErr).Message}, "", "", result.Model, auditRetry(attempt+1)); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		attempt++
		messages = append(messages, app.ChatMessage{ID: app.NewID("msg"), Role: app.RoleSystem, Content: c.RetryController.CorrectionPrompt([]string{app.AsError(parseErr).Message}), CreatedAt: time.Now().UTC()})
	}

	if parseErr != nil {
		result.Messages = messages
		result.RenderedPrompt = renderMessages(messages)
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "rejected", []string{app.AsError(parseErr).Message}, "", "", result.Model); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		return result, app.ErrorWithHint(app.CategoryValidation, app.AsError(parseErr).Code, app.AsError(parseErr).Message, "output rejected after validation", parseErr)
	}

	if len(validatorErrors) > 0 {
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "rejected", validatorErrors, "", "", result.Model); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		return result, app.NewError(app.CategoryValidation, "validation_failed", "response rejected: "+strings.Join(validatorErrors, "; "), nil)
	}
	if strings.TrimSpace(lastRaw) == "" {
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "rejected", []string{"empty assistant output"}, "", "", result.Model); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		return result, app.NewError(app.CategoryValidation, "empty_output", "assistant output is empty", nil)
	}

	result.Answer = lastRaw
	result.Messages = messages
	result.RenderedPrompt = renderMessages(messages)
	if taskPtr != nil && taskPtr.Status == app.TaskStatusPaused && action == ActionAnswerQuestion {
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "accepted", nil, "", "", result.Model); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		return result, nil
	}

	var transitionCandidate *TransitionResult
	if c.TransitionGate != nil && taskPtr != nil && action != ActionAnswerQuestion {
		transition, err := c.TransitionGate.Check(*taskPtr, parsed, TransitionOptions{AutoApprovePlanning: input.AutoApproveTransitions})
		if err != nil {
			if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "rejected", []string{app.AsError(err).Message}, "", "", result.Model); auditErr != nil {
				result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
			}
			return result, err
		}
		if transition.To != transition.From {
			transitionCandidate = &transition
		}
	}

	if transitionCandidate != nil {
		transition, err := c.TransitionGate.Apply(*taskPtr, parsed, TransitionOptions{AutoApprovePlanning: input.AutoApproveTransitions})
		if err != nil {
			if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "transition_failed", nil, transitionCandidate.From, transitionCandidate.To, result.Model, auditError(err), auditTransitionReason(transitionCandidate.Reason)); auditErr != nil {
				result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
			}
			return result, err
		} else if transition.Moved {
			result.Transition = &transition
			if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "transitioned", nil, transition.From, transition.To, result.Model, auditTransitionReason(transition.Reason)); auditErr != nil {
				result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
			}
			taskPtr = &transition.State
		}
	}

	userRecord, assistantRecord, err := c.Memory.SaveShortExchange(ctx, sessionID, profile.ID, taskID(taskPtr), input.Input, lastRaw)
	if err != nil {
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "persistence_failed", nil, "", "", result.Model, auditError(err)); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		return result, err
	}
	if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "accepted", nil, "", "", result.Model); auditErr != nil {
		result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
	}

	memoryModel := c.MemoryModel
	if memoryModel == "" {
		memoryModel = c.Model
	}
	if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "provider_call", nil, "", "", memoryModel); auditErr != nil {
		return result, auditErr
	}
	proposal, err := c.Classifier.Propose(ctx, memory.ClassificationInput{
		SessionID:          sessionID,
		UserMessageID:      userRecord.ID,
		AssistantMessageID: assistantRecord.ID,
		UserMessage:        input.Input,
		AssistantMessage:   lastRaw,
		Profile:            profile,
		Task:               taskPtr,
		Model:              memoryModel,
		ExistingShort:      bundle.Short,
		ExistingWork:       bundle.Work,
		ExistingLong:       bundle.Long,
	})
	if err != nil {
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "rejected", nil, "", "", memoryModel, auditError(err)); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		if input.RequireMemoryProposal {
			return result, err
		}
		result.Warnings = append(result.Warnings, "memory proposal skipped: "+app.AsError(err).Code)
		return result, nil
	}
	if err := c.Proposals.Save(ctx, proposal); err != nil {
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "persistence_failed", nil, "", "", memoryModel, auditError(err)); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		if input.RequireMemoryProposal {
			return result, err
		}
		result.Warnings = append(result.Warnings, "memory proposal skipped: "+app.AsError(err).Code)
		return result, nil
	}
	result.Proposal = &proposal

	return result, nil
}

// Preflight runs local hard gates without loading provider/profile/memory.
func (c *ProcessController) Preflight(input ExchangeInput) error {
	if c == nil {
		return app.NewError(app.CategoryInternal, "missing_process_controller", "process controller is required", nil)
	}
	sessionID := input.SessionID
	if sessionID == "" {
		sessionID = app.NewID("session")
	}
	if c.Tasks == nil {
		return app.NewError(app.CategoryInternal, "missing_task_manager", "task manager is required", nil)
	}
	if validation.HasSecret(input.Input) {
		err := app.NewError(app.CategoryValidation, "secret_blocked", "secret-like input cannot be sent to provider", nil)
		_ = c.saveAudit(sessionID, nil, "", ActionAnswerQuestion, "rejected", []string{err.Message}, "", "", c.Model)
		return err
	}
	_, err := c.resolveProcessState(input)
	if err != nil {
		task, stage, action := c.bestEffortState(input)
		_ = c.saveAudit(sessionID, task, stage, action, "rejected", []string{app.AsError(err).Message}, "", "", c.Model)
	}
	return err
}

type resolvedProcessState struct {
	Task   *app.TaskState
	Stage  app.TaskStage
	Action ActionKind
}

func (c *ProcessController) resolveProcessState(input ExchangeInput) (resolvedProcessState, error) {
	taskState, taskErr := c.Tasks.Current()
	var taskPtr *app.TaskState
	if taskErr != nil {
		appErr := app.AsError(taskErr)
		if appErr.Category != app.CategoryValidation || appErr.Code != "missing_current_task" {
			return resolvedProcessState{}, taskErr
		}
	} else if taskState.ID != "" {
		taskPtr = &taskState
	}

	stage := app.TaskStage("")
	if taskPtr != nil {
		stage = taskPtr.Stage
	}

	var expected app.ExpectedAction
	if taskPtr != nil {
		expected = taskPtr.ExpectedAction
	}
	resolvedAction := ResolveActionKind(input.Input, stage, expected)
	action := input.ActionKind
	if action == "" {
		action = resolvedAction
	} else if action == ActionAnswerQuestion && resolvedAction != ActionAnswerQuestion {
		action = resolvedAction
	}
	if !action.Valid() {
		return resolvedProcessState{}, app.NewError(app.CategoryValidation, "invalid_action", "invalid process action", nil)
	}

	if stage == "" && action != ActionAnswerQuestion {
		return resolvedProcessState{}, app.ErrorWithHint(app.CategoryValidation, "missing_task", "no active task; start a task before process actions", "use /task start <title> to create a task", nil)
	}

	if taskPtr != nil && taskPtr.Status == app.TaskStatusPaused && (action != ActionAnswerQuestion || isPausedTaskScopedInput(input.Input)) {
		return resolvedProcessState{}, app.NewError(app.CategoryValidation, "task_paused", "task is paused; resume before continuing", nil)
	}

	if stage == app.StageDone {
		if action != ActionAnswerQuestion && action != ActionSummarizeDone {
			return resolvedProcessState{}, app.NewError(app.CategoryValidation, "task_done", "done task is terminal; no mutations allowed", nil)
		}
	}

	if stage != "" {
		if c.PolicyRegistry == nil {
			c.PolicyRegistry = NewStagePolicyRegistry()
		}
		policy, err := c.PolicyRegistry.PolicyFor(stage)
		if err != nil {
			return resolvedProcessState{}, err
		}
		if !policy.Allows(action) {
			return resolvedProcessState{}, app.ErrorWithHint(app.CategoryValidation, "forbidden_action", "action is not allowed in current stage", string(action)+" is not allowed in "+string(stage), nil)
		}
	}
	return resolvedProcessState{Task: taskPtr, Stage: stage, Action: action}, nil
}

func isPausedTaskScopedInput(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	return containsAny(lower, []string{"task", "задач", "stage", "current_step", "expected_action", "plan", "criteria", "work memory", "working memory", "продолж", "continue", "resume", "реализ", "исполн", "выполн", "план", "критери", "шаг", "стад", "этап", "исправ", "fix", "edit", "change", "update", "write", "create", "delete"})
}

type auditMeta struct {
	Err              error
	Reason           string
	RetryCount       int
	TransitionReason string
}

func auditError(err error) auditMeta { return auditMeta{Err: err} }
func auditRetry(count int) auditMeta { return auditMeta{RetryCount: count} }
func auditTransitionReason(reason string) auditMeta {
	return auditMeta{TransitionReason: reason}
}

func (c *ProcessController) saveAudit(sessionID string, task *app.TaskState, stage app.TaskStage, action ActionKind, decision string, validatorErrors []string, from, to app.TaskStage, model string, metas ...auditMeta) error {
	if c == nil || c.AuditStore == nil {
		return app.NewError(app.CategoryInternal, "missing_audit_store", "process audit store is required", nil)
	}
	taskID := ""
	if task != nil {
		taskID = task.ID
	}
	var meta auditMeta
	for _, item := range metas {
		if item.Err != nil {
			meta.Err = item.Err
		}
		if item.Reason != "" {
			meta.Reason = item.Reason
		}
		if item.RetryCount != 0 {
			meta.RetryCount = item.RetryCount
		}
		if item.TransitionReason != "" {
			meta.TransitionReason = item.TransitionReason
		}
	}
	if meta.Err != nil {
		appErr := app.AsError(meta.Err)
		meta.Reason = appErr.Message
	}
	return c.AuditStore.Save(ProcessAuditEvent{
		TaskID:           taskID,
		SessionID:        sessionID,
		Stage:            stage,
		ActionKind:       action,
		Decision:         decision,
		ValidatorErrors:  validatorErrors,
		ErrorCategory:    errorCategory(meta.Err),
		ErrorCode:        errorCode(meta.Err),
		Reason:           meta.Reason,
		RetryCount:       meta.RetryCount,
		PromptPolicyID:   "p0-process-controller-v1",
		TransitionFrom:   string(from),
		TransitionTo:     string(to),
		TransitionReason: meta.TransitionReason,
		Model:            model,
		CreatedAt:        time.Now().UTC(),
	})
}

func errorCategory(err error) string {
	if err == nil {
		return ""
	}
	return string(app.AsError(err).Category)
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	return app.AsError(err).Code
}

func rejectedOutputPrompt(raw string) string {
	return `<rejected_model_output trust="untrusted">` + "\n" + validation.EscapeUntrusted(raw) + "\n</rejected_model_output>"
}

func shouldRetryValidatorErrors(errs []string) bool {
	for _, err := range errs {
		lower := strings.ToLower(err)
		if strings.Contains(lower, "missing required") || strings.Contains(lower, "unknown") || strings.Contains(lower, "schema") {
			return true
		}
	}
	return false
}

func (c *ProcessController) bestEffortState(input ExchangeInput) (*app.TaskState, app.TaskStage, ActionKind) {
	if c == nil || c.Tasks == nil {
		return nil, "", ActionAnswerQuestion
	}
	state, err := c.Tasks.Current()
	if err != nil || state.ID == "" {
		action := input.ActionKind
		if action == "" || !action.Valid() {
			action = ActionAnswerQuestion
		}
		return nil, "", action
	}
	action := input.ActionKind
	if action == "" || !action.Valid() {
		action = ResolveActionKind(input.Input, state.Stage, state.ExpectedAction)
	}
	return &state, state.Stage, action
}

func taskID(task *app.TaskState) string {
	if task == nil {
		return ""
	}
	return task.ID
}

func renderMessages(messages []app.ChatMessage) string {
	// Duplicated from prompting.RenderMessages to keep process self-contained.
	var out string
	for _, msg := range messages {
		out += string(msg.Role) + "\n" + msg.Content + "\n\n"
	}
	return out
}
