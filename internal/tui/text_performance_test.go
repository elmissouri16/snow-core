package tui

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
)

var referenceStringSink string

func TestTerminalTextTransformsMatchReference(t *testing.T) {
	values := []string{"", "hello", "hello\nworld\ttext", "é 中文 😀", "\x1b[31mred\x1b[0m", "\x00\x7f\u0085\u009b", "\xff\xfe\xc0a", "�", strings.Repeat("a\n", 5000), strings.Repeat("字", 10000)}
	rng := rand.New(rand.NewPCG(42, 19))
	for range 10000 {
		data := make([]byte, rng.IntN(256))
		for i := range data {
			data[i] = byte(rng.Uint32())
		}
		values = append(values, string(data))
	}
	for i, value := range values {
		for _, limit := range []int{-1, 0, 1, 2, 6, 32, 120, 2400} {
			if got, want := sanitizeTerminalTextLimit(value, limit), referenceOriginalSanitize(value, limit); got != want {
				t.Fatalf("sanitize case=%d limit=%d got=%q want=%q", i, limit, got, want)
			}
			if got, want := truncateRunes(value, limit), referenceOriginalTruncate(value, limit); got != want {
				t.Fatalf("truncate case=%d limit=%d got=%q want=%q", i, limit, got, want)
			}
		}
	}
	for _, value := range values[:10] {
		for _, width := range []int{0, 20, 80, 120} {
			if got, want := renderToolOutputPreview(value, width), referenceOriginalPreview(value, width); got != want {
				t.Fatalf("preview output changed width=%d", width)
			}
			if got, want := renderEditDiff(value, width), referenceOriginalDiff(value, width); got != want {
				t.Fatalf("diff output changed width=%d", width)
			}
		}
	}
	t.Logf("verified %d inputs at eight limits, plus rendered preview/diff cases", len(values))
}

func BenchmarkSanitizeTerminalText(b *testing.B) {
	for _, tc := range []struct{ name, text string }{
		{"token", "inspecting the code "},
		{"ascii4K", strings.Repeat("file ready: main.go\n", 220)},
		{"ascii128K", strings.Repeat("file ready: main.go\n", 7000)},
		{"unicode4K", strings.Repeat("é 中文 😀 file ready\n", 150)},
		{"control4K", strings.Repeat("\x1b[31mred\x1b[0m\n", 300)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				referenceStringSink = sanitizeTerminalText(tc.text)
			}
		})
	}
}
func BenchmarkTruncateRunes(b *testing.B) {
	for _, tc := range []struct{ name, text string }{
		{"short", "internal/agent/lifecycle_run.go"},
		{"ascii4K", strings.Repeat("a", 4096)},
		{"ascii128K", strings.Repeat("a", 128<<10)},
		{"unicode128K", strings.Repeat("字", (128<<10)/3)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				referenceStringSink = truncateRunes(tc.text, 120)
			}
		})
	}
}
func BenchmarkToolOutputPreview(b *testing.B) {
	for _, tc := range []struct{ name, text string }{
		{"normal", strings.Repeat("internal/agent/lifecycle_run.go:123: example result\n", 45)},
		{"short_lines", strings.Repeat("a\n", 1200)},
		{"long_line", strings.Repeat("a", 2400)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				referenceStringSink = renderToolOutputPreview(tc.text, 120)
			}
		})
	}
}
func BenchmarkCompactAgentText(b *testing.B) {
	for _, size := range []int{4096, 128 << 10} {
		value := strings.Repeat("worker inspected source file ", size/29)
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				referenceStringSink = compactAgentText(value, 120)
			}
		})
	}
}
