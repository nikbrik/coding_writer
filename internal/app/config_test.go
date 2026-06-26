package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDoesNotPersistAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENROUTER_API_KEY", "sk-secret123456789")
	mgr := NewConfigManager(dir)
	if err := mgr.Save(AppConfig{StorageDir: dir, ActiveModel: "fake/model"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "OPENROUTER_API_KEY") || strings.Contains(string(data), "sk-secret") {
		t.Fatalf("API key leaked into config: %s", string(data))
	}
}

func TestConfigMCPServerDoesNotPersistEnvValue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_TOKEN", "ghp_secretvalue123456789")
	mgr := NewConfigManager(dir)
	if err := mgr.Save(AppConfig{
		StorageDir:  dir,
		ActiveModel: "fake/model",
		MCPServers: []MCPServerConfig{{
			Name:    "github-api",
			Command: "python3",
			EnvKeys: []string{"GITHUB_TOKEN"},
			Enabled: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ghp_secretvalue") {
		t.Fatalf("MCP env value leaked into config: %s", string(data))
	}
	if !strings.Contains(string(data), "GITHUB_TOKEN") {
		t.Fatalf("MCP env key should be persisted: %s", string(data))
	}
}

func TestConfigRejectsInsecureBaseURL(t *testing.T) {
	dir := t.TempDir()
	mgr := NewConfigManager(dir)
	err := mgr.Save(AppConfig{StorageDir: dir, OpenRouterBaseURL: "http://example.com"})
	if err == nil || !strings.Contains(err.Error(), "invalid_base_url") {
		t.Fatalf("want invalid_base_url, got %v", err)
	}
}
