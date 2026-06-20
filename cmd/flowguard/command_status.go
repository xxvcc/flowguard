package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/traffic"
	"flowguard/internal/util"
)

type statusReport struct {
	SchemaVersion        int                 `json:"schema_version"`
	Config               map[string]any      `json:"config"`
	Usage                traffic.Usage       `json:"usage"`
	RecentUsage          traffic.RecentUsage `json:"recent_usage"`
	RecentUsageAvailable bool                `json:"recent_usage_available"`
	RecentUsageError     string              `json:"recent_usage_error,omitempty"`
	Forecast             usageForecast       `json:"forecast"`
	State                config.State        `json:"state"`
	Decision             traffic.Decision    `json:"decision"`
	RemainingBytes       uint64              `json:"remaining_bytes"`
	DeltaBytes           map[string]uint64   `json:"delta_bytes"`
	TC                   map[string]string   `json:"tc"`
}

type usageForecast struct {
	Available           bool    `json:"available"`
	AvgPerDayBytes      uint64  `json:"avg_per_day_bytes"`
	WindowDays          int     `json:"window_days"`
	WindowSource        string  `json:"window_source"`
	DaysRemaining       int     `json:"days_remaining"`
	PredictedTotalBytes uint64  `json:"predicted_total_bytes"`
	PredictedPercent    float64 `json:"predicted_percent"`
	DaysToSoftLimit     int     `json:"days_to_soft_limit"`
	SoftLimitReached    bool    `json:"soft_limit_reached"`
}

func printStatus(cfg config.Config, statePath string, jsonOutput bool, verbose bool) error {
	now := time.Now()
	snapshot, err := traffic.ReadSnapshot()
	if err != nil {
		return err
	}
	usage, err := snapshot.Usage(cfg, now)
	if err != nil {
		return err
	}
	state, err := config.LoadState(statePath, usage.Period)
	if err != nil {
		return err
	}
	decision := traffic.DecideWithState(cfg, usage, state)
	report := buildStatusReport(cfg, usage, state, decision)
	recent, recentErr := snapshot.RecentUsage(cfg, now)
	if recentErr == nil {
		report.RecentUsage = recent
		report.RecentUsageAvailable = true
	} else {
		report.RecentUsageError = recentErr.Error()
	}
	report.Forecast = computeForecast(cfg, usage, recent, recentErr == nil, now)
	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	printStatusDashboard(cfg, report, recentErr, now)
	if verbose {
		printStatusTechnicalDetails(cfg, report, recentErr)
	}
	return nil
}

func printStatusDashboard(cfg config.Config, report statusReport, recentErr error, now time.Time) {
	usage := report.Usage
	state := report.State
	decision := report.Decision
	if isZH(cfg) {
		fmt.Println(statusTitle("FlowGuard 流量看板"))
		fmt.Printf("%-10s %s\n", "账期", periodRangeText(cfg, now, true))
		fmt.Printf("%-10s %s\n", "网卡", strings.Join(cfg.Interfaces, ","))
		fmt.Printf("%-10s %s\n", "状态", statusBadge(state, decision)+" "+stateText(state, decision, true))
		fmt.Printf("%-10s %s %.2f%%  %s / %s\n", "进度", progressBar(usage.Percent, 24), usage.Percent, util.FormatBytes(usage.BillableBytes), util.FormatBytes(cfg.AllowanceBytes))
		fmt.Printf("%-10s %s\n", "剩余", util.FormatBytes(report.RemainingBytes))
		fmt.Println()
		fmt.Println(statusSection("近期计费用量"))
		printRecentUsageDashboard(report.RecentUsage, report.RecentUsageAvailable, true)
		if recentErr != nil {
			fmt.Printf("  说明      近期统计暂不可用（可用 --verbose 查看原因）\n")
		}
		fmt.Println()
		fmt.Println(statusSection("预测"))
		printForecastDashboard(report.Forecast, true)
		fmt.Println()
		fmt.Println(statusSection("流量方向"))
		fmt.Printf("  %-8s %s\n", "入站", util.FormatBytes(usage.RXBytes))
		fmt.Printf("  %-8s %s\n", "出站", util.FormatBytes(usage.TXBytes))
		fmt.Println()
		fmt.Println(statusSection("策略"))
		fmt.Printf("  %-8s %s\n", "计费方式", billingModeText(cfg, true))
		fmt.Printf("  %-8s %s\n", "本次决策", decisionText(decision, true))
		if report.RemainingBytes <= cfg.AllowanceBytes/10 {
			fmt.Printf("  %-8s %s\n", "风险", statusWarn("剩余额度低于 10%"))
		}
		return
	}
	fmt.Println(statusTitle("FlowGuard dashboard"))
	fmt.Printf("%-12s %s\n", "Period", periodRangeText(cfg, now, false))
	fmt.Printf("%-12s %s\n", "Interfaces", strings.Join(cfg.Interfaces, ","))
	fmt.Printf("%-12s %s %s\n", "State", statusBadge(state, decision), stateText(state, decision, false))
	fmt.Printf("%-12s %s %.2f%%  %s / %s\n", "Progress", progressBar(usage.Percent, 24), usage.Percent, util.FormatBytes(usage.BillableBytes), util.FormatBytes(cfg.AllowanceBytes))
	fmt.Printf("%-12s %s\n", "Remaining", util.FormatBytes(report.RemainingBytes))
	fmt.Println()
	fmt.Println(statusSection("Recent billable usage"))
	printRecentUsageDashboard(report.RecentUsage, report.RecentUsageAvailable, false)
	if recentErr != nil {
		fmt.Printf("  %-10s recent usage unavailable (use --verbose for details)\n", "Note")
	}
	fmt.Println()
	fmt.Println(statusSection("Forecast"))
	printForecastDashboard(report.Forecast, false)
	fmt.Println()
	fmt.Println(statusSection("Traffic direction"))
	fmt.Printf("  %-10s %s\n", "Inbound", util.FormatBytes(usage.RXBytes))
	fmt.Printf("  %-10s %s\n", "Outbound", util.FormatBytes(usage.TXBytes))
	fmt.Println()
	fmt.Println(statusSection("Policy"))
	fmt.Printf("  %-10s %s\n", "Billing", billingModeText(cfg, false))
	fmt.Printf("  %-10s %s\n", "Decision", decisionText(decision, false))
	if report.RemainingBytes <= cfg.AllowanceBytes/10 {
		fmt.Printf("  %-10s %s\n", "Risk", statusWarn("remaining allowance is below 10%"))
	}
}

func printStatusTechnicalDetails(cfg config.Config, report statusReport, recentErr error) {
	if isZH(cfg) {
		fmt.Println("\n技术细节：")
		if recentErr != nil {
			fmt.Printf("近期统计错误：%v\n", recentErr)
		}
		fmt.Printf("后台上次检查后：%s\n", deltaText(report.DeltaBytes, true))
		fmt.Printf("state: level=%s limited=%v rate=%s applied=%s\n", report.State.Level, report.State.Limited, report.State.CurrentLimitRate, report.State.AppliedLimitKey)
		for iface, tc := range report.TC {
			fmt.Printf("tc[%s]: %s\n", iface, tc)
		}
		return
	}
	fmt.Println("\nTechnical details:")
	if recentErr != nil {
		fmt.Printf("Recent usage error: %v\n", recentErr)
	}
	fmt.Printf("Since background last check: %s\n", deltaText(report.DeltaBytes, false))
	fmt.Printf("state: level=%s limited=%v rate=%s applied=%s\n", report.State.Level, report.State.Limited, report.State.CurrentLimitRate, report.State.AppliedLimitKey)
	for iface, tc := range report.TC {
		fmt.Printf("tc[%s]: %s\n", iface, tc)
	}
}

func printRecentUsageDashboard(recent traffic.RecentUsage, available bool, zh bool) {
	if !available {
		if zh {
			fmt.Printf("  %-8s %s\n", "今天", "暂不可用")
			fmt.Printf("  %-8s %s\n", "昨天", "暂不可用")
			fmt.Printf("  %-8s %s\n", "本周", "暂不可用")
		} else {
			fmt.Printf("  %-10s %s\n", "Today", "unavailable")
			fmt.Printf("  %-10s %s\n", "Yesterday", "unavailable")
			fmt.Printf("  %-10s %s\n", "This week", "unavailable")
		}
		return
	}
	if zh {
		fmt.Printf("  %-8s %s\n", "今天", util.FormatBytes(recent.Today.BillableBytes))
		fmt.Printf("  %-8s %s\n", "昨天", util.FormatBytes(recent.Yesterday.BillableBytes))
		fmt.Printf("  %-8s %s  （周一开始）\n", "本周", util.FormatBytes(recent.ThisWeek.BillableBytes))
		return
	}
	fmt.Printf("  %-10s %s\n", "Today", util.FormatBytes(recent.Today.BillableBytes))
	fmt.Printf("  %-10s %s\n", "Yesterday", util.FormatBytes(recent.Yesterday.BillableBytes))
	fmt.Printf("  %-10s %s  (starts Monday)\n", "This week", util.FormatBytes(recent.ThisWeek.BillableBytes))
}

func printForecastDashboard(f usageForecast, zh bool) {
	if !f.Available {
		if zh {
			fmt.Printf("  %-8s %s\n", "状态", "数据不足，暂不预测")
		} else {
			fmt.Printf("  %-10s %s\n", "Status", "insufficient data")
		}
		return
	}
	if zh {
		fmt.Printf("  %-8s %s/天（依据：%s）\n", "日均", util.FormatBytes(f.AvgPerDayBytes), forecastWindowText(f.WindowSource, true))
		fmt.Printf("  %-8s %s（约 %.1f%%，剩余 %d 天）\n", "本月预计", util.FormatBytes(f.PredictedTotalBytes), f.PredictedPercent, f.DaysRemaining)
		if f.SoftLimitReached {
			fmt.Printf("  %-8s %s\n", "软限速", "已达到软限速阈值")
		} else if f.AvgPerDayBytes == 0 {
			fmt.Printf("  %-8s %s\n", "软限速", "按当前用量短期不会触发")
		} else if f.DaysToSoftLimit <= 0 {
			fmt.Printf("  %-8s %s\n", "软限速", "按当前用量本周期内不会触发")
		} else {
			fmt.Printf("  %-8s 约 %d 天后\n", "软限速", f.DaysToSoftLimit)
		}
		return
	}
	fmt.Printf("  %-10s %s/day (based on %s)\n", "Avg/day", util.FormatBytes(f.AvgPerDayBytes), forecastWindowText(f.WindowSource, false))
	fmt.Printf("  %-10s %s (~%.1f%%, %d days left)\n", "Month est.", util.FormatBytes(f.PredictedTotalBytes), f.PredictedPercent, f.DaysRemaining)
	if f.SoftLimitReached {
		fmt.Printf("  %-10s soft limit already reached\n", "Soft ETA")
	} else if f.AvgPerDayBytes == 0 {
		fmt.Printf("  %-10s no soft-limit ETA at current rate\n", "Soft ETA")
	} else if f.DaysToSoftLimit <= 0 {
		fmt.Printf("  %-10s soft limit will not trigger this period\n", "Soft ETA")
	} else {
		fmt.Printf("  %-10s ~%d days\n", "Soft ETA", f.DaysToSoftLimit)
	}
}

func forecastWindowText(source string, zh bool) string {
	if zh {
		switch source {
		case "this_week":
			return "本周用量"
		case "today":
			return "今天用量"
		case "yesterday":
			return "昨天用量"
		}
		return source
	}
	switch source {
	case "this_week":
		return "this week"
	case "today":
		return "today"
	case "yesterday":
		return "yesterday"
	}
	return source
}

func computeForecast(cfg config.Config, usage traffic.Usage, recent traffic.RecentUsage, recentAvailable bool, now time.Time) usageForecast {
	f := usageForecast{}
	periodStart := traffic.CurrentPeriodStart(now, cfg.PeriodDay)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	periodEnd := periodStart.AddDate(0, 1, -1)
	totalDays := calendarDaysInclusive(periodStart, periodEnd)
	daysElapsed := calendarDaysInclusive(periodStart, today)
	if daysElapsed < 1 {
		daysElapsed = 1
	}
	if daysElapsed > totalDays {
		daysElapsed = totalDays
	}
	daysRemaining := totalDays - daysElapsed
	if daysRemaining < 0 {
		daysRemaining = 0
	}
	f.DaysRemaining = daysRemaining
	if !recentAvailable {
		return f
	}
	avg, window, source := pickForecastWindow(now, recent)
	if avg == 0 || window == 0 {
		return f
	}
	f.Available = true
	f.AvgPerDayBytes = avg
	f.WindowDays = window
	f.WindowSource = source
	predicted := saturatingAddU64(usage.BillableBytes, avg*uint64(daysRemaining))
	f.PredictedTotalBytes = predicted
	if cfg.AllowanceBytes > 0 {
		f.PredictedPercent = float64(predicted) * 100 / float64(cfg.AllowanceBytes)
	}
	softBytes := uint64(0)
	if cfg.AllowanceBytes > 0 && cfg.Thresholds.SoftPercent > 0 {
		softBytes = uint64(float64(cfg.AllowanceBytes) * cfg.Thresholds.SoftPercent / 100)
	}
	if softBytes > 0 {
		if usage.BillableBytes >= softBytes {
			f.SoftLimitReached = true
		} else {
			remainingToSoft := softBytes - usage.BillableBytes
			daysToSoft := int((remainingToSoft + avg - 1) / avg)
			if daysToSoft > daysRemaining {
				f.DaysToSoftLimit = 0
			} else {
				f.DaysToSoftLimit = daysToSoft
			}
		}
	}
	return f
}

func calendarDaysInclusive(start time.Time, end time.Time) int {
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endKey := end.Year()*10000 + int(end.Month())*100 + end.Day()
	startKey := start.Year()*10000 + int(start.Month())*100 + start.Day()
	if endKey < startKey {
		return 0
	}
	days := 0
	for cursor := start; cursor.Year()*10000+int(cursor.Month())*100+cursor.Day() <= endKey; cursor = cursor.AddDate(0, 0, 1) {
		days++
	}
	return days
}

func pickForecastWindow(now time.Time, recent traffic.RecentUsage) (uint64, int, string) {
	weekday := int(now.Weekday())
	weekDays := ((weekday + 6) % 7) + 1
	if recent.ThisWeekWindowDays > 0 {
		weekDays = recent.ThisWeekWindowDays
	}
	if recent.ThisWeek.BillableBytes > 0 && weekDays > 0 {
		return recent.ThisWeek.BillableBytes / uint64(weekDays), weekDays, "this_week"
	}
	if recent.Today.BillableBytes > 0 {
		return recent.Today.BillableBytes, 1, "today"
	}
	if recent.Yesterday.BillableBytes > 0 {
		return recent.Yesterday.BillableBytes, 1, "yesterday"
	}
	return 0, 0, ""
}

func saturatingAddU64(a uint64, b uint64) uint64 {
	if ^uint64(0)-a < b {
		return ^uint64(0)
	}
	return a + b
}

func statusTitle(text string) string {
	return statusColor("\033[1;36m", text)
}

func statusSection(text string) string {
	return statusColor("\033[1m", text)
}

func statusWarn(text string) string {
	return statusColor("\033[33m", text)
}

func statusGood(text string) string {
	return statusColor("\033[32m", text)
}

func statusBad(text string) string {
	return statusColor("\033[31m", text)
}

func statusColor(code string, text string) string {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || !stdoutIsTerminal() {
		return text
	}
	return code + text + "\033[0m"
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func progressBar(percent float64, width int) string {
	if width <= 0 {
		return ""
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent / 100 * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	if percent >= 95 {
		return statusBad(bar)
	}
	if percent >= 85 {
		return statusWarn(bar)
	}
	return statusGood(bar)
}

func statusBadge(state config.State, decision traffic.Decision) string {
	if state.Limited || decision.Level == traffic.LevelHard {
		return statusBad("●")
	}
	if decision.Level == traffic.LevelSoft || decision.Level == traffic.LevelWarn || decision.LimitRate != "" {
		return statusWarn("●")
	}
	return statusGood("●")
}

func periodRangeText(cfg config.Config, now time.Time, zh bool) string {
	start := traffic.CurrentPeriodStart(now, cfg.PeriodDay)
	end := start.AddDate(0, 1, -1)
	if !zh {
		return fmt.Sprintf("%s to %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	return fmt.Sprintf("%s 至 %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
}

func billingModeText(cfg config.Config, zh bool) string {
	if zh {
		if cfg.BillingMode == "outbound" {
			return "仅出站流量"
		}
		return "总流量（入站 + 出站）"
	}
	if cfg.BillingMode == "outbound" {
		return "outbound only"
	}
	return "total (inbound + outbound)"
}

func decisionText(decision traffic.Decision, zh bool) string {
	level := levelText(decision.Level, zh)
	if decision.LimitRate == "" {
		if zh {
			return level + "，无需限速"
		}
		return level + ", no limit needed"
	}
	if zh {
		return level + "，限速 " + decision.LimitRate
	}
	return level + ", limit " + decision.LimitRate
}

func stateText(state config.State, decision traffic.Decision, zh bool) string {
	level := levelText(state.Level, zh)
	if state.Limited {
		if state.CurrentLimitRate != "" {
			if zh {
				return level + "，已限速 " + state.CurrentLimitRate
			}
			return level + ", limited at " + state.CurrentLimitRate
		}
		if zh {
			return level + "，已限速"
		}
		return level + ", limited"
	}
	if decision.LimitRate != "" {
		if zh {
			return level + "，即将限速 " + decision.LimitRate
		}
		return level + ", will limit at " + decision.LimitRate
	}
	if zh {
		return level + "，未限速"
	}
	return level + ", not limited"
}

func levelText(level string, zh bool) string {
	if !zh {
		return level
	}
	switch level {
	case traffic.LevelNormal:
		return "正常"
	case traffic.LevelWarn:
		return "提醒"
	case traffic.LevelSoft:
		return "轻度限速"
	case traffic.LevelHard:
		return "强限速"
	default:
		return level
	}
}

func deltaText(delta map[string]uint64, zh bool) string {
	if delta["rx"] == 0 && delta["tx"] == 0 && delta["total"] == 0 {
		if zh {
			return "暂无新增流量"
		}
		return "no new traffic"
	}
	if zh {
		return fmt.Sprintf("入站 +%s，出站 +%s，总计 +%s", util.FormatBytes(delta["rx"]), util.FormatBytes(delta["tx"]), util.FormatBytes(delta["total"]))
	}
	return fmt.Sprintf("inbound +%s, outbound +%s, total +%s", util.FormatBytes(delta["rx"]), util.FormatBytes(delta["tx"]), util.FormatBytes(delta["total"]))
}

func buildStatusReport(cfg config.Config, usage traffic.Usage, state config.State, decision traffic.Decision) statusReport {
	remaining := uint64(0)
	if cfg.AllowanceBytes > usage.BillableBytes {
		remaining = cfg.AllowanceBytes - usage.BillableBytes
	}
	delta := map[string]uint64{"rx": 0, "tx": 0, "total": 0, "billable": 0}
	if usage.RXBytes >= state.LastRXBytes {
		delta["rx"] = usage.RXBytes - state.LastRXBytes
	}
	if usage.TXBytes >= state.LastTXBytes {
		delta["tx"] = usage.TXBytes - state.LastTXBytes
	}
	if usage.TotalBytes >= state.LastTotalBytes {
		delta["total"] = usage.TotalBytes - state.LastTotalBytes
	}
	if usage.BillableBytes >= state.LastBillableBytes {
		delta["billable"] = usage.BillableBytes - state.LastBillableBytes
	}
	tc := map[string]string{}
	for _, iface := range cfg.Interfaces {
		tc[iface] = traffic.CurrentLimit(iface)
	}
	configSummary := map[string]any{"interface": cfg.Interface, "interfaces": cfg.Interfaces, "allowance_bytes": cfg.AllowanceBytes, "language": cfg.Language, "period_day": cfg.PeriodDay, "billing_mode": cfg.BillingMode, "check_interval_seconds": cfg.CheckIntervalSeconds, "telegram_enabled": cfg.Telegram.Enabled}
	return statusReport{SchemaVersion: 1, Config: configSummary, Usage: usage, State: state, Decision: decision, RemainingBytes: remaining, DeltaBytes: delta, TC: tc}

}
