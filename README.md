# FlowGuard

> Lightweight VPS traffic allowance guard powered by `vnStat`, Linux `tc`, Telegram alerts, and a Bubble Tea TUI installer.

[中文文档](README.zh-CN.md)

FlowGuard helps small VPS instances avoid unexpected bandwidth overage by monitoring recorded traffic, warning at thresholds, and applying outbound rate limits when needed.

## Features

- Persistent traffic accounting via `vnStat`
- Billing modes:
  - `total`: inbound + outbound
  - `outbound`: outbound only
- Billing period start day: `1-28`
- Initial usage offset for mid-cycle installation
- Multiple interfaces: `eth0,ens5` or `auto-public`
- Threshold hysteresis to prevent limit flapping
- First-limit dry run protection
- Telegram notifications
- Config backup and rollback
- `doctor` diagnostics
- JSON status output without leaking Telegram tokens
- Hardened systemd service
- Bubble Tea powered installer UI

## Quick Start

```bash
go build -o flowguard ./cmd/flowguard
sudo ./flowguard install
```

Running FlowGuard does **not** require Go on the VPS. Go is only required if you build from source on that machine. You can also build elsewhere and copy the binary to a fresh VPS.

## Deploy to a Fresh VPS

Recommended one-line install from GitHub Releases:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | sudo sh
```

If your repository or tag is different:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_REPO=xxvcc/flowguard FLOWGUARD_VERSION=latest sh
```

For mirrors or self-hosted release files, override the download base URL:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_BASE_URL=https://example.com/flowguard/releases/latest sh
```

The install script:

1. Detects Linux architecture: `amd64`, `arm64`, or `armv7`
2. Downloads the matching release archive
3. Downloads `checksums.txt`
4. Verifies SHA256 before installing
5. Installs `flowguard` to `/usr/local/bin`
6. Starts `flowguard install`

Expected release assets:

```text
flowguard_linux_amd64.tar.gz
flowguard_linux_arm64.tar.gz
flowguard_linux_armv7.tar.gz
checksums.txt
```

Manual copy also works:

```bash
# Build on your local machine
GOOS=linux GOARCH=amd64 go build -o flowguard ./cmd/flowguard

# Copy to VPS
scp flowguard root@your-vps:/root/flowguard

# Run on VPS
ssh root@your-vps
chmod +x /root/flowguard
sudo /root/flowguard install
```

The first interactive prompt chooses language. In a terminal, use `↑/↓` to move the filled radio dot and press Enter to confirm. In non-interactive input, it falls back to numbered choices.

```text
? Language / 语言
  ● zh
  ○ en
```

## Non-Interactive Install

Provider counts inbound + outbound:

```bash
sudo ./flowguard install --yes \
  --allowance 1000GB \
  --billing-mode total \
  --period-day 1 \
  --initial-total 0
```

Provider counts outbound only:

```bash
sudo ./flowguard install --yes \
  --allowance 1000GB \
  --billing-mode outbound \
  --period-day 1 \
  --initial-rx 20GB \
  --initial-tx 80GB
```

Enable Telegram:

```bash
sudo ./flowguard install --yes \
  --allowance 1000GB \
  --billing-mode total \
  --tg-token '123:abc' \
  --tg-chat-id '123456789'
```

## Commands

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

## Configuration

Default config path: `/etc/flowguard/config.json`

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

## Notes

- `vnStat` may need 1-2 minutes after installation before it has usable data.
- `period_day=1` uses `vnStat` monthly data. Other start days are calculated from `vnStat` daily data.
- If private traffic is free and uses a separate private NIC, monitor only the public NIC.
- If public and private traffic share one NIC, `vnStat` cannot distinguish them; this would require a future nftables accounting mode.
- `flowguard limit` replaces the root qdisc with `tbf`; do not combine it with another root `tc` shaper on the same interface.
- `flowguard unlimit` only removes root qdisc when it is currently `tbf`.
