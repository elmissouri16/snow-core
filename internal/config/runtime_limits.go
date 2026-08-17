package config

import (
	"time"
)

// BashTimeout returns the configured bash timeout as a duration.
func (c Config) BashTimeout() time.Duration {
	if c.BashTimeoutMS <= 0 {
		return DefaultBashTimeout
	}
	return time.Duration(c.BashTimeoutMS) * time.Millisecond
}

// ToolOutputLimit returns the tool output byte cap.
func (c Config) ToolOutputLimit() int {
	if c.ToolOutputBytes <= 0 {
		return DefaultToolOutputBytes
	}
	return c.ToolOutputBytes
}
