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
	"flowguard/internal/install"
	"flowguard/internal/notify"
	"flowguard/internal/service"
	"flowguard/internal/traffic"
	"flowguard/internal/util"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "install":
		err = cmdInstall(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "modify":
		err = cmdModify(os.Args[2:])
	case "config-example":
		err = cmdConfigExample(os.Args[2:])
	case "rollback":
		err = cmdRollback(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "uninstall":
		err = cmdUninstall(os.Args[2:])
	case "limit":
		err = cmdLimit(os.Args[2:])
	case "unlimit":
		err = cmdUnlimit(os.Args[2:])
	case "test-notify":
		err = cmdTestNotify(os.Args[2:])
	case "check-once":
		err = cmdCheckOnce(os.Args[2:])
	case "help", "--help", "-h":
		usage()
		return
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

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
		cfg.Interfaces = parseInterfaces(*ifaceValue)
		if len(cfg.Interfaces) > 0 {
			cfg.Interface = cfg.Interfaces[0]
		} else {
			cfg.Interface = *ifaceValue
		}
	}
	if *billingMode != "" {
		cfg.BillingMode = *billingMode
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
	if *warnPercent != 0 {
		cfg.Thresholds.WarnPercent = *warnPercent
	}
	if *softPercent != 0 {
		cfg.Thresholds.SoftPercent = *softPercent
	}
	if *hardPercent != 0 {
		cfg.Thresholds.HardPercent = *hardPercent
	}
	if *warnClearPercent != 0 {
		cfg.Thresholds.WarnClearPercent = *warnClearPercent
	}
	if *softClearPercent != 0 {
		cfg.Thresholds.SoftClearPercent = *softClearPercent
	}
	if *hardClearPercent != 0 {
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
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "first-limit-dry-run" {
			cfg.Safety.FirstLimitDryRun = *firstLimitDryRun
		}
	})
	if backup, err := config.Backup(*cfgPath); err == nil && backup != "" {
		fmt.Printf("Backup: %s\n", backup)
	} else if err != nil {
		return err
	}
	if err := config.Save(*cfgPath, cfg); err != nil {
		return err
	}
	if *cfgPath == config.DefaultConfigPath {
		_ = service.Restart()
	}
	_ = *statePath
	fmt.Printf("Updated config: %s\n", *cfgPath)
	printStatus(cfg, *statePath, false)
	return nil
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	keepConfig := fs.Bool("keep-config", false, "keep config and state files")
	keepBinary := fs.Bool("keep-binary", false, "keep /usr/local/bin/flowguard")
	removeVnstat := fs.Bool("remove-vnstat", false, "remove configured interfaces from vnStat database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var cfg config.Config
	var haveConfig bool
	if loaded, err := config.Load(*cfgPath); err == nil {
		cfg = loaded
		haveConfig = true
	}
	_, _ = util.Run(30*time.Second, "systemctl", "disable", "--now", "flowguard")
	_ = os.Remove("/etc/systemd/system/flowguard.service")
	_, _ = util.Run(30*time.Second, "systemctl", "daemon-reload")
	if haveConfig {
		if err := removeLimits(cfg); err != nil {
			return err
		}
		if *removeVnstat {
			for _, iface := range cfg.Interfaces {
				_, _ = util.Run(30*time.Second, "vnstat", "--remove", "-i", iface, "--force")
			}
		}
	}
	if !*keepConfig {
		_ = os.RemoveAll("/etc/flowguard")
		_ = os.RemoveAll("/var/lib/flowguard")
	}
	if !*keepBinary {
		_ = os.Remove("/usr/local/bin/flowguard")
	}
	if haveConfig {
		fmt.Println("FlowGuard limits removed for configured interfaces.")
	}
	fmt.Println("FlowGuard uninstalled.")
	return nil
}

func usage() {
	fmt.Println(`flowguard commands:
  install       install dependencies, config, and service
  run           run monitor loop
  status        print current usage and limit status
  modify        update config values
  config-example print example config
  rollback      restore latest config backup
  doctor        diagnose config, vnStat, tc, and service
  uninstall     remove service and optionally files
  limit         apply outbound limit
  unlimit       remove outbound limit
  test-notify   send Telegram test notification
  check-once    run one monitor iteration`)
}

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
		_ = service.Restart()
	}
	fmt.Printf("Rolled back %s from %s\n", *cfgPath, backup)
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

func parseInterfaces(value string) []string {
	if value == "auto-public" {
		items, err := util.DetectPublicInterfaces()
		if err == nil {
			return items
		}
	}
	var items []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	if len(items) == 0 && value != "" {
		items = []string{value}
	}
	return items
}

func interfaceCheck(name string) error {
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

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	opts := install.Options{}
	fs.BoolVar(&opts.Yes, "yes", false, "non-interactive install")
	fs.StringVar(&opts.ConfigPath, "config", config.DefaultConfigPath, "config path")
	fs.StringVar(&opts.StatePath, "state", config.DefaultStatePath, "state path")
	fs.StringVar(&opts.Allowance, "allowance", "", "monthly allowance, e.g. 1000GB")
	fs.StringVar(&opts.InitialTotal, "initial-total", "", "initial total used this period")
	fs.StringVar(&opts.InitialRX, "initial-rx", "", "initial inbound used this period")
	fs.StringVar(&opts.InitialTX, "initial-tx", "", "initial outbound used this period")
	fs.StringVar(&opts.Interface, "interface", "", "network interface")
	fs.StringVar(&opts.TGToken, "tg-token", "", "Telegram bot token")
	fs.StringVar(&opts.TGChatID, "tg-chat-id", "", "Telegram chat ID")
	fs.StringVar(&opts.SoftRate, "soft-rate", "", "soft limit rate")
	fs.StringVar(&opts.HardRate, "hard-rate", "", "hard limit rate")
	fs.IntVar(&opts.CheckSeconds, "check-seconds", 60, "check interval seconds")
	fs.IntVar(&opts.PeriodDay, "period-day", 0, "billing period start day, 1-28")
	fs.StringVar(&opts.BillingMode, "billing-mode", "", "billing traffic mode: total or outbound")
	fs.StringVar(&opts.Language, "language", "", "interactive language: zh or en")
	firstLimitDryRun := fs.Bool("first-limit-dry-run", true, "enable first-limit dry-run protection")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "first-limit-dry-run" {
			opts.FirstLimitDryRun = firstLimitDryRun
		}
	})
	return install.Run(opts)
}

func cmdRun(args []string) error {
	cfgPath, statePath := pathsFromFlags("run", args)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	interval := time.Duration(cfg.CheckIntervalSeconds) * time.Second
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	for {
		if err := evaluateOnce(cfg, statePath, true); err != nil {
			fmt.Fprintf(os.Stderr, "loop error: %v\n", err)
		}
		time.Sleep(interval)
	}
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	statePath := fs.String("state", config.DefaultStatePath, "state path")
	jsonOutput := fs.Bool("json", false, "print JSON status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	return printStatus(cfg, *statePath, *jsonOutput)
}

type statusReport struct {
	Config         map[string]any    `json:"config"`
	Usage          traffic.Usage     `json:"usage"`
	State          config.State      `json:"state"`
	Decision       traffic.Decision  `json:"decision"`
	RemainingBytes uint64            `json:"remaining_bytes"`
	DeltaBytes     map[string]uint64 `json:"delta_bytes"`
	TC             map[string]string `json:"tc"`
}

func printStatus(cfg config.Config, statePath string, jsonOutput bool) error {
	usage, err := traffic.ReadUsage(cfg, time.Now())
	if err != nil {
		return err
	}
	state, err := config.LoadState(statePath, usage.Period)
	if err != nil {
		return err
	}
	decision := traffic.DecideWithState(cfg, usage, state)
	report := buildStatusReport(cfg, usage, state, decision)
	if jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("Interface: %s\n", cfg.Interface)
	fmt.Printf("Interfaces: %s\n", strings.Join(cfg.Interfaces, ","))
	fmt.Printf("Period: %s\n", usage.Period)
	fmt.Printf("Allowance: %s\n", util.FormatBytes(cfg.AllowanceBytes))
	fmt.Printf("Inbound: %s\n", util.FormatBytes(usage.RXBytes))
	fmt.Printf("Outbound: %s\n", util.FormatBytes(usage.TXBytes))
	fmt.Printf("Total: %s\n", util.FormatBytes(usage.TotalBytes))
	fmt.Printf("Billing mode: %s, billable: %s (%.2f%%)\n", cfg.BillingMode, util.FormatBytes(usage.BillableBytes), usage.Percent)
	fmt.Printf("Remaining: %s\n", util.FormatBytes(report.RemainingBytes))
	if report.RemainingBytes <= cfg.AllowanceBytes/10 {
		fmt.Println("Risk: remaining allowance is below 10%")
	}
	fmt.Printf("Delta since last check: inbound +%s, outbound +%s, total +%s\n", util.FormatBytes(report.DeltaBytes["rx"]), util.FormatBytes(report.DeltaBytes["tx"]), util.FormatBytes(report.DeltaBytes["total"]))
	fmt.Printf("Decision: %s", decision.Level)
	if decision.LimitRate != "" {
		fmt.Printf(" at %s", decision.LimitRate)
	}
	fmt.Println()
	fmt.Printf("State: level=%s limited=%v rate=%s\n", state.Level, state.Limited, state.CurrentLimitRate)
	for iface, tc := range report.TC {
		fmt.Printf("tc[%s]: %s\n", iface, tc)
	}
	return nil
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
	configSummary := map[string]any{"interface": cfg.Interface, "interfaces": cfg.Interfaces, "allowance_bytes": cfg.AllowanceBytes, "period_day": cfg.PeriodDay, "billing_mode": cfg.BillingMode, "check_interval_seconds": cfg.CheckIntervalSeconds, "telegram_enabled": cfg.Telegram.Enabled}
	return statusReport{Config: configSummary, Usage: usage, State: state, Decision: decision, RemainingBytes: remaining, DeltaBytes: delta, TC: tc}
}

func cmdLimit(args []string) error {
	fs := flag.NewFlagSet("limit", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	rate := fs.String("rate", "", "rate, e.g. 1mbit")
	iface := fs.String("interface", "", "network interface")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *iface == "" {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			return err
		}
		if *rate == "" {
			*rate = cfg.Limits.HardRate
		}
		for _, item := range cfg.Interfaces {
			if err := traffic.ApplyLimit(item, *rate); err != nil {
				return err
			}
		}
		return nil
	}
	return traffic.ApplyLimit(*iface, *rate)
}

func cmdUnlimit(args []string) error {
	fs := flag.NewFlagSet("unlimit", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	iface := fs.String("interface", "", "network interface")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *iface == "" {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			return err
		}
		return removeLimits(cfg)
	}
	return traffic.RemoveLimit(*iface)
}

func cmdTestNotify(args []string) error {
	cfgPath, _ := pathsFromFlags("test-notify", args)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	return (notify.Notifier{Config: cfg}).Send("FlowGuard test notification.")
}

func cmdCheckOnce(args []string) error {
	cfgPath, statePath := pathsFromFlags("check-once", args)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	return evaluateOnce(cfg, statePath, true)
}

func pathsFromFlags(name string, args []string) (string, string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	configPath := fs.String("config", config.DefaultConfigPath, "config path")
	statePath := fs.String("state", config.DefaultStatePath, "state path")
	_ = fs.Parse(args)
	return *configPath, *statePath
}

func evaluateOnce(cfg config.Config, statePath string, sendNotifications bool) error {
	usage, err := traffic.ReadUsage(cfg, time.Now())
	if err != nil {
		if sendNotifications {
			_ = (notify.Notifier{Config: cfg}).Send("FlowGuard: vnStat read failed: " + err.Error())
		}
		return err
	}
	state, err := config.LoadState(statePath, usage.Period)
	if err != nil {
		return err
	}
	if state.Period != usage.Period {
		_ = removeLimits(cfg)
		state = config.DefaultState(usage.Period)
		if sendNotifications {
			_ = (notify.Notifier{Config: cfg}).Send("FlowGuard: new period started, limit removed.")
		}
	}
	decision := traffic.DecideWithState(cfg, usage, state)
	if cfg.Safety.FirstLimitDryRun && decision.LimitRate != "" && !state.FirstLimitSeen {
		state.FirstLimitSeen = true
		state = traffic.UpdateState(state, usage, traffic.Decision{Level: traffic.LevelWarn})
		if sendNotifications {
			_ = (notify.Notifier{Config: cfg}).Send(formatDryRunNotification(cfg, usage, decision))
		}
		return config.SaveState(statePath, state)
	}
	if decision.LimitRate != "" && decision.LimitRate != state.CurrentLimitRate {
		if err := applyLimits(cfg, decision.LimitRate); err != nil {
			if sendNotifications {
				_ = (notify.Notifier{Config: cfg}).Send("FlowGuard: tc limit failed: " + err.Error())
			}
			return err
		}
	} else if decision.LimitRate == "" && state.Limited {
		if err := removeLimits(cfg); err != nil {
			return err
		}
	}
	if sendNotifications && shouldNotify(state, decision) {
		msg := formatNotification(cfg, usage, decision)
		if err := (notify.Notifier{Config: cfg}).Send(msg); err != nil {
			fmt.Fprintf(os.Stderr, "notify error: %v\n", err)
		} else {
			state.LastNotifiedLevel = decision.Level
		}
	}
	state = traffic.UpdateState(state, usage, decision)
	return config.SaveState(statePath, state)
}

func applyLimits(cfg config.Config, rate string) error {
	for _, iface := range cfg.Interfaces {
		if err := traffic.ApplyLimit(iface, rate); err != nil {
			return err
		}
	}
	return nil
}

func removeLimits(cfg config.Config) error {
	for _, iface := range cfg.Interfaces {
		if err := traffic.RemoveLimit(iface); err != nil {
			return err
		}
	}
	return nil
}

func formatDryRunNotification(cfg config.Config, usage traffic.Usage, decision traffic.Decision) string {
	return formatNotification(cfg, usage, decision) + "\nFirst-limit dry run: no tc limit applied this cycle. Next matching cycle will apply the limit."
}

func shouldNotify(state config.State, decision traffic.Decision) bool {
	return state.LastNotifiedLevel != decision.Level
}

func formatNotification(cfg config.Config, usage traffic.Usage, decision traffic.Decision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "FlowGuard: %s\n\n", decision.Level)
	fmt.Fprintf(&b, "Interface: %s\n", cfg.Interface)
	fmt.Fprintf(&b, "Allowance: %s\n", util.FormatBytes(cfg.AllowanceBytes))
	fmt.Fprintf(&b, "Billing mode: %s\n", cfg.BillingMode)
	fmt.Fprintf(&b, "Billable: %s / %s (%.2f%%)\n", util.FormatBytes(usage.BillableBytes), util.FormatBytes(cfg.AllowanceBytes), usage.Percent)
	if cfg.AllowanceBytes > usage.BillableBytes {
		fmt.Fprintf(&b, "Remaining: %s\n", util.FormatBytes(cfg.AllowanceBytes-usage.BillableBytes))
	} else {
		fmt.Fprintf(&b, "Remaining: 0 B\n")
	}
	if cfg.AllowanceBytes > 0 && usage.BillableBytes >= cfg.AllowanceBytes*90/100 {
		fmt.Fprintf(&b, "Risk: remaining allowance is below 10%%\n")
	}
	fmt.Fprintf(&b, "Total: %s\n", util.FormatBytes(usage.TotalBytes))
	fmt.Fprintf(&b, "Inbound: %s\n", util.FormatBytes(usage.RXBytes))
	fmt.Fprintf(&b, "Outbound: %s\n", util.FormatBytes(usage.TXBytes))
	if decision.LimitRate != "" {
		fmt.Fprintf(&b, "Limit: %s\n", decision.LimitRate)
	} else {
		fmt.Fprintf(&b, "Limit: none\n")
	}
	return b.String()
}
