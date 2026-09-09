// Package javascript adapts trusted local Goja scripts to Snow tools and extensions.
package javascript

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/internal/tools"
	"github.com/elmissouri16/snow-core/pkg/plugin"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

const (
	MaxPlugins       = 32
	MaxManifestBytes = 64 << 10
	MaxScriptBytes   = 1 << 20
	MaxConfigBytes   = 64 << 10
	MaxTools         = 64
	MaxSubscriptions = 128
	MaxQueueEvents   = 256
	MaxQueueBytes    = 1 << 20
	MaxHostCalls     = 64
	MaxLogs          = 256
	MaxLogBytes      = 2 << 10
)

type Manifest struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Version      string                   `json:"version"`
	APIVersion   int                      `json:"api_version"`
	Entry        string                   `json:"entry"`
	HostTools    []string                 `json:"host_tools,omitempty"`
	Capabilities []string                 `json:"capabilities,omitempty"`
	Settings     []protocol.PluginSetting `json:"settings,omitempty"`
}

// Package contains an immutable startup snapshot, not an open executable path.
type Package struct {
	Manifest    Manifest
	Path        string
	Script      []byte
	Config      json.RawMessage
	Fingerprint string
}

// ReadPackage never executes JavaScript. ConfinedRoot is set for project input.
func ReadPackage(ctx context.Context, path, confinedRoot string, config json.RawMessage) (*Package, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("JavaScript package path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := openDirectory(abs, confinedRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readRegular(root, "snow-plugin.json", MaxManifestBytes)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err = jsonv2.Unmarshal(raw, &m, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("JavaScript manifest: %w", err)
	}
	if err = plugin.ValidateManifest(plugin.Manifest{ID: m.ID, Name: m.Name, Version: m.Version}); err != nil {
		return nil, err
	}
	if m.APIVersion != 1 && m.APIVersion != 2 {
		return nil, fmt.Errorf("unsupported JavaScript api_version %d; want 1 or 2", m.APIVersion)
	}
	if err := validateExtensionManifest(m); err != nil {
		return nil, err
	}
	if !filepath.IsLocal(m.Entry) || filepath.Ext(m.Entry) != ".js" {
		return nil, errors.New("entry must be a relative .js file inside the package")
	}
	seen := map[string]bool{}
	for _, name := range m.HostTools {
		if (m.APIVersion == 1 && !tools.JavaScriptBuiltin(name)) || name == "" || seen[name] {
			return nil, fmt.Errorf("invalid or duplicate host tool %q", name)
		}
		seen[name] = true
	}
	script, err := readRegular(root, m.Entry, MaxScriptBytes)
	if err != nil {
		return nil, err
	}
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	if len(config) > MaxConfigBytes {
		return nil, errors.New("JavaScript config exceeds 64 KiB")
	}
	var object map[string]any
	if err = jsonv2.Unmarshal(config, &object); err != nil || object == nil {
		return nil, errors.New("JavaScript config must be a JSON object")
	}
	if err = applySettingDefaults(m.Settings, object); err != nil {
		return nil, err
	}
	canonical, err := jsonv2.Marshal(object, jsonv2.Deterministic(true))
	if err != nil {
		return nil, err
	}
	fingerprintInput, err := jsonv2.Marshal([]any{abs, string(raw), string(script), string(canonical)})
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &Package{Manifest: m, Path: abs, Script: script, Config: canonical, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(fingerprintInput))}, nil
}

// Each component is opened and checked against the observed inode before
// descending. This pins the authorized tree without following symlink entries.
func openDirectory(path, confined string) (*os.Root, error) {
	base := string(filepath.Separator)
	if confined != "" {
		base = filepath.Clean(confined)
	}
	rel, err := filepath.Rel(base, path)
	if err != nil || (!filepath.IsLocal(rel) && rel != ".") {
		return nil, fmt.Errorf("plugin path is outside its declaring root")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return root, nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		next, err := descend(root, part)
		root.Close()
		if err != nil {
			return nil, err
		}
		root = next
	}
	return root, nil
}

func descend(root *os.Root, part string) (*os.Root, error) {
	info, err := root.Lstat(part)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("plugin directory %q must not be a symlink", part)
	}
	next, err := root.OpenRoot(part)
	if err != nil {
		return nil, err
	}
	actual, err := next.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		next.Close()
		return nil, fmt.Errorf("plugin directory changed while opening %q", part)
	}
	return next, nil
}

func readRegular(root *os.Root, name string, limit int) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, errors.New("package file must be local")
	}
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))
	var opened []*os.Root
	defer func() {
		for _, r := range opened {
			r.Close()
		}
	}()
	for _, part := range parts[:len(parts)-1] {
		next, err := descend(root, part)
		if err != nil {
			return nil, err
		}
		opened = append(opened, next)
		root = next
	}
	name = parts[len(parts)-1]
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, fmt.Errorf("package file %q must be regular and at most %d bytes", name, limit)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, errors.New("package file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("package file exceeds %d bytes", limit)
	}
	after, err := file.Stat()
	if err != nil || after.Size() != actual.Size() || !after.ModTime().Equal(actual.ModTime()) {
		return nil, errors.New("package file changed while reading")
	}
	return data, nil
}

func clonePackage(p *Package) *Package {
	out := *p
	out.Script = slices.Clone(p.Script)
	out.Config = slices.Clone(p.Config)
	out.Manifest.HostTools = slices.Clone(p.Manifest.HostTools)
	out.Manifest.Capabilities = slices.Clone(p.Manifest.Capabilities)
	out.Manifest.Settings = slices.Clone(p.Manifest.Settings)
	for i := range out.Manifest.Settings {
		out.Manifest.Settings[i].Default = slices.Clone(p.Manifest.Settings[i].Default)
		out.Manifest.Settings[i].Choices = slices.Clone(p.Manifest.Settings[i].Choices)
	}
	return &out
}
