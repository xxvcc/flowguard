package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/service"
	"flowguard/internal/traffic"
	"flowguard/internal/util"
)

func cmdRollback(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	backupPath := fs.String("backup", "", "backup path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	backup := *backupPath
	if backup == "" {
		matches, err := filepath.Glob(*cfgPath + ".bak.*")
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			return fmt.Errorf("no backups found for %s", *cfgPath)
		}
		sort.Strings(matches)
		backup = matches[len(matches)-1]
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if _, err := config.Backup(*cfgPath); err != nil {
		return err
	}
	if err := config.Save(*cfgPath, cfg); err != nil {
		return err
	}
	if *cfgPath == config.DefaultConfigPath {
		if err := service.Restart(); err != nil {
			if isZH(cfg) {
				fmt.Fprintf(os.Stderr, "服务重启警告：%v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "service restart warning: %v\n", err)
			}
		}
	}
	if isZH(cfg) {
		fmt.Printf("已将 %s 回滚到备份 %s\n", *cfgPath, backup)
	} else {
		fmt.Printf("Rolled back %s from %s\n", *cfgPath, backup)
	}
	return nil
}

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	failures := 0
	check := func(name string, err error) {
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", name, err)
			failures++
		} else {
			fmt.Printf("OK   %s\n", name)
		}
	}
	warn := func(name string, err error) {
		if err != nil {
			fmt.Printf("WARN %s: %v\n", name, err)
		} else {
			fmt.Printf("OK   %s\n", name)
		}
	}
	cfg, err := config.Load(*cfgPath)
	check("config", err)
	check("vnstat command", commandCheck("vnstat"))
	check("tc command", commandCheck("tc"))
	if err == nil {
		for _, iface := range cfg.Interfaces {
			check("interface "+iface, interfaceCheck(iface))
			check("tc "+iface, nilIfNotUnknown(traffic.CurrentLimit(iface)))
		}
		_, usageErr := traffic.ReadUsage(cfg, time.Now())
		check("vnstat usage", usageErr)
		warn("install baseline date", baselineDateCheck(cfg))
		check("vnstat reset detection", vnstatResetCheck(cfg))
		check("recent baseline reset detection", recentBaselineResetCheck(cfg))
	}
	if util.CommandExists("systemctl") {
		_, serviceErr := util.Run(10*time.Second, "systemctl", "is-active", "flowguard")
		check("flowguard service", serviceErr)
	}
	if failures > 0 {
		return fmt.Errorf("doctor found %d issue(s)", failures)
	}
	return nil
}

func commandCheck(name string) error {
	if !util.CommandExists(name) {
		return fmt.Errorf("%s not found", name)
	}
	return nil
}

func parseInterfaces(value string) ([]string, error) {
	if value == "auto-public" {
		items, err := util.DetectPublicInterfaces()
		if err != nil {
			return nil, err
		}
		return items, nil
	}
	var items []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			if !config.ValidInterfaceName(item) {
				return nil, fmt.Errorf("invalid interface name %q", item)
			}
			items = append(items, item)
		}
	}
	if len(items) == 0 && value != "" {
		if !config.ValidInterfaceName(value) {
			return nil, fmt.Errorf("invalid interface name %q", value)
		}
		items = []string{value}
	}
	return items, nil
}

func interfaceCheck(name string) error {
	if !config.ValidInterfaceName(name) {
		return fmt.Errorf("invalid interface name %q", name)
	}
	if _, err := os.Stat("/sys/class/net/" + name); err != nil {
		return err
	}
	return nil
}

func nilIfNotUnknown(value string) error {
	if value == "unknown" {
		return fmt.Errorf("tc status unknown")
	}
	return nil
}

func baselineDateCheck(cfg config.Config) error {
	if cfg.BaselineAt == "" {
		return fmt.Errorf("baseline_at not set; run `sudo flowguard modify --reset-recent-baseline` once after upgrading to populate today/this-week baseline")
	}
	if _, err := time.Parse("2006-01-02", cfg.BaselineAt); err != nil {
		return fmt.Errorf("baseline_at %q is not a YYYY-MM-DD date: %w", cfg.BaselineAt, err)
	}
	return nil
}

func vnstatResetCheck(cfg config.Config) error {
	now := time.Now()
	currentPeriod := traffic.CurrentPeriod(now, cfg.PeriodDay)
	if cfg.InitialPeriod != "" && cfg.InitialPeriod != currentPeriod {
		return nil
	}
	// Without baseline values to compare against, this check has nothing to
	// say (manual configs, fresh installs before setInstallBaseline ran, or
	// older versions). Stay silent rather than report false positives.
	if cfg.BaselineRXBytes == 0 && cfg.BaselineTXBytes == 0 {
		return nil
	}
	stripped := cfg
	stripped.BaselineRXBytes = 0
	stripped.BaselineTXBytes = 0
	stripped.InitialRXBytes = 0
	stripped.InitialTXBytes = 0
	rawUsage, err := traffic.ReadUsage(stripped, now)
	if err != nil {
		return fmt.Errorf("could not read raw vnStat monthly: %w", err)
	}
	if rawUsage.RXBytes < cfg.BaselineRXBytes || rawUsage.TXBytes < cfg.BaselineTXBytes {
		return fmt.Errorf("vnstat monthly total (RX=%s, TX=%s) is below install baseline (RX=%s, TX=%s); vnStat database may have been reset; review monthly allowance accounting before relying on status, and run `sudo flowguard modify --reset-recent-baseline` only to repair today/this-week baselines",
			util.FormatBytes(rawUsage.RXBytes), util.FormatBytes(rawUsage.TXBytes),
			util.FormatBytes(cfg.BaselineRXBytes), util.FormatBytes(cfg.BaselineTXBytes),
		)
	}
	return nil
}

func recentBaselineResetCheck(cfg config.Config) error {
	if cfg.BaselineAt == "" {
		return nil
	}
	baselineDate, err := time.Parse("2006-01-02", cfg.BaselineAt)
	if err != nil {
		return nil
	}
	if cfg.BaselineDayRXBytes == 0 && cfg.BaselineDayTXBytes == 0 && cfg.BaselineWeekRXBytes == 0 && cfg.BaselineWeekTXBytes == 0 {
		return nil
	}
	now := time.Now()
	rawRecent, err := traffic.ReadRawRecentUsage(cfg, now)
	if err != nil {
		return fmt.Errorf("could not read raw vnStat daily data: %w", err)
	}
	today := dateOnlyForStatus(now)
	yesterday := today.AddDate(0, 0, -1)
	if sameStatusDate(baselineDate, today) && (rawRecent.Today.RXBytes < cfg.BaselineDayRXBytes || rawRecent.Today.TXBytes < cfg.BaselineDayTXBytes) {
		return fmt.Errorf("today vnStat total (RX=%s, TX=%s) is below recent baseline (RX=%s, TX=%s); vnStat daily database may have been reset; run `sudo flowguard modify --reset-recent-baseline`",
			util.FormatBytes(rawRecent.Today.RXBytes), util.FormatBytes(rawRecent.Today.TXBytes),
			util.FormatBytes(cfg.BaselineDayRXBytes), util.FormatBytes(cfg.BaselineDayTXBytes),
		)
	}
	if sameStatusDate(baselineDate, yesterday) && (rawRecent.Yesterday.RXBytes < cfg.BaselineDayRXBytes || rawRecent.Yesterday.TXBytes < cfg.BaselineDayTXBytes) {
		return fmt.Errorf("yesterday vnStat total (RX=%s, TX=%s) is below recent baseline (RX=%s, TX=%s); vnStat daily database may have been reset; run `sudo flowguard modify --reset-recent-baseline`",
			util.FormatBytes(rawRecent.Yesterday.RXBytes), util.FormatBytes(rawRecent.Yesterday.TXBytes),
			util.FormatBytes(cfg.BaselineDayRXBytes), util.FormatBytes(cfg.BaselineDayTXBytes),
		)
	}
	if sameStatusDate(weekStartForStatus(baselineDate), weekStartForStatus(today)) && (rawRecent.ThisWeek.RXBytes < cfg.BaselineWeekRXBytes || rawRecent.ThisWeek.TXBytes < cfg.BaselineWeekTXBytes) {
		return fmt.Errorf("this-week vnStat total (RX=%s, TX=%s) is below recent baseline (RX=%s, TX=%s); vnStat daily database may have been reset; run `sudo flowguard modify --reset-recent-baseline`",
			util.FormatBytes(rawRecent.ThisWeek.RXBytes), util.FormatBytes(rawRecent.ThisWeek.TXBytes),
			util.FormatBytes(cfg.BaselineWeekRXBytes), util.FormatBytes(cfg.BaselineWeekTXBytes),
		)
	}
	return nil
}

func dateOnlyForStatus(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func sameStatusDate(left time.Time, right time.Time) bool {
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day()
}

func weekStartForStatus(value time.Time) time.Time {
	value = dateOnlyForStatus(value)
	return value.AddDate(0, 0, -((int(value.Weekday()) + 6) % 7))
}
