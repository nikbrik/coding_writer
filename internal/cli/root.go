package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/invariants"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/process"
	"github.com/nikbrik/coding_writer/internal/profiles"
	"github.com/nikbrik/coding_writer/internal/prompting"
	"github.com/nikbrik/coding_writer/internal/providers"
	"github.com/nikbrik/coding_writer/internal/storage"
	"github.com/nikbrik/coding_writer/internal/tasks"
	"github.com/nikbrik/coding_writer/internal/tui"
	"github.com/nikbrik/coding_writer/internal/validation"
)

var Version = "dev"

const (
	trustedVerificationTimeout       = 2 * time.Minute
	trustedVerificationOutputLimit   = 256 * 1024
	trustedVerificationCommandMaxLen = 512
)

type globalOptions struct {
	StorageDir             string
	Model                  string
	MemoryModel            string
	Profile                string
	OpenRouterBaseURL      string
	TrustOpenRouterBaseURL bool
	JSON                   bool
	Quiet                  bool
}

type runtime struct {
	StorageDir      string
	Config          app.AppConfig
	ConfigMgr       *app.ConfigManager
	Profiles        *profiles.Manager
	Tasks           *tasks.Manager
	Memory          *memory.Manager
	Invariants      *invariants.Manager
	Proposals       *memory.ProposalStore
	Provider        providers.LLMProvider
	Builder         *prompting.Builder
	Classifier      *memory.Classifier
	Process         *process.ProcessController
	PolicyRegistry  *process.StagePolicyRegistry
	TransitionGate  *process.TransitionGate
	RetryController *process.RetryController
	AuditStore      *process.AuditStore
	DisclosureShown bool
	Quiet           bool
}

func Execute() error {
	return ExecuteNamed(filepath.Base(os.Args[0]))
}

func ExecuteNamed(invocation string) error {
	opts := &globalOptions{}
	cmd := newRootCommandForInvocation(opts, invocation)
	if err := cmd.Execute(); err != nil {
		err = normalizeTopLevelCLIError(err)
		printError(cmd.ErrOrStderr(), err, opts.JSON || argvRequestsJSON(os.Args[1:]))
		return err
	}
	return nil
}

func ExitCode(err error) int { return app.ExitCode(err) }

func normalizeTopLevelCLIError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *app.Error
	if errors.As(err, &appErr) {
		return err
	}
	return app.ErrorWithHint(app.CategoryCLI, "command_error", err.Error(), "run assistant --help", err)
}

func argvRequestsJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" {
			return true
		}
		if value, ok := strings.CutPrefix(arg, "--json="); ok {
			parsed, err := strconv.ParseBool(value)
			return err == nil && parsed
		}
	}
	return false
}

func newRootCommand(opts *globalOptions) *cobra.Command {
	return newRootCommandForInvocation(opts, "assistant")
}

func newRootCommandForInvocation(opts *globalOptions, invocation string) *cobra.Command {
	productMode := invocation == "cw" || invocation == "codingwriter"
	topChat := &chatOptions{}
	use := "assistant"
	short := "Stateful CLI assistant with memory layers"
	if productMode {
		use = invocation
		short = "Terminal coding agent workspace"
	}
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !productMode {
				return cmd.Help()
			}
			return runTopLevelChat(cmd, opts, topChat)
		},
	}
	cmd.PersistentFlags().StringVar(&opts.StorageDir, "storage-dir", "", "runtime storage directory")
	cmd.PersistentFlags().StringVar(&opts.Model, "model", "", "active model id")
	cmd.PersistentFlags().StringVar(&opts.MemoryModel, "memory-model", "", "memory classifier model id")
	cmd.PersistentFlags().StringVar(&opts.Profile, "profile", "", "active profile id")
	cmd.PersistentFlags().StringVar(&opts.OpenRouterBaseURL, "openrouter-base-url", "", "OpenRouter-compatible base URL")
	cmd.PersistentFlags().BoolVar(&opts.TrustOpenRouterBaseURL, "trust-openrouter-base-url", false, "trust non-default OpenRouter-compatible base URL for this invocation")
	cmd.PersistentFlags().BoolVar(&opts.JSON, "json", false, "emit JSON")
	cmd.PersistentFlags().BoolVar(&opts.Quiet, "quiet", false, "suppress diagnostic output")
	if productMode {
		cmd.Flags().BoolVar(&topChat.TUI, "tui", false, "run TUI")
		cmd.Flags().BoolVar(&topChat.Plain, "plain", false, "run plain REPL fallback")
		cmd.Flags().BoolVar(&topChat.Once, "once", false, "run one request")
		cmd.Flags().StringVar(&topChat.Input, "input", "", "input text for --once")
		cmd.Flags().BoolVar(&topChat.RenderPrompt, "render-prompt", false, "render prompt without provider call")
		cmd.Flags().StringVar(&topChat.Verify, "verify", "", "run verification command and pass trusted evidence to validation")
	}
	cmd.AddCommand(initCommand(opts), chatCommand(opts), profilesCommand(opts), memoryCommand(opts), invariantsCommand(opts), taskCommand(opts), processCommand(opts), privacyCommand(opts))
	return cmd
}

func newRuntime(ctx context.Context, opts *globalOptions) (*runtime, error) {
	storageDir, err := app.ResolveStorageDir(opts.StorageDir)
	if err != nil {
		return nil, app.NewError(app.CategoryStorage, "storage_dir", err.Error(), err)
	}
	cfgMgr := app.NewConfigManager(storageDir)
	profMgr := profiles.NewManager(storageDir, cfgMgr)
	cfg, err := cfgMgr.LoadEffective(app.ConfigOptions{StorageDir: storageDir, ActiveModel: opts.Model, MemoryModel: opts.MemoryModel, ActiveProfileID: opts.Profile, OpenRouterBaseURL: opts.OpenRouterBaseURL, TrustOpenRouterBaseURL: opts.TrustOpenRouterBaseURL})
	if err != nil {
		return nil, err
	}
	if cfg.ActiveProfileID == "" {
		cfg.ActiveProfileID = "student"
	}
	if opts.Profile != "" {
		cfg.ActiveProfileID = opts.Profile
	}
	if opts.Model != "" {
		cfg.ActiveModel = opts.Model
	}
	if opts.MemoryModel != "" {
		cfg.MemoryModel = opts.MemoryModel
	}
	if cfg.MemoryModel == "" {
		cfg.MemoryModel = cfg.ActiveModel
	}
	if opts.Model != "" {
		if err := validateModelSyntax(opts.Model); err != nil {
			return nil, err
		}
	}
	memMgr := memory.NewManager(storageDir)
	invMgr := invariants.NewManager(storageDir)
	return &runtime{
		StorageDir: storageDir,
		Config:     cfg,
		ConfigMgr:  cfgMgr,
		Profiles:   profMgr,
		Tasks:      tasks.NewManager(storageDir),
		Memory:     memMgr,
		Invariants: invMgr,
		Proposals:  memory.NewProposalStore(storageDir, memMgr),
		Quiet:      opts.Quiet,
	}, nil
}

func (rt *runtime) activeProfile() (app.UserProfile, error) {
	id := strings.TrimSpace(rt.Config.ActiveProfileID)
	if id == "" {
		id = "student"
	}
	return rt.profileByID(id)
}

func (rt *runtime) syncActiveProfile(profile app.UserProfile) {
	rt.Config.ActiveProfileID = profile.ID
	if rt.Process != nil {
		rt.Process.ActiveProfileID = profile.ID
	}
}

func (rt *runtime) currentMutableWorkTask() (app.TaskState, error) {
	taskState, err := rt.Tasks.Current()
	if err != nil {
		appErr := app.AsError(err)
		if appErr.Category == app.CategoryValidation && appErr.Code == "missing_current_task" {
			return app.TaskState{}, app.NewError(app.CategoryValidation, "missing_current_task", "work memory requires active task", nil)
		}
		return app.TaskState{}, err
	}
	if taskState.Status == app.TaskStatusPaused {
		return taskState, app.NewError(app.CategoryValidation, "task_paused", "resume task before mutating working memory", nil)
	}
	if taskState.Stage == app.StageDone {
		return taskState, app.NewError(app.CategoryValidation, "task_done", "done task is terminal; no work memory mutations allowed", nil)
	}
	return taskState, nil
}

func (rt *runtime) workApplyContext() (taskID string, blockCode string, blockMessage string, err error) {
	taskState, err := rt.Tasks.Current()
	if err != nil {
		appErr := app.AsError(err)
		if appErr.Category == app.CategoryValidation && appErr.Code == "missing_current_task" {
			return "", "missing_current_task", "work memory requires active task", nil
		}
		return "", "", "", err
	}
	if taskState.Status == app.TaskStatusPaused {
		return "", "task_paused", "resume task before applying memory proposal", nil
	}
	if taskState.Stage == app.StageDone {
		return "", "task_done", "done task is terminal; no work memory mutations allowed", nil
	}
	return taskState.ID, "", "", nil
}

func (rt *runtime) profileByID(id string) (app.UserProfile, error) {
	profile, err := rt.Profiles.Get(id)
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

func (rt *runtime) ensureProvider() providers.LLMProvider {
	if rt.Provider == nil {
		rt.Provider = chooseProvider(rt.Config)
	}
	return rt.Provider
}

func (rt *runtime) ensureClassifier() *memory.Classifier {
	if rt.Classifier == nil {
		rt.Classifier = memory.NewClassifier(rt.ensureProvider())
	}
	return rt.Classifier
}

func (rt *runtime) ensureBuilder() *prompting.Builder {
	if rt.Builder == nil {
		rt.Builder = prompting.NewBuilder()
	}
	return rt.Builder
}

func (rt *runtime) ensureProcessController() *process.ProcessController {
	if rt.PolicyRegistry == nil {
		rt.PolicyRegistry = process.NewStagePolicyRegistry()
	}
	if rt.TransitionGate == nil {
		rt.TransitionGate = &process.TransitionGate{Tasks: rt.Tasks}
	}
	if rt.RetryController == nil {
		rt.RetryController = process.NewRetryController()
	}
	if rt.AuditStore == nil {
		rt.AuditStore = process.NewAuditStore(rt.StorageDir)
	}
	if rt.Process == nil {
		rt.Process = &process.ProcessController{}
	}
	rt.Process.Tasks = rt.Tasks
	rt.Process.Profiles = rt.Profiles
	rt.Process.ActiveProfileID = rt.Config.ActiveProfileID
	rt.Process.Memory = rt.Memory
	rt.Process.Invariants = rt.Invariants
	rt.Process.Proposals = rt.Proposals
	rt.Process.Model = rt.Config.ActiveModel
	rt.Process.MemoryModel = rt.Config.MemoryModel
	rt.Process.Builder = rt.ensureBuilder()
	rt.Process.PolicyRegistry = rt.PolicyRegistry
	rt.Process.TransitionGate = rt.TransitionGate
	rt.Process.LifecycleGate = &process.LifecycleGate{Tasks: rt.Tasks}
	rt.Process.RetryController = rt.RetryController
	rt.Process.AuditStore = rt.AuditStore
	return rt.Process
}

func (rt *runtime) preflightProcess(ctx context.Context, input process.ExchangeInput) error {
	return rt.ensureProcessController().PreflightContext(ctx, input)
}

func (rt *runtime) attachProviderToProcess() *process.ProcessController {
	pc := rt.ensureProcessController()
	provider := rt.ensureProvider()
	pc.Provider = provider
	pc.Classifier = rt.ensureClassifier()
	if semanticValidationEnabled(provider) {
		pc.PromptImprover = &process.PromptImprover{Provider: provider, Model: rt.Config.ActiveModel}
		pc.AgentRunner = &process.AgentRunner{Provider: provider, Model: rt.Config.ActiveModel, Factory: process.NewStagePromptFactory(rt.PolicyRegistry)}
		pc.PlanningSwarm = &process.PlanningSwarm{Runner: pc.AgentRunner}
	} else {
		pc.PromptImprover = nil
		pc.AgentRunner = nil
		pc.PlanningSwarm = nil
	}
	if semanticValidationEnabled(provider) {
		model := rt.Config.MemoryModel
		if model == "" {
			model = rt.Config.ActiveModel
		}
		pc.SemanticValidator = process.NewSemanticValidator(provider, model)
		pc.InvariantValidator = process.NewSemanticValidator(provider, model)
	} else {
		pc.SemanticValidator = nil
		pc.InvariantValidator = nil
	}
	return pc
}

func applyTaskMove(rt *runtime, stage app.TaskStage) (app.TaskState, error) {
	current, err := rt.Tasks.Current()
	if err != nil {
		return app.TaskState{}, err
	}
	gate := &process.LifecycleGate{Tasks: rt.Tasks}
	var signal process.TransitionSignal
	switch {
	case current.Stage == app.StagePlanning && stage == app.StageExecution:
		signal = process.SignalApprovePlanning
	case current.Stage == app.StageExecution && stage == app.StageValidation:
		signal = process.SignalReadyForValidation
	case current.Stage == app.StageExecution && stage == app.StagePlanning:
		signal = process.SignalPlanningRequired
	case current.Stage == app.StageValidation && stage == app.StageExecution:
		signal = process.SignalNeedsExecutionFixes
	case current.Stage == app.StageValidation && stage == app.StageDone:
		signal = process.SignalReadyForDone
	default:
		return app.TaskState{}, app.NewError(app.CategoryValidation, "forbidden_transition", "unsupported task move command", nil)
	}
	res, err := gate.Apply(process.LifecycleTransitionRequest{
		State:           current,
		Source:          process.TransitionSourceRecoveryDebug,
		Signal:          signal,
		RecoveryDebug:   true,
		TrustedEvidence: current.ValidationEvidence,
		Reason:          current.LastSessionID,
	})
	if err != nil {
		return app.TaskState{}, err
	}
	return res.State, nil
}

func chooseProvider(cfg app.AppConfig) providers.LLMProvider {
	if os.Getenv("ASSISTANT_PROVIDER") == "fake" || os.Getenv("ASSISTANT_FAKE_PROVIDER") == "1" {
		fake := providers.NewFakeProvider()
		if value := os.Getenv("ASSISTANT_FAKE_CLASSIFIER_RESPONSE"); value != "" {
			fake.ClassifierResponse = value
		}
		return fake
	}
	return providers.NewOpenRouterProvider(cfg.OpenRouterBaseURL)
}

func semanticValidationEnabled(provider providers.LLMProvider) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ASSISTANT_LLM_VALIDATION"))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes", "always":
		return true
	}
	_, fake := provider.(*providers.FakeProvider)
	return !fake
}

func initCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize local assistant storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if rt.Config.ActiveModel == "" {
				return app.ErrorWithHint(app.CategoryProvider, "missing_model", "active model is required", "run assistant init --model <provider/model>", nil)
			}
			if err := validateModelSyntax(rt.Config.ActiveModel); err != nil {
				return err
			}
			if err := validateModelSyntax(rt.Config.MemoryModel); err != nil {
				return err
			}
			if err := rt.ConfigMgr.Save(rt.Config); err != nil {
				return err
			}
			if err := rt.Profiles.EnsureDefaults(); err != nil {
				return err
			}
			if err := rt.Invariants.EnsureDefaults(); err != nil {
				return err
			}
			profilesList, err := rt.Profiles.List()
			if err != nil {
				return err
			}
			invariantsList, err := rt.Invariants.List(cmd.Context())
			if err != nil {
				return err
			}
			out := map[string]any{"ok": true, "storage_dir": rt.StorageDir, "config": rt.Config, "profiles": profilesList, "invariants": invariantsList}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, out, fmt.Sprintf("initialized %s\n", rt.StorageDir))
		},
	}
}

type chatOptions struct {
	Once         bool
	Input        string
	RenderPrompt bool
	Verify       string
	TUI          bool
	Plain        bool
}

func chatCommand(opts *globalOptions) *cobra.Command {
	chatOpts := &chatOptions{}
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start chat loop or run one request",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChatMode(cmd, opts, chatOpts, false)
		},
	}
	cmd.Flags().BoolVar(&chatOpts.Once, "once", false, "run one request")
	cmd.Flags().StringVar(&chatOpts.Input, "input", "", "input text for --once")
	cmd.Flags().BoolVar(&chatOpts.RenderPrompt, "render-prompt", false, "render prompt without provider call")
	cmd.Flags().StringVar(&chatOpts.Verify, "verify", "", "run verification command and pass trusted evidence to validation")
	cmd.Flags().BoolVar(&chatOpts.TUI, "tui", false, "run TUI")
	cmd.Flags().BoolVar(&chatOpts.Plain, "plain", false, "run plain REPL fallback")
	return cmd
}

func runTopLevelChat(cmd *cobra.Command, opts *globalOptions, chatOpts *chatOptions) error {
	return runChatMode(cmd, opts, chatOpts, true)
}

func runChatMode(cmd *cobra.Command, opts *globalOptions, chatOpts *chatOptions, productMode bool) error {
	if opts.JSON && !chatOpts.Once {
		return app.ErrorWithHint(app.CategoryCLI, "json_repl_unsupported", "chat --json requires --once", "use --once for single-request JSON output", nil)
	}
	if strings.TrimSpace(chatOpts.Verify) != "" && !chatOpts.Once {
		return app.ErrorWithHint(app.CategoryCLI, "verify_requires_once", "chat --verify requires --once", "use --once --verify <command> --input <text>", nil)
	}
	if chatOpts.TUI && !isInteractiveReader(cmd.InOrStdin()) {
		return app.ErrorWithHint(app.CategoryCLI, "tui_requires_terminal", "TUI requires an interactive terminal", "use --plain", nil)
	}
	rt, err := newRuntime(cmd.Context(), opts)
	if err != nil {
		return err
	}
	if chatOpts.Once {
		return runChatOnce(cmd, opts, chatOpts, rt)
	}
	if chatOpts.Plain || !productMode && !chatOpts.TUI {
		return runREPL(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), rt)
	}
	if !isInteractiveReader(cmd.InOrStdin()) {
		if chatOpts.TUI {
			return app.ErrorWithHint(app.CategoryCLI, "tui_requires_terminal", "TUI requires an interactive terminal", "use --plain", nil)
		}
		return runREPL(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), rt)
	}
	return tui.Run(cmd.Context(), newChatBackendFromRuntime(rt), cmd.InOrStdin(), cmd.OutOrStdout())
}

func runChatOnce(cmd *cobra.Command, opts *globalOptions, chatOpts *chatOptions, rt *runtime) error {
	if strings.TrimSpace(chatOpts.Input) == "" {
		return app.NewError(app.CategoryCLI, "missing_input", "--input is required with --once", nil)
	}
	sessionID := app.NewID("session")
	if !chatOpts.RenderPrompt {
		if err := rt.preflightProcess(cmd.Context(), process.ExchangeInput{SessionID: sessionID, Input: chatOpts.Input, RenderOnly: chatOpts.RenderPrompt}); err != nil {
			return err
		}
		rt.ensureProvider()
		ensureProviderDisclosure(cmd.ErrOrStderr(), rt)
	}
	stopProgress := startAPIProgress(cmd.ErrOrStderr(), !rt.Quiet && !opts.JSON && !chatOpts.RenderPrompt)
	result, err := runChatExchange(cmd.Context(), rt, sessionID, chatOpts.Input, chatOpts.RenderPrompt, false, chatOpts.Verify)
	stopProgress()
	if err != nil {
		return err
	}
	if err := recordRenderedPrompt(rt.StorageDir, result.SessionID, result.RenderedPromptID, result.Messages, result.RenderedPrompt); err != nil {
		result.Warnings = append(result.Warnings, "prompt audit skipped: "+app.AsError(err).Code)
	}
	if !chatOpts.RenderPrompt {
		result.RenderedPrompt = ""
		result.Messages = nil
	}
	if !rt.Quiet && opts.JSON {
		for _, warning := range result.Warnings {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}
	}
	return writeOutput(cmd.OutOrStdout(), opts.JSON, result, renderChatResult(result, chatRenderOptions{Color: terminalColorEnabled(cmd.OutOrStdout())}))
}

type chatResult struct {
	OK               bool                        `json:"ok"`
	SessionID        string                      `json:"session_id"`
	Answer           string                      `json:"answer,omitempty"`
	Model            string                      `json:"model,omitempty"`
	RenderedPromptID string                      `json:"rendered_prompt_id,omitempty"`
	RenderedPrompt   string                      `json:"rendered_prompt,omitempty"`
	Messages         []app.ChatMessage           `json:"messages,omitempty"`
	Proposal         *app.MemoryProposal         `json:"proposal,omitempty"`
	Transition       *process.TransitionResult   `json:"transition,omitempty"`
	AppliedArtifacts []string                    `json:"applied_artifacts,omitempty"`
	Warnings         []string                    `json:"warnings,omitempty"`
	Task             *app.TaskState              `json:"-"`
	AuditEvents      []process.ProcessAuditEvent `json:"-"`
}

func runChatExchange(ctx context.Context, rt *runtime, sessionID, input string, renderOnly bool, requireMemoryProposal bool, verifyCommand string) (chatResult, error) {
	if !renderOnly {
		var result chatResult
		err := withChatTurnLock(rt, sessionID, func() error {
			var runErr error
			result, runErr = runChatExchangeLocked(ctx, rt, sessionID, input, renderOnly, requireMemoryProposal, verifyCommand)
			return runErr
		})
		return result, err
	}
	return runChatExchangeLocked(ctx, rt, sessionID, input, renderOnly, requireMemoryProposal, verifyCommand)
}

func runChatExchangeLocked(ctx context.Context, rt *runtime, sessionID, input string, renderOnly bool, requireMemoryProposal bool, verifyCommand string) (chatResult, error) {
	if err := rt.preflightProcess(ctx, process.ExchangeInput{SessionID: sessionID, Input: input, RenderOnly: renderOnly}); err != nil {
		return chatResult{OK: false, SessionID: sessionID, Model: rt.Config.ActiveModel}, err
	}
	if !renderOnly {
		if rt.Config.ActiveModel == "" {
			return chatResult{OK: false, SessionID: sessionID, Model: rt.Config.ActiveModel}, app.NewError(app.CategoryProvider, "missing_model", "active model is required", nil)
		}
		if err := validateModelID(ctx, rt.ensureProvider(), rt.Config.ActiveModel); err != nil {
			return chatResult{OK: false, SessionID: sessionID, Model: rt.Config.ActiveModel}, err
		}
	}
	pc := rt.ensureProcessController()
	if !renderOnly {
		pc = rt.attachProviderToProcess()
	}
	currentTask, _ := rt.Tasks.Current()
	autoVerifyCommand := ""
	if strings.TrimSpace(verifyCommand) == "" {
		var autoErr error
		autoVerifyCommand, autoErr = autoTrustedVerificationCommand(ctx, rt, sessionID, input, currentTask, renderOnly, pc.SemanticValidator)
		if autoErr != nil {
			return chatResult{OK: false, SessionID: sessionID, Model: rt.Config.ActiveModel}, autoErr
		}
		verifyCommand = autoVerifyCommand
	}
	trustedEvidence, err := runTrustedVerification(ctx, rt.StorageDir, currentTask.ID, sessionID, verifyCommand, renderOnly)
	if err != nil {
		return chatResult{OK: false, SessionID: sessionID, Model: rt.Config.ActiveModel}, err
	}
	procResult, err := pc.RunExchange(ctx, process.ExchangeInput{SessionID: sessionID, Input: input, RenderOnly: renderOnly, RequireMemoryProposal: requireMemoryProposal, TrustedEvidence: trustedEvidence})
	result := chatResult{OK: true, SessionID: sessionID, Model: rt.Config.ActiveModel, RenderedPromptID: app.NewID("prompt")}
	if procResult != nil {
		result.Answer = procResult.Answer
		result.Model = procResult.Model
		if result.Model == "" {
			result.Model = rt.Config.ActiveModel
		}
		result.RenderedPrompt = procResult.RenderedPrompt
		result.Messages = procResult.Messages
		result.Proposal = procResult.Proposal
		result.Transition = procResult.Transition
		result.Warnings = procResult.Warnings
	}
	if materialized, matErr := materializeExecutionDeliverable(procResult, currentTask); matErr != nil {
		result.Warnings = append(result.Warnings, "artifact materialization skipped: "+app.AsError(matErr).Code)
	} else {
		result.AppliedArtifacts = append(result.AppliedArtifacts, materialized...)
	}
	if autoVerifyCommand != "" {
		result.Warnings = append(result.Warnings, "auto verification: "+autoVerifyCommand)
	}
	if err != nil {
		result.OK = false
		if task, taskErr := rt.Tasks.Current(); taskErr == nil {
			result.Task = &task
		}
		return result, err
	}
	if postResult, postCommand, postErr := runPostApprovalTrustedVerification(ctx, rt, pc, sessionID, renderOnly, procResult); postErr != nil {
		result.OK = false
		if task, taskErr := rt.Tasks.Current(); taskErr == nil {
			result.Task = &task
		}
		return result, postErr
	} else if postResult != nil {
		if strings.TrimSpace(postResult.Answer) != "" {
			result.Answer = postResult.Answer
		}
		if postResult.Model != "" {
			result.Model = postResult.Model
		}
		result.Transition = postResult.Transition
		if postResult.Transition != nil {
			result.Warnings = dropExecutionAutoContinueWarnings(result.Warnings)
		}
		result.Warnings = append(result.Warnings, postResult.Warnings...)
		if postCommand != "" {
			result.Warnings = append(result.Warnings, "auto verification: "+postCommand)
		}
	}
	if task, taskErr := rt.Tasks.Current(); taskErr == nil {
		result.Task = &task
	}
	result.AuditEvents = chatAuditEvents(rt.StorageDir, sessionID, result.Task)
	return result, nil
}

func withChatTurnLock(rt *runtime, sessionID string, fn func() error) error {
	if rt == nil || strings.TrimSpace(rt.StorageDir) == "" {
		return fn()
	}
	key := "session_" + sessionID
	if task, err := rt.Tasks.Current(); err == nil && strings.TrimSpace(task.ID) != "" {
		key = "task_" + task.ID
	}
	if err := storage.ValidateID(key); err != nil {
		return app.NewError(app.CategoryValidation, "unsafe_turn_lock_id", "unsafe chat turn lock id", err)
	}
	path, err := storage.SafeJoin(rt.StorageDir, "turns", key+".lock")
	if err != nil {
		return app.NewError(app.CategoryValidation, "unsafe_turn_lock_path", "unsafe chat turn lock path", err)
	}
	err = storage.WithFileLock(path, true, fn)
	if err == nil {
		return nil
	}
	var storageErr *storage.Error
	if errors.As(err, &storageErr) && storageErr.Code == "lock_timeout" {
		return app.ErrorWithHint(app.CategoryCLI, "turn_in_progress", "another chat turn is already updating this task; wait for it to finish and retry", "the application serializes state-mutating chat turns per task/session", err)
	}
	return err
}

func dropExecutionAutoContinueWarnings(warnings []string) []string {
	out := warnings[:0]
	for _, warning := range warnings {
		if strings.HasPrefix(warning, "execution auto-continue stopped:") || strings.HasPrefix(warning, "execution continuation skipped:") {
			continue
		}
		out = append(out, warning)
	}
	return out
}

func chatAuditEvents(storageDir, sessionID string, task *app.TaskState) []process.ProcessAuditEvent {
	if strings.TrimSpace(storageDir) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	events, err := process.NewAuditStore(storageDir).Latest(80)
	if err != nil {
		return nil
	}
	taskID := taskID(task)
	out := []process.ProcessAuditEvent{}
	for _, event := range events {
		if event.SessionID != sessionID {
			continue
		}
		if taskID != "" && event.TaskID != "" && event.TaskID != taskID {
			continue
		}
		out = append(out, event)
	}
	return out
}

func materializeExecutionDeliverable(procResult *process.ExchangeResult, task app.TaskState) ([]string, error) {
	if procResult == nil || strings.TrimSpace(procResult.Answer) == "" || task.ID == "" {
		return nil, nil
	}
	blocks := []deliverableFileBlock{}
	for _, answer := range splitExecutionAnswers(procResult.Answer) {
		parsed, err := process.Parse(app.StageExecution, process.ActionExecutePlanStep, answer)
		if err != nil || parsed.Execution == nil {
			continue
		}
		blocks = append(blocks, extractDeliverableFileBlocks(parsed.Execution.Deliverable, task)...)
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, app.NewError(app.CategoryCLI, "working_dir_unavailable", "could not resolve working directory", err)
	}
	written := []string{}
	seenWritten := map[string]bool{}
	for _, block := range blocks {
		target, err := safeWorkspacePath(cwd, block.Path)
		if err != nil {
			return written, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return written, app.NewError(app.CategoryCLI, "artifact_dir_create_failed", "could not create artifact directory", err)
		}
		content := block.Content
		if filepath.Ext(target) == ".go" {
			content = normalizeGoPackageForDirectory(filepath.Dir(target), content)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return written, app.NewError(app.CategoryCLI, "artifact_write_failed", "could not write execution artifact", err)
		}
		rel, _ := filepath.Rel(cwd, target)
		relSlash := filepath.ToSlash(rel)
		if !seenWritten[relSlash] {
			seenWritten[relSlash] = true
			written = append(written, relSlash)
		}
	}
	return written, nil
}

func normalizeGoPackageForDirectory(dir, content string) string {
	existing := existingGoPackageName(dir)
	if existing == "" {
		return content
	}
	re := regexp.MustCompile(`(?m)^package\s+[A-Za-z_][A-Za-z0-9_]*\s*$`)
	return re.ReplaceAllString(content, "package "+existing)
}

func existingGoPackageName(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?m)^package\s+([A-Za-z_][A-Za-z0-9_]*)\s*$`)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		match := re.FindSubmatch(data)
		if len(match) == 2 {
			return string(match[1])
		}
	}
	return ""
}

func splitExecutionAnswers(answer string) []string {
	parts := strings.Split(answer, "\n\n")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 && strings.TrimSpace(answer) != "" {
		out = append(out, strings.TrimSpace(answer))
	}
	return out
}

type deliverableFileBlock struct {
	Path    string
	Content string
}

func extractDeliverableFileBlocks(deliverable string, task app.TaskState) []deliverableFileBlock {
	baseDir := inferSingleTaskDirectory(task)
	re := regexp.MustCompile(`(?ms)^#{2,6}\s+([^\n]+?)\s*\n` + "```[A-Za-z0-9_-]*\n(.*?)```")
	matches := re.FindAllStringSubmatch(deliverable, -1)
	blocks := []deliverableFileBlock{}
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		path := normalizeDeliverablePath(match[1], baseDir)
		if path == "" {
			continue
		}
		content := strings.TrimRight(match[2], "\n") + "\n"
		blocks = append(blocks, deliverableFileBlock{Path: path, Content: content})
	}
	return blocks
}

func normalizeDeliverablePath(heading, baseDir string) string {
	path := strings.TrimSpace(heading)
	path = strings.Trim(path, "`'\" ")
	fields := strings.Fields(path)
	if len(fields) > 0 {
		path = fields[len(fields)-1]
	}
	path = strings.Trim(path, "`'\" :")
	path = filepath.ToSlash(path)
	if path == "" {
		return ""
	}
	if !strings.Contains(path, "/") && baseDir != "" {
		path = strings.TrimRight(baseDir, "/") + "/" + path
	}
	return path
}

func inferSingleTaskDirectory(task app.TaskState) string {
	text := strings.Join(append(append([]string{task.Objective}, task.AcceptanceCriteria...), task.Plan...), "\n")
	re := regexp.MustCompile(`(?:^|[\s'"` + "`" + `])((?:[A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+)(?:[\s'"` + "`" + `.,:]|$)`)
	seen := map[string]bool{}
	var only string
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		if len(match) != 2 {
			continue
		}
		candidate := strings.Trim(match[1], "/.,:;")
		if candidate == "" || strings.Contains(candidate, "..") || strings.Contains(filepath.Base(candidate), ".") {
			continue
		}
		if !seen[candidate] {
			seen[candidate] = true
			if only != "" && only != candidate {
				return ""
			}
			only = candidate
		}
	}
	return only
}

func safeWorkspacePath(cwd, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" || filepath.IsAbs(rel) {
		return "", app.NewError(app.CategoryValidation, "unsafe_artifact_path", "artifact path must be relative", nil)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", app.NewError(app.CategoryValidation, "unsafe_artifact_path", "artifact path escapes workspace", nil)
	}
	target := filepath.Join(cwd, clean)
	relToCWD, err := filepath.Rel(cwd, target)
	if err != nil || strings.HasPrefix(relToCWD, ".."+string(filepath.Separator)) || relToCWD == ".." {
		return "", app.NewError(app.CategoryValidation, "unsafe_artifact_path", "artifact path escapes workspace", err)
	}
	return target, nil
}

func runPostApprovalTrustedVerification(ctx context.Context, rt *runtime, pc *process.ProcessController, sessionID string, renderOnly bool, procResult *process.ExchangeResult) (*process.ExchangeResult, string, error) {
	if renderOnly || rt == nil || pc == nil || procResult == nil || procResult.Transition == nil {
		return nil, "", nil
	}
	if !procResult.Transition.Moved || procResult.Transition.From != app.StagePlanning || procResult.Transition.To != app.StageExecution {
		return nil, "", nil
	}
	task, err := rt.Tasks.Current()
	if err != nil {
		return nil, "", err
	}
	command, err := resolveTrustedVerificationCommand(ctx, rt, task)
	if err != nil {
		return nil, "", err
	}
	if command == "" {
		return nil, "", nil
	}
	evidence, err := runTrustedVerification(ctx, rt.StorageDir, task.ID, sessionID, command, false)
	if err != nil {
		if app.AsError(err).Code == "verification_failed" {
			return &process.ExchangeResult{Warnings: []string{"post-approval verification skipped: " + app.AsError(err).Code}}, command, nil
		}
		return nil, command, err
	}
	follow, err := pc.RunExchange(ctx, process.ExchangeInput{
		SessionID:             sessionID,
		Input:                 "Application-issued trusted verification evidence is available for the approved plan. Move to validation if the evidence satisfies execution readiness.",
		ActionKind:            process.ActionSummarizeExecution,
		TrustedEvidence:       evidence,
		RequireMemoryProposal: false,
		SkipSemanticIntent:    true,
		SkipPromptImprovement: true,
		SkipInputInvariants:   true,
	})
	if err != nil {
		return nil, command, err
	}
	return follow, command, nil
}

func autoTrustedVerificationCommand(ctx context.Context, rt *runtime, sessionID, input string, task app.TaskState, renderOnly bool, semanticValidator *process.SemanticValidator) (string, error) {
	if renderOnly || strings.TrimSpace(task.ID) == "" || task.Status == app.TaskStatusPaused || task.Stage == app.StageDone {
		return "", nil
	}
	if task.Stage == app.StageValidation && task.ValidationStatus == "ready_for_done" && len(task.ValidationEvidence) > 0 {
		return "", nil
	}
	if semanticValidator != nil {
		intent, err := semanticValidator.ResolveIntent(ctx, process.SemanticIntentInput{
			SessionID:      sessionID,
			UserInput:      input,
			Stage:          task.Stage,
			ExpectedAction: task.ExpectedAction,
			ActionKind:     process.ResolveActionKind(input, task.Stage, task.ExpectedAction),
			Task:           &task,
		})
		if err != nil {
			return "", err
		}
		if !semanticAutoVerificationIntent(intent, task.Stage) {
			return "", nil
		}
		return resolveTrustedVerificationCommand(ctx, rt, task)
	}
	return "", nil
}

func semanticAutoVerificationIntent(intent process.SemanticIntentResult, stage app.TaskStage) bool {
	if intent.Confidence < 0.65 {
		return false
	}
	switch stage {
	case app.StageExecution:
		return intent.TransitionSignal == "ready_for_validation" || intent.ActionKind == process.ActionSummarizeExecution
	case app.StageValidation:
		return intent.TransitionSignal == "ready_for_done"
	default:
		return false
	}
}

func resolveTrustedVerificationCommand(ctx context.Context, rt *runtime, task app.TaskState) (string, error) {
	if command := explicitTrustedVerificationCommand(task); command != "" {
		return command, nil
	}
	return planTrustedVerificationCommand(ctx, rt, task)
}

type verificationCommandPlan struct {
	Command    string  `json:"command"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func planTrustedVerificationCommand(ctx context.Context, rt *runtime, task app.TaskState) (string, error) {
	if rt == nil || strings.TrimSpace(task.ID) == "" || strings.TrimSpace(rt.Config.ActiveModel) == "" {
		return "", nil
	}
	payload, err := verificationPlannerPayload(task)
	if err != nil {
		return "", err
	}
	temp := 0.0
	res, err := rt.ensureProvider().Complete(ctx, providers.CompletionRequest{
		Purpose:     providers.PurposeValidator,
		Model:       rt.Config.ActiveModel,
		JSONMode:    true,
		Temperature: &temp,
		Messages: []app.ChatMessage{
			{ID: app.NewID("msg"), Role: app.RoleSystem, Content: verificationPlannerSystemPrompt()},
			{ID: app.NewID("msg"), Role: app.RoleUser, Content: payload},
		},
	})
	if err != nil {
		return "", err
	}
	plan, err := decodeVerificationCommandPlan(res.Message.Content)
	if err != nil {
		return "", nil
	}
	command := strings.TrimSpace(plan.Command)
	if command == "" || plan.Confidence < 0.65 {
		return "", nil
	}
	tokens, err := parseTrustedVerificationCommand(command)
	if err != nil || !trustedVerificationCandidateUsable(tokens) {
		return "", nil
	}
	tokens = normalizeTrustedVerificationTokens(tokens)
	if !trustedVerificationCandidateUsable(tokens) {
		return "", nil
	}
	return strings.Join(tokens, " "), nil
}

func verificationPlannerPayload(task app.TaskState) (string, error) {
	payload := map[string]any{
		"task": map[string]any{
			"id":                  task.ID,
			"title":               task.Title,
			"objective":           task.Objective,
			"stage":               task.Stage,
			"current_step":        task.CurrentStep,
			"completed_steps":     task.CompletedSteps,
			"acceptance_criteria": task.AcceptanceCriteria,
			"plan":                task.Plan,
			"microtasks":          task.Microtasks,
			"validation_evidence": task.ValidationEvidence,
			"history_log":         task.HistoryLog,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", app.NewError(app.CategoryInternal, "verification_planner_payload", err.Error(), err)
	}
	redacted, _ := validation.RedactText(string(data))
	if validation.HasSecret(redacted) {
		return "", app.NewError(app.CategoryValidation, "secret_blocked", "secret-like verification planner payload cannot be sent to provider", nil)
	}
	return redacted, nil
}

func verificationPlannerSystemPrompt() string {
	return `You are an internal verification command planner for a terminal-first coding agent.
Return strict JSON only with exactly these keys: command, confidence, reason.
command must be one exact argv-only command, not prose, not markdown, not a shell script.
Choose a command only when task state semantically justifies a concrete verification action.
Do not use language-specific path heuristics such as "directory path means go test".
If no safe exact command is justified, return {"command":"","confidence":0,"reason":"no safe exact verification command"}.
Allowed command families after local policy validation: go test/vet/version/build, git diff/status, npm/pnpm/yarn test or run test*, pytest, python -m pytest, cargo/dotnet/mvn/make test.
Never include shell operators, environment expansion, redirection, absolute paths, parent paths, or secrets.`
}

func decodeVerificationCommandPlan(raw string) (verificationCommandPlan, error) {
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	dec.DisallowUnknownFields()
	var out verificationCommandPlan
	if err := dec.Decode(&out); err != nil {
		return verificationCommandPlan{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return verificationCommandPlan{}, app.NewError(app.CategoryValidation, "invalid_json", "verification planner returned multiple JSON documents", err)
	}
	if out.Command == "" && out.Confidence > 0 {
		return verificationCommandPlan{}, app.NewError(app.CategoryValidation, "invalid_json", "verification planner confidence requires command", nil)
	}
	if out.Confidence < 0 || out.Confidence > 1 {
		return verificationCommandPlan{}, app.NewError(app.CategoryValidation, "invalid_json", "verification planner confidence must be between 0 and 1", nil)
	}
	return out, nil
}

func explicitTrustedVerificationCommand(task app.TaskState) string {
	candidates := []string{}
	candidates = append(candidates, task.AcceptanceCriteria...)
	candidates = append(candidates, task.Plan...)
	candidates = append(candidates, task.CurrentStep, task.Objective)
	for _, microtask := range task.Microtasks {
		candidates = append(candidates, microtask.PlanItem)
	}
	for _, text := range candidates {
		for _, command := range trustedVerificationCommandCandidates(text) {
			if tokens, err := parseTrustedVerificationCommand(command); err == nil {
				tokens = normalizeTrustedVerificationTokens(tokens)
				if trustedVerificationCandidateUsable(tokens) {
					return strings.Join(tokens, " ")
				}
			}
		}
	}
	return ""
}

var (
	quotedVerificationCommandPattern = regexp.MustCompile("[`\"']([^`\"']+)[`\"']")
	verificationPrefixPattern        = regexp.MustCompile(`(?i)\b(go)\s+(test|vet|version|build)\b|\b(git)\s+(diff|status)\b|\b(npm|pnpm)\s+(test|run)\b|\b(yarn)\s+(test|run)\b|\b(pytest)\b|\b(python3?|py)\s+-m\s+pytest\b|\b(cargo|dotnet|mvn|make)\s+test\b`)
)

func trustedVerificationCommandCandidates(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	out := []string{}
	for _, match := range quotedVerificationCommandPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			out = append(out, strings.TrimSpace(match[1]))
		}
	}
	prefixMatches := verificationPrefixPattern.FindAllStringIndex(text, -1)
	for _, loc := range prefixMatches {
		out = append(out, scanVerificationCommand(text[loc[0]:]))
	}
	return out
}

func trustedVerificationCandidateUsable(tokens []string) bool {
	baseArgCount := trustedVerificationBaseArgCount(tokens)
	if baseArgCount == 0 || len(tokens) < baseArgCount {
		return false
	}
	extras := tokens[baseArgCount:]
	if tokens[0] == "go" && (tokens[1] == "test" || tokens[1] == "vet" || tokens[1] == "build") && !hasVerificationPackageTarget(extras) {
		return false
	}
	allowFlagValue := false
	for _, arg := range extras {
		if allowFlagValue && verificationFlagValueUsable(arg) {
			allowFlagValue = false
			continue
		}
		allowFlagValue = false
		if verificationExtraArgUsable(arg) {
			allowFlagValue = verificationFlagMayTakeValue(arg)
			continue
		}
		return false
	}
	return true
}

func normalizeTrustedVerificationTokens(tokens []string) []string {
	out := append([]string(nil), tokens...)
	if len(out) < 3 || out[0] != "go" {
		return out
	}
	switch out[1] {
	case "test", "vet", "build":
	default:
		return out
	}
	skipFlagValue := false
	for i := 2; i < len(out); i++ {
		arg := out[i]
		if skipFlagValue {
			skipFlagValue = false
			continue
		}
		if strings.HasPrefix(arg, "-") || arg == "--" {
			skipFlagValue = verificationFlagMayTakeValue(arg)
			continue
		}
		if !strings.Contains(arg, "/") {
			continue
		}
		arg = strings.TrimSuffix(arg, "/.")
		arg = strings.TrimRight(arg, "/")
		if !strings.HasPrefix(arg, "./") {
			arg = "./" + arg
		}
		out[i] = arg
	}
	return out
}

func trustedVerificationBaseArgCount(tokens []string) int {
	if !allowedVerificationCommand(tokens) {
		return 0
	}
	switch tokens[0] {
	case "pytest":
		return 1
	case "python", "python3", "py":
		return 3
	case "npm", "pnpm":
		if len(tokens) >= 3 && tokens[1] == "run" {
			return 3
		}
		return 2
	case "yarn":
		if len(tokens) >= 3 && tokens[1] == "run" {
			return 3
		}
		return 2
	default:
		return 2
	}
}

func hasVerificationPackageTarget(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") || arg == "--" {
			continue
		}
		if verificationExtraArgUsable(arg) {
			return true
		}
	}
	return false
}

func verificationExtraArgUsable(arg string) bool {
	if arg == "--" || strings.HasPrefix(arg, "-") {
		return true
	}
	return arg == "." || arg == "./..." || strings.HasPrefix(arg, "./") || strings.Contains(arg, "/")
}

func verificationFlagMayTakeValue(arg string) bool {
	if arg == "--" || !strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
		return false
	}
	switch arg {
	case "-run", "-bench", "-count", "-timeout", "-tags", "-coverprofile", "-k", "-m", "--grep", "--filter", "--testNamePattern":
		return true
	default:
		return false
	}
}

func verificationFlagValueUsable(arg string) bool {
	return arg != "" && !strings.HasPrefix(arg, "-") && isVerificationArgToken(arg)
}

func scanVerificationCommand(text string) string {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return strings.TrimSpace(text)
	}
	tokens := []string{cleanVerificationToken(fields[0]), cleanVerificationToken(fields[1])}
	for _, raw := range fields[2:] {
		token := cleanVerificationToken(raw)
		if token == "" || isVerificationStopWord(token) || !isVerificationArgToken(token) {
			break
		}
		tokens = append(tokens, token)
	}
	return strings.Join(tokens, " ")
}

func cleanVerificationToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "`\"'()[]{}")
	token = strings.TrimRight(token, ",;:")
	if strings.HasSuffix(token, ".") && !strings.HasSuffix(token, "...") {
		token = strings.TrimRight(token, ".")
	}
	return token
}

func isVerificationStopWord(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "pass", "passes", "passed", "complete", "completes", "successfully", "succeeds", "без", "успешно", "проходит", "прошла", "прошли", "должен", "должна", "должно", "and", "or", "then", "to", "as", "using", "via", "when", "if", "after", "before", "как", "через", "если", "для":
		return true
	default:
		return false
	}
}

func isVerificationArgToken(token string) bool {
	if strings.ContainsAny(token, "\x00\n\r;&|<>`$") || strings.Contains(token, "$(") || strings.Contains(token, "${") {
		return false
	}
	if hasParentPathSegment(token) || strings.HasPrefix(token, "/") {
		return false
	}
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '.', '/', '_', '-', '=', ':':
			continue
		default:
			return false
		}
	}
	return true
}

func runREPL(ctx context.Context, in io.Reader, out io.Writer, diag io.Writer, rt *runtime) error {
	sessionID := app.NewID("session")
	scanner := bufio.NewScanner(in)
	failed := false
	interactive := isInteractiveReader(in)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			result, err := handleSlashResult(ctx, diag, rt, sessionID, line)
			if err != nil {
				failed = true
				printError(diag, err, false)
			}
			if strings.TrimSpace(result.Output) != "" {
				_, _ = fmt.Fprintln(out, strings.TrimSpace(result.Output))
			}
			if result.ActiveSessionID != "" {
				sessionID = result.ActiveSessionID
			}
			if result.Done {
				if failed && !interactive {
					return app.NewError(app.CategoryCLI, "batch_failed", "one or more non-interactive REPL commands failed", nil)
				}
				return nil
			}
			continue
		}
		if err := rt.preflightProcess(ctx, process.ExchangeInput{SessionID: sessionID, Input: line}); err != nil {
			failed = true
			printError(diag, err, false)
			continue
		}
		rt.ensureProvider()
		ensureProviderDisclosure(diag, rt)
		stopProgress := startAPIProgress(diag, interactive && !rt.Quiet)
		result, err := runChatExchange(ctx, rt, sessionID, line, false, true, "")
		stopProgress()
		if err != nil {
			failed = true
			printError(diag, err, false)
			continue
		}
		_, _ = fmt.Fprint(out, renderChatResult(result, chatRenderOptions{Color: terminalColorEnabled(out)}))
		if err := recordRenderedPrompt(rt.StorageDir, sessionID, result.RenderedPromptID, result.Messages, result.RenderedPrompt); err != nil && !rt.Quiet {
			_, _ = fmt.Fprintln(diag, "prompt audit skipped: "+app.AsError(err).Code)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if failed && !interactive {
		return app.NewError(app.CategoryCLI, "batch_failed", "one or more non-interactive REPL commands failed", nil)
	}
	return nil
}

type slashContextResult struct {
	Done            bool
	Output          string
	ActiveSessionID string
	ActiveTask      *app.TaskState
	TaskCleared     bool
	ActiveProfile   *app.UserProfile
	ActiveConfig    *app.AppConfig
	Picker          *slashPicker
	PendingBlocked  string
}

type slashPicker struct {
	Kind     string
	Sessions []memory.SessionSummary
	Tasks    []tasks.TaskSummary
	Profiles []app.UserProfile
}

func startAPIProgress(diag io.Writer, enabled bool) func() {
	if !enabled || diag == nil {
		return func() {}
	}
	started := time.Now()
	_, _ = fmt.Fprintln(diag, "[api] запрос к модели...")
	return func() {
		elapsed := time.Since(started).Round(time.Second)
		if elapsed < time.Second {
			elapsed = time.Second
		}
		_, _ = fmt.Fprintf(diag, "[api] ответ получен за %s\n", elapsed)
	}
}

func runTrustedVerification(ctx context.Context, storageDir, taskID, sessionID, command string, renderOnly bool) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" || renderOnly {
		return nil, nil
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, app.NewError(app.CategoryValidation, "missing_task", "trusted verification requires a current task", nil)
	}
	tokens, err := parseTrustedVerificationCommand(command)
	if err != nil {
		return nil, err
	}
	tokens = normalizeTrustedVerificationTokens(tokens)
	verifyCtx, cancel := context.WithTimeout(ctx, trustedVerificationTimeout)
	defer cancel()
	cmd := exec.CommandContext(verifyCtx, tokens[0], tokens[1:]...)
	output := &boundedVerificationOutput{limit: trustedVerificationOutputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	err = cmd.Run()
	if verifyCtx.Err() == context.DeadlineExceeded {
		return nil, app.NewError(app.CategoryValidation, "verification_timeout", "verification command timed out", nil)
	}
	if output.truncated {
		return nil, app.NewError(app.CategoryValidation, "verification_output_too_large", "verification command output exceeded limit", nil)
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, app.NewError(app.CategoryCLI, "verification_failed", "verification command failed to run", err)
		}
	}
	if exitCode != 0 {
		return nil, app.NewError(app.CategoryValidation, "verification_failed", "verification command exited non-zero", nil)
	}
	token, _, err := process.NewTrustedEvidenceStore(storageDir).Issue(taskID, sessionID, strings.Join(tokens, " "), exitCode, output.String())
	if err != nil {
		return nil, err
	}
	return []string{token}, nil
}

type boundedVerificationOutput struct {
	buf       strings.Builder
	limit     int
	truncated bool
}

func (b *boundedVerificationOutput) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
			return len(p), nil
		}
		_, _ = b.buf.Write(p)
		return len(p), nil
	}
	b.truncated = true
	return len(p), nil
}

func (b *boundedVerificationOutput) String() string {
	return b.buf.String()
}

func parseTrustedVerificationCommand(command string) ([]string, error) {
	if len(command) > trustedVerificationCommandMaxLen {
		return nil, app.NewError(app.CategoryValidation, "unsafe_verification_command", "verification command is too long", nil)
	}
	if containsUnsafeVerificationSyntax(command) {
		return nil, app.NewError(app.CategoryValidation, "unsafe_verification_command", "verification command must be argv-only without shell operators or env expansion", nil)
	}
	tokens := splitShellTokens(command)
	if len(tokens) == 0 || strings.TrimSpace(tokens[0]) == "" {
		return nil, app.NewError(app.CategoryCLI, "missing_verification_command", "verification command is required", nil)
	}
	for _, token := range tokens {
		if hasParentPathSegment(token) || strings.HasPrefix(token, "/") || strings.ContainsAny(token, "\x00\n\r") {
			return nil, app.NewError(app.CategoryValidation, "unsafe_verification_command", "verification command contains an unsafe path or control character", nil)
		}
	}
	name := tokens[0]
	if strings.ContainsAny(name, `/\`) {
		return nil, app.NewError(app.CategoryValidation, "unsafe_verification_command", "verification command executable must be resolved from PATH", nil)
	}
	if !allowedVerificationCommand(tokens) {
		return nil, app.ErrorWithHint(app.CategoryValidation, "unsafe_verification_command", "verification command is not in the trusted allowlist", "allowed: go test|go vet|go build|go version; git diff|git status; npm|pnpm|yarn test/run test*; pytest; python -m pytest; cargo|dotnet|mvn|make test", nil)
	}
	return tokens, nil
}

func containsUnsafeVerificationSyntax(command string) bool {
	if strings.ContainsAny(command, "\x00\n\r;&|<>`$") {
		return true
	}
	return strings.Contains(command, "$(") || strings.Contains(command, "${")
}

func allowedVerificationCommand(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	switch tokens[0] {
	case "go":
		if len(tokens) < 2 {
			return false
		}
		switch tokens[1] {
		case "test", "vet", "build", "version":
			return true
		}
	case "git":
		if len(tokens) < 2 {
			return false
		}
		switch tokens[1] {
		case "diff", "status":
			return true
		}
	case "npm", "pnpm":
		if len(tokens) < 2 {
			return false
		}
		if tokens[1] == "test" {
			return true
		}
		return len(tokens) >= 3 && tokens[1] == "run" && isVerificationTestScript(tokens[2])
	case "yarn":
		if len(tokens) < 2 {
			return false
		}
		if isVerificationTestScript(tokens[1]) {
			return true
		}
		return len(tokens) >= 3 && tokens[1] == "run" && isVerificationTestScript(tokens[2])
	case "pytest":
		return true
	case "python", "python3", "py":
		return len(tokens) >= 3 && tokens[1] == "-m" && tokens[2] == "pytest"
	case "cargo", "dotnet", "mvn", "make":
		if len(tokens) < 2 {
			return false
		}
		return tokens[1] == "test"
	}
	return false
}

func isVerificationTestScript(script string) bool {
	script = strings.ToLower(strings.TrimSpace(script))
	return script == "test" || strings.HasPrefix(script, "test:") || strings.HasPrefix(script, "test-") || strings.HasPrefix(script, "test_")
}

func hasParentPathSegment(token string) bool {
	for _, segment := range strings.Split(strings.ReplaceAll(token, `\`, "/"), "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func isInteractiveReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func profilesCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "profiles",
		Aliases: []string{"profile"},
		Short:   "List profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			items, err := rt.Profiles.List()
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "profiles": items}, profileListText(items, rt.Config.ActiveProfileID))
		},
	}
	cmd.AddCommand(profileListCommand(opts), profileShowCommand(opts), profileSetCommand(opts), profileCreateCommand(opts))
	return cmd
}

func profileListCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List profiles", RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := newRuntime(cmd.Context(), opts)
		if err != nil {
			return err
		}
		items, err := rt.Profiles.List()
		if err != nil {
			return err
		}
		return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "profiles": items}, profileListText(items, rt.Config.ActiveProfileID))
	}}
}

func profileShowCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{Use: "show [id]", Short: "Show profile", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := newRuntime(cmd.Context(), opts)
		if err != nil {
			return err
		}
		var profile app.UserProfile
		if len(args) == 0 {
			profile, err = rt.activeProfile()
		} else {
			profile, err = rt.profileByID(args[0])
		}
		if err != nil {
			return err
		}
		return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "profile": profile}, profileText(profile, rt.Config.ActiveProfileID))
	}}
}

func profileSetCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{Use: "set <id>", Short: "Set active profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := newRuntime(cmd.Context(), opts)
		if err != nil {
			return err
		}
		if err := rt.Profiles.EnsureDefaults(); err != nil {
			return err
		}
		profile, err := rt.Profiles.SetActive(args[0])
		if err != nil {
			return err
		}
		return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "profile": profile}, fmt.Sprintf("active profile: %s\n", profile.ID))
	}}
}

func profileCreateCommand(opts *globalOptions) *cobra.Command {
	var displayName string
	var style, responseFormat, constraints []string
	cmd := &cobra.Command{Use: "create <id>", Short: "Create profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := newRuntime(cmd.Context(), opts)
		if err != nil {
			return err
		}
		styleMap, err := parseKeyValues(style)
		if err != nil {
			return err
		}
		formatMap, err := parseKeyValues(responseFormat)
		if err != nil {
			return err
		}
		if len(styleMap) == 0 {
			styleMap = map[string]string{"language": "ru", "tone": "direct"}
		}
		if len(formatMap) == 0 {
			formatMap = map[string]string{"structure": "concise"}
		}
		if len(constraints) == 0 {
			constraints = []string{"follow user preferences"}
		}
		if displayName == "" {
			displayName = args[0]
		}
		now := time.Now().UTC()
		profile := app.UserProfile{ID: args[0], DisplayName: displayName, Style: styleMap, ResponseFormat: formatMap, Constraints: constraints, CreatedAt: now, UpdatedAt: now}
		if err := rt.Profiles.Create(profile); err != nil {
			return err
		}
		return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "profile": profile}, fmt.Sprintf("created profile: %s\n", profile.ID))
	}}
	cmd.Flags().StringVar(&displayName, "display-name", "", "profile display name")
	cmd.Flags().StringArrayVar(&style, "style", nil, "style key=value, repeatable (default: language=ru,tone=direct)")
	cmd.Flags().StringArrayVar(&responseFormat, "format", nil, "response format key=value, repeatable (default: structure=concise)")
	cmd.Flags().StringArrayVar(&constraints, "constraint", nil, "profile constraint, repeatable (default: follow user preferences)")
	return cmd
}

func memoryCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "Inspect/propose/apply memory"}
	cmd.AddCommand(memoryListCommand(opts), memoryProposeCommand(opts), memoryApplyCommand(opts), memoryProposalsCommand(opts))
	return cmd
}

func memoryProposalsCommand(opts *globalOptions) *cobra.Command {
	var sessionFlag string
	cmd := &cobra.Command{
		Use:   "proposals",
		Short: "List pending memory proposals",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			proposals, err := rt.Proposals.List(cmd.Context(), sessionFlag)
			if err != nil {
				return err
			}
			var pending []app.MemoryProposal
			for _, p := range proposals {
				hasPending := false
				for _, r := range p.Records {
					if r.Status == app.ProposalPending {
						hasPending = true
						break
					}
				}
				if hasPending {
					pending = append(pending, p)
				}
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "proposals": pending}, proposalsText(pending))
		},
	}
	cmd.Flags().StringVar(&sessionFlag, "session", "", "filter by session id")
	return cmd
}

func memoryListCommand(opts *globalOptions) *cobra.Command {
	var sessionFlag, taskFlag string
	var allProfiles bool
	cmd := &cobra.Command{
		Use:   "list <short|work|long>",
		Short: "List memory layer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			layer := app.MemoryLayer(args[0])
			taskState, taskErr := rt.Tasks.Current()
			sessionID := sessionFlag
			taskID := taskFlag
			switch layer {
			case app.LayerShort:
				if sessionID == "" {
					sessionID, err = memory.LatestSessionID(rt.StorageDir)
					if err != nil {
						return err
					}
				}
			case app.LayerWork:
				if taskID == "" {
					if taskErr != nil {
						return taskErr
					}
					taskID = taskState.ID
				}
			}
			profileID := ""
			if layer == app.LayerLong && !allProfiles {
				profile, err := rt.activeProfile()
				if err != nil {
					return err
				}
				profileID = profile.ID
			}
			records, err := rt.Memory.List(cmd.Context(), layer, sessionID, taskID, profileID)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "layer": layer, "session_id": sessionID, "task_id": taskID, "records": records}, memoryListHeader(layer, sessionID, taskID)+memoryText(records))
		},
	}
	cmd.Flags().StringVar(&sessionFlag, "session", "", "session id for short layer")
	cmd.Flags().StringVar(&taskFlag, "task", "", "task id for work layer")
	cmd.Flags().BoolVar(&allProfiles, "all-profiles", false, "include all profile-scoped long memory")
	return cmd
}

func memoryProposeCommand(opts *globalOptions) *cobra.Command {
	var latest bool
	cmd := &cobra.Command{
		Use:   "propose",
		Short: "Propose memory from latest exchange",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			sessionID, err := memory.LatestSessionID(rt.StorageDir)
			if err != nil {
				return err
			}
			if proposal, ok, err := rt.Proposals.LatestPending(cmd.Context(), sessionID); err != nil {
				if app.AsError(err).Code != "proposal_read" && app.AsError(err).Code != "session_missing" {
					return err
				}
			} else if ok {
				return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "proposal": proposal, "session_id": sessionID}, proposalText(proposal))
			}
			userRecord, assistantRecord, err := rt.Memory.LatestExchange(cmd.Context(), sessionID)
			if err != nil {
				return err
			}
			profile, err := rt.activeProfile()
			if err != nil {
				return err
			}
			taskState, _ := rt.Tasks.Current()
			var taskPtr *app.TaskState
			if taskState.ID != "" {
				if taskState.Status == app.TaskStatusPaused {
					return app.NewError(app.CategoryValidation, "task_paused", "task is paused; resume before continuing", nil)
				}
				taskPtr = &taskState
			}
			rt.ensureProvider()
			ensureProviderDisclosure(cmd.ErrOrStderr(), rt)
			if rt.Config.MemoryModel != "" {
				if err := validateModelID(cmd.Context(), rt.ensureProvider(), rt.Config.MemoryModel); err != nil {
					return err
				}
			}
			bundle, _ := rt.Memory.SelectForPrompt(cmd.Context(), sessionID, taskID(taskPtr), profile.ID)
			if err := process.NewAuditStore(rt.StorageDir).Save(process.ProcessAuditEvent{SessionID: sessionID, TaskID: taskID(taskPtr), Stage: stageOf(taskPtr), Decision: "memory_proposal_provider_call", Reason: "memory_classifier", Model: rt.Config.MemoryModel, CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
			proposal, err := rt.ensureClassifier().Propose(cmd.Context(), memory.ClassificationInput{SessionID: sessionID, UserMessageID: userRecord.ID, AssistantMessageID: assistantRecord.ID, UserMessage: userRecord.Content, AssistantMessage: assistantRecord.Content, Profile: profile, Task: taskPtr, Model: rt.Config.MemoryModel, ExistingShort: bundle.Short, ExistingWork: bundle.Work, ExistingLong: bundle.Long})
			if err != nil {
				return err
			}
			if err := rt.Proposals.Save(cmd.Context(), proposal); err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "proposal": proposal, "session_id": sessionID}, proposalText(proposal))
		},
	}
	cmd.Flags().BoolVar(&latest, "latest", true, "use latest user/assistant exchange")
	return cmd
}

func memoryApplyCommand(opts *globalOptions) *cobra.Command {
	var proposalID string
	var accept, reject, edit []string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply memory proposal",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if proposalID == "" {
				proposalID = "latest"
			}
			taskID, workBlockedCode, workBlockedMessage, err := rt.workApplyContext()
			if err != nil {
				return err
			}
			profile, err := rt.activeProfile()
			if err != nil {
				return err
			}
			optsApply := memory.ApplyOptions{ProposalID: proposalID, AcceptIDs: map[string]bool{}, RejectIDs: map[string]bool{}, Edits: map[string]memory.ProposalEdit{}, TaskID: taskID, ProfileID: profile.ID, WorkBlockedCode: workBlockedCode, WorkBlockedMessage: workBlockedMessage}
			for _, v := range accept {
				if v == "all" {
					optsApply.AcceptAll = true
				} else {
					optsApply.AcceptIDs[v] = true
				}
			}
			for _, v := range reject {
				if v == "all" {
					optsApply.RejectAll = true
				} else {
					optsApply.RejectIDs[v] = true
				}
			}
			for _, v := range edit {
				id, parsed, err := parseEdit(v)
				if err != nil {
					return err
				}
				optsApply.Edits[id] = parsed
			}
			result, err := rt.Proposals.Apply(cmd.Context(), optsApply)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "result": result}, fmt.Sprintf("saved %d records\n", len(result.SavedRecords)))
		},
	}
	cmd.Flags().StringVar(&proposalID, "proposal", "", "proposal id")
	cmd.Flags().StringArrayVar(&accept, "accept", nil, "accept 'all' or record id, repeatable")
	cmd.Flags().StringArrayVar(&reject, "reject", nil, "reject record id, repeatable")
	cmd.Flags().StringArrayVar(&edit, "edit", nil, "edit record_id:layer=<layer>,content=<text>, repeatable")
	return cmd
}

func invariantsCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "invariants", Short: "Inspect invariant policy"}
	cmd.AddCommand(invariantsListCommand(opts), invariantsAddCommand(opts))
	return cmd
}

func invariantsListCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active invariants",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if err := rt.Invariants.EnsureDefaults(); err != nil {
				return err
			}
			items, err := rt.Invariants.List(cmd.Context())
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "invariants": items}, invariantsText(items))
		},
	}
}

func invariantsAddCommand(opts *globalOptions) *cobra.Command {
	var kind, content, severity string
	var forbid []string
	cmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Add project invariant",
		Long:  "Add a bounded project invariant. Invariant content can be rendered into provider-visible prompts.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			inv, err := rt.Invariants.Add(cmd.Context(), app.Invariant{ID: args[0], Scope: "project", Kind: kind, Content: content, Severity: severity, ForbiddenTerms: forbid, Source: "user"})
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "invariant": inv}, invariantsText([]app.Invariant{inv}))
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "other", "invariant kind")
	cmd.Flags().StringVar(&content, "content", "", "invariant content")
	cmd.Flags().StringVar(&severity, "severity", "block", "block or warn")
	cmd.Flags().StringArrayVar(&forbid, "forbid", nil, "forbidden literal term, repeatable")
	return cmd
}

func taskCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Task state commands", Long: lifecycleHelpText(), RunE: func(cmd *cobra.Command, args []string) error {
		return app.NewError(app.CategoryCLI, "unknown_task_command", "unknown task command", nil)
	}}
	cmd.AddCommand(&cobra.Command{
		Use:   "start <title>",
		Short: "Start current task",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.Start(strings.Join(args, " "))
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show current task",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.Current()
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
	moveCmd := &cobra.Command{
		Use:   "move <stage>",
		Short: "Move current task stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := applyTaskMove(rt, app.TaskStage(args[0]))
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	}
	cmd.AddCommand(moveCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "step <text>",
		Short: "Set current task step",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.SetStep(strings.Join(args, " "))
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "plan <text>",
		Short: "Add current task plan item",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.AddPlanItem(strings.Join(args, " "))
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "criteria <text>",
		Short: "Add current task acceptance criterion",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.AddCriteria(strings.Join(args, " "))
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "expect <action>",
		Short: "Set current task expected action",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.SetExpectedAction(app.ExpectedAction(args[0]))
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pause",
		Short: "Pause current task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.Pause()
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "resume",
		Short: "Resume current task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.Resume()
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
	return cmd
}

func lifecycleHelpText() string {
	return `Task lifecycle:
  planning -> execution -> validation -> done
  execution can return to planning; validation can return to execution.

Rules:
  execution requires approved planning and approved plan items become microtasks.
  validation requires accepted execution or app-issued --verify evidence bound to task/session.
  done requires an accepted validation record with validation_status=ready_for_done.
  lifecycle-changing chat intent is decided by structured semantic validation in provider-backed paths.
  task move is lifecycle-gated recovery/debug; it cannot bypass done validation invariants.
  pause/resume preserves current step, plan, criteria, microtasks, and expected action.
  planning swarm audit appears in process audit with role, microtask_id, rounds, and summary.`
}

func processCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "process", Short: "Inspect process controller audit"}
	cmd.AddCommand(processAuditCommand(opts))
	return cmd
}

func processAuditCommand(opts *globalOptions) *cobra.Command {
	var latest bool
	var limit int
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show process audit events",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if latest {
				limit = 1
			}
			events, err := process.NewAuditStore(rt.StorageDir).Latest(limit)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "events": events}, processAuditText(events))
		},
	}
	cmd.Flags().BoolVar(&latest, "latest", false, "show latest process audit event")
	cmd.Flags().IntVar(&limit, "limit", 20, "number of events to show")
	return cmd
}

func privacyCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "privacy",
		Short: "Show privacy summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			storageDir, err := app.ResolveStorageDir(opts.StorageDir)
			if err != nil {
				return err
			}
			summary := map[string]any{"ok": true, "api_key": "OPENROUTER_API_KEY env-only; never persisted", "provider_data": []string{"rendered prompt", "latest exchange", "classifier payload"}, "storage_dir": storageDir, "prompt_audit": "ASSISTANT_PROMPT_AUDIT stores metadata/hash only; ASSISTANT_RAW_PROMPT_AUDIT stores raw prompts", "raw_transcripts": "not required in P0", "purge": "assistant privacy purge --audit --yes removes prompts.jsonl and audit files"}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, summary, "OPENROUTER_API_KEY env-only; prompts sent to provider; memory/profile/task stored local. ASSISTANT_PROMPT_AUDIT stores metadata/hash only; ASSISTANT_RAW_PROMPT_AUDIT stores raw prompts. Purge with: assistant privacy purge --audit --yes\n")
		},
	}
	cmd.AddCommand(privacyPurgeCommand(opts))
	return cmd
}

func privacyPurgeCommand(opts *globalOptions) *cobra.Command {
	var purgeAudit, purgeTranscripts, yes bool
	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Purge local audit/transcript data",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return app.NewError(app.CategoryCLI, "confirmation_required", "privacy purge requires --yes", nil)
			}
			storageDir, err := app.ResolveStorageDir(opts.StorageDir)
			if err != nil {
				return err
			}
			removed, err := purgePrivacyData(storageDir, purgeAudit, purgeTranscripts)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "removed": removed}, fmt.Sprintf("removed %d files\n", removed))
		},
	}
	cmd.Flags().BoolVar(&purgeAudit, "audit", false, "purge memory proposal audit files")
	cmd.Flags().BoolVar(&purgeTranscripts, "transcripts", false, "purge transcript files")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm purge")
	return cmd
}

func purgePrivacyData(storageDir string, purgeAudit, purgeTranscripts bool) (int, error) {
	if !purgeAudit && !purgeTranscripts {
		return 0, app.NewError(app.CategoryCLI, "missing_purge_target", "choose --audit and/or --transcripts", nil)
	}
	sessionsDir, err := storage.SafeJoin(storageDir, "sessions")
	if err != nil {
		return 0, app.NewError(app.CategoryStorage, "privacy_purge", "unsafe sessions path", err)
	}
	if err := storage.EnsureNoSymlinkParents(sessionsDir); err != nil {
		return 0, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
	}
	if err := storage.RejectSymlinkTarget(sessionsDir); err != nil {
		return 0, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
	}
	removed := 0
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return removed, nil
		}
		return removed, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		if err := storage.ValidateID(sessionID); err != nil {
			return removed, app.NewError(app.CategoryStorage, "privacy_purge", "unsafe session id", err)
		}
		sessionDir, err := storage.SafeJoin(storageDir, "sessions", sessionID)
		if err != nil {
			return removed, app.NewError(app.CategoryStorage, "privacy_purge", "unsafe session path", err)
		}
		if err := storage.EnsureNoSymlinkParents(sessionDir); err != nil {
			return removed, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
		}
		if err := storage.RejectSymlinkTarget(sessionDir); err != nil {
			return removed, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
		}
		if purgeAudit {
			for _, name := range []string{"memory_proposals.jsonl", "prompts.jsonl"} {
				ok, err := removePrivacyFile(storageDir, "sessions", sessionID, name)
				if err != nil {
					return removed, err
				}
				if ok {
					removed++
				}
			}
		}
		if purgeTranscripts {
			ok, err := removePrivacyFile(storageDir, "sessions", sessionID, "transcript.md")
			if err != nil {
				return removed, err
			}
			if ok {
				removed++
			}
		}
	}
	if purgeAudit {
		ok, err := removePrivacyFile(storageDir, "process_audit.jsonl")
		if err != nil {
			return removed, err
		}
		if ok {
			removed++
		}
	}
	return removed, nil
}

func removePrivacyFile(storageDir string, elems ...string) (bool, error) {
	path, err := storage.SafeJoin(storageDir, elems...)
	if err != nil {
		return false, app.NewError(app.CategoryStorage, "privacy_purge", "unsafe purge path", err)
	}
	if err := storage.EnsureNoSymlinkParents(path); err != nil {
		return false, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
	}
	if err := storage.RejectSymlinkTarget(path); err != nil {
		return false, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
	}
	return true, nil
}

func handleSlash(ctx context.Context, out io.Writer, diag io.Writer, rt *runtime, sessionID, line string) (bool, error) {
	result, err := handleSlashResult(ctx, diag, rt, sessionID, line)
	if strings.TrimSpace(result.Output) != "" {
		_, _ = fmt.Fprintln(out, strings.TrimSpace(result.Output))
	}
	return result.Done, err
}

func handleSlashResult(ctx context.Context, diag io.Writer, rt *runtime, sessionID, line string) (slashContextResult, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return slashContextResult{}, nil
	}
	switch parts[0] {
	case "/", "/help":
		return slashContextResult{Output: slashHelpText()}, nil
	case "/new":
		if blocked := pendingContextBlock(ctx, rt, sessionID); blocked != "" {
			return slashContextResult{Output: blocked, PendingBlocked: blocked}, nil
		}
		newSessionID := app.NewID("session")
		if err := memory.TouchSessionActivity(rt.StorageDir, newSessionID); err != nil {
			return slashContextResult{}, err
		}
		if task, err := updateCurrentTaskSession(rt, newSessionID); err != nil {
			return slashContextResult{}, err
		} else if task.ID != "" {
			return slashContextResult{ActiveSessionID: newSessionID, ActiveTask: &task, Output: fmt.Sprintf("new chat: %s\ntask unchanged: %s\nprofile unchanged: %s", newSessionID, task.ID, emptyDash(rt.Config.ActiveProfileID))}, nil
		}
		return slashContextResult{ActiveSessionID: newSessionID, TaskCleared: true, Output: fmt.Sprintf("new chat: %s\ntask unchanged: none\nprofile unchanged: %s", newSessionID, emptyDash(rt.Config.ActiveProfileID))}, nil
	case "/resume":
		if blocked := pendingContextBlock(ctx, rt, sessionID); blocked != "" {
			return slashContextResult{Output: blocked, PendingBlocked: blocked}, nil
		}
		if len(parts) == 1 {
			sessions, err := memory.ListSessions(rt.StorageDir)
			if err != nil {
				return slashContextResult{}, err
			}
			return slashContextResult{Picker: &slashPicker{Kind: "sessions", Sessions: sessions}, Output: sessionsText(sessions)}, nil
		}
		summary, err := memory.LookupSession(rt.StorageDir, parts[1])
		if err != nil {
			return slashContextResult{}, err
		}
		if task, err := updateCurrentTaskSession(rt, summary.ID); err != nil {
			return slashContextResult{}, err
		} else if task.ID != "" {
			return slashContextResult{ActiveSessionID: summary.ID, ActiveTask: &task, Output: fmt.Sprintf("resumed chat: %s\ntask unchanged: %s\nprofile unchanged: %s", summary.ID, task.ID, emptyDash(rt.Config.ActiveProfileID))}, nil
		}
		return slashContextResult{ActiveSessionID: summary.ID, TaskCleared: true, Output: fmt.Sprintf("resumed chat: %s\ntask unchanged: none\nprofile unchanged: %s", summary.ID, emptyDash(rt.Config.ActiveProfileID))}, nil
	case "/task":
		if blocked := pendingContextBlock(ctx, rt, sessionID); blocked != "" {
			return slashContextResult{Output: blocked, PendingBlocked: blocked}, nil
		}
		return handleTaskSlashResult(rt, sessionID, parts)
	case "/profile":
		if blocked := pendingContextBlock(ctx, rt, sessionID); blocked != "" {
			return slashContextResult{Output: blocked, PendingBlocked: blocked}, nil
		}
		return handleProfileSlashResult(rt, parts)
	default:
		var out bytes.Buffer
		done, err := handleSlashLegacy(ctx, &out, diag, rt, sessionID, line)
		return slashContextResult{Done: done, Output: strings.TrimSpace(out.String())}, err
	}
}

func pendingContextBlock(ctx context.Context, rt *runtime, sessionID string) string {
	if rt == nil {
		return ""
	}
	if task, err := rt.Tasks.Current(); err == nil && task.PendingPlanning != nil {
		return "context switch blocked: resolve pending planning first"
	}
	if proposal, ok, err := rt.Proposals.LatestPending(ctx, sessionID); err == nil && ok && proposal.ID != "" {
		return "context switch blocked: accept, reject, or resolve pending memory proposal first"
	}
	return ""
}

func updateCurrentTaskSession(rt *runtime, sessionID string) (app.TaskState, error) {
	task, err := rt.Tasks.Current()
	if err != nil {
		appErr := app.AsError(err)
		if appErr.Category == app.CategoryValidation && appErr.Code == "missing_current_task" {
			return app.TaskState{}, nil
		}
		return app.TaskState{}, err
	}
	if task.Stage == app.StageDone {
		return task, nil
	}
	return rt.Tasks.SetLastSessionID(sessionID)
}

func handleTaskSlashResult(rt *runtime, sessionID string, parts []string) (slashContextResult, error) {
	if len(parts) == 1 {
		items, err := rt.Tasks.ListTasks()
		if err != nil {
			return slashContextResult{}, err
		}
		return slashContextResult{Picker: &slashPicker{Kind: "tasks", Tasks: items}, Output: tasksListText(items)}, nil
	}
	switch parts[1] {
	case "start", "status", "step", "expect", "plan", "criteria", "move", "pause", "resume":
		var out bytes.Buffer
		err := handleTaskSlash(&out, rt, parts, "")
		task, _ := rt.Tasks.Current()
		result := slashContextResult{Output: strings.TrimSpace(out.String())}
		if task.ID != "" {
			result.ActiveTask = &task
		}
		return result, err
	case "close":
		if err := rt.Tasks.ClearCurrentFocus(); err != nil {
			return slashContextResult{}, err
		}
		return slashContextResult{TaskCleared: true, Output: "task focus: none"}, nil
	case "archive":
		if len(parts) < 3 {
			return slashContextResult{}, app.NewError(app.CategoryCLI, "missing_task_id", "task id is required", nil)
		}
		task, err := rt.Tasks.ArchiveTaskMetadata(parts[2])
		if err != nil {
			return slashContextResult{}, err
		}
		return slashContextResult{TaskCleared: true, Output: fmt.Sprintf("archived task: %s", task.ID)}, nil
	case "restore":
		if len(parts) < 3 {
			return slashContextResult{}, app.NewError(app.CategoryCLI, "missing_task_id", "task id is required", nil)
		}
		task, err := rt.Tasks.RestoreArchivedTask(parts[2], sessionID)
		if err != nil {
			return slashContextResult{}, err
		}
		return slashContextResult{ActiveTask: &task, Output: fmt.Sprintf("restored and active task: %s", task.ID)}, nil
	default:
		task, err := rt.Tasks.SelectTask(parts[1], sessionID)
		if err != nil {
			return slashContextResult{}, err
		}
		return slashContextResult{ActiveTask: &task, Output: fmt.Sprintf("active task: %s", task.ID)}, nil
	}
}

func handleProfileSlashResult(rt *runtime, parts []string) (slashContextResult, error) {
	if err := rt.Profiles.EnsureDefaults(); err != nil {
		return slashContextResult{}, err
	}
	if len(parts) == 1 {
		items, err := rt.Profiles.List()
		if err != nil {
			return slashContextResult{}, err
		}
		active, _ := rt.activeProfile()
		cfg := rt.Config
		return slashContextResult{ActiveProfile: &active, ActiveConfig: &cfg, Picker: &slashPicker{Kind: "profiles", Profiles: items}, Output: profilesListText(items, rt.Config.ActiveProfileID)}, nil
	}
	if parts[1] == "create" {
		if len(parts) < 3 {
			return slashContextResult{}, app.NewError(app.CategoryCLI, "missing_profile_id", "profile id is required", nil)
		}
		profile, err := rt.Profiles.CreateDefault(parts[2])
		if err != nil {
			return slashContextResult{}, err
		}
		rt.syncActiveProfile(profile)
		cfg := rt.Config
		return slashContextResult{ActiveProfile: &profile, ActiveConfig: &cfg, Output: fmt.Sprintf("created and active profile: %s", profile.ID)}, nil
	}
	profile, err := rt.Profiles.SetActive(parts[1])
	if err != nil {
		return slashContextResult{}, err
	}
	rt.syncActiveProfile(profile)
	cfg := rt.Config
	return slashContextResult{ActiveProfile: &profile, ActiveConfig: &cfg, Output: fmt.Sprintf("active profile: %s", profile.ID)}, nil
}

func slashHelpText() string {
	return `Slash commands:
Chat/session:
  /new                         start new chat session; new short; keeps task/work/profile/long/model
  /resume                      list old chat sessions
  /resume <session_id>         resume chat session / short
Task focus/work:
  /task                        list saved tasks
  /task <task_id>              select task/work in current chat
  /task close                  clear current task focus
  /task archive <task_id>      archive task metadata, keep work memory
  /task restore <task_id>      restore archived task and make it current
Task lifecycle/recovery:
  /task status                 show current task and allowed stages
  /task pause                  pause task lifecycle / work
  /task resume                 resume paused task lifecycle / work
  /task move <stage>           move task stage
  /task step <text>            set current step
  /task expect <action>        set expected action
  /task plan <text>            add plan item
  /task criteria <text>        add acceptance criterion
Profile/long:
  /profile                     list profiles
  /profile <id>                switch profile / long context
  /profile create <id>         create profile with safe defaults
Utility:
  /model <id>                  set active model
  /memory <short|work|long>    list memory layer
  /memory propose              propose memory from latest exchange
  /memory apply ...            apply pending memory proposal
  /save <short|work|long> <text>
  /clear short
  /invariants
  /process audit
  /privacy
  /exit`
}

func sessionsText(sessions []memory.SessionSummary) string {
	if len(sessions) == 0 {
		return "sessions: none\nusage: /resume <session_id>"
	}
	var b strings.Builder
	b.WriteString("sessions:\n")
	for _, session := range sessions {
		b.WriteString(fmt.Sprintf("  %s  %s\n", session.ID, session.LastActivity.Format(time.RFC3339)))
	}
	b.WriteString("usage: /resume <session_id>")
	return strings.TrimRight(b.String(), "\n")
}

func tasksListText(items []tasks.TaskSummary) string {
	if len(items) == 0 {
		return "tasks: none\nusage: /task <task_id>"
	}
	var b strings.Builder
	b.WriteString("tasks:\n")
	section := ""
	for _, item := range items {
		nextSection := "active"
		if item.Archived {
			nextSection = "archived"
		}
		if nextSection != section {
			b.WriteString(nextSection + ":\n")
			section = nextSection
		}
		marker := " "
		if item.IsCurrent {
			marker = "*"
		}
		paused := ""
		if item.State.Status == app.TaskStatusPaused {
			paused = " paused"
		}
		b.WriteString(fmt.Sprintf("  %s %s  %s/%s%s  %s\n", marker, item.State.ID, item.State.Stage, item.State.Status, paused, item.State.Title))
	}
	b.WriteString("usage: /task <task_id> | /task close | /task archive <task_id> | /task restore <task_id>")
	return strings.TrimRight(b.String(), "\n")
}

func profilesListText(items []app.UserProfile, active string) string {
	var b strings.Builder
	b.WriteString("profiles:\n")
	for _, profile := range items {
		marker := " "
		if profile.ID == active {
			marker = "*"
		}
		b.WriteString(fmt.Sprintf("  %s %s  %s\n", marker, profile.ID, profile.DisplayName))
	}
	b.WriteString("  + new\n")
	b.WriteString("usage: /profile <id> | /profile create <id>")
	return strings.TrimRight(b.String(), "\n")
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func handleSlashLegacy(ctx context.Context, out io.Writer, diag io.Writer, rt *runtime, sessionID, line string) (bool, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false, nil
	}
	switch parts[0] {
	case "/exit":
		return true, nil
	case "/help":
		help := `Slash commands:
  /model <id>                    set active model
  /profile [id|create <id>]      show/set/create active profile
  /task start <title>            start a task
  /task status                   show current task and allowed stages
  /task move <stage>             move task stage
  /task step <text>              set current step
  /task expect <action>          set expected action
  /task plan <text>              add plan item
  /task criteria <text>          add acceptance criterion
  /task pause|resume             pause or resume task
  lifecycle                      planning->execution->validation->done; done requires accepted validation
  --verify <argv>                records app-issued evidence bound to current task/session
  /save <short|work|long> <text> save memory to layer
  /memory <short|work|long>      list memory layer
  /memory propose                propose memory from latest exchange
  /memory apply --accept all     apply pending memory proposal
  /memory apply --accept <id>|--reject <id>|--edit <id>:layer=<l>,content=<c>
	/invariants                    list active invariants
	/invariants add <id> --kind <k> --content <text> --forbid <term>
  /clear short                   clear short-term memory
	/process audit                  show latest process audit event
  /privacy                       show privacy summary
  /exit                          leave REPL`
		_, _ = fmt.Fprintln(out, help)
	case "/privacy":
		_, _ = fmt.Fprintln(out, "OPENROUTER_API_KEY env-only; memory/profile/task stored local.")
	case "/profile":
		if len(parts) == 1 {
			profile, err := rt.activeProfile()
			if err != nil {
				return false, err
			}
			_, _ = fmt.Fprint(out, profileText(profile, rt.Config.ActiveProfileID))
			return false, nil
		}
		if parts[1] == "create" {
			id := "profile"
			if len(parts) > 2 {
				id = parts[2]
			}
			styleMap := map[string]string{"language": "ru", "tone": "direct"}
			formatMap := map[string]string{"structure": "concise"}
			constraints := []string{"be concise"}
			remainder := strings.TrimSpace(strings.TrimPrefix(line, "/profile create "+id))
			if remainder == "" {
				remainder = strings.TrimSpace(strings.TrimPrefix(line, "/profile create"))
			}
			if remainder != "" {
				tokens := splitShellTokens(remainder)
				for i := 0; i < len(tokens); {
					switch tokens[i] {
					case "--style":
						if i+1 < len(tokens) {
							k, v, ok := strings.Cut(tokens[i+1], "=")
							if ok {
								styleMap[k] = v
							}
							i += 2
						} else {
							i++
						}
					case "--format":
						if i+1 < len(tokens) {
							k, v, ok := strings.Cut(tokens[i+1], "=")
							if ok {
								formatMap[k] = v
							}
							i += 2
						} else {
							i++
						}
					case "--constraint":
						if i+1 < len(tokens) {
							constraints = append(constraints, tokens[i+1])
							i += 2
						} else {
							i++
						}
					default:
						i++
					}
				}
			}
			now := time.Now().UTC()
			p := app.UserProfile{ID: id, DisplayName: id, Style: styleMap, ResponseFormat: formatMap, Constraints: constraints, CreatedAt: now, UpdatedAt: now}
			if err := rt.Profiles.Create(p); err != nil {
				return false, err
			}
			profile, err := rt.Profiles.SetActive(id)
			if err != nil {
				return false, err
			}
			rt.syncActiveProfile(profile)
			_, _ = fmt.Fprintf(out, "created and active profile: %s\n", id)
			return false, nil
		}
		if err := rt.Profiles.EnsureDefaults(); err != nil {
			return false, err
		}
		profile, err := rt.Profiles.SetActive(parts[1])
		if err != nil {
			return false, err
		}
		rt.syncActiveProfile(profile)
		_, _ = fmt.Fprintf(out, "active profile: %s\n", profile.ID)
	case "/model":
		if len(parts) < 2 {
			_, _ = fmt.Fprintf(out, "active model: %s\n", rt.Config.ActiveModel)
			return false, nil
		}
		cfg, err := setActiveModel(ctx, rt, diag, parts[1])
		if err != nil {
			return false, err
		}
		_, _ = fmt.Fprintf(out, "active model: %s\n", cfg.ActiveModel)
	case "/task":
		return false, handleTaskSlash(out, rt, parts, strings.TrimSpace(strings.TrimPrefix(line, strings.Join(parts[:min(len(parts), 2)], " "))))
	case "/save":
		if len(parts) < 3 {
			return false, app.NewError(app.CategoryCLI, "missing_args", "/save <short|work|long> <text>", nil)
		}
		layer := app.MemoryLayer(parts[1])
		text := strings.TrimSpace(strings.TrimPrefix(line, "/save "+parts[1]))
		taskState, _ := rt.Tasks.Current()
		if layer == app.LayerWork {
			var err error
			taskState, err = rt.currentMutableWorkTask()
			if err != nil {
				return false, err
			}
		}
		profile, err := rt.activeProfile()
		if err != nil {
			return false, err
		}
		record, err := rt.Memory.Save(ctx, memory.SaveInput{Layer: layer, Kind: "manual", Content: text, Source: "manual", ProfileID: profile.ID, SessionID: sessionID, TaskID: taskState.ID})
		if err != nil {
			return false, err
		}
		_, _ = fmt.Fprintf(out, "saved: %s\n", record.ID)
	case "/memory":
		return false, handleMemorySlash(ctx, out, diag, rt, sessionID, parts, line)
	case "/invariants":
		return false, handleInvariantsSlash(ctx, out, rt, parts, line)
	case "/process":
		if len(parts) >= 2 && parts[1] == "audit" {
			events, err := process.NewAuditStore(rt.StorageDir).Latest(1)
			if err != nil {
				return false, err
			}
			_, _ = fmt.Fprint(out, processAuditText(events))
			return false, nil
		}
		return false, app.NewError(app.CategoryCLI, "unknown_process_command", "unknown process command", nil)
	case "/clear":
		if len(parts) == 2 && parts[1] == "short" {
			return false, rt.Memory.ClearShort(ctx, sessionID)
		}
		return false, app.NewError(app.CategoryCLI, "unknown_command", "unknown clear command", nil)
	default:
		return false, app.ErrorWithHint(app.CategoryCLI, "unknown_command", "unknown slash command", "type /help for available commands", nil)
	}
	return false, nil
}

func handleInvariantsSlash(ctx context.Context, out io.Writer, rt *runtime, parts []string, rawLine string) error {
	if len(parts) == 1 {
		items, err := rt.Invariants.List(ctx)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, invariantsText(items))
		return nil
	}
	if parts[1] != "add" {
		return app.NewError(app.CategoryCLI, "unknown_invariants_command", "unknown invariants command", nil)
	}
	if len(parts) < 3 {
		return app.NewError(app.CategoryCLI, "missing_invariant_id", "invariant id required", nil)
	}
	tokens := splitShellTokens(strings.TrimSpace(strings.TrimPrefix(rawLine, "/invariants add "+parts[2])))
	inv := app.Invariant{ID: parts[2], Scope: "project", Kind: "other", Severity: "block", Source: "user"}
	for i := 0; i < len(tokens); {
		switch tokens[i] {
		case "--kind":
			if i+1 >= len(tokens) {
				return app.NewError(app.CategoryCLI, "missing_kind", "--kind requires a value", nil)
			}
			inv.Kind = tokens[i+1]
			i += 2
		case "--content":
			if i+1 >= len(tokens) {
				return app.NewError(app.CategoryCLI, "missing_content", "--content requires a value", nil)
			}
			inv.Content = tokens[i+1]
			i += 2
		case "--severity":
			if i+1 >= len(tokens) {
				return app.NewError(app.CategoryCLI, "missing_severity", "--severity requires a value", nil)
			}
			inv.Severity = tokens[i+1]
			i += 2
		case "--forbid":
			if i+1 >= len(tokens) {
				return app.NewError(app.CategoryCLI, "missing_forbid", "--forbid requires a value", nil)
			}
			inv.ForbiddenTerms = append(inv.ForbiddenTerms, tokens[i+1])
			i += 2
		default:
			i++
		}
	}
	added, err := rt.Invariants.Add(ctx, inv)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprint(out, invariantsText([]app.Invariant{added}))
	return nil
}

func handleTaskSlash(out io.Writer, rt *runtime, parts []string, rest string) error {
	if len(parts) < 2 {
		return app.NewError(app.CategoryCLI, "missing_task_command", "task command required", nil)
	}
	switch parts[1] {
	case "start":
		title := strings.TrimSpace(strings.TrimPrefix(strings.Join(parts, " "), "/task start"))
		state, err := rt.Tasks.Start(title)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "task: %s planning\n", state.ID)
	case "status":
		state, err := rt.Tasks.Current()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, taskText(state))
	case "step":
		state, err := rt.Tasks.SetStep(strings.TrimSpace(strings.TrimPrefix(strings.Join(parts, " "), "/task step")))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "current_step: %s\n", safeTerminalText(state.CurrentStep))
	case "expect":
		if len(parts) < 3 {
			return app.NewError(app.CategoryCLI, "missing_expected_action", "expected action required", nil)
		}
		state, err := rt.Tasks.SetExpectedAction(app.ExpectedAction(parts[2]))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "expected_action: %s\n", state.ExpectedAction)
	case "plan":
		state, err := rt.Tasks.AddPlanItem(strings.TrimSpace(strings.TrimPrefix(strings.Join(parts, " "), "/task plan")))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "plan_items: %d\n", len(state.Plan))
	case "criteria":
		state, err := rt.Tasks.AddCriteria(strings.TrimSpace(strings.TrimPrefix(strings.Join(parts, " "), "/task criteria")))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "acceptance_criteria: %d\n", len(state.AcceptanceCriteria))
	case "move":
		if len(parts) < 3 {
			return app.NewError(app.CategoryCLI, "missing_stage", "stage required", nil)
		}
		state, err := applyTaskMove(rt, app.TaskStage(parts[2]))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "stage: %s\n", state.Stage)
	case "pause":
		state, err := rt.Tasks.Pause()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, taskText(state))
	case "resume":
		state, err := rt.Tasks.Resume()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, taskText(state))
	default:
		return app.NewError(app.CategoryCLI, "unknown_task_command", "unknown task command", nil)
	}
	_ = rest
	return nil
}

func handleMemorySlash(ctx context.Context, out io.Writer, diag io.Writer, rt *runtime, sessionID string, parts []string, rawLine string) error {
	if len(parts) < 2 {
		return app.NewError(app.CategoryCLI, "missing_memory_command", "memory command required", nil)
	}
	switch parts[1] {
	case "short", "work", "long":
		taskState, _ := rt.Tasks.Current()
		profileID := ""
		if parts[1] == "long" {
			profile, err := rt.activeProfile()
			if err != nil {
				return err
			}
			profileID = profile.ID
		}
		records, err := rt.Memory.List(ctx, app.MemoryLayer(parts[1]), sessionID, taskState.ID, profileID)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, memoryText(records))
	case "propose":
		if proposal, ok, err := rt.Proposals.LatestPending(ctx, sessionID); err != nil {
			if app.AsError(err).Code != "proposal_read" && app.AsError(err).Code != "session_missing" {
				return err
			}
		} else if ok {
			_, _ = fmt.Fprint(out, proposalText(proposal))
			return nil
		}
		userRecord, assistantRecord, err := rt.Memory.LatestExchange(ctx, sessionID)
		if err != nil {
			return err
		}
		profile, err := rt.activeProfile()
		if err != nil {
			return err
		}
		taskState, _ := rt.Tasks.Current()
		var taskPtr *app.TaskState
		if taskState.ID != "" {
			if taskState.Status == app.TaskStatusPaused {
				return app.NewError(app.CategoryValidation, "task_paused", "task is paused; resume before continuing", nil)
			}
			taskPtr = &taskState
		}
		rt.ensureProvider()
		ensureProviderDisclosure(diag, rt)
		if rt.Config.MemoryModel != "" {
			if err := validateModelID(ctx, rt.ensureProvider(), rt.Config.MemoryModel); err != nil {
				return err
			}
		}
		bundle, _ := rt.Memory.SelectForPrompt(ctx, sessionID, taskID(taskPtr), profile.ID)
		if err := process.NewAuditStore(rt.StorageDir).Save(process.ProcessAuditEvent{SessionID: sessionID, TaskID: taskID(taskPtr), Stage: stageOf(taskPtr), Decision: "memory_proposal_provider_call", Reason: "memory_classifier", Model: rt.Config.MemoryModel, CreatedAt: time.Now().UTC()}); err != nil {
			return err
		}
		proposal, err := rt.ensureClassifier().Propose(ctx, memory.ClassificationInput{SessionID: sessionID, UserMessageID: userRecord.ID, AssistantMessageID: assistantRecord.ID, UserMessage: userRecord.Content, AssistantMessage: assistantRecord.Content, Profile: profile, Task: taskPtr, Model: rt.Config.MemoryModel, ExistingShort: bundle.Short, ExistingWork: bundle.Work, ExistingLong: bundle.Long})
		if err != nil {
			return err
		}
		if err := rt.Proposals.Save(ctx, proposal); err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, proposalText(proposal))
	case "apply":
		taskID, workBlockedCode, workBlockedMessage, err := rt.workApplyContext()
		if err != nil {
			return err
		}
		profile, err := rt.activeProfile()
		if err != nil {
			return err
		}
		applyOpts, err := parseMemoryApplyArgsRaw(rawLine)
		if err != nil {
			latest, latestErr := rt.Proposals.Latest(ctx, sessionID)
			if latestErr == nil {
				_, _ = fmt.Fprint(out, proposalText(latest))
			}
			return err
		}
		if applyOpts.ProposalID == "" {
			applyOpts.ProposalID = "latest"
		}
		applyOpts.SessionID = sessionID
		applyOpts.TaskID = taskID
		applyOpts.ProfileID = profile.ID
		applyOpts.WorkBlockedCode = workBlockedCode
		applyOpts.WorkBlockedMessage = workBlockedMessage
		result, err := rt.Proposals.Apply(ctx, applyOpts)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "saved %d records\n", len(result.SavedRecords))
	default:
		return app.NewError(app.CategoryCLI, "unknown_memory_command", "unknown memory command", nil)
	}
	return nil
}

func parseMemoryApplyArgs(parts []string) (memory.ApplyOptions, error) {
	opts := memory.ApplyOptions{AcceptIDs: map[string]bool{}, RejectIDs: map[string]bool{}, Edits: map[string]memory.ProposalEdit{}}
	if len(parts) == 2 {
		return opts, app.ErrorWithHint(app.CategoryCLI, "missing_apply_action", "/memory apply requires --accept all|--accept <id>, --reject <id>, or --edit <id>:layer=<layer>,content=<text>", "example: /memory apply --accept all", nil)
	}
	for i := 2; i < len(parts); {
		switch parts[i] {
		case "--accept":
			if i+1 >= len(parts) {
				return opts, app.NewError(app.CategoryCLI, "missing_accept_target", "--accept requires 'all' or a record id", nil)
			}
			if parts[i+1] == "all" {
				opts.AcceptAll = true
			} else {
				opts.AcceptIDs[parts[i+1]] = true
			}
			i += 2
		case "--reject":
			if i+1 >= len(parts) {
				return opts, app.NewError(app.CategoryCLI, "missing_reject_target", "--reject requires 'all' or a record id", nil)
			}
			if parts[i+1] == "all" {
				opts.RejectAll = true
			} else {
				opts.RejectIDs[parts[i+1]] = true
			}
			i += 2
		case "--edit":
			if i+1 >= len(parts) {
				return opts, app.NewError(app.CategoryCLI, "missing_edit_value", "--edit requires record_id:layer=<layer>,content=<text>", nil)
			}
			id, edit, err := parseEdit(parts[i+1])
			if err != nil {
				return opts, err
			}
			opts.Edits[id] = edit
			i += 2
		case "--proposal":
			if i+1 >= len(parts) {
				return opts, app.NewError(app.CategoryCLI, "missing_proposal", "--proposal requires a proposal id", nil)
			}
			opts.ProposalID = parts[i+1]
			i += 2
		default:
			return opts, app.NewError(app.CategoryCLI, "unknown_apply_option", "unknown /memory apply option: "+parts[i], nil)
		}
	}
	return opts, nil
}

func parseMemoryApplyArgsRaw(rawLine string) (memory.ApplyOptions, error) {
	remainder := strings.TrimSpace(strings.TrimPrefix(rawLine, "/memory apply"))
	if remainder == "" {
		return memory.ApplyOptions{}, app.ErrorWithHint(app.CategoryCLI, "missing_apply_action", "/memory apply requires --accept all|--accept <id>, --reject <id>, or --edit <id>:layer=<layer>,content=<text>", "example: /memory apply --accept all", nil)
	}
	tokens := splitShellTokens(remainder)
	opts := memory.ApplyOptions{AcceptIDs: map[string]bool{}, RejectIDs: map[string]bool{}, Edits: map[string]memory.ProposalEdit{}}
	for i := 0; i < len(tokens); {
		switch tokens[i] {
		case "--accept":
			if i+1 >= len(tokens) {
				return opts, app.NewError(app.CategoryCLI, "missing_accept_target", "--accept requires 'all' or a record id", nil)
			}
			if tokens[i+1] == "all" {
				opts.AcceptAll = true
			} else {
				opts.AcceptIDs[tokens[i+1]] = true
			}
			i += 2
		case "--reject":
			if i+1 >= len(tokens) {
				return opts, app.NewError(app.CategoryCLI, "missing_reject_target", "--reject requires 'all' or a record id", nil)
			}
			if tokens[i+1] == "all" {
				opts.RejectAll = true
			} else {
				opts.RejectIDs[tokens[i+1]] = true
			}
			i += 2
		case "--edit":
			if i+1 >= len(tokens) {
				return opts, app.NewError(app.CategoryCLI, "missing_edit_value", "--edit requires record_id:layer=<layer>,content=<text>", nil)
			}
			id, edit, err := parseEdit(tokens[i+1])
			if err != nil {
				return opts, err
			}
			opts.Edits[id] = edit
			i += 2
		case "--proposal":
			if i+1 >= len(tokens) {
				return opts, app.NewError(app.CategoryCLI, "missing_proposal", "--proposal requires a proposal id", nil)
			}
			opts.ProposalID = tokens[i+1]
			i += 2
		default:
			return opts, app.NewError(app.CategoryCLI, "unknown_apply_option", "unknown /memory apply option: "+tokens[i], nil)
		}
	}
	return opts, nil
}

func splitShellTokens(s string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = true
			quoteChar = c
			continue
		}
		if c == ' ' || c == '\t' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(c)
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
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

func recordRenderedPrompt(storageDir, sessionID, promptID string, messages []app.ChatMessage, rendered string) error {
	if os.Getenv("ASSISTANT_PROMPT_AUDIT") != "1" && os.Getenv("ASSISTANT_RAW_PROMPT_AUDIT") != "1" {
		return nil
	}
	if sessionID == "" {
		return nil
	}
	dir, err := storage.SafeJoin(storageDir, "sessions", sessionID)
	if err != nil {
		return err
	}
	if err := storage.EnsureDir(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, "prompts.jsonl")
	digest := sha256.Sum256([]byte(rendered))
	roles := make([]string, 0, len(messages))
	for _, msg := range messages {
		roles = append(roles, string(msg.Role))
	}
	record := map[string]any{"id": promptID, "session_id": sessionID, "rendered_prompt_sha256": fmt.Sprintf("%x", digest), "message_count": len(messages), "roles": roles, "raw": false, "created_at": time.Now().UTC()}
	if os.Getenv("ASSISTANT_RAW_PROMPT_AUDIT") == "1" {
		record["raw"] = true
		record["rendered_prompt"] = rendered
		record["messages"] = messages
	}
	return storage.AppendJSONL(path, record)
}

func ensureProviderDisclosure(w io.Writer, rt *runtime) {
	if rt == nil || rt.DisclosureShown {
		return
	}
	provider, ok := rt.Provider.(*providers.OpenRouterProvider)
	if !ok {
		return
	}
	rt.DisclosureShown = true
	_, _ = io.WriteString(w, providerDisclosureText(provider.BaseURL))
}

func providerDisclosureText(host string) string {
	if host == "" {
		host = app.DefaultOpenRouterBaseURL
	}
	return fmt.Sprintf("Provider disclosure: rendered prompt, active profile, task state, selected memory, latest exchange, classifier payload, and semantic validation payload may be sent to %s. OPENROUTER_API_KEY is read from env and never persisted.\n", host)
}

func validateModelSyntax(model string) error {
	if strings.TrimSpace(model) == "" || strings.ContainsAny(model, " \t\n\r") || len(model) > 200 || !strings.Contains(model, "/") {
		return app.ErrorWithHint(app.CategoryValidation, "invalid_model", "invalid model id", "model id must be in provider/model format, e.g. openai/gpt-4.1-mini", nil)
	}
	return nil
}

func validateModelID(ctx context.Context, provider providers.LLMProvider, model string) error {
	if err := validateModelSyntax(model); err != nil {
		return err
	}
	models, err := provider.ListModels(ctx)
	if err != nil {
		appErr := app.AsError(err)
		if appErr.Category == app.CategoryProvider && appErr.Code == "missing_api_key" {
			return nil
		}
		return err
	}
	if len(models) == 0 {
		return nil
	}
	for _, candidate := range models {
		if candidate == model {
			return nil
		}
	}
	return app.NewError(app.CategoryValidation, "invalid_model", "model id not found", nil)
}

func setActiveModel(ctx context.Context, rt *runtime, diag io.Writer, model string) (app.AppConfig, error) {
	rt.ensureProvider()
	ensureProviderDisclosure(diag, rt)
	if err := validateModelID(ctx, rt.ensureProvider(), model); err != nil {
		return rt.Config, err
	}
	cfg, err := rt.ConfigMgr.Update(func(cfg *app.AppConfig) error {
		cfg.ActiveModel = model
		cfg.MemoryModel = model
		return nil
	})
	if err != nil {
		return rt.Config, err
	}
	rt.Config = cfg
	if rt.Process != nil {
		rt.Process.Model = cfg.ActiveModel
		rt.Process.MemoryModel = cfg.MemoryModel
	}
	return cfg, nil
}

func parseEdit(raw string) (string, memory.ProposalEdit, error) {
	id, body, ok := strings.Cut(raw, ":")
	if !ok || id == "" {
		return "", memory.ProposalEdit{}, app.NewError(app.CategoryCLI, "invalid_edit", "edit must be record_id:layer=<layer>,content=<text>", nil)
	}
	edit := memory.ProposalEdit{}
	parts := []string{}
	if before, content, ok := strings.Cut(body, ",content="); ok {
		if before != "" {
			parts = append(parts, strings.Split(before, ",")...)
		}
		parts = append(parts, "content="+content)
	} else {
		parts = strings.Split(body, ",")
	}
	for _, part := range parts {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return "", memory.ProposalEdit{}, app.NewError(app.CategoryCLI, "invalid_edit", "edit fields must be key=value", nil)
		}
		switch key {
		case "layer":
			edit.Layer = app.ProposedMemoryLayer(value)
		case "content":
			edit.Content = value
		default:
			return "", memory.ProposalEdit{}, app.NewError(app.CategoryCLI, "invalid_edit", "unknown edit field", nil)
		}
	}
	return id, edit, nil
}

func parseKeyValues(items []string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range items {
		key, value, ok := strings.Cut(item, "=")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return nil, app.NewError(app.CategoryCLI, "invalid_key_value", "expected key=value", nil)
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out, nil
}

func writeOutput(w io.Writer, asJSON bool, value any, text string) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	_, err := io.WriteString(w, text)
	return err
}

func printError(w io.Writer, err error, asJSON bool) {
	if err == nil {
		return
	}
	appErr := app.AsError(err)
	if asJSON {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": appErr})
		return
	}
	_, _ = fmt.Fprintf(w, "%s: %s\n", appErr.Code, safeTerminalText(appErr.Message))
	if appErr.Hint != "" {
		_, _ = fmt.Fprintf(w, "hint: %s\n", safeTerminalText(appErr.Hint))
	}
}

type chatRenderOptions struct {
	Color bool
}

type chatStyle struct {
	color bool
}

func textChatResult(result chatResult) string {
	return renderChatResult(result, chatRenderOptions{})
}

func renderChatResult(result chatResult, opts chatRenderOptions) string {
	style := chatStyle{color: opts.Color}
	var b strings.Builder
	if strings.TrimSpace(result.Answer) == "" && strings.TrimSpace(result.RenderedPrompt) != "" {
		writeSectionHeader(&b, style, "Rendered prompt")
		b.WriteString(safeTerminalText(strings.TrimSpace(result.RenderedPrompt)))
		b.WriteString("\n")
		return b.String()
	}

	writeSectionHeader(&b, style, "Assistant")
	b.WriteString(renderAssistantAnswer(result.Answer, style))
	if answerStage(result.Answer) == app.StagePlanning {
		writePlanningSwarmSummary(&b, style, result.AuditEvents)
	}

	task := displayTask(result)
	if task != nil {
		writeTaskSummary(&b, style, *task)
	}
	if result.Transition != nil {
		writeTransitionSummary(&b, style, *result.Transition)
	}
	writeAppliedArtifactsSummary(&b, style, result.AppliedArtifacts)
	writeEvidenceAndWarnings(&b, style, result, task)
	if hasProposalRecords(result.Proposal) {
		writeProposalSummary(&b, style, *result.Proposal)
	}
	return b.String()
}

func writePlanningSwarmSummary(b *strings.Builder, style chatStyle, events []process.ProcessAuditEvent) {
	items := planningSwarmItems(events)
	if len(items) == 0 {
		return
	}
	writeSectionHeader(b, style, "Planning swarm")
	for _, item := range items {
		b.WriteString("- ")
		b.WriteString(style.label(item.Role))
		b.WriteString(": ")
		b.WriteString(renderMarkdownTerminalText(item.Summary, style))
		stats := []string{}
		if item.Findings != "" {
			stats = append(stats, "findings="+item.Findings)
		}
		if item.PlanItems != "" {
			stats = append(stats, "plan proposals="+item.PlanItems)
		}
		if item.Criteria != "" {
			stats = append(stats, "criteria proposals="+item.Criteria)
		}
		if len(stats) > 0 {
			b.WriteString(" (")
			b.WriteString(strings.Join(stats, ", "))
			b.WriteString(")")
		}
		b.WriteString("\n")
		if item.TopFinding != "" {
			b.WriteString("  finding: ")
			b.WriteString(renderMarkdownTerminalText(item.TopFinding, style))
			b.WriteString("\n")
		}
		if item.ProposedPlan != "" {
			b.WriteString("  proposed plan: ")
			b.WriteString(renderMarkdownTerminalText(item.ProposedPlan, style))
			b.WriteString("\n")
		}
		if item.ProposedCriteria != "" {
			b.WriteString("  proposed criteria: ")
			b.WriteString(renderMarkdownTerminalText(item.ProposedCriteria, style))
			b.WriteString("\n")
		}
	}
}

type planningSwarmDisplayItem struct {
	Role             string
	Summary          string
	Findings         string
	PlanItems        string
	Criteria         string
	TopFinding       string
	ProposedPlan     string
	ProposedCriteria string
}

func planningSwarmItems(events []process.ProcessAuditEvent) []planningSwarmDisplayItem {
	seen := map[string]bool{}
	out := []planningSwarmDisplayItem{}
	for _, event := range events {
		if event.Decision != "planning_specialist_summary" || strings.TrimSpace(event.AgentRole) == "" {
			continue
		}
		role := strings.TrimSpace(event.AgentRole)
		if seen[role] {
			continue
		}
		seen[role] = true
		item := compactPlanningSummary(event.Reason)
		item.Role = role
		out = append(out, item)
	}
	return out
}

func compactPlanningSummary(reason string) planningSwarmDisplayItem {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return planningSwarmDisplayItem{Summary: "review completed"}
	}
	fields := semicolonFields(reason)
	if len(fields) == 0 {
		return planningSwarmDisplayItem{Summary: reason}
	}
	item := planningSwarmDisplayItem{
		Summary:          firstNonEmpty(fields["summary"], reason),
		Findings:         fields["findings"],
		PlanItems:        fields["plan_items"],
		Criteria:         fields["criteria"],
		TopFinding:       fields["top_finding"],
		ProposedPlan:     fields["proposed_plan"],
		ProposedCriteria: fields["proposed_criteria"],
	}
	if item.Summary == reason {
		parts := strings.Split(reason, ";")
		item.Summary = strings.TrimSpace(parts[0])
	}
	return item
}

func semicolonFields(reason string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(reason, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func renderAssistantAnswer(answer string, style chatStyle) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return style.dim("(no assistant answer)") + "\n"
	}
	if text, ok := renderStructuredStageAnswer(answer, style); ok {
		return text
	}
	return renderMarkdownTerminalText(answer, style) + "\n"
}

func answerStage(answer string) app.TaskStage {
	cleaned := cleanStructuredAnswer(answer)
	chunks, ok := structuredJSONChunks(cleaned)
	if !ok || len(chunks) != 1 {
		return ""
	}
	var stageField struct {
		Stage app.TaskStage `json:"stage"`
	}
	if err := json.Unmarshal([]byte(chunks[0]), &stageField); err != nil {
		return ""
	}
	return stageField.Stage
}

func renderStructuredStageAnswer(answer string, style chatStyle) (string, bool) {
	cleaned := cleanStructuredAnswer(answer)
	if cleaned == "" {
		return "", false
	}
	chunks, ok := structuredJSONChunks(cleaned)
	if !ok || len(chunks) == 0 {
		return "", false
	}
	if len(chunks) == 1 {
		return renderStructuredStageJSON(chunks[0], style)
	}
	var b strings.Builder
	rendered := 0
	for i, chunk := range chunks {
		text, ok := renderStructuredStageJSON(chunk, style)
		if !ok {
			return "", false
		}
		if rendered > 0 {
			b.WriteString("\n")
		}
		b.WriteString(style.label(fmt.Sprintf("Stage output %d", i+1)))
		b.WriteString(":\n")
		b.WriteString(text)
		rendered++
	}
	return b.String(), rendered > 0
}

func structuredJSONChunks(cleaned string) ([]string, bool) {
	decoder := json.NewDecoder(strings.NewReader(cleaned))
	chunks := []string{}
	for {
		var raw json.RawMessage
		err := decoder.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		chunks = append(chunks, string(raw))
	}
	return chunks, len(chunks) > 0
}

func renderStructuredStageJSON(cleaned string, style chatStyle) (string, bool) {
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || !strings.HasPrefix(cleaned, "{") {
		return "", false
	}
	var stageField struct {
		Stage string `json:"stage"`
	}
	if err := json.Unmarshal([]byte(cleaned), &stageField); err != nil || stageField.Stage == "" {
		return "", false
	}
	var b strings.Builder
	switch app.TaskStage(stageField.Stage) {
	case app.StagePlanning:
		var out process.PlanningOutput
		if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
			return "", false
		}
		writeField(&b, style, "Summary", out.Summary)
		writeListBlock(&b, style, "Assumptions", out.Assumptions, 8)
		writeListBlock(&b, style, "Acceptance criteria", out.AcceptanceCriteria, 12)
		writeListBlock(&b, style, "Plan", out.Plan, 12)
		writeListBlock(&b, style, "Open questions", out.OpenQuestions, 8)
		writeField(&b, style, "Readiness", out.Readiness)
	case app.StageExecution:
		var out process.ExecutionOutput
		if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
			return "", false
		}
		writeField(&b, style, "Summary", out.Summary)
		writeBlock(&b, style, "Deliverable", out.Deliverable)
		writeField(&b, style, "Current step", out.CurrentStep)
		writeListBlock(&b, style, "Completed steps", out.CompletedSteps, 8)
		writeField(&b, style, "Next step", out.NextStep)
		writeListBlock(&b, style, "Changed artifacts", out.ChangedArtifacts, 8)
		writeListBlock(&b, style, "Verification", out.Verification, 8)
		writeListBlock(&b, style, "Blockers", out.Blockers, 8)
		writeField(&b, style, "Next signal", out.NextSignal)
	case app.StageValidation:
		var out process.ValidationOutput
		if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
			return "", false
		}
		writeFindingsBlock(&b, style, out.Findings)
		writeListBlock(&b, style, "Passed checks", out.PassedChecks, 10)
		writeListBlock(&b, style, "Missing evidence", out.MissingEvidence, 10)
		writeListBlock(&b, style, "Residual risks", out.ResidualRisks, 10)
		writeField(&b, style, "Verdict", out.Verdict)
	case app.StageDone:
		var out process.DoneOutput
		if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
			return "", false
		}
		writeField(&b, style, "Summary", out.Summary)
		writeListBlock(&b, style, "Acceptance status", out.AcceptanceStatus, 12)
		writeListBlock(&b, style, "Validation evidence", out.ValidationEvidence, 12)
		writeListBlock(&b, style, "Follow-up tasks", out.FollowUpTaskProposals, 8)
	default:
		return "", false
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

func cleanStructuredAnswer(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
		if firstLine, rest, ok := strings.Cut(text, "\n"); ok && !strings.HasPrefix(strings.TrimSpace(firstLine), "{") {
			text = rest
		}
		text = strings.TrimSpace(text)
		text = strings.TrimSuffix(text, "```")
	}
	return strings.TrimSpace(text)
}

func displayTask(result chatResult) *app.TaskState {
	if result.Task != nil {
		return result.Task
	}
	if result.Transition != nil && strings.TrimSpace(result.Transition.State.ID) != "" {
		return &result.Transition.State
	}
	return nil
}

func writeTaskSummary(b *strings.Builder, style chatStyle, state app.TaskState) {
	writeSectionHeader(b, style, "Task")
	writeKVLine(b, style, "id", state.ID)
	writeKVLine(b, style, "stage", string(state.Stage))
	writeKVLine(b, style, "expected", string(state.ExpectedAction))
	writeKVLine(b, style, "status", string(state.Status))
	writeKVLine(b, style, "current_step", state.CurrentStep)
	if state.ValidationStatus != "" {
		writeKVLine(b, style, "validation", state.ValidationStatus)
	}
	if len(state.Microtasks) > 0 {
		done := 0
		for _, mt := range state.Microtasks {
			if strings.EqualFold(mt.Status, "done") || strings.EqualFold(mt.Status, "completed") || strings.TrimSpace(mt.ResultSummary) != "" {
				done++
			}
		}
		writeKVLine(b, style, "microtasks", fmt.Sprintf("%d/%d with results", done, len(state.Microtasks)))
	}
	if len(state.Plan) > 0 {
		writeListBlock(b, style, "Plan", state.Plan, 5)
	}
	if len(state.AcceptanceCriteria) > 0 {
		writeListBlock(b, style, "Acceptance criteria", state.AcceptanceCriteria, 5)
	}
}

func writeTransitionSummary(b *strings.Builder, style chatStyle, transition process.TransitionResult) {
	writeSectionHeader(b, style, "Transition")
	from := string(transition.From)
	if strings.TrimSpace(from) == "" {
		from = "new_task"
	}
	writeKVLine(b, style, "move", fmt.Sprintf("%s -> %s", from, transition.To))
	if transition.Reason != "" {
		writeKVLine(b, style, "reason", transition.Reason)
	}
	if transition.State.ExpectedAction != "" {
		writeKVLine(b, style, "next_expected", string(transition.State.ExpectedAction))
	}
}

func writeAppliedArtifactsSummary(b *strings.Builder, style chatStyle, artifacts []string) {
	if len(artifacts) == 0 {
		return
	}
	writeSectionHeader(b, style, "Files")
	for _, artifact := range artifacts {
		b.WriteString("- ")
		b.WriteString(style.label("applied"))
		b.WriteString(": ")
		b.WriteString(safeTerminalText(artifact))
		b.WriteString("\n")
	}
}

func writeEvidenceAndWarnings(b *strings.Builder, style chatStyle, result chatResult, task *app.TaskState) {
	evidenceWarnings := []string{}
	otherWarnings := []string{}
	for _, warning := range result.Warnings {
		if strings.HasPrefix(warning, "auto verification:") {
			evidenceWarnings = append(evidenceWarnings, warning)
			continue
		}
		otherWarnings = append(otherWarnings, warning)
	}
	evidenceRefs := []string{}
	if task != nil {
		evidenceRefs = append(evidenceRefs, task.ValidationEvidence...)
	}
	if len(evidenceRefs) == 0 && result.Transition != nil {
		evidenceRefs = append(evidenceRefs, result.Transition.State.ValidationEvidence...)
	}
	if len(evidenceRefs) > 0 || len(evidenceWarnings) > 0 {
		writeSectionHeader(b, style, "Evidence")
		if len(evidenceRefs) > 0 {
			writeKVLine(b, style, "trusted_refs", strconv.Itoa(len(evidenceRefs)))
		}
		for _, warning := range evidenceWarnings {
			b.WriteString("- ")
			b.WriteString(safeTerminalText(warning))
			b.WriteString("\n")
		}
	}
	if len(otherWarnings) > 0 {
		writeSectionHeader(b, style, "Warnings")
		for _, warning := range otherWarnings {
			b.WriteString("- ")
			b.WriteString(safeTerminalText(warning))
			b.WriteString("\n")
		}
	}
}

func hasProposalRecords(proposal *app.MemoryProposal) bool {
	return proposal != nil && len(proposal.Records) > 0
}

func writeProposalSummary(b *strings.Builder, style chatStyle, proposal app.MemoryProposal) {
	writeSectionHeader(b, style, "Memory proposal")
	writeKVLine(b, style, "id", proposal.ID)
	for _, record := range proposal.Records {
		b.WriteString("- ")
		b.WriteString(record.ID)
		b.WriteString(" [")
		b.WriteString(string(record.Layer))
		b.WriteString("] ")
		b.WriteString(string(record.Status))
		if record.Kind != "" {
			b.WriteString(" ")
			b.WriteString(record.Kind)
		}
		b.WriteString(": ")
		b.WriteString(safeTerminalText(record.Content))
		if record.BlockReason != "" {
			b.WriteString(" (blocked: ")
			b.WriteString(safeTerminalText(record.BlockReason))
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	writeSectionHeader(b, style, "Next")
	b.WriteString("- Review memory proposal; apply it only if these records should be saved.\n")
	b.WriteString("- CLI: assistant memory apply --proposal ")
	b.WriteString(proposal.ID)
	b.WriteString(" --accept all\n")
	b.WriteString("- REPL: /memory apply --proposal ")
	b.WriteString(proposal.ID)
	b.WriteString(" --accept all\n")
}

func writeSectionHeader(b *strings.Builder, style chatStyle, title string) {
	if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
		b.WriteString("\n")
	}
	b.WriteString(style.header("== " + title + " =="))
	b.WriteString("\n")
}

func writeField(b *strings.Builder, style chatStyle, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(style.label(label))
	b.WriteString(": ")
	b.WriteString(renderMarkdownTerminalText(value, style))
	b.WriteString("\n")
}

func writeKVLine(b *strings.Builder, style chatStyle, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(style.label(key))
	b.WriteString(": ")
	b.WriteString(safeTerminalText(value))
	b.WriteString("\n")
}

func writeBlock(b *strings.Builder, style chatStyle, title, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(style.label(title))
	b.WriteString(":\n")
	b.WriteString(renderMarkdownTerminalText(value, style))
	b.WriteString("\n")
}

func renderMarkdownTerminalText(text string, style chatStyle) string {
	text = safeTerminalText(text)
	if !style.color || !strings.Contains(text, "```") {
		return text
	}
	var b strings.Builder
	remaining := text
	inCode := false
	lang := ""
	for {
		idx := strings.Index(remaining, "```")
		if idx < 0 {
			if inCode {
				b.WriteString(highlightCode(remaining, lang, style))
			} else {
				b.WriteString(remaining)
			}
			break
		}
		if inCode {
			b.WriteString(highlightCode(remaining[:idx], lang, style))
			b.WriteString(style.dim("```"))
			remaining = remaining[idx+3:]
			inCode = false
			lang = ""
			continue
		}
		b.WriteString(remaining[:idx])
		remaining = remaining[idx+3:]
		firstLine, rest, ok := strings.Cut(remaining, "\n")
		if !ok {
			b.WriteString(style.dim("```"))
			b.WriteString(remaining)
			break
		}
		lang = strings.TrimSpace(firstLine)
		b.WriteString(style.dim("```" + lang))
		b.WriteString("\n")
		remaining = rest
		inCode = true
	}
	return b.String()
}

func highlightCode(code, lang string, style chatStyle) string {
	if strings.EqualFold(strings.TrimSpace(lang), "go") || strings.TrimSpace(lang) == "" {
		return highlightGoCode(code, style)
	}
	return style.paint("38;5;250", code)
}

func highlightGoCode(code string, style chatStyle) string {
	keywords := map[string]bool{
		"break": true, "case": true, "chan": true, "const": true, "continue": true, "default": true,
		"defer": true, "else": true, "fallthrough": true, "for": true, "func": true, "go": true,
		"goto": true, "if": true, "import": true, "interface": true, "map": true, "package": true,
		"range": true, "return": true, "select": true, "struct": true, "switch": true, "type": true, "var": true,
	}
	var b strings.Builder
	for i := 0; i < len(code); {
		if i+1 < len(code) && code[i] == '/' && code[i+1] == '/' {
			end := strings.IndexByte(code[i:], '\n')
			if end < 0 {
				b.WriteString(style.paint("38;5;244", code[i:]))
				break
			}
			b.WriteString(style.paint("38;5;244", code[i:i+end]))
			i += end
			continue
		}
		if code[i] == '"' || code[i] == '`' {
			quote := code[i]
			j := i + 1
			for j < len(code) {
				if code[j] == quote && (quote == '`' || code[j-1] != '\\') {
					j++
					break
				}
				j++
			}
			b.WriteString(style.paint("38;5;214", code[i:j]))
			i = j
			continue
		}
		if isIdentStart(code[i]) {
			j := i + 1
			for j < len(code) && isIdentPart(code[j]) {
				j++
			}
			word := code[i:j]
			if keywords[word] {
				b.WriteString(style.paint("38;5;81", word))
			} else {
				b.WriteString(word)
			}
			i = j
			continue
		}
		b.WriteByte(code[i])
		i++
	}
	return b.String()
}

func isIdentStart(b byte) bool {
	return b == '_' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || b >= '0' && b <= '9'
}

func writeListBlock(b *strings.Builder, style chatStyle, title string, items []string, limit int) {
	if len(items) == 0 {
		return
	}
	b.WriteString(style.label(title))
	b.WriteString(":\n")
	visible := len(items)
	if limit > 0 && visible > limit {
		visible = limit
	}
	for i := 0; i < visible; i++ {
		item := strings.TrimSpace(items[i])
		if item == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, safeTerminalText(item)))
	}
	if visible < len(items) {
		b.WriteString(fmt.Sprintf("... %d more\n", len(items)-visible))
	}
}

func writeFindingsBlock(b *strings.Builder, style chatStyle, findings []process.ValidationFinding) {
	b.WriteString(style.label("Findings"))
	b.WriteString(":\n")
	if len(findings) == 0 {
		b.WriteString("- none\n")
		return
	}
	for _, finding := range findings {
		severity := strings.TrimSpace(finding.Severity)
		if severity == "" {
			severity = "finding"
		}
		b.WriteString("- ")
		b.WriteString(safeTerminalText(severity))
		if finding.Location != "" {
			b.WriteString(" at ")
			b.WriteString(safeTerminalText(finding.Location))
		}
		if finding.Problem != "" {
			b.WriteString(": ")
			b.WriteString(safeTerminalText(finding.Problem))
		}
		if finding.Fix != "" {
			b.WriteString(" fix: ")
			b.WriteString(safeTerminalText(finding.Fix))
		}
		b.WriteString("\n")
	}
}

func terminalColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("ASSISTANT_NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (s chatStyle) header(text string) string {
	return s.paint("1;36", text)
}

func (s chatStyle) label(text string) string {
	return s.paint("1", text)
}

func (s chatStyle) dim(text string) string {
	return s.paint("2", text)
}

func (s chatStyle) paint(code, text string) string {
	if !s.color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func profileListText(items []app.UserProfile, active string) string {
	var b strings.Builder
	for _, profile := range items {
		mark := ""
		if profile.ID == active {
			mark = "*"
		}
		b.WriteString(mark)
		b.WriteString(profile.ID)
		if profile.DisplayName != "" {
			b.WriteString(" ")
			b.WriteString(safeTerminalText(profile.DisplayName))
		}
		if len(profile.Style) > 0 {
			b.WriteString(" style=")
			b.WriteString(safeTerminalText(formatStringMap(profile.Style)))
		}
		if len(profile.ResponseFormat) > 0 {
			b.WriteString(" format=")
			b.WriteString(safeTerminalText(formatStringMap(profile.ResponseFormat)))
		}
		if len(profile.Constraints) > 0 {
			b.WriteString(" constraints=")
			b.WriteString(safeTerminalText(strings.Join(profile.Constraints, "; ")))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func profileText(profile app.UserProfile, active string) string {
	var b strings.Builder
	if profile.ID == active {
		b.WriteString("active ")
	}
	b.WriteString("profile: ")
	b.WriteString(profile.ID)
	b.WriteByte('\n')
	b.WriteString("display_name: ")
	b.WriteString(safeTerminalText(profile.DisplayName))
	b.WriteByte('\n')
	b.WriteString("style: ")
	b.WriteString(safeTerminalText(formatStringMap(profile.Style)))
	b.WriteByte('\n')
	b.WriteString("response_format: ")
	b.WriteString(safeTerminalText(formatStringMap(profile.ResponseFormat)))
	b.WriteByte('\n')
	b.WriteString("constraints: ")
	b.WriteString(safeTerminalText(strings.Join(profile.Constraints, "; ")))
	b.WriteByte('\n')
	return b.String()
}

func formatStringMap(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ",")
}

func memoryListHeader(layer app.MemoryLayer, sessionID, taskID string) string {
	var b strings.Builder
	b.WriteString("layer=")
	b.WriteString(string(layer))
	if sessionID != "" {
		b.WriteString(" session=")
		b.WriteString(sessionID)
	}
	if taskID != "" {
		b.WriteString(" task=")
		b.WriteString(taskID)
	}
	b.WriteByte('\n')
	return b.String()
}

func memoryText(records []app.MemoryRecord) string {
	var b strings.Builder
	for _, record := range records {
		b.WriteString("[")
		b.WriteString(string(record.Layer))
		b.WriteString("] ")
		b.WriteString(record.Kind)
		if record.Source != "" {
			b.WriteString(" source=")
			b.WriteString(record.Source)
		}
		b.WriteString(": ")
		b.WriteString(safeTerminalText(record.Content))
		b.WriteByte('\n')
	}
	return b.String()
}

func proposalText(proposal app.MemoryProposal) string {
	var b strings.Builder
	b.WriteString("== Memory proposal ==\n")
	b.WriteString("id: ")
	b.WriteString(proposal.ID)
	b.WriteByte('\n')
	for _, record := range proposal.Records {
		b.WriteString("- ")
		b.WriteString(record.ID)
		b.WriteString(" ")
		b.WriteString("[")
		b.WriteString(string(record.Layer))
		b.WriteString("] ")
		b.WriteString(string(record.Status))
		b.WriteString(" ")
		b.WriteString(record.Kind)
		if record.Scope != "" {
			b.WriteString(" scope=")
			b.WriteString(safeTerminalText(record.Scope))
		}
		if record.ProfileID != "" {
			b.WriteString(" profile=")
			b.WriteString(safeTerminalText(record.ProfileID))
		}
		b.WriteString(": ")
		b.WriteString(safeTerminalText(record.Content))
		if record.Reason != "" {
			b.WriteString(" reason=")
			b.WriteString(safeTerminalText(record.Reason))
		}
		if record.Confidence != 0 {
			b.WriteString(fmt.Sprintf(" confidence=%.2f", record.Confidence))
		}
		b.WriteByte('\n')
	}
	b.WriteString("Next: assistant memory apply --proposal ")
	b.WriteString(proposal.ID)
	b.WriteString(" --accept all | --reject <record_id> | --edit <record_id>:layer=<layer>,content=<text>\n")
	b.WriteString("REPL: /memory apply --proposal ")
	b.WriteString(proposal.ID)
	b.WriteString(" --accept all | --reject <record_id> | --edit <record_id>:layer=<layer>,content=<text>\n")
	return b.String()
}

func proposalsText(proposals []app.MemoryProposal) string {
	if len(proposals) == 0 {
		return "no pending proposals\n"
	}
	var b strings.Builder
	for _, p := range proposals {
		b.WriteString(fmt.Sprintf("proposal=%s session=%s records=%d\n", p.ID, p.SessionID, len(p.Records)))
	}
	return b.String()
}

func invariantsText(items []app.Invariant) string {
	if len(items) == 0 {
		return "no invariants\n"
	}
	var b strings.Builder
	for _, inv := range items {
		b.WriteString(inv.ID)
		b.WriteString(" [")
		b.WriteString(inv.Severity)
		b.WriteString("] ")
		b.WriteString(inv.Kind)
		b.WriteString(": ")
		b.WriteString(safeTerminalText(redactSecretLikeDisplay(inv.Content)))
		if len(inv.ForbiddenTerms) > 0 {
			b.WriteString(" forbid=")
			b.WriteString(safeTerminalText(redactSecretLikeDisplay(strings.Join(inv.ForbiddenTerms, ","))))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func redactSecretLikeDisplay(text string) string {
	redacted, findings := validation.RedactText(text)
	for _, marker := range []string{"OPENROUTER_API_KEY", "openrouter_api_key"} {
		redacted = strings.ReplaceAll(redacted, marker, "[REDACTED_SECRET_PATTERN]")
	}
	if len(findings) == 0 {
		return redacted
	}
	return strings.ReplaceAll(redacted, "[REDACTED_SECRET]", "[REDACTED_SECRET_PATTERN]")
}

func processAuditText(events []process.ProcessAuditEvent) string {
	if len(events) == 0 {
		return "no process audit events\n"
	}
	var b strings.Builder
	for _, e := range events {
		b.WriteString(fmt.Sprintf("%s session=%s task=%s stage=%s action=%s decision=%s", e.CreatedAt.Format(time.RFC3339), e.SessionID, e.TaskID, e.Stage, e.ActionKind, e.Decision))
		if len(e.ValidatorErrors) > 0 {
			b.WriteString(" errors=")
			b.WriteString(safeTerminalText(strings.Join(e.ValidatorErrors, "; ")))
		}
		if e.TransitionFrom != "" || e.TransitionTo != "" {
			b.WriteString(fmt.Sprintf(" transition=%s->%s", e.TransitionFrom, e.TransitionTo))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func safeTerminalText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) && !unicode.Is(unicode.Bidi_Control, r) && !unicode.Is(unicode.Cf, r) {
			b.WriteRune(r)
			continue
		}
		b.WriteString(strconv.QuoteRuneToASCII(r)[1 : len(strconv.QuoteRuneToASCII(r))-1])
	}
	return b.String()
}

func taskText(state app.TaskState) string {
	var b strings.Builder
	style := chatStyle{}
	writeTaskSummary(&b, style, state)
	writeField(&b, style, "Title", state.Title)
	writeField(&b, style, "Objective", state.Objective)
	writeListBlock(&b, style, "Decisions", state.Decisions, 10)
	writeListBlock(&b, style, "Open questions", state.OpenQuestions, 10)
	allowed := tasks.AllowedNext(state.Stage)
	if len(allowed) > 0 {
		items := make([]string, 0, len(allowed))
		for _, stage := range allowed {
			items = append(items, string(stage))
		}
		writeListBlock(&b, style, "Allowed next stages", items, 10)
	}
	return b.String()
}
