package config

import "errors"

// ValidateTerminalNotifications validates the operator's terminal alert policy.
func ValidateTerminalNotifications(value string) error {
	switch value {
	case "off", "unfocused", "always":
		return nil
	default:
		return errors.New("config: tui.notifications must be off, unfocused, or always")
	}
}
