package skills

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/elmissouri16/snow-core/internal/config"
)

// Built-in skills are immutable binary assets. They never require project trust
// or extraction into a writable cache. User/explicit/trusted project skills
// retain their ordinary higher-precedence ranks.
//
//go:embed bundled/snow-js-plugin
var bundledSkills embed.FS

func (r *Registry) discoverBundled(maxBytes int64) {
	const name = "snow-js-plugin"
	const directory = "builtin:" + name
	const location = directory + "/SKILL.md"
	resources, err := fs.Sub(bundledSkills, "bundled/"+name)
	if err != nil {
		r.diagnostics = append(r.diagnostics, Diagnostic{Path: location, Level: "error", Message: err.Error()})
		return
	}
	data, err := readBundledResource(Skill{resources: resources}, "SKILL.md", maxBytes)
	if err != nil {
		r.diagnostics = append(r.diagnostics, Diagnostic{Path: location, Level: "error", Message: err.Error()})
		return
	}
	// Supply a conventional basename to the portable format validator, then
	// expose a virtual address that cannot be mistaken for an extracted file.
	skill, diagnostics, err := parseSkillData(data, location, name)
	r.diagnostics = append(r.diagnostics, diagnostics...)
	if err != nil {
		return
	}
	skill.ExplicitOnly = true
	skill.Directory = directory
	skill.Scope, skill.Source, skill.rank = "builtin", "builtin", 0
	skill.resources = resources
	r.allByName[skill.Name] = skill
}

func readBundledResource(skill Skill, name string, maxBytes int64) ([]byte, error) {
	name = filepath.ToSlash(name)
	if !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") {
		return nil, errors.New("resource path must stay inside the skill directory")
	}
	file, err := skill.resources.Open(name)
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
	if maxBytes < 0 || info.Size() > maxBytes {
		return nil, fmt.Errorf("skill resource exceeds %d bytes", maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, min(maxBytes, info.Size())+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("skill resource exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func listBundledResources(ctx context.Context, skill Skill, limit int) ([]string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var resources []string
	seen := 0
	truncated := false
	err := fs.WalkDir(skill.resources, ".", func(name string, entry fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		seen++
		if seen > 2000 {
			truncated = true
			return fs.SkipAll
		}
		if entry.IsDir() {
			if strings.Count(name, "/") >= 5 || entry.Name() == ".git" || config.IsDefaultGeneratedDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || name == "SKILL.md" {
			return nil
		}
		if len(resources) >= limit {
			truncated = true
			return fs.SkipAll
		}
		resources = append(resources, name)
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	slices.Sort(resources)
	return resources, truncated, nil
}
