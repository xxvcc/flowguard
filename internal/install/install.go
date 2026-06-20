package install

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/notify"
	"flowguard/internal/service"
	"flowguard/internal/traffic"
	"flowguard/internal/util"
)

type Options struct {
	Yes              bool
	ConfigPath       string
	StatePath        string
	Allowance        string
	InitialTotal     string
	InitialRX        string
	InitialTX        string
	Interface        string
	TGToken          string
	TGChatID         string
	SoftRate         string
	HardRate         string
	CheckSeconds     int
	PeriodDay        int
	BillingMode      string
	FirstLimitDryRun *bool
	Language         string
}

type OSInfo struct {
	ID      string
	IDLike  string
	Name    string
	Version string
}

func Run(opts Options) error {
	if !util.IsRoot() {
		return fmt.Errorf("install must be run as root, try: sudo flowguard install")
	}
	osInfo := detectOS()
	fmt.Printf("Detected OS: %s %s (%s)\n", osInfo.Name, osInfo.Version, osInfo.ID)

	missing := missingDependencies()
	if len(missing) > 0 {
		fmt.Printf("Missing dependencies: %s\n", strings.Join(missing, ", "))
		if opts.Yes || askInstallDeps() {
			if err := installDependencies(osInfo); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("dependencies are required: %s", strings.Join(missing, ", "))
		}
	}
	missing = missingDependencies()
	if len(missing) > 0 {
		return fmt.Errorf("dependencies still missing after install: %s", strings.Join(missing, ", "))
	}

	iface := opts.Interface
	var interfaces []string
	if iface == "auto-public" {
		detected, err := util.DetectPublicInterfaces()
		if err != nil {
			return err
		}
		interfaces = detected
		iface = interfaces[0]
	} else if strings.Contains(iface, ",") {
		for _, item := range strings.Split(iface, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				if !config.ValidInterfaceName(item) {
					return fmt.Errorf("invalid interface name %q", item)
				}
				interfaces = append(interfaces, item)
			}
		}
		if len(interfaces) > 0 {
			iface = interfaces[0]
		}
	}
	if iface == "" {
		detected, err := util.DetectDefaultInterface()
		if err != nil {
			return err
		}
		iface = detected
	}
	if !config.ValidInterfaceName(iface) {
		return fmt.Errorf("invalid interface name %q", iface)
	}
	if len(interfaces) == 0 {
		interfaces = []string{iface}
	}
	fmt.Printf("Using interface: %s\n", iface)

	for _, setupIface := range interfaces {
		if err := setupVnStat(setupIface); err != nil {
			fmt.Printf("Warning: vnStat setup returned for %s: %v\n", setupIface, err)
		}
	}

	cfg, lang, err := buildConfig(opts, iface)
	if err != nil {
		return err
	}
	cfg.Interfaces = interfaces
	if opts.ConfigPath == "" {
		opts.ConfigPath = config.DefaultConfigPath
	}
	if opts.StatePath == "" {
		opts.StatePath = config.DefaultStatePath
	}
	if err := setInstallBaseline(&cfg); err != nil {
		if !isVnStatWarmupError(err) {
			return err
		}
		fmt.Printf("Warning: %v; using zero install baseline. Run `flowguard status` after vnStat has collected data.\n", err)
	}
	if backup, err := config.Backup(opts.ConfigPath); err == nil && backup != "" {
		fmt.Printf("Backup: %s\n", backup)
	} else if err != nil {
		return err
	}
	if err := config.Save(opts.ConfigPath, cfg); err != nil {
		return err
	}
	if _, err := os.Stat(opts.StatePath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		period := traffic.CurrentPeriod(time.Now(), cfg.PeriodDay)
		if err := config.SaveState(opts.StatePath, config.DefaultState(period)); err != nil {
			return err
		}
	}
	fmt.Printf("Wrote config: %s\n", opts.ConfigPath)

	if err := installBinary(); err != nil {
		return err
	}
	if util.CommandExists("systemctl") {
		if err := service.InstallSystemd("/usr/local/bin/flowguard", opts.ConfigPath, opts.StatePath); err != nil {
			return err
		}
		fmt.Println("Installed and started systemd service: flowguard")
	} else {
		fmt.Println("systemctl not found; run manually with: flowguard run")
	}

	if cfg.Telegram.Enabled {
		n := notify.Notifier{Config: cfg}
		if err := n.Send("FlowGuard installed and notification test succeeded."); err != nil {
			fmt.Printf("Warning: Telegram test failed: %v\n", err)
		} else {
			fmt.Println("Telegram test notification sent.")
		}
	}
	printInstallSummary(cfg, lang)
	return nil
}

func printInstallSummary(cfg config.Config, lang string) {
	if lang == "en" {
		fmt.Println("\nFlowGuard summary:")
		fmt.Printf("  Interface: %s\n", cfg.Interface)
		fmt.Printf("  Allowance: %s\n", util.FormatBytes(cfg.AllowanceBytes))
		fmt.Printf("  Billing mode: %s\n", cfg.BillingMode)
		fmt.Printf("  Billing period day: %d\n", cfg.PeriodDay)
		fmt.Printf("  Thresholds: warn %.1f%%, soft %.1f%%, hard %.1f%%\n", cfg.Thresholds.WarnPercent, cfg.Thresholds.SoftPercent, cfg.Thresholds.HardPercent)
		fmt.Printf("  Limits: soft %s, hard %s\n", cfg.Limits.SoftRate, cfg.Limits.HardRate)
		if cfg.Telegram.Enabled {
			fmt.Println("  Notify: telegram")
		} else {
			fmt.Println("  Notify: none")
		}
		fmt.Printf("  First-limit dry run: %v\n", cfg.Safety.FirstLimitDryRun)
		return
	}
	fmt.Println("\nFlowGuard 配置摘要：")
	fmt.Printf("  网卡：%s\n", cfg.Interface)
	fmt.Printf("  流量额度：%s\n", util.FormatBytes(cfg.AllowanceBytes))
	fmt.Printf("  计费模式：%s\n", cfg.BillingMode)
	fmt.Printf("  账期起始日：%d\n", cfg.PeriodDay)
	fmt.Printf("  触发阈值：提醒 %.1f%%，轻度限速 %.1f%%，强限速 %.1f%%\n", cfg.Thresholds.WarnPercent, cfg.Thresholds.SoftPercent, cfg.Thresholds.HardPercent)
	fmt.Printf("  限速值：轻度 %s，强限速 %s\n", cfg.Limits.SoftRate, cfg.Limits.HardRate)
	if cfg.Telegram.Enabled {
		fmt.Println("  通知：Telegram")
	} else {
		fmt.Println("  通知：未启用")
	}
	fmt.Printf("  首次限速保护：%v\n", cfg.Safety.FirstLimitDryRun)
}

func labels(lang string) map[string]string {
	zh := map[string]string{
		"allowance":      "月流量额度（默认单位 GB，例：1000 或 1000GB）",
		"period_day":     "账期起始日",
		"billing_mode":   "计费流量模式",
		"initial_mode":   "本月已用流量录入方式",
		"initial_total":  "本月已用总流量（默认单位 GB）",
		"initial_rx":     "本月已用入站流量（默认单位 GB）",
		"initial_tx":     "本月已用出站流量（默认单位 GB）",
		"soft_rate":      "轻度限速值（默认单位 mbit）",
		"hard_rate":      "强限速值（默认单位 mbit）",
		"check_interval": "检查间隔秒数",
		"enable_tg":      "启用 Telegram 通知",
		"tg_token":       "Telegram Bot Token",
		"tg_chat":        "Telegram Chat ID",
		"dry_run":        "启用首次限速保护",
	}
	if lang == "en" {
		return map[string]string{
			"allowance":      "Monthly allowance (default unit GB, e.g. 1000 or 1000GB)",
			"period_day":     "Billing period start day",
			"billing_mode":   "Billing traffic mode",
			"initial_mode":   "Initial usage input mode",
			"initial_total":  "Initial total used this period (default unit GB)",
			"initial_rx":     "Initial inbound used this period (default unit GB)",
			"initial_tx":     "Initial outbound used this period (default unit GB)",
			"soft_rate":      "Soft limit rate (default unit mbit)",
			"hard_rate":      "Hard limit rate (default unit mbit)",
			"check_interval": "Check interval seconds",
			"enable_tg":      "Enable Telegram notifications",
			"tg_token":       "Telegram bot token",
			"tg_chat":        "Telegram chat ID",
			"dry_run":        "Enable first-limit dry run protection",
		}
	}
	return zh
}

func buildConfig(opts Options, iface string) (config.Config, string, error) {
	cfg := config.DefaultConfig()
	cfg.Interface = iface
	lang := opts.Language
	if lang == "" {
		lang = "zh"
	}
	cfg.Language = lang
	if opts.PeriodDay != 0 {
		cfg.PeriodDay = opts.PeriodDay
	}
	if opts.BillingMode != "" {
		cfg.BillingMode = opts.BillingMode
	}
	if opts.FirstLimitDryRun != nil {
		cfg.Safety.FirstLimitDryRun = *opts.FirstLimitDryRun
	}
	cfg.InitialPeriod = traffic.CurrentPeriod(time.Now(), cfg.PeriodDay)
	cfg.CheckIntervalSeconds = opts.CheckSeconds
	if cfg.CheckIntervalSeconds == 0 {
		cfg.CheckIntervalSeconds = 60
	}
	cfg.Limits.SoftRate = firstNonEmpty(opts.SoftRate, cfg.Limits.SoftRate)
	cfg.Limits.HardRate = firstNonEmpty(opts.HardRate, cfg.Limits.HardRate)

	if opts.Yes {
		if opts.Allowance == "" {
			return cfg, lang, fmt.Errorf("--allowance is required when --yes is used")
		}
		cfg, err := applyOptionValues(cfg, opts)
		return cfg, lang, err
	}

	scanner, closeInput := newPromptScanner()
	defer closeInput()
	lang = opts.Language
	if lang == "" {
		lang = util.PromptChoice(scanner, "Language / 语言", "zh", []string{"zh", "en"})
	}
	cfg.Language = lang
	tr := labels(lang)
	allowance := opts.Allowance
	if allowance == "" {
		allowance = withDefaultUnit(util.Prompt(scanner, tr["allowance"], "1000GB"), "GB")
	}
	periodDay := cfg.PeriodDay
	if opts.PeriodDay == 0 {
		periodDayText := util.Prompt(scanner, tr["period_day"], strconv.Itoa(cfg.PeriodDay))
		parsedPeriodDay, err := strconv.Atoi(periodDayText)
		if err != nil {
			return cfg, lang, fmt.Errorf("invalid billing period start day: %w", err)
		}
		periodDay = parsedPeriodDay
	}
	cfg.PeriodDay = periodDay
	cfg.InitialPeriod = traffic.CurrentPeriod(time.Now(), cfg.PeriodDay)
	if opts.BillingMode == "" {
		cfg.BillingMode = promptValueChoice(scanner, tr["billing_mode"], cfg.BillingMode, billingModeChoices(lang))
	}
	initialTotal := opts.InitialTotal
	initialRX := opts.InitialRX
	initialTX := opts.InitialTX
	if initialTotal == "" && initialRX == "" && initialTX == "" {
		mode := promptValueChoice(scanner, tr["initial_mode"], "none", initialModeChoices(lang))
		switch mode {
		case "total":
			initialTotal = withDefaultUnit(util.Prompt(scanner, tr["initial_total"], "0"), "GB")
		case "split":
			initialRX = withDefaultUnit(util.Prompt(scanner, tr["initial_rx"], "0"), "GB")
			initialTX = withDefaultUnit(util.Prompt(scanner, tr["initial_tx"], "0"), "GB")
		}
	}
	if initialTotal == "" {
		if initialRX == "" {
			initialRX = withDefaultUnit(util.Prompt(scanner, tr["initial_rx"], "0"), "GB")
		}
		if initialTX == "" {
			initialTX = withDefaultUnit(util.Prompt(scanner, tr["initial_tx"], "0"), "GB")
		}
	}
	if opts.SoftRate == "" {
		cfg.Limits.SoftRate = withDefaultUnit(util.Prompt(scanner, tr["soft_rate"], cfg.Limits.SoftRate), "mbit")
	}
	if opts.HardRate == "" {
		cfg.Limits.HardRate = withDefaultUnit(util.Prompt(scanner, tr["hard_rate"], cfg.Limits.HardRate), "mbit")
	}
	check := util.Prompt(scanner, tr["check_interval"], strconv.Itoa(cfg.CheckIntervalSeconds))
	checkSeconds, err := strconv.Atoi(check)
	if err != nil {
		return cfg, lang, fmt.Errorf("invalid check interval: %w", err)
	}
	cfg.CheckIntervalSeconds = checkSeconds
	cfg.Telegram.Enabled = promptBoolLocalized(scanner, tr["enable_tg"], opts.TGToken != "" || opts.TGChatID != "", lang)
	if cfg.Telegram.Enabled {
		cfg.Telegram.BotToken = opts.TGToken
		if cfg.Telegram.BotToken == "" {
			cfg.Telegram.BotToken = util.Prompt(scanner, tr["tg_token"], "")
		}
		cfg.Telegram.ChatID = opts.TGChatID
		if cfg.Telegram.ChatID == "" {
			cfg.Telegram.ChatID = util.Prompt(scanner, tr["tg_chat"], "")
		}
	}
	if opts.FirstLimitDryRun == nil {
		cfg.Safety.FirstLimitDryRun = promptBoolLocalized(scanner, tr["dry_run"], cfg.Safety.FirstLimitDryRun, lang)
	}
	opts.Allowance = allowance
	opts.InitialTotal = initialTotal
	opts.InitialRX = initialRX
	opts.InitialTX = initialTX
	cfg, err = applyOptionValues(cfg, opts)
	return cfg, lang, err
}

func newPromptScanner() (*bufio.Scanner, func()) {
	if tty, err := os.Open("/dev/tty"); err == nil {
		return bufio.NewScanner(tty), func() { _ = tty.Close() }
	}
	return bufio.NewScanner(os.Stdin), func() {}
}

type valueChoice struct {
	value string
	label string
}

func promptValueChoice(scanner *bufio.Scanner, label string, defaultValue string, choices []valueChoice) string {
	labels := make([]string, 0, len(choices))
	labelToValue := map[string]string{}
	defaultLabel := ""
	for _, choice := range choices {
		labels = append(labels, choice.label)
		labelToValue[choice.label] = choice.value
		if choice.value == defaultValue {
			defaultLabel = choice.label
		}
	}
	if defaultLabel == "" && len(choices) > 0 {
		defaultLabel = choices[0].label
	}
	selected := util.PromptChoice(scanner, label, defaultLabel, labels)
	if value, ok := labelToValue[selected]; ok {
		return value
	}
	return defaultValue
}

func promptBoolLocalized(scanner *bufio.Scanner, label string, defaultValue bool, lang string) bool {
	if lang == "en" {
		return util.PromptBool(scanner, label, defaultValue)
	}
	defaultLabel := "否"
	if defaultValue {
		defaultLabel = "是"
	}
	return util.PromptChoice(scanner, label, defaultLabel, []string{"是", "否"}) == "是"
}

func billingModeChoices(lang string) []valueChoice {
	if lang == "en" {
		return []valueChoice{{value: "total", label: "total"}, {value: "outbound", label: "outbound"}}
	}
	return []valueChoice{{value: "total", label: "total（入站 + 出站）"}, {value: "outbound", label: "outbound（仅出站）"}}
}

func initialModeChoices(lang string) []valueChoice {
	if lang == "en" {
		return []valueChoice{{value: "none", label: "none"}, {value: "total", label: "total"}, {value: "split", label: "split"}}
	}
	return []valueChoice{{value: "none", label: "不录入"}, {value: "total", label: "录入总量"}, {value: "split", label: "入站/出站分开录入"}}
}

func withDefaultUnit(value string, unit string) string {
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

func applyOptionValues(cfg config.Config, opts Options) (config.Config, error) {
	allowance, err := util.ParseBytes(opts.Allowance)
	if err != nil {
		return cfg, err
	}
	cfg.AllowanceBytes = allowance
	applyTelegramOptions(&cfg, opts)
	if opts.InitialTotal != "" && (opts.InitialRX != "" || opts.InitialTX != "") {
		return cfg, fmt.Errorf("use either --initial-total or --initial-rx/--initial-tx, not both")
	}
	if opts.InitialTotal != "" {
		total, err := util.ParseBytes(opts.InitialTotal)
		if err != nil {
			return cfg, err
		}
		if cfg.BillingMode == "outbound" {
			cfg.InitialRXBytes = 0
			cfg.InitialTXBytes = total
		} else {
			cfg.InitialRXBytes = total
			cfg.InitialTXBytes = 0
		}
		return cfg, cfg.Validate()
	}
	rx, err := util.ParseBytes(opts.InitialRX)
	if err != nil {
		return cfg, err
	}
	tx, err := util.ParseBytes(opts.InitialTX)
	if err != nil {
		return cfg, err
	}
	cfg.InitialRXBytes = rx
	cfg.InitialTXBytes = tx
	return cfg, cfg.Validate()
}

func applyTelegramOptions(cfg *config.Config, opts Options) {
	if opts.TGToken != "" || opts.TGChatID != "" {
		cfg.Telegram.Enabled = true
		cfg.Telegram.BotToken = opts.TGToken
		cfg.Telegram.ChatID = opts.TGChatID
	}
}

func setInstallBaseline(cfg *config.Config) error {
	baselineConfig := *cfg
	baselineConfig.InitialRXBytes = 0
	baselineConfig.InitialTXBytes = 0
	baselineConfig.BaselineRXBytes = 0
	baselineConfig.BaselineTXBytes = 0
	baselineConfig.BaselineAt = ""
	baselineConfig.BaselineDayRXBytes = 0
	baselineConfig.BaselineDayTXBytes = 0
	baselineConfig.BaselineWeekRXBytes = 0
	baselineConfig.BaselineWeekTXBytes = 0
	now := time.Now()
	var lastErr error
	for attempt := 1; attempt <= 6; attempt++ {
		usage, err := traffic.ReadUsage(baselineConfig, now)
		if err == nil {
			cfg.BaselineRXBytes = usage.RXBytes
			cfg.BaselineTXBytes = usage.TXBytes
			cfg.BaselineAt = now.Format("2006-01-02")
			if recent, recentErr := traffic.ReadRawRecentUsage(baselineConfig, now); recentErr == nil {
				cfg.BaselineDayRXBytes = recent.Today.RXBytes
				cfg.BaselineDayTXBytes = recent.Today.TXBytes
				cfg.BaselineWeekRXBytes = recent.ThisWeek.RXBytes
				cfg.BaselineWeekTXBytes = recent.ThisWeek.TXBytes
			}
			return nil
		}
		lastErr = err
		if attempt < 6 {
			fmt.Printf("Waiting for vnStat baseline data (%d/6): %v\n", attempt, err)
			time.Sleep(10 * time.Second)
		}
	}
	return fmt.Errorf("read install baseline: %w", lastErr)
}

func isVnStatWarmupError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "monthly data") && strings.Contains(message, "is missing") || strings.Contains(message, "daily data is empty")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func installBinary() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	const installDir = "/usr/local/bin"
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(installDir, "flowguard.tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, installDir+"/flowguard"); err != nil {
		return err
	}
	_ = syncDir(installDir)
	fmt.Println("Installed binary: /usr/local/bin/flowguard")
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func askInstallDeps() bool {
	scanner, closeInput := newPromptScanner()
	defer closeInput()
	return util.PromptBool(scanner, "自动安装缺失依赖 / Install missing dependencies automatically", true)
}

func missingDependencies() []string {
	var missing []string
	for _, command := range []string{"vnstat", "tc", "ip"} {
		if !util.CommandExists(command) {
			missing = append(missing, command)
		}
	}
	return missing
}

func detectOS() OSInfo {
	info := OSInfo{ID: "unknown", Name: "unknown"}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return info
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, "\"")
		switch key {
		case "ID":
			info.ID = value
		case "ID_LIKE":
			info.IDLike = value
		case "NAME":
			info.Name = value
		case "VERSION_ID":
			info.Version = value
		}
	}
	return info
}

func installDependencies(info OSInfo) error {
	id := strings.ToLower(info.ID + " " + info.IDLike)
	switch {
	case strings.Contains(id, "debian") || strings.Contains(id, "ubuntu"):
		if _, err := util.Run(10*time.Minute, "apt-get", "update"); err != nil {
			return err
		}
		_, err := util.Run(10*time.Minute, "apt-get", "install", "-y", "vnstat", "iproute2")
		return err
	case strings.Contains(id, "alpine"):
		_, err := util.Run(10*time.Minute, "apk", "add", "--no-cache", "vnstat", "iproute2")
		return err
	case strings.Contains(id, "suse") || strings.Contains(id, "opensuse"):
		_, err := util.Run(10*time.Minute, "zypper", "--non-interactive", "install", "vnstat", "iproute2")
		return err
	case strings.Contains(id, "rhel") || strings.Contains(id, "fedora") || strings.Contains(id, "centos") || strings.Contains(id, "rocky") || strings.Contains(id, "almalinux") || strings.Contains(id, "amzn"):
		pm := "dnf"
		if !util.CommandExists(pm) {
			pm = "yum"
		}
		_, err := util.Run(10*time.Minute, pm, "install", "-y", "vnstat", "iproute")
		if err == nil {
			return nil
		}
		if strings.Contains(id, "centos") || strings.Contains(id, "rhel") {
			_, _ = util.Run(10*time.Minute, pm, "install", "-y", "epel-release")
			_, err = util.Run(10*time.Minute, pm, "install", "-y", "vnstat", "iproute")
		}
		return err
	default:
		return fmt.Errorf("unsupported OS for automatic dependency install; please install vnstat and iproute/tc manually")
	}
}

func setupVnStat(iface string) error {
	if util.CommandExists("systemctl") {
		_, _ = util.Run(30*time.Second, "systemctl", "enable", "--now", "vnstat")
	}
	if _, err := util.Run(30*time.Second, "vnstat", "--add", "-i", iface); err != nil {
		if !strings.Contains(err.Error(), "already") && !strings.Contains(err.Error(), "exists") && !vnStatHasInterface(iface) {
			return err
		}
	}
	if util.CommandExists("systemctl") {
		_, _ = util.Run(30*time.Second, "systemctl", "restart", "vnstat")
	} else if util.CommandExists("rc-service") {
		_, _ = util.Run(30*time.Second, "rc-service", "vnstat", "start")
	}
	return nil
}

func vnStatHasInterface(iface string) bool {
	result, err := util.Run(30*time.Second, "vnstat", "--json")
	return err == nil && strings.Contains(result.Stdout, `"name":"`+iface+`"`)
}
