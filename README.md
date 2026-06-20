# FlowGuard

[![CI](https://github.com/xxvcc/flowguard/actions/workflows/ci.yml/badge.svg)](https://github.com/xxvcc/flowguard/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/xxvcc/flowguard)](https://github.com/xxvcc/flowguard/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/xxvcc/flowguard)](go.mod)

**FlowGuard** 是一个轻量 VPS 流量额度守卫。它通过 `vnStat` 统计流量，用 Linux `tc` 做出站限速，并提供 Bubble Tea TUI 安装向导。

<p align="center">
  <strong>帮小 VPS 降低流量超额和意外账单风险。</strong>
</p>

<p align="center">
  <a href="README.en.md">English README</a> ·
  <a href="https://github.com/xxvcc/flowguard/releases">Releases</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

---

## 全新 VPS 一行安装

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | sudo sh
```

安装脚本会下载最新 GitHub Release，校验 `checksums.txt`，安装 `flowguard` 二进制，安装依赖，并启动安装向导。
即使通过 `curl | sudo sh` 管道运行，安装向导也会从终端读取输入。
安装向导里的裸数字会按默认单位理解：流量默认 `GB`，限速默认 `mbit`。
内置防抖机制：例如 85% 触发轻度限速，但要降到默认 80% 以下才解除，避免在边界反复限速/解限。

> 安全提示：`curl | sudo sh` 适合全新自管 VPS 的快速安装。生产环境建议先下载并审阅脚本，或固定 `FLOWGUARD_VERSION=vX.Y.Z` 后安装；使用 `FLOWGUARD_BASE_URL` 镜像时，请只使用你信任的发布源。

> VPS 上运行 FlowGuard **不需要 Go 环境**。只有从源码编译时才需要 Go。

## TUI 预览

```text
? Language / 语言 (↑/↓, Enter)
  ● zh
  ○ en

? 计费流量模式 (↑/↓, Enter)
  ● total
  ○ outbound
```

使用 `↑/↓` 移动，`Enter` 确认，也可以用数字键快捷选择。非 TTY 输入会自动回退为数字提示。

## 为什么用 FlowGuard？

| 问题 | FlowGuard 的处理 |
| --- | --- |
| VPS 流量额度容易超 | 用 `vnStat` 持久化统计 |
| 不同服务商计费规则不同 | 支持 `total` 和 `outbound` 两种计费模式 |
| 突发流量可能导致账单风险 | 阈值提醒、自动限速、首次限速保护 |
| 手动配置 `tc` 容易出错 | 自动管理限速，并避免误删无关 qdisc |
| 新 VPS 配置繁琐 | 一行安装 + TUI 向导 |

## 功能

- 使用 `vnStat` 做持久化流量统计
- 计费模式：`total`（入站 + 出站）或 `outbound`（仅出站）
- 支持账期起始日：`1-28`
- 支持安装时录入本月已用流量：不填 / 总量 / 分项
- 支持多网卡：`eth0,ens5` 或 `auto-public`
- 多网卡限速按总限速值平分到每个接口
- 恢复阈值防抖，避免反复限速/解限
- 首次限速保护：第一次触发只提醒，不立刻执行 `tc`
- Telegram 通知
- 配置自动备份和回滚
- `doctor` 诊断命令
- `status --json`，不会泄露 Telegram token
- systemd 服务加固
- 基于 Bubble Tea 的安装 TUI

## 常用命令

| 命令 | 用途 |
| --- | --- |
| `flowguard status` | 查看当前流量、决策和 `tc` 状态 |
| `flowguard status --verbose` | 显示原始 `tc` 等技术细节 |
| `flowguard status --json` | 输出适合脚本读取的 JSON |
| `flowguard help` | 按配置语言显示所有命令和用途 |
| `flowguard doctor` | 检查配置、`vnStat`、`tc`、网卡和服务 |
| `flowguard modify --allowance 1000GB` | 修改配置并自动备份 |
| `flowguard modify --language zh` | 切换后续命令和通知输出语言 |
| `flowguard modify --reset-recent-baseline` | 升级后跑一次，重设今日/本周基线 |
| `flowguard topup 100GB` | 购买额外流量后追加额度，并立即重新评估/解除限速 |
| `flowguard topup 100` | 同上；裸数字默认单位为 `GB` |
| `flowguard rollback` | 回滚到最近一次配置备份 |
| `flowguard upgrade` | 下载、校验并升级到最新 Release |
| `flowguard upgrade --version vX.Y.Z` | 升级到指定版本 |
| `flowguard test-notify` | 发送 Telegram 测试通知 |
| `flowguard uninstall` | 卸载服务并删除配置、状态和二进制 |
| `flowguard uninstall --keep-config=true --keep-binary=true` | 卸载服务但保留配置、状态和二进制 |
| `flowguard uninstall --remove-vnstat=true` | 卸载时同时移除配置网卡的 vnStat 数据 |

`flowguard status` 默认显示用户摘要，并包含今天、昨天、本周（周一开始）的计费用量、本月预测和软限速 ETA；原始 `tc` 输出只在 `--verbose` 中显示。

## 数据口径

FlowGuard 在 `status` 中使用两套口径，避免把安装前的 vnStat 历史流量算进来：

- **账期总用量** = 当前 vnStat 月度数据 − 安装基线（`baseline_rx_bytes` / `baseline_tx_bytes`）+ 你录入的初始用量（`initial_rx_bytes` / `initial_tx_bytes`）。
- **今天 / 昨天 / 本周（自然周，周一开始）** = 当前 vnStat 日数据 − 安装当日 / 当周基线（`baseline_day_*` / `baseline_week_*`）。
- 安装前的流量（vnStat 已有的历史数据）一律不算入 FlowGuard 用量。
- `recent_usage_available=false` 时表示 vnStat 暂无可用日数据；近期统计返回零值，主账期用量不受影响。
- `recent_usage.this_week_window_days` 表示本周统计实际覆盖的天数；重设近期基线后，预测会从基线日期开始算日均。
- 升级到 `v0.1.13+` 的旧安装需要执行一次 `sudo flowguard modify --reset-recent-baseline` 来补齐今天/本周基线，否则近期统计会回到 vnStat 原始日数据。

## 非交互安装

服务商按入站 + 出站计费：

```bash
sudo flowguard install --yes \
  --allowance 1000GB \
  --billing-mode total \
  --period-day 1 \
  --initial-total 0
```

服务商只按出站计费：

```bash
sudo flowguard install --yes \
  --allowance 1000GB \
  --billing-mode outbound \
  --period-day 1 \
  --initial-rx 20GB \
  --initial-tx 80GB
```

启用 Telegram：

```bash
sudo flowguard install --yes \
  --allowance 1000GB \
  --billing-mode total \
  --tg-token '123:abc' \
  --tg-chat-id '123456789'
```

## 从源码构建

```bash
git clone https://github.com/xxvcc/flowguard.git
cd flowguard
go build -o flowguard ./cmd/flowguard
sudo ./flowguard install
```

## Release 资产

自动 Release 会生成：

```text
flowguard_linux_amd64.tar.gz
flowguard_linux_arm64.tar.gz
flowguard_linux_armv7.tar.gz
checksums.txt
```

如果使用镜像或自建 release 文件：

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_BASE_URL=https://example.com/flowguard/releases/latest sh
```

只升级二进制、跳过安装向导：

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_SKIP_SETUP=1 sh
```

如果只想替换二进制、不自动重启服务：

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_SKIP_SETUP=1 FLOWGUARD_NO_RESTART=1 sh
```

已安装后也可以直接使用内置升级命令：

```bash
sudo flowguard upgrade
sudo flowguard upgrade --version v0.1.4
sudo flowguard upgrade --no-restart
```

升级只替换二进制，默认会在检测到 `flowguard.service` 时重启服务；如果服务重启失败，内置升级会回滚二进制。升级不会修改 `/etc/flowguard/config.json` 和 `/var/lib/flowguard/state.json`。

## 配置文件

默认路径：`/etc/flowguard/config.json`

```json
{
  "interface": "eth0",
  "interfaces": ["eth0"],
  "allowance_bytes": 1000000000000,
  "period_day": 1,
  "billing_mode": "total",
  "check_interval_seconds": 60,
  "initial_period": "2026-06",
  "initial_rx_bytes": 0,
  "initial_tx_bytes": 0,
  "baseline_rx_bytes": 0,
  "baseline_tx_bytes": 0,
  "thresholds": {
    "warn_percent": 70,
    "soft_percent": 85,
    "hard_percent": 95,
    "warn_clear_percent": 65,
    "soft_clear_percent": 80,
    "hard_clear_percent": 90
  },
  "limits": {
    "soft_rate": "10mbit",
    "hard_rate": "1mbit"
  },
  "safety": {
    "first_limit_dry_run": true
  },
  "telegram": {
    "enabled": false,
    "bot_token": "",
    "chat_id": ""
  }
}
```

## 注意事项和限制

- `vnStat` 安装后可能需要 1-2 分钟才有可用数据。
- `period_day=1` 使用 `vnStat` 月统计；其他起始日通过 `vnStat` 日统计累加。
- 如果服务商内网免费且内网有独立网卡，只监控公网网卡即可。
- 如果公网/内网共用同一网卡，`vnStat` 无法区分，需要未来增加 nftables 统计模式。
- `flowguard limit` 会把网卡 root qdisc 替换成 FlowGuard 管理的 `tbf`（handle `10f1:`），不要和其他 root `tc` 限速器共用。
- `flowguard unlimit` 只会在当前 root qdisc 是 FlowGuard 的 `tbf` 时删除，避免误删用户已有的 `tc` 配置。
- `flowguard uninstall` 会先解除 FlowGuard 管理的 `tbf` 限速，再删除服务、配置、状态和二进制。
- 默认不会删除 `vnStat` 数据库和系统依赖；如确认这些接口数据只给 FlowGuard 使用，可加 `--remove-vnstat=true`。

## License

MIT
