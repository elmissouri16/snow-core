package web

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteStartupOrganizesTrustedLANDetails(t *testing.T) {
	var output bytes.Buffer
	err := writeStartup(&output, startupDetails{
		version:        "test",
		profile:        "trusted-LAN HTTP",
		origin:         "http://192.168.1.50:7331",
		localURL:       "http://127.0.0.1:7331",
		addressNotice:  "Private listener IP: 192.168.1.50\n",
		pairingCode:    "secret-code",
		pairingExpires: "2026-10-11T22:57:53Z",
		trustedLAN:     true,
		showQRCode:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	startup := output.String()
	for _, want := range []string{
		"Snow Web Manager test\nMode: trusted-LAN HTTP",
		"Access\n  LAN URL:   http://192.168.1.50:7331",
		"  Local URL: http://127.0.0.1:7331",
		"Scan to open the LAN URL on another device:",
		"Pairing\n  Pairing code: secret-code\n  Expires:      2026-10-11T22:57:53Z",
		"Security\n  • HTTP traffic is unencrypted.",
		"Press Ctrl+C to stop.\n",
	} {
		if !strings.Contains(startup, want) {
			t.Errorf("startup output missing %q:\n%s", want, startup)
		}
	}
	if !strings.Contains(startup, qrColorStart) || !strings.Contains(startup, "█") {
		t.Errorf("startup output does not contain a terminal QR code:\n%s", startup)
	}
}

func TestWriteStartupKeepsNonTerminalLocalPreviewPlain(t *testing.T) {
	var output bytes.Buffer
	err := writeStartup(&output, startupDetails{
		version:        "dev",
		profile:        "local preview",
		origin:         "http://127.0.0.1:7331",
		addressNotice:  "Private IP: none detected; manager remains loopback-only.\n",
		pairingCode:    "secret-code",
		pairingExpires: "later",
	})
	if err != nil {
		t.Fatal(err)
	}
	startup := output.String()
	if strings.Contains(startup, "Scan to open") || strings.Contains(startup, "\x1b[") {
		t.Fatalf("local non-terminal startup unexpectedly contains a QR code: %q", startup)
	}
	if !strings.Contains(startup, "  • Remote access is disabled.") {
		t.Fatalf("local startup missing remote access boundary: %q", startup)
	}
	if outputSupportsQRCode(&output, "http://127.0.0.1:7331") {
		t.Fatal("bytes.Buffer reported terminal QR support")
	}
}

func TestTerminalQRCodeRequiresEnoughWidth(t *testing.T) {
	const origin = "http://192.168.1.50:7331"
	if terminalQRCodeFits(origin, 1) {
		t.Fatal("QR code reported that it fits in a one-column terminal")
	}
	if !terminalQRCodeFits(origin, 80) {
		t.Fatal("QR code did not fit in an 80-column terminal")
	}
}
