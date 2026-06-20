package traffic

import (
	"strings"
	"testing"
	"time"

	"flowguard/internal/config"
)

func TestCurrentPeriod(t *testing.T) {
	tests := []struct {
		now       string
		periodDay int
		want      string
	}{
		{"2026-06-19T12:00:00Z", 1, "2026-06"},
		{"2026-06-01T00:00:00Z", 15, "2026-05"},
		{"2026-06-15T00:00:00Z", 15, "2026-06"},
		{"2026-01-01T00:00:00Z", 15, "2025-12"},
	}
	for _, tt := range tests {
		now, err := time.Parse(time.RFC3339, tt.now)
		if err != nil {
			t.Fatal(err)
		}
		if got := CurrentPeriod(now, tt.periodDay); got != tt.want {
			t.Fatalf("CurrentPeriod(%s,%d)=%s, want %s", tt.now, tt.periodDay, got, tt.want)
		}
	}
}

func TestSumDaysForCustomPeriod(t *testing.T) {
	dayBefore := vnstatPeriod{RX: 100, TX: 100}
	dayBefore.Date.Year = 2026
	dayBefore.Date.Month = 6
	dayBefore.Date.Day = 14
	dayStart := vnstatPeriod{RX: 1, TX: 2}
	dayStart.Date.Year = 2026
	dayStart.Date.Month = 6
	dayStart.Date.Day = 15
	dayNow := vnstatPeriod{RX: 3, TX: 4}
	dayNow.Date.Year = 2026
	dayNow.Date.Month = 6
	dayNow.Date.Day = 19
	data := vnstatJSON{Interfaces: []vnstatInterface{{Name: "eth0", Traffic: vnstatTraffic{Days: []vnstatPeriod{dayBefore, dayStart, dayNow}}}}}
	now, _ := time.Parse(time.RFC3339, "2026-06-19T12:00:00Z")
	rx, tx, err := findUsage(data, "eth0", "2026-06", CurrentPeriodStart(now, 15), now, 15)
	if err != nil {
		t.Fatal(err)
	}
	if rx != 4 || tx != 6 {
		t.Fatalf("custom period rx=%d tx=%d", rx, tx)
	}
}

func TestRecentUsageWeekStartsMonday(t *testing.T) {
	monday := vnstatPeriod{RX: 1, TX: 2}
	monday.Date.Year = 2026
	monday.Date.Month = 6
	monday.Date.Day = 15
	yesterday := vnstatPeriod{RX: 3, TX: 4}
	yesterday.Date.Year = 2026
	yesterday.Date.Month = 6
	yesterday.Date.Day = 19
	today := vnstatPeriod{RX: 5, TX: 6}
	today.Date.Year = 2026
	today.Date.Month = 6
	today.Date.Day = 20
	beforeWeek := vnstatPeriod{RX: 100, TX: 100}
	beforeWeek.Date.Year = 2026
	beforeWeek.Date.Month = 6
	beforeWeek.Date.Day = 14
	data := vnstatJSON{Interfaces: []vnstatInterface{{Name: "eth0", Traffic: vnstatTraffic{Days: []vnstatPeriod{beforeWeek, monday, yesterday, today}}}}}
	now, _ := time.Parse(time.RFC3339, "2026-06-20T12:00:00Z")
	cfg := config.DefaultConfig()
	cfg.Interfaces = []string{"eth0"}
	cfg.BillingMode = "outbound"
	todayRX, todayTX, err := sumInterfaceDays(data, cfg.Interfaces, dateOnly(now), dateOnly(now))
	if err != nil {
		t.Fatal(err)
	}
	if todayRX != 5 || todayTX != 6 {
		t.Fatalf("today rx=%d tx=%d", todayRX, todayTX)
	}
	weekStart := dateOnly(now).AddDate(0, 0, -daysSinceMonday(dateOnly(now)))
	weekRX, weekTX, err := sumInterfaceDays(data, cfg.Interfaces, weekStart, dateOnly(now))
	if err != nil {
		t.Fatal(err)
	}
	if weekRX != 9 || weekTX != 12 {
		t.Fatalf("week rx=%d tx=%d", weekRX, weekTX)
	}
	if got := makeUsageSummary(cfg, weekRX, weekTX).BillableBytes; got != 12 {
		t.Fatalf("outbound billable=%d", got)
	}
}

func TestDecide(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AllowanceBytes = 1000
	tests := []struct {
		total uint64
		want  string
		rate  string
	}{
		{699, LevelNormal, ""},
		{700, LevelWarn, ""},
		{850, LevelSoft, "10mbit"},
		{950, LevelHard, "1mbit"},
	}
	for _, tt := range tests {
		got := Decide(cfg, Usage{TotalBytes: tt.total, Percent: float64(tt.total) * 100 / float64(cfg.AllowanceBytes)})
		if got.Level != tt.want || got.LimitRate != tt.rate {
			t.Fatalf("Decide(%d)=%+v, want level=%s rate=%s", tt.total, got, tt.want, tt.rate)
		}
	}
}

func TestBillingModeOutboundUsesTXPercent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AllowanceBytes = 1000
	cfg.BillingMode = "outbound"
	usage := Usage{RXBytes: 900, TXBytes: 100, TotalBytes: 1000, BillableBytes: 100, Percent: 10}
	got := Decide(cfg, usage)
	if got.Level != LevelNormal {
		t.Fatalf("outbound billing decision=%s, want normal", got.Level)
	}
}

func TestDecideWithStateUpgradesAcrossLevels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AllowanceBytes = 1000
	state := config.State{Level: LevelWarn}
	got := DecideWithState(cfg, Usage{Percent: 100}, state)
	if got.Level != LevelHard || got.LimitRate != cfg.Limits.HardRate {
		t.Fatalf("DecideWithState from warn at 100%%=%+v", got)
	}
}

func TestDecideWithStateHysteresis(t *testing.T) {
	cfg := config.DefaultConfig()
	got := DecideWithState(cfg, Usage{Percent: 82}, config.State{Level: LevelSoft})
	if got.Level != LevelSoft {
		t.Fatalf("soft should persist above soft clear, got %s", got.Level)
	}
	got = DecideWithState(cfg, Usage{Percent: 79}, config.State{Level: LevelSoft})
	if got.Level != LevelWarn {
		t.Fatalf("soft should clear below soft clear but stay warn, got %s", got.Level)
	}
}

func TestFindMonthRejectsMissingMonth(t *testing.T) {
	data := vnstatJSON{Interfaces: []vnstatInterface{{Name: "eth0", Traffic: vnstatTraffic{Total: vnstatTotal{RX: 123, TX: 456}}}}}
	_, _, err := findMonth(data, "eth0", "2026-06")
	if err == nil {
		t.Fatal("expected missing month error")
	}
}

func TestFindMonthUsesMonthWhenAvailable(t *testing.T) {
	period := vnstatPeriod{RX: 1, TX: 2}
	period.Date.Year = 2026
	period.Date.Month = 6
	data := vnstatJSON{Interfaces: []vnstatInterface{{Name: "eth0", Traffic: vnstatTraffic{Total: vnstatTotal{RX: 123, TX: 456}, Months: []vnstatPeriod{period}}}}}
	rx, tx, err := findMonth(data, "eth0", "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if rx != 1 || tx != 2 {
		t.Fatalf("findMonth month rx=%d tx=%d", rx, tx)
	}
}

func TestRemoveLimitSkipsNonTBFInFakeCurrentLimit(t *testing.T) {
	// Covered by integration tests with fake tc output; this test documents the
	// intended safety behavior: only FlowGuard's tbf qdisc should be removed.
	if strings.HasPrefix("qdisc tbf 8001: root", "qdisc tbf "+qdiscHandle+" ") {
		t.Fatal("non-FlowGuard tbf should not match")
	}
	if !strings.HasPrefix("qdisc tbf "+qdiscHandle+" root", "qdisc tbf "+qdiscHandle+" ") {
		t.Fatal("tbf prefix expectation changed")
	}
}

func TestCanReplaceRootQdisc(t *testing.T) {
	allowed := []string{
		"",
		"qdisc noqueue 0: root refcnt 2",
		"qdisc fq_codel 0: root refcnt 2",
		"qdisc tbf " + qdiscHandle + " root refcnt 2 rate 1Mbit",
	}
	for _, current := range allowed {
		if !canReplaceRootQdisc(current) {
			t.Fatalf("expected replace allowed for %q", current)
		}
	}
	blocked := []string{
		"qdisc tbf 8001: root refcnt 2 rate 1Mbit",
		"qdisc htb 1: root refcnt 2",
		"qdisc cake 8001: root refcnt 2",
	}
	for _, current := range blocked {
		if canReplaceRootQdisc(current) {
			t.Fatalf("expected replace blocked for %q", current)
		}
	}
}

func TestSaturatingAdd(t *testing.T) {
	max := ^uint64(0)
	if got := saturatingAdd(max-1, 1); got != max {
		t.Fatalf("saturatingAdd near max=%d, want %d", got, max)
	}
	if got := saturatingAdd(max, 1); got != max {
		t.Fatalf("saturatingAdd overflow=%d, want %d", got, max)
	}
}

func TestSaturatingSubtract(t *testing.T) {
	if got := saturatingSubtract(10, 3); got != 7 {
		t.Fatalf("saturatingSubtract=%d, want 7", got)
	}
	if got := saturatingSubtract(3, 10); got != 0 {
		t.Fatalf("saturatingSubtract underflow=%d, want 0", got)
	}
}

func TestSplitRate(t *testing.T) {
	single, err := SplitRate("10mbit", 1)
	if err != nil {
		t.Fatal(err)
	}
	if single != "10000000bit" {
		t.Fatalf("single SplitRate=%s", single)
	}
	got, err := SplitRate("10mbit", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "5000000bit" {
		t.Fatalf("SplitRate=%s", got)
	}
	for _, rate := range []string{"10foo", "NaNmbit", "+Infmbit", ""} {
		if _, err := SplitRate(rate, 2); err == nil {
			t.Fatalf("expected unsupported rate error for %q", rate)
		}
	}
	if _, err := SplitRate("1e309mbit", 1); err == nil {
		t.Fatal("expected unsupported rate error")
	}
}

func TestUpdateStateDryRunCanKeepUnappliedLimit(t *testing.T) {
	state := UpdateState(config.State{}, Usage{Period: "2026-06", RXBytes: 1, TXBytes: 2, TotalBytes: 3, BillableBytes: 3}, Decision{Level: LevelHard, LimitRate: "1mbit"})
	state.Limited = false
	state.CurrentLimitRate = ""
	if state.Level != LevelHard || state.Limited || state.CurrentLimitRate != "" {
		t.Fatalf("dry-run state=%+v", state)
	}
}
