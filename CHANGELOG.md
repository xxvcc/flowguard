# Changelog

## Unreleased

- Harden upgrade archive extraction, binary replacement, backups, and install-directory validation.
- Add optional minisign verification for release checksums in the installer script and built-in upgrade command.
- Harden config and backup writes against symlink and non-regular-file targets.
- Harden systemd unit path validation, including rejecting binary, config, and state paths under `/tmp`.
- Redact Telegram bot tokens from notification error paths.
- Add clearer permission hints for `tc` failures that require root or `CAP_NET_ADMIN`.
- Improve installer and upgrade safety checks, documentation, and regression coverage.

## v0.1.16 - 2026-06-20

- Refactor CLI command handling into focused command files.
- Add schema version metadata to config and status JSON output.
- Add notification sender metadata and improve notification output.
- Introduce vnStat snapshot handling for more stable runtime and status behavior.

## v0.1.15 - 2026-06-20

- Harden uninstall and rollback behavior.
- Tighten systemd service capabilities and runtime restrictions.
- Improve limit handling and version metadata.

## v0.1.14 - 2026-06-20

- Harden recent usage baselines and forecast handling.
- Improve doctor checks and service restart behavior.
- Harden installer edge cases.

## v0.1.13 - 2026-06-20

- Add install baselines for recent day and week statistics.
- Add `modify --reset-recent-baseline`.
- Add more doctor checks.
- Add status forecast output.
- Stabilize JSON status fields.
- Include recent usage details in notifications.

## v0.1.12 - 2026-06-20

- Improve the status dashboard.
- Add clearer usage, forecast, and limit-state output.

## v0.1.11 - 2026-06-20

- Add localized `help` command output.

## v0.1.10 - 2026-06-20

- Persist language preference for command output and notifications.

## v0.1.9 - 2026-06-20

- Add `topup` command for adding extra allowance during the current billing period.

## v0.1.8 - 2026-06-20

- Simplify installer threshold summary wording.

## v0.1.7 - 2026-06-20

- Clarify installer units and threshold wording.

## v0.1.6 - 2026-06-20

- Fix default units for numeric installer input.

## v0.1.5 - 2026-06-20

- Fix installer input handling when running from the installed binary.

## v0.1.4 - 2026-06-20

- Add safe built-in upgrade flow.
- Harden runtime behavior around service restarts and rollback.

## v0.1.3 - 2026-06-20

- Clean up FlowGuard-managed limits during uninstall.

## v0.1.2 - 2026-06-20

- Make uninstall purge FlowGuard files by default.
- Keep options for preserving configuration or binary files when needed.

## v0.1.1 - 2026-06-19

- Fix default GitHub repository references to `xxvcc/flowguard`.
- Add GitHub Actions CI and release workflows.
- Add MIT license, security policy, changelog, and issue templates.
- Add README badges.

## v0.1.0 - 2026-06-19

- Initial public release.
- Add FlowGuard CLI and installer.
- Add `vnStat`-based traffic accounting.
- Add `tc` outbound rate limiting.
- Add billing modes: `total` and `outbound`.
- Add billing period start day support.
- Add initial usage offset modes: none, total, split.
- Add multi-interface support and `auto-public`.
- Add Bubble Tea installer UI.
- Add Telegram notifications.
- Add config backup, rollback, doctor, JSON status, and systemd service.
