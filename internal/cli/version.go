// Package cli implements Snow's command-line surfaces and management commands.
// Runtime wiring and turn execution remain in app and agent.
package cli

import "github.com/elmissouri16/snow-core/internal/buildinfo"

var version = buildinfo.Version
