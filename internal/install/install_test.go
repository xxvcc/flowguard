package install

import (
	"bufio"
	"strings"
	"testing"

	"flowguard/internal/config"
)

func TestApplyOptionValuesInitialTotal(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	got, err := applyOptionValues(cfg, Options{Allowance: "1000GB", InitialTotal: "123GB", TGToken: "token", TGChatID: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if got.InitialRXBytes != 123*1000*1000*1000 || got.InitialTXBytes != 0 {
		t.Fatalf("initial total mapped to rx=%d tx=%d", got.InitialRXBytes, got.InitialTXBytes)
	}
	if !got.Telegram.Enabled || got.Telegram.BotToken != "token" || got.Telegram.ChatID != "chat" {
		t.Fatalf("telegram options not applied: %+v", got.Telegram)
	}
}

func TestApplyOptionValuesInitialTotalOutbound(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Interface = "eth0"
	cfg.BillingMode = "outbound"
	got, err := applyOptionValues(cfg, Options{Allowance: "1000GB", InitialTotal: "123GB"})
	if err != nil {
		t.Fatal(err)
	}
	if got.InitialRXBytes != 0 || got.InitialTXBytes != 123*1000*1000*1000 {
		t.Fatalf("outbound initial total mapped to rx=%d tx=%d", got.InitialRXBytes, got.InitialTXBytes)
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

func TestBuildConfigRejectsInvalidInterface(t *testing.T) {
	_, _, err := buildConfig(Options{Yes: true, Allowance: "1000GB"}, "bad/name")
	if err == nil {
		t.Fatal("expected invalid interface error")
	}
}

func TestPromptValueChoiceChineseLabelMapsToValue(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("2\n"))
	got := promptValueChoice(scanner, "计费模式", "total", billingModeChoices("zh"))
	if got != "outbound" {
		t.Fatalf("promptValueChoice=%q", got)
	}
}

func TestPromptBoolLocalized(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("2\n"))
	if promptBoolLocalized(scanner, "启用", true, "zh") {
		t.Fatal("expected Chinese no selection")
	}
}
