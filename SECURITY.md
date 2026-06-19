# Security Policy

FlowGuard runs with elevated privileges during installation and uses Linux `tc` to manage network shaping. Please report security issues responsibly.

## Supported Versions

Security fixes target the latest released version.

## Reporting a Vulnerability

Please open a private security advisory on GitHub or contact the maintainer directly if private advisory is unavailable.

Do not publish working exploits or sensitive details before a fix is available.

## Sensitive Data

- Telegram bot tokens are stored in `/etc/flowguard/config.json` with root-only permissions.
- `flowguard status --json` intentionally omits Telegram token values.
- Avoid sharing full config files publicly if Telegram is enabled.
