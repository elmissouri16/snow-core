package hostcontrol

import (
	"context"
	"errors"
	"slices"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

var ErrReplaceConfirmationRequired = auth.ErrHostKeyConfirmationRequired

func (s *Service) authorizeAPIKeyProvider(ctx context.Context, id string, write bool) error {
	ids, err := config.ReadManagerConfiguredProviderIDs(ctx, s.configPath)
	if err != nil {
		return err
	}
	if !slices.Contains(ids, id) || write && id == "chatgpt" {
		return ErrInvalidRequest
	}
	return nil
}

func hostAPIKeyError(err error) error {
	switch {
	case errors.Is(err, auth.ErrHostKeyInvalid):
		return ErrInvalidRequest
	case errors.Is(err, auth.ErrHostKeyUnavailable):
		return ErrUnavailable
	case errors.Is(err, auth.ErrHostKeyConflict):
		return ErrRevisionConflict
	default:
		return err
	}
}

// InspectAPIKey returns only local capability, consent requirements, status and
// opaque file-metadata revision. It never creates, exports or refreshes auth.
func (s *Service) InspectAPIKey(ctx context.Context, request protocol.HostAPIKeyInspectRequest) (protocol.HostAPIKeyStatusResponse, error) {
	if err := s.authorizeAPIKeyProvider(ctx, request.ProviderID, false); err != nil {
		return protocol.HostAPIKeyStatusResponse{}, err
	}
	response, err := auth.InspectHostAPIKey(ctx, s.authPath, request.ProviderID)
	return response, hostAPIKeyError(err)
}

// SetAPIKey is for explicit trusted local control transports only. Public
// adapters must enforce their HTTPS/CSRF authority gate before parsing secrets.
// This service does not install transports, run provider code or reload workers.
func (s *Service) SetAPIKey(ctx context.Context, request protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error) {
	if err := s.authorizeAPIKeyProvider(ctx, request.ProviderID, true); err != nil {
		return protocol.HostAPIKeyStatusResponse{}, err
	}
	response, err := auth.SetHostAPIKey(ctx, s.authPath, request)
	return response, hostAPIKeyError(err)
}
