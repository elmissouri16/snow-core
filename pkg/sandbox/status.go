// Package sandbox defines the dependency-light public status of Snow's optional
// smolvm Bash execution backend.
package sandbox

// Status is a secret-free snapshot fixed when the Snow runtime is assembled or
// changed through that runtime's lifecycle controller.
type Status struct {
	// Configured reports whether this canonical project has a persistent machine association.
	Configured bool `json:"configured"`
	// Active reports whether this runtime routes model-facing Bash through it.
	Active     bool   `json:"active"`
	Backend    string `json:"backend,omitempty"`
	Machine    string `json:"machine,omitempty"`
	Profile    string `json:"profile,omitempty"`
	GuestCWD   string `json:"guest_cwd,omitempty"`
	ReadOnly   bool   `json:"read_only,omitempty"`
	Network    bool   `json:"network,omitempty"`
	CPUs       int    `json:"cpus,omitempty"`
	MemoryMiB  int    `json:"memory_mib,omitempty"`
	StorageGiB int    `json:"storage_gib,omitempty"`
	OverlayGiB int    `json:"overlay_gib,omitempty"`
}
