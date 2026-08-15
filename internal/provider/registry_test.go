package provider

import (
	"context"
	"io"
	"testing"

	"github.com/snow-core/snow/internal/auth"
	"github.com/snow-core/snow/pkg/protocol"
)

type registryProvider struct{ id string }

func (p registryProvider) ID() string { return p.id }
func (p registryProvider) ListModels(context.Context) ([]protocol.Model, error) {
	return []protocol.Model{{Provider: p.id, ID: "model"}}, nil
}
func (registryProvider) Chat(context.Context, protocol.ChatRequest) (protocol.EventStream, error) {
	return registryStream{}, nil
}

type registryStream struct{}

func (registryStream) Next(context.Context) (protocol.StreamEvent, error) {
	return protocol.StreamEvent{}, io.EOF
}
func (registryStream) Close() error { return nil }

func TestRegistryBuildsAuthenticatedCustomModule(t *testing.T) {
	registry := NewRegistry()
	runtime := registryProvider{id: "custom"}
	if err := registry.Register(Module{ID: "custom", Order: 10, Transport: NoAuthTransport{Provider: runtime}, Auth: auth.NewNoAuthDriver("custom", "Custom")}); err != nil {
		t.Fatal(err)
	}
	service := auth.NewService(auth.NewMemoryStore())
	providers, err := registry.Build(service)
	if err != nil {
		t.Fatal(err)
	}
	custom := providers["custom"]
	models, err := custom.ListModels(context.Background())
	if err != nil || len(models) != 1 || models[0].Provider != "custom" {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	stream, err := custom.Chat(context.Background(), protocol.ChatRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
}

func TestRegistryUsesDeterministicOrder(t *testing.T) {
	registry := NewRegistry()
	for _, module := range []Module{
		{ID: "z", Order: 20, Transport: NoAuthTransport{Provider: registryProvider{id: "z"}}, Auth: auth.NewNoAuthDriver("z", "Z")},
		{ID: "a", Order: 10, Transport: NoAuthTransport{Provider: registryProvider{id: "a"}}, Auth: auth.NewNoAuthDriver("a", "A")},
	} {
		if err := registry.Register(module); err != nil {
			t.Fatal(err)
		}
	}
	modules := registry.Modules()
	if len(modules) != 2 || modules[0].ID != "a" || modules[1].ID != "z" {
		t.Fatalf("modules=%+v", modules)
	}
}
