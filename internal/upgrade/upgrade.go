package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"flowguard/internal/service"
	"flowguard/internal/util"
)

const (
	DefaultRepo       = "xxvcc/flowguard"
	DefaultInstallDir = "/usr/local/bin"
	BinaryName        = "flowguard"
	maxDownloadBytes  = 100 * 1024 * 1024
	maxBinaryBytes    = 100 * 1024 * 1024
)

var (
	repoPattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

type Options struct {
	Repo           string
	Version        string
	BaseURL        string
	InstallDir     string
	MinisignPubKey string
	NoRestart      bool
	DryRun         bool
	HTTPClient     *http.Client
}

type Plan struct {
	Version      string
	Asset        string
	AssetURL     string
	ChecksumURL  string
	SignatureURL string
	InstallPath  string
}

func Run(opts Options) error {
	plan, err := BuildPlan(opts)
	if err != nil {
		return err
	}
	if opts.DryRun {
		fmt.Printf("Would download: %s\n", plan.AssetURL)
		fmt.Printf("Would verify checksum: %s\n", plan.ChecksumURL)
		if opts.MinisignPubKey != "" {
			fmt.Printf("Would verify signature: %s\n", plan.SignatureURL)
		}
		fmt.Printf("Would install: %s\n", plan.InstallPath)
		return nil
	}
	if !util.IsRoot() {
		return fmt.Errorf("upgrade must be run as root, try: sudo flowguard upgrade")
	}
	if _, err := os.Stat(plan.InstallPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("existing binary not found at %s; run flowguard install first", plan.InstallPath)
		}
		return err
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	tmpDir, err := os.MkdirTemp("", "flowguard-upgrade-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	assetPath := filepath.Join(tmpDir, plan.Asset)
	checksumPath := filepath.Join(tmpDir, "checksums.txt")
	if err := download(client, plan.AssetURL, assetPath); err != nil {
		return err
	}
	if err := download(client, plan.ChecksumURL, checksumPath); err != nil {
		return err
	}
	if opts.MinisignPubKey != "" {
		signaturePath := filepath.Join(tmpDir, "checksums.txt.minisig")
		if err := download(client, plan.SignatureURL, signaturePath); err != nil {
			return err
		}
		if err := verifyMinisign(checksumPath, signaturePath, opts.MinisignPubKey); err != nil {
			return err
		}
	}
	want, err := findChecksum(checksumPath, plan.Asset)
	if err != nil {
		return err
	}
	if err := verifySHA256(assetPath, want); err != nil {
		return err
	}
	extractedPath := filepath.Join(tmpDir, BinaryName)
	if err := extractBinary(assetPath, extractedPath); err != nil {
		return err
	}
	if err := installBinary(extractedPath, plan.InstallPath); err != nil {
		return err
	}
	fmt.Printf("Upgraded %s to %s\n", plan.InstallPath, plan.Version)
	if !opts.NoRestart {
		if err := service.Restart(); err != nil {
			if restoreErr := restoreBackup(plan.InstallPath); restoreErr != nil {
				return fmt.Errorf("restart flowguard service: %w; rollback failed: %v", err, restoreErr)
			}
			if restartErr := service.Restart(); restartErr != nil {
				return fmt.Errorf("restart flowguard service: %w; rolled back but restarting backup failed: %v", err, restartErr)
			}
			return fmt.Errorf("restart flowguard service: %w", err)
		}
		fmt.Println("Restarted flowguard service if available.")
	}
	return nil
}

func BuildPlan(opts Options) (Plan, error) {
	repo := opts.Repo
	if repo == "" {
		repo = DefaultRepo
	}
	if !validRepo(repo) {
		return Plan{}, fmt.Errorf("invalid repo %q, expected owner/name", repo)
	}
	version := opts.Version
	if version == "" {
		version = "latest"
	}
	if version != "latest" && !versionPattern.MatchString(version) {
		return Plan{}, fmt.Errorf("invalid version %q", version)
	}
	installDir := opts.InstallDir
	if installDir == "" {
		installDir = DefaultInstallDir
	}
	osName, arch, err := platformAssetParts(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Plan{}, err
	}
	asset := fmt.Sprintf("flowguard_%s_%s.tar.gz", osName, arch)
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		if version == "latest" {
			baseURL = "https://github.com/" + repo + "/releases/latest/download"
		} else {
			baseURL = "https://github.com/" + repo + "/releases/download/" + version
		}
	}
	if err := validateBaseURL(baseURL); err != nil {
		return Plan{}, err
	}
	return Plan{
		Version:      version,
		Asset:        asset,
		AssetURL:     baseURL + "/" + asset,
		ChecksumURL:  baseURL + "/checksums.txt",
		SignatureURL: baseURL + "/checksums.txt.minisig",
		InstallPath:  filepath.Join(installDir, BinaryName),
	}, nil
}

func validRepo(repo string) bool {
	if !repoPattern.MatchString(repo) {
		return false
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validateBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("release asset base URL must not contain query or fragment")
	}
	if parsed.Scheme == "https" && parsed.Host != "" {
		return nil
	}
	if parsed.Scheme == "http" && isLocalHost(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf("release asset URL must be https, except localhost test mirrors")
}

func isLocalHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func platformAssetParts(goos string, goarch string) (string, string, error) {
	if goos != "linux" {
		return "", "", fmt.Errorf("unsupported OS: %s", goos)
	}
	switch goarch {
	case "amd64":
		return "linux", "amd64", nil
	case "arm64":
		return "linux", "arm64", nil
	case "arm":
		return "linux", "armv7", nil
	default:
		return "", "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
}

func download(client *http.Client, url string, path string) error {
	if err := validateDownloadURL(url); err != nil {
		return err
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := validateDownloadURL(resp.Request.URL.String()); err != nil {
		return fmt.Errorf("download redirected to unsafe URL: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s returned HTTP %d", url, resp.StatusCode)
	}
	if resp.ContentLength > maxDownloadBytes {
		return fmt.Errorf("download %s is too large", url)
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	reader := io.LimitReader(resp.Body, maxDownloadBytes+1)
	written, err := io.Copy(out, reader)
	if err != nil {
		return err
	}
	if written > maxDownloadBytes {
		return fmt.Errorf("download %s exceeded size limit", url)
	}
	return out.Sync()
}

func validateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme == "https" && parsed.Host != "" {
		return nil
	}
	if parsed.Scheme == "http" && isLocalHost(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf("download URL must be https, except localhost test mirrors")
}

func findChecksum(path string, asset string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != 64 {
				return "", fmt.Errorf("invalid checksum for %s", asset)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", asset)
}

func verifyMinisign(messagePath string, signaturePath string, publicKey string) error {
	if strings.TrimSpace(publicKey) == "" {
		return fmt.Errorf("minisign public key is required")
	}
	if _, err := util.Run(30*time.Second, "minisign", "-Vm", messagePath, "-x", signaturePath, "-P", publicKey); err != nil {
		return fmt.Errorf("verify minisign signature: %w", err)
	}
	return nil
}

func verifySHA256(path string, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != strings.ToLower(want) {
		return fmt.Errorf("checksum mismatch for %s", filepath.Base(path))
	}
	return nil
}

func extractBinary(archivePath string, outPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != BinaryName && header.Name != "./"+BinaryName {
			return fmt.Errorf("archive contains unexpected entry %q", header.Name)
		}
		if found {
			return fmt.Errorf("archive contains duplicate %s entry", BinaryName)
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("archive entry %q is not a regular file", header.Name)
		}
		if header.Size < 0 || header.Size > maxBinaryBytes {
			return fmt.Errorf("binary in archive is too large")
		}
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		written, err := io.Copy(out, tr)
		if err != nil {
			_ = out.Close()
			return err
		}
		if written != header.Size {
			_ = out.Close()
			return fmt.Errorf("binary size mismatch in archive")
		}
		if err := out.Sync(); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return fmt.Errorf("%s not found in archive", BinaryName)
	}
	return nil
}

func installBinary(src string, dst string) error {
	if err := ensureRegularFile(src); err != nil {
		return fmt.Errorf("invalid source binary: %w", err)
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := ensureSafeInstallDir(dir); err != nil {
		return err
	}
	if err := ensureRegularFileIfExists(dst); err != nil {
		return err
	}
	backup := dst + ".bak"
	if err := ensureRegularFileIfExists(backup); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := copyFile(dst, backup, 0755); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(dst)+".tmp.")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	in, err := os.Open(src)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = in.Close()
		_ = tmp.Close()
		return err
	}
	if err := in.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
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
	if err := ensureRegularFileIfExists(dst); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		if statErr := ensureRegularFileIfExists(backup); statErr == nil {
			_ = copyFile(backup, dst, 0755)
		}
		return err
	}
	return syncDir(dir)
}

func restoreBackup(dst string) error {
	backup := dst + ".bak"
	if err := ensureRegularFile(backup); err != nil {
		return err
	}
	if err := ensureRegularFileIfExists(dst); err != nil {
		return err
	}
	if err := copyFile(backup, dst, 0755); err != nil {
		return err
	}
	return syncDir(filepath.Dir(dst))
}

func copyFile(src string, dst string, mode os.FileMode) error {
	if err := ensureRegularFile(src); err != nil {
		return err
	}
	if err := ensureRegularFileIfExists(dst); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func ensureSafeInstallDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("install directory %s must not be a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("install directory %s is not a directory", path)
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("install directory %s must not be group/world writable", path)
	}
	return nil
}

func ensureRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must not be a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func ensureRegularFileIfExists(path string) error {
	if err := ensureRegularFile(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
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
