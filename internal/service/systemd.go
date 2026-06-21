package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flowguard/internal/util"
)

const systemdUnitPath = "/etc/systemd/system/flowguard.service"

func InstallSystemd(binaryPath string, configPath string, statePath string) error {
	if err := validateSystemdPaths(binaryPath, configPath, statePath); err != nil {
		return err
	}
	unit := renderSystemdUnit(binaryPath, configPath, statePath)
	if err := os.WriteFile(systemdUnitPath, []byte(unit), 0644); err != nil {
		return err
	}
	if _, err := util.Run(30*time.Second, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if _, err := util.Run(30*time.Second, "systemctl", "enable", "flowguard"); err != nil {
		return err
	}
	if _, err := util.Run(30*time.Second, "systemctl", "restart", "flowguard"); err != nil {
		return err
	}
	return nil
}

func renderSystemdUnit(binaryPath string, configPath string, statePath string) string {
	stateDir := filepath.Dir(statePath)
	configDir := filepath.Dir(configPath)
	return fmt.Sprintf(`[Unit]
Description=FlowGuard
After=network-online.target vnstat.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run --config %s --state %s
Restart=always
RestartSec=5s
Environment=FLOWGUARD_TEST_PATH=0
UMask=0077
NoNewPrivileges=true
AmbientCapabilities=CAP_NET_ADMIN
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadOnlyPaths=%s
ReadWritePaths=%s
CapabilityBoundingSet=CAP_NET_ADMIN
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
`, systemdEscapeArg(binaryPath), systemdEscapeArg(configPath), systemdEscapeArg(statePath), systemdEscapeArg(configDir), systemdEscapeArg(stateDir))
}

func systemdEscapeArg(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '%':
			b.WriteString("%%")
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func validateSystemdPaths(binaryPath string, configPath string, statePath string) error {
	binaryDir := filepath.Clean(filepath.Dir(binaryPath))
	configDir := filepath.Clean(filepath.Dir(configPath))
	stateDir := filepath.Clean(filepath.Dir(statePath))
	if binaryDir == "." || binaryDir == string(os.PathSeparator) {
		return fmt.Errorf("refusing unsafe binary directory for systemd unit: %s", binaryDir)
	}
	if configDir == "." || configDir == string(os.PathSeparator) {
		return fmt.Errorf("refusing unsafe config directory for systemd unit: %s", configDir)
	}
	if stateDir == "." || stateDir == string(os.PathSeparator) {
		return fmt.Errorf("refusing unsafe state directory for systemd unit: %s", stateDir)
	}
	for _, path := range []struct {
		label string
		value string
	}{
		{"binary directory", binaryDir},
		{"config directory", configDir},
		{"state directory", stateDir},
	} {
		if isUnderTmp(path.value) {
			return fmt.Errorf("refusing %s under /tmp for systemd unit with PrivateTmp=true: %s", path.label, path.value)
		}
	}
	for _, unsafe := range []string{"/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/var", "/var/lib", "/dev", "/proc", "/sys", "/run", "/home"} {
		if stateDir == unsafe {
			return fmt.Errorf("refusing overly broad state directory for systemd ReadWritePaths: %s", stateDir)
		}
	}
	return nil
}

func isUnderTmp(path string) bool {
	path = filepath.Clean(path)
	return path == "/tmp" || strings.HasPrefix(path, "/tmp/")
}

func Restart() error {
	if !util.CommandExists("systemctl") {
		return nil
	}
	exists, err := unitExists()
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	_, err = util.Run(30*time.Second, "systemctl", "restart", "flowguard")
	return err
}

func unitExists() (bool, error) {
	result, err := util.Run(10*time.Second, "systemctl", "list-unit-files", "flowguard.service", "--no-legend")
	if err != nil {
		return false, err
	}
	return unitListContainsFlowGuard(result.Stdout), nil
}

func unitListContainsFlowGuard(stdout string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "flowguard.service" {
			return true
		}
	}
	return false
}
