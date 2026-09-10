package tui

import tea "charm.land/bubbletea/v2"

func isMouseClick(msg tea.MouseMsg) bool   { _, ok := msg.(tea.MouseClickMsg); return ok }
func isMouseRelease(msg tea.MouseMsg) bool { _, ok := msg.(tea.MouseReleaseMsg); return ok }
func isMouseMotion(msg tea.MouseMsg) bool  { _, ok := msg.(tea.MouseMotionMsg); return ok }
func isMouseWheel(msg tea.MouseMsg) bool   { _, ok := msg.(tea.MouseWheelMsg); return ok }
