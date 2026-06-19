package traffic

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/util"
)

const (
	LevelNormal = "normal"
	LevelWarn   = "warn"
	LevelSoft   = "soft_limit"
	LevelHard   = "hard_limit"
)

type Usage struct {
	Period        string   `json:"period"`
	Interfaces    []string `json:"interfaces"`
	RXBytes       uint64   `json:"rx_bytes"`
	TXBytes       uint64   `json:"tx_bytes"`
	TotalBytes    uint64   `json:"total_bytes"`
	BillableBytes uint64   `json:"billable_bytes"`
	Percent       float64  `json:"percent"`
}

type Decision struct {
	Level     string `json:"level"`
	LimitRate string `json:"limit_rate"`
}

type vnstatJSON struct {
	Interfaces []vnstatInterface `json:"interfaces"`
}

type vnstatInterface struct {
	Name    string        `json:"name"`
	Traffic vnstatTraffic `json:"traffic"`
}

type vnstatTraffic struct {
	Total  vnstatTotal    `json:"total"`
	Days   []vnstatPeriod `json:"day"`
	Months []vnstatPeriod `json:"month"`
}

type vnstatTotal struct {
	RX uint64 `json:"rx"`
	TX uint64 `json:"tx"`
}

type vnstatPeriod struct {
	Date struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	} `json:"date"`
	RX uint64 `json:"rx"`
	TX uint64 `json:"tx"`
}

func CurrentPeriod(now time.Time, periodDay int) string {
	start := CurrentPeriodStart(now, periodDay)
	return fmt.Sprintf("%04d-%02d", start.Year(), int(start.Month()))
}

func CurrentPeriodStart(now time.Time, periodDay int) time.Time {
	if periodDay < 1 || periodDay > 28 {
		periodDay = 1
	}
	year, month, day := now.Date()
	if day < periodDay {
		month--
		if month == 0 {
			month = 12
			year--
		}
	}
	return time.Date(year, month, periodDay, 0, 0, 0, 0, now.Location())
}

func ReadUsage(cfg config.Config, now time.Time) (Usage, error) {
	result, err := util.Run(30*time.Second, "vnstat", "--json")
	if err != nil {
		return Usage{}, err
	}
	var parsed vnstatJSON
	if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
		return Usage{}, err
	}
	period := CurrentPeriod(now, cfg.PeriodDay)
	var rx uint64
	var tx uint64
	for _, iface := range cfg.Interfaces {
		ifaceRX, ifaceTX, err := findUsage(parsed, iface, period, CurrentPeriodStart(now, cfg.PeriodDay), now, cfg.PeriodDay)
		if err != nil {
			return Usage{}, err
		}
		rx = saturatingAdd(rx, ifaceRX)
		tx = saturatingAdd(tx, ifaceTX)
	}
	if cfg.InitialPeriod == period {
		rx = saturatingAdd(rx, cfg.InitialRXBytes)
		tx = saturatingAdd(tx, cfg.InitialTXBytes)
	}
	total := saturatingAdd(rx, tx)
	billable := total
	if cfg.BillingMode == "outbound" {
		billable = tx
	}
	return Usage{
		Period:        period,
		Interfaces:    cfg.Interfaces,
		RXBytes:       rx,
		TXBytes:       tx,
		TotalBytes:    total,
		BillableBytes: billable,
		Percent:       float64(billable) * 100 / float64(cfg.AllowanceBytes),
	}, nil
}

func saturatingAdd(left uint64, right uint64) uint64 {
	if ^uint64(0)-left < right {
		return ^uint64(0)
	}
	return left + right
}

func findUsage(data vnstatJSON, iface string, period string, start time.Time, now time.Time, periodDay int) (uint64, uint64, error) {
	for _, item := range data.Interfaces {
		if item.Name != iface {
			continue
		}
		if periodDay != 1 {
			return sumDays(item.Traffic.Days, start, now)
		}
		for _, month := range item.Traffic.Months {
			key := fmt.Sprintf("%04d-%02d", month.Date.Year, month.Date.Month)
			if key == period {
				return month.RX, month.TX, nil
			}
		}
		return item.Traffic.Total.RX, item.Traffic.Total.TX, nil
	}
	return 0, 0, fmt.Errorf("interface %q not found in vnstat output", iface)
}

func findMonth(data vnstatJSON, iface string, period string) (uint64, uint64, error) {
	return findUsage(data, iface, period, time.Time{}, time.Time{}, 1)
}

func sumDays(days []vnstatPeriod, start time.Time, now time.Time) (uint64, uint64, error) {
	if len(days) == 0 {
		return 0, 0, fmt.Errorf("vnStat daily data is empty; custom billing period cannot be calculated")
	}
	var rx uint64
	var tx uint64
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, day := range days {
		if day.Date.Year == 0 || day.Date.Month == 0 || day.Date.Day == 0 {
			continue
		}
		date := time.Date(day.Date.Year, time.Month(day.Date.Month), day.Date.Day, 0, 0, 0, 0, now.Location())
		if date.Before(start) || date.After(end) {
			continue
		}
		rx = saturatingAdd(rx, day.RX)
		tx = saturatingAdd(tx, day.TX)
	}
	return rx, tx, nil
}

func Decide(cfg config.Config, usage Usage) Decision {
	return decideAt(cfg, usage.Percent, "")
}

func DecideWithState(cfg config.Config, usage Usage, state config.State) Decision {
	return decideAt(cfg, usage.Percent, state.Level)
}

func decideAt(cfg config.Config, percent float64, currentLevel string) Decision {
	if percent >= cfg.Thresholds.HardPercent || (currentLevel == LevelHard && percent >= cfg.Thresholds.HardClearPercent) {
		return Decision{Level: LevelHard, LimitRate: cfg.Limits.HardRate}
	}
	if percent >= cfg.Thresholds.SoftPercent || (currentLevel == LevelSoft && percent >= cfg.Thresholds.SoftClearPercent) {
		return Decision{Level: LevelSoft, LimitRate: cfg.Limits.SoftRate}
	}
	if percent >= cfg.Thresholds.WarnPercent || (currentLevel == LevelWarn && percent >= cfg.Thresholds.WarnClearPercent) {
		return Decision{Level: LevelWarn}
	}
	return Decision{Level: LevelNormal}
}

func ApplyLimit(iface string, rate string) error {
	if strings.TrimSpace(rate) == "" {
		return fmt.Errorf("rate is required")
	}
	_, err := util.Run(30*time.Second, "tc", "qdisc", "replace", "dev", iface, "root", "tbf", "rate", rate, "burst", "32kbit", "latency", "400ms")
	return err
}

func RemoveLimit(iface string) error {
	current := CurrentLimit(iface)
	if current != "unknown" && !strings.HasPrefix(current, "qdisc tbf ") {
		return nil
	}
	_, err := util.Run(30*time.Second, "tc", "qdisc", "del", "dev", iface, "root")
	if err != nil && (strings.Contains(err.Error(), "No such file") || strings.Contains(err.Error(), "Invalid argument") || strings.Contains(err.Error(), "Cannot delete")) {
		return nil
	}
	return err
}

func CurrentLimit(iface string) string {
	result, err := util.Run(30*time.Second, "tc", "qdisc", "show", "dev", iface)
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(result.Stdout)
}

func UpdateState(state config.State, usage Usage, decision Decision) config.State {
	state.Period = usage.Period
	state.Level = decision.Level
	state.LastRXBytes = usage.RXBytes
	state.LastTXBytes = usage.TXBytes
	state.LastTotalBytes = usage.TotalBytes
	state.LastBillableBytes = usage.BillableBytes
	state.Limited = decision.LimitRate != ""
	state.CurrentLimitRate = decision.LimitRate
	return state
}
