package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsInvalidInterfaceName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Interface = "../eth0"
	cfg.Interfaces = []string{"eth0"}
	cfg.AllowanceBytes = 1000
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid interface name error")
	}
	cfg.Interface = "eth0"
	cfg.Interfaces = []string{"../eth0"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid interfaces error")
	}
}

func TestNormalizeInterfacesTrimsAndDeduplicates(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Interface = "eth0"
	cfg.Interfaces = []string{" eth0 ", "ens5", "eth0", ""}
	cfg.AllowanceBytes = 1000
	cfg.Normalize()
	if len(cfg.Interfaces) != 2 || cfg.Interfaces[0] != "eth0" || cfg.Interfaces[1] != "ens5" {
		t.Fatalf("interfaces not normalized: %#v", cfg.Interfaces)
	}
}

func TestValidateRejectsInvalidThresholds(t *testing.T) {
	tests := []func(*Config){
		func(cfg *Config) { cfg.Thresholds.HardPercent = 101 },
		func(cfg *Config) { cfg.Thresholds.WarnPercent = math.Inf(1) },
		func(cfg *Config) { cfg.Thresholds.SoftClearPercent = math.NaN() },
	}
	for _, mutate := range tests {
		cfg := DefaultConfig()
		cfg.Interface = "eth0"
		cfg.Interfaces = []string{"eth0"}
		cfg.AllowanceBytes = 1000
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected threshold validation error")
		}
	}
}

func TestLoadPreservesExplicitZeroClearThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
		"interface":"eth0",
		"interfaces":["eth0"],
		"allowance_bytes":1000,
		"thresholds":{
			"warn_percent":70,
			"soft_percent":85,
			"hard_percent":95,
			"warn_clear_percent":0,
			"soft_clear_percent":0,
			"hard_clear_percent":0
		},
		"limits":{"soft_rate":"10mbit","hard_rate":"1mbit"}
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Thresholds.WarnClearPercent != 0 || cfg.Thresholds.SoftClearPercent != 0 || cfg.Thresholds.HardClearPercent != 0 {
		t.Fatalf("explicit zero clear thresholds not preserved: %+v", cfg.Thresholds)
	}
}

func TestLoadDefaultsMissingClearThresholds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
		"interface":"eth0",
		"interfaces":["eth0"],
		"allowance_bytes":1000,
		"thresholds":{
			"warn_percent":70,
			"soft_percent":85,
			"hard_percent":95
		},
		"limits":{"soft_rate":"10mbit","hard_rate":"1mbit"}
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Thresholds.WarnClearPercent != 65 || cfg.Thresholds.SoftClearPercent != 80 || cfg.Thresholds.HardClearPercent != 90 {
		t.Fatalf("missing clear thresholds not defaulted: %+v", cfg.Thresholds)
	}
}

func TestValidateLanguage(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Interface = "eth0"
	cfg.Interfaces = []string{"eth0"}
	cfg.AllowanceBytes = 1000
	cfg.Language = "en"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Language = "fr"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid language error")
	}
}

func TestLoadStateNormalizesInvalidLevels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"period":"2026-06","level":"bad","last_notified_level":"also_bad"}`), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path, "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if state.Level != LevelNormal || state.LastNotifiedLevel != LevelNormal {
		t.Fatalf("state levels not normalized: %+v", state)
	}
}

func TestBackupCreatesUniqueFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("backups should be unique: %s", first)
	}
}

func TestSaveUsesPrivateFileAndNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := DefaultConfig()
	cfg.Interface = "eth0"
	cfg.Interfaces = []string{"eth0"}
	cfg.AllowanceBytes = 1000
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o, want 0600", info.Mode().Perm())
	}
	matches, err := filepath.Glob(path + ".tmp.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}
