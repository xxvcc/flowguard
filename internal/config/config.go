package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const (
	DefaultConfigPath = "/etc/flowguard/config.json"
	DefaultStatePath  = "/var/lib/flowguard/state.json"
)

var interfaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

type Config struct {
	Interface            string     `json:"interface"`
	Interfaces           []string   `json:"interfaces"`
	AllowanceBytes       uint64     `json:"allowance_bytes"`
	PeriodDay            int        `json:"period_day"`
	BillingMode          string     `json:"billing_mode"`
	CheckIntervalSeconds int        `json:"check_interval_seconds"`
	InitialPeriod        string     `json:"initial_period"`
	InitialRXBytes       uint64     `json:"initial_rx_bytes"`
	InitialTXBytes       uint64     `json:"initial_tx_bytes"`
	Thresholds           Thresholds `json:"thresholds"`
	Limits               Limits     `json:"limits"`
	Safety               Safety     `json:"safety"`
	Telegram             Telegram   `json:"telegram"`
}

type Thresholds struct {
	WarnPercent      float64 `json:"warn_percent"`
	SoftPercent      float64 `json:"soft_percent"`
	HardPercent      float64 `json:"hard_percent"`
	WarnClearPercent float64 `json:"warn_clear_percent"`
	SoftClearPercent float64 `json:"soft_clear_percent"`
	HardClearPercent float64 `json:"hard_clear_percent"`
}

type Limits struct {
	SoftRate string `json:"soft_rate"`
	HardRate string `json:"hard_rate"`
}

type Telegram struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

type Safety struct {
	FirstLimitDryRun bool `json:"first_limit_dry_run"`
}

type State struct {
	Period            string `json:"period"`
	Level             string `json:"level"`
	LastNotifiedLevel string `json:"last_notified_level"`
	Limited           bool   `json:"limited"`
	CurrentLimitRate  string `json:"current_limit_rate"`
	LastRXBytes       uint64 `json:"last_rx_bytes"`
	LastTXBytes       uint64 `json:"last_tx_bytes"`
	LastTotalBytes    uint64 `json:"last_total_bytes"`
	LastBillableBytes uint64 `json:"last_billable_bytes"`
	FirstLimitSeen    bool   `json:"first_limit_seen"`
	UpdatedAt         string `json:"updated_at"`
}

func DefaultConfig() Config {
	return Config{
		PeriodDay:            1,
		BillingMode:          "total",
		CheckIntervalSeconds: 60,
		Thresholds: Thresholds{
			WarnPercent:      70,
			SoftPercent:      85,
			HardPercent:      95,
			WarnClearPercent: 65,
			SoftClearPercent: 80,
			HardClearPercent: 90,
		},
		Limits: Limits{
			SoftRate: "10mbit",
			HardRate: "1mbit",
		},
		Safety: Safety{FirstLimitDryRun: true},
	}
}

func DefaultState(period string) State {
	return State{
		Period:            period,
		Level:             "normal",
		LastNotifiedLevel: "normal",
		UpdatedAt:         time.Now().Format(time.RFC3339),
	}
}

func Load(path string) (Config, error) {
	if path == "" {
		path = DefaultConfigPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	cfg.Normalize()
	return cfg, cfg.Validate()
}

func (c *Config) Normalize() {
	if c.BillingMode == "" {
		c.BillingMode = "total"
	}
	if len(c.Interfaces) == 0 && c.Interface != "" {
		c.Interfaces = []string{c.Interface}
	}
	if c.Interface == "" && len(c.Interfaces) > 0 {
		c.Interface = c.Interfaces[0]
	}
	if c.PeriodDay == 0 {
		c.PeriodDay = 1
	}
	if c.CheckIntervalSeconds == 0 {
		c.CheckIntervalSeconds = 60
	}
	if c.Thresholds.WarnPercent == 0 {
		c.Thresholds.WarnPercent = 70
	}
	if c.Thresholds.SoftPercent == 0 {
		c.Thresholds.SoftPercent = 85
	}
	if c.Thresholds.HardPercent == 0 {
		c.Thresholds.HardPercent = 95
	}
	if c.Thresholds.WarnClearPercent == 0 {
		c.Thresholds.WarnClearPercent = 65
	}
	if c.Thresholds.SoftClearPercent == 0 {
		c.Thresholds.SoftClearPercent = 80
	}
	if c.Thresholds.HardClearPercent == 0 {
		c.Thresholds.HardClearPercent = 90
	}
	if c.Limits.SoftRate == "" {
		c.Limits.SoftRate = "10mbit"
	}
	if c.Limits.HardRate == "" {
		c.Limits.HardRate = "1mbit"
	}
}

func (c Config) Validate() error {
	if c.Interface == "" && len(c.Interfaces) == 0 {
		return errors.New("interface or interfaces is required")
	}
	for _, iface := range c.Interfaces {
		if iface == "" || !interfaceNamePattern.MatchString(iface) {
			return fmt.Errorf("invalid interface name %q", iface)
		}
	}
	if c.AllowanceBytes == 0 {
		return errors.New("allowance_bytes is required")
	}
	if c.PeriodDay < 1 || c.PeriodDay > 28 {
		return errors.New("period_day must be between 1 and 28")
	}
	if c.BillingMode != "total" && c.BillingMode != "outbound" {
		return errors.New("billing_mode must be total or outbound")
	}
	if c.CheckIntervalSeconds < 10 {
		return errors.New("check_interval_seconds must be >= 10")
	}
	if c.Thresholds.WarnPercent <= 0 || c.Thresholds.SoftPercent <= c.Thresholds.WarnPercent || c.Thresholds.HardPercent <= c.Thresholds.SoftPercent {
		return errors.New("thresholds must satisfy 0 < warn < soft < hard")
	}
	if c.Thresholds.WarnClearPercent > c.Thresholds.WarnPercent || c.Thresholds.SoftClearPercent > c.Thresholds.SoftPercent || c.Thresholds.HardClearPercent > c.Thresholds.HardPercent {
		return errors.New("clear thresholds must be <= matching trigger thresholds")
	}
	if c.Limits.SoftRate == "" || c.Limits.HardRate == "" {
		return errors.New("soft_rate and hard_rate are required")
	}
	if c.Telegram.Enabled && (c.Telegram.BotToken == "" || c.Telegram.ChatID == "") {
		return errors.New("telegram bot_token and chat_id are required when telegram is enabled")
	}
	return nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultConfigPath
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	return writeJSONAtomic(path, cfg, 0600)
}

func Backup(path string) (string, error) {
	if path == "" {
		path = DefaultConfigPath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	backup := fmt.Sprintf("%s.bak.%s", path, time.Now().Format("20060102-150405.000000000"))
	if err := os.WriteFile(backup, data, 0600); err != nil {
		return "", err
	}
	return backup, nil
}

func LoadState(path string, period string) (State, error) {
	if path == "" {
		path = DefaultStatePath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultState(period), nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	if state.Period == "" {
		state.Period = period
	}
	if state.Level == "" {
		state.Level = "normal"
	}
	if state.LastNotifiedLevel == "" {
		state.LastNotifiedLevel = "normal"
	}
	return state, nil
}

func SaveState(path string, state State) error {
	if path == "" {
		path = DefaultStatePath
	}
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	return writeJSONAtomic(path, state, 0600)
}

func writeJSONAtomic(path string, value any, perm os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
