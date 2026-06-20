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
	"flowguard/internal/upgrade"
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
	case "topup":
		err = cmdTopup(os.Args[2:])
	case "config-example":
		err = cmdConfigExample(os.Args[2:])
	case "rollback":
		err = cmdRollback(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "uninstall":
		err = cmdUninstall(os.Args[2:])
	case "upgrade":
		err = cmdUpgrade(os.Args[2:])
	case "limit":
		err = cmdLimit(os.Args[2:])
	case "unlimit":
		err = cmdUnlimit(os.Args[2:])
	case "test-notify":
		err = cmdTestNotify(os.Args[2:])
	case "check-once":
		err = cmdCheckOnce(os.Args[2:])
	case "help", "--help", "-h":
		err = cmdHelp(os.Args[2:])
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
			fmt.Printf("已重设近期基线：baseline_at=%s 今日基线=%s/%s 本周基线=%s/%s\n",
				cfg.BaselineAt,
				util.FormatBytes(cfg.BaselineDayRXBytes), util.FormatBytes(cfg.BaselineDayTXBytes),
				util.FormatBytes(cfg.BaselineWeekRXBytes), util.FormatBytes(cfg.BaselineWeekTXBytes),
			)
		} else {
			fmt.Printf("Recent baseline reset: baseline_at=%s today=%s/%s this_week=%s/%s\n",
				cfg.BaselineAt,
				util.FormatBytes(cfg.BaselineDayRXBytes), util.FormatBytes(cfg.BaselineDayTXBytes),
				util.FormatBytes(cfg.BaselineWeekRXBytes), util.FormatBytes(cfg.BaselineWeekTXBytes),
			)
		}
	}
	if backup, err := config.Backup(*cfgPath); err == nil && backup != "" {
		printBackup(cfg, backup)
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

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	statePath := fs.String("state", config.DefaultStatePath, "state path")
	ifaceValue := fs.String("interface", "", "interface, comma list, or auto-public for cleanup if config is unavailable")
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
	} else if *ifaceValue != "" {
		cfg = config.DefaultConfig()
		cfg.AllowanceBytes = 1
		interfaces, err := parseInterfaces(*ifaceValue)
		if err != nil {
			return err
		}
		cfg.Interfaces = interfaces
		if len(cfg.Interfaces) > 0 {
			cfg.Interface = cfg.Interfaces[0]
		}
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
		if err := removeManagedFile(*cfgPath, config.DefaultConfigPath); err != nil {
			return err
		}
		if err := removeManagedFile(*statePath, config.DefaultStatePath); err != nil {
			return err
		}
	}
	if !*keepBinary {
		_ = os.Remove("/usr/local/bin/flowguard")
	}
	if haveConfig {
		if isZH(cfg) {
			fmt.Println("已清理配置网卡上的 FlowGuard 限速。")
		} else {
			fmt.Println("FlowGuard limits removed for configured interfaces.")
		}
	}
	if haveConfig && isZH(cfg) {
		fmt.Println("FlowGuard 已卸载。")
	} else {
		fmt.Println("FlowGuard uninstalled.")
	}
	return nil
}

func printBackup(cfg config.Config, backup string) {
	if isZH(cfg) {
		fmt.Printf("备份：%s\n", backup)
		return
	}
	fmt.Printf("Backup: %s\n", backup)
}

func isZH(cfg config.Config) bool {
	return cfg.Language == "" || cfg.Language == "zh"
}

func removeManagedFile(path string, defaultPath string) error {
	if path == "" {
		path = defaultPath
	}
	cleanPath := filepath.Clean(path)
	if cleanPath == "/" || cleanPath == "." {
		return fmt.Errorf("refusing to remove unsafe path %q", path)
	}
	if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if cleanPath == filepath.Clean(defaultPath) {
		if err := os.Remove(filepath.Dir(cleanPath)); err != nil && !os.IsNotExist(err) && !strings.Contains(err.Error(), "directory not empty") {
			return err
		}
	}
	return nil
}

func cmdUpgrade(args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	opts := upgrade.Options{}
	fs.StringVar(&opts.Repo, "repo", upgrade.DefaultRepo, "GitHub repository owner/name")
	fs.StringVar(&opts.Version, "version", "latest", "release version tag or latest")
	fs.StringVar(&opts.BaseURL, "base-url", "", "release asset base URL override")
	fs.StringVar(&opts.InstallDir, "install-dir", upgrade.DefaultInstallDir, "install directory")
	fs.BoolVar(&opts.NoRestart, "no-restart", false, "do not restart flowguard service after upgrade")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print upgrade plan without downloading")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return upgrade.Run(opts)
}

func cmdHelp(args []string) error {
	fs := flag.NewFlagSet("help", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	language := fs.String("language", "", "help language: zh or en")
	if err := fs.Parse(args); err != nil {
		return err
	}
	lang := *language
	if lang == "" {
		if cfg, err := config.Load(*cfgPath); err == nil {
			lang = cfg.Language
		}
	}
	if lang == "" {
		lang = "zh"
	}
	fmt.Print(helpText(lang))
	return nil
}

type helpEntry struct {
	command string
	zh      string
	en      string
}

var helpEntries = []helpEntry{
	{"flowguard install", "安装依赖、生成配置并安装 systemd 服务", "Install dependencies, write config, and install systemd service"},
	{"flowguard status", "查看当前流量、决策和 tc 状态", "Show current traffic, decision, and tc state"},
	{"flowguard status --verbose", "显示原始 tc 等技术细节", "Show raw tc and other technical details"},
	{"flowguard status --json", "输出适合脚本读取的 JSON", "Print script-friendly JSON status"},
	{"flowguard doctor", "检查配置、vnStat、tc、网卡和服务", "Diagnose config, vnStat, tc, interfaces, and service"},
	{"flowguard modify --allowance 1000GB", "修改配置并自动备份", "Update config with automatic backup"},
	{"flowguard modify --language zh", "切换后续命令和通知输出语言", "Switch later command and notification output language"},
	{"flowguard modify --reset-recent-baseline", "重设今日/本周基线（升级到含日/周基线版本后跑一次）", "Recapture today/this-week baselines (run once after upgrading)"},
	{"flowguard topup 100GB", "购买额外流量后追加额度，并立即重新评估/解除限速", "Add purchased traffic allowance, then immediately recheck/unlimit"},
	{"flowguard topup 100", "同上；裸数字默认单位为 GB", "Same as above; bare numbers default to GB"},
	{"flowguard rollback", "回滚到最近一次配置备份", "Restore latest config backup"},
	{"flowguard upgrade", "下载、校验并升级到最新 Release", "Download, verify, and upgrade to the latest Release"},
	{"flowguard upgrade --version vX.Y.Z", "升级到指定版本", "Upgrade to a specific version"},
	{"flowguard test-notify", "发送 Telegram 测试通知", "Send a Telegram test notification"},
	{"flowguard check-once", "运行一次检查，适合调试/cron", "Run one monitor iteration for debugging/cron"},
	{"flowguard limit", "按配置对所有网卡应用强限速", "Apply the configured hard limit to all interfaces"},
	{"flowguard unlimit", "移除 FlowGuard 自己创建的限速", "Remove FlowGuard-managed limits"},
	{"flowguard uninstall", "卸载服务并删除配置、状态和二进制", "Remove service, config, state, and binary"},
	{"flowguard uninstall --keep-config=true --keep-binary=true", "卸载服务但保留配置、状态和二进制", "Remove service while keeping config, state, and binary"},
	{"flowguard uninstall --remove-vnstat=true", "卸载时同时移除配置网卡的 vnStat 数据", "Also remove configured interfaces from the vnStat database"},
	{"flowguard config-example", "输出示例配置 JSON", "Print example config JSON"},
}

func helpText(lang string) string {
	var b strings.Builder
	if lang == "en" {
		b.WriteString("FlowGuard commands:\n\n")
		for _, entry := range helpEntries {
			fmt.Fprintf(&b, "  %-55s %s\n", entry.command, entry.en)
		}
		b.WriteString("\nUse `flowguard help --language zh` for Chinese help.\n")
		return b.String()
	}
	b.WriteString("FlowGuard 命令：\n\n")
	for _, entry := range helpEntries {
		fmt.Fprintf(&b, "  %-55s %s\n", entry.command, entry.zh)
	}
	b.WriteString("\n使用 `flowguard help --language en` 查看英文帮助。\n")
	return b.String()
}

func usage() {
	fmt.Print(helpText("zh"))
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
		check("install baseline date", baselineDateCheck(cfg))
		check("vnstat reset detection", vnstatResetCheck(cfg))
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
		return fmt.Errorf("vnstat monthly total (RX=%s, TX=%s) is below install baseline (RX=%s, TX=%s); vnStat database may have been reset; consider `sudo flowguard modify --reset-recent-baseline` and review allowance accounting",
			util.FormatBytes(rawUsage.RXBytes), util.FormatBytes(rawUsage.TXBytes),
			util.FormatBytes(cfg.BaselineRXBytes), util.FormatBytes(cfg.BaselineTXBytes),
		)
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
	cfgPath, statePath, err := pathsFromFlags("run", args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	for {
		if latest, err := config.Load(cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "config reload error, keeping previous config: %v\n", err)
		} else {
			cfg = latest
		}
		if err := evaluateOnce(cfg, statePath, true); err != nil {
			fmt.Fprintf(os.Stderr, "loop error: %v\n", err)
		}
		interval := time.Duration(cfg.CheckIntervalSeconds) * time.Second
		if interval < 10*time.Second {
			interval = 10 * time.Second
		}
		time.Sleep(interval)
	}
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	statePath := fs.String("state", config.DefaultStatePath, "state path")
	jsonOutput := fs.Bool("json", false, "print JSON status")
	verbose := fs.Bool("verbose", false, "print technical details such as raw tc output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	return printStatus(cfg, *statePath, *jsonOutput, *verbose)
}

type statusReport struct {
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
	usage, err := traffic.ReadUsage(cfg, now)
	if err != nil {
		return err
	}
	state, err := config.LoadState(statePath, usage.Period)
	if err != nil {
		return err
	}
	decision := traffic.DecideWithState(cfg, usage, state)
	report := buildStatusReport(cfg, usage, state, decision)
	recent, recentErr := traffic.ReadRecentUsage(cfg, now)
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
	totalDays := int(periodEnd.Sub(periodStart).Hours()/24) + 1
	daysElapsed := int(today.Sub(periodStart).Hours()/24) + 1
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

func pickForecastWindow(now time.Time, recent traffic.RecentUsage) (uint64, int, string) {
	weekday := int(now.Weekday())
	weekDays := ((weekday + 6) % 7) + 1
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
		return applyLimits(cfg, *rate)
	}
	if !config.ValidInterfaceName(*iface) {
		return fmt.Errorf("invalid interface name %q", *iface)
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
	if !config.ValidInterfaceName(*iface) {
		return fmt.Errorf("invalid interface name %q", *iface)
	}
	return traffic.RemoveLimit(*iface)
}

func cmdTestNotify(args []string) error {
	cfgPath, _, err := pathsFromFlags("test-notify", args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	return (notify.Notifier{Config: cfg}).Send("FlowGuard test notification.")
}

func cmdCheckOnce(args []string) error {
	cfgPath, statePath, err := pathsFromFlags("check-once", args)
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	return evaluateOnce(cfg, statePath, true)
}

func pathsFromFlags(name string, args []string) (string, string, error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	configPath := fs.String("config", config.DefaultConfigPath, "config path")
	statePath := fs.String("state", config.DefaultStatePath, "state path")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	return *configPath, *statePath, nil
}

func evaluateOnce(cfg config.Config, statePath string, sendNotifications bool) error {
	now := time.Now()
	usage, err := traffic.ReadUsage(cfg, now)
	if err != nil {
		state, stateErr := config.LoadState(statePath, traffic.CurrentPeriod(now, cfg.PeriodDay))
		if stateErr == nil && sendErrorNotification(cfg, statePath, state, "vnstat", "FlowGuard: vnStat read failed: "+err.Error(), sendNotifications) == nil {
			return err
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
		state = traffic.UpdateState(state, usage, decision)
		state.Limited = false
		state.CurrentLimitRate = ""
		state.AppliedLimitKey = ""
		if sendNotifications {
			recent := loadRecentForNotification(cfg, now)
			_ = (notify.Notifier{Config: cfg}).Send(formatDryRunNotification(cfg, usage, decision, recent))
		}
		return config.SaveState(statePath, state)
	}
	desiredLimitKey, err := appliedLimitKey(cfg, decision.LimitRate)
	if err != nil {
		return err
	}
	if decision.LimitRate != "" && (!state.Limited || desiredLimitKey != state.AppliedLimitKey) {
		if err := applyLimits(cfg, decision.LimitRate); err != nil {
			if notifyErr := sendErrorNotification(cfg, statePath, state, "tc", "FlowGuard: tc limit failed: "+err.Error(), sendNotifications); notifyErr != nil {
				if sendNotifications {
					fmt.Fprintf(os.Stderr, "notify error: %v\n", notifyErr)
				}
			}
			return err
		}
	} else if decision.LimitRate == "" && state.Limited {
		if err := removeLimits(cfg); err != nil {
			return err
		}
		desiredLimitKey = ""
	}
	if sendNotifications && shouldNotify(state, decision) {
		recent := loadRecentForNotification(cfg, now)
		msg := formatNotification(cfg, usage, decision, recent)
		if isErrorCoolingDown(state, "notify", now) {
			// Preserve LastErrorAt so the retry window can expire.
		} else if err := (notify.Notifier{Config: cfg}).Send(msg); err != nil {
			fmt.Fprintf(os.Stderr, "notify error: %v\n", err)
			markErrorCooldown(&state, "notify", now)
		} else {
			state.LastNotifiedLevel = decision.Level
			state.LastNotifiedKey = notificationKey(decision)
			clearErrorCooldown(&state)
		}
	}
	state = traffic.UpdateState(state, usage, decision)
	state.AppliedLimitKey = desiredLimitKey
	return config.SaveState(statePath, state)
}

const errorNotificationCooldown = 30 * time.Minute

func sendErrorNotification(cfg config.Config, statePath string, state config.State, key string, message string, sendNotifications bool) error {
	if !sendNotifications {
		return nil
	}
	now := time.Now()
	if isErrorCoolingDown(state, key, now) {
		return nil
	}
	markErrorCooldown(&state, key, now)
	if err := (notify.Notifier{Config: cfg}).Send(message); err != nil {
		_ = config.SaveState(statePath, state)
		return err
	}
	return config.SaveState(statePath, state)
}

func isErrorCoolingDown(state config.State, key string, now time.Time) bool {
	if state.LastErrorKey != key {
		return false
	}
	last, err := time.Parse(time.RFC3339, state.LastErrorAt)
	return err == nil && now.Sub(last) < errorNotificationCooldown
}

func markErrorCooldown(state *config.State, key string, now time.Time) {
	state.LastErrorKey = key
	state.LastErrorAt = now.Format(time.RFC3339)
}

func clearErrorCooldown(state *config.State) {
	state.LastErrorKey = ""
	state.LastErrorAt = ""
}

func applyLimits(cfg config.Config, rate string) error {
	limitRate, err := traffic.SplitRate(rate, len(cfg.Interfaces))
	if err != nil {
		return err
	}
	var applied []string
	for _, iface := range cfg.Interfaces {
		if err := traffic.ApplyLimit(iface, limitRate); err != nil {
			for _, appliedIface := range applied {
				_ = traffic.RemoveLimit(appliedIface)
			}
			return err
		}
		applied = append(applied, iface)
	}
	return nil
}

func appliedLimitKey(cfg config.Config, rate string) (string, error) {
	if rate == "" {
		return "", nil
	}
	limitRate, err := traffic.SplitRate(rate, len(cfg.Interfaces))
	if err != nil {
		return "", err
	}
	return strings.Join(cfg.Interfaces, ",") + "=" + limitRate, nil
}

func removeLimits(cfg config.Config) error {
	for _, iface := range cfg.Interfaces {
		if err := traffic.RemoveLimit(iface); err != nil {
			return err
		}
	}
	return nil
}

// loadRecentForNotification fetches recent usage best-effort. Errors are
// swallowed so notifications never fail just because vnStat hasn't built up
// daily history yet; the formatter handles a zero-value RecentUsage.
func loadRecentForNotification(cfg config.Config, now time.Time) traffic.RecentUsage {
	recent, err := traffic.ReadRecentUsage(cfg, now)
	if err != nil {
		return traffic.RecentUsage{}
	}
	return recent
}

func formatDryRunNotification(cfg config.Config, usage traffic.Usage, decision traffic.Decision, recent traffic.RecentUsage) string {
	if isZH(cfg) {
		return formatNotification(cfg, usage, decision, recent) + "\n首次限速保护：本轮只通知，不执行 tc 限速。下次仍满足条件时会实际限速。"
	}
	return formatNotification(cfg, usage, decision, recent) + "\nFirst-limit dry run: no tc limit applied this cycle. Next matching cycle will apply the limit."
}

func shouldNotify(state config.State, decision traffic.Decision) bool {
	key := notificationKey(decision)
	if state.LastNotifiedKey != "" {
		return state.LastNotifiedKey != key
	}
	return state.LastNotifiedLevel != decision.Level
}

func notificationKey(decision traffic.Decision) string {
	if decision.LimitRate == "" {
		return decision.Level
	}
	return decision.Level + ":" + decision.LimitRate
}

func formatNotification(cfg config.Config, usage traffic.Usage, decision traffic.Decision, recent traffic.RecentUsage) string {
	var b strings.Builder
	if isZH(cfg) {
		fmt.Fprintf(&b, "FlowGuard：%s\n\n", decision.Level)
		fmt.Fprintf(&b, "网卡：%s\n", cfg.Interface)
		fmt.Fprintf(&b, "流量额度：%s\n", util.FormatBytes(cfg.AllowanceBytes))
		fmt.Fprintf(&b, "计费模式：%s\n", cfg.BillingMode)
		fmt.Fprintf(&b, "计费用量：%s / %s (%.2f%%)\n", util.FormatBytes(usage.BillableBytes), util.FormatBytes(cfg.AllowanceBytes), usage.Percent)
		if cfg.AllowanceBytes > usage.BillableBytes {
			fmt.Fprintf(&b, "剩余额度：%s\n", util.FormatBytes(cfg.AllowanceBytes-usage.BillableBytes))
		} else {
			fmt.Fprintf(&b, "剩余额度：0 B\n")
		}
		if cfg.AllowanceBytes > 0 && usage.BillableBytes >= cfg.AllowanceBytes*90/100 {
			fmt.Fprintf(&b, "风险：剩余额度低于 10%%\n")
		}
		fmt.Fprintf(&b, "总流量：%s\n", util.FormatBytes(usage.TotalBytes))
		fmt.Fprintf(&b, "入站：%s\n", util.FormatBytes(usage.RXBytes))
		fmt.Fprintf(&b, "出站：%s\n", util.FormatBytes(usage.TXBytes))
		if recent.ThisWeek.BillableBytes > 0 || recent.Today.BillableBytes > 0 {
			fmt.Fprintf(&b, "今日新增：%s\n", util.FormatBytes(recent.Today.BillableBytes))
			fmt.Fprintf(&b, "本周累计：%s\n", util.FormatBytes(recent.ThisWeek.BillableBytes))
		}
		if decision.LimitRate != "" {
			fmt.Fprintf(&b, "限速：%s\n", decision.LimitRate)
		} else {
			fmt.Fprintf(&b, "限速：无\n")
		}
		return b.String()
	}
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
	if recent.ThisWeek.BillableBytes > 0 || recent.Today.BillableBytes > 0 {
		fmt.Fprintf(&b, "Today: %s\n", util.FormatBytes(recent.Today.BillableBytes))
		fmt.Fprintf(&b, "This week: %s\n", util.FormatBytes(recent.ThisWeek.BillableBytes))
	}
	if decision.LimitRate != "" {
		fmt.Fprintf(&b, "Limit: %s\n", decision.LimitRate)
	} else {
		fmt.Fprintf(&b, "Limit: none\n")
	}
	return b.String()
}
