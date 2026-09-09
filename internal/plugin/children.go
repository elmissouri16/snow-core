package plugin

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/elmissouri16/snow-core/internal/tools"
	publicplugin "github.com/elmissouri16/snow-core/pkg/plugin"
)

type childPlugin interface {
	ChildToolUses(string) ([]string, bool)
	ChildPlugin([]string) (publicplugin.Plugin, error)
}

func (m *Manager) SelectChildTools(names []string, allowed func(string) bool) (map[string]string, error) {
	if len(names) > 32 {
		return nil, errors.New("at most 32 child plugin tools may be selected")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for _, name := range names {
		found := false
		for _, p := range m.plugins {
			cp, ok := p.goPlugin.(childPlugin)
			if !ok {
				continue
			}
			uses, ok := cp.ChildToolUses(name)
			if !ok {
				continue
			}
			if !allowed(name) {
				return nil, fmt.Errorf("role does not allow %s", name)
			}
			for _, use := range uses {
				if use != "storage" && !allowed(use) {
					return nil, fmt.Errorf("child cannot use %s required by %s", use, name)
				}
			}
			out[name] = p.fingerprint
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("child plugin tool unavailable: %s", name)
		}
	}
	return out, nil
}
func (m *Manager) CloneChild(ctx context.Context, reg tools.DescriptorRegistry, selected map[string]string, opts ManagerOptions) (*Manager, error) {
	child := NewManager(reg, opts)
	m.mu.Lock()
	parents := slices.Clone(m.plugins)
	m.mu.Unlock()
	count := 0
	for _, p := range parents {
		cp, ok := p.goPlugin.(childPlugin)
		if !ok {
			continue
		}
		var names []string
		for name, fingerprint := range selected {
			if _, ok := cp.ChildToolUses(name); ok {
				if fingerprint != p.fingerprint {
					_ = child.Close(context.Background())
					return nil, fmt.Errorf("child plugin %s changed; create a new child", p.id)
				}
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			continue
		}
		slices.Sort(names)
		clone, err := cp.ChildPlugin(names)
		if err != nil {
			_ = child.Close(context.Background())
			return nil, err
		}
		if err := child.LoadJavaScript(clone, p.fingerprint); err != nil {
			_ = child.Close(context.Background())
			return nil, err
		}
		count += len(names)
	}
	if count != len(selected) {
		_ = child.Close(context.Background())
		return nil, errors.New("selected child plugin is no longer available")
	}
	if err := child.Initialize(ctx); err != nil {
		_ = child.Close(context.Background())
		return nil, err
	}
	return child, nil
}
