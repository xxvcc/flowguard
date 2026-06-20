package util

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	colorReset = "\033[0m"
	colorCyan  = "\033[36m"
	colorGreen = "\033[32m"
	colorDim   = "\033[2m"
)

type CmdResult struct {
	Stdout string
	Stderr string
}

func CommandExists(name string) bool {
	_, err := ResolveCommand(name)
	return err == nil
}

func ResolveCommand(name string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		return name, nil
	}
	if os.Getenv("FLOWGUARD_TEST_PATH") == "1" {
		return exec.LookPath(name)
	}
	for _, dir := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin", "/usr/local/sbin", "/usr/local/bin"} {
		candidate := dir + string(os.PathSeparator) + name
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func Run(timeout time.Duration, name string, args ...string) (CmdResult, error) {
	ctx, cancel := contextWithTimeout(timeout)
	defer cancel()
	path, err := ResolveCommand(name)
	if err != nil {
		return CmdResult{}, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	result := CmdResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() != nil {
		return result, fmt.Errorf("%s timed out", name)
	}
	if err != nil {
		return result, fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(result.Stderr))
	}
	return result, nil
}

func contextWithTimeout(timeout time.Duration) (ctx context.Context, cancel context.CancelFunc) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return context.WithTimeout(context.Background(), timeout)
}

func IsRoot() bool {
	return os.Geteuid() == 0
}

func DetectDefaultInterface() (string, error) {
	if CommandExists("ip") {
		out, err := Run(10*time.Second, "ip", "route", "show", "default")
		if err == nil {
			fields := strings.Fields(out.Stdout)
			for i := 0; i < len(fields)-1; i++ {
				if fields[i] == "dev" {
					return fields[i+1], nil
				}
			}
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
			return iface.Name, nil
		}
	}
	return "", errors.New("unable to detect default network interface")
}

func DetectPublicInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if strings.HasPrefix(iface.Name, "docker") || strings.HasPrefix(iface.Name, "veth") || strings.HasPrefix(iface.Name, "br-") || strings.HasPrefix(iface.Name, "virbr") || strings.HasPrefix(iface.Name, "lo") {
			continue
		}
		names = append(names, iface.Name)
	}
	if len(names) == 0 {
		name, err := DetectDefaultInterface()
		if err != nil {
			return nil, err
		}
		return []string{name}, nil
	}
	return names, nil
}

func ParseBytes(input string) (uint64, error) {
	s := strings.TrimSpace(strings.ToLower(input))
	if s == "" {
		return 0, nil
	}
	s = strings.ReplaceAll(s, " ", "")
	units := []struct {
		suffix string
		mult   float64
	}{
		{"tib", 1024 * 1024 * 1024 * 1024},
		{"tb", 1000 * 1000 * 1000 * 1000},
		{"gib", 1024 * 1024 * 1024},
		{"gb", 1000 * 1000 * 1000},
		{"mib", 1024 * 1024},
		{"mb", 1000 * 1000},
		{"kib", 1024},
		{"kb", 1000},
		{"b", 1},
	}
	mult := float64(1)
	num := s
	for _, unit := range units {
		if strings.HasSuffix(s, unit.suffix) {
			mult = unit.mult
			num = strings.TrimSuffix(s, unit.suffix)
			break
		}
	}
	value, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte value %q", input)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("byte value must be finite")
	}
	if value < 0 {
		return 0, fmt.Errorf("byte value must be non-negative")
	}
	if value > float64(^uint64(0))/mult {
		return 0, fmt.Errorf("byte value is too large")
	}
	return uint64(value * mult), nil
}

func FormatBytes(bytes uint64) string {
	const gb = 1000 * 1000 * 1000
	const mb = 1000 * 1000
	if bytes >= gb {
		return fmt.Sprintf("%.2f GB", float64(bytes)/gb)
	}
	if bytes >= mb {
		return fmt.Sprintf("%.2f MB", float64(bytes)/mb)
	}
	return fmt.Sprintf("%d B", bytes)
}

func Prompt(scanner *bufio.Scanner, label string, defaultValue string) string {
	prefix := colorize(colorCyan, "?")
	if defaultValue != "" {
		fmt.Printf("%s %s %s: ", prefix, label, colorize(colorDim, "["+defaultValue+"]"))
	} else {
		fmt.Printf("%s %s: ", prefix, label)
	}
	if !scanner.Scan() {
		return defaultValue
	}
	value := strings.TrimSpace(scanner.Text())
	if value == "" {
		return defaultValue
	}
	return value
}

func PromptBool(scanner *bufio.Scanner, label string, defaultValue bool) bool {
	defaultChoice := "no"
	if defaultValue {
		defaultChoice = "yes"
	}
	return PromptChoice(scanner, label, defaultChoice, []string{"yes", "no"}) == "yes"
}

func PromptChoice(scanner *bufio.Scanner, label string, defaultValue string, choices []string) string {
	if isInteractiveTerminal() {
		if selected, ok := promptChoiceArrow(label, defaultValue, choices); ok {
			return selected
		}
	}
	allowed := map[string]string{}
	defaultIndex := 0
	for i, choice := range choices {
		index := strconv.Itoa(i + 1)
		allowed[index] = choice
		allowed[strings.ToLower(choice)] = choice
		if choice == defaultValue {
			defaultIndex = i + 1
		}
	}
	for {
		fmt.Printf("%s %s\n", colorize(colorCyan, "?"), label)
		for i, choice := range choices {
			marker := colorize(colorDim, "○")
			if i+1 == defaultIndex {
				marker = colorize(colorGreen, "●")
			}
			fmt.Printf("  %s %d) %s\n", marker, i+1, choice)
		}
		answer := strings.ToLower(Prompt(scanner, "Choose / 选择", strconv.Itoa(defaultIndex)))
		if selected, ok := allowed[answer]; ok {
			return selected
		}
		fmt.Printf("Please choose / 请选择 1-%d.\n", len(choices))
	}
}

func promptChoiceArrow(label string, defaultValue string, choices []string) (string, bool) {
	if len(choices) == 0 {
		return "", false
	}
	selected := 0
	for i, choice := range choices {
		if choice == defaultValue {
			selected = i
			break
		}
	}
	model := choiceModel{label: label, choices: choices, selected: selected}
	program := tea.NewProgram(model)
	result, err := program.Run()
	if err != nil {
		return "", false
	}
	finalModel, ok := result.(choiceModel)
	if !ok || finalModel.canceled {
		return "", false
	}
	return choices[finalModel.selected], true
}

type choiceModel struct {
	label    string
	choices  []string
	selected int
	canceled bool
}

func (m choiceModel) Init() tea.Cmd { return nil }

func (m choiceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.canceled = true
			return m, tea.Quit
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(m.choices)-1 {
				m.selected++
			}
		case "enter":
			return m, tea.Quit
		default:
			if len(msg.String()) == 1 && msg.String()[0] >= '1' && msg.String()[0] <= '9' {
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < len(m.choices) {
					m.selected = idx
				}
			}
		}
	}
	return m, nil
}

func (m choiceModel) View() string {
	questionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	dimStyle := lipgloss.NewStyle().Faint(true)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s\n", questionStyle.Render("?"), m.label, dimStyle.Render("(↑/↓, Enter)"))
	for i, choice := range m.choices {
		marker := dimStyle.Render("○")
		if i == m.selected {
			marker = selectedStyle.Render("●")
		}
		fmt.Fprintf(&b, "  %s %s\n", marker, choice)
	}
	return b.String()
}

func isInteractiveTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func colorize(color string, text string) string {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return text
	}
	return color + text + colorReset
}
