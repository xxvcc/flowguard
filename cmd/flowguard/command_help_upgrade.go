package main

import (
	"flag"
	"fmt"
	"strings"

	"flowguard/internal/config"
	"flowguard/internal/upgrade"
)

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
	{"flowguard help", "按配置语言显示所有命令和用途", "Show all commands and purposes using the configured language"},
	{"flowguard version", "显示版本和提交信息", "Show version and commit information"},
	{"flowguard doctor", "检查配置、vnStat、tc、网卡和服务", "Diagnose config, vnStat, tc, interfaces, and service"},
	{"flowguard modify --allowance 1000GB", "修改配置并自动备份", "Update config with automatic backup"},
	{"flowguard modify --language zh|en", "切换后续命令和通知输出语言", "Switch later command and notification output language"},
	{"flowguard modify --reset-recent-baseline", "重设今日/本周基线（升级到含日/周基线版本后跑一次）", "Recapture today/this-week baselines (run once after upgrading)"},
	{"flowguard topup 100GB", "购买额外流量后追加额度，并立即重新评估/解除限速", "Add purchased traffic allowance, then immediately recheck/unlimit"},
	{"flowguard topup 100", "同上；裸数字默认单位为 GB", "Same as above; bare numbers default to GB"},
	{"flowguard rollback", "回滚到最近一次配置备份", "Restore latest config backup"},
	{"flowguard upgrade", "下载、校验并升级到最新 Release", "Download, verify, and upgrade to the latest Release"},
	{"flowguard upgrade --version vX.Y.Z", "升级到指定版本", "Upgrade to a specific version"},
	{"flowguard upgrade --no-restart", "只替换二进制，不自动重启服务", "Replace binary without restarting the service"},
	{"flowguard test-notify", "发送 Telegram 测试通知", "Send a Telegram test notification"},
	{"flowguard check-once", "运行一次检查，适合调试/cron", "Run one monitor iteration for debugging/cron"},
	{"flowguard limit", "按配置对所有网卡应用强限速", "Apply the configured hard limit to all interfaces"},
	{"flowguard unlimit", "移除 FlowGuard 自己创建的限速", "Remove FlowGuard-managed limits"},
	{"flowguard uninstall", "卸载服务并删除配置、状态和二进制", "Remove service, config, state, and binary"},
	{"flowguard uninstall --keep-config=true --keep-binary=true", "卸载服务但保留配置、状态和二进制", "Remove service while keeping config, state, and binary"},
	{"flowguard uninstall --remove-vnstat=true", "卸载时同时移除配置网卡的 vnStat 数据", "Also remove configured interfaces from the vnStat database"},
	{"flowguard uninstall --delete-custom-paths=true", "允许删除非默认 --config / --state 路径", "Allow deleting non-default --config / --state paths"},
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
