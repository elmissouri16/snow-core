// Package buildinfo exposes build metadata shared by every Snow surface.
package buildinfo

// Version is the Snow semantic version. Release builds override it with:
//
//	-X github.com/elmissouri16/snow-core/internal/buildinfo.Version=<version>
var Version = "0.1.0-dev"
