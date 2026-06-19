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
	if len(interfaces) == 0 {
		interfaces = []string{iface}
	}
	fmt.Printf("Using interface: %s\n", iface)

	for _, setupIface := range interfaces {
		if err := setupVnStat(setupIface); err != nil {
			fmt.Printf("Warning: vnStat setup returned for %s: %v\n", setupIface, err)
		}
	}

	cfg, err := buildConfig(opts, iface)
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
	printInstallSummary(cfg)
	return nil
}

func printInstallSummary(cfg config.Config) {
	fmt.Println("\nFlowGuard summary:")
	fmt.Printf("  Interface: %s\n", cfg.Interface)
	fmt.Printf("  Allowance: %s\n", util.FormatBytes(cfg.AllowanceBytes))
	fmt.Printf("  Billing mode: %s\n", cfg.BillingMode)
	fmt.Printf("  Billing period day: %d\n", cfg.PeriodDay)
	fmt.Printf("  Thresholds: warn %.1f%%, soft %.1f%%, hard %.1f%%\n", cfg.Thresholds.WarnPercent, cfg.Thresholds.SoftPercent, cfg.Thresholds.HardPercent)
	fmt.Printf("  Clear thresholds: warn %.1f%%, soft %.1f%%, hard %.1f%%\n", cfg.Thresholds.WarnClearPercent, cfg.Thresholds.SoftClearPercent, cfg.Thresholds.HardClearPercent)
	fmt.Printf("  Limits: soft %s, hard %s\n", cfg.Limits.SoftRate, cfg.Limits.HardRate)
	if cfg.Telegram.Enabled {
		fmt.Println("  Notify: telegram")
	} else {
		fmt.Println("  Notify: none")
	}
	fmt.Printf("  First-limit dry run: %v\n", cfg.Safety.FirstLimitDryRun)
}

func labels(lang string) map[string]string {
	zh := map[string]string{
		"allowance":      "月流量额度",
		"period_day":     "账期起始日",
		"billing_mode":   "计费流量模式",
		"initial_mode":   "本月已用流量录入方式",
		"initial_total":  "本月已用总流量",
		"initial_rx":     "本月已用入站流量",
		"initial_tx":     "本月已用出站流量",
		"soft_rate":      "轻度限速值",
		"hard_rate":      "强限速值",
		"check_interval": "检查间隔秒数",
		"enable_tg":      "启用 Telegram 通知",
		"tg_token":       "Telegram Bot Token",
		"tg_chat":        "Telegram Chat ID",
		"dry_run":        "启用首次限速保护",
	}
	if lang == "en" {
		return map[string]string{
			"allowance":      "Monthly allowance",
			"period_day":     "Billing period start day",
			"billing_mode":   "Billing traffic mode",
			"initial_mode":   "Initial usage input mode",
			"initial_total":  "Initial total used this period",
			"initial_rx":     "Initial inbound used this period",
			"initial_tx":     "Initial outbound used this period",
			"soft_rate":      "Soft limit rate",
			"hard_rate":      "Hard limit rate",
			"check_interval": "Check interval seconds",
			"enable_tg":      "Enable Telegram notifications",
			"tg_token":       "Telegram bot token",
			"tg_chat":        "Telegram chat ID",
			"dry_run":        "Enable first-limit dry run protection",
		}
	}
	return zh
}

func buildConfig(opts Options, iface string) (config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.Interface = iface
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
			return cfg, fmt.Errorf("--allowance is required when --yes is used")
		}
		return applyOptionValues(cfg, opts)
	}

	scanner := bufio.NewScanner(os.Stdin)
	lang := opts.Language
	if lang == "" {
		lang = util.PromptChoice(scanner, "Language / 语言", "zh", []string{"zh", "en"})
	}
	tr := labels(lang)
	allowance := opts.Allowance
	if allowance == "" {
		allowance = util.Prompt(scanner, tr["allowance"], "1000GB")
	}
	periodDay := cfg.PeriodDay
	if opts.PeriodDay == 0 {
		periodDayText := util.Prompt(scanner, tr["period_day"], strconv.Itoa(cfg.PeriodDay))
		parsedPeriodDay, err := strconv.Atoi(periodDayText)
		if err != nil {
			return cfg, fmt.Errorf("invalid billing period start day: %w", err)
		}
		periodDay = parsedPeriodDay
	}
	cfg.PeriodDay = periodDay
	cfg.InitialPeriod = traffic.CurrentPeriod(time.Now(), cfg.PeriodDay)
	if opts.BillingMode == "" {
		cfg.BillingMode = util.PromptChoice(scanner, tr["billing_mode"], cfg.BillingMode, []string{"total", "outbound"})
	}
	initialTotal := opts.InitialTotal
	initialRX := opts.InitialRX
	initialTX := opts.InitialTX
	if initialTotal == "" && initialRX == "" && initialTX == "" {
		mode := util.PromptChoice(scanner, tr["initial_mode"], "none", []string{"none", "total", "split"})
		switch mode {
		case "total":
			initialTotal = util.Prompt(scanner, tr["initial_total"], "0")
		case "split":
			initialRX = util.Prompt(scanner, tr["initial_rx"], "0")
			initialTX = util.Prompt(scanner, tr["initial_tx"], "0")
		}
	}
	if initialTotal == "" {
		if initialRX == "" {
			initialRX = util.Prompt(scanner, tr["initial_rx"], "0")
		}
		if initialTX == "" {
			initialTX = util.Prompt(scanner, tr["initial_tx"], "0")
		}
	}
	if opts.SoftRate == "" {
		cfg.Limits.SoftRate = util.Prompt(scanner, tr["soft_rate"], cfg.Limits.SoftRate)
	}
	if opts.HardRate == "" {
		cfg.Limits.HardRate = util.Prompt(scanner, tr["hard_rate"], cfg.Limits.HardRate)
	}
	check := util.Prompt(scanner, tr["check_interval"], strconv.Itoa(cfg.CheckIntervalSeconds))
	checkSeconds, err := strconv.Atoi(check)
	if err != nil {
		return cfg, fmt.Errorf("invalid check interval: %w", err)
	}
	cfg.CheckIntervalSeconds = checkSeconds
	cfg.Telegram.Enabled = util.PromptBool(scanner, tr["enable_tg"], opts.TGToken != "" || opts.TGChatID != "")
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
		cfg.Safety.FirstLimitDryRun = util.PromptBool(scanner, tr["dry_run"], cfg.Safety.FirstLimitDryRun)
	}
	opts.Allowance = allowance
	opts.InitialTotal = initialTotal
	opts.InitialRX = initialRX
	opts.InitialTX = initialTX
	return applyOptionValues(cfg, opts)
}

func applyOptionValues(cfg config.Config, opts Options) (config.Config, error) {
	allowance, err := util.ParseBytes(opts.Allowance)
	if err != nil {
		return cfg, err
	}
	cfg.AllowanceBytes = allowance
	if opts.InitialTotal != "" && (opts.InitialRX != "" || opts.InitialTX != "") {
		return cfg, fmt.Errorf("use either --initial-total or --initial-rx/--initial-tx, not both")
	}
	if opts.InitialTotal != "" {
		total, err := util.ParseBytes(opts.InitialTotal)
		if err != nil {
			return cfg, err
		}
		cfg.InitialRXBytes = total
		cfg.InitialTXBytes = 0
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
	if opts.TGToken != "" || opts.TGChatID != "" {
		cfg.Telegram.Enabled = true
		cfg.Telegram.BotToken = opts.TGToken
		cfg.Telegram.ChatID = opts.TGChatID
	}
	return cfg, cfg.Validate()
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
	tmp := fmt.Sprintf("/usr/local/bin/flowguard.tmp.%d", os.Getpid())
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return err
	}
	if err := os.Rename(tmp, "/usr/local/bin/flowguard"); err != nil {
		return err
	}
	fmt.Println("Installed binary: /usr/local/bin/flowguard")
	return nil
}

func askInstallDeps() bool {
	scanner := bufio.NewScanner(os.Stdin)
	return util.PromptBool(scanner, "Install missing dependencies automatically", true)
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
