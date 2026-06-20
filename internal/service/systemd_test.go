package service

import "testing"

func TestSystemdEscapeArg(t *testing.T) {
	got := systemdEscapeArg(`/tmp/flow guard/%n/"cfg"`)
	want := `"/tmp/flow guard/%%n/\"cfg\""`
	if got != want {
		t.Fatalf("systemdEscapeArg=%q, want %q", got, want)
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
