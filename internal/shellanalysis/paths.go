package shellanalysis

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/elmissouri16/snow-core/internal/permission"
)

func (a *analyzer) addPathEffect(operation, value, command, confidence string, st *state) {
	if value == "" {
		a.addUnknown("unresolved path for " + command)
		return
	}
	if value == "-" {
		return
	}
	if !filepath.IsAbs(value) && st.cwd == "" {
		a.addUnknown("working directory is unresolved for " + command)
		return
	}
	path := a.resolvePath(value, st.cwd)
	if resolved, changed := a.resolver.resolve(path, operation == "delete"); changed {
		path = resolved
		a.rememberable = false
	}
	capability := a.pathCapability(operation, path)
	reason := operation + " filesystem path"
	if protected := a.protectedCapability(operation, path); protected != "" {
		capability = protected
		switch protected {
		case permission.CapabilityProtectedResourceAccess:
			reason = "operator-protected resource access"
		case permission.CapabilityCredentialsRead:
			reason = "protected credential read"
		case permission.CapabilitySSHAuthorizationWrite:
			reason = "SSH authorization change"
		case permission.CapabilityRawDeviceAccess:
			reason = "raw device access"
		case permission.CapabilityDockerSocketAccess:
			reason = "container control socket access"
		case permission.CapabilityPersistenceWrite:
			reason = "persistent startup configuration"
		}
	}
	a.addEffect(permission.Effect{Type: "filesystem", Capability: capability, Operation: operation, Resource: path, Command: command, Reason: reason, Confidence: confidence})
}

func (a *analyzer) resolvePath(value, cwd string) string {
	if !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
	}
	return filepath.Clean(value)
}

func resolvePathSymlinks(path string, preserveFinal bool) (string, bool) {
	return (&pathResolver{}).resolve(path, preserveFinal)
}

type resolvedPath struct {
	path   string
	exists bool
}

// Cache filesystem observations only for this preflight. Never reuse stale
// symlink state across approvals; cap cache memory independently of AST size.
type pathResolver struct{ entries map[string]resolvedPath }

func (r *pathResolver) resolve(path string, preserveFinal bool) (string, bool) {
	original := filepath.Clean(path)
	current := original
	parts := make([]string, 0, 4)
	if preserveFinal {
		parts = append(parts, filepath.Base(current))
		current = filepath.Dir(current)
	}
	for {
		entry, cached := r.entries[current]
		if !cached {
			// A missing leaf needs no full component walk. Sibling paths share
			// their resolved ancestor instead of repeating that walk.
			if _, err := os.Lstat(current); err == nil {
				if resolved, err := filepath.EvalSymlinks(current); err == nil {
					entry = resolvedPath{resolved, true}
				}
			}
			if len(r.entries) < 256 {
				if r.entries == nil {
					r.entries = make(map[string]resolvedPath)
				}
				r.entries[current] = entry
			}
		}
		if entry.exists {
			resolved := entry.path
			for i := len(parts) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, parts[i])
			}
			resolved = filepath.Clean(resolved)
			return resolved, resolved != original
		}
		parent := filepath.Dir(current)
		if parent == current {
			return original, false
		}
		parts = append(parts, filepath.Base(current))
		current = parent
	}
}

func (a *analyzer) pathCapability(operation, path string) permission.Capability {
	workspace := false
	for _, root := range a.roots {
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			workspace = true
			break
		}
	}
	switch operation {
	case "read":
		if workspace {
			return permission.CapabilityFilesystemReadWorkspace
		}
		return permission.CapabilityFilesystemReadExternal
	case "delete":
		if workspace {
			return permission.CapabilityFilesystemDeleteWorkspace
		}
		return permission.CapabilityFilesystemDeleteExternal
	default:
		if workspace {
			return permission.CapabilityFilesystemWriteWorkspace
		}
		return permission.CapabilityFilesystemWriteExternal
	}
}
