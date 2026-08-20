package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
)

var pluginSpecKeys = []string{
	"id", "command", "enabled", "cwd", "env", "timeout_ms", "max_frame_bytes",
	"max_output_bytes", "max_progress_bytes", "max_input_bytes", "max_concurrent",
	"capabilities", "config",
}

type rawPluginDeclaration struct {
	spec   publicplugin.PluginSpec
	object map[string]json.RawMessage
}

// LoadPluginDeclarations reads and validates configured external plugin
// declarations without starting them. Duplicate IDs are rejected because one
// configuration scope cannot contain two authoritative declarations.
func LoadPluginDeclarations(path string) ([]publicplugin.PluginSpec, error) {
	data, err := readConfigFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	declarations, err := parsePluginDeclarations(root["plugins"])
	if err != nil {
		return nil, err
	}
	out := make([]publicplugin.PluginSpec, len(declarations))
	for i, declaration := range declarations {
		out[i] = declaration.spec
	}
	return out, nil
}

// AddPlugin atomically adds or replaces one declaration while preserving
// unknown fields in the containing config and in a replaced plugin object.
func AddPlugin(path string, global bool, spec publicplugin.PluginSpec, replace bool) error {
	if err := publicplugin.ValidateSpec(spec); err != nil {
		return err
	}
	return updatePluginDeclarations(path, global, func(declarations []rawPluginDeclaration) ([]rawPluginDeclaration, error) {
		for i, declaration := range declarations {
			if declaration.spec.ID != spec.ID {
				continue
			}
			if !replace {
				return nil, fmt.Errorf("plugin: plugin %q already exists (use --replace)", spec.ID)
			}
			encoded, err := pluginObject(spec)
			if err != nil {
				return nil, err
			}
			for _, key := range pluginSpecKeys {
				delete(declaration.object, key)
			}
			for key, value := range encoded {
				declaration.object[key] = value
			}
			declaration.spec = spec
			declarations[i] = declaration
			return declarations, nil
		}
		object, err := pluginObject(spec)
		if err != nil {
			return nil, err
		}
		return append(declarations, rawPluginDeclaration{spec: spec, object: object}), nil
	})
}

// SetPluginEnabled updates only enabled on a declaration that already exists
// in the selected scope. It never copies commands, environment, or private
// runtime configuration across global/project boundaries.
func SetPluginEnabled(path string, global bool, id string, enabled bool) error {
	if err := publicplugin.ValidateIdentifier("plugin id", id); err != nil {
		return err
	}
	return updatePluginDeclarations(path, global, func(declarations []rawPluginDeclaration) ([]rawPluginDeclaration, error) {
		for i, declaration := range declarations {
			if declaration.spec.ID != id {
				continue
			}
			value, _ := json.Marshal(enabled)
			declaration.object["enabled"] = value
			declaration.spec.Enabled = enabled
			declarations[i] = declaration
			return declarations, nil
		}
		return nil, fmt.Errorf("plugin: plugin %q is not declared in the target scope", id)
	})
}

// RemovePlugin atomically removes one declaration from the selected scope.
func RemovePlugin(path string, global bool, id string) error {
	if err := publicplugin.ValidateIdentifier("plugin id", id); err != nil {
		return err
	}
	return updatePluginDeclarations(path, global, func(declarations []rawPluginDeclaration) ([]rawPluginDeclaration, error) {
		for i, declaration := range declarations {
			if declaration.spec.ID == id {
				return append(declarations[:i], declarations[i+1:]...), nil
			}
		}
		return nil, fmt.Errorf("plugin: plugin %q is not declared in the target scope", id)
	})
}

func updatePluginDeclarations(path string, global bool, update func([]rawPluginDeclaration) ([]rawPluginDeclaration, error)) error {
	return updateSection(path, global, "plugins", func(raw json.RawMessage) (json.RawMessage, error) {
		declarations, err := parsePluginDeclarations(raw)
		if err != nil {
			return nil, err
		}
		declarations, err = update(declarations)
		if err != nil {
			return nil, err
		}
		objects := make([]map[string]json.RawMessage, len(declarations))
		for i, declaration := range declarations {
			objects[i] = declaration.object
		}
		return json.Marshal(objects)
	})
}

func parsePluginDeclarations(raw json.RawMessage) ([]rawPluginDeclaration, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var objects []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &objects); err != nil {
		return nil, fmt.Errorf("config: parse plugins: %w", err)
	}
	seen := make(map[string]bool, len(objects))
	declarations := make([]rawPluginDeclaration, 0, len(objects))
	for i, object := range objects {
		encoded, err := json.Marshal(object)
		if err != nil {
			return nil, fmt.Errorf("config: encode plugin %d: %w", i, err)
		}
		var spec publicplugin.PluginSpec
		if err := json.Unmarshal(encoded, &spec); err != nil {
			return nil, fmt.Errorf("config: parse plugin %d: %w", i, err)
		}
		if err := publicplugin.ValidateSpec(spec); err != nil {
			return nil, fmt.Errorf("config: plugin %d: %w", i, err)
		}
		if seen[spec.ID] {
			return nil, fmt.Errorf("config: duplicate plugin id %q", spec.ID)
		}
		seen[spec.ID] = true
		declarations = append(declarations, rawPluginDeclaration{spec: spec, object: object})
	}
	return declarations, nil
}

func pluginObject(spec publicplugin.PluginSpec) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("config: encode plugin %s: %w", spec.ID, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, err
	}
	return object, nil
}

// UpdateMCPServers atomically edits only the mcp_servers object while
// preserving unrelated and unknown top-level configuration fields.
func UpdateMCPServers(path string, global bool, update func(map[string]publicmcp.ServerSpec) error) error {
	return updateSection(path, global, "mcp_servers", func(raw json.RawMessage) (json.RawMessage, error) {
		servers := map[string]publicmcp.ServerSpec{}
		if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
			if err := json.Unmarshal(raw, &servers); err != nil {
				return nil, fmt.Errorf("config: parse mcp_servers: %w", err)
			}
		}
		if err := update(servers); err != nil {
			return nil, err
		}
		return json.Marshal(servers)
	})
}

// UpdateSkills atomically edits only the global skills object.
func UpdateSkills(path string, update func(*SkillsConfig) error) error {
	return updateSection(path, true, "skills", func(raw json.RawMessage) (json.RawMessage, error) {
		cfg := SkillsConfig{Overrides: map[string]bool{}}
		if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("config: parse skills: %w", err)
			}
		}
		if cfg.Overrides == nil {
			cfg.Overrides = map[string]bool{}
		}
		if err := update(&cfg); err != nil {
			return nil, err
		}
		return json.Marshal(cfg)
	})
}

// UpdateProjectSkills atomically edits only a project's skills policy.
func UpdateProjectSkills(path string, update func(*ProjectSkillsConfig) error) error {
	return updateSection(path, false, "skills", func(raw json.RawMessage) (json.RawMessage, error) {
		cfg := ProjectSkillsConfig{Overrides: map[string]bool{}}
		if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, fmt.Errorf("config: parse project skills: %w", err)
			}
		}
		if cfg.Overrides == nil {
			cfg.Overrides = map[string]bool{}
		}
		if err := update(&cfg); err != nil {
			return nil, err
		}
		return json.Marshal(cfg)
	})
}

func updateSection(path string, global bool, key string, update func(json.RawMessage) (json.RawMessage, error)) error {
	if path == "" {
		return errors.New("config: empty path")
	}
	root := map[string]json.RawMessage{}
	data, err := readConfigFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if err == nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("config: parse %s: %w", path, err)
		}
		if root == nil {
			return fmt.Errorf("config: parse %s: root must be a JSON object", path)
		}
	}
	next, err := update(root[key])
	if err != nil {
		return err
	}
	root[key] = next
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encode %s: %w", path, err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > MaxConfigFileBytes {
		return fmt.Errorf("config: encoded configuration exceeds %d byte limit", MaxConfigFileBytes)
	}
	mode := os.FileMode(0o644)
	if global {
		mode = 0o600
	} else if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	return atomicWrite(path, encoded, mode)
}

func atomicWrite(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	file, err := os.CreateTemp(dir, ".snow-config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temporary file: %w", err)
	}
	tmp := file.Name()
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return fmt.Errorf("config: chmod temporary file: %w", err)
	}
	if _, err = io.Copy(file, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("config: write temporary file: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("config: sync temporary file: %w", err)
	}
	if err = file.Close(); err != nil {
		file = nil
		return fmt.Errorf("config: close temporary file: %w", err)
	}
	file = nil
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config: replace %s: %w", path, err)
	}
	return nil
}
