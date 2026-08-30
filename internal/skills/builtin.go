package skills

import (
	"cmp"
	"embed"
	"fmt"
	"io/fs"
	pathpkg "path"
	"slices"
)

// builtinSkillsFS keeps Snow's own skills available from the installed single
// binary. Built-ins are immutable and rank below every filesystem discovery
// root, so users and trusted projects can shadow them with the standard format.
//
//go:embed builtin
var builtinSkillsFS embed.FS

func discoverBuiltins(maxFileSize int64) (skills []Skill, diagnostics []Diagnostic) {
	entries, err := fs.ReadDir(builtinSkillsFS, "builtin")
	if err != nil {
		return nil, []Diagnostic{{Path: "snow-builtin://", Level: "error", Message: err.Error()}}
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		root, err := fs.Sub(builtinSkillsFS, pathpkg.Join("builtin", entry.Name()))
		location := "snow-builtin://" + entry.Name() + "/SKILL.md"
		directory := "snow-builtin://" + entry.Name()
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: directory, Level: "error", Message: err.Error()})
			continue
		}
		data, err := readBoundedFS(root, "SKILL.md", maxFileSize)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: location, Level: "error", Message: err.Error()})
			continue
		}
		skill, parsedDiagnostics, err := parseSkillData(data, location, directory)
		diagnostics = append(diagnostics, parsedDiagnostics...)
		if err != nil {
			if err != errNonconformant {
				diagnostics = append(diagnostics, Diagnostic{Path: location, Level: "error", Message: fmt.Sprintf("embedded skill: %v", err)})
			}
			continue
		}
		skill.Scope, skill.Source, skill.rank = "builtin", "snow", 0
		skill.embeddedRoot = root
		skills = append(skills, skill)
	}
	return skills, diagnostics
}
