package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckDiscoversPrereleaseAndEligibility(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	executable := filepath.Join(dir, "snow")
	writeVersionScript(t, executable, "0.1.0-alpha.1")
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Error("missing User-Agent")
		}
		_, _ = w.Write([]byte(`[{"tag_name":"v0.1.0-alpha.2","draft":false,"prerelease":true}]`))
	}))
	defer server.Close()
	svc := NewWithOptions(Options{CurrentVersion: "0.1.0-alpha.1", Executable: executable, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, HTTPClient: server.Client(), APIURL: server.URL})
	status, err := svc.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || !status.Eligible || status.LatestVersion != "0.1.0-alpha.2" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("release check made %d requests, want metadata only", got)
	}
}

func TestCheckChoosesHighestCanonicalPublishedRelease(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"tag_name":"v9.0.0","draft":true},
			{"tag_name":"not-a-version","draft":false},
			{"tag_name":"8.0.0","draft":false},
			{"tag_name":"v1.2.0-alpha.3","draft":false},
			{"tag_name":"v1.1.9","draft":false}
		]`))
	}))
	defer server.Close()
	svc := NewWithOptions(Options{CurrentVersion: "1.0.0", HTTPClient: server.Client(), APIURL: server.URL})
	status, err := svc.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.LatestVersion != "1.2.0-alpha.3" || status.Release.Tag != "v1.2.0-alpha.3" {
		t.Fatalf("unexpected selected release: %+v", status)
	}
}

func TestCheckDevelopmentBuildCannotInstall(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"v1.0.0","draft":false}]`))
	}))
	defer server.Close()
	svc := NewWithOptions(Options{CurrentVersion: "0.1.0-dev", HTTPClient: server.Client(), APIURL: server.URL})
	status, err := svc.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if status.Eligible || !strings.Contains(status.Reason, "development") {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestCheckBoundsAndSanitizesHTTPFailure(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("secret upstream body"))
	}))
	defer server.Close()
	svc := NewWithOptions(Options{CurrentVersion: "1.0.0", HTTPClient: server.Client(), APIURL: server.URL})
	_, err := svc.Check(t.Context())
	if err == nil || strings.Contains(err.Error(), "secret upstream body") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckCancellationAndMetadataLimit(t *testing.T) {
	t.Parallel()
	t.Run("canceled", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			<-t.Context().Done()
		}))
		defer server.Close()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		svc := NewWithOptions(Options{CurrentVersion: "1.0.0", HTTPClient: server.Client(), APIURL: server.URL})
		if _, err := svc.Check(ctx); err == nil {
			t.Fatal("canceled check unexpectedly succeeded")
		}
	})
	t.Run("oversized", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(metadataLimit+1))
			_, _ = w.Write(bytes.Repeat([]byte("x"), metadataLimit+1))
		}))
		defer server.Close()
		svc := NewWithOptions(Options{CurrentVersion: "1.0.0", HTTPClient: server.Client(), APIURL: server.URL})
		if _, err := svc.Check(t.Context()); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversized check error = %v", err)
		}
	})
}

func TestEligibilityRejectsSymlinksAndUnsupportedPlatforms(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "snow")
	writeVersionScript(t, target, "1.0.0")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if eligible, _ := NewWithOptions(Options{CurrentVersion: "1.0.0", Executable: link, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}).Eligibility(); eligible {
		t.Fatal("symlink executable was eligible")
	}
	if eligible, _ := NewWithOptions(Options{CurrentVersion: "1.0.0", Executable: target, GOOS: "freebsd", GOARCH: "amd64"}).Eligibility(); eligible {
		t.Fatal("unsupported platform was eligible")
	}
}

func TestSafeRedirectRejectsDowngradeAndUnknownHosts(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"http://github.com/file", "https://example.com/file"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := safeRedirect(req, nil); err == nil {
			t.Fatalf("safeRedirect accepted %q", target)
		}
	}
}

func TestOpenPinnedStageRejectsReplacement(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	stage, err := root.OpenFile("stage", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := stage.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "replacement"), []byte("replacement"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.Rename("replacement", "stage"); err != nil {
		t.Fatal(err)
	}
	if file, _, err := openPinnedStage(root, "stage", expected); err == nil {
		file.Close()
		t.Fatal("replaced staged binary retained its trusted identity")
	}
}

func TestInstallVerifiedReleaseAtomically(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("self-update is supported on macOS and Linux")
	}
	t.Parallel()
	const current = "0.1.0-alpha.1"
	const latest = "0.1.0-alpha.2"
	const versionCheckTimeout = 10 * time.Second
	dir := t.TempDir()
	executable := filepath.Join(dir, "snow")
	writeVersionScript(t, executable, current)
	archiveName := fmt.Sprintf("snow_%s_%s_%s.tar.gz", latest, runtime.GOOS, runtime.GOARCH)
	archive := releaseArchive(t, latest, runtime.GOOS, runtime.GOARCH, versionScript(latest), nil)
	digest := sha256.Sum256(archive)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%x  %s\n", digest, archiveName)
		case archiveName:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	svc := NewWithOptions(Options{CurrentVersion: current, Executable: executable, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, HTTPClient: server.Client(), DownloadURL: server.URL, CommandTimeout: versionCheckTimeout})
	status := Status{CurrentVersion: current, LatestVersion: latest, Available: true, Eligible: true, Release: Release{Version: latest, Tag: "v" + latest}}
	var progress []Progress
	result, err := svc.InstallWithProgress(t.Context(), status, func(snapshot Progress) {
		progress = append(progress, snapshot)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.InstalledVersion != latest {
		t.Fatalf("unexpected result: %+v", result)
	}
	phases := make(map[ProgressPhase]bool)
	for _, snapshot := range progress {
		phases[snapshot.Phase] = true
	}
	for _, phase := range []ProgressPhase{ProgressPreparing, ProgressDownloading, ProgressVerifying, ProgressInstalling} {
		if !phases[phase] {
			t.Fatalf("progress missing phase %d: %+v", phase, progress)
		}
	}
	lastDownload := Progress{}
	for _, snapshot := range progress {
		if snapshot.Phase == ProgressDownloading {
			lastDownload = snapshot
		}
	}
	if lastDownload.DownloadedBytes != int64(len(archive)) {
		t.Fatalf("downloaded bytes = %d, want %d", lastDownload.DownloadedBytes, len(archive))
	}
	reported, err := binaryVersion(t.Context(), executable, versionCheckTimeout)
	if err != nil || reported != latest {
		t.Fatalf("installed version = %q, %v", reported, err)
	}
}

func TestInstallChecksumMismatchPreservesExecutable(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("self-update is supported on macOS and Linux")
	}
	t.Parallel()
	const current = "1.0.0"
	const latest = "1.0.1"
	dir := t.TempDir()
	executable := filepath.Join(dir, "snow")
	original := versionScript(current)
	if err := os.WriteFile(executable, original, 0o755); err != nil {
		t.Fatal(err)
	}
	archiveName := fmt.Sprintf("snow_%s_%s_%s.tar.gz", latest, runtime.GOOS, runtime.GOARCH)
	archive := releaseArchive(t, latest, runtime.GOOS, runtime.GOARCH, versionScript(latest), nil)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "SHA256SUMS" {
			_, _ = fmt.Fprintf(w, "%064x  %s\n", 0, archiveName)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	svc := NewWithOptions(Options{CurrentVersion: current, Executable: executable, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, HTTPClient: server.Client(), DownloadURL: server.URL})
	_, err := svc.Install(t.Context(), Status{LatestVersion: latest, Available: true, Eligible: true, Release: Release{Version: latest, Tag: "v" + latest}})
	if err == nil {
		t.Fatal("Install unexpectedly succeeded")
	}
	got, readErr := os.ReadFile(executable)
	if readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("original executable changed: %v", readErr)
	}
}

func TestExtractReleaseBinaryRejectsUnexpectedAndLinks(t *testing.T) {
	t.Parallel()
	const version = "1.2.3"
	valid := releaseArchive(t, version, "linux", "amd64", []byte("binary"), nil)
	if got, err := extractReleaseBinary(valid, version, "linux", "amd64"); err != nil || string(got) != "binary" {
		t.Fatalf("valid archive: %q, %v", got, err)
	}
	unexpected := releaseArchive(t, version, "linux", "amd64", []byte("binary"), &tar.Header{Name: "unexpected", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	if _, err := extractReleaseBinary(unexpected, version, "linux", "amd64"); err == nil {
		t.Fatal("unexpected archive member was accepted")
	}
	link := releaseArchive(t, version, "linux", "amd64", []byte("binary"), &tar.Header{Name: "snow_1.2.3_linux_amd64/extra", Typeflag: tar.TypeSymlink, Linkname: "/tmp/x"})
	if _, err := extractReleaseBinary(link, version, "linux", "amd64"); err == nil {
		t.Fatal("link archive member was accepted")
	}
}

func TestExtractReleaseBinaryConsumesValidGzipTrailerAndRejectsTrailingData(t *testing.T) {
	t.Parallel()
	const version = "1.2.3"
	binary := make([]byte, 256<<10)
	state := uint64(0x9e3779b97f4a7c15)
	for i := range binary {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		binary[i] = byte(state)
	}
	archive := releaseArchive(t, version, "linux", "amd64", binary, nil)
	got, err := extractReleaseBinary(archive, version, "linux", "amd64")
	if err != nil {
		t.Fatalf("large valid archive was rejected: %v", err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatal("extracted release binary differs from archive")
	}
	if _, err := extractReleaseBinary(append(bytes.Clone(archive), "trailing"...), version, "linux", "amd64"); err == nil {
		t.Fatal("archive with trailing bytes was accepted")
	}
	if _, err := extractReleaseBinary(append(bytes.Clone(archive), archive...), version, "linux", "amd64"); err == nil {
		t.Fatal("archive with a second gzip member was accepted")
	}
	corruptTrailer := bytes.Clone(archive)
	corruptTrailer[len(corruptTrailer)-8] ^= 0xff
	if _, err := extractReleaseBinary(corruptTrailer, version, "linux", "amd64"); err == nil {
		t.Fatal("archive with a corrupt gzip checksum trailer was accepted")
	}
}

func TestExpectedChecksumRequiresExactlyOneEntry(t *testing.T) {
	t.Parallel()
	name := "snow_1.0.0_linux_amd64.tar.gz"
	hash := strings.Repeat("a", 64)
	if _, err := expectedChecksum([]byte(hash+"  "+name+"\n"), name); err != nil {
		t.Fatal(err)
	}
	if _, err := expectedChecksum([]byte(hash+"  "+name+"\n"+hash+"  "+name+"\n"), name); err == nil {
		t.Fatal("duplicate checksum was accepted")
	}
	if _, err := expectedChecksum([]byte("bad  "+name+"\n"), name); err == nil {
		t.Fatal("malformed checksum was accepted")
	}
}

func writeVersionScript(t *testing.T, path, version string) {
	t.Helper()
	if err := os.WriteFile(path, versionScript(version), 0o755); err != nil {
		t.Fatal(err)
	}
}

func versionScript(version string) []byte {
	return []byte("#!/bin/sh\n[ \"$1\" = version ] || exit 2\nprintf '%s\\n' '" + version + "'\n")
}

func releaseArchive(t *testing.T, version, goos, goarch string, binary []byte, extra *tar.Header) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	root := fmt.Sprintf("snow_%s_%s_%s", version, goos, goarch)
	members := []struct {
		header tar.Header
		data   []byte
	}{
		{tar.Header{Name: root + "/", Typeflag: tar.TypeDir, Mode: 0o755}, nil},
		{tar.Header{Name: root + "/LICENSE", Typeflag: tar.TypeReg, Mode: 0o644, Size: 7}, []byte("license")},
		{tar.Header{Name: root + "/README.md", Typeflag: tar.TypeReg, Mode: 0o644, Size: 6}, []byte("readme")},
		{tar.Header{Name: root + "/snow", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(binary))}, binary},
	}
	for _, member := range members {
		if err := tw.WriteHeader(&member.header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(member.data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		if err := tw.WriteHeader(extra); err != nil {
			t.Fatal(err)
		}
		if extra.Size > 0 {
			if _, err := tw.Write(make([]byte, extra.Size)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
