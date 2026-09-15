package config

import "context"

// ReadManagerConfiguredProviderIDs is the stricter mutation-capability view:
// unlike ordinary default reads, it requires an existing operator config file.
// This prevents an unavailable/missing configuration from authorizing an auth
// mutation using implicit fallback provider registrations.
func ReadManagerConfiguredProviderIDs(ctx context.Context, path string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, base, err := openManagerParent(path, false)
	if err != nil {
		return nil, ErrManagerUnavailable
	}
	defer root.Close()
	document, info, err := readManagerRoot(ctx, root, base)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, ErrManagerUnavailable
	}
	return managerProviders(document)
}
