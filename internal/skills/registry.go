package skills

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"

	"github.com/elmissouri16/snow-core/internal/tools/builtin"
)

// Discover scans standard user and project paths. Higher-ranked project and
// Snow-native locations deterministically override cross-client locations.
func Discover(opts Options) *Registry {
	maxSkills := opts.MaxSkills
	if maxSkills <= 0 {
		maxSkills = defaultMaxSkills
	}
	maxFile := opts.MaxSkillFileSize
	if maxFile <= 0 {
		maxFile = defaultMaxSkillFile
	}
	r := &Registry{byName: make(map[string]Skill), allByName: make(map[string]Skill), maxFileSize: maxFile}
	if opts.IncludeBuiltins {
		builtins, diagnostics := discoverBuiltins(maxFile)
		r.diagnostics = append(r.diagnostics, diagnostics...)
		for _, skill := range builtins {
			r.allByName[skill.Name] = skill
		}
	}

	home := opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	snowHome := opts.SnowHome
	if snowHome == "" && home != "" {
		snowHome = filepath.Join(home, ".snow")
	}
	var dirs []sourceDir
	if opts.IncludeClaude && home != "" {
		dirs = append(dirs, sourceDir{filepath.Join(home, ".claude", "skills"), "user", "claude", 10})
	}
	if home != "" {
		dirs = append(dirs, sourceDir{filepath.Join(home, ".agents", "skills"), "user", "agents", 20})
	}
	if snowHome != "" {
		dirs = append(dirs, sourceDir{filepath.Join(snowHome, "skills"), "user", "snow", 30})
	}
	for _, dir := range opts.ExtraDirs {
		dirs = append(dirs, sourceDir{dir, "explicit", "configured", 100})
	}
	if opts.ProjectTrusted && opts.CWD != "" {
		if opts.IncludeClaude {
			dirs = append(dirs, sourceDir{filepath.Join(opts.CWD, ".claude", "skills"), "project", "claude", 210})
		}
		dirs = append(dirs,
			sourceDir{filepath.Join(opts.CWD, ".agents", "skills"), "project", "agents", 220},
			sourceDir{filepath.Join(opts.CWD, ".snow", "skills"), "project", "snow", 230},
		)
	} else if opts.CWD != "" {
		for _, path := range []string{filepath.Join(opts.CWD, ".agents", "skills"), filepath.Join(opts.CWD, ".snow", "skills")} {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: path, Level: "warning", Message: "project skills require an explicit trust allow"})
			}
		}
	}

	seenLocations := make(map[string]bool)
	candidateCount := 0
scanDirs:
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir.path)
		if err != nil || seenLocations[abs] {
			continue
		}
		seenLocations[abs] = true
		entries, err := os.ReadDir(abs)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: abs, Level: "warning", Message: err.Error()})
			}
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if candidateCount >= maxSkills {
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: abs, Level: "warning", Message: fmt.Sprintf("skill discovery stopped at %d candidates", maxSkills)})
				break scanDirs
			}
			candidateCount++
			directory := filepath.Join(abs, entry.Name())
			location := filepath.Join(directory, "SKILL.md")
			root, identity, err := openPinnedSkillRoot(directory)
			if err != nil {
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: directory, Level: "error", Message: err.Error()})
				continue
			}
			skill, diagnostics, err := parseRoot(root, location, maxFile)
			closeErr := root.Close()
			r.diagnostics = append(r.diagnostics, diagnostics...)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errNonconformant) {
					r.diagnostics = append(r.diagnostics, Diagnostic{Path: location, Level: "error", Message: err.Error()})
				}
				continue
			}
			if closeErr != nil {
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: directory, Level: "error", Message: closeErr.Error()})
				continue
			}
			skill.identity = identity
			skill.Scope, skill.Source, skill.rank = dir.scope, dir.source, dir.rank
			if prior, exists := r.allByName[skill.Name]; exists {
				if prior.rank > skill.rank {
					r.diagnostics = append(r.diagnostics, Diagnostic{Path: location, Skill: skill.Name, Level: "warning", Message: "skill shadowed by higher-precedence " + prior.Location})
					continue
				}
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: prior.Location, Skill: skill.Name, Level: "warning", Message: "skill shadowed by higher-precedence " + location})
			}
			r.allByName[skill.Name] = skill
		}
	}
	candidates := make([]Skill, 0, len(r.allByName))
	for _, skill := range r.allByName {
		skill.Enabled = !opts.Disabled
		if !skill.Enabled {
			skill.DisabledBy = opts.DisabledReason
			if skill.DisabledBy == "" {
				skill.DisabledBy = "skills disabled by configuration"
			}
		}
		if enabled, ok := opts.Overrides[skill.Name]; ok {
			skill.Enabled = enabled
			skill.DisabledBy = ""
			if !enabled {
				skill.DisabledBy = opts.OverrideReasons[skill.Name]
				if skill.DisabledBy == "" {
					skill.DisabledBy = "disabled by named skill policy"
				}
			}
		}
		candidates = append(candidates, skill)
	}
	// Prefer higher-precedence locations when the bounded catalog cannot admit
	// every otherwise-valid entry, then sort the admitted/provider view by name.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank > candidates[j].rank
		}
		return candidates[i].Name < candidates[j].Name
	})
	catalogLimit := opts.MaxCatalogBytes
	if catalogLimit <= 0 {
		catalogLimit = defaultCatalogBytes
	}
	catalogBytes := len(catalogPromptPrefix(true)) + len("</available_skills>")
	for _, skill := range candidates {
		if skill.Enabled {
			entryBytes := len(catalogPromptEntry(skill))
			if catalogBytes+entryBytes > catalogLimit {
				skill.Enabled = false
				skill.DisabledBy = catalogDisabledReason
				r.diagnostics = append(r.diagnostics, Diagnostic{Path: skill.Location, Skill: skill.Name, Level: "warning", Message: catalogDisabledReason})
			} else {
				catalogBytes += entryBytes
			}
		}
		r.allByName[skill.Name] = skill
		r.allOrdered = append(r.allOrdered, skill)
		if skill.Enabled {
			r.byName[skill.Name] = skill
			r.ordered = append(r.ordered, skill)
		}
	}
	sort.Slice(r.ordered, func(i, j int) bool { return r.ordered[i].Name < r.ordered[j].Name })
	sort.Slice(r.allOrdered, func(i, j int) bool { return r.allOrdered[i].Name < r.allOrdered[j].Name })
	return r
}

func parseRoot(root *os.Root, location string, maxBytes int64) (Skill, []Diagnostic, error) {
	data, err := readFrontmatterRoot(root, "SKILL.md", maxBytes)
	if err != nil {
		return Skill{}, nil, err
	}
	return parseSkillData(data, location, filepath.Dir(location))
}

func parseSkillData(data []byte, location, directory string) (Skill, []Diagnostic, error) {
	meta, _, err := split(data)
	if err != nil {
		return Skill{}, nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(meta, &raw); err != nil {
		return Skill{}, nil, fmt.Errorf("parse YAML frontmatter: %w", err)
	}
	var fm frontmatter
	if err := yaml.Unmarshal(meta, &fm); err != nil {
		return Skill{}, nil, fmt.Errorf("parse YAML frontmatter: %w", err)
	}
	rawDescription := fm.Description
	rawCompatibility := fm.Compatibility
	fm.Name = norm.NFKC.String(strings.TrimSpace(fm.Name))
	fm.Description = strings.TrimSpace(fm.Description)
	skill := Skill{Name: fm.Name, Description: fm.Description, License: strings.TrimSpace(fm.License), Compatibility: strings.TrimSpace(fm.Compatibility), Metadata: fm.Metadata, AllowedTools: strings.TrimSpace(fm.AllowedTools), Location: location, Directory: directory}
	var diagnostics []Diagnostic
	invalid := func(message string) {
		diagnostics = append(diagnostics, Diagnostic{Path: location, Skill: skill.Name, Level: "error", Message: message})
	}
	for _, field := range []string{"name", "description", "license", "compatibility", "allowed-tools"} {
		if value, present := raw[field]; present {
			if _, ok := value.(string); !ok {
				invalid(fmt.Sprintf("frontmatter field %q must be a string", field))
			}
		}
	}
	if value, present := raw["metadata"]; present {
		mapping, ok := value.(map[string]any)
		if !ok || mapping == nil {
			invalid("frontmatter field \"metadata\" must be a string-to-string mapping")
		} else {
			for key, metadataValue := range mapping {
				if _, ok := metadataValue.(string); !ok {
					invalid(fmt.Sprintf("metadata value %q must be a string", key))
				}
			}
		}
	}
	if skill.Name == "" {
		invalid("missing required name")
	} else if !validSkillName(skill.Name) {
		invalid("name does not satisfy the Agent Skills naming constraints")
	}
	if norm.NFKC.String(pathpkg.Base(filepath.ToSlash(directory))) != skill.Name {
		invalid("name must match the parent directory")
	}
	if skill.Description == "" {
		invalid("missing required description")
	} else if utf8.RuneCountInString(rawDescription) > 1024 {
		invalid("description exceeds 1024 characters")
	}
	if _, present := raw["compatibility"]; present {
		if skill.Compatibility == "" {
			invalid("compatibility must be non-empty when provided")
		} else if utf8.RuneCountInString(rawCompatibility) > 500 {
			invalid("compatibility exceeds 500 characters")
		}
	}
	for field := range raw {
		if _, ok := allowedFrontmatterFields[field]; !ok {
			invalid(fmt.Sprintf("unexpected frontmatter field %q", field))
		}
	}
	if len(diagnostics) > 0 {
		return Skill{}, diagnostics, errNonconformant
	}
	return skill, nil, nil
}

func validSkillName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 64 || name != strings.ToLower(name) || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for _, r := range name {
		if r != '-' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

func split(data []byte) (meta, body []byte, err error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, nil, errors.New("SKILL.md must start with YAML frontmatter")
	}
	end := bytes.Index(data[4:], []byte("\n---\n"))
	if end < 0 {
		if bytes.HasSuffix(data, []byte("\n---")) {
			end = max(4, len(data)-len("\n---"))
			return data[4:end], nil, nil
		}
		return nil, nil, errors.New("SKILL.md has no closing frontmatter delimiter")
	}
	end += 4
	return data[4:end], bytes.TrimSpace(data[end+5:]), nil
}

func openPinnedSkillRoot(path string) (*os.Root, fs.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("skill path is not a real directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	after, err := os.Lstat(path)
	if err != nil || !after.IsDir() || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(after, opened) {
		_ = root.Close()
		return nil, nil, errors.New("skill directory changed while it was being opened")
	}
	return root, opened, nil
}

func openSkillRoot(skill Skill) (*os.Root, error) {
	root, identity, err := openPinnedSkillRoot(skill.Directory)
	if err != nil {
		return nil, err
	}
	if skill.identity == nil || !os.SameFile(skill.identity, identity) {
		_ = root.Close()
		return nil, errors.New("skill directory no longer matches the discovered directory")
	}
	return root, nil
}

func readFrontmatterRoot(root *os.Root, name string, maxBytes int64) ([]byte, error) {
	if root == nil {
		return nil, errors.New("skill directory is closed")
	}
	file, info, err := builtin.OpenRootedRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds %d-byte limit", maxBytes)
	}
	var data []byte
	buf := make([]byte, 32<<10)
	for int64(len(data)) <= maxBytes {
		n, readErr := file.Read(buf)
		data = append(data, buf[:n]...)
		normalized := bytes.ReplaceAll(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), []byte("\r\n"), []byte("\n"))
		start := 0
		if len(normalized) >= 4 {
			start = 4
		}
		if bytes.Index(normalized[start:], []byte("\n---\n")) >= 0 {
			return data, nil
		}
		if readErr == io.EOF {
			return data, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return nil, fmt.Errorf("file exceeds %d-byte limit", maxBytes)
}

func readBoundedRoot(root *os.Root, name string, maxBytes int64) ([]byte, error) {
	if root == nil {
		return nil, errors.New("skill directory is closed")
	}
	file, info, err := builtin.OpenRootedRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBoundedFile(file, info, maxBytes)
}

func readBoundedFS(root fs.FS, name string, maxBytes int64) ([]byte, error) {
	if root == nil || !fs.ValidPath(name) {
		return nil, errors.New("invalid embedded skill resource path")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("skill resource is not a regular file")
	}
	return readBoundedFile(file, info, maxBytes)
}

func readBoundedFile(file io.Reader, info fs.FileInfo, maxBytes int64) ([]byte, error) {
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds %d-byte limit", maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d-byte limit", maxBytes)
	}
	return data, nil
}

// List returns a defensive, name-sorted skill catalog.
func (r *Registry) List() []Skill {
	if r == nil {
		return nil
	}
	return append([]Skill(nil), r.ordered...)
}

// Inventory returns every discovered effective skill, including entries that
// policy suppresses from prompts and activation tools.
func (r *Registry) Inventory() []Skill {
	if r == nil {
		return nil
	}
	return append([]Skill(nil), r.allOrdered...)
}

// Lookup returns discovered metadata regardless of its enabled state.
func (r *Registry) Lookup(name string) (Skill, bool) {
	if r == nil {
		return Skill{}, false
	}
	skill, ok := r.allByName[name]
	return skill, ok
}

// ResourceSummary returns the bounded resource inventory size for a discovered
// skill without loading any resource contents.
func (r *Registry) ResourceSummary(name string) (count int, truncated bool, err error) {
	skill, ok := r.Lookup(name)
	if !ok {
		return 0, false, fmt.Errorf("unknown skill %q", name)
	}
	resources, truncated, err := listResources(context.Background(), skill, 200)
	return len(resources), truncated, err
}

// Get returns a discovered skill by frontmatter name.
func (r *Registry) Get(name string) (Skill, bool) {
	if r == nil {
		return Skill{}, false
	}
	s, ok := r.byName[name]
	return s, ok
}

// Diagnostics returns discovery warnings and errors.
func (r *Registry) Diagnostics() []Diagnostic {
	if r == nil {
		return nil
	}
	return append([]Diagnostic(nil), r.diagnostics...)
}

// DisableAll removes every currently enabled skill from runtime/provider
// surfaces while retaining the complete management inventory.
func (r *Registry) DisableAll(reason string) {
	if r == nil {
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "skills disabled by runtime policy"
	}
	for i, skill := range r.allOrdered {
		if skill.Enabled {
			skill.Enabled = false
			skill.DisabledBy = reason
			r.allOrdered[i] = skill
			r.allByName[skill.Name] = skill
		}
	}
	clear(r.byName)
	r.ordered = nil
}

// Close is retained as the registry lifecycle hook. Resource directory handles
// are opened per operation and closed immediately, so there is currently no
// long-lived state to release.
func (r *Registry) Close() error { return nil }

// CatalogPrompt returns tier-one progressive disclosure for the standard Snow
// skill tool set.
func (r *Registry) CatalogPrompt() string { return r.CatalogPromptForTools(true) }

// CatalogPromptForTools omits resource-reader instructions when an explicit
// tool allowlist excludes that optional capability.
func (r *Registry) CatalogPromptForTools(resourceReader bool) string {
	if r == nil || len(r.ordered) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(catalogPromptPrefix(resourceReader))
	for _, skill := range r.ordered {
		b.WriteString(catalogPromptEntry(skill))
	}
	b.WriteString("</available_skills>")
	return b.String()
}

func catalogPromptPrefix(resourceReader bool) string {
	prefix := "The following Agent Skills provide specialized instructions. When a task matches a description, call activate_skill with its name before proceeding. A prompt beginning with $skill-name activates that skill directly. Relative paths are rooted at the activated skill directory."
	if resourceReader {
		prefix += " Use read_skill_resource for referenced files."
	}
	return prefix + "\n<available_skills>\n"
}

func catalogPromptEntry(skill Skill) string {
	var b strings.Builder
	b.WriteString("  <skill><name>")
	_ = xml.EscapeText(&b, []byte(skill.Name))
	b.WriteString("</name><description>")
	_ = xml.EscapeText(&b, []byte(skill.Description))
	b.WriteString("</description></skill>\n")
	return b.String()
}

func (r *Registry) load(name string) (Skill, []byte, error) {
	skill, ok := r.Get(name)
	if !ok {
		return Skill{}, nil, fmt.Errorf("unknown skill %q", name)
	}
	var data []byte
	var err error
	if skill.embeddedRoot != nil {
		data, err = readBoundedFS(skill.embeddedRoot, "SKILL.md", r.maxFileSize)
	} else {
		var root *os.Root
		root, err = openSkillRoot(skill)
		if err == nil {
			defer root.Close()
			data, err = readBoundedRoot(root, "SKILL.md", r.maxFileSize)
		}
	}
	if err != nil {
		return Skill{}, nil, err
	}
	_, body, err := split(data)
	return skill, body, err
}

func (r *Registry) readResource(skill Skill, name string, maxBytes int64) ([]byte, error) {
	if skill.embeddedRoot != nil {
		return readBoundedFS(skill.embeddedRoot, filepath.ToSlash(name), maxBytes)
	}
	root, err := openSkillRoot(skill)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readBoundedRoot(root, name, maxBytes)
}

func listResources(ctx context.Context, skill Skill, limit int) ([]string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if skill.embeddedRoot != nil {
		return listFSResources(ctx, skill.embeddedRoot, limit)
	}
	root, err := openSkillRoot(skill)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	type directory struct {
		path  string
		depth int
	}
	stack := []directory{{path: "."}}
	var resources []string
	entriesSeen := 0
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		dir, err := root.Open(current.path)
		if err != nil {
			return nil, false, err
		}
		for {
			if err := ctx.Err(); err != nil {
				_ = dir.Close()
				return nil, false, err
			}
			entries, readErr := dir.ReadDir(64)
			for _, entry := range entries {
				entriesSeen++
				if entriesSeen > 2000 {
					_ = dir.Close()
					sort.Strings(resources)
					return resources, true, nil
				}
				if entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				resourcePath := entry.Name()
				if current.path != "." {
					resourcePath = pathpkg.Join(current.path, entry.Name())
				}
				if entry.IsDir() {
					if current.depth < 5 && entry.Name() != ".git" && entry.Name() != "node_modules" {
						stack = append(stack, directory{path: resourcePath, depth: current.depth + 1})
					}
					continue
				}
				if resourcePath == "SKILL.md" {
					continue
				}
				if len(resources) >= limit {
					_ = dir.Close()
					sort.Strings(resources)
					return resources, true, nil
				}
				resources = append(resources, resourcePath)
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				_ = dir.Close()
				return nil, false, readErr
			}
		}
		if err := dir.Close(); err != nil {
			return nil, false, err
		}
	}
	sort.Strings(resources)
	return resources, false, nil
}

func listFSResources(ctx context.Context, root fs.FS, limit int) ([]string, bool, error) {
	type directory struct {
		path  string
		depth int
	}
	stack := []directory{{path: "."}}
	var resources []string
	entriesSeen := 0
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		entries, err := fs.ReadDir(root, current.path)
		if err != nil {
			return nil, false, err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
			entriesSeen++
			if entriesSeen > 2000 {
				sort.Strings(resources)
				return resources, true, nil
			}
			resourcePath := entry.Name()
			if current.path != "." {
				resourcePath = pathpkg.Join(current.path, entry.Name())
			}
			if entry.IsDir() {
				if current.depth < 5 && entry.Name() != ".git" && entry.Name() != "node_modules" {
					stack = append(stack, directory{path: resourcePath, depth: current.depth + 1})
				}
				continue
			}
			if resourcePath == "SKILL.md" {
				continue
			}
			if len(resources) >= limit {
				sort.Strings(resources)
				return resources, true, nil
			}
			resources = append(resources, resourcePath)
		}
	}
	sort.Strings(resources)
	return resources, false, nil
}
