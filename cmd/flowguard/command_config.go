package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/service"
	"flowguard/internal/traffic"
	"flowguard/internal/util"
)

func cmdConfigExample(args []string) error {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	cfg.Interfaces = []string{"eth0"}
	cfg.AllowanceBytes = 1000 * 1000 * 1000 * 1000
	cfg.InitialPeriod = traffic.CurrentPeriod(time.Now(), cfg.PeriodDay)
	cfg.Normalize()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func cmdModify(args []string) error {
	fs := flag.NewFlagSet("modify", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	statePath := fs.String("state", config.DefaultStatePath, "state path")
	allowance := fs.String("allowance", "", "monthly allowance")
	ifaceValue := fs.String("interface", "", "interface, comma list, or auto-public")
	billingMode := fs.String("billing-mode", "", "total or outbound")
	language := fs.String("language", "", "output language: zh or en")
	periodDay := fs.Int("period-day", 0, "billing period start day")
	softRate := fs.String("soft-rate", "", "soft limit rate")
	hardRate := fs.String("hard-rate", "", "hard limit rate")
	warnPercent := fs.Float64("warn-percent", 0, "warn threshold percent")
	softPercent := fs.Float64("soft-percent", 0, "soft threshold percent")
	hardPercent := fs.Float64("hard-percent", 0, "hard threshold percent")
	warnClearPercent := fs.Float64("warn-clear-percent", 0, "warn clear threshold percent")
	softClearPercent := fs.Float64("soft-clear-percent", 0, "soft clear threshold percent")
	hardClearPercent := fs.Float64("hard-clear-percent", 0, "hard clear threshold percent")
	tgEnabled := fs.Bool("telegram", false, "enable Telegram notifications")
	tgDisabled := fs.Bool("no-telegram", false, "disable Telegram notifications")
	tgToken := fs.String("tg-token", "", "Telegram bot token")
	tgChat := fs.String("tg-chat-id", "", "Telegram chat ID")
	firstLimitDryRun := fs.Bool("first-limit-dry-run", true, "enable first-limit dry-run protection")
	resetRecent := fs.Bool("reset-recent-baseline", false, "recapture today/this-week vnStat baselines from now (for upgrades from versions before recent-baseline tracking)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *allowance != "" {
		bytes, err := util.ParseBytes(*allowance)
		if err != nil {
			return err
		}
		cfg.AllowanceBytes = bytes
	}
	if *ifaceValue != "" {
		interfaces, err := parseInterfaces(*ifaceValue)
		if err != nil {
			return err
		}
		cfg.Interfaces = interfaces
		if len(cfg.Interfaces) > 0 {
			cfg.Interface = cfg.Interfaces[0]
		} else {
			cfg.Interface = *ifaceValue
		}
	}
	if *billingMode != "" {
		cfg.BillingMode = *billingMode
	}
	if *language != "" {
		cfg.Language = *language
	}
	if *periodDay != 0 {
		cfg.PeriodDay = *periodDay
	}
	if *softRate != "" {
		cfg.Limits.SoftRate = *softRate
	}
	if *hardRate != "" {
		cfg.Limits.HardRate = *hardRate
	}
	visited := visitedFlags(fs)
	if visited["warn-percent"] {
		cfg.Thresholds.WarnPercent = *warnPercent
	}
	if visited["soft-percent"] {
		cfg.Thresholds.SoftPercent = *softPercent
	}
	if visited["hard-percent"] {
		cfg.Thresholds.HardPercent = *hardPercent
	}
	if visited["warn-clear-percent"] {
		cfg.Thresholds.WarnClearPercent = *warnClearPercent
	}
	if visited["soft-clear-percent"] {
		cfg.Thresholds.SoftClearPercent = *softClearPercent
	}
	if visited["hard-clear-percent"] {
		cfg.Thresholds.HardClearPercent = *hardClearPercent
	}
	if *tgEnabled {
		cfg.Telegram.Enabled = true
	}
	if *tgDisabled {
		cfg.Telegram.Enabled = false
	}
	if *tgToken != "" {
		cfg.Telegram.BotToken = *tgToken
		cfg.Telegram.Enabled = true
	}
	if *tgChat != "" {
		cfg.Telegram.ChatID = *tgChat
		cfg.Telegram.Enabled = true
	}
	if visited["first-limit-dry-run"] {
		cfg.Safety.FirstLimitDryRun = *firstLimitDryRun
	}
	resetRecentMessage := ""
	if *resetRecent {
		now := time.Now()
		stripped := cfg
		stripped.BaselineAt = ""
		stripped.BaselineDayRXBytes = 0
		stripped.BaselineDayTXBytes = 0
		stripped.BaselineWeekRXBytes = 0
		stripped.BaselineWeekTXBytes = 0
		recent, err := traffic.ReadRawRecentUsage(stripped, now)
		if err != nil {
			return fmt.Errorf("reset-recent-baseline failed to read vnStat: %w", err)
		}
		cfg.BaselineAt = now.Format("2006-01-02")
		cfg.BaselineDayRXBytes = recent.Today.RXBytes
		cfg.BaselineDayTXBytes = recent.Today.TXBytes
		cfg.BaselineWeekRXBytes = recent.ThisWeek.RXBytes
		cfg.BaselineWeekTXBytes = recent.ThisWeek.TXBytes
		if isZH(cfg) {
			resetRecentMessage = fmt.Sprintf("已重设近期基线：baseline_at=%s 今日基线=%s/%s 本周基线=%s/%s",
				cfg.BaselineAt,
				util.FormatBytes(cfg.BaselineDayRXBytes), util.FormatBytes(cfg.BaselineDayTXBytes),
				util.FormatBytes(cfg.BaselineWeekRXBytes), util.FormatBytes(cfg.BaselineWeekTXBytes),
			)
		} else {
			resetRecentMessage = fmt.Sprintf("Recent baseline reset: baseline_at=%s today=%s/%s this_week=%s/%s",
				cfg.BaselineAt,
				util.FormatBytes(cfg.BaselineDayRXBytes), util.FormatBytes(cfg.BaselineDayTXBytes),
				util.FormatBytes(cfg.BaselineWeekRXBytes), util.FormatBytes(cfg.BaselineWeekTXBytes),
			)
		}
	}
	cfg.Normalize()
	if backup, err := config.Backup(*cfgPath); err == nil && backup != "" {
		printBackup(cfg, backup)
	} else if err != nil {
		return err
	}
	if err := config.Save(*cfgPath, cfg); err != nil {
		return err
	}
	if *cfgPath == config.DefaultConfigPath && !onlyResetRecentBaselineChange(visited) {
		if err := service.Restart(); err != nil {
			if isZH(cfg) {
				fmt.Fprintf(os.Stderr, "服务重启警告：%v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "service restart warning: %v\n", err)
			}
		}
	}
	_ = *statePath
	if resetRecentMessage != "" {
		fmt.Println(resetRecentMessage)
	}
	if isZH(cfg) {
		fmt.Printf("已更新配置：%s\n", *cfgPath)
	} else {
		fmt.Printf("Updated config: %s\n", *cfgPath)
	}
	if err := printStatus(cfg, *statePath, false, false); err != nil {
		if isZH(cfg) {
			fmt.Fprintf(os.Stderr, "状态检查警告：%v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "status warning: %v\n", err)
		}
	}
	return nil
}

func onlyResetRecentBaselineChange(visited map[string]bool) bool {
	if !visited["reset-recent-baseline"] {
		return false
	}
	for name := range visited {
		if name == "reset-recent-baseline" || name == "config" || name == "state" {
			continue
		}
		return false
	}
	return true
}

func visitedFlags(fs *flag.FlagSet) map[string]bool {
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})
	return visited
}

func cmdTopup(args []string) error {
	fs := flag.NewFlagSet("topup", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	statePath := fs.String("state", config.DefaultStatePath, "state path")
	amountFlag := fs.String("amount", "", "extra traffic amount, e.g. 100GB; bare numbers use GB")
	checkNow := fs.Bool("check-now", true, "run one check after updating allowance")
	if err := fs.Parse(args); err != nil {
		return err
	}
	amount := *amountFlag
	if amount == "" && fs.NArg() > 0 {
		amount = fs.Arg(0)
	}
	if amount == "" {
		return fmt.Errorf("topup amount is required, e.g. flowguard topup 100GB")
	}
	amount = withDefaultByteUnit(amount, "GB")
	extraBytes, err := util.ParseBytes(amount)
	if err != nil {
		return err
	}
	if extraBytes == 0 {
		return fmt.Errorf("topup amount must be greater than zero")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	oldAllowance := cfg.AllowanceBytes
	if ^uint64(0)-cfg.AllowanceBytes < extraBytes {
		return fmt.Errorf("allowance overflow")
	}
	cfg.AllowanceBytes += extraBytes
	if backup, err := config.Backup(*cfgPath); err == nil && backup != "" {
		printBackup(cfg, backup)
	} else if err != nil {
		return err
	}
	if err := config.Save(*cfgPath, cfg); err != nil {
		return err
	}
	if isZH(cfg) {
		fmt.Printf("流量额度已增加：%s + %s = %s\n", util.FormatBytes(oldAllowance), util.FormatBytes(extraBytes), util.FormatBytes(cfg.AllowanceBytes))
	} else {
		fmt.Printf("Allowance increased: %s + %s = %s\n", util.FormatBytes(oldAllowance), util.FormatBytes(extraBytes), util.FormatBytes(cfg.AllowanceBytes))
	}
	if *checkNow {
		if err := evaluateOnce(cfg, *statePath, true); err != nil {
			if isZH(cfg) {
				return fmt.Errorf("追加额度已保存，但立即检查失败：%w", err)
			}
			return fmt.Errorf("topup saved, but immediate check failed: %w", err)
		}
		if isZH(cfg) {
			fmt.Println("已重新检查当前用量和限速状态。")
		} else {
			fmt.Println("Rechecked current usage and limit state.")
		}
	}
	return nil
}

func withDefaultByteUnit(value string, unit string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return trimmed
	}
	for _, r := range trimmed {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return trimmed
		}
	}
	return trimmed + unit
}
