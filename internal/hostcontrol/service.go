// Package hostcontrol implements runtime-free operator configuration control.
// It deliberately depends only on local config/auth helpers and protocol DTOs.
// Constructing or calling Service never starts an App, Agent, provider, session,
// tool, plugin, debug recorder, OAuth flow or network client.
package hostcontrol

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type Options struct {
	ConfigPath string
	AuthPath   string
}

type Service struct{ configPath, authPath string }

var (
	ErrInvalidRequest   = config.ErrManagerInvalid
	ErrUnavailable      = config.ErrManagerUnavailable
	ErrRevisionConflict = config.ErrManagerConflict
)

// New only selects paths; it does not touch the filesystem. Empty paths use
// operator global defaults (including SNOW_HOME). It never loads project config.
func New(options Options) (*Service, error) {
	configPath, authPath, _ := config.DefaultPaths()
	if options.ConfigPath != "" {
		configPath = options.ConfigPath
	}
	if options.AuthPath != "" {
		authPath = options.AuthPath
	}
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	authPath, err = filepath.Abs(authPath)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	return &Service{configPath: configPath, authPath: authPath}, nil
}

func (s *Service) GetDefaults(ctx context.Context, request protocol.HostDefaultsRequest) (protocol.HostDefaultsResponse, error) {
	return config.ReadManagerDefaults(ctx, s.configPath, request)
}
func (s *Service) UpdateDefaults(ctx context.Context, request protocol.HostDefaultsUpdateRequest) (protocol.HostDefaultsResponse, error) {
	return config.UpdateManagerDefaults(ctx, s.configPath, request)
}
func (s *Service) ProviderStatus(ctx context.Context) (protocol.HostProviderStatusResponse, error) {
	ids, err := config.ReadManagerProviderIDs(ctx, s.configPath)
	if err != nil {
		return protocol.HostProviderStatusResponse{}, err
	}
	response, err := auth.InspectHostStatus(ctx, s.authPath, ids)
	if errors.Is(err, auth.ErrHostStatusUnavailable) {
		err = ErrUnavailable
	}
	return response, err
}
