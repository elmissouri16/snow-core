package rpc

import (
	"context"
	"sync"

	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// NewControlHostOperations selects only trusted, fixed executable composition.
// It does no filesystem access or resource initialization until Prepare.
func NewControlHostOperations(options hostops.Options) HostOperationService {
	return &controlHostOperations{load: sync.OnceValues(func() (*hostops.Service, error) { return hostops.New(options) })}
}

type controlHostOperations struct {
	load func() (*hostops.Service, error)
}

func (s *controlHostOperations) Prepare(ctx context.Context, params protocol.RPCProjectPrepareParams) (PreparedHostOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service, err := s.load()
	if err != nil {
		return nil, err
	}
	prepared, err := service.Prepare(ctx, params)
	if err != nil {
		return nil, err
	}
	return controlPreparedHostOperation{prepared}, nil
}

type controlPreparedHostOperation struct{ *hostops.Prepared }

func (p controlPreparedHostOperation) StartClone(ctx context.Context, params protocol.RPCProjectCloneStartParams) (HostCloneOperation, error) {
	return p.Prepared.StartClone(ctx, params)
}
