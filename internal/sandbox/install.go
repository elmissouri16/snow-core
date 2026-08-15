package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	smolVMInstallerURL    = "https://raw.githubusercontent.com/smol-machines/smolvm/v1.8.1/scripts/install.sh"
	smolVMInstallerSHA256 = "24e9ed2c1e5c550ddf8b1e04b5d2ce31774b2e067d90fa6399e6762348e66bf3"
	maxInstallerBytes     = 128 << 10
	maxReleaseBytes       = 128 << 20
	installerTimeout      = 10 * time.Minute
	trustedInstallerBash  = "/bin/bash"
)

var smolVMReleaseSHA256 = map[string]string{
	"darwin-arm64": "4cd5cae1f749fd8947ac9f17c7f7b25b44992a7935213bdae1f145e66a47b437",
	"linux-arm64":  "155d091a89e7adb7e72c75bc7aca36b1c6908d871f371c8011981612385f3497",
	"linux-x86_64": "4649834fc90f494812618b349731097e715f27837ae0897eb3db18d2b6185ee9",
}

// Installer bootstraps the pinned external smolvm distribution after an
// explicit sandbox init. Implementations must return an absolute executable.
type Installer interface {
	Install(context.Context, string) (string, error)
}

type officialInstaller struct {
	scriptClient  *http.Client
	releaseClient *http.Client
}

func newOfficialInstaller() Installer {
	scriptClient := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: sameHostHTTPSRedirects("raw.githubusercontent.com"),
	}
	releaseClient := &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many release redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("release redirect is not HTTPS: %s", req.URL.Redacted())
			}
			switch req.URL.Hostname() {
			case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
				return nil
			default:
				return fmt.Errorf("release redirect left pinned GitHub origins: %s", req.URL.Redacted())
			}
		},
	}
	return &officialInstaller{scriptClient: scriptClient, releaseClient: releaseClient}
}

func sameHostHTTPSRedirects(host string) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many installer redirects")
		}
		if req.URL.Scheme != "https" || req.URL.Hostname() != host {
			return fmt.Errorf("installer redirect left pinned HTTPS origin: %s", req.URL.Redacted())
		}
		return nil
	}
}

func trustedInstallerInterpreter() (string, error) {
	info, err := os.Stat(trustedInstallerBash)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("sandbox: trusted installer interpreter %s is unavailable", trustedInstallerBash)
	}
	return trustedInstallerBash, nil
}

func (i *officialInstaller) Install(ctx context.Context, version string) (string, error) {
	if version != MinimumSmolVMVersion {
		return "", fmt.Errorf("sandbox: installer only supports pinned smolvm %s", MinimumSmolVMVersion)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	installCtx, cancel := context.WithTimeout(ctx, installerTimeout)
	defer cancel()
	home, err := installerHome()
	if err != nil {
		return "", err
	}
	script, err := i.downloadScript(installCtx)
	if err != nil {
		return "", err
	}
	release, err := i.downloadPinnedRelease(installCtx, home)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(release.dir)

	bash, err := trustedInstallerInterpreter()
	if err != nil {
		return "", err
	}
	scriptPath := filepath.Join(release.dir, "install.sh")
	if err := os.WriteFile(scriptPath, script, 0o600); err != nil {
		return "", fmt.Errorf("sandbox: write installer file: %w", err)
	}
	output, err := boundedCombinedOutputEnv(installCtx, installerEnvironment(home, release), bash, scriptPath,
		"--version", MinimumSmolVMVersion,
		"--no-modify-path",
	)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return "", fmt.Errorf("sandbox: official smolvm installer failed: %w: %s", err, message)
		}
		return "", fmt.Errorf("sandbox: official smolvm installer failed: %w", err)
	}
	executable, err := validateInstalledLayout(home)
	if err != nil {
		return "", err
	}
	if err := writeInstallReceipt(home, release.platform); err != nil {
		return "", err
	}
	return executable, nil
}

type pinnedRelease struct {
	dir           string
	platform      string
	archivePath   string
	checksumsPath string
	archiveName   string
	helperDir     string
}

func smolVMReleasePlatform() (string, error) {
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("sandbox: smolvm %s is unsupported on architecture %s", MinimumSmolVMVersion, runtime.GOARCH)
	}
	platform := runtime.GOOS + "-" + arch
	if _, ok := smolVMReleaseSHA256[platform]; !ok {
		return "", fmt.Errorf("sandbox: smolvm %s has no pinned release for %s", MinimumSmolVMVersion, platform)
	}
	return platform, nil
}

func (i *officialInstaller) downloadPinnedRelease(ctx context.Context, home string) (pinnedRelease, error) {
	platform, err := smolVMReleasePlatform()
	if err != nil {
		return pinnedRelease{}, err
	}
	digest := smolVMReleaseSHA256[platform]
	name := fmt.Sprintf("smolvm-%s-%s.tar.gz", MinimumSmolVMVersion, platform)
	releaseURL := fmt.Sprintf("https://github.com/smol-machines/smolvm/releases/download/v%s/%s", MinimumSmolVMVersion, name)
	cacheRoot := filepath.Join(home, ".cache", "snow")
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return pinnedRelease{}, fmt.Errorf("sandbox: create private installer cache: %w", err)
	}
	if err := os.Chmod(cacheRoot, 0o700); err != nil {
		return pinnedRelease{}, fmt.Errorf("sandbox: secure private installer cache: %w", err)
	}
	dir, err := os.MkdirTemp(cacheRoot, ".smolvm-release-*")
	if err != nil {
		return pinnedRelease{}, fmt.Errorf("sandbox: create release directory: %w", err)
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(dir)
		}
	}()
	archive := filepath.Join(dir, name)
	if err := i.downloadReleaseFile(ctx, releaseURL, archive, digest); err != nil {
		return pinnedRelease{}, err
	}
	checksums := filepath.Join(dir, "checksums.sha256")
	line := digest + "  " + name + "\n"
	if err := os.WriteFile(checksums, []byte(line), 0o600); err != nil {
		return pinnedRelease{}, fmt.Errorf("sandbox: write pinned release checksum: %w", err)
	}
	helperDir := filepath.Join(dir, "helpers")
	if err := os.Mkdir(helperDir, 0o700); err != nil {
		return pinnedRelease{}, fmt.Errorf("sandbox: create installer helper directory: %w", err)
	}
	wrapper := `#!/bin/sh
set -eu
out=""
url=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) shift; out="$1" ;;
    https://*) url="$1" ;;
  esac
  shift
done
[ -n "$out" ] && [ -n "$url" ]
case "$url" in
  */checksums.sha256) source="$SNOW_SMOLVM_CHECKSUMS" ;;
  */"$SNOW_SMOLVM_ARCHIVE_NAME") source="$SNOW_SMOLVM_ARCHIVE" ;;
  *) echo "blocked unexpected installer download: $url" >&2; exit 1 ;;
esac
/bin/cp "$source" "$out"
`
	if err := os.WriteFile(filepath.Join(helperDir, "curl"), []byte(wrapper), 0o700); err != nil {
		return pinnedRelease{}, fmt.Errorf("sandbox: write installer download helper: %w", err)
	}
	failed = false
	return pinnedRelease{dir: dir, platform: platform, archivePath: archive, checksumsPath: checksums, archiveName: name, helperDir: helperDir}, nil
}

func (i *officialInstaller) downloadReleaseFile(ctx context.Context, sourceURL, destination, expectedSHA string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "github.com" {
		return errors.New("sandbox: invalid pinned smolvm release URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return fmt.Errorf("sandbox: build release request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "snow-core-smolvm-bootstrap/1")
	resp, err := i.releaseClient.Do(req)
	if err != nil {
		return fmt.Errorf("sandbox: download pinned smolvm release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("sandbox: download pinned smolvm release: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxReleaseBytes {
		return fmt.Errorf("sandbox: pinned smolvm release is too large: %d", resp.ContentLength)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("sandbox: create release archive: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, maxReleaseBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("sandbox: read pinned smolvm release: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("sandbox: close pinned smolvm release: %w", closeErr)
	}
	if written == 0 || written > maxReleaseBytes {
		return fmt.Errorf("sandbox: pinned smolvm release size %d is invalid", written)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expectedSHA {
		return fmt.Errorf("sandbox: pinned smolvm release checksum mismatch (got %s)", actual)
	}
	return nil
}

func installerEnvironment(home string, release pinnedRelease) []string {
	tmp := release.dir
	// Do not let a project-controlled/current-directory PATH or shell hooks
	// choose helper programs used by the privileged host-side installer. Curl is
	// a local wrapper that serves only the already verified release archive.
	return []string{
		"HOME=" + home,
		"PATH=" + release.helperDir + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"SHELL=/bin/sh",
		"TMPDIR=" + tmp,
		"LANG=C",
		"LC_ALL=C",
		"SNOW_SMOLVM_ARCHIVE=" + release.archivePath,
		"SNOW_SMOLVM_CHECKSUMS=" + release.checksumsPath,
		"SNOW_SMOLVM_ARCHIVE_NAME=" + release.archiveName,
	}
}

func validateInstalledLayout(home string) (string, error) {
	executable := filepath.Join(home, ".local", "bin", "smolvm")
	info, err := os.Lstat(executable)
	if err != nil {
		return "", fmt.Errorf("sandbox: installer completed but %s is unavailable: %w", executable, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("sandbox: installer produced an unexpected non-symlink at %s", executable)
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve installed smolvm: %w", err)
	}
	expected := filepath.Join(home, ".smolvm", "smolvm")
	if filepath.Clean(resolved) != filepath.Clean(expected) {
		return "", fmt.Errorf("sandbox: installed smolvm symlink targets %s, want %s", resolved, expected)
	}
	info, err = os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("sandbox: installer produced an invalid executable at %s", resolved)
	}
	return executable, nil
}

func installReceiptContent(platform string) string {
	return fmt.Sprintf("smolvm=%s\nplatform=%s\narchive_sha256=%s\n", MinimumSmolVMVersion, platform, smolVMReleaseSHA256[platform])
}

func writeInstallReceipt(home, platform string) error {
	path := filepath.Join(home, ".smolvm", ".snow-verified-install")
	temp, err := os.CreateTemp(filepath.Dir(path), ".snow-receipt-*")
	if err != nil {
		return fmt.Errorf("sandbox: create verified-install receipt: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sandbox: secure verified-install receipt: %w", err)
	}
	if _, err := io.WriteString(temp, installReceiptContent(platform)); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sandbox: write verified-install receipt: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sandbox: sync verified-install receipt: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("sandbox: close verified-install receipt: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("sandbox: publish verified-install receipt: %w", err)
	}
	return nil
}

func validateVerifiedOfficialInstall(home string) (string, error) {
	executable, err := validateInstalledLayout(home)
	if err != nil {
		return "", err
	}
	platform, err := smolVMReleasePlatform()
	if err != nil {
		return "", err
	}
	receipt := filepath.Join(home, ".smolvm", ".snow-verified-install")
	info, err := os.Lstat(receipt)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("sandbox: verified smolvm install receipt is unavailable or unsafe")
	}
	data, err := os.ReadFile(receipt)
	if err != nil {
		return "", fmt.Errorf("sandbox: read verified smolvm install receipt: %w", err)
	}
	if string(data) != installReceiptContent(platform) {
		return "", fmt.Errorf("sandbox: verified smolvm install receipt does not match pinned release")
	}
	return executable, nil
}

func installerHome() (string, error) {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("sandbox: locate user home for smolvm install: %w", err)
		}
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("sandbox: HOME is not absolute: %q", home)
	}
	if strings.ContainsRune(home, os.PathListSeparator) {
		return "", fmt.Errorf("sandbox: HOME contains path-list separator %q", os.PathListSeparator)
	}
	home = filepath.Clean(home)
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve HOME for smolvm install: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("sandbox: HOME is not an existing directory: %s", resolved)
	}
	return filepath.Clean(resolved), nil
}

func (i *officialInstaller) downloadScript(ctx context.Context) ([]byte, error) {
	parsed, err := url.Parse(smolVMInstallerURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "raw.githubusercontent.com" {
		return nil, errors.New("sandbox: invalid pinned installer URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("sandbox: build installer request: %w", err)
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "snow-core-smolvm-bootstrap/1")
	resp, err := i.scriptClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sandbox: download official smolvm installer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("sandbox: download official smolvm installer: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInstallerBytes+1))
	if err != nil {
		return nil, fmt.Errorf("sandbox: read official smolvm installer: %w", err)
	}
	if len(data) == 0 || len(data) > maxInstallerBytes {
		return nil, fmt.Errorf("sandbox: official smolvm installer size %d is invalid", len(data))
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != smolVMInstallerSHA256 {
		return nil, fmt.Errorf("sandbox: official smolvm installer checksum mismatch (got %s)", actual)
	}
	return data, nil
}
