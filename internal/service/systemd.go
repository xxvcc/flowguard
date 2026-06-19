package service

import (
	"fmt"
	"os"
	"strings"
	"time"

	"flowguard/internal/util"
)

const systemdUnitPath = "/etc/systemd/system/flowguard.service"

func InstallSystemd(binaryPath string, configPath string, statePath string) error {
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
ReadWritePaths=/var/lib/flowguard
CapabilityBoundingSet=CAP_NET_ADMIN
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
`, binaryPath, configPath, statePath)
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

func Restart() error {
	if !util.CommandExists("systemctl") {
		return nil
	}
	_, err := util.Run(30*time.Second, "systemctl", "restart", "flowguard")
	if err != nil && strings.Contains(err.Error(), "not found") {
		return nil
	}
	return err
}
