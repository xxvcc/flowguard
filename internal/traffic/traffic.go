package traffic

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/util"
)

const (
	LevelNormal = "normal"
	LevelWarn   = "warn"
	LevelSoft   = "soft_limit"
	LevelHard   = "hard_limit"
	qdiscHandle = "10f1:"
)

type Usage struct {
	Period        string   `json:"period"`
	Interfaces    []string `json:"interfaces"`
	RXBytes       uint64   `json:"rx_bytes"`
	TXBytes       uint64   `json:"tx_bytes"`
	TotalBytes    uint64   `json:"total_bytes"`
	BillableBytes uint64   `json:"billable_bytes"`
	Percent       float64  `json:"percent"`
}

type UsageSummary struct {
	RXBytes       uint64 `json:"rx_bytes"`
	TXBytes       uint64 `json:"tx_bytes"`
	TotalBytes    uint64 `json:"total_bytes"`
	BillableBytes uint64 `json:"billable_bytes"`
}

type RecentUsage struct {
	Today              UsageSummary `json:"today"`
	Yesterday          UsageSummary `json:"yesterday"`
	ThisWeek           UsageSummary `json:"this_week"`
	ThisWeekWindowDays int          `json:"this_week_window_days"`
}

type Decision struct {
	Level     string `json:"level"`
	LimitRate string `json:"limit_rate"`
}

type vnstatJSON struct {
	Interfaces []vnstatInterface `json:"interfaces"`
}

type vnstatInterface struct {
	Name    string        `json:"name"`
	Traffic vnstatTraffic `json:"traffic"`
}

type vnstatTraffic struct {
	Total  vnstatTotal    `json:"total"`
	Days   []vnstatPeriod `json:"day"`
	Months []vnstatPeriod `json:"month"`
}

type vnstatTotal struct {
	RX uint64 `json:"rx"`
	TX uint64 `json:"tx"`
}

type vnstatPeriod struct {
	Date struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	} `json:"date"`
	RX uint64 `json:"rx"`
	TX uint64 `json:"tx"`
}

func CurrentPeriod(now time.Time, periodDay int) string {
	start := CurrentPeriodStart(now, periodDay)
	return fmt.Sprintf("%04d-%02d", start.Year(), int(start.Month()))
}

func CurrentPeriodStart(now time.Time, periodDay int) time.Time {
	if periodDay < 1 || periodDay > 28 {
		periodDay = 1
	}
	year, month, day := now.Date()
	if day < periodDay {
		month--
		if month == 0 {
			month = 12
			year--
		}
	}
	return time.Date(year, month, periodDay, 0, 0, 0, 0, now.Location())
}

func ReadUsage(cfg config.Config, now time.Time) (Usage, error) {
	result, err := util.Run(30*time.Second, "vnstat", "--json")
	if err != nil {
		return Usage{}, err
	}
	var parsed vnstatJSON
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return Usage{}, err
	}
	period := CurrentPeriod(now, cfg.PeriodDay)
	var rx uint64
	var tx uint64
	for _, iface := range cfg.Interfaces {
		ifaceRX, ifaceTX, err := findUsage(parsed, iface, period, CurrentPeriodStart(now, cfg.PeriodDay), now, cfg.PeriodDay)
		if err != nil {
			return Usage{}, err
		}
		rx = saturatingAdd(rx, ifaceRX)
		tx = saturatingAdd(tx, ifaceTX)
	}
	if cfg.InitialPeriod == period {
		rx = saturatingSubtract(rx, cfg.BaselineRXBytes)
		tx = saturatingSubtract(tx, cfg.BaselineTXBytes)
		rx = saturatingAdd(rx, cfg.InitialRXBytes)
		tx = saturatingAdd(tx, cfg.InitialTXBytes)
	}
	total := saturatingAdd(rx, tx)
	billable := total
	if cfg.BillingMode == "outbound" {
		billable = tx
	}
	return Usage{
		Period:        period,
		Interfaces:    cfg.Interfaces,
		RXBytes:       rx,
		TXBytes:       tx,
		TotalBytes:    total,
		BillableBytes: billable,
		Percent:       float64(billable) * 100 / float64(cfg.AllowanceBytes),
	}, nil
}

func ReadRecentUsage(cfg config.Config, now time.Time) (RecentUsage, error) {
	raw, err := readRawRecentUsage(cfg, now)
	if err != nil {
		return RecentUsage{}, err
	}
	today := dateOnly(now)
	yesterday := today.AddDate(0, 0, -1)
	weekStart := today.AddDate(0, 0, -daysSinceMonday(today))
	todayRX, todayTX := raw.Today.RXBytes, raw.Today.TXBytes
	yesterdayRX, yesterdayTX := raw.Yesterday.RXBytes, raw.Yesterday.TXBytes
	weekRX, weekTX := raw.ThisWeek.RXBytes, raw.ThisWeek.TXBytes
	todayRX, todayTX = applyDailyBaseline(cfg, today, todayRX, todayTX)
	yesterdayRX, yesterdayTX = applyDailyBaseline(cfg, yesterday, yesterdayRX, yesterdayTX)
	weekRX, weekTX = applyWeekBaseline(cfg, weekStart, today, weekRX, weekTX)
	return RecentUsage{
		Today:              makeUsageSummary(cfg, todayRX, todayTX),
		Yesterday:          makeUsageSummary(cfg, yesterdayRX, yesterdayTX),
		ThisWeek:           makeUsageSummary(cfg, weekRX, weekTX),
		ThisWeekWindowDays: thisWeekWindowDays(cfg, weekStart, today),
	}, nil
}

// ReadRawRecentUsage returns today/yesterday/this-week vnStat sums without
// subtracting any FlowGuard install baseline; intended for capturing a fresh
// baseline (e.g. modify --reset-recent-baseline) or for diagnostics.
func ReadRawRecentUsage(cfg config.Config, now time.Time) (RecentUsage, error) {
	return readRawRecentUsage(cfg, now)
}

func readRawRecentUsage(cfg config.Config, now time.Time) (RecentUsage, error) {
	result, err := util.Run(30*time.Second, "vnstat", "--json")
	if err != nil {
		return RecentUsage{}, err
	}
	var parsed vnstatJSON
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return RecentUsage{}, err
	}
	today := dateOnly(now)
	yesterday := today.AddDate(0, 0, -1)
	weekStart := today.AddDate(0, 0, -daysSinceMonday(today))
	todayRX, todayTX, err := sumInterfaceDays(parsed, cfg.Interfaces, today, today)
	if err != nil {
		return RecentUsage{}, err
	}
	yesterdayRX, yesterdayTX, err := sumInterfaceDays(parsed, cfg.Interfaces, yesterday, yesterday)
	if err != nil {
		return RecentUsage{}, err
	}
	weekRX, weekTX, err := sumInterfaceDays(parsed, cfg.Interfaces, weekStart, today)
	if err != nil {
		return RecentUsage{}, err
	}
	return RecentUsage{
		Today:              makeUsageSummary(cfg, todayRX, todayTX),
		Yesterday:          makeUsageSummary(cfg, yesterdayRX, yesterdayTX),
		ThisWeek:           makeUsageSummary(cfg, weekRX, weekTX),
		ThisWeekWindowDays: daysBetweenInclusive(weekStart, today),
	}, nil
}

func thisWeekWindowDays(cfg config.Config, weekStart time.Time, today time.Time) int {
	baselineDate, ok := baselineDate(cfg)
	if !ok {
		return daysBetweenInclusive(weekStart, today)
	}
	if dateKey(today) < dateKey(baselineDate) {
		return 0
	}
	if dateKey(baselineDate) > dateKey(weekStart) {
		return daysBetweenInclusive(baselineDate, today)
	}
	return daysBetweenInclusive(weekStart, today)
}

func daysBetweenInclusive(start time.Time, end time.Time) int {
	start = dateOnly(start)
	end = dateOnly(end)
	if dateKey(end) < dateKey(start) {
		return 0
	}
	days := 0
	for cursor := start; dateKey(cursor) <= dateKey(end); cursor = cursor.AddDate(0, 0, 1) {
		days++
	}
	return days
}

func applyDailyBaseline(cfg config.Config, date time.Time, rx uint64, tx uint64) (uint64, uint64) {
	baselineDate, ok := baselineDate(cfg)
	if !ok {
		return rx, tx
	}
	date = dateOnly(date)
	if dateKey(date) < dateKey(baselineDate) {
		return 0, 0
	}
	if sameDate(date, baselineDate) {
		return saturatingSubtract(rx, cfg.BaselineDayRXBytes), saturatingSubtract(tx, cfg.BaselineDayTXBytes)
	}
	return rx, tx
}

func applyWeekBaseline(cfg config.Config, weekStart time.Time, weekEnd time.Time, rx uint64, tx uint64) (uint64, uint64) {
	baselineDate, ok := baselineDate(cfg)
	if !ok {
		return rx, tx
	}
	weekStart = dateOnly(weekStart)
	weekEnd = dateOnly(weekEnd)
	if dateKey(weekEnd) < dateKey(baselineDate) {
		return 0, 0
	}
	baselineWeekStart := baselineDate.AddDate(0, 0, -daysSinceMonday(baselineDate))
	if sameDate(weekStart, baselineWeekStart) && dateKey(baselineDate) <= dateKey(weekEnd) {
		return saturatingSubtract(rx, cfg.BaselineWeekRXBytes), saturatingSubtract(tx, cfg.BaselineWeekTXBytes)
	}
	return rx, tx
}

func sameDate(left time.Time, right time.Time) bool {
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day()
}

func dateKey(value time.Time) int {
	return value.Year()*10000 + int(value.Month())*100 + value.Day()
}

func baselineDate(cfg config.Config) (time.Time, bool) {
	if cfg.BaselineAt == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse("2006-01-02", cfg.BaselineAt)
	if err != nil {
		return time.Time{}, false
	}
	return dateOnly(parsed), true
}

func makeUsageSummary(cfg config.Config, rx uint64, tx uint64) UsageSummary {
	total := saturatingAdd(rx, tx)
	billable := total
	if cfg.BillingMode == "outbound" {
		billable = tx
	}
	return UsageSummary{RXBytes: rx, TXBytes: tx, TotalBytes: total, BillableBytes: billable}
}

func sumInterfaceDays(data vnstatJSON, interfaces []string, start time.Time, end time.Time) (uint64, uint64, error) {
	var rx uint64
	var tx uint64
	for _, iface := range interfaces {
		ifaceRX, ifaceTX, err := sumInterfaceDaysFor(data, iface, start, end)
		if err != nil {
			return 0, 0, err
		}
		rx = saturatingAdd(rx, ifaceRX)
		tx = saturatingAdd(tx, ifaceTX)
	}
	return rx, tx, nil
}

func sumInterfaceDaysFor(data vnstatJSON, iface string, start time.Time, end time.Time) (uint64, uint64, error) {
	for _, item := range data.Interfaces {
		if item.Name != iface {
			continue
		}
		return sumDays(item.Traffic.Days, start, end)
	}
	return 0, 0, fmt.Errorf("interface %q not found in vnstat output", iface)
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func daysSinceMonday(value time.Time) int {
	return (int(value.Weekday()) + 6) % 7
}

func saturatingAdd(left uint64, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func saturatingSubtract(left uint64, right uint64) uint64 {
	if right > left {
		return 0
	}
	return left - right
}

func findUsage(data vnstatJSON, iface string, period string, start time.Time, now time.Time, periodDay int) (uint64, uint64, error) {
	for _, item := range data.Interfaces {
		if item.Name != iface {
			continue
		}
		if periodDay != 1 {
			return sumDays(item.Traffic.Days, start, now)
		}
		for _, month := range item.Traffic.Months {
			key := fmt.Sprintf("%04d-%02d", month.Date.Year, month.Date.Month)
			if key == period {
				return month.RX, month.TX, nil
			}
		}
		return 0, 0, fmt.Errorf("vnStat monthly data for %s is missing on interface %q", period, iface)
	}
	return 0, 0, fmt.Errorf("interface %q not found in vnstat output", iface)
}

func findMonth(data vnstatJSON, iface string, period string) (uint64, uint64, error) {
	return findUsage(data, iface, period, time.Time{}, time.Time{}, 1)
}

func sumDays(days []vnstatPeriod, start time.Time, now time.Time) (uint64, uint64, error) {
	if len(days) == 0 {
		return 0, 0, fmt.Errorf("vnStat daily data is empty; custom billing period cannot be calculated")
	}
	var rx uint64
	var tx uint64
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, day := range days {
		if day.Date.Year == 0 || day.Date.Month == 0 || day.Date.Day == 0 {
			continue
		}
		date := time.Date(day.Date.Year, time.Month(day.Date.Month), day.Date.Day, 0, 0, 0, 0, now.Location())
		if date.Before(start) || date.After(end) {
			continue
		}
		rx = saturatingAdd(rx, day.RX)
		tx = saturatingAdd(tx, day.TX)
	}
	return rx, tx, nil
}

func Decide(cfg config.Config, usage Usage) Decision {
	return decideAt(cfg, usage.Percent, "")
}

func DecideWithState(cfg config.Config, usage Usage, state config.State) Decision {
	return decideAt(cfg, usage.Percent, state.Level)
}

func decideAt(cfg config.Config, percent float64, currentLevel string) Decision {
	if percent >= cfg.Thresholds.HardPercent || (currentLevel == LevelHard && percent >= cfg.Thresholds.HardClearPercent) {
		return Decision{Level: LevelHard, LimitRate: cfg.Limits.HardRate}
	}
	if percent >= cfg.Thresholds.SoftPercent || (currentLevel == LevelSoft && percent >= cfg.Thresholds.SoftClearPercent) {
		return Decision{Level: LevelSoft, LimitRate: cfg.Limits.SoftRate}
	}
	if percent >= cfg.Thresholds.WarnPercent || (currentLevel == LevelWarn && percent >= cfg.Thresholds.WarnClearPercent) {
		return Decision{Level: LevelWarn}
	}
	return Decision{Level: LevelNormal}
}

func ApplyLimit(iface string, rate string) error {
	limitRate, err := SplitRate(rate, 1)
	if err != nil {
		return err
	}
	current, err := currentLimit(iface)
	if err != nil {
		return err
	}
	if !canReplaceRootQdisc(current) {
		return fmt.Errorf("refusing to replace unmanaged root qdisc on %s: %s", iface, current)
	}
	_, err = util.Run(30*time.Second, "tc", "qdisc", "replace", "dev", iface, "root", "handle", qdiscHandle, "tbf", "rate", limitRate, "burst", "32kbit", "latency", "400ms")
	return err
}

func canReplaceRootQdisc(current string) bool {
	current = strings.TrimSpace(current)
	if current == "" {
		return true
	}
	if strings.HasPrefix(current, "qdisc tbf "+qdiscHandle+" ") {
		return true
	}
	for _, prefix := range []string{
		"qdisc noqueue ",
		"qdisc pfifo_fast ",
		"qdisc fq_codel ",
		"qdisc fq ",
		"qdisc mq ",
	} {
		if strings.HasPrefix(current, prefix) {
			return true
		}
	}
	return false
}

func SplitRate(rate string, parts int) (string, error) {
	if parts < 1 {
		parts = 1
	}
	trimmed := strings.TrimSpace(strings.ToLower(rate))
	if trimmed == "" {
		return "", fmt.Errorf("rate is required")
	}
	units := []struct {
		suffix string
		mult   float64
	}{
		{"gbit", 1000 * 1000 * 1000},
		{"mbit", 1000 * 1000},
		{"kbit", 1000},
		{"bit", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(trimmed, unit.suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(trimmed, unit.suffix))
			value, err := strconv.ParseFloat(number, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
				return "", fmt.Errorf("invalid rate %q", rate)
			}
			bitsPerSecond := value * unit.mult / float64(parts)
			if math.IsNaN(bitsPerSecond) || math.IsInf(bitsPerSecond, 0) || bitsPerSecond > float64(^uint64(0)) {
				return "", fmt.Errorf("rate %q is too large", rate)
			}
			if bitsPerSecond < 1 {
				bitsPerSecond = 1
			}
			return fmt.Sprintf("%dbit", uint64(math.Ceil(bitsPerSecond))), nil
		}
	}
	return "", fmt.Errorf("cannot split unsupported rate %q; use bit/kbit/mbit/gbit", rate)
}

func RemoveLimit(iface string) error {
	current, err := currentLimit(iface)
	if err != nil {
		return fmt.Errorf("tc status unknown for %s; refusing to remove qdisc: %w", iface, err)
	}
	if !strings.HasPrefix(current, "qdisc tbf "+qdiscHandle+" ") {
		return nil
	}
	_, err = util.Run(30*time.Second, "tc", "qdisc", "del", "dev", iface, "root")
	if err != nil && (strings.Contains(err.Error(), "No such file") || strings.Contains(err.Error(), "Invalid argument") || strings.Contains(err.Error(), "Cannot delete")) {
		return nil
	}
	return err
}

func CurrentLimit(iface string) string {
	current, err := currentLimit(iface)
	if err != nil {
		return "unknown"
	}
	return current
}

func currentLimit(iface string) (string, error) {
	result, err := util.Run(30*time.Second, "tc", "qdisc", "show", "dev", iface)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func UpdateState(state config.State, usage Usage, decision Decision) config.State {
	state.Period = usage.Period
	state.Level = decision.Level
	state.LastRXBytes = usage.RXBytes
	state.LastTXBytes = usage.TXBytes
	state.LastTotalBytes = usage.TotalBytes
	state.LastBillableBytes = usage.BillableBytes
	state.Limited = decision.LimitRate != ""
	state.CurrentLimitRate = decision.LimitRate
	return state
}
