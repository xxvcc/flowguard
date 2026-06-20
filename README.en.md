# FlowGuard

[![CI](https://github.com/xxvcc/flowguard/actions/workflows/ci.yml/badge.svg)](https://github.com/xxvcc/flowguard/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/xxvcc/flowguard)](https://github.com/xxvcc/flowguard/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/xxvcc/flowguard)](go.mod)

**FlowGuard** is a lightweight VPS bandwidth allowance guard. It watches traffic with `vnStat`, applies outbound shaping with Linux `tc`, and guides setup with a Bubble Tea TUI.

<p align="center">
  <strong>Prevent surprise bandwidth overage on small VPS instances.</strong>
</p>

<p align="center">
  <a href="README.md">中文文档</a> ·
  <a href="https://github.com/xxvcc/flowguard/releases">Releases</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

---

## Install on a Fresh VPS

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | sudo sh
```

The installer downloads the latest GitHub Release, verifies `checksums.txt`, installs the `flowguard` binary, installs dependencies, and starts the setup wizard.
Even when run through `curl | sudo sh`, the setup wizard reads input from the terminal.
Bare numbers in the setup wizard use default units: traffic defaults to `GB`, rate limits default to `mbit`.
Clear thresholds provide hysteresis: for example, soft limiting starts at 85% but clears below the default 80%, avoiding repeated limit/unlimit flapping near the boundary.

> Security note: `curl | sudo sh` is intended for quick installs on new self-managed VPS hosts. For production, download and review the script first, or pin `FLOWGUARD_VERSION=vX.Y.Z`; only use `FLOWGUARD_BASE_URL` mirrors that you trust.

> Running FlowGuard does **not** require Go on the VPS. Go is only needed when building from source.

## TUI Preview

```text
? Language / 语言 (↑/↓, Enter)
  ● zh
  ○ en

? 计费流量模式 (↑/↓, Enter)
  ● total
  ○ outbound
```

Use `↑/↓` to move, `Enter` to confirm, or number keys as shortcuts. Non-TTY input automatically falls back to numbered prompts.

## Why FlowGuard?

| Problem | FlowGuard does |
| --- | --- |
| VPS traffic allowance is easy to exceed | Tracks usage with `vnStat` |
| Providers count traffic differently | Supports `total` and `outbound` billing modes |
| Sudden traffic spikes can become expensive | Warns, rate-limits, and supports first-limit dry run |
| Manual traffic shaping is error-prone | Manages `tc` safely and avoids deleting unrelated qdiscs |
| Fresh VPS setup should be simple | One-line installer and TUI wizard |

## Features

- Persistent traffic accounting via `vnStat`
- Billing modes: `total` (`rx + tx`) or `outbound` (`tx` only)
- Billing period start day: `1-28`
- Mid-cycle initial usage offset: none / total / split
- Multi-interface support: `eth0,ens5` or `auto-public`
- Multi-interface limits split the configured total rate across interfaces
- Threshold hysteresis to prevent limit flapping
- First-limit dry run protection
- Telegram notifications
- Config backup and rollback
- `doctor` diagnostics
- `status --json` without leaking Telegram tokens
- Hardened systemd service
- Bubble Tea powered installer UI

## Common Commands

| Command | Purpose |
| --- | --- |
| `flowguard status` | Show current traffic, decision, and `tc` state |
| `flowguard status --verbose` | Show raw `tc` and other technical details |
| `flowguard status --json` | Script-friendly status output |
| `flowguard help` | Show all commands and purposes using the configured language |
| `flowguard doctor` | Diagnose config, `vnStat`, `tc`, interfaces, and service |
| `flowguard modify --allowance 1000GB` | Update config with automatic backup |
| `flowguard modify --language en` | Switch later command and notification output language |
| `flowguard topup 100GB` | Add purchased traffic allowance, then immediately recheck/unlimit |
| `flowguard topup 100` | Same as above; bare numbers default to `GB` |
| `flowguard rollback` | Restore latest config backup |
| `flowguard upgrade` | Download, verify, and upgrade to the latest Release |
| `flowguard upgrade --version vX.Y.Z` | Upgrade to a specific version |
| `flowguard test-notify` | Send a Telegram test notification |
| `flowguard uninstall` | Remove service, config, state, and binary |
| `flowguard uninstall --keep-config=true --keep-binary=true` | Remove service while keeping config, state, and binary |
| `flowguard uninstall --remove-vnstat=true` | Also remove configured interfaces from the vnStat database |

`flowguard status` shows a user-facing summary by default, including today, yesterday, and this week (Monday start) billable usage; raw `tc` output is only shown with `--verbose`.

## Non-Interactive Install

Provider counts inbound + outbound:

```bash
sudo flowguard install --yes \
  --allowance 1000GB \
  --billing-mode total \
  --period-day 1 \
  --initial-total 0
```

Provider counts outbound only:

```bash
sudo flowguard install --yes \
  --allowance 1000GB \
  --billing-mode outbound \
  --period-day 1 \
  --initial-rx 20GB \
  --initial-tx 80GB
```

Enable Telegram:

```bash
sudo flowguard install --yes \
  --allowance 1000GB \
  --billing-mode total \
  --tg-token '123:abc' \
  --tg-chat-id '123456789'
```

## Build from Source

```bash
git clone https://github.com/xxvcc/flowguard.git
cd flowguard
go build -o flowguard ./cmd/flowguard
sudo ./flowguard install
```

## Release Assets

Automatic releases provide:

```text
flowguard_linux_amd64.tar.gz
flowguard_linux_arm64.tar.gz
flowguard_linux_armv7.tar.gz
checksums.txt
```

For mirrors or self-hosted release files:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_BASE_URL=https://example.com/flowguard/releases/latest sh
```

Upgrade only the binary and skip the setup wizard:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_SKIP_SETUP=1 sh
```

After installation, you can also use the built-in upgrade command:

```bash
sudo flowguard upgrade
sudo flowguard upgrade --version v0.1.4
```

Upgrade replaces only the binary and restarts the service. It does not modify `/etc/flowguard/config.json` or `/var/lib/flowguard/state.json`.

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

## Notes and Limitations

- `vnStat` may need 1-2 minutes after installation before data appears.
- `period_day=1` uses `vnStat` monthly data; other start days sum `vnStat` daily data.
- If internal/private traffic is free on a separate NIC, monitor only the public NIC.
- If public and private traffic share one NIC, `vnStat` cannot distinguish them; a future nftables accounting mode would be needed.
- `flowguard limit` replaces the root qdisc with FlowGuard's managed `tbf` (handle `10f1:`); do not combine it with another root `tc` shaper on the same interface.
- `flowguard unlimit` only removes the root qdisc when it is FlowGuard's managed `tbf`, avoiding user-owned `tc` configurations.
- `flowguard uninstall` removes FlowGuard-managed `tbf` limits before deleting the service, config, state, and binary.
- It does not remove the `vnStat` database or system packages by default; use `--remove-vnstat=true` only when those interface records are dedicated to FlowGuard.

## License

MIT
