package process

import (
	"context"
	"fmt"
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
	Tasks              *tasks.Manager
	Profiles           *profiles.Manager
	ActiveProfileID    string
	Memory             *memory.Manager
	Invariants         *invariants.Manager
	Proposals          *memory.ProposalStore
	Classifier         *memory.Classifier
	Provider           providers.LLMProvider
	Model              string
	MemoryModel        string
	Builder            PromptBuilder
	PolicyRegistry     *StagePolicyRegistry
	TransitionGate     *TransitionGate
	LifecycleGate      *LifecycleGate
	RetryController    *RetryController
	AuditStore         *AuditStore
	SemanticValidator  *SemanticValidator
	InvariantValidator *SemanticValidator
	PromptImprover     *PromptImprover
	PlanningSwarm      *PlanningSwarm
	AgentRunner        *AgentRunner
}

// ExchangeInput controls a single process-controlled exchange.
type ExchangeInput struct {
	SessionID             string
	Input                 string
	RenderOnly            bool
	ActionKind            ActionKind
	TrustedEvidence       []string
	RequireMemoryProposal bool
	SkipSemanticIntent    bool
	SkipPromptImprovement bool
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
	originalInput := input.Input
	if validation.HasSecret(originalInput) {
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
	improvedPrompt := ""
	preWarnings := []string{}
	if !input.RenderOnly && !input.SkipPromptImprovement && strings.TrimSpace(originalInput) != "" {
		improved, err := c.improvePrompt(ctx, sessionID, originalInput, taskPtr)
		if err != nil {
			_ = c.saveAudit(sessionID, taskPtr, stage, action, "prompt_improvement_skipped", []string{app.AsError(err).Message}, "", "", c.Model, auditError(err))
			preWarnings = append(preWarnings, "prompt improvement skipped: "+app.AsError(err).Code)
		} else {
			improvedPrompt = improved
		}
	} else if input.SkipPromptImprovement {
		_ = c.saveAudit(sessionID, taskPtr, stage, action, "prompt_improvement_skipped", []string{"explicit internal skip"}, "", "", c.Model)
	}
	semanticSignal := "none"
	if c.Invariants != nil {
		violations, err := c.checkInvariantPolicy(ctx, sessionID, "input", originalInput, taskPtr, stage, action, !input.RenderOnly)
		if err != nil {
			return nil, err
		}
		if len(violations) > 0 {
			invErr := invariants.Error(violations)
			_ = c.saveAudit(sessionID, taskPtr, stage, action, "rejected", invariantAuditMessages(violations), "", "", c.Model)
			return nil, invErr
		}
		if strings.TrimSpace(input.Input) != strings.TrimSpace(originalInput) {
			violations, err = c.checkInvariantPolicy(ctx, sessionID, "input", input.Input, taskPtr, stage, action, !input.RenderOnly)
			if err != nil {
				return nil, err
			}
			if len(violations) > 0 {
				invErr := invariants.Error(violations)
				_ = c.saveAudit(sessionID, taskPtr, stage, action, "rejected", invariantAuditMessages(violations), "", "", c.Model)
				return nil, invErr
			}
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
			movedRes, err := c.lifecycle().Apply(LifecycleTransitionRequest{
				State:  *taskPtr,
				Source: TransitionSourceSystemReplan,
				Signal: SignalPlanningRequired,
				Reason: preflight.AutoReason,
			})
			if err != nil {
				_ = c.saveAudit(sessionID, taskPtr, from, action, "transition_failed", nil, from, preflight.AutoStage, c.Model, auditError(err), auditTransitionReason(preflight.AutoReason))
				return nil, err
			}
			autoTransition = &movedRes
			_ = c.saveAudit(sessionID, taskPtr, from, action, "transitioned", nil, from, movedRes.State.Stage, c.Model, auditTransitionReason(preflight.AutoReason), auditTransitionSource(TransitionSourceSystemReplan), auditTransitionSignal(SignalPlanningRequired))
			taskPtr = &movedRes.State
		} else {
			virtual := *taskPtr
			virtual.Stage = preflight.AutoStage
			virtual.ExpectedAction = app.ExpectedUserInput
			taskPtr = &virtual
		}
		stage = taskPtr.Stage
		action = ResolveActionKind(input.Input, stage, taskPtr.ExpectedAction)
	}
	if !input.RenderOnly && c.SemanticValidator == nil && taskPtr != nil && taskPtr.Stage == app.StageExecution && len(input.TrustedEvidence) > 0 && isReadyForExecutionReviewIntent(input.Input) {
		from := taskPtr.Stage
		transition, err := c.lifecycle().Apply(LifecycleTransitionRequest{
			State:           *taskPtr,
			Source:          TransitionSourceTrustedVerification,
			Signal:          SignalReadyForValidation,
			TrustedEvidence: input.TrustedEvidence,
			Reason:          sessionID,
		})
		if err != nil {
			return nil, err
		}
		_ = c.saveAudit(sessionID, &transition.State, from, ActionSummarizeExecution, "transitioned", nil, from, transition.State.Stage, c.Model, auditTransitionReason(transition.Reason), auditTransitionSource(TransitionSourceTrustedVerification), auditTransitionSignal(SignalReadyForValidation), auditEvidenceRefs(input.TrustedEvidence))
		return &ExchangeResult{Answer: "ready for validation", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: &transition}, nil
	}
	if !input.RenderOnly && c.SemanticValidator == nil && taskPtr != nil && taskPtr.Stage == app.StageValidation && len(input.TrustedEvidence) > 0 && isDoneValidationSignal(input.Input) {
		reviewWarnings := []string{}
		if !hasAcceptedReadyValidation(taskPtr) {
			if warnings, err := c.runTrustedEvidenceReviewer(ctx, sessionID, taskPtr, input); err != nil {
				reviewWarnings = append(reviewWarnings, "reviewer warning: "+app.AsError(err).Message)
			} else {
				reviewWarnings = append(reviewWarnings, warnings...)
			}
		}
		from := taskPtr.Stage
		parsedValidation := trustedValidationParsed(input.TrustedEvidence)
		transition, err := c.lifecycle().Apply(LifecycleTransitionRequest{
			State:           *taskPtr,
			Source:          TransitionSourceTrustedVerification,
			Signal:          SignalReadyForDone,
			Parsed:          parsedValidation,
			TrustedEvidence: input.TrustedEvidence,
			Reason:          sessionID,
		})
		if err != nil {
			return nil, err
		}
		_ = c.saveAudit(sessionID, &transition.State, from, ActionReviewOutput, "transitioned", nil, from, transition.State.Stage, c.Model, auditTransitionReason(transition.Reason), auditTransitionSource(TransitionSourceTrustedVerification), auditTransitionSignal(SignalReadyForDone), auditEvidenceRefs(input.TrustedEvidence))
		return &ExchangeResult{Answer: "trusted verification completed", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: &transition, Warnings: append(preWarnings, reviewWarnings...)}, nil
	}
	if !input.RenderOnly && c.SemanticValidator == nil && taskPtr != nil && taskPtr.Stage == app.StageValidation && len(input.TrustedEvidence) > 0 && isValidationReviewIntent(input.Input) {
		req := LifecycleTransitionRequest{
			State:           *taskPtr,
			Source:          TransitionSourceTrustedVerification,
			Signal:          SignalReadyForDone,
			TrustedEvidence: input.TrustedEvidence,
			Reason:          sessionID,
		}
		if !c.lifecycle().trustedEvidenceSatisfiesState(*taskPtr, req) {
			return nil, app.NewError(app.CategoryValidation, "transition_precondition_failed", "trusted verification does not satisfy acceptance criteria", nil)
		}
		if hasAcceptedReadyValidation(taskPtr) {
			return &ExchangeResult{Answer: trustedValidationReviewAnswer(), Model: c.Model, Proposal: noSaveProposal(sessionID), Warnings: preWarnings}, nil
		}
		reviewWarnings := []string{}
		if warnings, err := c.runTrustedEvidenceReviewer(ctx, sessionID, taskPtr, input); err != nil {
			reviewWarnings = append(reviewWarnings, "reviewer warning: "+app.AsError(err).Message)
		} else {
			reviewWarnings = append(reviewWarnings, warnings...)
		}
		state, err := c.Tasks.RecordAcceptedValidation("ready_for_done", input.TrustedEvidence)
		if err != nil {
			return nil, err
		}
		_ = c.saveAudit(sessionID, &state, app.StageValidation, ActionReviewOutput, "accepted", nil, "", "", c.Model, auditTransitionSource(TransitionSourceTrustedVerification), auditTransitionSignal(SignalReadyForDone), auditEvidenceRefs(input.TrustedEvidence))
		return &ExchangeResult{Answer: trustedValidationReviewAnswer(), Model: c.Model, Proposal: noSaveProposal(sessionID), Warnings: append(preWarnings, reviewWarnings...)}, nil
	}
	if !input.RenderOnly && c.SemanticValidator != nil && !input.SkipSemanticIntent {
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
			action, semanticSignal = guardUnsignaledSemanticTransition(stage, deterministicAction, action, semanticSignal)
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
			if stage == app.StagePlanning && action == ActionProposeTransition && taskPtr != nil && taskPtr.PendingPlanning == nil && !hasRunnablePlanningState(*taskPtr) {
				action = ActionPlanTask
				semanticSignal = "none"
			}
		}
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.Stage == app.StageValidation && semanticSignal != "ready_for_done" && action == ActionReviewOutput && len(taskPtr.ValidationEvidence) > 0 {
		if hasAcceptedReadyValidation(taskPtr) {
			return &ExchangeResult{Answer: trustedValidationReviewAnswer(), Model: c.Model, Proposal: noSaveProposal(sessionID), Warnings: preWarnings}, nil
		}
		reviewInput := input
		reviewInput.TrustedEvidence = append([]string(nil), taskPtr.ValidationEvidence...)
		req := LifecycleTransitionRequest{
			State:           *taskPtr,
			Source:          TransitionSourceTrustedVerification,
			Signal:          SignalReadyForDone,
			TrustedEvidence: reviewInput.TrustedEvidence,
			Reason:          sessionID,
		}
		if !c.lifecycle().trustedEvidenceSatisfiesState(*taskPtr, req) {
			return &ExchangeResult{Answer: blockedValidationReviewAnswer(), Model: c.Model, Proposal: noSaveProposal(sessionID), Warnings: preWarnings}, nil
		}
		reviewWarnings := []string{}
		if warnings, err := c.runTrustedEvidenceReviewer(ctx, sessionID, taskPtr, reviewInput); err != nil {
			reviewWarnings = append(reviewWarnings, "reviewer warning: "+app.AsError(err).Message)
		} else {
			reviewWarnings = append(reviewWarnings, warnings...)
		}
		state, err := c.Tasks.RecordAcceptedValidation("ready_for_done", reviewInput.TrustedEvidence)
		if err != nil {
			return nil, err
		}
		_ = c.saveAudit(sessionID, &state, app.StageValidation, ActionReviewOutput, "accepted", nil, "", "", c.Model, auditTransitionSource(TransitionSourceTrustedVerification), auditTransitionSignal(SignalReadyForDone), auditEvidenceRefs(reviewInput.TrustedEvidence))
		return &ExchangeResult{Answer: trustedValidationReviewAnswer(), Model: c.Model, Proposal: noSaveProposal(sessionID), Warnings: append(preWarnings, reviewWarnings...)}, nil
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.PendingPlanning != nil {
		if semanticSignal == "reject_planning" || c.SemanticValidator == nil && isPlanningRejection(input.Input) {
			state, err := c.Tasks.RejectPendingPlanningProposal()
			if err != nil {
				return nil, err
			}
			_ = c.saveAudit(sessionID, &state, state.Stage, action, "accepted", nil, "", "", c.Model)
			return &ExchangeResult{Answer: "planning proposal rejected", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: nil}, nil
		}
		if action == ActionProposeTransition {
			from := taskPtr.Stage
			approvedState, err := c.acceptPlanningApproval(ctx, sessionID, originalInput, taskPtr)
			if err != nil {
				return nil, err
			}
			taskPtr = &approvedState
			transition, err := c.lifecycle().Apply(LifecycleTransitionRequest{
				State:           *taskPtr,
				Source:          TransitionSourceUserApproval,
				Signal:          SignalApprovePlanning,
				TrustedEvidence: input.TrustedEvidence,
			})
			if err != nil {
				return nil, err
			}
			_ = c.saveAudit(sessionID, &transition.State, from, action, "transitioned", nil, from, transition.State.Stage, c.Model, auditTransitionReason(transition.Reason), auditTransitionSource(TransitionSourceUserApproval), auditTransitionSignal(SignalApprovePlanning))
			return c.continueAfterPlanningApproval(ctx, input, sessionID, &transition)
		}
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.Stage == app.StagePlanning && taskPtr.PendingPlanning == nil && action == ActionProposeTransition && hasRunnablePlanningState(*taskPtr) {
		from := taskPtr.Stage
		approvedState, err := c.acceptPlanningApproval(ctx, sessionID, originalInput, taskPtr)
		if err != nil {
			return nil, err
		}
		taskPtr = &approvedState
		transition, err := c.lifecycle().Apply(LifecycleTransitionRequest{
			State:  *taskPtr,
			Source: TransitionSourceUserApproval,
			Signal: SignalApprovePlanning,
			Reason: "current planning approved",
		})
		if err != nil {
			return nil, err
		}
		_ = c.saveAudit(sessionID, &transition.State, from, action, "transitioned", nil, from, transition.State.Stage, c.Model, auditTransitionReason(transition.Reason), auditTransitionSource(TransitionSourceUserApproval), auditTransitionSignal(SignalApprovePlanning))
		return c.continueAfterPlanningApproval(ctx, input, sessionID, &transition)
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.Stage == app.StageExecution && action == ActionSummarizeExecution && (semanticSignal == "ready_for_validation" || len(input.TrustedEvidence) > 0 && c.SemanticValidator != nil) {
		from := taskPtr.Stage
		transition, err := c.lifecycle().Apply(LifecycleTransitionRequest{
			State:           *taskPtr,
			Source:          TransitionSourceTrustedVerification,
			Signal:          SignalReadyForValidation,
			TrustedEvidence: input.TrustedEvidence,
			Reason:          sessionID,
		})
		if err != nil {
			return nil, err
		}
		_ = c.saveAudit(sessionID, &transition.State, from, action, "transitioned", nil, from, transition.State.Stage, c.Model, auditTransitionReason(transition.Reason), auditTransitionSource(TransitionSourceTrustedVerification), auditTransitionSignal(SignalReadyForValidation), auditEvidenceRefs(input.TrustedEvidence))
		return &ExchangeResult{Answer: "ready for validation", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: &transition}, nil
	}
	if !input.RenderOnly && taskPtr != nil && taskPtr.Stage == app.StageValidation && validationReadyForDoneRequested(input.Input, action, semanticSignal) {
		from := taskPtr.Stage
		evidence := append([]string(nil), input.TrustedEvidence...)
		if len(evidence) == 0 {
			evidence = append(evidence, taskPtr.ValidationEvidence...)
		}
		parsedValidation := &ParsedResponse{
			Stage:           app.StageValidation,
			ActionKind:      ActionReviewOutput,
			TrustedEvidence: evidence,
			Validation: &ValidationOutput{
				Findings:        []ValidationFinding{},
				PassedChecks:    []string{"trusted verification evidence available"},
				MissingEvidence: []string{},
				ResidualRisks:   []string{},
				Verdict:         "ready_for_done",
			},
		}
		req := LifecycleTransitionRequest{
			State:           *taskPtr,
			Source:          TransitionSourceTrustedVerification,
			Signal:          SignalReadyForDone,
			Parsed:          parsedValidation,
			TrustedEvidence: evidence,
			Reason:          sessionID,
		}
		if semanticSignal == "ready_for_done" || c.lifecycle().trustedEvidenceSatisfiesState(*taskPtr, req) {
			transition, err := c.lifecycle().Apply(req)
			if err != nil {
				return nil, err
			}
			_ = c.saveAudit(sessionID, &transition.State, from, action, "transitioned", nil, from, transition.State.Stage, c.Model, auditTransitionReason(transition.Reason), auditTransitionSource(TransitionSourceTrustedVerification), auditTransitionSignal(SignalReadyForDone), auditEvidenceRefs(evidence))
			return &ExchangeResult{Answer: "trusted verification completed", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: &transition}, nil
		}
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
	if strings.TrimSpace(improvedPrompt) != "" && strings.TrimSpace(improvedPrompt) != strings.TrimSpace(originalInput) {
		messages = append(messages, app.ChatMessage{
			ID:        app.NewID("msg"),
			Role:      app.RoleSystem,
			Content:   "Prompt improvement advisory (trusted as rewrite guidance, not as new user requirements): " + validation.EscapeUntrusted(improvedPrompt),
			CreatedAt: time.Now().UTC(),
		})
	}

	rendered := renderMessages(messages)
	result := &ExchangeResult{
		Model:          "",
		Messages:       messages,
		RenderedPrompt: rendered,
		Transition:     autoTransition,
		Warnings:       append([]string(nil), preWarnings...),
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
		res, err := c.completePrimary(ctx, sessionID, taskPtr, stage, action, input, messages)
		if err != nil {
			if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "rejected", nil, "", "", c.Model, auditError(err)); auditErr != nil {
				result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
			}
			return result, err
		}
		lastRaw = res.Message.Content
		result.Model = res.Model
		if c.Invariants != nil {
			violations, err := c.checkInvariantPolicy(ctx, sessionID, "output", lastRaw, taskPtr, stage, action, true)
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
			normalizeParsedActionForOutput(stage, &parsed)
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
						if shouldSkipBrokenSemanticValidator(err, parsed) {
							result.Warnings = append(result.Warnings, "semantic validation skipped: "+app.AsError(err).Code)
						} else {
							validatorErrors = append(validatorErrors, app.AsError(err).Message)
						}
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
		transition, err := c.TransitionGate.Check(*taskPtr, parsed, TransitionOptions{SessionID: sessionID})
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
		transition, err := c.TransitionGate.Apply(*taskPtr, parsed, TransitionOptions{SessionID: sessionID})
		if err != nil {
			if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "transition_failed", nil, transitionCandidate.From, transitionCandidate.To, result.Model, auditError(err), auditTransitionReason(transitionCandidate.Reason)); auditErr != nil {
				result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
			}
			return result, err
		} else if transition.Moved {
			result.Transition = &transition
			if auditErr := c.saveAudit(sessionID, taskPtr, stage, action, "transitioned", nil, transition.From, transition.To, result.Model, auditTransitionReason(transition.Reason), auditEvidenceRefs(input.TrustedEvidence)); auditErr != nil {
				result.Warnings = append(result.Warnings, "process audit skipped: "+app.AsError(auditErr).Code)
			}
			taskPtr = &transition.State
		}
	}
	if transitionCandidate == nil && taskPtr != nil && stage == app.StagePlanning && parsed.Planning != nil && parsed.Planning.Readiness == "ready_for_execution_proposal" {
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
		if _, err := c.Tasks.RecordAcceptedExecution(parsed.Execution.Summary, input.TrustedEvidence); err != nil {
			return result, err
		}
		state, err := c.Tasks.SetExecutionProgress(parsed.Execution.CurrentStep, parsed.Execution.NextStep, parsed.Execution.CompletedSteps)
		if err != nil {
			return result, err
		}
		taskPtr = &state
	}
	if transitionCandidate == nil && taskPtr != nil && stage == app.StageValidation && parsed.Validation != nil {
		state, err := c.Tasks.RecordAcceptedValidation(parsed.Validation.Verdict, input.TrustedEvidence)
		if err != nil {
			return result, err
		}
		taskPtr = &state
	}

	memoryTaskID := taskID(taskPtr)
	if pausedGeneric {
		memoryTaskID = ""
	}
	userRecord, assistantRecord, err := c.Memory.SaveShortExchange(ctx, sessionID, profile.ID, memoryTaskID, originalInput, lastRaw)
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
		UserMessage:        originalInput,
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

func shouldSkipBrokenSemanticValidator(err error, parsed ParsedResponse) bool {
	appErr := app.AsError(err)
	if appErr.Category != app.CategoryValidation || appErr.Code != "semantic_validator_invalid_json" {
		return false
	}
	if parsed.Stage != app.StageExecution || parsed.Execution == nil {
		return false
	}
	return strings.TrimSpace(parsed.Execution.Deliverable) != ""
}

func (c *ProcessController) continueAfterPlanningApproval(ctx context.Context, input ExchangeInput, sessionID string, transition *TransitionResult) (*ExchangeResult, error) {
	fallback := &ExchangeResult{Answer: "planning proposal approved", Model: c.Model, Proposal: noSaveProposal(sessionID), Transition: transition}
	next := input
	next.SessionID = sessionID
	next.Input = "Продолжи выполнение текущего шага утвержденного плана. Не повторяй исходные требования. Если нет trusted_evidence, не заявляй, что файлы созданы, код изменен, команды или тесты запущены; completed_steps и changed_artifacts должны быть пустыми, verification должна быть [\"not run\"], next_signal должен быть continue_execution, пока план не исчерпан. Для implementation code tasks в поле deliverable дай код для текущего шага в fenced code block или unified diff, который пользователь может применить. Для read-only verification tasks не выдумывай code diff; если нужен trusted tool evidence, дай явный blocker. Не возвращай только progress metadata."
	next.RenderOnly = false
	next.ActionKind = ActionExecutePlanStep
	next.TrustedEvidence = nil
	next.SkipSemanticIntent = true
	result, err := c.RunExchange(ctx, next)
	if err != nil {
		fallback.Warnings = append(fallback.Warnings, "execution continuation skipped: "+app.AsError(err).Code)
		return fallback, nil
	}
	if result == nil {
		return fallback, nil
	}
	answers := []string{}
	if strings.TrimSpace(result.Answer) != "" {
		answers = append(answers, result.Answer)
	}
	current := result
	for i := 1; i < c.autoExecutionLimit(transition); i++ {
		parsed, ok := parseExecutionAnswer(current.Answer)
		if !ok || parsed.NextSignal != "continue_execution" || hasNonEmpty(parsed.Blockers) {
			break
		}
		next.Input = "Продолжай выполнение следующего шага утвержденного плана автоматически. Не жди дополнительной команды пользователя. Если нет trusted_evidence, не заявляй, что файлы созданы, код изменен, команды или тесты запущены; completed_steps и changed_artifacts должны быть пустыми, verification должна быть [\"not run\"], next_signal должен быть continue_execution, пока план не исчерпан. Для implementation code tasks дай код для текущего шага в fenced code block или unified diff. Для read-only verification tasks не выдумывай code diff; если нужен trusted tool evidence, дай явный blocker."
		next.ActionKind = ActionExecutePlanStep
		next.TrustedEvidence = nil
		more, err := c.RunExchange(ctx, next)
		if err != nil {
			current.Warnings = append(current.Warnings, "execution auto-continue stopped: "+app.AsError(err).Code)
			break
		}
		if more == nil || strings.TrimSpace(more.Answer) == "" {
			break
		}
		answers = append(answers, more.Answer)
		current = more
	}
	if len(answers) > 0 {
		current.Answer = strings.Join(answers, "\n\n")
	} else if strings.TrimSpace(current.Answer) == "" {
		current.Answer = fallback.Answer
	}
	current.Transition = transition
	return current, nil
}

func (c *ProcessController) autoExecutionLimit(transition *TransitionResult) int {
	limit := 10
	if transition != nil && len(transition.State.Plan) > 0 {
		limit = len(transition.State.Plan)
	}
	if limit < 1 {
		return 1
	}
	if limit > 20 {
		return 20
	}
	return limit
}

func parseExecutionAnswer(answer string) (*ExecutionOutput, bool) {
	parsed, err := Parse(app.StageExecution, ActionExecutePlanStep, answer)
	if err != nil || parsed.Execution == nil {
		return nil, false
	}
	return parsed.Execution, true
}

func normalizeParsedActionForOutput(stage app.TaskStage, parsed *ParsedResponse) {
	if parsed == nil {
		return
	}
	if stage == app.StagePlanning && parsed.ActionKind == ActionProposeTransition && parsed.Planning != nil {
		parsed.ActionKind = ActionPlanTask
	}
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
	if hasExplicitTransitionNegation(normalized) || containsAny(normalized, []string{"отклоняю", "отмена", "reject", "cancel"}) {
		return true
	}
	for _, token := range strings.Fields(replacer.Replace(normalized)) {
		switch token {
		case "no", "n", "reject", "rejected", "cancel", "нет":
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
		violations, err := c.checkInvariantPolicy(ctx, sessionID, "input", input.Input, preflight.Task, preflight.Stage, preflight.Action, true)
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

func (c *ProcessController) checkInvariantPolicy(ctx context.Context, sessionID, direction, text string, task *app.TaskState, stage app.TaskStage, action ActionKind, allowSemantic bool) ([]app.InvariantViolation, error) {
	if c == nil || c.Invariants == nil {
		return nil, nil
	}
	if allowSemantic && c.InvariantValidator != nil {
		items, err := c.Invariants.List(ctx)
		if err != nil {
			return nil, err
		}
		return c.InvariantValidator.ValidateInvariants(ctx, InvariantValidationInput{
			SessionID:  sessionID,
			Direction:  direction,
			Text:       text,
			Stage:      stage,
			ActionKind: action,
			Task:       task,
			Invariants: items,
		})
	}
	switch direction {
	case "input":
		return c.Invariants.CheckInput(ctx, text)
	case "output":
		return c.Invariants.CheckOutput(ctx, text)
	default:
		return nil, app.NewError(app.CategoryValidation, "invalid_invariant_direction", "invalid invariant validation direction", nil)
	}
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
	case "approve_planning":
		if stage == app.StagePlanning {
			return ActionProposeTransition
		}
	case "reject_planning":
		if stage == app.StagePlanning {
			return ActionAnswerQuestion
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
	if stage == app.StageExecution && deterministic == ActionSummarizeExecution {
		return deterministic
	}
	if semantic.IsAllowedIn(stage) {
		return semantic
	}
	if stage == app.StageExecution && semantic == ActionReviewOutput {
		return ActionSummarizeExecution
	}
	if deterministic.IsAllowedIn(stage) {
		return deterministic
	}
	return semantic
}

func guardUnsignaledSemanticTransition(stage app.TaskStage, deterministic, semantic ActionKind, signal string) (ActionKind, string) {
	if stage == app.StagePlanning && semantic == ActionProposeTransition && deterministic != ActionProposeTransition && signal != "approve_planning" {
		return deterministic, signal
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
	if hasExplicitTransitionNegation(lower) {
		return false
	}
	return containsAny(lower, []string{"готово к проверке", "ready for validation", "ready to validate"})
}

func isDoneValidationSignal(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	if hasExplicitTransitionNegation(lower) {
		return false
	}
	return containsAny(lower, []string{"проверь и заверши", "проверь критерии и заверши", "заверши задачу", "завершить задачу", "verify and finish", "verify and complete", "finish the task", "complete the task"})
}

func isValidationReviewIntent(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	return containsAny(lower, []string{"проверь", "проверить", "критери", "review", "verify", "validate", "evidence"})
}

func hasAcceptedReadyValidation(task *app.TaskState) bool {
	return task != nil &&
		strings.TrimSpace(task.LastValidationID) != "" &&
		strings.TrimSpace(task.ValidationStatus) == "ready_for_done" &&
		len(task.ValidationEvidence) > 0
}

func validationReadyForDoneRequested(input string, action ActionKind, semanticSignal string) bool {
	if semanticSignal == "ready_for_done" {
		return true
	}
	if hasExplicitTransitionNegation(strings.ToLower(strings.TrimSpace(input))) {
		return false
	}
	return action == ActionVerifyCriteria
}

func trustedValidationParsed(evidence []string) *ParsedResponse {
	return &ParsedResponse{
		Stage:           app.StageValidation,
		ActionKind:      ActionReviewOutput,
		TrustedEvidence: append([]string(nil), evidence...),
		Validation: &ValidationOutput{
			Findings:        []ValidationFinding{},
			PassedChecks:    []string{"trusted verification evidence available"},
			MissingEvidence: []string{},
			ResidualRisks:   []string{},
			Verdict:         "ready_for_done",
		},
	}
}

func trustedValidationReviewAnswer() string {
	return strings.Join([]string{
		"Validation review:",
		"- trusted verification evidence is bound to the current task and satisfies acceptance criteria.",
		"- verdict: ready for done.",
		"- task is not closed yet because the user did not ask to finish in this step.",
	}, "\n")
}

func blockedValidationReviewAnswer() string {
	return strings.Join([]string{
		"Validation review:",
		"- trusted verification evidence is not sufficient for the current acceptance criteria.",
		"- verdict: blocked missing evidence.",
		"- task is not closed.",
	}, "\n")
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
	TransitionSource TransitionSource
	TransitionSignal TransitionSignal
	AgentRole        AgentRole
	MicrotaskID      string
	EvidenceRefs     []string
}

func auditError(err error) auditMeta { return auditMeta{Err: err} }
func auditRetry(count int) auditMeta { return auditMeta{RetryCount: count} }
func auditTransitionReason(reason string) auditMeta {
	return auditMeta{TransitionReason: reason}
}
func auditReason(reason string) auditMeta {
	return auditMeta{Reason: reason}
}
func auditTransitionSource(source TransitionSource) auditMeta {
	return auditMeta{TransitionSource: source}
}
func auditTransitionSignal(signal TransitionSignal) auditMeta {
	return auditMeta{TransitionSignal: signal}
}
func auditAgent(role AgentRole, microtaskID string) auditMeta {
	return auditMeta{AgentRole: role, MicrotaskID: microtaskID}
}
func auditEvidenceRefs(refs []string) auditMeta {
	return auditMeta{EvidenceRefs: append([]string(nil), refs...)}
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
		if item.TransitionSource != "" {
			meta.TransitionSource = item.TransitionSource
		}
		if item.TransitionSignal != "" {
			meta.TransitionSignal = item.TransitionSignal
		}
		if item.AgentRole != "" {
			meta.AgentRole = item.AgentRole
		}
		if item.MicrotaskID != "" {
			meta.MicrotaskID = item.MicrotaskID
		}
		if len(item.EvidenceRefs) > 0 {
			meta.EvidenceRefs = append([]string(nil), item.EvidenceRefs...)
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
		TransitionSource: string(meta.TransitionSource),
		TransitionSignal: string(meta.TransitionSignal),
		AgentRole:        string(meta.AgentRole),
		MicrotaskID:      meta.MicrotaskID,
		EvidenceRefs:     meta.EvidenceRefs,
		Model:            model,
		CreatedAt:        time.Now().UTC(),
	})
}

func (c *ProcessController) lifecycle() *LifecycleGate {
	if c.LifecycleGate == nil {
		c.LifecycleGate = &LifecycleGate{Tasks: c.Tasks}
	}
	if c.LifecycleGate.Tasks == nil {
		c.LifecycleGate.Tasks = c.Tasks
	}
	return c.LifecycleGate
}

func (c *ProcessController) improvePrompt(ctx context.Context, sessionID, original string, task *app.TaskState) (string, error) {
	if c.PromptImprover == nil {
		_ = c.saveAudit(sessionID, task, stageOf(task), ActionAnswerQuestion, "prompt_improvement_unavailable", []string{"prompt improver not configured"}, "", "", c.Model)
		return original, nil
	}
	_ = c.saveAudit(sessionID, task, stageOf(task), ActionAnswerQuestion, "prompt_improvement_call", nil, "", "", c.Model)
	res, err := c.PromptImprover.Improve(ctx, PromptImprovementInput{
		SessionID: sessionID,
		Original:  original,
		Task:      task,
	})
	if err != nil {
		return "", err
	}
	if c.SemanticValidator != nil {
		if err := c.SemanticValidator.ValidatePromptImprovement(ctx, PromptEquivalenceInput{
			SessionID: sessionID,
			Original:  original,
			Improved:  res.Improved,
			Task:      task,
		}); err != nil {
			return "", err
		}
	}
	_ = c.saveAudit(sessionID, task, stageOf(task), ActionAnswerQuestion, "prompt_improvement_accepted", nil, "", "", res.Model)
	return res.Improved, nil
}

func (c *ProcessController) acceptPlanningApproval(ctx context.Context, sessionID, userInput string, task *app.TaskState) (app.TaskState, error) {
	if task == nil {
		return app.TaskState{}, app.NewError(app.CategoryValidation, "missing_task", "planning approval requires task", nil)
	}
	reason := "offline planning approval fallback"
	if c.SemanticValidator != nil {
		_ = c.saveAudit(sessionID, task, task.Stage, ActionProposeTransition, "planning_approval_call", nil, "", "", c.Model)
		approval, err := c.SemanticValidator.ValidatePlanningApproval(ctx, PlanningApprovalInput{
			SessionID: sessionID,
			UserInput: userInput,
			Task:      task,
		})
		if err != nil {
			_ = c.saveAudit(sessionID, task, task.Stage, ActionProposeTransition, "rejected", []string{app.AsError(err).Message}, "", "", c.Model, auditError(err))
			return app.TaskState{}, err
		}
		if approval.Verdict != "approved" || approval.Confidence < 0.65 {
			state, err := c.Tasks.RecordPlanningApproval(approval.Verdict, approval.Reason, approval.Confidence, userInput)
			if err != nil {
				return app.TaskState{}, err
			}
			_ = c.saveAudit(sessionID, &state, state.Stage, ActionProposeTransition, "planning_approval_rejected", []string{approval.Reason}, "", "", c.Model)
			return app.TaskState{}, app.NewError(app.CategoryValidation, "planning_approval_not_accepted", "planning approval was not accepted", nil)
		}
		reason = approval.Reason
	}
	state, err := c.Tasks.RecordPlanningApproval("approved", reason, 1, userInput)
	if err != nil {
		return app.TaskState{}, err
	}
	decision := "planning_approval_accepted"
	if c.SemanticValidator == nil {
		decision = "planning_approval_fallback"
	}
	_ = c.saveAudit(sessionID, &state, state.Stage, ActionProposeTransition, decision, nil, "", "", c.Model)
	return state, nil
}

func (c *ProcessController) runTrustedEvidenceReviewer(ctx context.Context, sessionID string, task *app.TaskState, input ExchangeInput) ([]string, error) {
	if c == nil || c.AgentRunner == nil || task == nil {
		return nil, nil
	}
	role := AgentRoleReviewer
	microtaskID := app.NewID("microtask")
	_ = c.saveAudit(sessionID, task, app.StageValidation, ActionReviewOutput, "agent_call", nil, "", "", c.Model, auditAgent(role, microtaskID), auditEvidenceRefs(input.TrustedEvidence))
	run, err := c.AgentRunner.Run(ctx, AgentRunInput{
		SessionID: sessionID,
		Task:      task,
		UserInput: input.Input,
		Microtask: Microtask{
			ID:          microtaskID,
			Role:        role,
			Stage:       app.StageValidation,
			ActionKind:  ActionReviewOutput,
			Instruction: "Review the accepted execution using the app-issued trusted evidence summary. Return the validation schema only. Do not claim fixes or implementation changes.",
			PlanItem:    currentPlanItem(task),
		},
		TrustedEvidence: c.reviewerEvidenceContext(task, sessionID, input.TrustedEvidence),
	})
	if err != nil {
		_ = c.saveAudit(sessionID, task, app.StageValidation, ActionReviewOutput, "agent_rejected", []string{app.AsError(err).Message}, "", "", c.Model, auditError(err), auditAgent(role, microtaskID), auditEvidenceRefs(input.TrustedEvidence))
		return nil, err
	}
	parsed, parseErr := Parse(app.StageValidation, ActionReviewOutput, run.Raw)
	if parseErr != nil {
		_ = c.saveAudit(sessionID, task, app.StageValidation, ActionReviewOutput, "agent_invalid_json", []string{app.AsError(parseErr).Message}, "", "", run.Model, auditError(parseErr), auditAgent(role, microtaskID), auditEvidenceRefs(input.TrustedEvidence))
		return nil, parseErr
	}
	parsed.TrustedEvidence = append([]string(nil), input.TrustedEvidence...)
	validatorErrors := RunValidators(parsed)
	if len(validatorErrors) > 0 {
		_ = c.saveAudit(sessionID, task, app.StageValidation, ActionReviewOutput, "agent_rejected", validatorErrors, "", "", run.Model, auditAgent(role, microtaskID), auditEvidenceRefs(input.TrustedEvidence))
		return nil, app.NewError(app.CategoryValidation, "validation_failed", "reviewer output rejected: "+strings.Join(validatorErrors, "; "), nil)
	}
	decision := "agent_accepted"
	warnings := []string{}
	if parsed.Validation != nil && parsed.Validation.Verdict != "ready_for_done" {
		decision = "agent_revalidation_accepted"
		warnings = append(warnings, "reviewer verdict was "+parsed.Validation.Verdict+"; trusted evidence gate kept ready_for_done")
	}
	_ = c.saveAudit(sessionID, task, app.StageValidation, ActionReviewOutput, decision, nil, "", "", run.Model, auditAgent(role, microtaskID), auditEvidenceRefs(input.TrustedEvidence))
	return warnings, nil
}

func (c *ProcessController) reviewerEvidenceContext(task *app.TaskState, sessionID string, refs []string) []string {
	if c == nil || c.Tasks == nil || task == nil || strings.TrimSpace(c.Tasks.StorageDir) == "" {
		return append([]string(nil), refs...)
	}
	records, err := NewTrustedEvidenceStore(c.Tasks.StorageDir).Validate(task.ID, sessionID, refs)
	if err != nil || len(records) == 0 {
		return append([]string(nil), refs...)
	}
	out := make([]string, 0, len(records))
	for _, rec := range records {
		out = append(out, fmt.Sprintf("app-issued evidence ref=%s source=%q exit_code=%d sha256=%s", rec.ID, rec.Source, rec.ExitCode, rec.SHA256))
	}
	return out
}

func (c *ProcessController) completePrimary(ctx context.Context, sessionID string, task *app.TaskState, stage app.TaskStage, action ActionKind, input ExchangeInput, messages []app.ChatMessage) (providers.CompletionResponse, error) {
	if c.Provider == nil {
		return providers.CompletionResponse{}, app.NewError(app.CategoryProvider, "missing_provider", "provider is required", nil)
	}
	if stage == app.StagePlanning && action != ActionAnswerQuestion && c.PlanningSwarm != nil {
		c.PlanningSwarm.Audit = func(role AgentRole, microtaskID string, round int, decision string) {
			_ = c.saveAudit(sessionID, task, stage, action, decision, nil, "", "", c.Model, auditAgent(role, microtaskID), auditReason("planning_round="+intString(round)))
		}
		swarm, err := c.PlanningSwarm.Run(ctx, sessionID, task, input.Input)
		if err != nil {
			return providers.CompletionResponse{}, err
		}
		_ = c.saveAudit(sessionID, task, stage, action, "planning_swarm_final", nil, "", "", c.Model, auditReason(planningSwarmSummary(swarm)))
		return providers.CompletionResponse{
			Message: app.ChatMessage{ID: app.NewID("msg"), Role: app.RoleAssistant, Content: swarm.Raw, CreatedAt: time.Now().UTC()},
			Model:   c.Model,
		}, nil
	}
	if (stage == app.StageExecution || stage == app.StageValidation) && c.AgentRunner != nil {
		role := AgentRoleExecutor
		if stage == app.StageValidation {
			role = AgentRoleReviewer
		}
		microtaskID := app.NewID("microtask")
		_ = c.saveAudit(sessionID, task, stage, action, "agent_call", nil, "", "", c.Model, auditAgent(role, microtaskID))
		run, err := c.AgentRunner.Run(ctx, AgentRunInput{
			SessionID: sessionID,
			Task:      task,
			UserInput: input.Input,
			Microtask: Microtask{
				ID:          microtaskID,
				Role:        role,
				Stage:       stage,
				ActionKind:  action,
				Instruction: "Return the stage schema only.",
				PlanItem:    currentPlanItem(task),
			},
			TrustedEvidence: input.TrustedEvidence,
		})
		if err != nil {
			return providers.CompletionResponse{}, err
		}
		_ = c.saveAudit(sessionID, task, stage, action, "agent_accepted", nil, "", "", run.Model, auditAgent(role, microtaskID))
		return providers.CompletionResponse{
			Message: app.ChatMessage{ID: app.NewID("msg"), Role: app.RoleAssistant, Content: run.Raw, CreatedAt: time.Now().UTC()},
			Model:   run.Model,
		}, nil
	}
	return c.Provider.Complete(ctx, providers.CompletionRequest{
		Purpose:  providers.PurposeChat,
		Model:    c.Model,
		Messages: messages,
		JSONMode: RequiresSchema(action),
	})
}

func currentPlanItem(task *app.TaskState) string {
	if task == nil {
		return ""
	}
	return task.CurrentStep
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func planningSwarmSummary(result PlanningSwarmResult) string {
	return "rounds=" + intString(result.Rounds) + ";findings=" + intString(len(result.Findings)) + ";reviews=" + intString(len(result.Reviews))
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
		if strings.Contains(lower, "open questions block readiness") || strings.Contains(lower, "ask_clarification requires") || strings.Contains(lower, "ready_for_validation requires trusted evidence") || strings.Contains(lower, "execution deliverable is required") || strings.Contains(lower, "execution deliverable for code tasks") || strings.Contains(lower, "llm_validator:missing_user_input") || strings.Contains(lower, "llm_validator:read_only_violation") || strings.Contains(lower, "llm_validator:false_claim") || strings.Contains(lower, "llm_validator:unauthorized_claim") || strings.Contains(lower, "llm_validator:false_read_only_claim") || strings.Contains(lower, "llm_validator:memory_claim") || strings.Contains(lower, "llm_validator:missing_trusted_evidence") || strings.Contains(lower, "llm_validator:missing_implementation") || strings.Contains(lower, "llm_validator:no_trusted_evidence") || strings.Contains(lower, "llm_validator:no_side_effects") || strings.Contains(lower, "llm_validator:unauthorized_mutation") || strings.Contains(lower, "llm_validator:unsupported_mutation") || strings.Contains(lower, "llm_validator:unsupported_execution_claim") {
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

func stageOf(task *app.TaskState) app.TaskStage {
	if task == nil {
		return ""
	}
	return task.Stage
}

func renderMessages(messages []app.ChatMessage) string {
	// Duplicated from prompting.RenderMessages to keep process self-contained.
	var out string
	for _, msg := range messages {
		out += string(msg.Role) + "\n" + msg.Content + "\n\n"
	}
	return out
}
