package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flowguard/internal/config"
	"flowguard/internal/util"
)

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath, "config path")
	statePath := fs.String("state", config.DefaultStatePath, "state path")
	ifaceValue := fs.String("interface", "", "interface, comma list, or auto-public for cleanup if config is unavailable")
	keepConfig := fs.Bool("keep-config", false, "keep config and state files")
	keepBinary := fs.Bool("keep-binary", false, "keep /usr/local/bin/flowguard")
	removeVnstat := fs.Bool("remove-vnstat", false, "remove configured interfaces from vnStat database")
	deleteCustomPaths := fs.Bool("delete-custom-paths", false, "allow uninstall to delete non-default --config/--state paths")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var cfg config.Config
	var haveConfig bool
	if loaded, err := config.Load(*cfgPath); err == nil {
		cfg = loaded
		haveConfig = true
	} else if *ifaceValue != "" {
		cfg = config.DefaultConfig()
		cfg.AllowanceBytes = 1
		interfaces, err := parseInterfaces(*ifaceValue)
		if err != nil {
			return err
		}
		cfg.Interfaces = interfaces
		if len(cfg.Interfaces) > 0 {
			cfg.Interface = cfg.Interfaces[0]
		}
		haveConfig = true
	}
	_, _ = util.Run(30*time.Second, "systemctl", "disable", "--now", "flowguard")
	_ = os.Remove("/etc/systemd/system/flowguard.service")
	_, _ = util.Run(30*time.Second, "systemctl", "daemon-reload")
	if haveConfig {
		if err := removeLimits(cfg); err != nil {
			if isZH(cfg) {
				fmt.Fprintf(os.Stderr, "限速清理警告：%v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "limit cleanup warning: %v\n", err)
			}
		}
		if *removeVnstat {
			for _, iface := range cfg.Interfaces {
				_, _ = util.Run(30*time.Second, "vnstat", "--remove", "-i", iface, "--force")
			}
		}
	}
	if !*keepConfig {
		removed, err := removeManagedFile(*cfgPath, config.DefaultConfigPath, *deleteCustomPaths)
		if err != nil {
			return err
		}
		if !removed {
			printSkippedCustomPath(cfg, haveConfig, *cfgPath)
		}
		removed, err = removeManagedFile(*statePath, config.DefaultStatePath, *deleteCustomPaths)
		if err != nil {
			return err
		}
		if !removed {
			printSkippedCustomPath(cfg, haveConfig, *statePath)
		}
	}
	if !*keepBinary {
		_ = os.Remove("/usr/local/bin/flowguard")
	}
	if haveConfig {
		if isZH(cfg) {
			fmt.Println("已清理配置网卡上的 FlowGuard 限速。")
		} else {
			fmt.Println("FlowGuard limits removed for configured interfaces.")
		}
	}
	if haveConfig && isZH(cfg) {
		fmt.Println("FlowGuard 已卸载。")
	} else {
		fmt.Println("FlowGuard uninstalled.")
	}
	return nil
}

func printBackup(cfg config.Config, backup string) {
	if isZH(cfg) {
		fmt.Printf("备份：%s\n", backup)
		return
	}
	fmt.Printf("Backup: %s\n", backup)
}

func isZH(cfg config.Config) bool {
	return cfg.Language == "" || cfg.Language == "zh"
}

func printSkippedCustomPath(cfg config.Config, haveConfig bool, path string) {
	if haveConfig && isZH(cfg) {
		fmt.Fprintf(os.Stderr, "跳过非默认路径：%s（如需删除请加 --delete-custom-paths=true）\n", path)
		return
	}
	fmt.Fprintf(os.Stderr, "Skipped non-default path: %s (use --delete-custom-paths=true to delete it)\n", path)
}

func removeManagedFile(path string, defaultPath string, allowCustom bool) (bool, error) {
	if path == "" {
		path = defaultPath
	}
	cleanPath := filepath.Clean(path)
	if cleanPath == "/" || cleanPath == "." {
		return false, fmt.Errorf("refusing to remove unsafe path %q", path)
	}
	cleanDefault := filepath.Clean(defaultPath)
	if cleanPath != cleanDefault && !allowCustom {
		return false, nil
	}
	if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	// Remove timestamped backups alongside the file. Config backups can contain
	// the Telegram bot token, so leaving them behind after uninstall would leak
	// the secret.
	backups, err := filepath.Glob(cleanPath + ".bak.*")
	if err != nil {
		return false, err
	}
	for _, backup := range backups {
		if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	if cleanPath == cleanDefault {
		if err := os.Remove(filepath.Dir(cleanPath)); err != nil && !os.IsNotExist(err) && !strings.Contains(err.Error(), "directory not empty") {
			return false, err
		}
	}
	return true, nil
}
