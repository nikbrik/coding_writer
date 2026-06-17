package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/memory"
	"github.com/nikbrik/coding_writer/internal/profiles"
	"github.com/nikbrik/coding_writer/internal/prompting"
	"github.com/nikbrik/coding_writer/internal/providers"
	"github.com/nikbrik/coding_writer/internal/tasks"
)

func TestDay11EndToEndMemoryProposalApplyInfluence(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	taskState, err := rt.tasks.Start("CLI assistant MVP")
	if err != nil {
		t.Fatal(err)
	}
	if taskState, err = rt.tasks.Move(app.StageExecution); err != nil {
		t.Fatal(err)
	}
	sessionID := "session_day11"
	query := "Спланируй модуль памяти. Требование: CLI должен поддерживать выбор модели OpenRouter. Я предпочитаю короткие ответы на русском."
	profile, _ := rt.profiles.Active()
	bundle, _ := rt.memory.SelectForPrompt(ctx, sessionID, taskState.ID, profile.ID)
	messages, err := rt.builder.Build(prompting.BuildInput{Profile: profile, Task: &taskState, Memory: bundle, Query: query})
	if err != nil {
		t.Fatal(err)
	}
	chatRes, err := rt.provider.Complete(ctx, providers.CompletionRequest{Purpose: providers.PurposeChat, Model: "fake/model", Messages: messages})
	if err != nil {
		t.Fatal(err)
	}
	userRecord, err := rt.memory.Save(ctx, memory.SaveInput{Layer: app.LayerShort, Kind: "message_user", Content: query, Source: "chat", SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	assistantRecord, err := rt.memory.Save(ctx, memory.SaveInput{Layer: app.LayerShort, Kind: "message_assistant", Content: chatRes.Message.Content, Source: "chat", SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := rt.classifier.Propose(ctx, memory.ClassificationInput{SessionID: sessionID, UserMessageID: userRecord.ID, AssistantMessageID: assistantRecord.ID, UserMessage: query, AssistantMessage: chatRes.Message.Content, Profile: profile, Task: &taskState, Model: "fake/model"})
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.Records) != 4 {
		t.Fatalf("want short/work/long/ignore proposal, got %+v", proposal.Records)
	}
	if err := rt.proposals.Save(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	applyResult, err := rt.proposals.Apply(ctx, memory.ApplyOptions{ProposalID: proposal.ID, AcceptAll: true, SessionID: sessionID, TaskID: taskState.ID, ProfileID: profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(applyResult.SavedRecords) != 3 {
		t.Fatalf("ignore must not save physically, saved=%d", len(applyResult.SavedRecords))
	}
	shortRecords, _ := rt.memory.List(ctx, app.LayerShort, sessionID, "")
	workRecords, _ := rt.memory.List(ctx, app.LayerWork, "", taskState.ID)
	longRecords, _ := rt.memory.List(ctx, app.LayerLong, "", "")
	if !containsContent(shortRecords, "В текущем диалоге") || !containsContent(workRecords, "выбор модели OpenRouter") || !containsContent(longRecords, "короткие ответы на русском") {
		t.Fatalf("records not routed: short=%+v work=%+v long=%+v", shortRecords, workRecords, longRecords)
	}
	for _, layerRecords := range [][]app.MemoryRecord{shortRecords, workRecords, longRecords} {
		for _, record := range layerRecords {
			if strings.Contains(record.Content, "Низкоценный шум") || record.Layer == app.MemoryLayer("ignore") {
				t.Fatalf("ignore leaked into memory layer: %+v", record)
			}
		}
	}
	bundle, _ = rt.memory.SelectForPrompt(ctx, sessionID, taskState.ID, profile.ID)
	nextMessages, err := rt.builder.Build(prompting.BuildInput{Profile: profile, Task: &taskState, Memory: bundle, Query: "Продолжай задачу."})
	if err != nil {
		t.Fatal(err)
	}
	nextRes, err := rt.provider.Complete(ctx, providers.CompletionRequest{Purpose: providers.PurposeChat, Model: "fake/model", Messages: nextMessages})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nextRes.Message.Content, "учтено требование выбора модели OpenRouter") || !strings.Contains(nextRes.Message.Content, "коротко на русском") {
		t.Fatalf("memory did not influence answer: %s", nextRes.Message.Content)
	}
	if err := rt.memory.ClearShort(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	shortRecords, _ = rt.memory.List(ctx, app.LayerShort, sessionID, "")
	workRecords, _ = rt.memory.List(ctx, app.LayerWork, "", taskState.ID)
	longRecords, _ = rt.memory.List(ctx, app.LayerLong, "", "")
	if len(shortRecords) != 0 || len(workRecords) == 0 || len(longRecords) == 0 {
		t.Fatalf("clear short failed: short=%d work=%d long=%d", len(shortRecords), len(workRecords), len(longRecords))
	}
}

func TestDay12ProfilesChangePromptAndResponse(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	student, err := rt.profiles.SetActive("student")
	if err != nil {
		t.Fatal(err)
	}
	studentMessages, err := rt.builder.Build(prompting.BuildInput{Profile: student, Query: "Объясни архитектуру memory layers."})
	if err != nil {
		t.Fatal(err)
	}
	studentResponse, _ := rt.provider.Complete(ctx, providers.CompletionRequest{Purpose: providers.PurposeChat, Model: "fake/model", Messages: studentMessages})
	senior, err := rt.profiles.SetActive("senior")
	if err != nil {
		t.Fatal(err)
	}
	seniorMessages, err := rt.builder.Build(prompting.BuildInput{Profile: senior, Query: "Объясни архитектуру memory layers."})
	if err != nil {
		t.Fatal(err)
	}
	seniorResponse, _ := rt.provider.Complete(ctx, providers.CompletionRequest{Purpose: providers.PurposeChat, Model: "fake/model", Messages: seniorMessages})
	studentPrompt := prompting.RenderMessages(studentMessages)
	seniorPrompt := prompting.RenderMessages(seniorMessages)
	if studentPrompt == seniorPrompt || studentResponse.Message.Content == seniorResponse.Message.Content {
		t.Fatalf("profiles did not change prompt/response")
	}
	if !strings.Contains(studentPrompt, "profile.active") || !strings.Contains(seniorPrompt, "profile.active") {
		t.Fatal("profile block missing")
	}
}

func TestDay13PauseResumeAfterRestartUsesWorkingMemory(t *testing.T) {
	ctx := context.Background()
	rt := newAcceptanceRuntime(t)
	state, err := rt.tasks.Start("CLI assistant MVP")
	if err != nil {
		t.Fatal(err)
	}
	state, _ = rt.tasks.SetStep("реализовать MemoryManager")
	state, _ = rt.tasks.SetExpectedAction(app.ExpectedLLMResponse)
	state, _ = rt.tasks.Move(app.StageExecution)
	if _, err := rt.memory.Save(ctx, memory.SaveInput{Layer: app.LayerWork, Kind: "requirement", Content: "Acceptance: memory layers must be separate files", Source: "test", TaskID: state.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.tasks.Pause(); err != nil {
		t.Fatal(err)
	}
	restartedTasks := tasks.NewManager(rt.dir)
	resumed, err := restartedTasks.Resume()
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Stage != app.StageExecution || resumed.CurrentStep != "реализовать MemoryManager" || resumed.ExpectedAction != app.ExpectedLLMResponse {
		t.Fatalf("resume lost context: %+v", resumed)
	}
	profile, _ := rt.profiles.Active()
	bundle, _ := rt.memory.SelectForPrompt(ctx, "session_day13", resumed.ID, profile.ID)
	messages, err := rt.builder.Build(prompting.BuildInput{Profile: profile, Task: &resumed, Memory: bundle, Query: "Продолжай задачу."})
	if err != nil {
		t.Fatal(err)
	}
	rendered := prompting.RenderMessages(messages)
	if !strings.Contains(rendered, "реализовать MemoryManager") || !strings.Contains(rendered, "Acceptance: memory layers must be separate files") {
		t.Fatalf("prompt missing resumed task/work memory:\n%s", rendered)
	}
	res, _ := rt.provider.Complete(ctx, providers.CompletionRequest{Purpose: providers.PurposeChat, Model: "fake/model", Messages: messages})
	if !strings.Contains(res.Message.Content, "продолжаю execution") {
		t.Fatalf("fake provider did not see resumed execution: %s", res.Message.Content)
	}
}

type acceptanceRuntime struct {
	dir        string
	cfg        *app.ConfigManager
	profiles   *profiles.Manager
	tasks      *tasks.Manager
	memory     *memory.Manager
	proposals  *memory.ProposalStore
	provider   *providers.FakeProvider
	classifier *memory.Classifier
	builder    *prompting.Builder
}

func newAcceptanceRuntime(t *testing.T) acceptanceRuntime {
	t.Helper()
	dir := t.TempDir()
	cfg := app.NewConfigManager(dir)
	if err := cfg.EnsureStorageTree(); err != nil {
		t.Fatal(err)
	}
	profMgr := profiles.NewManager(dir, cfg)
	if err := profMgr.EnsureDefaults(); err != nil {
		t.Fatal(err)
	}
	loaded, _ := cfg.Load()
	loaded.ActiveProfileID = "student"
	loaded.ActiveModel = "fake/model"
	loaded.MemoryModel = "fake/model"
	if err := cfg.Save(loaded); err != nil {
		t.Fatal(err)
	}
	memMgr := memory.NewManager(dir)
	fake := providers.NewFakeProvider()
	return acceptanceRuntime{dir: dir, cfg: cfg, profiles: profMgr, tasks: tasks.NewManager(dir), memory: memMgr, proposals: memory.NewProposalStore(dir, memMgr), provider: fake, classifier: memory.NewClassifier(fake), builder: prompting.NewBuilder()}
}

func containsContent(records []app.MemoryRecord, needle string) bool {
	for _, record := range records {
		if strings.Contains(record.Content, needle) {
			return true
		}
	}
	return false
}
