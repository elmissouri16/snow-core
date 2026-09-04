package tui

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkSanitizeProcessOutput(b *testing.B) {
	for _, size := range []int{4096, 128 << 10} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			value := strings.Repeat("worker ready: processing file main.go\n", size/37+1)[:size]
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tuiPerformanceTextSink = sanitizeProcessOutput(value)
			}
		})
	}
}
func BenchmarkProcessOutputLines(b *testing.B) {
	m := Model{processFleetOutput: strings.Repeat("worker ready: processing file main.go\n", 3500)}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m.processFleetOutputVersion++
		tuiPerformanceLinesSink = m.processOutputLines(120)
	}
}
func BenchmarkMentionMatchingMixedCase(b *testing.B) {
	files := make([]string, 2000)
	for i := range files {
		files[i] = fmt.Sprintf("internal/package-%03d/ServiceHandler.go", i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		tuiPerformanceLinesSink = matchMentionFiles(files, "service")
	}
}

var tuiPerformanceTextSink string
var tuiPerformanceLinesSink []string
