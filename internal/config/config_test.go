package config

import (
	"math"
	"os"
	"path/filepath"
	"strings"
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

func TestNormalizeDefaultsSchemaVersion(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SchemaVersion = 0
	cfg.Normalize()
	if cfg.SchemaVersion != ConfigSchemaVersion {
		t.Fatalf("schema_version=%d", cfg.SchemaVersion)
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

func TestLoadDerivesClearThresholdsForLowCustomTriggers(t *testing.T) {
	// A config written before clear thresholds existed, with custom triggers
	// below the old fixed clear defaults (65/80/90), must still load instead of
	// failing validation and crash-looping the service.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
		"interface":"eth0",
		"interfaces":["eth0"],
		"allowance_bytes":1000,
		"thresholds":{"warn_percent":50,"soft_percent":60,"hard_percent":70},
		"limits":{"soft_rate":"10mbit","hard_rate":"1mbit"}
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("low custom triggers should still load: %v", err)
	}
	th := cfg.Thresholds
	if th.WarnClearPercent > th.WarnPercent || th.SoftClearPercent > th.SoftPercent || th.HardClearPercent > th.HardPercent {
		t.Fatalf("derived clear thresholds exceed triggers: %+v", th)
	}
	if th.WarnClearPercent != 45 || th.SoftClearPercent != 55 || th.HardClearPercent != 65 {
		t.Fatalf("expected trigger-5 clears, got %+v", th)
	}
}

func TestLoadHealsUnparseableRateInsteadOfFailing(t *testing.T) {
	// A legacy or hand-edited config with a non-empty but unparseable rate must
	// still load (coerced to the default) so the service does not crash-loop on
	// startup. Fresh CLI input is validated separately.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{
		"interface":"eth0",
		"interfaces":["eth0"],
		"allowance_bytes":1000,
		"thresholds":{"warn_percent":70,"soft_percent":85,"hard_percent":95},
		"limits":{"soft_rate":"0mbit","hard_rate":"10mb"}
	}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("legacy bad-rate config should still load: %v", err)
	}
	if cfg.Limits.SoftRate != "10mbit" || cfg.Limits.HardRate != "1mbit" {
		t.Fatalf("bad rates not healed to defaults: %+v", cfg.Limits)
	}
}

func TestValidateRejectsInvalidRate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Interface = "eth0"
	cfg.Interfaces = []string{"eth0"}
	cfg.AllowanceBytes = 1000
	cfg.Limits.SoftRate = "garbage"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid soft_rate to be rejected at validation time")
	}
	cfg.Limits.SoftRate = "10mbit"
	cfg.Limits.HardRate = "10" // no unit suffix
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unit-less hard_rate to be rejected")
	}
	cfg.Limits.HardRate = "1mbit"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid rates should pass: %v", err)
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

func TestBackupPrunesOldFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxConfigBackups+3; i++ {
		if _, err := Backup(path); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(path + ".bak.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != MaxConfigBackups {
		t.Fatalf("backups=%d, want %d: %v", len(matches), MaxConfigBackups, matches)
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

func TestWriteFileAtomicRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	link := filepath.Join(dir, "config.json")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := writeFileAtomic(link, []byte("replace"), 0600)
	if err == nil {
		t.Fatal("expected symlink target to be rejected")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink error, got %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("symlink target was modified: %q", data)
	}
}

func TestWriteFileAtomicRejectsDirectoryTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("replace"), 0600); err == nil {
		t.Fatal("expected directory target to be rejected")
	}
}
