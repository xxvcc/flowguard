package service

import (
	"strings"
	"testing"
)

func TestSystemdEscapeArg(t *testing.T) {
	got := systemdEscapeArg(`/tmp/flow guard/%n/"cfg"`)
	want := `"/tmp/flow guard/%%n/\"cfg\""`
	if got != want {
		t.Fatalf("systemdEscapeArg=%q, want %q", got, want)
	}
}

func TestRenderSystemdUnitIncludesConfigReadOnlyPath(t *testing.T) {
	unit := renderSystemdUnit(`/opt/flow guard/flowguard`, `/etc/flow guard/config.json`, `/var/lib/flowguard/state.json`)
	if !strings.Contains(unit, `ReadOnlyPaths="/etc/flow guard"`) {
		t.Fatalf("unit missing config read-only path: %s", unit)
	}
	if !strings.Contains(unit, `ReadWritePaths="/var/lib/flowguard"`) {
		t.Fatalf("unit missing state write path: %s", unit)
	}
	if !strings.Contains(unit, `ExecStart="/opt/flow guard/flowguard" run --config "/etc/flow guard/config.json" --state "/var/lib/flowguard/state.json"`) {
		t.Fatalf("unit missing escaped ExecStart: %s", unit)
	}
}

func TestValidateSystemdPathsRejectsBroadStateDir(t *testing.T) {
	if err := validateSystemdPaths("/usr/local/bin/flowguard", "/etc/flowguard/config.json", "/var/lib/state.json"); err == nil {
		t.Fatal("expected broad state directory to be rejected")
	}
	if err := validateSystemdPaths("/usr/local/bin/flowguard", "/etc/flowguard/config.json", "/var/lib/flowguard/state.json"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSystemdPathsRejectsRootConfigDir(t *testing.T) {
	if err := validateSystemdPaths("/usr/local/bin/flowguard", "/config.json", "/var/lib/flowguard/state.json"); err == nil {
		t.Fatal("expected root config directory to be rejected")
	}
}

func TestValidateSystemdPathsRejectsTmpPaths(t *testing.T) {
	tests := []struct {
		binary string
		config string
		state  string
	}{
		{"/tmp/flowguard/bin/flowguard", "/etc/flowguard/config.json", "/var/lib/flowguard/state.json"},
		{"/usr/local/bin/flowguard", "/tmp/flowguard/config.json", "/var/lib/flowguard/state.json"},
		{"/usr/local/bin/flowguard", "/etc/flowguard/config.json", "/tmp/flowguard/state.json"},
	}
	for _, tt := range tests {
		if err := validateSystemdPaths(tt.binary, tt.config, tt.state); err == nil {
			t.Fatalf("expected /tmp path to be rejected: %+v", tt)
		}
	}
}

func TestUnitListContainsFlowGuard(t *testing.T) {
	if !unitListContainsFlowGuard("flowguard.service enabled enabled\n") {
		t.Fatal("expected flowguard service to be detected")
	}
	if unitListContainsFlowGuard("other.service enabled enabled\n") {
		t.Fatal("unexpected flowguard service detection")
	}
	if unitListContainsFlowGuard("") {
		t.Fatal("empty output should mean missing unit")
	}
}
