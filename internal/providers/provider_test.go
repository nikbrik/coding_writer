package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestOpenRouterMissingAPIKeyDoesNotCallHTTP(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()
	provider := NewOpenRouterProvider(server.URL)
	_, err := provider.Complete(context.Background(), CompletionRequest{Purpose: PurposeChat, Model: "fake/model", Messages: []app.ChatMessage{{Role: app.RoleUser, Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "missing_api_key") {
		t.Fatalf("want missing key error, got %v", err)
	}
	if called {
		t.Fatal("HTTP called without API key")
	}
}

func TestOpenRouterTimeoutErrorTyped(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"id":"late"}`))
	}))
	defer server.Close()
	provider := NewOpenRouterProvider(server.URL)
	provider.Client = &http.Client{Timeout: 5 * time.Millisecond}
	_, err := provider.Complete(context.Background(), CompletionRequest{Purpose: PurposeChat, Model: "fake/model", Messages: []app.ChatMessage{{Role: app.RoleUser, Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

func TestOpenRouterBodyReadTimeoutErrorTyped(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"id":"late"}`))
	}))
	defer server.Close()
	provider := NewOpenRouterProvider(server.URL)
	provider.Client = &http.Client{Timeout: 5 * time.Millisecond}
	_, err := provider.Complete(context.Background(), CompletionRequest{Purpose: PurposeChat, Model: "fake/model", Messages: []app.ChatMessage{{Role: app.RoleUser, Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("want timeout error, got %v", err)
	}
}

func TestOpenRouterAuthErrorTyped(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	provider := NewOpenRouterProvider(server.URL)
	_, err := provider.Complete(context.Background(), CompletionRequest{Purpose: PurposeChat, Model: "fake/model", Messages: []app.ChatMessage{{Role: app.RoleUser, Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("want auth error, got %v", err)
	}
}

func TestFakeProviderRedactsSecretsBeforeRecordingCalls(t *testing.T) {
	provider := NewFakeProvider()
	_, err := provider.Complete(context.Background(), CompletionRequest{Purpose: PurposeChat, Model: "fake/model", Messages: []app.ChatMessage{{Role: app.RoleUser, Content: "Bearer abcdefghijklmnop"}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := provider.SnapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("want one call, got %d", len(calls))
	}
	if strings.Contains(calls[0].Messages[0].Content, "abcdefghijklmnop") || !strings.Contains(calls[0].Messages[0].Content, "[REDACTED_SECRET]") {
		t.Fatalf("secret not redacted: %+v", calls[0].Messages[0].Content)
	}
}

func TestOpenRouterRetriesTemporaryHTTP(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"completion_test","model":"fake/model","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()
	provider := NewOpenRouterProvider(server.URL)
	res, err := provider.Complete(context.Background(), CompletionRequest{Purpose: PurposeChat, Model: "fake/model", Messages: []app.ChatMessage{{Role: app.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || res.RetryCount != 1 || res.Message.Content != "ok" {
		t.Fatalf("bad retry result calls=%d retry=%d res=%+v", calls, res.RetryCount, res)
	}
}

func TestOpenRouterRetryBackoffRespectsCancellation(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	provider := NewOpenRouterProvider(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := provider.Complete(ctx, CompletionRequest{Purpose: PurposeChat, Model: "fake/model", Messages: []app.ChatMessage{{Role: app.RoleUser, Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("want canceled error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 80*time.Millisecond {
		t.Fatalf("cancellation waited for full backoff: %s", elapsed)
	}
}
