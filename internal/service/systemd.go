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
	stateDir := filepath.Dir(statePath)
	unit := fmt.Sprintf(`[Unit]
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
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s
CapabilityBoundingSet=CAP_NET_ADMIN
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
`, systemdEscapeArg(binaryPath), systemdEscapeArg(configPath), systemdEscapeArg(statePath), systemdEscapeArg(stateDir))
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
