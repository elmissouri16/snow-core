package opencodego

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func BenchmarkChatSSE600(b *testing.B) {
	var payload strings.Builder
	for range 600 {
		fmt.Fprintf(&payload, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"},\"finish_reason\":\"\"}]}\n\n")
	}
	payload.WriteString("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	wire := payload.String()
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		stream := newStream(ctx, 128, nil, "benchmark", "")
		go stream.readSSE(&http.Response{Body: io.NopCloser(strings.NewReader(wire))})
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
}
