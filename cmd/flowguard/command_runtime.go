package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/notify"
	"flowguard/internal/traffic"
	"flowguard/internal/util"
)

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
	return (notify.New(cfg)).Send("FlowGuard test notification.")
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

func cmdVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	jsonOutput := fs.Bool("json", false, "print version as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	info := map[string]string{"version": version, "commit": commit}
	if *jsonOutput {
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("flowguard %s (%s)\n", version, commit)
	return nil
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
		if err := removeLimits(cfg); err != nil {
			if notifyErr := sendErrorNotification(cfg, statePath, state, "tc", "FlowGuard: new period started but removing old limit failed: "+err.Error(), sendNotifications); notifyErr != nil && sendNotifications {
				fmt.Fprintf(os.Stderr, "notify error: %v\n", notifyErr)
			}
			return err
		}
		state = config.DefaultState(usage.Period)
		if sendNotifications {
			_ = (notify.New(cfg)).Send("FlowGuard: new period started, limit removed.")
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
			recent, recentAvailable := loadRecentForNotification(cfg, now)
			_ = (notify.New(cfg)).Send(formatDryRunNotification(cfg, usage, decision, recent, recentAvailable))
		}
		return config.SaveState(statePath, state)
	}
	desiredLimitKey, err := appliedLimitKey(cfg, decision.LimitRate)
	if err != nil {
		return err
	}
	limitMissing := false
	if decision.LimitRate != "" && state.Limited && desiredLimitKey == state.AppliedLimitKey {
		missing, err := anyFlowGuardLimitMissing(cfg)
		if err != nil {
			return err
		}
		limitMissing = missing
	}
	if decision.LimitRate != "" && (!state.Limited || desiredLimitKey != state.AppliedLimitKey || limitMissing) {
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
		recent, recentAvailable := loadRecentForNotification(cfg, now)
		msg := formatNotification(cfg, usage, decision, recent, recentAvailable)
		if isErrorCoolingDown(state, "notify", now) {
			// Preserve LastErrorAt so the retry window can expire.
		} else if err := (notify.New(cfg)).Send(msg); err != nil {
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
	if err := (notify.New(cfg)).Send(message); err != nil {
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
	var applied []string
	for _, iface := range cfg.Interfaces {
		if err := traffic.ApplySplitLimit(iface, rate, len(cfg.Interfaces)); err != nil {
			for _, appliedIface := range applied {
				_ = traffic.RemoveLimit(appliedIface)
			}
			return err
		}
		applied = append(applied, iface)
	}
	return nil
}

func anyFlowGuardLimitMissing(cfg config.Config) (bool, error) {
	for _, iface := range cfg.Interfaces {
		present, err := traffic.HasFlowGuardLimit(iface)
		if err != nil {
			return false, err
		}
		if !present {
			return true, nil
		}
	}
	return false, nil
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
func loadRecentForNotification(cfg config.Config, now time.Time) (traffic.RecentUsage, bool) {
	recent, err := traffic.ReadRecentUsage(cfg, now)
	if err != nil {
		return traffic.RecentUsage{}, false
	}
	return recent, true
}

func formatDryRunNotification(cfg config.Config, usage traffic.Usage, decision traffic.Decision, recent traffic.RecentUsage, recentAvailable bool) string {
	if isZH(cfg) {
		return formatNotification(cfg, usage, decision, recent, recentAvailable) + "\n首次限速保护：本轮只通知，不执行 tc 限速。下次仍满足条件时会实际限速。"
	}
	return formatNotification(cfg, usage, decision, recent, recentAvailable) + "\nFirst-limit dry run: no tc limit applied this cycle. Next matching cycle will apply the limit."
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

func formatNotification(cfg config.Config, usage traffic.Usage, decision traffic.Decision, recent traffic.RecentUsage, recentAvailable bool) string {
	var b strings.Builder
	if isZH(cfg) {
		fmt.Fprintf(&b, "FlowGuard：%s\n\n", decision.Level)
		fmt.Fprintf(&b, "网卡：%s\n", interfaceListText(cfg))
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
		if recentAvailable {
			fmt.Fprintf(&b, "今日新增：%s\n", util.FormatBytes(recent.Today.BillableBytes))
			fmt.Fprintf(&b, "本周累计：%s\n", util.FormatBytes(recent.ThisWeek.BillableBytes))
		} else {
			fmt.Fprintf(&b, "近期统计：暂不可用\n")
		}
		if decision.LimitRate != "" {
			fmt.Fprintf(&b, "限速：%s\n", decision.LimitRate)
		} else {
			fmt.Fprintf(&b, "限速：无\n")
		}
		return b.String()
	}
	fmt.Fprintf(&b, "FlowGuard: %s\n\n", decision.Level)
	fmt.Fprintf(&b, "Interfaces: %s\n", interfaceListText(cfg))
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
	if recentAvailable {
		fmt.Fprintf(&b, "Today: %s\n", util.FormatBytes(recent.Today.BillableBytes))
		fmt.Fprintf(&b, "This week: %s\n", util.FormatBytes(recent.ThisWeek.BillableBytes))
	} else {
		fmt.Fprintf(&b, "Recent usage: unavailable\n")
	}
	if decision.LimitRate != "" {
		fmt.Fprintf(&b, "Limit: %s\n", decision.LimitRate)
	} else {
		fmt.Fprintf(&b, "Limit: none\n")
	}
	return b.String()
}

func interfaceListText(cfg config.Config) string {
	if len(cfg.Interfaces) > 0 {
		return strings.Join(cfg.Interfaces, ",")
	}
	return cfg.Interface
}
