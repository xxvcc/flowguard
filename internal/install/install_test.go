package install

import (
	"testing"

	"flowguard/internal/config"
)

func TestApplyOptionValuesInitialTotal(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	got, err := applyOptionValues(cfg, Options{Allowance: "1000GB", InitialTotal: "123GB"})
	if err != nil {
		t.Fatal(err)
	}
	if got.InitialRXBytes != 123*1000*1000*1000 || got.InitialTXBytes != 0 {
		t.Fatalf("initial total mapped to rx=%d tx=%d", got.InitialRXBytes, got.InitialTXBytes)
	}
}

func TestApplyOptionValuesRejectsMixedInitialModes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	_, err := applyOptionValues(cfg, Options{Allowance: "1000GB", InitialTotal: "123GB", InitialRX: "1GB"})
	if err == nil {
		t.Fatal("expected mixed initial mode error")
	}
}

func TestApplyOptionValuesBillingMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	cfg.BillingMode = "outbound"
	got, err := applyOptionValues(cfg, Options{Allowance: "1000GB"})
	if err != nil {
		t.Fatal(err)
	}
	if got.BillingMode != "outbound" {
		t.Fatalf("billing mode=%s", got.BillingMode)
	}
}
