package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
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

func TestWithDefaultByteUnit(t *testing.T) {
	tests := map[string]string{
		"100":   "100GB",
		"1.5":   "1.5GB",
		"100GB": "100GB",
		" 20 ":  "20GB",
	}
	for input, want := range tests {
		if got := withDefaultByteUnit(input, "GB"); got != want {
			t.Fatalf("withDefaultByteUnit(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestCmdTopupAddsAllowance(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	statePath := filepath.Join(dir, "state.json")
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	cfg.Interfaces = []string{"eth0"}
	cfg.AllowanceBytes = 1000 * 1000 * 1000
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := cmdTopup([]string{"--config", cfgPath, "--state", statePath, "--check-now=false", "100"}); err != nil {
		t.Fatal(err)
	}
	updated, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	want := uint64(101) * 1000 * 1000 * 1000
	if updated.AllowanceBytes != want {
		t.Fatalf("allowance=%d, want %d", updated.AllowanceBytes, want)
	}
}

func TestCmdModifyUpdatesLanguage(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	cfg.Interfaces = []string{"eth0"}
	cfg.AllowanceBytes = 1000 * 1000 * 1000
	cfg.Language = "zh"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := cmdModify([]string{"--config", cfgPath, "--state", filepath.Join(dir, "state.json"), "--language", "en"}); err != nil {
		t.Fatal(err)
	}
	updated, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Language != "en" {
		t.Fatalf("language=%q", updated.Language)
	}
}

func TestOnlyResetRecentBaselineChange(t *testing.T) {
	if !onlyResetRecentBaselineChange(map[string]bool{"reset-recent-baseline": true}) {
		t.Fatal("expected reset-only change")
	}
	if !onlyResetRecentBaselineChange(map[string]bool{"reset-recent-baseline": true, "config": true, "state": true}) {
		t.Fatal("expected reset-only change with path flags")
	}
	if onlyResetRecentBaselineChange(map[string]bool{"reset-recent-baseline": true, "allowance": true}) {
		t.Fatal("allowance change should require normal restart behavior")
	}
}

func TestFormatNotificationUsesChinese(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	cfg.AllowanceBytes = 1000
	cfg.Language = "zh"
	msg := formatNotification(cfg, traffic.Usage{BillableBytes: 900, TotalBytes: 900, RXBytes: 400, TXBytes: 500, Percent: 90}, traffic.Decision{Level: traffic.LevelWarn}, traffic.RecentUsage{}, true)
	if !strings.Contains(msg, "流量额度") || !strings.Contains(msg, "剩余额度") {
		t.Fatalf("notification not localized: %s", msg)
	}
}

func TestFormatNotificationIncludesRecentUsage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	cfg.AllowanceBytes = 1000
	cfg.Language = "zh"
	recent := traffic.RecentUsage{
		Today:    traffic.UsageSummary{RXBytes: 1, TXBytes: 1, TotalBytes: 2, BillableBytes: 2},
		ThisWeek: traffic.UsageSummary{RXBytes: 10, TXBytes: 10, TotalBytes: 20, BillableBytes: 20},
	}
	msg := formatNotification(cfg, traffic.Usage{BillableBytes: 900, TotalBytes: 900, RXBytes: 400, TXBytes: 500, Percent: 90}, traffic.Decision{Level: traffic.LevelWarn}, recent, true)
	if !strings.Contains(msg, "今日新增") || !strings.Contains(msg, "本周累计") {
		t.Fatalf("notification missing recent usage: %s", msg)
	}
}

func TestFormatNotificationReportsUnavailableRecentUsage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	cfg.AllowanceBytes = 1000
	cfg.Language = "zh"
	msg := formatNotification(cfg, traffic.Usage{BillableBytes: 100, TotalBytes: 100, RXBytes: 40, TXBytes: 60, Percent: 10}, traffic.Decision{Level: traffic.LevelNormal}, traffic.RecentUsage{}, false)
	if !strings.Contains(msg, "近期统计：暂不可用") {
		t.Fatalf("notification missing unavailable recent usage: %s", msg)
	}
}

func TestComputeForecastUsesWeekly(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AllowanceBytes = 1000
	cfg.PeriodDay = 1
	cfg.Thresholds.SoftPercent = 80
	usage := traffic.Usage{BillableBytes: 100}
	recent := traffic.RecentUsage{
		ThisWeek:           traffic.UsageSummary{BillableBytes: 70, TotalBytes: 70},
		ThisWeekWindowDays: 6,
	}
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC) // Saturday => 6 days into week
	f := computeForecast(cfg, usage, recent, true, now)
	if !f.Available {
		t.Fatalf("forecast unavailable")
	}
	if f.AvgPerDayBytes == 0 {
		t.Fatalf("avg/day zero")
	}
	if f.WindowSource != "this_week" {
		t.Fatalf("expected this_week window, got %q", f.WindowSource)
	}
	if f.PredictedTotalBytes <= usage.BillableBytes {
		t.Fatalf("predicted total not increasing: %d", f.PredictedTotalBytes)
	}
	if f.DaysToSoftLimit < 0 {
		t.Fatalf("days to soft negative")
	}
}

func TestComputeForecastUsesBaselineWeekWindow(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AllowanceBytes = 1000
	cfg.PeriodDay = 1
	cfg.Thresholds.SoftPercent = 80
	usage := traffic.Usage{BillableBytes: 100}
	recent := traffic.RecentUsage{
		ThisWeek:           traffic.UsageSummary{BillableBytes: 70, TotalBytes: 70},
		ThisWeekWindowDays: 1,
	}
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	f := computeForecast(cfg, usage, recent, true, now)
	if f.AvgPerDayBytes != 70 {
		t.Fatalf("avg/day=%d, want 70", f.AvgPerDayBytes)
	}
}

func TestCalendarDaysInclusiveUsesDates(t *testing.T) {
	start := time.Date(2026, 6, 18, 0, 0, 0, 0, time.FixedZone("A", 8*3600))
	end := time.Date(2026, 7, 17, 23, 0, 0, 0, time.FixedZone("B", -5*3600))
	if got := calendarDaysInclusive(start, end); got != 30 {
		t.Fatalf("calendar days=%d, want 30", got)
	}
}

func TestHelpTextLanguages(t *testing.T) {
	zh := helpText("zh")
	if !strings.Contains(zh, "FlowGuard 命令") || !strings.Contains(zh, "flowguard topup 100GB") || !strings.Contains(zh, "购买额外流量") {
		t.Fatalf("Chinese help missing content: %s", zh)
	}
	en := helpText("en")
	if !strings.Contains(en, "FlowGuard commands") || !strings.Contains(en, "flowguard topup 100GB") || !strings.Contains(en, "Add purchased traffic") {
		t.Fatalf("English help missing content: %s", en)
	}
}

func TestStatusTextHelpers(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PeriodDay = 18
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	if got := periodRangeText(cfg, now, true); got != "2026-06-18 至 2026-07-17" {
		t.Fatalf("periodRangeText zh=%q", got)
	}
	if got := periodRangeText(cfg, now, false); got != "2026-06-18 to 2026-07-17" {
		t.Fatalf("periodRangeText en=%q", got)
	}
	if got := billingModeText(cfg, true); got != "总流量（入站 + 出站）" {
		t.Fatalf("billingModeText=%q", got)
	}
	cfg.BillingMode = "outbound"
	if got := billingModeText(cfg, true); got != "仅出站流量" {
		t.Fatalf("billingModeText outbound=%q", got)
	}
	if got := decisionText(traffic.Decision{Level: traffic.LevelNormal}, true); got != "正常，无需限速" {
		t.Fatalf("decisionText=%q", got)
	}
	if got := deltaText(map[string]uint64{"rx": 0, "tx": 0, "total": 0}, true); got != "暂无新增流量" {
		t.Fatalf("deltaText=%q", got)
	}
}

func TestCmdUpgradeDryRun(t *testing.T) {
	if err := cmdUpgrade([]string{"--dry-run", "--version", "v0.1.4", "--base-url", "https://example.com/releases", "--install-dir", t.TempDir(), "--no-restart"}); err != nil {
		t.Fatal(err)
	}
}
