package process

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"testing"
)

func TestOutputRingMatchesRetainedByteOracle(t *testing.T) {
	rng := rand.New(rand.NewPCG(84, 35))
	for _, capacity := range []int{1, 7, 1024, 64 << 10, 1 << 20} {
		ring := newOutputRing(capacity)
		var want []byte
		var total int64
		writes := 500
		if capacity >= 64<<10 {
			writes = 2000
		}
		for step := range writes {
			n := rng.IntN(8192)
			if step%41 == 0 {
				n = capacity + 3
			}
			if step%11 == 0 {
				n = 0
			}
			payload := make([]byte, n)
			for i := range payload {
				payload[i] = byte(rng.Uint32())
			}
			oldNotify := ring.notify
			got, err := ring.Write(payload)
			if err != nil || got != len(payload) {
				t.Fatal(got, err)
			}
			select {
			case <-oldNotify:
			default:
				t.Fatal("write failed to notify")
			}
			total += int64(len(payload))
			want = append(want, payload...)
			if len(want) > capacity {
				want = want[len(want)-capacity:]
			}
			if !bytes.Equal(ring.tail(capacity), want) || ring.end != total || ring.start != total-int64(len(want)) {
				t.Fatalf("capacity=%d step=%d bytes/cursors changed", capacity, step)
			}
			read, err := ring.read(nil, capacity, true)
			if err != nil || !bytes.Equal(read.data, want) || read.next != total {
				t.Fatalf("capacity=%d step=%d full read changed", capacity, step)
			}
		}
	}
}

func BenchmarkFullOutputRingWrite(b *testing.B) {
	for _, capacity := range []int{64 << 10, 1 << 20} {
		for _, chunk := range []int{64, 4096, 32768} {
			b.Run(fmt.Sprintf("retained_%d/chunk_%d", capacity, chunk), func(b *testing.B) {
				ring := newOutputRing(capacity)
				payload := bytes.Repeat([]byte{'x'}, chunk)
				_, _ = ring.Write(bytes.Repeat([]byte{'a'}, capacity))
				b.ReportAllocs()
				b.SetBytes(int64(chunk))
				for b.Loop() {
					_, _ = ring.Write(payload)
				}
			})
		}
	}
}

// Benchmark the same os/exec stdout/stderr capture used by runtimeProcess,
// including subprocess startup, pipe transfer, buffer growth, and cleanup.
func BenchmarkProcessCapture32MiB(b *testing.B) {
	const size = 32 << 20
	if _, err := exec.LookPath("head"); err != nil {
		b.Skip("head unavailable")
	}
	b.ReportAllocs()
	b.SetBytes(size)
	for b.Loop() {
		ring := newOutputRing(DefaultRetainedOutputBytes)
		cmd := exec.Command("head", "-c", fmt.Sprint(size), "/dev/zero")
		cmd.Stdout = ring
		cmd.Stderr = ring
		if err := cmd.Run(); err != nil {
			b.Fatal(err)
		}
		if ring.end != size || len(ring.data) != DefaultRetainedOutputBytes {
			b.Fatal("capture size mismatch")
		}
	}
}
