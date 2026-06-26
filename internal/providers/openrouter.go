package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

const (
	maxModelsResponseSize    = 10 * 1024 * 1024
	maxCompletionResonseSize = 50 * 1024 * 1024
	maxRetryAfterDelay       = 5 * time.Second
	defaultOpenRouterTimeout = 120 * time.Second
)

type OpenRouterProvider struct {
	BaseURL string
	Client  *http.Client
}

func NewOpenRouterProvider(baseURL string) *OpenRouterProvider {
	if baseURL == "" {
		baseURL = app.DefaultOpenRouterBaseURL
	}
	return &OpenRouterProvider{BaseURL: strings.TrimRight(baseURL, "/"), Client: &http.Client{Timeout: defaultOpenRouterTimeout}}
}

func (p *OpenRouterProvider) ListModels(ctx context.Context) ([]string, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, app.ErrorWithHint(app.CategoryProvider, "missing_api_key", "OPENROUTER_API_KEY is required", "export OPENROUTER_API_KEY=...", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/models", nil)
	if err != nil {
		return nil, app.NewError(app.CategoryProvider, "request_build", err.Error(), err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := p.client().Do(req)
	if err != nil {
		return nil, providerTransportError(err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, app.NewError(app.CategoryProvider, "auth", "OpenRouter authorization failed", nil)
	}
	if res.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, app.NewError(app.CategoryProvider, "http", fmt.Sprintf("OpenRouter status %d", res.StatusCode), nil)
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxModelsResponseSize)).Decode(&parsed); err != nil {
		return nil, app.NewError(app.CategoryProvider, "malformed_response", err.Error(), err)
	}
	models := make([]string, 0, len(parsed.Data))
	for _, model := range parsed.Data {
		models = append(models, model.ID)
	}
	return models, nil
}

func (p *OpenRouterProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return CompletionResponse{}, app.ErrorWithHint(app.CategoryProvider, "missing_api_key", "OPENROUTER_API_KEY is required", "export OPENROUTER_API_KEY=...", nil)
	}
	if req.Model == "" {
		return CompletionResponse{}, app.NewError(app.CategoryProvider, "missing_model", "active model is required", nil)
	}
	req.Messages = sanitizeMessages(req.Messages)
	body := map[string]any{
		"model":    req.Model,
		"messages": toOpenRouterMessages(req.Messages),
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		if req.ToolChoice != nil {
			body["tool_choice"] = req.ToolChoice
		}
		if req.ParallelToolCalls != nil {
			body["parallel_tool_calls"] = *req.ParallelToolCalls
		}
	}
	if req.JSONMode {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return CompletionResponse{}, app.NewError(app.CategoryProvider, "request_encode", err.Error(), err)
	}
	bodyBytes := buf.Bytes()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		response, retry, retryAfter, err := p.completeOnce(ctx, key, req.Model, bodyBytes)
		if err == nil {
			response.RetryCount = attempt
			return response, nil
		}
		lastErr = err
		if !retry || ctx.Err() != nil {
			return CompletionResponse{}, err
		}
		backoff := time.Duration(attempt+1) * 100 * time.Millisecond
		if retryAfter > 0 {
			backoff = retryAfter
		}
		if backoff > maxRetryAfterDelay {
			backoff = maxRetryAfterDelay
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return CompletionResponse{}, app.NewError(app.CategoryProvider, "canceled", "OpenRouter request canceled", ctx.Err())
		case <-timer.C:
		}
	}
	if lastErr != nil {
		return CompletionResponse{}, lastErr
	}
	return CompletionResponse{}, app.NewError(app.CategoryProvider, "network", "OpenRouter request failed", nil)
}

func (p *OpenRouterProvider) completeOnce(ctx context.Context, key, model string, body []byte) (CompletionResponse, bool, time.Duration, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return CompletionResponse{}, false, 0, app.NewError(app.CategoryProvider, "request_build", err.Error(), err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")
	httpRes, err := p.client().Do(httpReq)
	if err != nil {
		return CompletionResponse{}, true, 0, providerTransportError(err)
	}
	defer httpRes.Body.Close()
	retryAfter := parseRetryAfter(httpRes.Header.Get("Retry-After"))
	if httpRes.StatusCode == http.StatusUnauthorized || httpRes.StatusCode == http.StatusForbidden {
		_, _ = io.Copy(io.Discard, httpRes.Body)
		return CompletionResponse{}, false, 0, app.NewError(app.CategoryProvider, "auth", "OpenRouter authorization failed", nil)
	}
	if httpRes.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, httpRes.Body)
		return CompletionResponse{}, false, 0, app.NewError(app.CategoryProvider, "model_not_found", "model not found", nil)
	}
	if httpRes.StatusCode == http.StatusTooManyRequests || httpRes.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, httpRes.Body)
		return CompletionResponse{}, true, retryAfter, app.NewError(app.CategoryProvider, "temporary_http", fmt.Sprintf("OpenRouter status %d", httpRes.StatusCode), nil)
	}
	if httpRes.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, httpRes.Body)
		return CompletionResponse{}, false, 0, app.NewError(app.CategoryProvider, "http", fmt.Sprintf("OpenRouter status %d", httpRes.StatusCode), nil)
	}
	var parsed struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   string             `json:"content"`
				ToolCalls []app.ChatToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(httpRes.Body, maxCompletionResonseSize)).Decode(&parsed); err != nil {
		if transportErr := providerTransportError(err); transportErr.Code != "network" {
			return CompletionResponse{}, false, 0, transportErr
		}
		return CompletionResponse{}, false, 0, app.NewError(app.CategoryProvider, "malformed_response", err.Error(), err)
	}
	if len(parsed.Choices) == 0 {
		return CompletionResponse{}, false, 0, app.NewError(app.CategoryProvider, "malformed_response", "missing choices", nil)
	}
	responseModel := parsed.Model
	if responseModel == "" {
		responseModel = model
	}
	response := newAssistantMessage(parsed.Choices[0].Message.Content, responseModel, parsed.ID)
	response.ToolCalls = append([]app.ChatToolCall(nil), parsed.Choices[0].Message.ToolCalls...)
	response.Message.ToolCalls = append([]app.ChatToolCall(nil), response.ToolCalls...)
	return response, false, 0, nil
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

func providerTransportError(err error) *app.Error {
	if errors.Is(err, context.Canceled) {
		return app.NewError(app.CategoryProvider, "canceled", "OpenRouter request canceled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return app.NewError(app.CategoryProvider, "timeout", "OpenRouter request timed out", err)
	}
	return app.NewError(app.CategoryProvider, "network", err.Error(), err)
}

func (p *OpenRouterProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: defaultOpenRouterTimeout}
}

func toOpenRouterMessages(messages []app.ChatMessage) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		item := map[string]any{"role": string(msg.Role), "content": msg.Content}
		if msg.ToolCallID != "" {
			item["tool_call_id"] = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			item["tool_calls"] = msg.ToolCalls
		}
		out = append(out, item)
	}
	return out
}
