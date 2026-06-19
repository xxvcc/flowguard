package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRejectsInvalidInterfaceName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Interface = "../eth0"
	cfg.Interfaces = []string{"../eth0"}
	cfg.AllowanceBytes = 1000
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid interface name error")
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
