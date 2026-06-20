package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/traffic"
)

func TestRemoveManagedFileRemovesOnlyFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedFile(file, file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("expected managed file removed, err=%v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("parent directory should remain: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file should remain: %v", err)
	}
}

func TestErrorCooldownHelpers(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	state := config.State{}
	markErrorCooldown(&state, "notify", now)
	if state.LastErrorKey != "notify" || state.LastErrorAt == "" {
		t.Fatalf("cooldown not marked: %+v", state)
	}
	if !isErrorCoolingDown(state, "notify", now.Add(10*time.Minute)) {
		t.Fatal("expected active cooldown")
	}
	previous := state.LastErrorAt
	if isErrorCoolingDown(state, "notify", now.Add(10*time.Minute)) {
		// Cooling-down checks must not refresh LastErrorAt, otherwise retries can be postponed forever.
	}
	if state.LastErrorAt != previous {
		t.Fatalf("cooldown timestamp refreshed: before=%s after=%s", previous, state.LastErrorAt)
	}
	if isErrorCoolingDown(state, "notify", now.Add(31*time.Minute)) {
		t.Fatal("expected expired cooldown")
	}
	clearErrorCooldown(&state)
	if state.LastErrorKey != "" || state.LastErrorAt != "" {
		t.Fatalf("cooldown not cleared: %+v", state)
	}
}

func TestAppliedLimitKeyIncludesInterfacesAndSplitRate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interfaces = []string{"eth0", "ens5"}
	key, err := appliedLimitKey(cfg, "10mbit")
	if err != nil {
		t.Fatal(err)
	}
	if key != "eth0,ens5=5000000bit" {
		t.Fatalf("appliedLimitKey=%q", key)
	}
	cfg.Interfaces = []string{"eth0"}
	key, err = appliedLimitKey(cfg, "10mbit")
	if err != nil {
		t.Fatal(err)
	}
	if key != "eth0=10000000bit" {
		t.Fatalf("single appliedLimitKey=%q", key)
	}
}

func TestRemoveManagedFileRejectsRoot(t *testing.T) {
	if err := removeManagedFile("/", "/"); err == nil {
		t.Fatal("expected unsafe root path error")
	}
}

func TestNotificationKeyIncludesRate(t *testing.T) {
	soft := traffic.Decision{Level: traffic.LevelSoft, LimitRate: "10mbit"}
	hard := traffic.Decision{Level: traffic.LevelSoft, LimitRate: "1mbit"}
	state := config.State{LastNotifiedLevel: traffic.LevelSoft, LastNotifiedKey: notificationKey(soft)}
	if shouldNotify(state, soft) {
		t.Fatal("same level and rate should not notify")
	}
	if !shouldNotify(state, hard) {
		t.Fatal("same level with different rate should notify")
	}
}

func TestParseInterfaces(t *testing.T) {
	items, err := parseInterfaces("eth0, ens5")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0] != "eth0" || items[1] != "ens5" {
		t.Fatalf("parseInterfaces=%v", items)
	}
}

func TestVisitedFlagsDetectsExplicitZero(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	value := fs.Float64("warn-clear-percent", 65, "")
	if err := fs.Parse([]string{"--warn-clear-percent", "0"}); err != nil {
		t.Fatal(err)
	}
	visited := visitedFlags(fs)
	if !visited["warn-clear-percent"] || *value != 0 {
		t.Fatalf("explicit zero not detected: visited=%v value=%v", visited, *value)
	}
}

func TestCmdUpgradeDryRun(t *testing.T) {
	if err := cmdUpgrade([]string{"--dry-run", "--version", "v0.1.4", "--base-url", "https://example.com/releases", "--install-dir", t.TempDir(), "--no-restart"}); err != nil {
		t.Fatal(err)
	}
}
