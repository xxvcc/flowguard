package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlanLatest(t *testing.T) {
	plan, err := BuildPlan(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != "latest" || plan.Asset == "" || !strings.Contains(plan.AssetURL, "/releases/latest/download/") {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestBuildPlanRejectsUnsafeInputs(t *testing.T) {
	if _, err := BuildPlan(Options{Repo: "bad repo"}); err == nil {
		t.Fatal("expected invalid repo error")
	}
	if _, err := BuildPlan(Options{Repo: "../flowguard"}); err == nil {
		t.Fatal("expected traversal-like repo error")
	}
	if _, err := BuildPlan(Options{Version: "v1.0.0/evil"}); err == nil {
		t.Fatal("expected invalid version error")
	}
	if _, err := BuildPlan(Options{BaseURL: "http://example.com/releases"}); err == nil {
		t.Fatal("expected unsafe URL error")
	}
	if _, err := BuildPlan(Options{BaseURL: "https://example.com/releases?asset=x"}); err == nil {
		t.Fatal("expected query URL error")
	}
	if _, err := BuildPlan(Options{BaseURL: "http://localhost/releases"}); err != nil {
		t.Fatal(err)
	}
}

func TestFindChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")
	want := strings.Repeat("a", 64)
	if err := os.WriteFile(path, []byte(want+"  flowguard_linux_amd64.tar.gz\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := findChecksum(path, "flowguard_linux_amd64.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("checksum=%s", got)
	}
	if _, err := findChecksum(path, "missing.tar.gz"); err == nil {
		t.Fatal("expected missing checksum error")
	}
}

func TestExtractBinaryRejectsMissing(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "asset.tar.gz")
	if err := writeTarGz(archive, "other", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(archive, filepath.Join(dir, BinaryName)); err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestExtractBinaryRejectsNestedFlowGuard(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "asset.tar.gz")
	if err := writeTarGz(archive, "nested/"+BinaryName, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(archive, filepath.Join(dir, BinaryName)); err == nil {
		t.Fatal("expected nested binary to be rejected")
	}
}

func TestExtractBinaryRejectsUnexpectedExtraEntry(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "asset.tar.gz")
	if err := writeTarGzEntries(archive,
		tarTestEntry{name: BinaryName, content: []byte("x"), typ: tar.TypeReg},
		tarTestEntry{name: "README.md", content: []byte("extra"), typ: tar.TypeReg},
	); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(archive, filepath.Join(dir, BinaryName)); err == nil {
		t.Fatal("expected extra archive entry to be rejected")
	}
}

func TestExtractBinaryRejectsDuplicateFlowGuard(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "asset.tar.gz")
	if err := writeTarGzEntries(archive,
		tarTestEntry{name: BinaryName, content: []byte("one"), typ: tar.TypeReg},
		tarTestEntry{name: "./" + BinaryName, content: []byte("two"), typ: tar.TypeReg},
	); err != nil {
		t.Fatal(err)
	}
	if err := extractBinary(archive, filepath.Join(dir, BinaryName)); err == nil {
		t.Fatal("expected duplicate binary entry to be rejected")
	}
}

func TestRunDryRunDoesNotNeedRoot(t *testing.T) {
	if err := Run(Options{DryRun: true, BaseURL: "https://example.invalid/releases", InstallDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}

func TestRunRequiresExistingBinaryBeforeDownload(t *testing.T) {
	err := Run(Options{BaseURL: "https://example.invalid/releases", InstallDir: t.TempDir(), NoRestart: true})
	if err == nil {
		t.Fatal("expected upgrade precondition error")
	}
	if !strings.Contains(err.Error(), "existing binary not found") && !strings.Contains(err.Error(), "upgrade must be run as root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadAndVerify(t *testing.T) {
	dir := t.TempDir()
	body := []byte("asset")
	sum := sha256.Sum256(body)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	path := filepath.Join(dir, "asset")
	if err := download(server.Client(), server.URL, path); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(path, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestDownloadRejectsUnsafeRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/asset", http.StatusFound)
	}))
	defer server.Close()
	if err := download(server.Client(), server.URL, filepath.Join(t.TempDir(), "asset")); err == nil {
		t.Fatal("expected unsafe redirect error")
	}
}

func TestExtractBinaryRejectsOversizedBinary(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "asset.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: BinaryName, Mode: 0755, Size: maxBinaryBytes + 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	_ = file.Close()
	if err := extractBinary(archive, filepath.Join(dir, BinaryName)); err == nil {
		t.Fatal("expected oversized binary error")
	}
}

func TestInstallBinaryCreatesBackupAndRestore(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, BinaryName)
	src := filepath.Join(dir, "new")
	if err := os.WriteFile(dst, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("installed data=%q", data)
	}
	backup, err := os.ReadFile(dst + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "old" {
		t.Fatalf("backup=%q", backup)
	}
	if err := restoreBackup(dst); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "old" {
		t.Fatalf("restored=%q", restored)
	}
}

func TestInstallBinaryRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, BinaryName)
	src := filepath.Join(dir, "new")
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(src, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dst); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(src, dst); err == nil {
		t.Fatal("expected symlink target to be rejected")
	}
}

func TestInstallBinaryRejectsSymlinkBackup(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, BinaryName)
	src := filepath.Join(dir, "new")
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(dst, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dst+".bak"); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(src, dst); err == nil {
		t.Fatal("expected symlink backup to be rejected")
	}
}

type tarTestEntry struct {
	name    string
	content []byte
	typ     byte
}

func writeTarGz(path string, name string, content []byte) error {
	return writeTarGzEntries(path, tarTestEntry{name: name, content: content, typ: tar.TypeReg})
}

func writeTarGzEntries(path string, entries ...tarTestEntry) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for _, entry := range entries {
		content := entry.content
		typ := entry.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		size := int64(len(content))
		if typ != tar.TypeReg {
			size = 0
		}
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0755, Size: size, Typeflag: typ}); err != nil {
			return err
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write(content); err != nil {
				return err
			}
		}
	}
	return nil
}
