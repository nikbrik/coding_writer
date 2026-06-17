package providers

import (
	"context"
	"strings"
	"sync"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/validation"
)

type FakeProvider struct {
	mu                 sync.Mutex
	Calls              []CompletionRequest
	Models             []string
	ChatResponse       string
	ClassifierResponse string
	Err                error
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
	if p.Err != nil {
		return CompletionResponse{}, p.Err
	}
	sanitized := req
	sanitized.Messages = sanitizeMessages(req.Messages)
	p.mu.Lock()
	p.Calls = append(p.Calls, sanitized)
	p.mu.Unlock()
	if req.Purpose == PurposeClassifier {
		content := p.ClassifierResponse
		if content == "" {
			content = defaultClassifierJSON(joinMessages(sanitized.Messages))
		}
		return newAssistantMessage(content, req.Model, "fake"), nil
	}
	content := p.ChatResponse
	if content == "" {
		content = defaultChatAnswer(joinMessages(sanitized.Messages))
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
	if strings.Contains(prompt, `"id": "student"`) || strings.Contains(prompt, "&#34;id&#34;: &#34;student&#34;") || strings.Contains(lower, "student") && strings.Contains(lower, "teacher") {
		parts = append(parts, "student profile: подробно, с шагами и примерами")
	}
	if strings.Contains(prompt, `"id": "senior"`) || strings.Contains(prompt, "&#34;id&#34;: &#34;senior&#34;") || strings.Contains(lower, "senior") && strings.Contains(lower, "trade") {
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

func defaultClassifierJSON(prompt string) string {
	if strings.Contains(prompt, "no-memory") {
		return `{"records":[]}`
	}
	return `{"records":[{"layer":"short","kind":"context","content":"В текущем диалоге планируем модуль памяти.","reason":"Текущий session context.","confidence":0.82},{"layer":"work","kind":"requirement","content":"CLI должен поддерживать выбор модели OpenRouter.","reason":"Требование текущей задачи.","confidence":0.91},{"layer":"long","kind":"preference","content":"Пользователь предпочитает короткие ответы на русском.","reason":"Стабильное предпочтение пользователя.","confidence":0.88},{"layer":"ignore","kind":"smalltalk","content":"Низкоценный шум диалога.","reason":"Не влияет на будущие ответы.","confidence":0.4}]}`
}
