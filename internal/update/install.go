package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	licenseLimit  = 1 << 20
	readmeLimit   = 4 << 20
	binaryLimit   = 128 << 20
	expandedLimit = licenseLimit + readmeLimit + binaryLimit
	commandLimit  = 4096
)

func (s *Service) Install(ctx context.Context, status Status) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !status.Available || !status.Eligible {
		return Result{}, errors.New("update: no eligible newer release to install")
	}
	latest, err := ParseVersion(status.Release.Version)
	if err != nil || status.Release.Tag != "v"+latest.String() || latest.String() != status.LatestVersion {
		return Result{}, errors.New("update: invalid checked release")
	}
	current, err := ParseVersion(s.currentVersion)
	if err != nil || Compare(latest, current) <= 0 {
		return Result{}, errors.New("update: release is not newer than the running version")
	}
	if eligible, reason := s.eligibility(); !eligible {
		return Result{}, fmt.Errorf("update: %s", reason)
	}

	executable, err := filepath.Abs(s.executable)
	if err != nil {
		return Result{}, fmt.Errorf("update: resolve executable: %w", err)
	}
	root, err := os.OpenRoot(filepath.Dir(executable))
	if err != nil {
		return Result{}, fmt.Errorf("update: pin executable directory: %w", err)
	}
	defer root.Close()
	name := filepath.Base(executable)
	lock, err := openUpdateLock(ctx, root, "."+name+".update.lock")
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()

	before, err := regularFile(root, name)
	if err != nil {
		return Result{}, err
	}
	archiveName := fmt.Sprintf("snow_%s_%s_%s.tar.gz", latest.String(), s.goos, s.goarch)
	base := s.downloadURL + "/" + status.Release.Tag
	checksums, err := s.download(ctx, base+"/SHA256SUMS", checksumLimit)
	if err != nil {
		return Result{}, err
	}
	expected, err := expectedChecksum(checksums, archiveName)
	if err != nil {
		return Result{}, err
	}
	archive, err := s.download(ctx, base+"/"+archiveName, archiveLimit)
	if err != nil {
		return Result{}, err
	}
	actual := sha256.Sum256(archive)
	if !bytes.Equal(actual[:], expected) {
		return Result{}, errors.New("update: release checksum verification failed")
	}
	binary, err := extractReleaseBinary(archive, latest.String(), s.goos, s.goarch)
	if err != nil {
		return Result{}, err
	}
	stageName, stageFile, err := createStage(root, name)
	if err != nil {
		return Result{}, err
	}
	removeStage := true
	defer func() {
		if removeStage {
			_ = root.Remove(stageName)
		}
	}()
	if _, err := stageFile.Write(binary); err != nil {
		stageFile.Close()
		return Result{}, fmt.Errorf("update: write staged binary: %w", err)
	}
	if err := stageFile.Sync(); err != nil {
		stageFile.Close()
		return Result{}, fmt.Errorf("update: sync staged binary: %w", err)
	}
	if err := stageFile.Chmod(0o755); err != nil {
		stageFile.Close()
		return Result{}, fmt.Errorf("update: chmod staged binary: %w", err)
	}
	writtenStage, err := stageFile.Stat()
	if err != nil {
		stageFile.Close()
		return Result{}, fmt.Errorf("update: inspect staged binary: %w", err)
	}
	if err := stageFile.Close(); err != nil {
		return Result{}, fmt.Errorf("update: close staged binary: %w", err)
	}
	stagedFile, stagedInfo, err := openPinnedStage(root, stageName, writtenStage)
	if err != nil {
		return Result{}, err
	}
	defer stagedFile.Close()
	reported, err := binaryVersionFile(ctx, stagedFile, filepath.Join(filepath.Dir(executable), stageName), s.commandTimeout)
	if err != nil {
		return Result{}, err
	}
	if reported != latest.String() {
		return Result{}, fmt.Errorf("update: staged binary reports %q, expected %q", reported, latest.String())
	}
	namedStage, err := root.Lstat(stageName)
	if err != nil || !os.SameFile(stagedInfo, namedStage) {
		return Result{}, errors.New("update: staged binary identity changed")
	}
	after, err := regularFile(root, name)
	if err != nil {
		return Result{}, err
	}
	if !os.SameFile(before, after) {
		return Result{}, errors.New("update: executable changed during update")
	}
	if err := root.Rename(stageName, name); err != nil {
		return Result{}, fmt.Errorf("update: atomically replace executable: %w", err)
	}
	removeStage = false
	installedInfo, err := root.Lstat(name)
	if err != nil || !os.SameFile(stagedInfo, installedInfo) {
		return Result{}, errors.New("update: executable was replaced but its installed identity could not be confirmed")
	}
	dir, err := root.Open(".")
	if err != nil {
		return Result{}, fmt.Errorf("update: executable was replaced but its directory could not be opened for sync: %w", err)
	}
	if err := dir.Sync(); err != nil {
		dir.Close()
		return Result{}, fmt.Errorf("update: executable was replaced but its directory could not be synced: %w", err)
	}
	if err := dir.Close(); err != nil {
		return Result{}, fmt.Errorf("update: executable was replaced but its directory could not be closed after sync: %w", err)
	}
	return Result{PreviousVersion: s.currentVersion, InstalledVersion: latest.String()}, nil
}

func regularFile(root *os.Root, name string) (os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("update: inspect executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("update: executable is not a regular non-symlink file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("update: open executable: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("update: executable identity changed")
	}
	return opened, nil
}

func createStage(root *os.Root, executableName string) (string, *os.File, error) {
	for range 100 {
		var token [12]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", nil, fmt.Errorf("update: generate stage name: %w", err)
		}
		name := fmt.Sprintf(".%s.update-%x", executableName, token[:])
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return name, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, fmt.Errorf("update: create staged binary: %w", err)
		}
	}
	return "", nil, errors.New("update: could not create a unique staged binary")
}

func openPinnedStage(root *os.Root, name string, expected os.FileInfo) (*os.File, os.FileInfo, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("update: reopen staged binary: %w", err)
	}
	opened, statErr := file.Stat()
	named, lstatErr := root.Lstat(name)
	if statErr != nil || lstatErr != nil || !opened.Mode().IsRegular() || !os.SameFile(expected, opened) || !os.SameFile(opened, named) {
		file.Close()
		return nil, nil, errors.New("update: staged binary identity changed")
	}
	return file, opened, nil
}

func expectedChecksum(data []byte, archiveName string) ([]byte, error) {
	var match []byte
	matches := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != archiveName {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return nil, errors.New("update: release checksum is malformed")
		}
		match = decoded
		matches++
	}
	if matches != 1 {
		return nil, errors.New("update: release checksum must contain exactly one matching entry")
	}
	return match, nil
}

func extractReleaseBinary(data []byte, version, goos, goarch string) ([]byte, error) {
	source := bytes.NewReader(data)
	gz, err := gzip.NewReader(source)
	if err != nil {
		return nil, errors.New("update: release archive is not valid gzip")
	}
	gz.Multistream(false)
	tr := tar.NewReader(gz)
	root := fmt.Sprintf("snow_%s_%s_%s", version, goos, goarch)
	expected := map[string]int64{
		root + "/":          4096,
		root + "/LICENSE":   licenseLimit,
		root + "/README.md": readmeLimit,
		root + "/snow":      binaryLimit,
	}
	seen := make(map[string]bool, len(expected))
	var binary []byte
	var total int64
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, errors.New("update: cannot read release archive")
		}
		limit, ok := expected[header.Name]
		if !ok || seen[header.Name] || filepath.IsAbs(header.Name) || strings.Contains(header.Name, "\\") || strings.ContainsRune(header.Name, '\x00') || filepath.Clean(header.Name) != strings.TrimSuffix(header.Name, "/") && header.Name != root+"/" {
			return nil, errors.New("update: release archive contains unexpected paths")
		}
		if header.Name == root+"/" {
			if header.Typeflag != tar.TypeDir || header.Size < 0 || header.Size > limit {
				return nil, errors.New("update: release archive contains an unsafe root")
			}
		} else if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, errors.New("update: release archive contains a non-regular member")
		}
		if header.Size < 0 || header.Size > limit || total+header.Size > expandedLimit {
			return nil, errors.New("update: expanded release archive exceeds its size limit")
		}
		total += header.Size
		seen[header.Name] = true
		if header.Name == root+"/snow" {
			binary, err = io.ReadAll(io.LimitReader(tr, limit+1))
			if err != nil || int64(len(binary)) != header.Size {
				return nil, errors.New("update: cannot read release binary")
			}
		} else if _, err := io.Copy(io.Discard, tr); err != nil {
			return nil, errors.New("update: cannot read release member")
		}
	}
	if err := gz.Close(); err != nil || source.Len() != 0 {
		return nil, errors.New("update: release archive has trailing or invalid data")
	}
	for name := range expected {
		if !seen[name] {
			return nil, errors.New("update: release archive is missing required members")
		}
	}
	return binary, nil
}

func binaryVersion(ctx context.Context, path string, timeout time.Duration) (string, error) {
	return runVersionCommand(ctx, timeout, exec.CommandContext, path, nil)
}

func binaryVersionFile(ctx context.Context, file *os.File, fallbackPath string, timeout time.Duration) (string, error) {
	path, err := inheritedExecutablePath()
	if err != nil {
		return "", err
	}
	if path == "" {
		return binaryVersion(ctx, fallbackPath, timeout)
	}
	return runVersionCommand(ctx, timeout, exec.CommandContext, path, []*os.File{file})
}

func runVersionCommand(ctx context.Context, timeout time.Duration, command func(context.Context, string, ...string) *exec.Cmd, path string, extraFiles []*os.File) (string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := command(checkCtx, path, "version")
	cmd.ExtraFiles = extraFiles
	var output limitedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("update: binary version check failed: %w", err)
	}
	if output.overflow {
		return "", errors.New("update: binary version output exceeds its limit")
	}
	value := strings.TrimSuffix(output.String(), "\n")
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("update: binary version output is invalid")
	}
	return value, nil
}

type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := commandLimit - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return original, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
		b.overflow = true
	}
	_, _ = b.Buffer.Write(p)
	return original, nil
}
