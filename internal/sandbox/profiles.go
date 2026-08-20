package sandbox

import "strings"

// Profile is a Snow-audited, digest-pinned development image selection. Built-in
// profiles deliberately enable persistent guest networking so their package
// managers work after initialization; the TUI makes that authority visible.
type Profile struct {
	ID          string
	Name        string
	Description string
	Source      string
	Network     bool
	CPUs        int
	MemoryMiB   int
}

var builtinProfiles = []Profile{
	{
		ID: "ubuntu", Name: "Minimal Ubuntu", Description: "Ubuntu 24.04 base tools and apt",
		Source: "index.docker.io/library/ubuntu:24.04@sha256:561618e2c15bf2397621dd04f96926663a3b5616c189cf7e38db7e82f5c538ea", Network: true,
	},
	{
		ID: "go", Name: "Go 1.27rc3", Description: "Official Go 1.27rc3 Bookworm image matching snow-core's toolchain line",
		Source: "index.docker.io/library/golang:1.27rc3-bookworm@sha256:e265a6dd120f4bff9beabc3e2e5e2f3198bdb6a7235cca1562962863980ea7e2", Network: true,
		CPUs: 4, MemoryMiB: 6144,
	},
	{
		ID: "node", Name: "Node.js 22", Description: "Official Node.js 22 Bookworm image with npm",
		Source: "index.docker.io/library/node:22-bookworm@sha256:0557ac14e0d45d02ed563067b82856ca5e7aa3437fa28d98d4350ea9c3d9494a", Network: true,
	},
	{
		ID: "python", Name: "Python 3.12 + uv", Description: "Official Astral uv 0.12.5 Python 3.12 Trixie image",
		Source: "ghcr.io/astral-sh/uv:0.12.5-python3.12-trixie@sha256:64165e61dd5fed90daa14ba2d17cdb5a49964837ff85431a3b8199d7a9aa98c0", Network: true,
	},
}

// Profiles returns a copy of the immutable built-in profile catalog.
func Profiles() []Profile { return append([]Profile(nil), builtinProfiles...) }

// FindProfile resolves a case-insensitive built-in profile ID.
func FindProfile(id string) (Profile, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, profile := range builtinProfiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}
