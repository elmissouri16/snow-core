package mcp

import "testing"

func TestServerSpecValidationAndTransportInference(t *testing.T) {
	tests := []struct {
		name      string
		spec      ServerSpec
		transport string
		wantErr   bool
	}{
		{"stdio", ServerSpec{ID: "local", Command: "server"}, TransportStdio, false},
		{"http", ServerSpec{ID: "remote", URL: "https://example.test/mcp"}, TransportStreamableHTTP, false},
		{"http alias", ServerSpec{ID: "remote", Transport: "http", URL: "https://example.test/mcp"}, TransportStreamableHTTP, false},
		{"bad id", ServerSpec{ID: "Bad ID", Command: "server"}, TransportStdio, true},
		{"mixed", ServerSpec{ID: "mixed", Command: "server", URL: "https://example.test/mcp"}, TransportStreamableHTTP, true},
		{"bad discovery", ServerSpec{ID: "local", Command: "server", ToolDiscovery: "sometimes"}, TransportStdio, true},
		{"lazy", ServerSpec{ID: "local", Command: "server", Lifecycle: LifecycleLazy, IdleTimeoutMS: 1000}, TransportStdio, false},
		{"lazy keep alive", ServerSpec{ID: "local", Command: "server", Lifecycle: LifecycleLazyKeepAlive}, TransportStdio, false},
		{"explicit bootstrap", ServerSpec{ID: "local", Command: "server", Lifecycle: LifecycleLazy, CacheBootstrap: CacheBootstrapExplicit}, TransportStdio, false},
		{"bad lifecycle", ServerSpec{ID: "local", Command: "server", Lifecycle: "sometimes"}, TransportStdio, true},
		{"bad bootstrap", ServerSpec{ID: "local", Command: "server", Lifecycle: LifecycleLazy, CacheBootstrap: "sometimes"}, TransportStdio, true},
		{"explicit eager bootstrap", ServerSpec{ID: "local", Command: "server", CacheBootstrap: CacheBootstrapExplicit}, TransportStdio, true},
		{"bad idle timeout", ServerSpec{ID: "local", Command: "server", IdleTimeoutMS: -1}, TransportStdio, true},
		{"eager idle timeout", ServerSpec{ID: "local", Command: "server", IdleTimeoutMS: 1000}, TransportStdio, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.spec.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() = %v", err)
			}
			if got := tt.spec.EffectiveTransport(); got != tt.transport {
				t.Fatalf("transport = %q, want %q", got, tt.transport)
			}
		})
	}
}
