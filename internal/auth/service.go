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
	Account     string
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
	ErrAccountChanged  = errors.New("auth: account identity changed or cannot be verified; start a new request")
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
	credential.Account = "default"
	s.explicit[providerID] = cloneCredential(credential)
}

func (s *Service) explicitCredential(providerID string) (Credential, bool) {
	s.mu.RLock()
	credential, ok := s.explicit[providerID]
	s.mu.RUnlock()
	return cloneCredential(credential), ok
}

func (s *Service) candidate(providerID, account, key string, descriptor Descriptor) (Credential, bool) {
	if account == "default" {
		if credential, ok := s.explicitCredential(providerID); ok {
			return bindAccount(credential, providerID, account), true
		}
	}
	if s.store != nil {
		if credential, ok := s.store.Get(key); ok && credential.Valid() {
			return bindAccount(credential, providerID, account), true
		}
	}
	if account == "default" {
		for _, name := range descriptor.Environment {
			if value := strings.TrimSpace(os.Getenv(name)); value != "" {
				return Credential{Provider: providerID, Account: account, Type: CredentialAPIKey, Key: value}, true
			}
		}
	}
	return Credential{Provider: providerID, Account: account}, false
}

func bindAccount(credential Credential, providerID, account string) Credential {
	credential.Provider = providerID
	credential.Account = account
	return credential
}

// Status performs local inspection only. It never refreshes or contacts a provider.
func (s *Service) Status(ctx context.Context, providerID string) (Status, error) {
	return s.StatusAccount(ctx, providerID, "default")
}

// StatusAccount performs local inspection of one account without network I/O.
func (s *Service) StatusAccount(_ context.Context, providerID, account string) (Status, error) {
	key, err := AccountKey(providerID, account)
	if err != nil {
		return Status{}, err
	}
	account = normalizeAccount(account)
	driver, err := s.driver(providerID)
	if err != nil {
		return Status{}, err
	}
	descriptor := normalizeDescriptor(driver.Descriptor())
	credential, ok := s.candidate(providerID, account, key, descriptor)
	if !ok {
		return Status{ProviderID: providerID, Account: account, State: StateMissing, Summary: "not configured"}, nil
	}
	status, err := driver.Inspect(credential)
	status.ProviderID = providerID
	status.Account = account
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
	return s.ResolveAccount(ctx, providerID, "default")
}

// ResolveAccount resolves and, when needed, refreshes only the selected account.
func (s *Service) ResolveAccount(ctx context.Context, providerID, account string) (Credential, error) {
	key, err := AccountKey(providerID, account)
	if err != nil {
		return Credential{}, err
	}
	account = normalizeAccount(account)
	driver, err := s.driver(providerID)
	if err != nil {
		return Credential{}, err
	}
	descriptor := normalizeDescriptor(driver.Descriptor())
	credential, ok := s.candidate(providerID, account, key, descriptor)
	if !ok {
		if descriptor.Required {
			return Credential{}, fmt.Errorf("%w for %s", ErrLoginRequired, providerID)
		}
		return Credential{Provider: providerID, Account: account}, nil
	}
	// A stored OAuth access token may already be expired. Refresh it before
	// validation, which otherwise rejects the token before rotation can run.
	// Explicit credentials are not backed by the store and cannot be refreshed.
	if driver.NeedsRefresh(credential, s.now()) {
		if _, explicit := s.explicitCredential(providerID); account != "default" || !explicit {
			credential, err = s.refresh(ctx, providerID, credential, RefreshExpiring)
			if err != nil {
				return Credential{}, err
			}
		}
	}
	if err := driver.Validate(credential); err != nil {
		return Credential{}, err
	}
	return credential, nil
}

func (s *Service) RefreshRejected(ctx context.Context, providerID string, rejected Credential) (Credential, error) {
	if _, err := s.driver(providerID); err != nil {
		return Credential{}, err
	}
	if _, explicit := s.explicitCredential(providerID); normalizeAccount(rejected.Account) == "default" && explicit {
		return Credential{}, fmt.Errorf("%w for %s", ErrNotRefreshable, providerID)
	}
	if rejected.Type != CredentialOAuth || strings.TrimSpace(rejected.Refresh) == "" {
		return Credential{}, fmt.Errorf("%w for %s", ErrNotRefreshable, providerID)
	}
	return s.refresh(ctx, providerID, rejected, RefreshRejected)
}

func (s *Service) refresh(ctx context.Context, providerID string, supplied Credential, reason RefreshReason) (Credential, error) {
	key, err := AccountKey(providerID, supplied.Account)
	if err != nil {
		return Credential{}, err
	}
	account := normalizeAccount(supplied.Account)
	driver, err := s.driver(providerID)
	if err != nil {
		return Credential{}, err
	}
	if s.store == nil {
		return Credential{}, fmt.Errorf("%w for %s: no persistent credential store", ErrLoginRequired, providerID)
	}
	refresh := func() (Credential, error) {
		current, exists := s.store.Get(key)
		current = bindAccount(current, providerID, account)
		if !exists {
			return Credential{}, fmt.Errorf("%w for %s", ErrLoginRequired, providerID)
		}
		if reason == RefreshRejected && !sameCredential(current, supplied) {
			if providerID == "chatgpt" && !sameAccountIdentity(current, supplied) {
				return Credential{}, ErrAccountChanged
			}
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
		if providerID == "chatgpt" && !sameCredential(next, current) {
			currentIdentity, nextIdentity := AccountIdentity(current), AccountIdentity(next)
			// A proactive refresh cannot replace an established identity. A
			// rejection retry additionally requires positive identity continuity
			// before replaying the original request under a changed token.
			if currentIdentity != "" && currentIdentity != nextIdentity || reason == RefreshRejected && !sameAccountIdentity(next, supplied) {
				return Credential{}, ErrAccountChanged
			}
		}
		resolved, exists, err := s.store.Update(key, func(latest Credential, latestExists bool) (Credential, bool, error) {
			if !latestExists {
				return latest, false, nil
			}
			if !sameCredential(latest, current) {
				if providerID == "chatgpt" && reason == RefreshRejected && !sameAccountIdentity(latest, supplied) {
					return Credential{}, false, ErrAccountChanged
				}
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
		return bindAccount(resolved, providerID, account), nil
	}
	var resolved Credential
	err = s.withCredentialLock(key, func() error {
		var inner error
		resolved, inner = refresh()
		return inner
	})
	return resolved, err
}

func (s *Service) withCredentialLock(key string, fn func() error) error {
	if coordinator, ok := s.store.(RefreshLockStore); ok {
		return coordinator.WithRefreshLock(key, fn)
	}
	s.refreshMu.Lock()
	lock := s.refreshes[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.refreshes[key] = lock
	}
	s.refreshMu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

func sameAccountIdentity(left, right Credential) bool {
	identity := AccountIdentity(left)
	return identity != "" && identity == AccountIdentity(right)
}

func sameCredential(left, right Credential) bool {
	return left.Type == right.Type && left.Key == right.Key && left.Access == right.Access && left.Refresh == right.Refresh && left.Expires == right.Expires && left.AccountID == right.AccountID && reflect.DeepEqual(left.Extra, right.Extra)
}

func (s *Service) Login(ctx context.Context, providerID string, request LoginRequest, interaction Interaction) (Status, error) {
	return s.LoginAccount(ctx, providerID, "default", request, interaction)
}

// LoginAccount validates and saves a credential to one account alias. The same
// upstream ChatGPT identity cannot be stored under multiple aliases.
func (s *Service) LoginAccount(ctx context.Context, providerID, account string, request LoginRequest, interaction Interaction) (Status, error) {
	key, err := AccountKey(providerID, account)
	if err != nil {
		return Status{}, err
	}
	account = normalizeAccount(account)
	driver, err := s.driver(providerID)
	if err != nil {
		return Status{}, err
	}
	if account != "default" && s.store != nil {
		if _, ok := s.store.(AccountStore); !ok {
			return Status{}, errors.New("auth: named account login requires a store with account enumeration")
		}
	}
	credential, err := driver.Login(ctx, request, interaction)
	if err != nil {
		return Status{}, err
	}
	credential = bindAccount(credential, providerID, account)
	if err := driver.Validate(credential); err != nil {
		return Status{}, err
	}
	if s.store == nil {
		return Status{}, errors.New("auth: login requires a persistent credential store")
	}
	err = s.withCredentialLock("login:"+providerID, func() error {
		if providerID == "chatgpt" {
			if err := s.rejectDuplicateAccount(driver, account, credential); err != nil {
				return err
			}
		}
		return s.store.Put(key, credential)
	})
	if err != nil {
		return Status{}, err
	}
	return s.StatusAccount(ctx, providerID, account)
}

func (s *Service) Logout(ctx context.Context, providerID string) error {
	return s.LogoutAccount(ctx, providerID, "default")
}

// LogoutAccount removes only the selected stored credential.
func (s *Service) LogoutAccount(_ context.Context, providerID, account string) error {
	key, err := AccountKey(providerID, account)
	if err != nil {
		return err
	}
	if _, err := s.driver(providerID); err != nil {
		return err
	}
	if s.store == nil {
		return nil
	}
	return s.store.Delete(key)
}
