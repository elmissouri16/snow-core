package sandbox

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestTrustedInstallerInterpreterIgnoresHostilePATH(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bash"), []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := trustedInstallerInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/bin/bash" {
		t.Fatalf("interpreter = %q", got)
	}
}

func TestInstallerHomeRejectsPathListSeparator(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home:hostile")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	if _, err := installerHome(); err == nil || !strings.Contains(err.Error(), "path-list separator") {
		t.Fatalf("installerHome error = %v", err)
	}
}

func TestVerifiedInstallRejectsRegularUserLocalExecutable(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin", "smolvm")
	if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("#!/bin/sh\necho smolvm 1.8.1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := validateVerifiedOfficialInstall(home); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("validated regular executable: %v", err)
	}
}

func TestInstallerEnvironmentUsesFixedHelperPath(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp/project:controlled")
	release := pinnedRelease{dir: "/tmp/release", helperDir: "/tmp/helpers", archivePath: "/tmp/archive", checksumsPath: "/tmp/checksums", archiveName: "archive.tgz"}
	env := strings.Join(installerEnvironment("/home/test", release), "\n")
	if !strings.Contains(env, "HOME=/home/test") || !strings.Contains(env, "PATH=/tmp/helpers:/usr/bin:/bin:/usr/sbin:/sbin") || !strings.Contains(env, "TMPDIR=/tmp/release") {
		t.Fatalf("installer environment = %q", env)
	}
	if strings.Contains(env, ".:") || strings.Contains(env, "PWD=") || strings.Contains(env, "BASH_ENV=") || strings.Contains(env, "BASH_FUNC_") {
		t.Fatalf("installer environment contains project-controlled lookup: %q", env)
	}
}

func TestOfficialInstallerRejectsUnpinnedReleaseArtifact(t *testing.T) {
	if _, err := smolVMReleasePlatform(); err != nil {
		t.Skip(err)
	}
	installer := &officialInstaller{releaseClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("tampered release archive")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}}
	_, err := installer.downloadPinnedRelease(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "release checksum mismatch") {
		t.Fatalf("unpinned release error = %v", err)
	}
}

func TestOfficialInstallerRejectsUnpinnedScript(t *testing.T) {
	installer := &officialInstaller{scriptClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != smolVMInstallerURL {
			t.Fatalf("installer URL = %s", req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("#!/bin/sh\necho tampered\n")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}}
	_, err := installer.downloadScript(context.Background())
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unpinned installer error = %v", err)
	}
}

func TestOfficialInstallerRejectsOversizedScript(t *testing.T) {
	installer := &officialInstaller{scriptClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxInstallerBytes+1))),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}}
	_, err := installer.downloadScript(context.Background())
	if err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("oversized installer error = %v", err)
	}
}
