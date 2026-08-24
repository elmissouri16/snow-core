package provider

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

type lazyTestTransport struct {
	id          string
	authRefresh func(context.Context, auth.Credential) (auth.Credential, error)
}

func (p *lazyTestTransport) ID() string { return p.id }
func (p *lazyTestTransport) ListModels(context.Context) ([]protocol.Model, error) {
	return []protocol.Model{{Provider: p.id, ID: "model"}}, nil
}
func (p *lazyTestTransport) ListModelsWithCredential(_ context.Context, credential auth.Credential) ([]protocol.Model, error) {
	return []protocol.Model{{Provider: p.id, ID: credential.Key}}, nil
}
func (p *lazyTestTransport) Chat(context.Context, auth.Credential, protocol.ChatRequest) (protocol.EventStream, error) {
	return nil, nil
}
func (p *lazyTestTransport) SetAuthRefresh(refresh func(context.Context, auth.Credential) (auth.Credential, error)) {
	p.authRefresh = refresh
}
func (p *lazyTestTransport) DefaultModel() protocol.Model {
	return protocol.Model{Provider: p.id, ID: "default"}
}
func (*lazyTestTransport) ModelCatalogAuthoritative() bool { return true }
func (*lazyTestTransport) RejectUnknownModels() bool       { return true }

func TestLazyTransportDefersAndSharesConstruction(t *testing.T) {
	var builds atomic.Int32
	lazy, err := NewLazyTransport("lazy", func() (Transport, error) {
		builds.Add(1)
		return &lazyTestTransport{id: "lazy"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if lazy.ID() != "lazy" || lazy.Materialized() || builds.Load() != 0 {
		t.Fatalf("lazy transport initialized during construction: id=%q materialized=%v builds=%d", lazy.ID(), lazy.Materialized(), builds.Load())
	}

	const callers = 32
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			models, listErr := lazy.ListModels(context.Background())
			if listErr != nil || len(models) != 1 || models[0].ID != "model" {
				t.Errorf("ListModels() = %+v, %v", models, listErr)
			}
		}()
	}
	wg.Wait()
	if !lazy.Materialized() || builds.Load() != 1 {
		t.Fatalf("materialized=%v builds=%d; want true, 1", lazy.Materialized(), builds.Load())
	}
}

func TestLazyTransportForwardsAuthAndMetadata(t *testing.T) {
	concrete := &lazyTestTransport{id: "lazy"}
	lazy, err := NewLazyTransport("lazy", func() (Transport, error) { return concrete, nil })
	if err != nil {
		t.Fatal(err)
	}
	refresh := func(_ context.Context, credential auth.Credential) (auth.Credential, error) {
		return credential, nil
	}
	lazy.SetAuthRefresh(refresh)
	if lazy.Materialized() {
		t.Fatal("SetAuthRefresh materialized the transport")
	}
	models, err := lazy.ListModelsWithCredential(context.Background(), auth.Credential{Key: "credential-model"})
	if err != nil || len(models) != 1 || models[0].ID != "credential-model" {
		t.Fatalf("credential catalog = %+v, %v", models, err)
	}
	if concrete.authRefresh == nil {
		t.Fatal("deferred auth refresh callback was not forwarded")
	}
	if model := lazy.DefaultModel(); model.ID != "default" {
		t.Fatalf("DefaultModel() = %+v", model)
	}
	if !lazy.ModelCatalogAuthoritative() || !lazy.RejectUnknownModels() {
		t.Fatal("optional provider metadata was not forwarded")
	}
}

func BenchmarkLazyTransportMaterializedChat(b *testing.B) {
	concrete := &lazyTestTransport{id: "lazy"}
	lazy, err := NewLazyTransport("lazy", func() (Transport, error) { return concrete, nil })
	if err != nil {
		b.Fatal(err)
	}
	if _, err := lazy.Materialize(); err != nil {
		b.Fatal(err)
	}
	request := protocol.ChatRequest{}
	credential := auth.Credential{}
	b.Run("direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = concrete.Chat(context.Background(), credential, request)
		}
	})
	b.Run("lazy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = lazy.Chat(context.Background(), credential, request)
		}
	})
}

func TestLazyTransportCachesConstructionFailure(t *testing.T) {
	want := errors.New("invalid configuration")
	var builds atomic.Int32
	lazy, err := NewLazyTransport("lazy", func() (Transport, error) {
		builds.Add(1)
		return nil, want
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := lazy.Materialize(); !errors.Is(err, want) || !IsTransportInitializationError(err) {
			t.Fatalf("Materialize() error = %v; want marked %v", err, want)
		}
	}
	if builds.Load() != 1 || lazy.Materialized() {
		t.Fatalf("builds=%d materialized=%v; want 1, false", builds.Load(), lazy.Materialized())
	}
}
