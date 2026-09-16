package web

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	qrterminal "github.com/mdp/qrterminal/v3"
	"golang.org/x/term"
)

// qrterminal's half-block renderer uses spaces for black modules and block
// glyphs for white modules. Fixing both terminal colors keeps that polarity
// stable across light and dark themes.
const (
	qrColorStart = "\x1b[97;40m"
	qrColorReset = "\x1b[0m"
)

type startupDetails struct {
	version        string
	profile        string
	origin         string
	localURL       string
	addressNotice  string
	pairingCode    string
	pairingExpires string
	trustedLAN     bool
	showQRCode     bool
}

func writeStartup(output io.Writer, details startupDetails) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Snow Web Manager %s\n", details.version)
	fmt.Fprintf(&text, "Mode: %s\n\n", details.profile)

	text.WriteString("Access\n")
	if details.trustedLAN {
		fmt.Fprintf(&text, "  LAN URL:   %s\n", details.origin)
		fmt.Fprintf(&text, "  Local URL: %s\n", details.localURL)
	} else {
		fmt.Fprintf(&text, "  Local URL: %s\n", details.origin)
	}
	if notice := strings.TrimSpace(details.addressNotice); notice != "" {
		fmt.Fprintf(&text, "  %s\n", notice)
	}

	text.WriteString("\nPairing\n")
	fmt.Fprintf(&text, "  Pairing code: %s\n", details.pairingCode)
	fmt.Fprintf(&text, "  Expires:      %s\n", details.pairingExpires)
	text.WriteString("  The code is reusable until expiry or rotation and survives restarts.\n")

	text.WriteString("\nSecurity\n")
	if details.trustedLAN {
		text.WriteString("  • HTTP traffic is unencrypted. Use only on a trusted home or work LAN.\n")
		text.WriteString("  • Pairing is required. You manage firewall and interface access.\n")
	} else {
		text.WriteString("  • Remote access is disabled.\n")
	}
	text.WriteString("  • No agent starts until you explicitly activate a project.\n")
	text.WriteString("  • Snow runs with your OS privileges and has no process sandbox.\n")

	if details.trustedLAN && details.showQRCode {
		text.WriteString("\nScan to open the LAN URL on another device:\n")
		text.WriteString(terminalQRCode(details.origin))
		text.WriteString("  Enter the pairing code shown above after the page opens.\n")
	}
	text.WriteString("\nPress Ctrl+C to stop.\n")

	_, err := io.WriteString(output, text.String())
	return err
}

func outputSupportsQRCode(output io.Writer, content string) bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := output.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	fileDescriptor := int(file.Fd())
	if !term.IsTerminal(fileDescriptor) {
		return false
	}
	width, _, err := term.GetSize(fileDescriptor)
	return err == nil && terminalQRCodeFits(content, width)
}

func terminalQRCodeFits(content string, terminalWidth int) bool {
	for line := range strings.SplitSeq(rawTerminalQRCode(content), "\n") {
		if utf8.RuneCountInString(line)+2 > terminalWidth {
			return false
		}
	}
	return true
}

func terminalQRCode(content string) string {
	var output strings.Builder
	for line := range strings.SplitSeq(strings.TrimSuffix(rawTerminalQRCode(content), "\n"), "\n") {
		fmt.Fprintf(&output, "  %s%s%s\n", qrColorStart, line, qrColorReset)
	}
	return output.String()
}

func rawTerminalQRCode(content string) string {
	var qr bytes.Buffer
	qrterminal.GenerateHalfBlock(content, qrterminal.M, &qr)
	return qr.String()
}
