# 安全策略 / Security Policy

## 中文

FlowGuard 在安装、升级和运行部分命令时需要较高权限，并会使用 Linux `tc` 管理网络限速。请负责任地报告安全问题，不要在修复发布前公开可利用细节。

### 支持版本

安全修复优先面向最新发布版本。请先升级到最新 release，再确认问题是否仍然存在。

### 报告漏洞

如果你发现安全漏洞，请优先使用 GitHub 的私有安全公告（Private security advisory）报告；如果不可用，请直接联系维护者。

报告时建议包含：

- 受影响的 FlowGuard 版本
- 操作系统和发行版版本
- 复现步骤
- 影响范围
- 相关日志或错误信息（请先删除敏感信息）

请不要在修复发布前公开可工作的 exploit、token、配置文件或其他敏感细节。

### 敏感数据

- Telegram bot token 存储在 `/etc/flowguard/config.json`，文件权限应仅允许 root 读取。
- 配置备份使用同样的 root-only 权限，并默认只保留最近 10 份，但备份中仍可能包含 Telegram bot token。
- `flowguard status --json` 会刻意省略 Telegram token 值。
- 如果启用了 Telegram，请不要公开分享完整配置文件、配置备份、终端完整记录或支持包。

### 发布校验

- 安装脚本和内置升级流程都会使用 `checksums.txt` 校验 release assets。
- 如果维护者发布了 `checksums.txt.minisig`，可以为 `scripts/install.sh` 设置 `FLOWGUARD_MINISIGN_PUBKEY`，或在升级时传入 `flowguard upgrade --minisign-pubkey RW...`，先验证 checksum 文件再安装/升级。
- 只使用可信 HTTPS release 镜像；localhost HTTP 仅用于测试镜像。

## English

FlowGuard may require elevated privileges during installation, upgrades, and some runtime commands. It also uses Linux `tc` to manage traffic shaping. Please report security issues responsibly and do not disclose exploitable details before a fix is available.

### Supported Versions

Security fixes target the latest released version. Please upgrade to the latest release first, then confirm whether the issue still applies.

### Reporting a Vulnerability

If you find a security vulnerability, please use GitHub private security advisories when available. If private advisories are unavailable, contact the maintainer directly.

Useful reports include:

- Affected FlowGuard version
- Operating system and distribution version
- Reproduction steps
- Impact assessment
- Relevant logs or errors with sensitive data removed

Do not publish working exploits, tokens, configuration files, or other sensitive details before a fix is released.

### Sensitive Data

- Telegram bot tokens are stored in `/etc/flowguard/config.json`, which should be readable only by root.
- Configuration backups use the same root-only permissions and keep the latest 10 files by default, but they may still contain Telegram bot tokens.
- `flowguard status --json` intentionally omits Telegram token values.
- If Telegram is enabled, avoid sharing full config files, config backups, complete terminal transcripts, or support bundles publicly.

### Release Verification

- The installer and built-in upgrade flow verify release assets against `checksums.txt`.
- When the maintainer publishes `checksums.txt.minisig`, set `FLOWGUARD_MINISIGN_PUBKEY` for `scripts/install.sh`, or pass `flowguard upgrade --minisign-pubkey RW...`, to verify the checksum file before installing or upgrading.
- Only use trusted HTTPS release mirrors; localhost HTTP is accepted only for test mirrors.
