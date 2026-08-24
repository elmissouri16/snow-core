package responsesapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func BenchmarkResponsesSSE600(b *testing.B) {
	var payload strings.Builder
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&payload, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\",\"output_index\":0}\n\n")
	}
	payload.WriteString("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	wire := payload.String()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		stream := NewStream(ctx, &http.Response{Body: io.NopCloser(strings.NewReader(wire))}, "benchmark")
		for {
			if _, err := stream.Next(ctx); err != nil {
				if err != io.EOF {
					b.Fatal(err)
				}
				break
			}
		}
		if err := stream.Close(); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkResponsesEventSink = protocol.StreamEvent{}
}

var benchmarkResponsesEventSink protocol.StreamEvent
