package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/install"
)

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
