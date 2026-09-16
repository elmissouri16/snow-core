package main

import (
	"strings"
	"testing"
)

func TestFixturePairingCodeIgnoresAddressNotices(t *testing.T) {
	output := "Snow Manager test\nhttp://127.0.0.1:7331\nDetected private IPs (manager remains loopback-only): 192.168.1.50\nPairing code (reusable until later): secret-code\n"
	code, ok := fixturePairingCode(output)
	if !ok || code != "secret-code" {
		t.Fatalf("fixture pairing code = %q, %v", code, ok)
	}
}

func fixturePairingCode(output string) (string, bool) {
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, "Pairing code (") {
			continue
		}
		_, code, ok := strings.Cut(line, "): ")
		return code, ok && code != ""
	}
	return "", false
}
