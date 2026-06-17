package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/profiles"
	"github.com/nikbrik/coding_writer/internal/prompting"
	"github.com/nikbrik/coding_writer/internal/providers"
	"github.com/nikbrik/coding_writer/internal/storage"
	"github.com/nikbrik/coding_writer/internal/tasks"
	"github.com/nikbrik/coding_writer/internal/validation"
)

var Version = "dev"

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
	Proposals       *memory.ProposalStore
	Provider        providers.LLMProvider
	Builder         *prompting.Builder
	Classifier      *memory.Classifier
	DisclosureShown bool
	Quiet           bool
}

func Execute() error {
	opts := &globalOptions{}
	cmd := newRootCommand(opts)
	if err := cmd.Execute(); err != nil {
		printError(cmd.ErrOrStderr(), err, opts.JSON)
		return err
	}
	return nil
}

func ExitCode(err error) int { return app.ExitCode(err) }

func newRootCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "assistant",
		Short:         "Stateful CLI assistant with memory layers",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&opts.StorageDir, "storage-dir", "", "runtime storage directory")
	cmd.PersistentFlags().StringVar(&opts.Model, "model", "", "active model id")
	cmd.PersistentFlags().StringVar(&opts.MemoryModel, "memory-model", "", "memory classifier model id")
	cmd.PersistentFlags().StringVar(&opts.Profile, "profile", "", "active profile id")
	cmd.PersistentFlags().StringVar(&opts.OpenRouterBaseURL, "openrouter-base-url", "", "OpenRouter-compatible base URL")
	cmd.PersistentFlags().BoolVar(&opts.TrustOpenRouterBaseURL, "trust-openrouter-base-url", false, "trust non-default OpenRouter-compatible base URL for this invocation")
	cmd.PersistentFlags().BoolVar(&opts.JSON, "json", false, "emit JSON")
	cmd.PersistentFlags().BoolVar(&opts.Quiet, "quiet", false, "suppress diagnostic output")
	cmd.AddCommand(initCommand(opts), chatCommand(opts), profilesCommand(opts), memoryCommand(opts), taskCommand(opts), privacyCommand(opts))
	return cmd
}

func newRuntime(ctx context.Context, opts *globalOptions) (*runtime, error) {
	storageDir, err := app.ResolveStorageDir(opts.StorageDir)
	if err != nil {
		return nil, app.NewError(app.CategoryStorage, "storage_dir", err.Error(), err)
	}
	cfgMgr := app.NewConfigManager(storageDir)
	if err := cfgMgr.EnsureStorageTree(); err != nil {
		return nil, err
	}
	profMgr := profiles.NewManager(storageDir, cfgMgr)
	if err := profMgr.EnsureDefaults(); err != nil {
		return nil, err
	}
	cfg, err := cfgMgr.LoadEffective(app.ConfigOptions{StorageDir: storageDir, ActiveModel: opts.Model, MemoryModel: opts.MemoryModel, ActiveProfileID: opts.Profile, OpenRouterBaseURL: opts.OpenRouterBaseURL, TrustOpenRouterBaseURL: opts.TrustOpenRouterBaseURL})
	if err != nil {
		return nil, err
	}
	if cfg.ActiveProfileID == "" {
		cfg.ActiveProfileID = "student"
	}
	if opts.Profile != "" {
		if _, err := profMgr.Get(opts.Profile); err != nil {
			return nil, err
		}
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
	return &runtime{
		StorageDir: storageDir,
		Config:     cfg,
		ConfigMgr:  cfgMgr,
		Profiles:   profMgr,
		Tasks:      tasks.NewManager(storageDir),
		Memory:     memMgr,
		Proposals:  memory.NewProposalStore(storageDir, memMgr),
	}, nil
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

func chooseProvider(cfg app.AppConfig) providers.LLMProvider {
	if os.Getenv("ASSISTANT_PROVIDER") == "fake" || os.Getenv("ASSISTANT_FAKE_PROVIDER") == "1" {
		return providers.NewFakeProvider()
	}
	return providers.NewOpenRouterProvider(cfg.OpenRouterBaseURL)
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
			profilesList, err := rt.Profiles.List()
			if err != nil {
				return err
			}
			out := map[string]any{"ok": true, "storage_dir": rt.StorageDir, "config": rt.Config, "profiles": profilesList}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, out, fmt.Sprintf("initialized %s\n", rt.StorageDir))
		},
	}
}

type chatOptions struct {
	Once         bool
	Input        string
	RenderPrompt bool
}

func chatCommand(opts *globalOptions) *cobra.Command {
	chatOpts := &chatOptions{}
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start chat loop or run one request",
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.JSON && !chatOpts.Once {
				return app.ErrorWithHint(app.CategoryCLI, "json_repl_unsupported", "chat --json requires --once", "use --once for single-request JSON output", nil)
			}
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
		if chatOpts.Once {
			if strings.TrimSpace(chatOpts.Input) == "" {
				return app.NewError(app.CategoryCLI, "missing_input", "--input is required with --once", nil)
			}
			if !chatOpts.RenderPrompt {
				rt.ensureProvider()
				ensureProviderDisclosure(cmd.ErrOrStderr(), rt)
			}
			result, err := runChatExchange(cmd.Context(), rt, app.NewID("session"), chatOpts.Input, chatOpts.RenderPrompt)
			if err != nil {
				return err
			}
			for _, warning := range result.Warnings {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), warning)
			}
			if err := recordRenderedPrompt(rt.StorageDir, result.SessionID, result.Messages, result.RenderedPrompt); err != nil {
				result.Warnings = append(result.Warnings, "prompt audit skipped: "+app.AsError(err).Code)
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, result, textChatResult(result))
		}
		return runREPL(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), rt)
		},
	}
	cmd.Flags().BoolVar(&chatOpts.Once, "once", false, "run one request")
	cmd.Flags().StringVar(&chatOpts.Input, "input", "", "input text for --once")
	cmd.Flags().BoolVar(&chatOpts.RenderPrompt, "render-prompt", false, "render prompt without provider call")
	return cmd
}

type chatResult struct {
	OK             bool                `json:"ok"`
	SessionID      string              `json:"session_id"`
	Answer         string              `json:"answer,omitempty"`
	Model          string              `json:"model,omitempty"`
	RenderedPrompt string              `json:"rendered_prompt"`
	Messages       []app.ChatMessage   `json:"messages,omitempty"`
	Proposal       *app.MemoryProposal `json:"proposal,omitempty"`
	Warnings       []string            `json:"warnings,omitempty"`
}

func runChatExchange(ctx context.Context, rt *runtime, sessionID, input string, renderOnly bool) (chatResult, error) {
	if validation.HasSecret(input) {
		return chatResult{OK: false, SessionID: sessionID, Model: rt.Config.ActiveModel}, app.NewError(app.CategoryValidation, "secret_blocked", "secret-like input cannot be sent to provider", nil)
	}
	profile, err := rt.Profiles.Active()
	if err != nil {
		return chatResult{}, err
	}
	taskState, taskErr := rt.Tasks.Current()
	var taskPtr *app.TaskState
	if taskErr != nil {
		appErr := app.AsError(taskErr)
		if appErr.Category != app.CategoryValidation || appErr.Code != "missing_current_task" {
			return chatResult{}, taskErr
		}
	} else if taskState.ID != "" {
		taskPtr = &taskState
	}
	bundle, err := rt.Memory.SelectForPrompt(ctx, sessionID, taskID(taskPtr), profile.ID)
	if err != nil {
		return chatResult{}, err
	}
	messages, err := rt.ensureBuilder().Build(prompting.BuildInput{Profile: profile, Task: taskPtr, Memory: bundle, Query: input})
	if err != nil {
		return chatResult{}, err
	}
	rendered := prompting.RenderMessages(messages)
	result := chatResult{OK: true, SessionID: sessionID, RenderedPrompt: rendered, Messages: messages, Model: rt.Config.ActiveModel}
	if renderOnly {
		return result, nil
	}
	if rt.Config.ActiveModel == "" {
		return result, app.NewError(app.CategoryProvider, "missing_model", "active model is required", nil)
	}
	provider := rt.ensureProvider()
	if err := validateModelID(ctx, provider, rt.Config.ActiveModel); err != nil {
		return result, err
	}
	res, err := provider.Complete(ctx, providers.CompletionRequest{Purpose: providers.PurposeChat, Model: rt.Config.ActiveModel, Messages: messages})
	if err != nil {
		return result, err
	}
	result.Answer = res.Message.Content
	result.Model = res.Model
	userRecord, err := rt.Memory.Save(ctx, memory.SaveInput{Layer: app.LayerShort, Kind: "message_user", Content: input, Source: "chat", SessionID: sessionID})
	if err != nil {
		return result, err
	}
	assistantRecord, err := rt.Memory.Save(ctx, memory.SaveInput{Layer: app.LayerShort, Kind: "message_assistant", Content: res.Message.Content, Source: "chat", SessionID: sessionID})
	if err != nil {
		return result, err
	}
	proposal, err := rt.ensureClassifier().Propose(ctx, memory.ClassificationInput{
		SessionID:          sessionID,
		UserMessageID:      userRecord.ID,
		AssistantMessageID: assistantRecord.ID,
		UserMessage:        input,
		AssistantMessage:   res.Message.Content,
		Profile:            profile,
		Task:               taskPtr,
		Model:              rt.Config.MemoryModel,
		ExistingShort:      bundle.Short,
		ExistingWork:       bundle.Work,
		ExistingLong:       bundle.Long,
	})
	if err != nil {
		result.Warnings = append(result.Warnings, "memory proposal skipped: "+app.AsError(err).Code)
		return result, nil
	}
	if err := rt.Proposals.Save(ctx, proposal); err != nil {
		result.Warnings = append(result.Warnings, "memory proposal skipped: "+app.AsError(err).Code)
		return result, nil
	}
	result.Proposal = &proposal
	return result, nil
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
			done, err := handleSlash(ctx, out, diag, rt, sessionID, line)
			if err != nil {
				failed = true
				_, _ = fmt.Fprintln(diag, err.Error())
			}
			if done {
				return nil
			}
			continue
		}
		rt.ensureProvider()
		ensureProviderDisclosure(diag, rt)
		result, err := runChatExchange(ctx, rt, sessionID, line, false)
		if err != nil {
			failed = true
			_, _ = fmt.Fprintln(diag, err.Error())
			continue
		}
		for _, warning := range result.Warnings {
			_, _ = fmt.Fprintln(diag, warning)
		}
		_, _ = fmt.Fprintln(out, safeTerminalText(result.Answer))
		if result.Proposal != nil {
			_, _ = fmt.Fprint(out, proposalText(*result.Proposal))
		}
		if err := recordRenderedPrompt(rt.StorageDir, sessionID, result.Messages, result.RenderedPrompt); err != nil {
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
			profile, err = rt.Profiles.Active()
		} else {
			profile, err = rt.Profiles.Get(args[0])
		}
		if err != nil {
			return err
		}
		return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "profile": profile}, fmt.Sprintf("profile: %s\n", profile.ID))
	}}
}

func profileSetCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{Use: "set <id>", Short: "Set active profile", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		rt, err := newRuntime(cmd.Context(), opts)
		if err != nil {
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
			records, err := rt.Memory.List(cmd.Context(), layer, sessionID, taskID)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "layer": layer, "session_id": sessionID, "task_id": taskID, "records": records}, memoryListHeader(layer, sessionID, taskID)+memoryText(records))
		},
	}
	cmd.Flags().StringVar(&sessionFlag, "session", "", "session id for short layer")
	cmd.Flags().StringVar(&taskFlag, "task", "", "task id for work layer")
	return cmd
}

func memoryProposeCommand(opts *globalOptions) *cobra.Command {
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
			userRecord, assistantRecord, err := rt.Memory.LatestExchange(cmd.Context(), sessionID)
			if err != nil {
				return err
			}
			profile, err := rt.Profiles.Active()
			if err != nil {
				return err
			}
			taskState, _ := rt.Tasks.Current()
			var taskPtr *app.TaskState
			if taskState.ID != "" {
				taskPtr = &taskState
			}
			rt.ensureProvider()
			ensureProviderDisclosure(cmd.ErrOrStderr(), rt)
			bundle, _ := rt.Memory.SelectForPrompt(cmd.Context(), sessionID, taskID(taskPtr), profile.ID)
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
			taskState, taskErr := rt.Tasks.Current()
			if taskErr != nil {
				appErr := app.AsError(taskErr)
				if appErr.Category != app.CategoryValidation || appErr.Code != "missing_current_task" {
					return taskErr
				}
			}
			if taskState.Status == app.TaskStatusPaused {
				return app.NewError(app.CategoryValidation, "task_paused", "resume task before applying memory proposal", nil)
			}
			profile, err := rt.Profiles.Active()
			if err != nil {
				return err
			}
			optsApply := memory.ApplyOptions{ProposalID: proposalID, AcceptIDs: map[string]bool{}, RejectIDs: map[string]bool{}, Edits: map[string]memory.ProposalEdit{}, TaskID: taskState.ID, ProfileID: profile.ID}
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

func taskCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Task state commands"}
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
	cmd.AddCommand(&cobra.Command{
		Use:   "move <stage>",
		Short: "Move current task stage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			state, err := rt.Tasks.Move(app.TaskStage(args[0]))
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "task": state, "allowed_next_stages": tasks.AllowedNext(state.Stage)}, taskText(state))
		},
	})
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
		Use:   "plan <text>",
		Short: "Append current task plan item",
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
		Short: "Append current task acceptance criteria",
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

func privacyCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "privacy",
		Short: "Show privacy summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			storageDir, err := app.ResolveStorageDir(opts.StorageDir)
			if err != nil {
				return err
			}
			summary := map[string]any{"ok": true, "api_key": "OPENROUTER_API_KEY env-only; never persisted", "provider_data": []string{"rendered prompt", "latest exchange", "classifier payload"}, "storage_dir": storageDir, "raw_transcripts": "not required in P0"}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, summary, "OPENROUTER_API_KEY env-only; prompts sent to provider; memory/profile/task stored local.\n")
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
	sessionsDir := filepath.Join(storageDir, "sessions")
	removed := 0
	err := filepath.WalkDir(sessionsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		shouldRemove := (purgeAudit && name == "memory_proposals.jsonl") || (purgeTranscripts && name == "transcript.md")
		if !shouldRemove {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		removed++
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return removed, nil
		}
		return removed, app.NewError(app.CategoryStorage, "privacy_purge", err.Error(), err)
	}
	return removed, nil
}

func handleSlash(ctx context.Context, out io.Writer, diag io.Writer, rt *runtime, sessionID, line string) (bool, error) {
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
  /save <short|work|long> <text> save memory to layer
  /memory <short|work|long>      list memory layer
  /memory propose                propose memory from latest exchange
  /memory apply --accept all     apply pending memory proposal
  /memory apply --accept <id>|--reject <id>|--edit <id>:layer=<l>,content=<c>
  /clear short                   clear short-term memory
  /privacy                       show privacy summary
  /exit                          leave REPL`
		_, _ = fmt.Fprintln(out, help)
	case "/privacy":
		_, _ = fmt.Fprintln(out, "OPENROUTER_API_KEY env-only; memory/profile/task stored local.")
	case "/profile":
		if len(parts) == 1 {
			profile, err := rt.Profiles.Active()
			if err != nil {
				return false, err
			}
			_, _ = fmt.Fprintf(out, "active profile: %s\n", profile.ID)
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
			_, _ = fmt.Fprintf(out, "created profile: %s\n", id)
			return false, nil
		}
		profile, err := rt.Profiles.SetActive(parts[1])
		if err != nil {
			return false, err
		}
		_, _ = fmt.Fprintf(out, "active profile: %s\n", profile.ID)
	case "/model":
		if len(parts) < 2 {
			_, _ = fmt.Fprintf(out, "active model: %s\n", rt.Config.ActiveModel)
			return false, nil
		}
		model := parts[1]
		if err := validateModelID(ctx, rt.ensureProvider(), model); err != nil {
			return false, err
		}
		_, err := rt.ConfigMgr.Update(func(cfg *app.AppConfig) error { cfg.ActiveModel = model; cfg.MemoryModel = model; return nil })
		if err != nil {
			return false, err
		}
		rt.Config.ActiveModel = model
		rt.Config.MemoryModel = model
		_, _ = fmt.Fprintf(out, "active model: %s\n", model)
	case "/task":
		return false, handleTaskSlash(out, rt, parts, strings.TrimSpace(strings.TrimPrefix(line, strings.Join(parts[:min(len(parts), 2)], " "))))
	case "/save":
		if len(parts) < 3 {
			return false, app.NewError(app.CategoryCLI, "missing_args", "/save <short|work|long> <text>", nil)
		}
		layer := app.MemoryLayer(parts[1])
		text := strings.TrimSpace(strings.TrimPrefix(line, "/save "+parts[1]))
		taskState, _ := rt.Tasks.Current()
		if layer == app.LayerWork && taskState.Status == app.TaskStatusPaused {
			return false, app.NewError(app.CategoryValidation, "task_paused", "resume task before mutating working memory", nil)
		}
		profile, err := rt.Profiles.Active()
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
		state, err := rt.Tasks.Move(app.TaskStage(parts[2]))
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
		records, err := rt.Memory.List(ctx, app.MemoryLayer(parts[1]), sessionID, taskState.ID)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, memoryText(records))
	case "propose":
		userRecord, assistantRecord, err := rt.Memory.LatestExchange(ctx, sessionID)
		if err != nil {
			return err
		}
		profile, err := rt.Profiles.Active()
		if err != nil {
			return err
		}
		taskState, _ := rt.Tasks.Current()
		var taskPtr *app.TaskState
		if taskState.ID != "" {
			taskPtr = &taskState
		}
		rt.ensureProvider()
		ensureProviderDisclosure(diag, rt)
		bundle, _ := rt.Memory.SelectForPrompt(ctx, sessionID, taskID(taskPtr), profile.ID)
		proposal, err := rt.ensureClassifier().Propose(ctx, memory.ClassificationInput{SessionID: sessionID, UserMessageID: userRecord.ID, AssistantMessageID: assistantRecord.ID, UserMessage: userRecord.Content, AssistantMessage: assistantRecord.Content, Profile: profile, Task: taskPtr, Model: rt.Config.MemoryModel, ExistingShort: bundle.Short, ExistingWork: bundle.Work, ExistingLong: bundle.Long})
		if err != nil {
			return err
		}
		if err := rt.Proposals.Save(ctx, proposal); err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, proposalText(proposal))
	case "apply":
		taskState, taskErr := rt.Tasks.Current()
		if taskErr != nil {
			appErr := app.AsError(taskErr)
			if appErr.Category != app.CategoryValidation || appErr.Code != "missing_current_task" {
				return taskErr
			}
		}
		if taskState.Status == app.TaskStatusPaused {
			return app.NewError(app.CategoryValidation, "task_paused", "resume task before applying memory proposal", nil)
		}
		profile, err := rt.Profiles.Active()
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
		applyOpts.ProposalID = "latest"
		applyOpts.SessionID = sessionID
		applyOpts.TaskID = taskState.ID
		applyOpts.ProfileID = profile.ID
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

func recordRenderedPrompt(storageDir, sessionID string, messages []app.ChatMessage, rendered string) error {
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
	record := map[string]any{"session_id": sessionID, "rendered_prompt": rendered, "messages": messages, "created_at": time.Now().UTC()}
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
	return fmt.Sprintf("Provider disclosure: rendered prompt, active profile, task state, selected memory, latest exchange, and classifier payload may be sent to %s. OPENROUTER_API_KEY is read from env and never persisted.\n", host)
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
	if _, ok := provider.(*providers.OpenRouterProvider); ok {
		return nil
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
	_, _ = fmt.Fprintf(w, "%s: %s\n", appErr.Code, appErr.Message)
}

func textChatResult(result chatResult) string {
	if result.Answer == "" {
		return safeTerminalText(result.RenderedPrompt)
	}
	text := safeTerminalText(result.Answer) + "\n"
	if result.Proposal != nil {
		text += proposalText(*result.Proposal)
	}
	return text
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
		b.WriteByte('\n')
	}
	return b.String()
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
		b.WriteString(": ")
		b.WriteString(safeTerminalText(record.Content))
		b.WriteByte('\n')
	}
	return b.String()
}

func proposalText(proposal app.MemoryProposal) string {
	var b strings.Builder
	b.WriteString("Memory proposal: ")
	b.WriteString(proposal.ID)
	b.WriteByte('\n')
	for _, record := range proposal.Records {
		b.WriteString("[")
		b.WriteString(string(record.Layer))
		b.WriteString("] ")
		b.WriteString(string(record.Status))
		b.WriteString(" ")
		b.WriteString(record.Kind)
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

func safeTerminalText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
			continue
		}
		b.WriteString(strconv.QuoteRuneToASCII(r)[1 : len(strconv.QuoteRuneToASCII(r))-1])
	}
	return b.String()
}

func taskText(state app.TaskState) string {
	return fmt.Sprintf("task=%s title=%s objective=%s stage=%s current_step=%s expected_action=%s status=%s plan=%v acceptance_criteria=%v decisions=%v open_questions=%v allowed=%v\n", state.ID, safeTerminalText(state.Title), safeTerminalText(state.Objective), state.Stage, safeTerminalText(state.CurrentStep), state.ExpectedAction, state.Status, state.Plan, state.AcceptanceCriteria, state.Decisions, state.OpenQuestions, tasks.AllowedNext(state.Stage))
}
