// Package sandbox manages Snow's optional persistent smolvm shell backend.
// It confines guest access to one explicitly mounted canonical project directory;
// it does not sandbox Snow itself or its in-process/network capabilities.
package sandbox

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

const (
	SourceImage          = "image"
	SourcePack           = "pack"
	MinimumSmolVMVersion = "1.8.1"
)

var assemblyExecutableCheckTimeout = 10 * time.Second

var (
	ErrNotInitialized     = errors.New("sandbox is not initialized for this project")
	ErrAlreadyInitialized = errors.New("sandbox is already initialized for this project")
	envNameRE             = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	profileIDRE           = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	smolVMVersionRE       = regexp.MustCompile(`^smolvm v?([0-9]+)\.([0-9]+)\.([0-9]+)$`)
)

// Launcher abstracts process creation for deterministic lifecycle tests.
type Launcher interface {
	LookPath(string) (string, error)
	CombinedOutput(context.Context, string, ...string) ([]byte, error)
	CommandContext(context.Context, string, ...string) *exec.Cmd
}

type osLauncher struct{}

// Options configure one project manager.
type Options struct {
	Context                  context.Context
	SkipExecutableValidation bool // recovery-only: permits forgetting a stale association
	AllowStaleProfilePolicy  bool // recovery-only: permits deleting or forgetting an obsolete built-in profile
	ProjectRoot              string
	StatePath                string
	Executable               string
	DefaultImage             string
	CPUs                     int
	MemoryMiB                int
	StorageGiB               int
	OverlayGiB               int
	GuestCWD                 string
	EnvAllowlist             []string
	Launcher                 Launcher
	AutoInstall              bool
	Installer                Installer
	ImageFetcher             ImageFetcher
	ImageFetchTimeout        time.Duration
}

// InitOptions are explicit authority choices for a new machine.
type InitOptions struct {
	Profile    string
	Source     string
	SourceKind string // image|pack; empty infers pack for an existing .smolmachine file
	CPUs       int
	MemoryMiB  int
	StorageGiB int
	OverlayGiB int
	StorageSet bool
	OverlaySet bool
	GuestCWD   string
	ReadOnly   bool
	Network    bool
}

// Status is a secret-free lifecycle snapshot.
type Status struct {
	Initialized bool   `json:"initialized"`
	Record      Record `json:"record,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
	Diagnostic  string `json:"diagnostic,omitempty"`
}

// Manager owns lifecycle and command construction for one canonical project.
type Manager struct {
	mu                sync.Mutex
	recordMu          sync.RWMutex
	record            *Record
	project           string
	store             *Store
	executable        string
	defaultImage      string
	cpus              int
	memoryMiB         int
	storageGiB        int
	overlayGiB        int
	guestCWD          string
	envAllowlist      []string
	launcher          Launcher
	autoInstall       bool
	installer         Installer
	imageFetcher      ImageFetcher
	imageFetchTimeout time.Duration
	active            atomic.Bool
}
