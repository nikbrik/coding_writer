package process

import (
	"context"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/invariants"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/profiles"
	"github.com/nikbrik/coding_writer/internal/providers"
	"github.com/nikbrik/coding_writer/internal/tasks"
	"github.com/nikbrik/coding_writer/internal/validation"
)

// ProcessController owns the deterministic chat exchange flow.
type ProcessController struct {
	Tasks             *tasks.Manager
	Profiles          *profiles.Manager
	ActiveProfileID   string
	Memory            *memory.Manager
	Invariants        *invariants.Manager
	Proposals         *memory.ProposalStore
	Classifier        *memory.Classifier
	Provider          providers.LLMProvider
	Model             string
	MemoryModel       string
	Builder           PromptBuilder
	PolicyRegistry    *StagePolicyRegistry
	TransitionGate    *TransitionGate
	RetryController   *RetryController
	AuditStore        *AuditStore
	SemanticValidator *SemanticValidator
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
	semanticSignal := "none"
	if c.Invariants != nil {
		violations, err := c.Invariants.CheckInput(ctx, input.Input)
		if err != nil {
			return nil, err
		}
		if len(violations) > 0 {
			invErr := invariants.Error(violations)
			_ = c.saveAudit(sessionID, taskPtr, stage, action, "rejected", invariantAuditMessages(violations), "", "", c.Model)
			return nil, invErr
		}
	}
	var autoTransition *TransitionResult
	if taskPtr == nil && preflight.AutoStartTitle != "" {
		if !input.RenderOnly {
			started, err := c.Tasks.Start(preflight.AutoStartTitle)
			if err != nil {
				_ = c.saveAudit(sessionID, nil, "", action, "transition_failed", nil, "", app.StagePlanning, c.Model, auditError(err), auditTransitionReason(preflight.AutoReason))
				return nil, err
			}
			autoTransition = &TransitionResult{Moved: true, From: "", To: started.Stage, Reason: preflight.AutoReason, State: started}
			_ = c.saveAudit(sessionID, &started, "", action, "transitioned", nil, "", started.Stage, c.Model, auditTransitionReason(preflight.AutoReason))
			taskPtr = &started
		} else {
			virtual := app.TaskState{Title: preflight.AutoStartTitle, Stage: app.StagePlanning, ExpectedAction: app.ExpectedUserInput}
			taskPtr = &virtual
		}
		stage = taskPtr.Stage
		action = ResolveActionKind(input.Input, stage, taskPtr.ExpectedAction)
	}
	if taskPtr != nil && preflight.AutoStage != "" {
		from := taskPtr.Stage
		if !input.RenderOnly {
			moved, err := c.Tasks.Move(preflight.AutoStage)
			if err != nil {
				_ = c.saveAudit(sessionID, taskPtr, from, action, "transition_failed", nil, from, preflight.AutoStage, c.Model, auditError(err), auditTransitionReason(preflight.AutoReason))
				return nil, err
			}
			autoTransition = &TransitionResult{Moved: true, From: from, To: moved.Stage, Reason: preflight.AutoReason, State: moved}
			_ = c.saveAudit(sessionID, taskPtr, from, action, "transitioned", nil, from, moved.Stage, c.Model, auditTransitionReason(preflight.AutoReason))
			taskPtr = &moved
		} else {
			virtual := *taskPtr
			virtual.Stage = preflight.AutoStage
			virtual.ExpectedAction = app.ExpectedUserInput
			taskPtr = &virtual
		}
		stage = taskPtr.Stage
		action = ResolveActionKind(input.Input, stage, taskPtr.ExpectedAction)
	}
	if !input.RenderOnly && c.SemanticValidator != nil {
		deterministicAction := action
		_ = c.saveAudit(sessionID, taskPtr, stage, action, "semantic_intent_call", nil, "", "", c.Model)
		intent, err := c.SemanticValidator.ResolveIntent(ctx, SemanticIntentInput{
			SessionID:      sessionID,
			UserInput:      input.Input,
			Stage:          stage,
			ExpectedAction: expectedAction(taskPtr),
			ActionKind:     action,
			Task:           taskPtr,
		})
		if err != nil {
			_ = c.saveAudit(sessionID, taskPtr, stage, action, "rejected", []string{app.AsError(err).Message}, "", "", c.Model, auditError(err))
			return nil, err
		}
		if intent.Confidence >= 0.65 && intent.ActionKind.Valid() {
			action = intent.ActionKind
			semanticSignal = intent.TransitionSignal
			action = actionForSemanticSignal(stage, action, semanticSignal)
			action = constrainSemanticActionToContext(stage, deterministicAction, action)
			action, semanticSignal = preserveLocalTransitionSignal(stage, input.Input, deterministicAction, action, semanticSignal)
			if stage != "" {
				if c.PolicyRegistry == nil {
					c.PolicyRegistry = NewStagePolicyRegistry()
				}
				policy, err := c.PolicyRegistry.PolicyFor(stage)
				if err != nil {
					return nil, err
				}
				if !policy.Allows(action) {
					err := app.ErrorWithHint(app.CategoryValidation, "forbidden_action", "action is not allowed in current stage", string(action)+" is not allowed in "+string(stage), nil)
					_ = c.saveAudit(sessionID, taskPtr, stage, action, "rejected", []string{app.AsError(err).Message}, "", "", c.Model)
					return nil, err
				}
			}
		}
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.PendingPlanning != nil {
		if isPlanningRejection(input.Input) || semanticSignal == "reject_planning" {
			state, err := c.Tasks.RejectPendingPlanningProposal()
			if err != nil {
				return nil, err
			}
			_ = c.saveAudit(sessionID, &state, state.Stage, action, "accepted", nil, "", "", c.Model)
			return &ExchangeResult{Answer: "planning proposal rejected", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: nil}, nil
		}
		if action == ActionProposeTransition {
			from := taskPtr.Stage
			state, err := c.Tasks.ApprovePendingPlanningProposal()
			if err != nil {
				return nil, err
			}
			transition := &TransitionResult{Moved: true, From: from, To: state.Stage, Reason: "pending planning approved", State: state}
			_ = c.saveAudit(sessionID, &state, from, action, "transitioned", nil, from, state.Stage, c.Model, auditTransitionReason(transition.Reason))
			return &ExchangeResult{Answer: "planning proposal approved", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: transition}, nil
		}
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.Stage == app.StagePlanning && taskPtr.PendingPlanning == nil && action == ActionProposeTransition && hasRunnablePlanningState(*taskPtr) {
		from := taskPtr.Stage
		state, err := c.Tasks.MoveWithPlanningOutput(taskPtr.Objective, taskPtr.AcceptanceCriteria, taskPtr.Plan, taskPtr.OpenQuestions, app.StageExecution)
		if err != nil {
			return nil, err
		}
		transition := &TransitionResult{Moved: true, From: from, To: state.Stage, Reason: "current planning approved", State: state}
		_ = c.saveAudit(sessionID, &state, from, action, "transitioned", nil, from, state.Stage, c.Model, auditTransitionReason(transition.Reason))
		return &ExchangeResult{Answer: "planning proposal approved", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: transition}, nil
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.Stage == app.StageExecution && action == ActionSummarizeExecution && (isReadyForValidationSignal(input.Input) || semanticSignal == "ready_for_validation") {
		from := taskPtr.Stage
		state, err := c.Tasks.Move(app.StageValidation)
		if err != nil {
			return nil, err
		}
		transition := &TransitionResult{Moved: true, From: from, To: state.Stage, Reason: "user signaled ready for validation", State: state}
		_ = c.saveAudit(sessionID, &state, from, action, "transitioned", nil, from, state.Stage, c.Model, auditTransitionReason(transition.Reason))
		return &ExchangeResult{Answer: "ready for validation", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: transition}, nil
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.Stage == app.StageValidation && (isDoneValidationSignal(input.Input) || semanticSignal == "ready_for_done") && hasTrustedEvidence(input.TrustedEvidence) {
		from := taskPtr.Stage
		state, err := c.Tasks.Move(app.StageDone)
		if err != nil {
			return nil, err
		}
		transition := &TransitionResult{Moved: true, From: from, To: state.Stage, Reason: "trusted verification completed", State: state}
		_ = c.saveAudit(sessionID, &state, from, action, "transitioned", nil, from, state.Stage, c.Model, auditTransitionReason(transition.Reason))
		return &ExchangeResult{Answer: "trusted verification completed", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: transition}, nil
	}

	profile, err := c.activeProfile()
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
	pausedGeneric := taskPtr != nil && taskPtr.Status == app.TaskStatusPaused && action == ActionAnswerQuestion
	if pausedGeneric {
		promptTask = nil
		promptStage = ""
		promptTaskID = ""
	}

	promptSessionID := sessionID
	if promptTask != nil && promptTask.LastSessionID != "" && promptTask.LastSessionID != sessionID {
		promptSessionID = promptTask.LastSessionID
	}
	bundle, err := c.Memory.SelectForPrompt(ctx, promptSessionID, promptTaskID, profile.ID)
	if err != nil {
		return nil, err
	}
	var activeInvariants []app.Invariant
	if c.Invariants != nil {
		activeInvariants, err = c.Invariants.List(ctx)
		if err != nil {
			return nil, err
		}
	}

	messages, err := c.Builder.Build(PromptBuildInput{
		Profile:    profile,
		Task:       promptTask,
		Memory:     bundle,
		Invariants: activeInvariants,
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
		Transition:     autoTransition,
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
		if c.Invariants != nil {
			violations, err := c.Invariants.CheckOutput(ctx, lastRaw)
			if err != nil {
				return result, err
			}
			if len(violations) > 0 {
				invErr := invariants.Error(violations)
				if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "rejected", invariantAuditMessages(violations), "", "", result.Model); auditErr != nil {
					result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
				}
				return result, invErr
			}
		}

		parsed, parseErr = Parse(stage, action, lastRaw)
		if parseErr == nil {
			parsed.TrustedEvidence = append([]string(nil), input.TrustedEvidence...)
			if c.SemanticValidator != nil {
				validatorErrors = RunStructuralValidators(parsed)
				if len(validatorErrors) == 0 {
					if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "semantic_output_call", nil, "", "", c.Model); auditErr != nil {
						result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
					}
					semanticErrors, err := c.SemanticValidator.ValidateResponse(ctx, SemanticValidationInput{
						SessionID:       sessionID,
						UserInput:       input.Input,
						Stage:           stage,
						ActionKind:      action,
						Task:            taskPtr,
						Parsed:          parsed,
						TrustedEvidence: input.TrustedEvidence,
					})
					if err != nil {
						validatorErrors = append(validatorErrors, app.AsError(err).Message)
					} else {
						validatorErrors = append(validatorErrors, semanticErrors...)
					}
				}
			} else {
				validatorErrors = RunValidators(parsed)
			}
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
		if strings.Contains(validatorErrors[0], "invariant_conflict") {
			return result, app.NewError(app.CategoryValidation, "invariant_conflict", "response rejected: "+strings.Join(validatorErrors, "; "), nil)
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
	if transitionCandidate == nil && taskPtr != nil && stage == app.StagePlanning && parsed.Planning != nil && parsed.Planning.Readiness == "ready_for_execution_proposal" && !input.AutoApproveTransitions {
		state, err := c.Tasks.SavePendingPlanningProposal(parsed.Planning.Summary, parsed.Planning.AcceptanceCriteria, parsed.Planning.Plan, parsed.Planning.OpenQuestions)
		if err != nil {
			return result, err
		}
		taskPtr = &state
	}
	if transitionCandidate == nil && taskPtr != nil && stage == app.StagePlanning && parsed.Planning != nil && parsed.Planning.Readiness != "ready_for_execution_proposal" && hasUsefulPlanningDraft(parsed.Planning) {
		state, err := c.Tasks.SetPlanningOutput(parsed.Planning.Summary, parsed.Planning.AcceptanceCriteria, parsed.Planning.Plan, parsed.Planning.OpenQuestions)
		if err != nil {
			return result, err
		}
		taskPtr = &state
	}
	if transitionCandidate == nil && taskPtr != nil && stage == app.StageExecution && parsed.Execution != nil {
		state, err := c.Tasks.SetExecutionProgress(parsed.Execution.CurrentStep, parsed.Execution.NextStep, parsed.Execution.CompletedSteps)
		if err != nil {
			return result, err
		}
		taskPtr = &state
	}

	memoryTaskID := taskID(taskPtr)
	if pausedGeneric {
		memoryTaskID = ""
	}
	userRecord, assistantRecord, err := c.Memory.SaveShortExchange(ctx, sessionID, profile.ID, memoryTaskID, input.Input, lastRaw)
	if err != nil {
		if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "persistence_failed", nil, "", "", result.Model, auditError(err)); auditErr != nil {
			result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
		}
		return result, err
	}
	if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "accepted", nil, "", "", result.Model); auditErr != nil {
		result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
	}
	if taskPtr != nil && taskPtr.Status == app.TaskStatusActive && taskPtr.Stage != app.StageDone {
		state, err := c.Tasks.SetLastSessionID(sessionID)
		if err != nil {
			return result, err
		}
		taskPtr = &state
	}

	memoryModel := c.MemoryModel
	if memoryModel == "" {
		memoryModel = c.Model
	}
	if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "provider_call", nil, "", "", memoryModel); auditErr != nil {
		return result, auditErr
	}
	classifierTask := taskPtr
	if pausedGeneric {
		classifierTask = nil
	}
	proposal, err := c.Classifier.Propose(ctx, memory.ClassificationInput{
		SessionID:          sessionID,
		UserMessageID:      userRecord.ID,
		AssistantMessageID: assistantRecord.ID,
		UserMessage:        input.Input,
		AssistantMessage:   lastRaw,
		Profile:            profile,
		Task:               classifierTask,
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
	if pausedGeneric {
		blockWorkProposalRecords(&proposal, "task paused; work memory mutation disabled")
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

func (c *ProcessController) activeProfile() (app.UserProfile, error) {
	id := strings.TrimSpace(c.ActiveProfileID)
	if id == "" {
		return c.Profiles.Active()
	}
	profile, err := c.Profiles.Get(id)
	if err == nil {
		return profile, nil
	}
	appErr := app.AsError(err)
	if appErr.Category == app.CategoryValidation && appErr.Code == "unknown_profile" {
		for _, candidate := range profiles.DefaultProfiles(time.Now().UTC()) {
			if candidate.ID == id {
				return candidate, nil
			}
		}
	}
	return app.UserProfile{}, err
}

func noSaveProposal(sessionID string) *app.MemoryProposal {
	return &app.MemoryProposal{ID: app.NewID("proposal"), SessionID: sessionID, SourceMessageIDs: []string{}, Records: []app.ProposedMemoryRecord{}, Provider: "local", Model: "no-save", CreatedAt: time.Now().UTC()}
}

func blockWorkProposalRecords(proposal *app.MemoryProposal, reason string) {
	for i := range proposal.Records {
		if proposal.Records[i].Layer == app.ProposedLayerWork && proposal.Records[i].Status == app.ProposalPending {
			proposal.Records[i].Status = app.ProposalBlocked
			proposal.Records[i].BlockReason = reason
		}
	}
}

func isPlanningRejection(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	replacer := strings.NewReplacer(".", " ", ",", " ", "!", " ", "?", " ", ";", " ", ":", " ", "\n", " ", "\t", " ")
	for _, token := range strings.Fields(replacer.Replace(normalized)) {
		switch token {
		case "no", "n", "reject", "rejected", "cancel", "нет", "не", "отклоняю", "отмена":
			return true
		}
	}
	return false
}

func (c *ProcessController) PreflightContext(ctx context.Context, input ExchangeInput) error {
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
	preflight, err := c.resolveProcessState(input)
	if err != nil {
		task, stage, action := c.bestEffortState(input)
		_ = c.saveAudit(sessionID, task, stage, action, "rejected", []string{app.AsError(err).Message}, "", "", c.Model)
		return err
	}
	if c.Invariants != nil {
		violations, err := c.Invariants.CheckInput(ctx, input.Input)
		if err != nil {
			return err
		}
		if len(violations) > 0 {
			invErr := invariants.Error(violations)
			_ = c.saveAudit(sessionID, preflight.Task, preflight.Stage, preflight.Action, "rejected", invariantAuditMessages(violations), "", "", c.Model)
			return invErr
		}
	}
	return nil
}

func invariantAuditMessages(violations []app.InvariantViolation) []string {
	out := make([]string, 0, len(violations))
	for _, v := range violations {
		out = append(out, "invariant_conflict: "+v.InvariantID+" evidence=[REDACTED]")
	}
	return out
}

type resolvedProcessState struct {
	Task           *app.TaskState
	Stage          app.TaskStage
	Action         ActionKind
	AutoStartTitle string
	AutoStage      app.TaskStage
	AutoReason     string
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
	autoStartTitle := ""
	autoStage := app.TaskStage("")
	autoReason := ""
	if taskPtr == nil && action == ActionPlanTask {
		autoStartTitle = taskTitleFromPlanningIntent(input.Input)
		autoReason = "planning intent started task"
		stage = app.StagePlanning
	}
	if taskPtr != nil && taskPtr.Stage == app.StageExecution && action == ActionPlanTask {
		autoStage = app.StagePlanning
		autoReason = "planning intent requires planning stage"
		stage = app.StagePlanning
	}
	if taskPtr != nil && taskPtr.Stage == app.StagePlanning && action == ActionProposeTransition && taskPtr.PendingPlanning == nil && !hasRunnablePlanningState(*taskPtr) {
		action = ActionPlanTask
	}

	if stage == "" && action != ActionAnswerQuestion && autoStartTitle == "" {
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
	return resolvedProcessState{Task: taskPtr, Stage: stage, Action: action, AutoStartTitle: autoStartTitle, AutoStage: autoStage, AutoReason: autoReason}, nil
}

func taskTitleFromPlanningIntent(input string) string {
	title := strings.TrimSpace(input)
	if title == "" {
		return "Task"
	}
	const maxTitleRunes = 80
	runes := []rune(title)
	if len(runes) > maxTitleRunes {
		return strings.TrimSpace(string(runes[:maxTitleRunes]))
	}
	return title
}

func expectedAction(task *app.TaskState) app.ExpectedAction {
	if task == nil {
		return ""
	}
	return task.ExpectedAction
}

func actionForSemanticSignal(stage app.TaskStage, action ActionKind, signal string) ActionKind {
	switch signal {
	case "approve_planning", "reject_planning":
		if stage == app.StagePlanning {
			return ActionProposeTransition
		}
	case "ready_for_validation":
		if stage == app.StageExecution {
			return ActionSummarizeExecution
		}
	case "ready_for_done":
		if stage == app.StageValidation {
			return ActionVerifyCriteria
		}
	}
	return action
}

func constrainSemanticActionToContext(stage app.TaskStage, deterministic, semantic ActionKind) ActionKind {
	if stage == "" {
		return deterministic
	}
	return semantic
}

func preserveLocalTransitionSignal(stage app.TaskStage, input string, deterministic, semantic ActionKind, signal string) (ActionKind, string) {
	if stage == app.StagePlanning && deterministic == ActionProposeTransition {
		if signal == "" || signal == "none" {
			signal = "approve_planning"
		}
		return deterministic, signal
	}
	if stage == app.StageExecution && deterministic == ActionSummarizeExecution && isReadyForValidationSignal(input) {
		return deterministic, "ready_for_validation"
	}
	if stage == app.StageValidation && isDoneValidationSignal(input) {
		return semantic, "ready_for_done"
	}
	return semantic, signal
}

func hasRunnablePlanningState(state app.TaskState) bool {
	return hasNonEmpty(state.Plan) && hasNonEmpty(state.AcceptanceCriteria)
}

func hasUsefulPlanningDraft(out *PlanningOutput) bool {
	return out != nil && (strings.TrimSpace(out.Summary) != "" || hasNonEmpty(out.Plan) || hasNonEmpty(out.AcceptanceCriteria) || hasNonEmpty(out.OpenQuestions))
}

func isReadyForValidationSignal(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	return containsAny(lower, []string{"готово к проверке", "ready for validation", "ready to validate"})
}

func isDoneValidationSignal(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	return containsAny(lower, []string{"проверь и заверши", "verify and finish", "verify and complete"})
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
