package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestRegistryImageFetcherRejectsUnqualifiedSourceBeforeNetwork(t *testing.T) {
	_, err := (registryImageFetcher{}).Fetch(context.Background(), "ubuntu:latest", filepath.Join(t.TempDir(), "image.tar"))
	if err == nil || !strings.Contains(err.Error(), "parse pinned default image") {
		t.Fatalf("unqualified image error = %v", err)
	}
}

func TestRegistryImageFetcherRejectsFloatingDefaultBeforeNetwork(t *testing.T) {
	_, err := (registryImageFetcher{}).Fetch(context.Background(), "index.docker.io/library/ubuntu:latest", filepath.Join(t.TempDir(), "image.tar"))
	if err == nil || !strings.Contains(err.Error(), "digest-pinned") {
		t.Fatalf("floating image error = %v", err)
	}
}

func TestPreflightImageManifestSizeRejectsOversizedImage(t *testing.T) {
	manifest := &v1.Manifest{
		Config: v1.Descriptor{Size: 1},
		Layers: []v1.Descriptor{{Size: maxDefaultImageArchiveBytes}},
	}
	if _, err := preflightImageManifestSize(manifest); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized manifest error = %v", err)
	}
}

func TestPreflightImageManifestSizeSumsDeclaredContent(t *testing.T) {
	manifest := &v1.Manifest{
		Config: v1.Descriptor{Size: 5},
		Layers: []v1.Descriptor{{Size: 7}, {Size: 11}},
	}
	if got, err := preflightImageManifestSize(manifest); err != nil || got != 23 {
		t.Fatalf("manifest size = %d, err=%v", got, err)
	}
}

func TestLimitedArchiveWriterStopsBeforeDiskLimit(t *testing.T) {
	var output bytes.Buffer
	writer := &limitedArchiveWriter{writer: &output, remaining: 4}
	written, err := writer.Write([]byte("oversized"))
	if err == nil || written != 4 || output.String() != "over" {
		t.Fatalf("bounded write = %d, %q, err=%v", written, output.String(), err)
	}
}

func TestCreateStagedImageArchiveIgnoresHostileTMPDIR(t *testing.T) {
	project := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(project, "relative:project-controlled"))
	stateDir := t.TempDir()
	path, cleanup, err := createStagedImageArchive(filepath.Join(stateDir, "sandboxes.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	resolvedState, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || !strings.HasPrefix(path, filepath.Join(resolvedState, ".sandbox-images")+string(filepath.Separator)) {
		t.Fatalf("staged path = %q, state = %q", path, resolvedState)
	}
	if strings.HasPrefix(path, project+string(filepath.Separator)) {
		t.Fatalf("staged archive used project-controlled TMPDIR: %s", path)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("staging directory mode: info=%v err=%v", info, err)
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("staged archive mode: info=%v err=%v", info, err)
	}
}

func TestCreateStagedImageArchiveRejectsRelativeStatePath(t *testing.T) {
	if _, _, err := createStagedImageArchive("relative/sandboxes.json"); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative state path error = %v", err)
	}
}

func TestValidateStagedImageArchiveRejectsReplacement(t *testing.T) {
	path, cleanup, err := createStagedImageArchive(filepath.Join(t.TempDir(), "sandboxes.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	original := []byte("verified archive")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	expected := ImageFetchResult{ArchiveSHA256: sha256.Sum256(original)}
	if err := os.WriteFile(path, []byte("replaced archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateStagedImageArchive(path, expected); err == nil || !strings.Contains(err.Error(), "digest changed") {
		t.Fatalf("replacement validation error = %v", err)
	}
}

func TestValidateStagedImageArchiveRejectsUnsafeMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.tar")
	data := []byte("archive")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateStagedImageArchive(path, ImageFetchResult{ArchiveSHA256: sha256.Sum256(data)}); err == nil || !strings.Contains(err.Error(), "private regular file") {
		t.Fatalf("unsafe-mode validation error = %v", err)
	}
}
