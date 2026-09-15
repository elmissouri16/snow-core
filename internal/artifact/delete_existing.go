package artifact

import (
	"context"
	"errors"
	"os"
)

// ExistingSessionDeleter removes namespaces from an existing private artifact
// root without creating or chmodding that root. Path is operator-owned.
type ExistingSessionDeleter struct{ Path string }

func (d ExistingSessionDeleter) DeleteSession(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := os.OpenRoot(d.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	store := &LocalStore{root: root}
	defer store.Close()
	return store.DeleteSession(ctx, id)
}
