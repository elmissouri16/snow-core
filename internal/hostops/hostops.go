// Package hostops implements runtime-free, descriptor-pinned host project
// creation and explicitly released anonymous HTTPS clones. It owns no durable
// database, application runtime, activation policy or browser credentials.
package hostops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// Errors are deliberately fixed public descriptions, never wrapped OS/Git errors.
var (
	ErrInvalid     = errors.New("invalid host operation")
	ErrUnavailable = errors.New("host operation unavailable")
	ErrIdentity    = errors.New("host directory identity changed")
	ErrExists      = errors.New("destination already exists")
	ErrClosed      = errors.New("host operation is closed or already started")
	ErrHTTPSOnly   = errors.New("only anonymous HTTPS clones are available; SSH requires a separately approved host profile")
)

// Options is trusted operator composition, never RPC/browser input. GitExecutable
// must be a fixed absolute trusted binary for cloning, never looked up through
// project PATH. Git validation belongs to StartClone: an empty, missing or
// unusable Git selection cannot prevent runtime-free CREATE.
// HelperExecutable defaults to this Snow executable. Overrides are for fixed
// packaging/test composition only, not a generic command-launch endpoint.
type Options struct {
	GitExecutable    string
	HelperExecutable string
}

type Service struct{ options Options }

func New(options Options) (*Service, error) {
	if !supported() {
		return nil, ErrUnavailable
	}
	if options.HelperExecutable == "" {
		var err error
		options.HelperExecutable, err = os.Executable()
		if err != nil {
			return nil, ErrUnavailable
		}
	}
	if !trustedExecutable(options.HelperExecutable) {
		return nil, ErrUnavailable
	}
	return &Service{options: options}, nil
}

func trustedExecutable(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

// Prepared owns pinned directory descriptors. Close never removes a destination,
// including a failed or partial clone. Creation performs no process/network work.
type Prepared struct {
	mu      sync.Mutex
	service *Service
	parent  *os.Root
	child   *os.File
	result  protocol.RPCProjectPrepared
	closed  bool
	started bool
}

func (s *Service) Prepare(ctx context.Context, p protocol.RPCProjectPrepareParams) (*Prepared, error) {
	if ctx.Err() != nil {
		return nil, ErrClosed
	}
	if !validOperationID(p.OperationID) || !ValidLeaf(p.Leaf) || !validIdentity(p.Parent) {
		return nil, ErrInvalid
	}
	// Reject noncanonical aliases before opening and verify the opened identity,
	// not just a path-based preflight stat. os.Root pins traversal thereafter.
	canonical, err := filepath.EvalSymlinks(p.Parent.Path)
	if err != nil || canonical != p.Parent.Path {
		return nil, ErrIdentity
	}
	root, err := os.OpenRoot(p.Parent.Path)
	if err != nil {
		return nil, ErrIdentity
	}
	keep := false
	defer func() {
		if !keep {
			_ = root.Close()
		}
	}()
	parent, err := root.Open(".")
	if err != nil {
		return nil, ErrIdentity
	}
	actual, err := fileIdentity(parent, p.Parent.Path)
	_ = parent.Close()
	if err != nil || actual != p.Parent {
		return nil, ErrIdentity
	}
	if ctx.Err() != nil {
		return nil, ErrClosed
	}
	// No existence precheck/adoption and no MkdirAll. Every existing target,
	// including a dangling symlink, makes Mkdir fail.
	if err := root.Mkdir(p.Leaf, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrExists
		}
		return nil, ErrUnavailable
	}
	// A failure after Mkdir deliberately leaves the child for reconciliation.
	createdInfo, err := root.Lstat(p.Leaf)
	if err != nil {
		return nil, ErrIdentity
	}
	created, err := statIdentity(createdInfo, filepath.Join(p.Parent.Path, p.Leaf))
	if err != nil {
		return nil, ErrIdentity
	}
	child, err := openChild(root, p.Leaf)
	if err != nil {
		return nil, ErrIdentity
	}
	identity, err := fileIdentity(child, filepath.Join(p.Parent.Path, p.Leaf))
	if err != nil || identity != created {
		_ = child.Close()
		return nil, ErrIdentity
	}
	if !matchesNamedChild(root, p.Leaf, identity) || !matchesPath(p.Parent) {
		_ = child.Close()
		return nil, ErrIdentity
	}
	keep = true
	return &Prepared{service: s, parent: root, child: child, result: protocol.RPCProjectPrepared{
		OperationID: p.OperationID, Parent: p.Parent, Child: identity, Leaf: p.Leaf,
	}}, nil
}

func (p *Prepared) Result() protocol.RPCProjectPrepared { return p.result }

func (p *Prepared) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	_ = p.child.Close()
	_ = p.parent.Close()
	return nil
}

// StartClone consumes this preparation once, checks the exact durable identity
// acknowledgement and returns a gated handle. No helper or Git starts until
// Release, which the transport calls only after successfully writing its ACK.
func (p *Prepared) StartClone(ctx context.Context, start protocol.RPCProjectCloneStartParams) (*Clone, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.started || ctx.Err() != nil {
		return nil, ErrClosed
	}
	// Check the operator-selected executable at clone admission, not service
	// construction. CREATE performs no Git work, and the file may have changed
	// since New (BUG-161). Never fall back to a PATH search.
	if !trustedExecutable(p.service.options.GitExecutable) {
		return nil, ErrUnavailable
	}
	if start.OperationID != p.result.OperationID || start.Child != p.result.Child {
		return nil, ErrIdentity
	}
	if !ValidCloneURL(start.URL) {
		return nil, ErrHTTPSOnly
	}
	if !matchesPath(p.result.Parent) || !matchesNamedChild(p.parent, p.result.Leaf, start.Child) {
		return nil, ErrIdentity
	}
	// Reopen through the pinned parent with NOFOLLOW, then recheck. The clone
	// receives its own descriptor, so closing Prepared cannot break its launch.
	child, err := openChild(p.parent, p.result.Leaf)
	if err != nil {
		return nil, ErrIdentity
	}
	actual, err := fileIdentity(child, start.Child.Path)
	if err != nil || actual != start.Child {
		_ = child.Close()
		return nil, ErrIdentity
	}
	p.started = true
	return newClone(ctx, p.service.options, p.result.Parent, start, child), nil
}

func matchesPath(want protocol.HostDirectoryIdentity) bool {
	canonical, err := filepath.EvalSymlinks(want.Path)
	if err != nil || canonical != want.Path {
		return false
	}
	file, err := os.Open(want.Path)
	if err != nil {
		return false
	}
	defer file.Close()
	got, err := fileIdentity(file, want.Path)
	return err == nil && got == want
}

func matchesNamedChild(root *os.Root, leaf string, want protocol.HostDirectoryIdentity) bool {
	info, err := root.Lstat(leaf)
	if err != nil || !info.IsDir() {
		return false
	}
	got, err := statIdentity(info, want.Path)
	return err == nil && got == want
}
