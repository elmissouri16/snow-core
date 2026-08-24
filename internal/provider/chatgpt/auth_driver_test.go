package chatgpt

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
)

func TestLazyAuthDriverDefersProviderForLocalOperations(t *testing.T) {
	var builds atomic.Int32
	driver := NewLazyAuthDriver(func() (*Provider, error) {
		builds.Add(1)
		return New(), nil
	})
	if descriptor := driver.Descriptor(); descriptor.ProviderID != ProviderID {
		t.Fatalf("Descriptor() provider = %q", descriptor.ProviderID)
	}
	if _, err := driver.Inspect(auth.Credential{Type: auth.CredentialOAuth}); err == nil {
		t.Fatal("Inspect accepted an incomplete OAuth credential")
	}
	if _, err := driver.Refresh(context.Background(), auth.Credential{}, auth.RefreshExpiring); err == nil {
		t.Fatal("Refresh accepted a missing refresh token")
	}
	if _, err := driver.Login(context.Background(), auth.LoginRequest{Method: "invalid"}, nil); err == nil {
		t.Fatal("Login accepted an invalid method")
	}
	if builds.Load() != 0 {
		t.Fatalf("local auth operations constructed provider %d times", builds.Load())
	}
	first, err := driver.configuredProvider()
	if err != nil {
		t.Fatal(err)
	}
	second, err := driver.configuredProvider()
	if err != nil {
		t.Fatal(err)
	}
	if first != second || builds.Load() != 1 {
		t.Fatalf("configuredProvider reuse = %v, builds = %d; want true, 1", first == second, builds.Load())
	}
}
