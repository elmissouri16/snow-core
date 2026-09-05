package shellanalysis

import (
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
)

//go:embed protected_paths.json
var pathSpecification []byte

type pathRule struct {
	Capability     permission.Capability `json:"capability"`
	Operation      string                `json:"operation"`
	Path           string                `json:"path"`
	Match          string                `json:"match"`
	BasenamePrefix string                `json:"basename_prefix"`
	ExcludeSuffix  string                `json:"exclude_suffix"`
}

var defaultPathRules, pathSpecificationError = loadPathRules()

func loadPathRules() ([]pathRule, error) {
	var rules []pathRule
	err := json.Unmarshal(pathSpecification, &rules, json.RejectUnknownMembers(true))
	return rules, err
}

func preparePathRules(home string, additional []string, resolver *pathResolver) ([]pathRule, error) {
	if specificationError != nil {
		return nil, fmt.Errorf("shell command specification: %w", specificationError)
	}
	if pathSpecificationError != nil {
		return nil, fmt.Errorf("shell path specification: %w", pathSpecificationError)
	}
	if len(additional) > 128 {
		return nil, fmt.Errorf("shell protected paths exceed 128 entries")
	}
	rules := make([]pathRule, 0, len(defaultPathRules)+len(additional))
	for _, rule := range defaultPathRules {
		if after, ok := strings.CutPrefix(rule.Path, "~/"); ok {
			if home == "" {
				continue
			}
			rule.Path = filepath.Join(home, after)
		}
		if rule.Match == "exact" || rule.Match == "tree" {
			resolved, _ := resolver.resolve(rule.Path, false)
			rule.Path = protectedPath(resolved)
		} else {
			rule.Path = protectedPath(rule.Path)
		}
		rules = append(rules, rule)
	}
	for _, path := range additional {
		if !filepath.IsAbs(path) || len(path) > maxValueBytes || strings.ContainsRune(path, 0) {
			return nil, fmt.Errorf("shell protected paths must be bounded absolute paths")
		}
		resolved, _ := resolver.resolve(path, false)
		rules = append(rules, pathRule{Capability: permission.CapabilityProtectedResourceAccess, Operation: "any", Path: protectedPath(resolved), Match: "tree"})
	}
	return rules, nil
}

func (a *analyzer) protectedCapability(operation, path string) permission.Capability {
	path = protectedPath(path)
	for _, rule := range a.policy {
		if rule.Operation != "any" && rule.Operation != operation && !(rule.Operation == "mutate" && operation != "read") {
			continue
		}
		matched := false
		switch rule.Match {
		case "exact":
			matched = path == rule.Path
		case "tree":
			matched = path == rule.Path || strings.HasPrefix(path, rule.Path+string(filepath.Separator))
		case "prefix":
			matched = strings.HasPrefix(path, rule.Path)
		case "suffix":
			matched = strings.HasSuffix(path, rule.Path)
		}
		if !matched {
			continue
		}
		base := filepath.Base(path)
		if rule.BasenamePrefix != "" && !strings.HasPrefix(base, rule.BasenamePrefix) {
			continue
		}
		if rule.ExcludeSuffix != "" && strings.HasSuffix(base, rule.ExcludeSuffix) {
			continue
		}
		return rule.Capability
	}
	return ""
}

func protectedLiteral(path string) string {
	resolved, _ := resolvePathSymlinks(path, false)
	return protectedPath(resolved)
}

func protectedPath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
