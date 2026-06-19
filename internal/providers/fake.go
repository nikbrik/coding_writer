package providers

import (
	"context"
	"encoding/json"
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
	ValidatorResponse   string
	ValidatorResponses  []string
	Err                 error
	chatCallIdx         int
	classifierCallIdx   int
	validatorCallIdx    int
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
	if req.Purpose == PurposeValidator {
		var content string
		if len(p.ValidatorResponses) > 0 {
			p.mu.Lock()
			if p.validatorCallIdx < len(p.ValidatorResponses) {
				content = p.ValidatorResponses[p.validatorCallIdx]
				p.validatorCallIdx++
			}
			p.mu.Unlock()
		}
		if content == "" {
			content = p.ValidatorResponse
		}
		if content == "" {
			content = defaultValidatorJSON(joinMessages(sanitized.Messages))
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
	if strings.Contains(lower, "agent role: requirements_specialist") {
		return `{"role":"requirements_specialist","summary":"requirements reviewed","findings":[],"proposed_plan":["clarify scope","implement change"],"proposed_acceptance_criteria":["criteria captured"]}`
	}
	if strings.Contains(lower, "agent role: code_research_specialist") {
		return `{"role":"code_research_specialist","summary":"code surface reviewed","findings":[],"proposed_plan":["inspect current modules","integrate changes"],"proposed_acceptance_criteria":["code path identified"]}`
	}
	if strings.Contains(lower, "agent role: architecture_specialist") {
		return `{"role":"architecture_specialist","summary":"architecture reviewed","findings":[],"proposed_plan":["preserve current architecture"],"proposed_acceptance_criteria":["no layering regressions"]}`
	}
	if strings.Contains(lower, "agent role: test_validation_specialist") {
		return `{"role":"test_validation_specialist","summary":"tests reviewed","findings":[],"proposed_plan":["add tests"],"proposed_acceptance_criteria":["go test ./... passes"]}`
	}
	if strings.Contains(lower, "agent role: risk_regression_specialist") {
		return `{"role":"risk_regression_specialist","summary":"risks reviewed","findings":[],"proposed_plan":["verify days 11-14"],"proposed_acceptance_criteria":["no regressions"]}`
	}
	if strings.Contains(lower, "agent role: planning_orchestrator") {
		if strings.Contains(lower, "manual_scratch/day15_contains_duplicate") {
			return `{"stage":"planning","summary":"solve Contains Duplicate in manual_scratch/day15_contains_duplicate","assumptions":[],"acceptance_criteria":["ContainsDuplicate(nums []int) bool uses an O(n) map/set approach","table tests cover empty, single, duplicate positive, duplicate negative, and no duplicate","go test ./manual_scratch/day15_contains_duplicate passes"],"plan":["implement ContainsDuplicate with a seen map","add table-driven tests for the required cases","run go test ./manual_scratch/day15_contains_duplicate as trusted verification","review evidence and finish only if checks pass"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
		}
		if strings.Contains(lower, "manual_scratch/day14_stock_profit") {
			return `{"stage":"planning","summary":"verify manual_scratch/day14_stock_profit with go test","assumptions":[],"acceptance_criteria":["go test ./manual_scratch/day14_stock_profit passes"],"plan":["inspect the existing package goal","run go test ./manual_scratch/day14_stock_profit as trusted verification","review evidence and finish only if checks pass"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
		}
		if strings.Contains(lower, "go version") {
			return `{"stage":"planning","summary":"verify go version with trusted evidence","assumptions":[],"acceptance_criteria":["go version passes"],"plan":["run go version as trusted verification","review evidence and finish only if checks pass"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
		}
		return `{"stage":"planning","summary":"fake planning response","assumptions":[],"acceptance_criteria":["criteria captured"],"plan":["proposed step"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
	}
	switch {
	case strings.Contains(lower, "current stage: planning"):
		if strings.Contains(lower, "manual_scratch/day15_contains_duplicate") {
			return `{"stage":"planning","summary":"solve Contains Duplicate in manual_scratch/day15_contains_duplicate","assumptions":[],"acceptance_criteria":["ContainsDuplicate(nums []int) bool uses an O(n) map/set approach","table tests cover empty, single, duplicate positive, duplicate negative, and no duplicate","go test ./manual_scratch/day15_contains_duplicate passes"],"plan":["implement ContainsDuplicate with a seen map","add table-driven tests for the required cases","run go test ./manual_scratch/day15_contains_duplicate as trusted verification","review evidence and finish only if checks pass"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
		}
		if strings.Contains(lower, "manual_scratch/day14_stock_profit") {
			return `{"stage":"planning","summary":"verify manual_scratch/day14_stock_profit with go test","assumptions":[],"acceptance_criteria":["go test ./manual_scratch/day14_stock_profit passes"],"plan":["inspect the existing package goal","run go test ./manual_scratch/day14_stock_profit as trusted verification","review evidence and finish only if checks pass"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
		}
		if strings.Contains(lower, "go version") {
			return `{"stage":"planning","summary":"verify go version with trusted evidence","assumptions":[],"acceptance_criteria":["go version passes"],"plan":["run go version as trusted verification","review evidence and finish only if checks pass"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
		}
		if strings.Contains(lower, "реализовать memorymanager") || strings.Contains(lower, "memorymanager") {
			return `{"stage":"planning","summary":"реализовать MemoryManager","assumptions":[],"acceptance_criteria":["task state persists across restart"],"plan":["реализовать MemoryManager"],"open_questions":[],"readiness":"ready_for_execution_proposal"}`
		}
		return `{"stage":"planning","summary":"fake planning response","assumptions":[],"acceptance_criteria":["criteria captured"],"plan":["proposed step"],"open_questions":[],"readiness":"needs_user_input"}`
	case strings.Contains(lower, "current stage: execution"):
		if strings.Contains(lower, "manual_scratch/day15_contains_duplicate") {
			return `{"stage":"execution","summary":"Contains Duplicate implementation prepared","deliverable":"\u0060\u0060\u0060go\nfunc ContainsDuplicate(nums []int) bool {\n\tseen := make(map[int]struct{}, len(nums))\n\tfor _, n := range nums {\n\t\tif _, ok := seen[n]; ok {\n\t\t\treturn true\n\t\t}\n\t\tseen[n] = struct{}{}\n\t}\n\treturn false\n}\n\u0060\u0060\u0060","current_step":"implement ContainsDuplicate with tests","completed_steps":["implemented O(n) map/set solution","covered required table tests"],"next_step":"","changed_artifacts":["manual_scratch/day15_contains_duplicate/contains_duplicate.go","manual_scratch/day15_contains_duplicate/contains_duplicate_test.go"],"verification":["go test ./manual_scratch/day15_contains_duplicate"],"blockers":[],"next_signal":"ready_for_validation"}`
		}
		if strings.Contains(lower, "готово к проверке") || strings.Contains(lower, "ready for validation") {
			return `{"stage":"execution","summary":"fake execution ready for validation","deliverable":"\u0060\u0060\u0060go\npackage main\n\u0060\u0060\u0060","current_step":"proposed step","completed_steps":["proposed step"],"next_step":"","changed_artifacts":["internal/memory/manager.go"],"verification":["not run"],"blockers":[],"next_signal":"ready_for_validation"}`
		}
		return `{"stage":"execution","summary":"fake execution response","deliverable":"\u0060\u0060\u0060go\npackage main\n\u0060\u0060\u0060","changed_artifacts":[],"verification":["not run"],"blockers":[],"next_signal":"continue_execution"}`
	case strings.Contains(lower, "current stage: validation"):
		if strings.Contains(lower, "проверь и заверши") || strings.Contains(lower, "проверь критерии и заверши") || strings.Contains(lower, "заверши") || strings.Contains(lower, "verify and finish") {
			return `{"stage":"validation","findings":[],"passed_checks":["tool evidence available"],"missing_evidence":[],"residual_risks":[],"verdict":"ready_for_done"}`
		}
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

func defaultValidatorJSON(prompt string) string {
	lower := strings.ToLower(prompt)
	if strings.Contains(lower, "you improve user prompts for an internal orchestrator") {
		return `{"improved_prompt":"` + escapeJSON(promptImproverOriginalFromPrompt(prompt)) + `","preserved_intent":true,"added_requirements":[],"removed_requirements":[],"clarifications":[],"rationale":"fake prompt improvement"}`
	}
	if strings.Contains(lower, "verification command planner") {
		switch {
		case strings.Contains(lower, "manual_scratch/day15_contains_duplicate"):
			return `{"command":"go test ./manual_scratch/day15_contains_duplicate","confidence":0.96,"reason":"fake planner selected exact package verification from task context"}`
		case strings.Contains(lower, "manual_scratch/day14_stock_profit"):
			return `{"command":"go test ./manual_scratch/day14_stock_profit","confidence":0.96,"reason":"fake planner selected exact package verification from task context"}`
		case strings.Contains(lower, "python") || strings.Contains(lower, "pytest"):
			return `{"command":"python -m pytest","confidence":0.9,"reason":"fake planner selected pytest verification"}`
		case strings.Contains(lower, "npm") || strings.Contains(lower, "package.json"):
			return `{"command":"npm test","confidence":0.9,"reason":"fake planner selected npm test verification"}`
		case strings.Contains(lower, "cargo") || strings.Contains(lower, "rust"):
			return `{"command":"cargo test","confidence":0.9,"reason":"fake planner selected cargo test verification"}`
		default:
			return `{"command":"","confidence":0,"reason":"fake planner found no safe exact verification command"}`
		}
	}
	if strings.Contains(lower, "planning approval referee") {
		return `{"verdict":"approved","confidence":0.9,"reason":"fake approval"}`
	}
	if strings.Contains(lower, "prompt-improvement equivalence referee") {
		return `{"verdict":"pass","reason":"fake equivalent"}`
	}
	if strings.Contains(lower, "out-of-band invariant policy referee") {
		text := strings.ToLower(invariantTextFromPrompt(prompt))
		if (strings.Contains(text, "brute force") || strings.Contains(text, "o(n^2)")) && strings.Contains(prompt, "algorithm.no_bruteforce") {
			return `{"violations":[{"invariant_id":"algorithm.no_bruteforce","severity":"block","problem":"text asks for brute-force stock-profit behavior forbidden by the algorithm invariant","evidence":"semantic conflict"}]}`
		}
		if containsStackForbiddenTerm(text) || strings.Contains(text, "brute force") || strings.Contains(text, "o(n^2)") {
			return `{"violations":[{"invariant_id":"stack.go","severity":"block","problem":"text asks for behavior that conflicts with the Go MVP stack invariant","evidence":"semantic conflict"}]}`
		}
		return `{"violations":[]}`
	}
	if strings.Contains(lower, "out-of-band intent referee") {
		action := "answer_question"
		for _, candidate := range []string{"answer_question", "plan_task", "ask_clarification", "execute_plan_step", "summarize_execution", "review_output", "verify_criteria", "summarize_done", "propose_transition"} {
			if strings.Contains(prompt, `"deterministic":"`+candidate+`"`) {
				action = candidate
				break
			}
		}
		userInput := strings.ToLower(semanticUserInputFromPrompt(prompt))
		signal := "none"
		if fakeIntentNegatesTransition(userInput) {
			signal = "none"
			if fakeValidationReviewIntent(userInput) {
				action = "review_output"
			}
		} else {
			if strings.Contains(userInput, "ready for validation") || strings.Contains(userInput, "ready to validate") || strings.Contains(userInput, "готово к проверке") || strings.Contains(userInput, "work is complete") || strings.Contains(userInput, "please review it now") {
				signal = "ready_for_validation"
			}
			if strings.Contains(userInput, "verify and finish") || strings.Contains(userInput, "verify and complete") || strings.Contains(userInput, "finish") || strings.Contains(userInput, "complete") || strings.Contains(userInput, "проверь и заверши") || strings.Contains(userInput, "заверши") {
				signal = "ready_for_done"
			}
		}
		return `{"action_kind":"` + action + `","transition_signal":"` + signal + `","confidence":0.8,"reason":"fake deterministic intent"}`
	}
	return `{"verdict":"pass","findings":[]}`
}

func fakeIntentNegatesTransition(userInput string) bool {
	for _, phrase := range []string{
		"not yet",
		"do not finish",
		"don't finish",
		"do not complete",
		"don't complete",
		"but do not finish",
		"пока не завершай",
		"не завершай",
		"не закрывай",
		"пока не закрывай",
	} {
		if strings.Contains(userInput, phrase) {
			return true
		}
	}
	return false
}

func fakeValidationReviewIntent(userInput string) bool {
	for _, phrase := range []string{"review", "validation review", "verify criteria", "проверь", "проверить", "критери"} {
		if strings.Contains(userInput, phrase) {
			return true
		}
	}
	return false
}

func promptImproverOriginalFromPrompt(prompt string) string {
	idx := strings.LastIndex(prompt, `"original":`)
	if idx < 0 {
		return "improved prompt"
	}
	payload := prompt[idx:]
	var decoded map[string]any
	if err := json.Unmarshal([]byte("{"+strings.TrimLeft(payload, "{")), &decoded); err == nil {
		if original, ok := decoded["original"].(string); ok && strings.TrimSpace(original) != "" {
			return original
		}
	}
	return "improved prompt"
}

func escapeJSON(s string) string {
	data, _ := json.Marshal(s)
	if len(data) >= 2 {
		return string(data[1 : len(data)-1])
	}
	return s
}

func invariantTextFromPrompt(prompt string) string {
	idx := strings.LastIndex(prompt, "user: ")
	if idx < 0 {
		return ""
	}
	payload := strings.TrimSpace(prompt[idx+len("user: "):])
	if nl := strings.IndexByte(payload, '\n'); nl >= 0 {
		payload = payload[:nl]
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return ""
	}
	if value, ok := decoded["text"].(string); ok {
		return value
	}
	return ""
}

func containsStackForbiddenTerm(text string) bool {
	replacer := strings.NewReplacer(".", " ", ",", " ", "!", " ", "?", " ", ";", " ", ":", " ", "\n", " ", "\t", " ", "/", " ", "\\", " ", "-", " ", "_", " ")
	for _, token := range strings.Fields(replacer.Replace(strings.ToLower(text))) {
		switch token {
		case "python", "node", "rust":
			return true
		}
	}
	return false
}

func semanticUserInputFromPrompt(prompt string) string {
	idx := strings.LastIndex(prompt, "user: ")
	if idx < 0 {
		return ""
	}
	payload := strings.TrimSpace(prompt[idx+len("user: "):])
	if nl := strings.IndexByte(payload, '\n'); nl >= 0 {
		payload = payload[:nl]
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return ""
	}
	if value, ok := decoded["user_input"].(string); ok {
		return value
	}
	return ""
}
