package opencodego

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elmissouri16/snow-core/internal/auth"
	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func TestChatScopesAndStabilizesSessionHeader(t *testing.T) {
	for _, test := range []struct {
		name       string
		providerID string
		want       []string
	}{
		{name: "native OpenCode Go", want: []string{"affinity-a", "affinity-a", "affinity-b"}},
		{name: "reused compatible codec", providerID: "compatible", want: []string{"", "", ""}},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := make(chan string, 3)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				headers <- r.Header.Get(SessionHeader)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer server.Close()

			provider, err := New(Config{
				BaseURL:    server.URL,
				APIKey:     "key",
				HTTPClient: server.Client(),
				ProviderID: test.providerID,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, affinity := range []string{"affinity-a", "affinity-a", "affinity-b"} {
				stream, err := provider.Chat(t.Context(), auth.Credential{}, protocol.ChatRequest{
					Model:                   protocol.Model{ID: "model"},
					ConversationAffinityKey: affinity,
				})
				if err != nil {
					t.Fatal(err)
				}
				_ = drain(t, stream, t.Context())
			}
			for index, want := range test.want {
				if got := <-headers; got != want {
					t.Errorf("request %d %s = %q, want %q", index+1, SessionHeader, got, want)
				}
			}
		})
	}
}
