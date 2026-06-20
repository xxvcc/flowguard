package util

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseBytes(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"", 0},
		{"0", 0},
		{"1", 1},
		{"1B", 1},
		{"1KB", 1000},
		{"1MB", 1000 * 1000},
		{"1GB", 1000 * 1000 * 1000},
		{"1.5GB", 1500 * 1000 * 1000},
		{"1GiB", 1024 * 1024 * 1024},
	}
	for _, tt := range tests {
		got, err := ParseBytes(tt.input)
		if err != nil {
			t.Fatalf("ParseBytes(%q) returned error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("ParseBytes(%q)=%d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseBytesRejectsInvalid(t *testing.T) {
	for _, input := range []string{"abc", "-1GB", "1e309GB", "999999999999999999999999999999999999999999999999999999GB"} {
		if _, err := ParseBytes(input); err == nil {
			t.Fatalf("ParseBytes(%q) expected error", input)
		}
	}
}

func TestPromptChoiceNumericFallback(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("2\n"))
	got := PromptChoice(scanner, "Choose", "one", []string{"one", "two"})
	if got != "two" {
		t.Fatalf("PromptChoice=%q, want two", got)
	}
}
