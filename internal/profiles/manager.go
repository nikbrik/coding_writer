package profiles

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
	"github.com/nikbrik/coding_writer/internal/storage"
	"github.com/nikbrik/coding_writer/internal/validation"
)

type Manager struct {
	StorageDir string
	Config     *app.ConfigManager
}

func NewManager(storageDir string, cfg *app.ConfigManager) *Manager {
	return &Manager{StorageDir: storageDir, Config: cfg}
}

func (m *Manager) profilePath(id string) (string, error) {
	if err := storage.ValidateID(id); err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_profile_id", "unsafe profile id", err)
	}
	path, err := storage.SafeJoin(m.StorageDir, "profiles", id+".json")
	if err != nil {
		return "", app.NewError(app.CategoryValidation, "unsafe_profile_path", "unsafe profile path", err)
	}
	return path, nil
}

func (m *Manager) EnsureDefaults() error {
	now := time.Now().UTC()
	for _, profile := range DefaultProfiles(now) {
		if _, err := m.Get(profile.ID); err == nil {
			continue
		} else {
			appErr := app.AsError(err)
			if appErr.Category != app.CategoryValidation || appErr.Code != "unknown_profile" {
				return err
			}
		}
		if err := m.Create(profile); err != nil {
			return err
		}
	}
	cfg, err := m.Config.Load()
	if err != nil {
		return err
	}
	if cfg.ActiveProfileID == "" {
		cfg.ActiveProfileID = "student"
		return m.Config.Save(cfg)
	}
	return nil
}

func (m *Manager) Create(profile app.UserProfile) error {
	if err := Validate(profile); err != nil {
		return err
	}
	path, err := m.profilePath(profile.ID)
	if err != nil {
		return err
	}
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = time.Now().UTC()
	}
	profile.UpdatedAt = time.Now().UTC()
	if err := storage.AtomicWriteJSON(path, profile); err != nil {
		return app.NewError(app.CategoryStorage, "profile_write", err.Error(), err)
	}
	return nil
}

func (m *Manager) Get(id string) (app.UserProfile, error) {
	path, err := m.profilePath(id)
	if err != nil {
		return app.UserProfile{}, err
	}
	var profile app.UserProfile
	if err := storage.ReadJSON(path, &profile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return profile, app.NewError(app.CategoryValidation, "unknown_profile", "unknown profile", err)
		}
		return profile, app.NewError(app.CategoryStorage, "profile_read", err.Error(), err)
	}
	if err := Validate(profile); err != nil {
		return profile, err
	}
	return profile, nil
}

func (m *Manager) List() ([]app.UserProfile, error) {
	dir := filepath.Join(m.StorageDir, "profiles")
	if err := storage.EnsureDir(dir); err != nil {
		return nil, app.NewError(app.CategoryStorage, "profiles_dir", err.Error(), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, app.NewError(app.CategoryStorage, "profiles_list", err.Error(), err)
	}
	var out []app.UserProfile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		profile, err := m.Get(id)
		if err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Manager) Active() (app.UserProfile, error) {
	cfg, err := m.Config.Load()
	if err != nil {
		return app.UserProfile{}, err
	}
	if cfg.ActiveProfileID == "" {
		if err := m.EnsureDefaults(); err != nil {
			return app.UserProfile{}, err
		}
		cfg, err = m.Config.Load()
		if err != nil {
			return app.UserProfile{}, err
		}
	}
	return m.Get(cfg.ActiveProfileID)
}

func (m *Manager) SetActive(id string) (app.UserProfile, error) {
	profile, err := m.Get(id)
	if err != nil {
		return profile, err
	}
	_, err = m.Config.Update(func(cfg *app.AppConfig) error {
		cfg.ActiveProfileID = id
		return nil
	})
	return profile, err
}

func Validate(profile app.UserProfile) error {
	if err := storage.ValidateID(profile.ID); err != nil {
		return app.NewError(app.CategoryValidation, "unsafe_profile_id", "unsafe profile id", err)
	}
	if strings.TrimSpace(profile.DisplayName) == "" || len(profile.Style) == 0 || len(profile.ResponseFormat) == 0 || len(profile.Constraints) == 0 {
		return app.NewError(app.CategoryValidation, "invalid_profile", "profile requires display_name, style, response_format, constraints", nil)
	}
	if validation.HasSecret(profile.ID) || validation.HasSecret(profile.DisplayName) || validation.HasSecret(profile.DefaultModel) {
		return app.NewError(app.CategoryValidation, "secret_blocked", "secret-like profile content cannot be saved", nil)
	}
	for key, value := range profile.Style {
		if validation.HasSecret(key) || validation.HasSecret(value) {
			return app.NewError(app.CategoryValidation, "secret_blocked", "secret-like profile content cannot be saved", nil)
		}
	}
	for key, value := range profile.ResponseFormat {
		if validation.HasSecret(key) || validation.HasSecret(value) {
			return app.NewError(app.CategoryValidation, "secret_blocked", "secret-like profile content cannot be saved", nil)
		}
	}
	for _, constraint := range profile.Constraints {
		if validation.HasSecret(constraint) {
			return app.NewError(app.CategoryValidation, "secret_blocked", "secret-like profile content cannot be saved", nil)
		}
	}
	return nil
}
