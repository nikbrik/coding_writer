package app

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"github.com/nikbrik/coding_writer/internal/storage"
)

const DefaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

type ConfigOptions struct {
	StorageDir             string
	ActiveModel            string
	MemoryModel            string
	ActiveProfileID        string
	OpenRouterBaseURL      string
	TrustOpenRouterBaseURL bool
}

type ConfigManager struct {
	StorageDir string
}

func DefaultStorageDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "coding-writer-assistant"), nil
}

func ResolveStorageDir(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	if env := os.Getenv("ASSISTANT_STORAGE_DIR"); env != "" {
		return filepath.Abs(env)
	}
	return DefaultStorageDir()
}

func NewConfigManager(storageDir string) *ConfigManager {
	return &ConfigManager{StorageDir: storageDir}
}

func (m *ConfigManager) ConfigPath() (string, error) {
	path, err := storage.SafeJoin(m.StorageDir, "config.json")
	if err != nil {
		return "", NewError(CategoryValidation, "unsafe_storage_path", "unsafe config path", err)
	}
	return path, nil
}

func (m *ConfigManager) EnsureStorageTree() error {
	for _, dir := range []string{"profiles", "sessions", "tasks", "long_term", "logs"} {
		path, err := storage.SafeJoin(m.StorageDir, dir)
		if err != nil {
			return NewError(CategoryValidation, "unsafe_storage_path", "unsafe storage path", err)
		}
		if err := storage.EnsureDir(path); err != nil {
			return NewError(CategoryStorage, "mkdir", err.Error(), err)
		}
	}
	return nil
}

func (m *ConfigManager) Load() (AppConfig, error) {
	cfg := AppConfig{StorageDir: m.StorageDir, OpenRouterBaseURL: DefaultOpenRouterBaseURL}
	configPath, err := m.ConfigPath()
	if err != nil {
		return cfg, err
	}
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, NewError(CategoryStorage, "config_stat", err.Error(), err)
	}
	if err := storage.ReadJSON(configPath, &cfg); err != nil {
		return cfg, NewError(CategoryStorage, "config_read", err.Error(), err)
	}
	if cfg.StorageDir == "" {
		cfg.StorageDir = m.StorageDir
	}
	if cfg.OpenRouterBaseURL == "" {
		cfg.OpenRouterBaseURL = DefaultOpenRouterBaseURL
	}
	return cfg, nil
}

func (m *ConfigManager) LoadEffective(opts ConfigOptions) (AppConfig, error) {
	cfg, err := m.Load()
	if err != nil {
		return cfg, err
	}
	if env := os.Getenv("ASSISTANT_MODEL"); env != "" {
		cfg.ActiveModel = env
	}
	if env := os.Getenv("ASSISTANT_MEMORY_MODEL"); env != "" {
		cfg.MemoryModel = env
	}
	if env := os.Getenv("ASSISTANT_PROFILE"); env != "" {
		cfg.ActiveProfileID = env
	}
	if env := os.Getenv("ASSISTANT_OPENROUTER_BASE_URL"); env != "" {
		cfg.OpenRouterBaseURL = env
	}
	if opts.ActiveModel != "" {
		cfg.ActiveModel = opts.ActiveModel
	}
	if opts.MemoryModel != "" {
		cfg.MemoryModel = opts.MemoryModel
	}
	if opts.ActiveProfileID != "" {
		cfg.ActiveProfileID = opts.ActiveProfileID
	}
	if opts.OpenRouterBaseURL != "" {
		cfg.OpenRouterBaseURL = opts.OpenRouterBaseURL
	}
	if opts.StorageDir != "" {
		cfg.StorageDir = opts.StorageDir
	} else if cfg.StorageDir == "" {
		cfg.StorageDir = m.StorageDir
	}
	if cfg.OpenRouterBaseURL == "" {
		cfg.OpenRouterBaseURL = DefaultOpenRouterBaseURL
	}
	if err := validateBaseURL(cfg.OpenRouterBaseURL); err != nil {
		return cfg, err
	}
	if err := validateTrustedBaseURL(cfg.OpenRouterBaseURL, cfg.TrustedOpenRouterBaseURLs, opts.TrustOpenRouterBaseURL); err != nil {
		return cfg, err
	}
	if cfg.MemoryModel == "" {
		cfg.MemoryModel = cfg.ActiveModel
	}
	return cfg, nil
}

func (m *ConfigManager) Save(cfg AppConfig) error {
	if err := m.EnsureStorageTree(); err != nil {
		return err
	}
	if cfg.StorageDir == "" {
		cfg.StorageDir = m.StorageDir
	}
	if cfg.OpenRouterBaseURL == "" {
		cfg.OpenRouterBaseURL = DefaultOpenRouterBaseURL
	}
	if err := validateBaseURL(cfg.OpenRouterBaseURL); err != nil {
		return err
	}
	if err := validateTrustedBaseURL(cfg.OpenRouterBaseURL, cfg.TrustedOpenRouterBaseURLs, false); err != nil {
		return err
	}
	configPath, err := m.ConfigPath()
	if err != nil {
		return err
	}
	if err := storage.AtomicWriteJSON(configPath, cfg); err != nil {
		return NewError(CategoryStorage, "config_write", err.Error(), err)
	}
	return nil
}

func validateBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return NewError(CategoryValidation, "invalid_base_url", "OpenRouter base URL must be a valid HTTPS URL", err)
	}
	if parsed.Scheme != "https" {
		return NewError(CategoryValidation, "invalid_base_url", "OpenRouter base URL must use HTTPS", nil)
	}
	return nil
}

func validateTrustedBaseURL(raw string, trusted []string, oneShotTrust bool) error {
	if raw == "" || raw == DefaultOpenRouterBaseURL || oneShotTrust {
		return nil
	}
	for _, item := range trusted {
		if item == raw {
			return nil
		}
	}
	return NewError(CategoryValidation, "untrusted_base_url", "non-default OpenRouter base URL requires explicit trust", nil)
}

func (m *ConfigManager) Update(fn func(*AppConfig) error) (AppConfig, error) {
	cfg, err := m.Load()
	if err != nil {
		return cfg, err
	}
	if err := fn(&cfg); err != nil {
		return cfg, err
	}
	return cfg, m.Save(cfg)
}
