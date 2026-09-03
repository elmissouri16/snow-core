package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	AuxiliaryFileLimit = 64 * 1024
	ThemeFileLimit     = 64
)

// Diagnostic is a non-fatal auxiliary configuration problem.
type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// AdaptiveColor is one semantic terminal color pair.
type AdaptiveColor struct {
	Light string `yaml:"light" json:"light"`
	Dark  string `yaml:"dark" json:"dark"`
}

// ThemeColors contains the semantic roles understood by the TUI.
type ThemeColors struct {
	Accent     AdaptiveColor `yaml:"accent" json:"accent"`
	Muted      AdaptiveColor `yaml:"muted" json:"muted"`
	Foreground AdaptiveColor `yaml:"foreground" json:"foreground"`
	Warning    AdaptiveColor `yaml:"warning" json:"warning"`
	Error      AdaptiveColor `yaml:"error" json:"error"`
	Success    AdaptiveColor `yaml:"success" json:"success"`
	Separator  AdaptiveColor `yaml:"separator" json:"separator"`
}

// ThemeFile is the versioned custom theme format.
type ThemeFile struct {
	Version int         `yaml:"version" json:"version"`
	Name    string      `yaml:"name" json:"name"`
	Extends string      `yaml:"extends" json:"extends"`
	Colors  ThemeColors `yaml:"colors" json:"colors"`
	Path    string      `yaml:"-" json:"path,omitempty"`
}

// KeybindingsFile maps stable action names to Bubble Tea key strings.
type KeybindingsFile struct {
	Version  int                 `yaml:"version" json:"version"`
	Bindings map[string][]string `yaml:"bindings" json:"bindings"`
}

// SearchPolicy controls soft grep/glob exclusions.
type SearchPolicy struct {
	Version          int      `yaml:"version" json:"version"`
	RespectGitignore *bool    `yaml:"respect_gitignore" json:"respect_gitignore,omitempty"`
	RespectIgnore    *bool    `yaml:"respect_ignore" json:"respect_ignore,omitempty"`
	Hidden           *bool    `yaml:"hidden" json:"hidden,omitempty"`
	GeneratedDirs    []string `yaml:"generated_dirs" json:"generated_dirs,omitempty"`
	Exclude          []string `yaml:"exclude" json:"exclude,omitempty"`
}

// EffectiveSearchPolicy has concrete values after scope merging.
type EffectiveSearchPolicy struct {
	RespectGitignore bool
	RespectIgnore    bool
	Hidden           bool
	GeneratedDirs    []string
	Exclude          []string
}

var defaultGeneratedDirs = []string{"vendor", "dist", "build", "coverage"}

func DefaultSearchPolicy() EffectiveSearchPolicy {
	return EffectiveSearchPolicy{
		RespectGitignore: true,
		RespectIgnore:    true,
		GeneratedDirs:    slices.Clone(defaultGeneratedDirs),
	}
}

// IsDefaultGeneratedDir reports whether name is excluded by the built-in search policy.
func IsDefaultGeneratedDir(name string) bool {
	return slices.Contains(defaultGeneratedDirs, name)
}

func mergeSearch(base EffectiveSearchPolicy, overlay SearchPolicy) EffectiveSearchPolicy {
	if overlay.RespectGitignore != nil {
		base.RespectGitignore = *overlay.RespectGitignore
	}
	if overlay.RespectIgnore != nil {
		base.RespectIgnore = *overlay.RespectIgnore
	}
	if overlay.Hidden != nil {
		base.Hidden = *overlay.Hidden
	}
	base.GeneratedDirs = append(base.GeneratedDirs, overlay.GeneratedDirs...)
	base.Exclude = append(base.Exclude, overlay.Exclude...)
	base.GeneratedDirs = uniqueStrings(base.GeneratedDirs)
	base.Exclude = uniqueStrings(base.Exclude)
	return base
}

// LoadSearchPolicy loads warn-and-fallback global and optional trusted project policy.
func LoadSearchPolicy(globalDir, projectRoot string, projectAllowed bool) (EffectiveSearchPolicy, []Diagnostic) {
	policy := DefaultSearchPolicy()
	var diagnostics []Diagnostic
	load := func(path, confinedRoot string) {
		var raw SearchPolicy
		if err := decodeAuxScoped(confinedRoot, path, &raw); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			}
			return
		}
		if raw.Version != 1 {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: fmt.Sprintf("unsupported version %d", raw.Version)})
			return
		}
		for _, pattern := range append(slices.Clone(raw.GeneratedDirs), raw.Exclude...) {
			if strings.TrimSpace(pattern) == "" {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: "empty ignore pattern"})
				return
			}
		}
		policy = mergeSearch(policy, raw)
	}
	load(filepath.Join(globalDir, "search.yaml"), "")
	if projectAllowed {
		load(filepath.Join(projectRoot, ".snow", "search.yaml"), projectRoot)
	}
	return policy, diagnostics
}

// KeybindingScope preserves scope boundaries so callers can fall back from an
// invalid project override without discarding a valid global map.
type KeybindingScope struct {
	Path string
	File KeybindingsFile
}

func LoadKeybindingScopes(globalDir, projectRoot string, projectAllowed bool) ([]KeybindingScope, []Diagnostic) {
	var scopes []KeybindingScope
	var diagnostics []Diagnostic
	effective := defaultAuxBindings()
	load := func(path, confinedRoot string) {
		var raw KeybindingsFile
		if err := decodeAuxScoped(confinedRoot, path, &raw); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			}
			return
		}
		if raw.Version != 1 || raw.Bindings == nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: "keybindings requires version: 1 and bindings"})
			return
		}
		if err := validateKeybindingFile(raw); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			return
		}
		candidate := cloneBindings(effective)
		for action, keys := range raw.Bindings {
			candidate[action] = slices.Clone(keys)
		}
		candidate["abort"] = appendUnique(candidate["abort"], "ctrl+c")
		candidate["abort"] = appendUnique(candidate["abort"], "esc")
		candidate["quit"] = appendUnique(candidate["quit"], "ctrl+c")
		candidate["close"] = appendUnique(candidate["close"], "esc")
		if err := validateEffectiveAuxBindings(candidate); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			return
		}
		effective = candidate
		scopes = append(scopes, KeybindingScope{Path: path, File: raw})
	}
	load(filepath.Join(globalDir, "keybindings.yaml"), "")
	if projectAllowed {
		load(filepath.Join(projectRoot, ".snow", "keybindings.yaml"), projectRoot)
	}
	return scopes, diagnostics
}

// LoadKeybindings returns the merged valid scopes for inventory callers.
func LoadKeybindings(globalDir, projectRoot string, projectAllowed bool) (KeybindingsFile, []Diagnostic) {
	result := KeybindingsFile{Version: 1, Bindings: map[string][]string{}}
	scopes, diagnostics := LoadKeybindingScopes(globalDir, projectRoot, projectAllowed)
	for _, scope := range scopes {
		for action, keys := range scope.File.Bindings {
			result.Bindings[action] = slices.Clone(keys)
		}
	}
	return result, diagnostics
}

// LoadThemes discovers bounded regular YAML files. Invalid files are ignored individually.
func LoadThemes(globalDir, projectRoot string, projectAllowed bool) (map[string]ThemeFile, []Diagnostic) {
	themes := map[string]ThemeFile{}
	var diagnostics []Diagnostic
	loadDir := func(dir, confinedRoot string) {
		if confinedRoot != "" {
			if err := validateAuxPath(confinedRoot, dir); err != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: dir, Message: err.Error()})
				return
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				diagnostics = append(diagnostics, Diagnostic{Path: dir, Message: err.Error()})
			}
			return
		}
		slices.SortFunc(entries, func(a, b os.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
		count := 0
		for _, entry := range entries {
			if count >= ThemeFileLimit {
				diagnostics = append(diagnostics, Diagnostic{Path: dir, Message: "theme file limit reached (64)"})
				break
			}
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".yaml") {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			count++
			path := filepath.Join(dir, entry.Name())
			var raw ThemeFile
			if err := decodeAuxScoped(confinedRoot, path, &raw); err != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
				continue
			}
			raw.Name = strings.TrimSpace(raw.Name)
			raw.Extends = strings.TrimSpace(raw.Extends)
			if err := validateTheme(raw); err != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
				continue
			}
			raw.Path = path
			themes[raw.Name] = raw
		}
	}
	loadDir(filepath.Join(globalDir, "themes"), "")
	if projectAllowed {
		loadDir(filepath.Join(projectRoot, ".snow", "themes"), projectRoot)
	}
	return themes, diagnostics
}

// KeybindingActions is the stable inventory accepted by auxiliary keybinding
// files and exposed by the interactive TUI editor.
var KeybindingActions = []string{
	"submit", "follow_up", "newline", "paste", "abort", "quit", "toggle_mode", "thinking", "models", "agents", "processes",
	"page_up", "page_down", "top", "bottom", "line_up", "line_down",
	"picker_up", "picker_down", "picker_previous", "picker_next", "picker_page_up", "picker_page_down", "picker_top", "picker_bottom",
	"accept", "close", "branch_fork", "branch_rename", "branch_delete", "confirm",
}

// DefaultKeybindings returns a detached copy of Snow's built-in bindings.
func DefaultKeybindings() map[string][]string {
	return cloneBindings(defaultAuxBindings())
}

func defaultAuxBindings() map[string][]string {
	return map[string][]string{
		"submit": {"enter"}, "follow_up": {"alt+enter"}, "newline": {"ctrl+j", "alt+enter"}, "paste": {"ctrl+v"}, "abort": {"ctrl+c", "esc"}, "quit": {"ctrl+c", "ctrl+d"}, "toggle_mode": {"shift+tab"}, "thinking": {"ctrl+t"},
		"models": {"alt+m"}, "agents": {"alt+a"}, "processes": {"alt+p"},
		"page_up": {"pgup"}, "page_down": {"pgdown"}, "top": {"home"}, "bottom": {"end"}, "line_up": {"ctrl+up"}, "line_down": {"ctrl+down"},
		"picker_up": {"up", "left", "k"}, "picker_down": {"down", "right", "j"}, "picker_previous": {"shift+tab"}, "picker_next": {"tab"}, "picker_page_up": {"pgup"}, "picker_page_down": {"pgdown"}, "picker_top": {"home"}, "picker_bottom": {"end"},
		"accept": {"enter"}, "close": {"esc"}, "branch_fork": {"f"}, "branch_rename": {"r"}, "branch_delete": {"d"}, "confirm": {"y"},
	}
}

func cloneBindings(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = slices.Clone(values)
	}
	return out
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func validateEffectiveAuxBindings(bindings map[string][]string) error {
	for _, context := range [][]string{
		{"submit", "newline", "paste", "toggle_mode", "thinking", "models", "agents", "processes", "abort"},
		{"submit", "follow_up", "thinking", "models", "agents", "processes", "abort"},
		{"models", "agents", "processes", "quit"},
		{"picker_up", "picker_down", "picker_previous", "picker_next", "picker_page_up", "picker_page_down", "picker_top", "picker_bottom", "accept", "close", "branch_fork", "branch_rename", "branch_delete", "confirm", "models", "agents", "processes"},
	} {
		seen := map[string]string{}
		for _, action := range context {
			for _, value := range bindings[action] {
				value = strings.ToLower(strings.TrimSpace(value))
				if prior := seen[value]; prior != "" && prior != action {
					return fmt.Errorf("key %q collides between %s and %s", value, prior, action)
				}
				seen[value] = action
			}
		}
	}
	return nil
}

func validateKeybindingFile(file KeybindingsFile) error {
	allowed := map[string]bool{}
	for _, name := range KeybindingActions {
		allowed[name] = true
	}
	clean := map[string][]string{}
	for action, keys := range file.Bindings {
		if !allowed[action] {
			return fmt.Errorf("unknown keybinding action %q", action)
		}
		if len(keys) == 0 {
			return fmt.Errorf("keybinding action %q cannot be empty", action)
		}
		for _, value := range keys {
			value = strings.ToLower(strings.TrimSpace(value))
			if !validAuxKeyName(value) {
				return fmt.Errorf("invalid key %q for %s", value, action)
			}
			clean[action] = append(clean[action], value)
		}
	}
	for _, context := range [][]string{
		{"submit", "newline", "paste", "toggle_mode", "thinking", "models", "agents", "processes", "abort"},
		{"submit", "follow_up", "thinking", "models", "agents", "processes", "abort"},
		{"models", "agents", "processes", "quit"},
		{"picker_up", "picker_down", "picker_previous", "picker_next", "picker_page_up", "picker_page_down", "picker_top", "picker_bottom", "accept", "close", "branch_fork", "branch_rename", "branch_delete", "confirm", "models", "agents", "processes"},
	} {
		seen := map[string]string{}
		for _, action := range context {
			for _, value := range clean[action] {
				if prior := seen[value]; prior != "" && prior != action {
					return fmt.Errorf("key %q collides between %s and %s", value, prior, action)
				}
				seen[value] = action
			}
		}
	}
	return nil
}

func validAuxKeyName(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	known := map[string]bool{"enter": true, "esc": true, "tab": true, "shift+tab": true, "up": true, "down": true, "left": true, "right": true, "home": true, "end": true, "pgup": true, "pgdown": true, "backspace": true, "delete": true}
	if known[value] {
		return true
	}
	if strings.HasPrefix(value, "ctrl+") || strings.HasPrefix(value, "alt+") {
		rest := strings.TrimPrefix(strings.TrimPrefix(value, "ctrl+"), "alt+")
		return len([]rune(rest)) == 1 || rest == "enter" || rest == "up" || rest == "down"
	}
	return len([]rune(value)) == 1
}

func validateTheme(t ThemeFile) error {
	if t.Version != 1 || t.Name == "" {
		return errors.New("theme requires version: 1 and name")
	}
	if len([]rune(t.Name)) > 64 {
		return errors.New("theme name exceeds 64 runes")
	}
	for _, r := range t.Name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/\\`, r) {
			return fmt.Errorf("invalid theme name %q", t.Name)
		}
	}
	if IsBuiltInTUITheme(t.Name) {
		return fmt.Errorf("theme name %q is reserved", t.Name)
	}
	if t.Extends == "" {
		t.Extends = "default"
	}
	if !IsBuiltInTUITheme(t.Extends) {
		return fmt.Errorf("theme extends unsupported built-in %q", t.Extends)
	}
	pairs := []AdaptiveColor{t.Colors.Accent, t.Colors.Muted, t.Colors.Foreground, t.Colors.Warning, t.Colors.Error, t.Colors.Success, t.Colors.Separator}
	for _, pair := range pairs {
		if err := validateColor(pair.Light); err != nil {
			return err
		}
		if err := validateColor(pair.Dark); err != nil {
			return err
		}
	}
	return nil
}

func validateColor(value string) error {
	if value == "" {
		return nil
	}
	if len(value) == 7 && value[0] == '#' {
		for _, r := range value[1:] {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return fmt.Errorf("invalid color %q", value)
			}
		}
		return nil
	}
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err == nil && n >= 0 && n <= 255 && fmt.Sprintf("%d", n) == value {
		return nil
	}
	return fmt.Errorf("invalid color %q (use #RRGGBB or 0..255)", value)
}

func decodeAuxScoped(confinedRoot, path string, out any) error {
	if confinedRoot == "" {
		return decodeAux(path, out)
	}
	rel, err := filepath.Rel(confinedRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("auxiliary config escapes trusted project root")
	}
	root, err := os.OpenRoot(confinedRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := validateAuxRootPath(root, rel); err != nil {
		return err
	}
	before, err := root.Lstat(rel)
	if err != nil {
		return err
	}
	file, err := root.Open(rel)
	if err != nil {
		return err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return errors.New("auxiliary config changed while opening")
	}
	return decodeAuxFile(file, after, out)
}

func validateAuxPath(rootPath, path string) error {
	rel, err := filepath.Rel(rootPath, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("auxiliary config escapes trusted project root")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	return validateAuxRootPath(root, rel)
}

func validateAuxRootPath(root *os.Root, rel string) error {
	current := ""
	for part := range strings.SplitSeq(filepath.Clean(rel), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		info, statErr := root.Lstat(current)
		if errors.Is(statErr, fs.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("auxiliary config path contains a symlink")
		}
	}
	return nil
}

func decodeAux(path string, out any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("auxiliary config must be a regular non-symlink file")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return errors.New("auxiliary config changed while opening")
	}
	return decodeAuxFile(file, opened, out)
}

func decodeAuxFile(file io.Reader, opened fs.FileInfo, out any) error {
	if !opened.Mode().IsRegular() {
		return errors.New("auxiliary config must remain a regular file")
	}
	if opened.Size() > AuxiliaryFileLimit {
		return fmt.Errorf("auxiliary config exceeds %d bytes", AuxiliaryFileLimit)
	}
	data, err := io.ReadAll(io.LimitReader(file, AuxiliaryFileLimit+1))
	if err != nil {
		return err
	}
	if len(data) > AuxiliaryFileLimit {
		return fmt.Errorf("auxiliary config exceeds %d bytes", AuxiliaryFileLimit)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple YAML documents are not supported")
		}
		return err
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
