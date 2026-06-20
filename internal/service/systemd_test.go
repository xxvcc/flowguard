package service

import "testing"

func TestSystemdEscapeArg(t *testing.T) {
	got := systemdEscapeArg(`/tmp/flow guard/%n/"cfg"`)
	want := `"/tmp/flow guard/%%n/\"cfg\""`
	if got != want {
		t.Fatalf("systemdEscapeArg=%q, want %q", got, want)
	}
}
