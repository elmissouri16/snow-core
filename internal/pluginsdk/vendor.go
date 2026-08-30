package pluginsdk

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

type Runtime string

const (
	RuntimePython     Runtime = "python"
	RuntimeJavaScript Runtime = "javascript"
)

var runtimeVersions = map[Runtime]string{
	RuntimePython:     "0.1.0.dev0",
	RuntimeJavaScript: "0.1.0-dev.0",
}

type FileReceipt struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type Receipt struct {
	Runtime     Runtime       `json:"runtime"`
	SDKVersion  string        `json:"sdk_version"`
	Destination string        `json:"destination"`
	Replaced    bool          `json:"replaced"`
	Files       []FileReceipt `json:"files"`
}

type Options struct {
	Runtime     Runtime
	Destination string
	Replace     bool
	HostVersion string
}

type vendorMetadata struct {
	Runtime     Runtime       `json:"runtime"`
	SDKVersion  string        `json:"sdk_version"`
	HostVersion string        `json:"snow_version,omitempty"`
	Files       []FileReceipt `json:"files"`
}

func ParseRuntime(value string) (Runtime, error) {
	runtime := Runtime(value)
	if _, ok := runtimeVersions[runtime]; !ok {
		return "", fmt.Errorf("plugin sdk: unsupported runtime %q (want python or javascript)", value)
	}
	return runtime, nil
}

// Vendor writes one embedded SDK beneath Destination/vendor/<runtime>. The
// destination plugin directory must already exist. A pinned os.Root confines
// every mutation to that directory, and a staged rename prevents partial SDKs
// from appearing at the final path.
func Vendor(options Options) (Receipt, error) {
	var receipt Receipt
	runtime, err := ParseRuntime(string(options.Runtime))
	if err != nil {
		return receipt, err
	}
	if options.Destination == "" {
		return receipt, errors.New("plugin sdk: destination is required")
	}
	absolute, err := filepath.Abs(options.Destination)
	if err != nil {
		return receipt, fmt.Errorf("plugin sdk: resolve destination: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return receipt, fmt.Errorf("plugin sdk: destination must be an existing directory: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return receipt, fmt.Errorf("plugin sdk: inspect destination: %w", err)
	}
	if !info.IsDir() {
		return receipt, fmt.Errorf("plugin sdk: destination is not a directory: %s", canonical)
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return receipt, fmt.Errorf("plugin sdk: pin destination: %w", err)
	}
	defer root.Close()

	if err := ensureVendorDirectory(root); err != nil {
		return receipt, err
	}
	finalName := path.Join("vendor", string(runtime))
	exists, err := rootEntryExists(root, finalName)
	if err != nil {
		return receipt, err
	}
	if exists {
		entry, err := root.Lstat(finalName)
		if err != nil {
			return receipt, fmt.Errorf("plugin sdk: inspect existing SDK: %w", err)
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return receipt, errors.New("plugin sdk: refusing to replace a symlinked SDK directory")
		}
		if !options.Replace {
			return receipt, fmt.Errorf("plugin sdk: %s already exists; pass --replace after review", filepath.Join(canonical, filepath.FromSlash(finalName)))
		}
	}

	suffix, err := randomSuffix()
	if err != nil {
		return receipt, fmt.Errorf("plugin sdk: create staging name: %w", err)
	}
	stageName := path.Join("vendor", "."+string(runtime)+".tmp-"+suffix)
	if err := root.Mkdir(stageName, 0o755); err != nil {
		return receipt, fmt.Errorf("plugin sdk: create staging directory: %w", err)
	}
	stagePresent := true
	defer func() {
		if stagePresent {
			_ = root.RemoveAll(stageName)
		}
	}()

	files, err := writeRuntime(root, stageName, runtime)
	if err != nil {
		return receipt, err
	}
	metadata := vendorMetadata{
		Runtime: runtime, SDKVersion: runtimeVersions[runtime],
		HostVersion: options.HostVersion, Files: slices.Clone(files),
	}
	metadataData, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return receipt, fmt.Errorf("plugin sdk: encode metadata: %w", err)
	}
	metadataData = append(metadataData, '\n')
	if err := root.WriteFile(path.Join(stageName, "snow-sdk.json"), metadataData, 0o644); err != nil {
		return receipt, fmt.Errorf("plugin sdk: write metadata: %w", err)
	}
	files = append(files, fileReceipt("snow-sdk.json", metadataData))

	backupName := ""
	if exists {
		backupName = path.Join("vendor", "."+string(runtime)+".old-"+suffix)
		if err := root.Rename(finalName, backupName); err != nil {
			return receipt, fmt.Errorf("plugin sdk: stage existing SDK for replacement: %w", err)
		}
	}
	if err := root.Rename(stageName, finalName); err != nil {
		installErr := fmt.Errorf("plugin sdk: install staged SDK: %w", err)
		if backupName != "" {
			if rollbackErr := root.Rename(backupName, finalName); rollbackErr != nil {
				return receipt, errors.Join(
					installErr,
					fmt.Errorf("plugin sdk: restore previous SDK from %s: %w", backupName, rollbackErr),
				)
			}
		}
		return receipt, installErr
	}
	stagePresent = false
	if backupName != "" {
		if err := root.RemoveAll(backupName); err != nil {
			return receipt, fmt.Errorf("plugin sdk: installed replacement but could not remove old SDK: %w", err)
		}
	}

	return Receipt{
		Runtime: runtime, SDKVersion: runtimeVersions[runtime],
		Destination: filepath.Join(canonical, filepath.FromSlash(finalName)),
		Replaced:    exists, Files: files,
	}, nil
}

func ensureVendorDirectory(root *os.Root) error {
	entry, err := root.Lstat("vendor")
	if errors.Is(err, os.ErrNotExist) {
		if err := root.Mkdir("vendor", 0o755); err != nil {
			return fmt.Errorf("plugin sdk: create vendor directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("plugin sdk: inspect vendor directory: %w", err)
	}
	if entry.Mode()&os.ModeSymlink != 0 || !entry.IsDir() {
		return errors.New("plugin sdk: vendor must be a real directory, not a symlink or file")
	}
	return nil
}

func writeRuntime(root *os.Root, stageName string, runtime Runtime) ([]FileReceipt, error) {
	assetRoot := path.Join("assets", string(runtime))
	var files []FileReceipt
	err := fs.WalkDir(sdkAssets, assetRoot, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == assetRoot {
			return nil
		}
		relative := strings.TrimPrefix(name, assetRoot+"/")
		if relative == name || !fs.ValidPath(relative) {
			return fmt.Errorf("invalid embedded SDK path %q", relative)
		}
		target := path.Join(stageName, relative)
		if entry.IsDir() {
			return root.MkdirAll(target, 0o755)
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("embedded SDK contains symlink %q", relative)
		}
		data, err := fs.ReadFile(sdkAssets, name)
		if err != nil {
			return err
		}
		if err := root.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		files = append(files, fileReceipt(relative, data))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("plugin sdk: write %s SDK: %w", runtime, err)
	}
	return files, nil
}

func rootEntryExists(root *os.Root, name string) (bool, error) {
	_, err := root.Lstat(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("plugin sdk: inspect %s: %w", name, err)
}

func fileReceipt(name string, data []byte) FileReceipt {
	sum := sha256.Sum256(data)
	return FileReceipt{Path: name, SHA256: hex.EncodeToString(sum[:]), Bytes: len(data)}
}

func randomSuffix() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
