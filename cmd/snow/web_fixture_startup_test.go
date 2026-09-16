package main

import (
	"strings"
	"testing"
)

func TestFixturePairingCodeIgnoresAddressNotices(t *testing.T) {
	tests := map[string]string{
		"organized output": "Snow Web Manager test\n\nAccess\n  Local URL: http://127.0.0.1:7331\n  Detected private IPs (manager remains loopback-only): 192.168.1.50\n\nPairing\n  Pairing code: secret-code\n",
		"legacy output":    "Snow Manager test\nhttp://127.0.0.1:7331\nDetected private IPs (manager remains loopback-only): 192.168.1.50\nPairing code (reusable until later): secret-code\n",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			origin, ok := fixtureOrigin(output)
			if !ok || origin != "http://127.0.0.1:7331" {
				t.Fatalf("fixture origin = %q, %v", origin, ok)
			}
			code, ok := fixturePairingCode(output)
			if !ok || code != "secret-code" {
				t.Fatalf("fixture pairing code = %q, %v", code, ok)
			}
		})
	}
}

func fixtureOrigin(output string) (string, bool) {
	var local string
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "LAN URL:"); ok {
			origin, _, _ := strings.Cut(strings.TrimSpace(value), " ")
			return origin, origin != ""
		}
		if value, ok := strings.CutPrefix(line, "Local URL:"); ok {
			local, _, _ = strings.Cut(strings.TrimSpace(value), " ")
			continue
		}
		if strings.HasPrefix(line, "http://") {
			local = line
		}
	}
	return local, local != ""
}

func fixturePairingCode(output string) (string, bool) {
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if code, ok := strings.CutPrefix(line, "Pairing code: "); ok {
			return code, code != ""
		}
		if !strings.HasPrefix(line, "Pairing code (") {
			continue
		}
		_, code, ok := strings.Cut(line, "): ")
		return code, ok && code != ""
	}
	return "", false
}
