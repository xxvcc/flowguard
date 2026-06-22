# FlowGuard

[![CI](https://github.com/xxvcc/flowguard/actions/workflows/ci.yml/badge.svg)](https://github.com/xxvcc/flowguard/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/xxvcc/flowguard)](https://github.com/xxvcc/flowguard/releases)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)
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

The installer downloads the latest GitHub Release, verifies `checksums.txt`, optionally verifies `checksums.txt.minisig` when `FLOWGUARD_MINISIGN_PUBKEY` is provided, installs the `flowguard` binary, installs dependencies, and starts the setup wizard.
Even when run through `curl | sudo sh`, the setup wizard reads input from the terminal.
Bare numbers in the setup wizard use default units: traffic defaults to `GB`, rate limits default to `mbit`.
Clear thresholds provide hysteresis: for example, soft limiting starts at 85% but clears below the default 80%, avoiding repeated limit/unlimit flapping near the boundary.

> Security note: `curl | sudo sh` is intended for quick installs on new self-managed VPS hosts. For production, download and review the script first, or pin `FLOWGUARD_VERSION=vX.Y.Z`; only use trusted HTTPS `FLOWGUARD_BASE_URL` mirrors. The installer rejects non-HTTPS downloads except localhost test mirrors.

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
- Config backup pruning, keeping the latest 10 backups, and rollback
- `doctor` diagnostics
- `status --json` without leaking Telegram tokens
- Hardened systemd service
- Bubble Tea powered installer UI

## Common Commands

| Command | Purpose |
| --- | --- |
| `flowguard install` | Install dependencies, write config, and install systemd service |
| `flowguard status` | Show current traffic, decision, and `tc` state |
| `flowguard status --verbose` | Show raw `tc` and other technical details |
| `flowguard status --json` | Script-friendly status output |
| `flowguard version` | Show version and commit information |
| `flowguard help` | Show all commands and purposes using the configured language |
| `flowguard doctor` | Diagnose config, `vnStat`, `tc`, interfaces, and service |
| `flowguard modify --allowance 1000GB` | Update config with automatic backup |
| `flowguard modify --language zh|en` | Switch later command and notification output language |
| `flowguard modify --reset-recent-baseline` | Run once after upgrading to recapture today/this-week baselines |
| `flowguard topup 100GB` | Add purchased traffic allowance, then immediately recheck/unlimit |
| `flowguard topup 100` | Same as above; bare numbers default to `GB` |
| `flowguard rollback` | Restore latest config backup |
| `flowguard upgrade` | Download, verify, and upgrade to the latest Release |
| `flowguard upgrade --version vX.Y.Z` | Upgrade to a specific version |
| `flowguard upgrade --minisign-pubkey RW...` | Verify `checksums.txt.minisig` before upgrading |
| `flowguard upgrade --no-restart` | Replace the binary without restarting the service |
| `flowguard test-notify` | Send a Telegram test notification |
| `flowguard check-once` | Run one check, useful for debugging or cron |
| `flowguard limit` | Apply the configured hard limit to all interfaces |
| `flowguard unlimit` | Remove FlowGuard-managed limits |
| `flowguard uninstall` | Remove service, config, state, and binary |
| `flowguard uninstall --keep-config=true --keep-binary=true` | Remove service while keeping config, state, and binary |
| `flowguard uninstall --remove-vnstat=true` | Also remove configured interfaces from the vnStat database |
| `flowguard uninstall --delete-custom-paths=true` | Allow deleting non-default `--config` / `--state` paths |
| `flowguard config-example` | Print example config JSON |

`flowguard status` shows a user-facing summary by default, including today, yesterday, this week (Monday start), a month-end estimate, and soft-limit ETA; raw `tc` output is only shown with `--verbose`.

## Data accounting

FlowGuard reports usage with two consistent rules so pre-install vnStat history is never counted:

- **Period total** = current vnStat monthly − install baseline (`baseline_rx_bytes` / `baseline_tx_bytes`) + your declared initial usage (`initial_rx_bytes` / `initial_tx_bytes`).
- **Today / Yesterday / This week (natural week, starts Monday)** = current vnStat daily − install day/week baseline (`baseline_day_*` / `baseline_week_*`).
- Traffic that occurred before FlowGuard was installed is excluded from FlowGuard accounting.
- If the current vnStat period total is below FlowGuard's install baseline, FlowGuard returns an error and skips the decision so a reset vnStat database cannot silently undercount usage.
- When `recent_usage_available=false`, vnStat lacks daily data; recent fields are zero and period total is unaffected.
- `recent_usage.this_week_window_days` is the number of days covered by this-week usage; after recapturing a recent baseline, forecasts average from the baseline date.
- Config files and `status --json` include `schema_version` so future scripts can detect structural versions.
- Existing installs upgrading to `v0.1.13+` should run `sudo flowguard modify --reset-recent-baseline` once so today/this-week stop including pre-install daily traffic.

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

> Note: command-line arguments may be saved in shell history or briefly visible in process listings. For a more cautious setup, run `sudo flowguard install` and enter Telegram details in the interactive wizard.

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
checksums.txt.minisig  # optional; generated when the publisher configures a minisign secret key
```

The installer always verifies the selected asset's SHA256 entry in `checksums.txt`. When `FLOWGUARD_MINISIGN_PUBKEY` is set, it first verifies `checksums.txt.minisig` with `minisign`, protecting the checksum file itself.

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_MINISIGN_PUBKEY='RW...' sh
```

For mirrors or self-hosted release files:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_BASE_URL=https://example.com/flowguard/releases/latest sh
```

To install into a custom directory, the installer forwards `FLOWGUARD_INSTALL_DIR` to the built-in setup wizard and the generated systemd unit:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_INSTALL_DIR=/opt/flowguard/bin sh
```

> Use a persistent custom path such as `/opt/flowguard/bin`. Do not place the binary, config, or state paths under `/tmp`; systemd sandboxing cannot reliably use `/tmp` paths, and FlowGuard rejects such systemd installs. If you use a custom install directory, keep using the same directory for future upgrades, for example `flowguard upgrade --install-dir /opt/flowguard/bin`.

Upgrade only the binary and skip the setup wizard:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_SKIP_SETUP=1 sh
```

To replace the binary without automatically restarting the service:

```bash
curl -fsSL https://raw.githubusercontent.com/xxvcc/flowguard/main/scripts/install.sh | \
  sudo env FLOWGUARD_SKIP_SETUP=1 FLOWGUARD_NO_RESTART=1 sh
```

After installation, you can also use the built-in upgrade command:

```bash
sudo flowguard upgrade
sudo flowguard upgrade --version v0.1.4
sudo flowguard upgrade --no-restart
sudo flowguard upgrade --minisign-pubkey 'RW...'
```

Upgrade replaces only the binary and restarts `flowguard.service` when it exists. If the built-in upgrade cannot restart the service, it rolls the binary back. It does not modify `/etc/flowguard/config.json` or `/var/lib/flowguard/state.json`. When `--minisign-pubkey` is provided, built-in upgrade requires a release `checksums.txt.minisig` that verifies successfully.

## Configuration

Default config path: `/etc/flowguard/config.json`

```json
{
  "schema_version": 1,
  "interface": "eth0",
  "interfaces": ["eth0"],
  "allowance_bytes": 1000000000000,
  "language": "zh",
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

## Notes and Limitations

- `vnStat` may need 1-2 minutes after installation before data appears.
- Config changes create root-only backups and keep the latest 10. When Telegram is enabled, those backups also contain the bot token, so redact them before sharing logs or support bundles.
- `period_day=1` uses `vnStat` monthly data; other start days sum `vnStat` daily data.
- If internal/private traffic is free on a separate NIC, monitor only the public NIC.
- If public and private traffic share one NIC, `vnStat` cannot distinguish them; a future nftables accounting mode would be needed.
- `flowguard limit` replaces the root qdisc with FlowGuard's managed `tbf` (handle `10f1:`); do not combine it with another root `tc` shaper on the same interface.
- `flowguard unlimit` only removes the root qdisc when it is FlowGuard's managed `tbf`, avoiding user-owned `tc` configurations.
- `flowguard uninstall` removes FlowGuard-managed `tbf` limits before deleting the service, default config, default state, and default binary. If you installed into a custom binary directory, remove that directory manually if you no longer need it.
- It does not remove the `vnStat` database or system packages by default; use `--remove-vnstat=true` only when those interface records are dedicated to FlowGuard.

## License

FlowGuard is licensed under the [GNU General Public License v3.0](LICENSE).

Copyright (C) 2026 Longlan. See [NOTICE](NOTICE) for attribution and original repository information.

Redistributed copies and modified versions must keep the copyright and license notices, clearly state changes, and make the corresponding source code available under the same GPL-3.0 license.
