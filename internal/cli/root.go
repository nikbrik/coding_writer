package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/profiles"
	"github.com/nikbrik/coding_writer/internal/prompting"
	"github.com/nikbrik/coding_writer/internal/providers"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

type globalOptions struct {
	StorageDir        string
	Model             string
	MemoryModel       string
	Profile           string
	OpenRouterBaseURL string
	JSON              bool
}

type runtime struct {
	StorageDir string
	Config     app.AppConfig
	ConfigMgr  *app.ConfigManager
	Profiles   *profiles.Manager
	Tasks      *tasks.Manager
	Memory     *memory.Manager
	Proposals  *memory.ProposalStore
	Provider   providers.LLMProvider
	Builder    *prompting.Builder
	Classifier *memory.Classifier
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
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&opts.StorageDir, "storage-dir", "", "runtime storage directory")
	cmd.PersistentFlags().StringVar(&opts.Model, "model", "", "active model id")
	cmd.PersistentFlags().StringVar(&opts.MemoryModel, "memory-model", "", "memory classifier model id")
	cmd.PersistentFlags().StringVar(&opts.Profile, "profile", "", "active profile id")
	cmd.PersistentFlags().StringVar(&opts.OpenRouterBaseURL, "openrouter-base-url", "", "OpenRouter-compatible base URL")
	cmd.PersistentFlags().BoolVar(&opts.JSON, "json", false, "emit JSON")
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
	cfg, err := cfgMgr.LoadEffective(app.ConfigOptions{StorageDir: storageDir, ActiveModel: opts.Model, MemoryModel: opts.MemoryModel, ActiveProfileID: opts.Profile, OpenRouterBaseURL: opts.OpenRouterBaseURL})
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
	if err := cfgMgr.Save(cfg); err != nil {
		return nil, err
	}
	memMgr := memory.NewManager(storageDir)
	provider := chooseProvider(cfg)
	return &runtime{
		StorageDir: storageDir,
		Config:     cfg,
		ConfigMgr:  cfgMgr,
		Profiles:   profMgr,
		Tasks:      tasks.NewManager(storageDir),
		Memory:     memMgr,
		Proposals:  memory.NewProposalStore(storageDir, memMgr),
		Provider:   provider,
		Builder:    prompting.NewBuilder(),
		Classifier: memory.NewClassifier(provider),
	}, nil
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
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if chatOpts.Once {
				if strings.TrimSpace(chatOpts.Input) == "" {
					return app.NewError(app.CategoryCLI, "missing_input", "--input is required with --once", nil)
				}
				result, err := runChatExchange(cmd.Context(), rt, app.NewID("session"), chatOpts.Input, chatOpts.RenderPrompt)
				if err != nil {
					return err
				}
				return writeOutput(cmd.OutOrStdout(), opts.JSON, result, textChatResult(result))
			}
			return runREPL(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), rt)
		},
	}
	cmd.Flags().BoolVar(&chatOpts.Once, "once", false, "run one request")
	cmd.Flags().BoolVar(&chatOpts.Once, "non-interactive", false, "run one request")
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
}

func runChatExchange(ctx context.Context, rt *runtime, sessionID, input string, renderOnly bool) (chatResult, error) {
	profile, err := rt.Profiles.Active()
	if err != nil {
		return chatResult{}, err
	}
	taskState, _ := rt.Tasks.Current()
	var taskPtr *app.TaskState
	if taskState.ID != "" {
		taskPtr = &taskState
	}
	bundle, err := rt.Memory.SelectForPrompt(ctx, sessionID, taskID(taskPtr))
	if err != nil {
		return chatResult{}, err
	}
	messages, err := rt.Builder.Build(prompting.BuildInput{Profile: profile, Task: taskPtr, Memory: bundle, Query: input})
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
	res, err := rt.Provider.Complete(ctx, providers.CompletionRequest{Purpose: providers.PurposeChat, Model: rt.Config.ActiveModel, Messages: messages})
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
	proposal, err := rt.Classifier.Propose(ctx, memory.ClassificationInput{
		SessionID:          sessionID,
		UserMessageID:     userRecord.ID,
		AssistantMessageID: assistantRecord.ID,
		UserMessage:        input,
		AssistantMessage:   res.Message.Content,
		Profile:            profile,
		Task:               taskPtr,
		Model:              rt.Config.MemoryModel,
	})
	if err != nil {
		return result, err
	}
	if err := rt.Proposals.Save(ctx, proposal); err != nil {
		return result, err
	}
	result.Proposal = &proposal
	return result, nil
}

func runREPL(ctx context.Context, in io.Reader, out io.Writer, rt *runtime) error {
	sessionID := app.NewID("session")
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			done, err := handleSlash(ctx, out, rt, sessionID, line)
			if err != nil {
				_, _ = fmt.Fprintln(out, err.Error())
			}
			if done {
				return nil
			}
			continue
		}
		result, err := runChatExchange(ctx, rt, sessionID, line, false)
		if err != nil {
			_, _ = fmt.Fprintln(out, err.Error())
			continue
		}
		_, _ = fmt.Fprintln(out, result.Answer)
		if result.Proposal != nil {
			_, _ = fmt.Fprintf(out, "Memory proposal: %s (%d records)\n", result.Proposal.ID, len(result.Proposal.Records))
		}
	}
	return scanner.Err()
}

func profilesCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "profiles",
		Short: "List profiles",
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
}

func memoryCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "Inspect/propose/apply memory"}
	cmd.AddCommand(memoryListCommand(opts), memoryProposeCommand(opts), memoryApplyCommand(opts))
	return cmd
}

func memoryListCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list <short|work|long>",
		Short: "List memory layer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := newRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			layer := app.MemoryLayer(args[0])
			taskState, _ := rt.Tasks.Current()
			records, err := rt.Memory.List(cmd.Context(), layer, "", taskState.ID)
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "layer": layer, "records": records}, memoryText(records))
		},
	}
}

func memoryProposeCommand(opts *globalOptions) *cobra.Command {
	latest := false
	cmd := &cobra.Command{
		Use:   "propose",
		Short: "Propose memory from latest exchange",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !latest {
				return app.NewError(app.CategoryCLI, "missing_latest", "--latest is required for P0", nil)
			}
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
			proposal, err := rt.Classifier.Propose(cmd.Context(), memory.ClassificationInput{SessionID: sessionID, UserMessageID: userRecord.ID, AssistantMessageID: assistantRecord.ID, UserMessage: userRecord.Content, AssistantMessage: assistantRecord.Content, Profile: profile, Task: taskPtr, Model: rt.Config.MemoryModel})
			if err != nil {
				return err
			}
			if err := rt.Proposals.Save(cmd.Context(), proposal); err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), opts.JSON, map[string]any{"ok": true, "proposal": proposal}, proposalText(proposal))
		},
	}
	cmd.Flags().BoolVar(&latest, "latest", false, "use latest exchange")
	return cmd
}

func memoryApplyCommand(opts *globalOptions) *cobra.Command {
	var proposalID, accept, reject, edit string
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
			taskState, _ := rt.Tasks.Current()
			optsApply := memory.ApplyOptions{ProposalID: proposalID, AcceptAll: accept == "all", RejectIDs: map[string]bool{}, Edits: map[string]memory.ProposalEdit{}, TaskID: taskState.ID}
			if reject != "" {
				optsApply.RejectIDs[reject] = true
			}
			if edit != "" {
				id, parsed, err := parseEdit(edit)
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
	cmd.Flags().StringVar(&accept, "accept", "", "accept all")
	cmd.Flags().StringVar(&reject, "reject", "", "reject record id")
	cmd.Flags().StringVar(&edit, "edit", "", "edit record_id:layer=<layer>,content=<text>")
	return cmd
}

func taskCommand(opts *globalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Task state commands"}
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
	return cmd
}

func privacyCommand(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
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
}

func handleSlash(ctx context.Context, out io.Writer, rt *runtime, sessionID, line string) (bool, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false, nil
	}
	switch parts[0] {
	case "/exit":
		return true, nil
	case "/help":
		_, _ = fmt.Fprintln(out, "/model /profile /task /save /memory /clear /privacy /exit")
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
			now := time.Now().UTC()
			p := app.UserProfile{ID: id, DisplayName: id, Style: map[string]string{"language": "ru", "tone": "direct"}, ResponseFormat: map[string]string{"structure": "concise"}, Constraints: []string{"be concise"}, CreatedAt: now, UpdatedAt: now}
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
		record, err := rt.Memory.Save(ctx, memory.SaveInput{Layer: layer, Kind: "manual", Content: text, Source: "manual", SessionID: sessionID, TaskID: taskState.ID})
		if err != nil {
			return false, err
		}
		_, _ = fmt.Fprintf(out, "saved: %s\n", record.ID)
	case "/memory":
		return false, handleMemorySlash(ctx, out, rt, sessionID, parts)
	case "/clear":
		if len(parts) == 2 && parts[1] == "short" {
			return false, rt.Memory.ClearShort(ctx, sessionID)
		}
		return false, app.NewError(app.CategoryCLI, "unknown_command", "unknown clear command", nil)
	default:
		return false, app.NewError(app.CategoryCLI, "unknown_command", "unknown slash command", nil)
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
		_, _ = fmt.Fprintf(out, "current_step: %s\n", state.CurrentStep)
	case "expect":
		if len(parts) < 3 {
			return app.NewError(app.CategoryCLI, "missing_expected_action", "expected action required", nil)
		}
		state, err := rt.Tasks.SetExpectedAction(app.ExpectedAction(parts[2]))
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "expected_action: %s\n", state.ExpectedAction)
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
		_, _ = fmt.Fprintf(out, "status: %s\n", state.Status)
	case "resume":
		state, err := rt.Tasks.Resume()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "status: %s stage: %s\n", state.Status, state.Stage)
	default:
		return app.NewError(app.CategoryCLI, "unknown_task_command", "unknown task command", nil)
	}
	_ = rest
	return nil
}

func handleMemorySlash(ctx context.Context, out io.Writer, rt *runtime, sessionID string, parts []string) error {
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
		proposal, err := rt.Classifier.Propose(ctx, memory.ClassificationInput{SessionID: sessionID, UserMessageID: userRecord.ID, AssistantMessageID: assistantRecord.ID, UserMessage: userRecord.Content, AssistantMessage: assistantRecord.Content, Profile: profile, Task: taskPtr, Model: rt.Config.MemoryModel})
		if err != nil {
			return err
		}
		if err := rt.Proposals.Save(ctx, proposal); err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, proposalText(proposal))
	case "apply":
		taskState, _ := rt.Tasks.Current()
		result, err := rt.Proposals.Apply(ctx, memory.ApplyOptions{ProposalID: "latest", AcceptAll: true, SessionID: sessionID, TaskID: taskState.ID})
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "saved %d records\n", len(result.SavedRecords))
	default:
		return app.NewError(app.CategoryCLI, "unknown_memory_command", "unknown memory command", nil)
	}
	return nil
}

func taskID(task *app.TaskState) string {
	if task == nil {
		return ""
	}
	return task.ID
}

func parseEdit(raw string) (string, memory.ProposalEdit, error) {
	id, body, ok := strings.Cut(raw, ":")
	if !ok || id == "" {
		return "", memory.ProposalEdit{}, app.NewError(app.CategoryCLI, "invalid_edit", "edit must be record_id:layer=<layer>,content=<text>", nil)
	}
	edit := memory.ProposalEdit{}
	for _, part := range strings.Split(body, ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "layer":
			edit.Layer = app.ProposedMemoryLayer(value)
		case "content":
			edit.Content = value
		}
	}
	return id, edit, nil
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
		return result.RenderedPrompt
	}
	return result.Answer + "\n"
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

func memoryText(records []app.MemoryRecord) string {
	var b strings.Builder
	for _, record := range records {
		b.WriteString("[")
		b.WriteString(string(record.Layer))
		b.WriteString("] ")
		b.WriteString(record.Kind)
		b.WriteString(": ")
		b.WriteString(record.Content)
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
		b.WriteString(record.Kind)
		b.WriteString(": ")
		b.WriteString(record.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func taskText(state app.TaskState) string {
	return fmt.Sprintf("task=%s title=%s stage=%s current_step=%s expected_action=%s status=%s allowed=%v\n", state.ID, state.Title, state.Stage, state.CurrentStep, state.ExpectedAction, state.Status, tasks.AllowedNext(state.Stage))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
