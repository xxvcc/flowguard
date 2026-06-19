# FlowGuard

> 基于 `vnStat`、Linux `tc`、Telegram 通知和 Bubble Tea TUI 安装向导的轻量 VPS 流量守卫。

[English README](README.md)

FlowGuard 用来降低 VPS 流量超额风险：持续读取流量统计，到达阈值后提醒，并在必要时自动限制出站带宽。

## 功能

- 使用 `vnStat` 做持久化流量统计
- 计费模式：
  - `total`：入站 + 出站
  - `outbound`：仅出站
- 支持账期起始日：`1-28`
- 支持安装时录入本月已用流量
- 支持多网卡：`eth0,ens5` 或 `auto-public`
- 恢复阈值防抖，避免反复限速/解限
- 首次限速保护：第一次触发只提醒，不立刻执行 `tc`
- Telegram 通知
- 配置自动备份和回滚
- `doctor` 诊断命令
- `status --json`，不会泄露 Telegram token
- systemd 服务加固
- 基于 Bubble Tea 的安装 TUI

## 快速开始

```bash
go build -o flowguard ./cmd/flowguard
sudo ./flowguard install
```

VPS 上运行 FlowGuard **不需要 Go 环境**。只有在 VPS 上从源码编译时才需要 Go；也可以在其他机器编译好后，把单个二进制复制到新 VPS。

## 部署到全新 VPS

推荐从 GitHub Releases 一行安装：

```bash
curl -fsSL https://raw.githubusercontent.com/cnlanny/flowguard/main/scripts/install.sh | sudo sh
```

如果仓库或版本不同：

```bash
curl -fsSL https://raw.githubusercontent.com/cnlanny/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_REPO=cnlanny/flowguard FLOWGUARD_VERSION=latest sh
```

如果使用镜像或自建 release 文件，可以覆盖下载基础地址：

```bash
curl -fsSL https://raw.githubusercontent.com/cnlanny/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_BASE_URL=https://example.com/flowguard/releases/latest sh
```

安装脚本会：

1. 检测 Linux 架构：`amd64`、`arm64` 或 `armv7`
2. 下载对应 release 压缩包
3. 下载 `checksums.txt`
4. 校验 SHA256 后再安装
5. 安装 `flowguard` 到 `/usr/local/bin`
6. 启动 `flowguard install` 交互安装

Release 需要包含这些文件：

```text
flowguard_linux_amd64.tar.gz
flowguard_linux_arm64.tar.gz
flowguard_linux_armv7.tar.gz
checksums.txt
```

手动复制二进制也可以：

```bash
# 在本机编译
GOOS=linux GOARCH=amd64 go build -o flowguard ./cmd/flowguard

# 上传到 VPS
scp flowguard root@your-vps:/root/flowguard

# 在 VPS 上运行安装
ssh root@your-vps
chmod +x /root/flowguard
sudo /root/flowguard install
```

安装的第一个问题会选择语言。在终端里使用 `↑/↓` 移动实心圆点，按 Enter 确认；在非交互输入里会自动回退为数字选择。

```text
? Language / 语言
  ● zh
  ○ en
```

## 非交互安装

服务商按入站 + 出站计费：

```bash
sudo ./flowguard install --yes \
  --allowance 1000GB \
  --billing-mode total \
  --period-day 1 \
  --initial-total 0
```

服务商只按出站计费：

```bash
sudo ./flowguard install --yes \
  --allowance 1000GB \
  --billing-mode outbound \
  --period-day 1 \
  --initial-rx 20GB \
  --initial-tx 80GB
```

启用 Telegram：

```bash
sudo ./flowguard install --yes \
  --allowance 1000GB \
  --billing-mode total \
  --tg-token '123:abc' \
  --tg-chat-id '123456789'
```

## 常用命令

```bash
flowguard status
flowguard status --json
flowguard doctor
flowguard modify --allowance 1000GB --billing-mode outbound
flowguard rollback
flowguard config-example
flowguard limit --rate 1mbit
flowguard unlimit
flowguard test-notify
flowguard check-once
flowguard uninstall --keep-config=true
```

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

## 注意事项

- `vnStat` 安装后可能需要 1-2 分钟才有可用数据。
- `period_day=1` 使用 `vnStat` 月统计；其他起始日通过 `vnStat` 日统计累加。
- 如果服务商内网免费且内网有独立网卡，只监控公网网卡即可。
- 如果公网/内网共用同一网卡，`vnStat` 无法区分，需要未来增加 nftables 统计模式。
- `flowguard limit` 会把网卡 root qdisc 替换成 `tbf`，不要和其他 root `tc` 限速器共用。
- `flowguard unlimit` 只会在当前 root qdisc 是 `tbf` 时删除，避免误删默认 `mq/fq_codel`。
