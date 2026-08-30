package auth

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

// AuthState is the safe, provider-independent state exposed to user surfaces.
type AuthState string

const (
	StateMissing    AuthState = "missing"
	StateConfigured AuthState = "configured"
	StateExpired    AuthState = "expired"
	StateInvalid    AuthState = "invalid"
)

// LoginMethod describes one login flow implemented by an authentication driver.
type LoginMethod struct {
	ID          string
	DisplayName string
	Kind        CredentialType
}

// Descriptor is safe provider authentication metadata. It never contains a secret.
type Descriptor struct {
	ProviderID  string
	DisplayName string
	Required    bool
	Kinds       []CredentialType
	Environment []string
	Methods     []LoginMethod
}

// Status is a safe, normalized authentication status.
type Status struct {
	ProviderID  string
	State       AuthState
	Method      CredentialType
	Refreshable bool
	ExpiresAt   time.Time
	AccountID   string
	Summary     string
}

func (s Status) Configured() bool { return s.State == StateConfigured || s.State == StateExpired }

// RefreshReason identifies why a refresh is requested.
type RefreshReason string

const (
	RefreshExpiring RefreshReason = "expiring"
	RefreshRejected RefreshReason = "rejected"
)

// LoginRequest selects a driver's login method and supplies non-secret options.
type LoginRequest struct {
	Method string
	Params map[string][]string
}

// Driver owns provider-specific validation, login, and token exchange. Store
// lookup, precedence, locking, and persistence belong to Service.
type Driver interface {
	Descriptor() Descriptor
	Inspect(Credential) (Status, error)
	Login(context.Context, LoginRequest, Interaction) (Credential, error)
	Validate(Credential) error
	NeedsRefresh(Credential, time.Time) bool
	Refresh(context.Context, Credential, RefreshReason) (Credential, error)
}

var (
	ErrUnknownProvider = errors.New("auth: unknown provider")
	ErrLoginRequired   = errors.New("auth: login required")
	ErrNotRefreshable  = errors.New("auth: credential is not refreshable")
)

// Service centralizes provider-scoped credential precedence and lifecycle.
type Service struct {
	store Store
	now   func() time.Time

	mu       sync.RWMutex
	drivers  map[string]Driver
	explicit map[string]Credential

	refreshMu sync.Mutex
	refreshes map[string]*sync.Mutex
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now, drivers: make(map[string]Driver), explicit: make(map[string]Credential), refreshes: make(map[string]*sync.Mutex)}
}

// NewServiceForTest permits deterministic refresh tests without exporting time
// mutation on the production service.
func NewServiceForTest(store Store, now func() time.Time) *Service {
	s := NewService(store)
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) Store() Store { return s.store }

func (s *Service) Registered(providerID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.drivers[providerID] != nil
}

func (s *Service) Register(driver Driver) error {
	if driver == nil {
		return errors.New("auth: nil driver")
	}
	d := normalizeDescriptor(driver.Descriptor())
	if d.ProviderID == "" {
		return errors.New("auth: driver provider id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.drivers[d.ProviderID]; exists {
		return fmt.Errorf("auth: driver %q already registered", d.ProviderID)
	}
	s.drivers[d.ProviderID] = driver
	return nil
}

func normalizeDescriptor(d Descriptor) Descriptor {
	d.ProviderID = strings.TrimSpace(d.ProviderID)
	d.DisplayName = strings.TrimSpace(d.DisplayName)
	if d.DisplayName == "" {
		d.DisplayName = d.ProviderID
	}
	return d
}

func (s *Service) driver(providerID string) (Driver, error) {
	s.mu.RLock()
	driver := s.drivers[providerID]
	s.mu.RUnlock()
	if driver == nil {
		return nil, fmt.Errorf("%w %q", ErrUnknownProvider, providerID)
	}
	return driver, nil
}

func (s *Service) Providers() []Descriptor {
	s.mu.RLock()
	out := make([]Descriptor, 0, len(s.drivers))
	for _, driver := range s.drivers {
		out = append(out, normalizeDescriptor(driver.Descriptor()))
	}
	s.mu.RUnlock()
	slices.SortFunc(out, func(a, b Descriptor) int { return cmp.Compare(a.ProviderID, b.ProviderID) })
	return out
}

// Descriptor returns safe authentication metadata for one registered provider.
func (s *Service) Descriptor(providerID string) (Descriptor, error) {
	driver, err := s.driver(providerID)
	if err != nil {
		return Descriptor{}, err
	}
	return normalizeDescriptor(driver.Descriptor()), nil
}

// SetExplicit binds a credential to exactly one provider. Passing an invalid
// credential clears the override.
func (s *Service) SetExplicit(providerID string, credential Credential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !credential.Valid() {
		delete(s.explicit, providerID)
		return
	}
	credential.Provider = providerID
	s.explicit[providerID] = cloneCredential(credential)
}

func (s *Service) explicitCredential(providerID string) (Credential, bool) {
	s.mu.RLock()
	credential, ok := s.explicit[providerID]
	s.mu.RUnlock()
	return cloneCredential(credential), ok
}

func (s *Service) candidate(providerID string, descriptor Descriptor) (Credential, bool) {
	if credential, ok := s.explicitCredential(providerID); ok {
		return credential, true
	}
	if s.store != nil {
		if credential, ok := s.store.Get(providerID); ok && credential.Valid() {
			credential.Provider = providerID
			return credential, true
		}
	}
	for _, name := range descriptor.Environment {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return Credential{Provider: providerID, Type: CredentialAPIKey, Key: value}, true
		}
	}
	return Credential{Provider: providerID}, false
}

// Status performs local inspection only. It never refreshes or contacts a provider.
func (s *Service) Status(_ context.Context, providerID string) (Status, error) {
	driver, err := s.driver(providerID)
	if err != nil {
		return Status{}, err
	}
	descriptor := normalizeDescriptor(driver.Descriptor())
	credential, ok := s.candidate(providerID, descriptor)
	if !ok {
		return Status{ProviderID: providerID, State: StateMissing, Summary: "not configured"}, nil
	}
	status, err := driver.Inspect(credential)
	status.ProviderID = providerID
	if err != nil {
		status.State = StateInvalid
		if status.Summary == "" {
			status.Summary = err.Error()
		}
		return status, err
	}
	return status, nil
}

func (s *Service) Resolve(ctx context.Context, providerID string) (Credential, error) {
	driver, err := s.driver(providerID)
	if err != nil {
		return Credential{}, err
	}
	descriptor := normalizeDescriptor(driver.Descriptor())
	credential, ok := s.candidate(providerID, descriptor)
	if !ok {
		if descriptor.Required {
			return Credential{}, fmt.Errorf("%w for %s", ErrLoginRequired, providerID)
		}
		return Credential{Provider: providerID}, nil
	}
	if err := driver.Validate(credential); err != nil {
		return Credential{}, err
	}
	if !driver.NeedsRefresh(credential, s.now()) {
		return credential, nil
	}
	return s.refresh(ctx, providerID, credential, RefreshExpiring)
}

func (s *Service) RefreshRejected(ctx context.Context, providerID string, rejected Credential) (Credential, error) {
	if _, err := s.driver(providerID); err != nil {
		return Credential{}, err
	}
	if _, explicit := s.explicitCredential(providerID); explicit {
		return Credential{}, fmt.Errorf("%w for %s", ErrNotRefreshable, providerID)
	}
	if rejected.Type != CredentialOAuth || strings.TrimSpace(rejected.Refresh) == "" {
		return Credential{}, fmt.Errorf("%w for %s", ErrNotRefreshable, providerID)
	}
	return s.refresh(ctx, providerID, rejected, RefreshRejected)
}

func (s *Service) refresh(ctx context.Context, providerID string, supplied Credential, reason RefreshReason) (Credential, error) {
	driver, err := s.driver(providerID)
	if err != nil {
		return Credential{}, err
	}
	if s.store == nil {
		return Credential{}, fmt.Errorf("%w for %s: no persistent credential store", ErrLoginRequired, providerID)
	}
	refresh := func() (Credential, error) {
		current, exists := s.store.Get(providerID)
		if !exists {
			return Credential{}, fmt.Errorf("%w for %s", ErrLoginRequired, providerID)
		}
		if reason == RefreshRejected && !sameCredential(current, supplied) {
			if err := driver.Validate(current); err != nil {
				return Credential{}, err
			}
			return current, nil
		}
		if reason == RefreshExpiring && !driver.NeedsRefresh(current, s.now()) {
			return current, nil
		}
		next, err := driver.Refresh(ctx, current, reason)
		if err != nil {
			return Credential{}, err
		}
		resolved, exists, err := s.store.Update(providerID, func(latest Credential, latestExists bool) (Credential, bool, error) {
			if !latestExists {
				return latest, false, nil
			}
			if !sameCredential(latest, current) {
				return latest, false, nil
			}
			return next, true, nil
		})
		if err != nil {
			return Credential{}, err
		}
		if !exists {
			return Credential{}, fmt.Errorf("%w for %s: credential removed during refresh", ErrLoginRequired, providerID)
		}
		return resolved, nil
	}
	if coordinator, ok := s.store.(RefreshLockStore); ok {
		var resolved Credential
		err := coordinator.WithRefreshLock(providerID, func() error {
			var inner error
			resolved, inner = refresh()
			return inner
		})
		return resolved, err
	}
	s.refreshMu.Lock()
	lock := s.refreshes[providerID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.refreshes[providerID] = lock
	}
	s.refreshMu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	return refresh()
}

func sameCredential(left, right Credential) bool {
	return left.Type == right.Type && left.Key == right.Key && left.Access == right.Access && left.Refresh == right.Refresh && left.Expires == right.Expires && left.AccountID == right.AccountID && reflect.DeepEqual(left.Extra, right.Extra)
}

func (s *Service) Login(ctx context.Context, providerID string, request LoginRequest, interaction Interaction) (Status, error) {
	driver, err := s.driver(providerID)
	if err != nil {
		return Status{}, err
	}
	credential, err := driver.Login(ctx, request, interaction)
	if err != nil {
		return Status{}, err
	}
	credential.Provider = providerID
	if err := driver.Validate(credential); err != nil {
		return Status{}, err
	}
	if s.store == nil {
		return Status{}, errors.New("auth: login requires a persistent credential store")
	}
	if err := s.store.Put(providerID, credential); err != nil {
		return Status{}, err
	}
	return s.Status(ctx, providerID)
}

func (s *Service) Logout(_ context.Context, providerID string) error {
	if _, err := s.driver(providerID); err != nil {
		return err
	}
	if s.store == nil {
		return nil
	}
	return s.store.Delete(providerID)
}
