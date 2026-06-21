package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	DefaultConfigPath   = "/etc/flowguard/config.json"
	DefaultStatePath    = "/var/lib/flowguard/state.json"
	ConfigSchemaVersion = 1
	MaxConfigBackups    = 10
	LevelNormal         = "normal"
	LevelWarn           = "warn"
	LevelSoft           = "soft_limit"
	LevelHard           = "hard_limit"
)

var interfaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

func ValidInterfaceName(iface string) bool {
	return iface != "" && interfaceNamePattern.MatchString(iface)
}

type Config struct {
	SchemaVersion        int        `json:"schema_version"`
	Interface            string     `json:"interface"`
	Interfaces           []string   `json:"interfaces"`
	AllowanceBytes       uint64     `json:"allowance_bytes"`
	Language             string     `json:"language,omitempty"`
	PeriodDay            int        `json:"period_day"`
	BillingMode          string     `json:"billing_mode"`
	CheckIntervalSeconds int        `json:"check_interval_seconds"`
	InitialPeriod        string     `json:"initial_period"`
	InitialRXBytes       uint64     `json:"initial_rx_bytes"`
	InitialTXBytes       uint64     `json:"initial_tx_bytes"`
	BaselineRXBytes      uint64     `json:"baseline_rx_bytes,omitempty"`
	BaselineTXBytes      uint64     `json:"baseline_tx_bytes,omitempty"`
	BaselineAt           string     `json:"baseline_at,omitempty"`
	BaselineDayRXBytes   uint64     `json:"baseline_day_rx_bytes,omitempty"`
	BaselineDayTXBytes   uint64     `json:"baseline_day_tx_bytes,omitempty"`
	BaselineWeekRXBytes  uint64     `json:"baseline_week_rx_bytes,omitempty"`
	BaselineWeekTXBytes  uint64     `json:"baseline_week_tx_bytes,omitempty"`
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
	LastNotifiedKey   string `json:"last_notified_key,omitempty"`
	Limited           bool   `json:"limited"`
	CurrentLimitRate  string `json:"current_limit_rate"`
	AppliedLimitKey   string `json:"applied_limit_key,omitempty"`
	LastRXBytes       uint64 `json:"last_rx_bytes"`
	LastTXBytes       uint64 `json:"last_tx_bytes"`
	LastTotalBytes    uint64 `json:"last_total_bytes"`
	LastBillableBytes uint64 `json:"last_billable_bytes"`
	LastErrorKey      string `json:"last_error_key,omitempty"`
	LastErrorAt       string `json:"last_error_at,omitempty"`
	FirstLimitSeen    bool   `json:"first_limit_seen"`
	UpdatedAt         string `json:"updated_at"`
}

func DefaultConfig() Config {
	return Config{
		SchemaVersion:        ConfigSchemaVersion,
		PeriodDay:            1,
		Language:             "zh",
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
		Level:             LevelNormal,
		LastNotifiedLevel: LevelNormal,
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
	applyMissingThresholdDefaults(data, &cfg)
	cfg.Normalize()
	return cfg, cfg.Validate()
}

func applyMissingThresholdDefaults(data []byte, cfg *Config) {
	var raw struct {
		Thresholds map[string]json.RawMessage `json:"thresholds"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	defaults := DefaultConfig().Thresholds
	if raw.Thresholds == nil {
		cfg.Thresholds.WarnClearPercent = defaults.WarnClearPercent
		cfg.Thresholds.SoftClearPercent = defaults.SoftClearPercent
		cfg.Thresholds.HardClearPercent = defaults.HardClearPercent
		return
	}
	if _, ok := raw.Thresholds["warn_clear_percent"]; !ok {
		cfg.Thresholds.WarnClearPercent = defaults.WarnClearPercent
	}
	if _, ok := raw.Thresholds["soft_clear_percent"]; !ok {
		cfg.Thresholds.SoftClearPercent = defaults.SoftClearPercent
	}
	if _, ok := raw.Thresholds["hard_clear_percent"]; !ok {
		cfg.Thresholds.HardClearPercent = defaults.HardClearPercent
	}
}

func (c *Config) Normalize() {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = ConfigSchemaVersion
	}
	if c.BillingMode == "" {
		c.BillingMode = "total"
	}
	if c.Language == "" {
		c.Language = "zh"
	}
	if len(c.Interfaces) == 0 && c.Interface != "" {
		c.Interfaces = []string{c.Interface}
	}
	c.Interfaces = normalizeInterfaces(c.Interfaces)
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
	if c.Limits.SoftRate == "" {
		c.Limits.SoftRate = "10mbit"
	}
	if c.Limits.HardRate == "" {
		c.Limits.HardRate = "1mbit"
	}
}

func normalizeInterfaces(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func (c Config) Validate() error {
	if c.Interface == "" && len(c.Interfaces) == 0 {
		return errors.New("interface or interfaces is required")
	}
	if c.Interface != "" && !ValidInterfaceName(c.Interface) {
		return fmt.Errorf("invalid interface name %q", c.Interface)
	}
	for _, iface := range c.Interfaces {
		if !ValidInterfaceName(iface) {
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
	if c.Language != "zh" && c.Language != "en" {
		return errors.New("language must be zh or en")
	}
	if c.CheckIntervalSeconds < 10 {
		return errors.New("check_interval_seconds must be >= 10")
	}
	if !validPercent(c.Thresholds.WarnPercent) || !validPercent(c.Thresholds.SoftPercent) || !validPercent(c.Thresholds.HardPercent) || c.Thresholds.SoftPercent <= c.Thresholds.WarnPercent || c.Thresholds.HardPercent <= c.Thresholds.SoftPercent {
		return errors.New("thresholds must satisfy 0 < warn < soft < hard <= 100")
	}
	if !validClearPercent(c.Thresholds.WarnClearPercent) || !validClearPercent(c.Thresholds.SoftClearPercent) || !validClearPercent(c.Thresholds.HardClearPercent) || c.Thresholds.WarnClearPercent > c.Thresholds.WarnPercent || c.Thresholds.SoftClearPercent > c.Thresholds.SoftPercent || c.Thresholds.HardClearPercent > c.Thresholds.HardPercent {
		return errors.New("clear thresholds must be finite percentages between 0 and their matching trigger thresholds")
	}
	if c.Limits.SoftRate == "" || c.Limits.HardRate == "" {
		return errors.New("soft_rate and hard_rate are required")
	}
	if c.Telegram.Enabled && (c.Telegram.BotToken == "" || c.Telegram.ChatID == "") {
		return errors.New("telegram bot_token and chat_id are required when telegram is enabled")
	}
	return nil
}

func validPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value <= 100
}

func validClearPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
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
	if err := writeFileAtomic(backup, data, 0600); err != nil {
		return "", err
	}
	if err := pruneBackups(path, MaxConfigBackups); err != nil {
		return "", err
	}
	return backup, nil
}

func pruneBackups(path string, keep int) error {
	if keep <= 0 {
		return nil
	}
	matches, err := filepath.Glob(path + ".bak.*")
	if err != nil {
		return err
	}
	if len(matches) <= keep {
		return nil
	}
	sort.Strings(matches)
	for _, oldBackup := range matches[:len(matches)-keep] {
		if err := os.Remove(oldBackup); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
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
	if !validLevel(state.Level) {
		state.Level = LevelNormal
	}
	if !validLevel(state.LastNotifiedLevel) {
		state.LastNotifiedLevel = LevelNormal
	}
	return state, nil
}

func validLevel(level string) bool {
	switch level {
	case LevelNormal, LevelWarn, LevelSoft, LevelHard:
		return true
	default:
		return false
	}
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
	return writeFileAtomic(path, data, perm)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := ensureSafeFileTarget(path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ensureSafeFileTarget(path); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func ensureSafeFileTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular file %s", path)
	}
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
