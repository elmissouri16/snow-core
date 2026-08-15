package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type staticInteraction struct{ value string }

func (s staticInteraction) Prompt(context.Context, Prompt) (Response, error) {
	return Response{Value: s.value}, nil
}
func (staticInteraction) OpenURL(context.Context, string) error { return nil }
func (staticInteraction) Progress(Progress)                     {}

func TestServiceAPIKeyPrecedenceAndProviderIsolation(t *testing.T) {
	t.Setenv("FIRST_KEY", "environment")
	store := NewMemoryStore()
	if err := store.Put("first", Credential{Type: CredentialAPIKey, Key: "stored"}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	if err := service.Register(NewAPIKeyDriver(APIKeyOptions{ProviderID: "first", Required: true, Environment: []string{"FIRST_KEY"}})); err != nil {
		t.Fatal(err)
	}
	if err := service.Register(NewAPIKeyDriver(APIKeyOptions{ProviderID: "second", Required: false})); err != nil {
		t.Fatal(err)
	}
	service.SetExplicit("first", Credential{Type: CredentialAPIKey, Key: "explicit"})
	first, err := service.Resolve(context.Background(), "first")
	if err != nil || first.Key != "explicit" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := service.Resolve(context.Background(), "second")
	if err != nil || second.Key != "" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	service.SetExplicit("first", Credential{})
	first, err = service.Resolve(context.Background(), "first")
	if err != nil || first.Key != "stored" {
		t.Fatalf("stored first=%+v err=%v", first, err)
	}
	if err := store.Delete("first"); err != nil {
		t.Fatal(err)
	}
	first, err = service.Resolve(context.Background(), "first")
	if err != nil || first.Key != "environment" {
		t.Fatalf("environment first=%+v err=%v", first, err)
	}
}

func TestServiceLoginStatusLogout(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	if err := service.Register(NewAPIKeyDriver(APIKeyOptions{ProviderID: "p", DisplayName: "Provider", Required: true})); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background(), "p")
	if err != nil || status.State != StateMissing {
		t.Fatalf("missing status=%+v err=%v", status, err)
	}
	status, err = service.Login(context.Background(), "p", LoginRequest{Method: "api_key"}, staticInteraction{value: "secret"})
	if err != nil || status.State != StateConfigured {
		t.Fatalf("login status=%+v err=%v", status, err)
	}
	if err := service.Logout(context.Background(), "p"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("p"); ok {
		t.Fatal("logout did not remove stored credential")
	}
}

type rotatingDriver struct {
	mu      sync.Mutex
	refresh int
	started chan struct{}
	release chan struct{}
}

func (*rotatingDriver) Descriptor() Descriptor {
	return Descriptor{ProviderID: "oauth", Required: true, Kinds: []CredentialType{CredentialOAuth}}
}
func (*rotatingDriver) Inspect(c Credential) (Status, error) {
	return Status{ProviderID: "oauth", State: StateExpired, Method: CredentialOAuth, Refreshable: c.Refresh != ""}, nil
}
func (*rotatingDriver) Login(context.Context, LoginRequest, Interaction) (Credential, error) {
	return Credential{}, errors.New("unused")
}
func (*rotatingDriver) Validate(c Credential) error {
	if c.Type != CredentialOAuth || c.Access == "" {
		return errors.New("invalid oauth")
	}
	return nil
}
func (*rotatingDriver) NeedsRefresh(c Credential, now time.Time) bool {
	return c.Refresh != "" && (now.IsZero() || c.Expires <= now.Unix())
}
func (d *rotatingDriver) Refresh(ctx context.Context, c Credential, _ RefreshReason) (Credential, error) {
	d.mu.Lock()
	d.refresh++
	count := d.refresh
	d.mu.Unlock()
	if d.started != nil {
		select {
		case d.started <- struct{}{}:
		default:
		}
	}
	if d.release != nil {
		select {
		case <-d.release:
		case <-ctx.Done():
			return Credential{}, ctx.Err()
		}
	}
	c.Access = "new"
	c.Refresh = "rotated"
	c.Expires += int64(count + 100)
	return c, nil
}

func TestServiceConcurrentRefreshRotatesOnce(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Put("oauth", Credential{Type: CredentialOAuth, Access: "old", Refresh: "refresh", Expires: 1}); err != nil {
		t.Fatal(err)
	}
	driver := &rotatingDriver{}
	service := NewServiceForTest(store, func() time.Time { return time.Unix(10, 0) })
	if err := service.Register(driver); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Resolve(context.Background(), "oauth")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	driver.mu.Lock()
	count := driver.refresh
	driver.mu.Unlock()
	if count != 1 {
		t.Fatalf("refreshes=%d, want 1", count)
	}
}

func TestServiceRefreshDoesNotResurrectLogout(t *testing.T) {
	store := NewMemoryStore()
	old := Credential{Type: CredentialOAuth, Access: "old", Refresh: "refresh", Expires: 1}
	if err := store.Put("oauth", old); err != nil {
		t.Fatal(err)
	}
	driver := &rotatingDriver{started: make(chan struct{}, 1), release: make(chan struct{})}
	service := NewServiceForTest(store, func() time.Time { return time.Unix(10, 0) })
	if err := service.Register(driver); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := service.Resolve(context.Background(), "oauth"); done <- err }()
	<-driver.started
	if err := store.Delete("oauth"); err != nil {
		t.Fatal(err)
	}
	close(driver.release)
	if err := <-done; !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("refresh error=%v, want login required", err)
	}
	if _, ok := store.Get("oauth"); ok {
		t.Fatal("refresh resurrected logout")
	}
}
