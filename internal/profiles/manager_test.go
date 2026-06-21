package profiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nikbrik/coding_writer/internal/app"
)

func TestDefaultProfilesAndSwitch(t *testing.T) {
	dir := t.TempDir()
	cfgMgr := app.NewConfigManager(dir)
	mgr := NewManager(dir, cfgMgr)
	if err := cfgMgr.EnsureStorageTree(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnsureDefaults(); err != nil {
		t.Fatal(err)
	}
	items, err := mgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 defaults, got %d", len(items))
	}
	if _, err := mgr.SetActive("senior"); err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgMgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfileID != "senior" {
		t.Fatalf("profile not switched: %+v", cfg)
	}
	if _, err := mgr.SetActive("missing"); err == nil {
		t.Fatal("unknown profile mutated active profile")
	}
	cfgAfter, _ := cfgMgr.Load()
	if cfgAfter.ActiveProfileID != "senior" {
		t.Fatalf("unknown profile changed active: %+v", cfgAfter)
	}
}

func TestCreateDefaultProfileUsesExactSafeDefaultsAndActivates(t *testing.T) {
	dir := t.TempDir()
	cfgMgr := app.NewConfigManager(dir)
	if err := cfgMgr.EnsureStorageTree(); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(dir, cfgMgr)
	profile, err := mgr.CreateDefault("custom")
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != "custom" || profile.DisplayName != "custom" || profile.DefaultModel != "" {
		t.Fatalf("bad identity defaults: %+v", profile)
	}
	if len(profile.Style) != 2 || profile.Style["language"] != "ru" || profile.Style["tone"] != "direct" {
		t.Fatalf("bad style defaults: %+v", profile.Style)
	}
	if len(profile.ResponseFormat) != 1 || profile.ResponseFormat["structure"] != "concise" {
		t.Fatalf("bad response defaults: %+v", profile.ResponseFormat)
	}
	wantConstraints := []string{"follow user preferences", "do not inherit hidden state", "do not copy long memory"}
	if strings.Join(profile.Constraints, "|") != strings.Join(wantConstraints, "|") {
		t.Fatalf("bad constraints: %+v", profile.Constraints)
	}
	cfg, err := cfgMgr.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ActiveProfileID != "custom" {
		t.Fatalf("created profile not active: %+v", cfg)
	}
}

func TestProfileRejectsReservedNewID(t *testing.T) {
	dir := t.TempDir()
	cfgMgr := app.NewConfigManager(dir)
	if err := cfgMgr.EnsureStorageTree(); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(dir, cfgMgr)
	if _, err := mgr.CreateDefault("new"); err == nil || !strings.Contains(err.Error(), "reserved_profile_id") {
		t.Fatalf("want reserved_profile_id, got %v", err)
	}
}

func TestProfileRenderDeterministicAndTagged(t *testing.T) {
	profile := DefaultProfiles(time.Now().UTC())[0]
	one, err := Render(profile)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Render(profile)
	if err != nil {
		t.Fatal(err)
	}
	if one != two {
		t.Fatal("render not deterministic")
	}
	if !strings.Contains(one, `id="profile.active"`) || !strings.Contains(one, `trust="untrusted"`) || !strings.Contains(one, `priority="high"`) {
		t.Fatalf("missing canonical tags: %s", one)
	}
}

func TestProfileRejectsSecrets(t *testing.T) {
	profile := DefaultProfiles(time.Now().UTC())[0]
	profile.Constraints = append(profile.Constraints, "OPENROUTER_API_KEY=sk-secret123456789")
	if err := Validate(profile); err == nil || !strings.Contains(err.Error(), "secret_blocked") {
		t.Fatalf("want secret blocked, got %v", err)
	}
}

func TestLoadedProfileIsValidated(t *testing.T) {
	dir := t.TempDir()
	cfgMgr := app.NewConfigManager(dir)
	if err := cfgMgr.EnsureStorageTree(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "bad.json"), []byte(`{"id":"bad","display_name":"Bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(dir, cfgMgr)
	if _, err := mgr.Get("bad"); err == nil || !strings.Contains(err.Error(), "invalid_profile") {
		t.Fatalf("want invalid_profile, got %v", err)
	}
}
