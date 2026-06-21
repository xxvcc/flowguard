# Security Policy

FlowGuard runs with elevated privileges during installation and uses Linux `tc` to manage network shaping. Please report security issues responsibly.

## Supported Versions

Security fixes target the latest released version.

## Reporting a Vulnerability

Please open a private security advisory on GitHub or contact the maintainer directly if private advisory is unavailable.

Do not publish working exploits or sensitive details before a fix is available.

## Sensitive Data

- Telegram bot tokens are stored in `/etc/flowguard/config.json` with root-only permissions.
- Configuration backups use the same root-only permissions and are pruned to the latest 10 files, but they can still contain Telegram bot tokens.
- `flowguard status --json` intentionally omits Telegram token values.
- Avoid sharing full config files, backups, terminal transcripts, or support bundles publicly if Telegram is enabled.

## Release Verification

- Release assets are always checked against `checksums.txt` by the installer and built-in upgrade path.
- When the maintainer publishes `checksums.txt.minisig`, set `FLOWGUARD_MINISIGN_PUBKEY` for `scripts/install.sh` or pass `flowguard upgrade --minisign-pubkey RW...` to verify the checksum file before installing or upgrading.
- Only use trusted HTTPS release mirrors; localhost HTTP is accepted only for test mirrors.
