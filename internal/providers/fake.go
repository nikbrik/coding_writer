package providers

import (
	"context"
	"strings"
	"sync"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/validation"
)

type FakeProvider struct {
	mu                  sync.Mutex
	Calls               []CompletionRequest
	Models              []string
	ChatResponse        string
	ChatResponses       []string
	ClassifierResponse  string
	ClassifierResponses []string
	Err                 error
	chatCallIdx         int
	classifierCallIdx   int
}

func NewFakeProvider() *FakeProvider {
	return &FakeProvider{Models: []string{"openai/gpt-4.1-mini", "fake/model"}}
}

func (p *FakeProvider) ListModels(ctx context.Context) ([]string, error) {
	if p.Err != nil {
		return nil, p.Err
	}
	if len(p.Models) == 0 {
		return []string{"fake/model"}, nil
	}
	return append([]string(nil), p.Models...), nil
}

func (p *FakeProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	sanitized := req
	sanitized.Messages = sanitizeMessages(req.Messages)
	p.mu.Lock()
	p.Calls = append(p.Calls, sanitized)
	p.mu.Unlock()
	if p.Err != nil {
		return CompletionResponse{}, p.Err
	}
	if req.Purpose == PurposeClassifier {
		var content string
		if len(p.ClassifierResponses) > 0 {
			p.mu.Lock()
			if p.classifierCallIdx < len(p.ClassifierResponses) {
				content = p.ClassifierResponses[p.classifierCallIdx]
				p.classifierCallIdx++
			}
			p.mu.Unlock()
		}
		if content == "" {
			content = p.ClassifierResponse
		}
		if content == "" {
			content = defaultClassifierJSON(joinMessages(sanitized.Messages))
		}
		return newAssistantMessage(content, req.Model, "fake"), nil
	}
	var content string
	if len(p.ChatResponses) > 0 {
		p.mu.Lock()
		if p.chatCallIdx < len(p.ChatResponses) {
			content = p.ChatResponses[p.chatCallIdx]
			p.chatCallIdx++
		}
		p.mu.Unlock()
	}
	if content == "" {
		content = p.ChatResponse
	}
	if content == "" {
		prompt := joinMessages(sanitized.Messages)
		if req.JSONMode {
			content = defaultStructuredChatAnswer(prompt)
		} else {
			content = defaultChatAnswer(prompt)
		}
	}
	return newAssistantMessage(content, req.Model, "fake"), nil
}

func (p *FakeProvider) SnapshotCalls() []CompletionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]CompletionRequest, len(p.Calls))
	copy(out, p.Calls)
	return out
}

func sanitizeMessages(messages []app.ChatMessage) []app.ChatMessage {
	out := make([]app.ChatMessage, len(messages))
	for i, msg := range messages {
		msg.Content, _ = validation.RedactText(msg.Content)
		out[i] = msg
	}
	return out
}

func joinMessages(messages []app.ChatMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(string(msg.Role))
		b.WriteString(": ")
		b.WriteString(msg.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func defaultChatAnswer(prompt string) string {
	lower := strings.ToLower(prompt)
	parts := []string{"fake assistant response"}
	if strings.Contains(prompt, `Profile: student`) || strings.Contains(prompt, `"id": "student"`) || strings.Contains(prompt, "&#34;id&#34;: &#34;student&#34;") || strings.Contains(lower, "student") && strings.Contains(lower, "teacher") {
		parts = append(parts, "student profile: подробно, с шагами и примерами")
	}
	if strings.Contains(prompt, `Profile: senior`) || strings.Contains(prompt, `"id": "senior"`) || strings.Contains(prompt, "&#34;id&#34;: &#34;senior&#34;") || strings.Contains(lower, "senior") && strings.Contains(lower, "trade") {
		parts = append(parts, "senior profile: кратко, риски и trade-offs")
	}
	if strings.Contains(prompt, "CLI должен поддерживать выбор модели OpenRouter") {
		parts = append(parts, "учтено требование выбора модели OpenRouter")
	}
	if strings.Contains(prompt, "Пользователь предпочитает короткие ответы на русском") || strings.Contains(prompt, "короткие ответы на русском") {
		parts = append(parts, "учтено предпочтение: коротко на русском")
	}
	if strings.Contains(prompt, `"stage": "execution"`) || strings.Contains(prompt, "&#34;stage&#34;: &#34;execution&#34;") {
		parts = append(parts, "продолжаю execution без повторного объяснения")
	}
	if strings.Contains(prompt, "task paused") {
		parts = append(parts, "задача paused: нужна /task resume")
	}
	return strings.Join(parts, "; ")
}

func defaultStructuredChatAnswer(prompt string) string {
	lower := strings.ToLower(prompt)
	switch {
	case strings.Contains(lower, "current stage: planning"):
		return `{"stage":"planning","summary":"fake planning response","assumptions":[],"acceptance_criteria":["criteria captured"],"plan":["proposed step"],"open_questions":[],"readiness":"needs_user_input"}`
	case strings.Contains(lower, "current stage: execution"):
		return `{"stage":"execution","summary":"fake execution response","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`
	case strings.Contains(lower, "current stage: validation"):
		return `{"stage":"validation","findings":[],"passed_checks":[],"missing_evidence":["no tool evidence provided"],"residual_risks":[],"verdict":"blocked_missing_evidence"}`
	case strings.Contains(lower, "current stage: done"):
		return `{"stage":"done","summary":"fake done response","acceptance_status":[],"validation_evidence":[],"follow_up_task_proposals":[]}`
	default:
		return `{"stage":"planning","summary":"fake planning response","assumptions":[],"acceptance_criteria":["criteria captured"],"plan":["proposed step"],"open_questions":[],"readiness":"needs_user_input"}`
	}
}

func defaultClassifierJSON(prompt string) string {
	if strings.Contains(prompt, "no-memory") {
		return `{"records":[]}`
	}
	return `{"records":[{"layer":"short","kind":"context","content":"В текущем диалоге планируем модуль памяти.","reason":"Текущий session context.","confidence":0.82},{"layer":"work","kind":"requirement","content":"CLI должен поддерживать выбор модели OpenRouter.","reason":"Требование текущей задачи.","confidence":0.91},{"layer":"long","kind":"preference","content":"Пользователь предпочитает короткие ответы на русском.","reason":"Стабильное предпочтение пользователя.","confidence":0.88},{"layer":"ignore","kind":"smalltalk","content":"Низкоценный шум диалога.","reason":"Не влияет на будущие ответы.","confidence":0.4}]}`
}
