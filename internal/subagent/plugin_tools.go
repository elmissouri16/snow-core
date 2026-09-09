package subagent

func (m *Manager) SetPluginToolSelection(selectTools func(Role, []string) (map[string]string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pluginSelection = selectTools
}
