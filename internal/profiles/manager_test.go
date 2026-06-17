package profiles

import (
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
	if !strings.Contains(one, `id="profile.active"`) || !strings.Contains(one, `trust="untrusted"`) {
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
