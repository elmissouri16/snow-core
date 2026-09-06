package builtin

import (
	"context"
	"fmt"
	"os"
)

// Serialize built-in file mutations across tool instances and path aliases.
// Reads remain concurrent. A shared gate avoids lock identities changing when
// an atomic save replaces an inode, and retains no per-path state.
var rootedMutationGate = make(chan struct{}, 1)

func lockRootedMutation(ctx context.Context) (func(), error) {
	select {
	case rootedMutationGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-rootedMutationGate
			return nil, err
		}
		return func() { <-rootedMutationGate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type rootedEditSnapshot struct {
	info    os.FileInfo
	content string
}

// Validate through the pinned root immediately before rename. This detects
// external atomic saves and in-place edits, including equal-length changes.
// An external writer can still race the final check and rename: portable file
// replacement has no compare-and-swap operation, and our gate only coordinates
// built-in tools in this process.
func (s *rootedEditSnapshot) validate(ctx context.Context, target rootedPath) error {
	conflict := func() error {
		return fmt.Errorf("file changed during edit; read it again before retrying")
	}
	file, info, err := openRootedRegular(target.root, target.name)
	if err != nil {
		return conflict()
	}
	defer file.Close()
	if !s.matchesInfo(info) {
		return conflict()
	}
	matched, err := readerMatchesString(ctx, file, s.content)
	if err != nil {
		return fmt.Errorf("verify edit source: %w", err)
	}
	if !matched {
		return conflict()
	}
	// Recheck the name after reading so replacing it during validation is also
	// detected, and a newly introduced symlink is never silently replaced.
	info, err = target.root.Lstat(target.name)
	if err != nil || !s.matchesInfo(info) {
		return conflict()
	}
	return ctx.Err()
}

func (s *rootedEditSnapshot) matchesInfo(info os.FileInfo) bool {
	return info.Mode().IsRegular() && os.SameFile(s.info, info) &&
		s.info.Size() == info.Size() && s.info.Mode() == info.Mode() &&
		s.info.ModTime().Equal(info.ModTime())
}
