package sandbox

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

const (
	maxDefaultImageArchiveBytes int64 = 2 << 30
	defaultImageFetchTimeout          = 10 * time.Minute
)

// ImageFetchResult binds the staged archive to the bytes produced by the
// fetcher. Manager.Init recomputes this digest immediately before handing the
// archive to smolvm so a replaced or redirected staging file fails closed.
type ImageFetchResult struct {
	ArchiveSHA256 [sha256.Size]byte
}

// ImageFetcher materializes a registry image as a local Docker-save archive.
// The default implementation uses anonymous host-side HTTPS only; no guest
// network authority is needed during sandbox creation or execution.
type ImageFetcher interface {
	Fetch(context.Context, string, string) (ImageFetchResult, error)
}

type registryImageFetcher struct{}

func (registryImageFetcher) Fetch(ctx context.Context, source, destination string) (ImageFetchResult, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, defaultImageFetchTimeout)
	defer cancel()
	ref, err := name.ParseReference(source, name.StrictValidation)
	if err != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: parse pinned default image: %w", err)
	}
	if _, ok := ref.(name.Digest); !ok {
		return ImageFetchResult{}, fmt.Errorf("sandbox: default registry image must be digest-pinned")
	}
	image, err := remote.Image(ref,
		remote.WithContext(fetchCtx),
		remote.WithAuth(authn.Anonymous),
		remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}),
		remote.WithUserAgent("snow-core-sandbox-bootstrap/1"),
	)
	if err != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: download pinned default image: %w", err)
	}
	manifest, err := image.Manifest()
	if err != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: inspect pinned default image manifest: %w", err)
	}
	if _, err := preflightImageManifestSize(manifest); err != nil {
		return ImageFetchResult{}, err
	}
	archive, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: open default image archive: %w", err)
	}
	writer := &limitedArchiveWriter{writer: archive, remaining: maxDefaultImageArchiveBytes}
	writeErr := tarball.Write(ref, image, writer)
	syncErr := archive.Sync()
	closeErr := archive.Close()
	if writeErr != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: write default image archive: %w", writeErr)
	}
	if syncErr != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: sync default image archive: %w", syncErr)
	}
	if closeErr != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: close default image archive: %w", closeErr)
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: secure default image archive: %w", err)
	}
	info, err := os.Lstat(destination)
	if err != nil {
		return ImageFetchResult{}, fmt.Errorf("sandbox: inspect default image archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return ImageFetchResult{}, fmt.Errorf("sandbox: default image archive is not a private regular file")
	}
	if info.Size() <= 0 || info.Size() > maxDefaultImageArchiveBytes {
		return ImageFetchResult{}, fmt.Errorf("sandbox: default image archive size %d is invalid", info.Size())
	}
	digest, err := digestArchive(destination)
	if err != nil {
		return ImageFetchResult{}, err
	}
	return ImageFetchResult{ArchiveSHA256: digest}, nil
}

type limitedArchiveWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *limitedArchiveWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		written := 0
		if w.remaining > 0 {
			var err error
			written, err = w.writer.Write(data[:w.remaining])
			w.remaining -= int64(written)
			if err != nil {
				return written, err
			}
		}
		return written, fmt.Errorf("default image archive exceeds %d-byte limit", maxDefaultImageArchiveBytes)
	}
	written, err := w.writer.Write(data)
	w.remaining -= int64(written)
	return written, err
}

func preflightImageManifestSize(manifest *v1.Manifest) (int64, error) {
	if manifest == nil {
		return 0, fmt.Errorf("sandbox: pinned default image has no manifest")
	}
	total := manifest.Config.Size
	if total < 0 || total > maxDefaultImageArchiveBytes {
		return 0, fmt.Errorf("sandbox: pinned default image declared size %d exceeds %d-byte limit", total, maxDefaultImageArchiveBytes)
	}
	for _, layer := range manifest.Layers {
		if layer.Size < 0 || layer.Size > maxDefaultImageArchiveBytes-total {
			return 0, fmt.Errorf("sandbox: pinned default image declared size exceeds %d-byte limit", maxDefaultImageArchiveBytes)
		}
		total += layer.Size
	}
	if total <= 0 {
		return 0, fmt.Errorf("sandbox: pinned default image declared size %d is invalid", total)
	}
	return total, nil
}

func createStagedImageArchive(statePath string) (string, func(), error) {
	if !filepath.IsAbs(statePath) {
		return "", nil, fmt.Errorf("sandbox: state path must be absolute for image staging")
	}
	stateDir := filepath.Dir(filepath.Clean(statePath))
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("sandbox: create operator state directory: %w", err)
	}
	resolvedStateDir, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		return "", nil, fmt.Errorf("sandbox: resolve operator state directory: %w", err)
	}
	if !filepath.IsAbs(resolvedStateDir) {
		return "", nil, fmt.Errorf("sandbox: resolved operator state directory is not absolute")
	}
	stageDir := filepath.Join(resolvedStateDir, ".sandbox-images")
	if err := os.Mkdir(stageDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", nil, fmt.Errorf("sandbox: create private image staging directory: %w", err)
	}
	info, err := os.Lstat(stageDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, fmt.Errorf("sandbox: image staging path is not a private directory")
	}
	if err := os.Chmod(stageDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("sandbox: secure image staging directory: %w", err)
	}
	archive, err := os.CreateTemp(stageDir, ".default-image-*.tar")
	if err != nil {
		return "", nil, fmt.Errorf("sandbox: create default image archive: %w", err)
	}
	archivePath := archive.Name()
	cleanup := func() { _ = os.Remove(archivePath) }
	if err := archive.Chmod(0o600); err != nil {
		_ = archive.Close()
		cleanup()
		return "", nil, fmt.Errorf("sandbox: secure default image archive: %w", err)
	}
	if err := archive.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("sandbox: prepare default image archive: %w", err)
	}
	return archivePath, cleanup, nil
}

func validateStagedImageArchive(path string, expected ImageFetchResult) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("sandbox: staged image archive path is not absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("sandbox: inspect staged image archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("sandbox: staged image archive is not a private regular file")
	}
	if info.Size() <= 0 || info.Size() > maxDefaultImageArchiveBytes {
		return fmt.Errorf("sandbox: staged image archive size %d is invalid", info.Size())
	}
	actual, err := digestArchive(path)
	if err != nil {
		return err
	}
	if actual != expected.ArchiveSHA256 {
		return fmt.Errorf("sandbox: staged image archive digest changed after download")
	}
	return nil
}

func digestArchive(path string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return digest, fmt.Errorf("sandbox: open staged image archive: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	read, err := io.Copy(hash, io.LimitReader(file, maxDefaultImageArchiveBytes+1))
	if err != nil {
		return digest, fmt.Errorf("sandbox: hash staged image archive: %w", err)
	}
	if read <= 0 || read > maxDefaultImageArchiveBytes {
		return digest, fmt.Errorf("sandbox: staged image archive size %d is invalid", read)
	}
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}
