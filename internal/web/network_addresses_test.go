package web

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCanonicalPrivateAddresses(t *testing.T) {
	addresses := canonicalPrivateAddresses([]net.Addr{
		&net.IPNet{IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("203.0.113.8"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("fd00::50"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)},
		nil,
	})
	want := []netip.Addr{netip.MustParseAddr("192.168.1.50"), netip.MustParseAddr("fd00::50")}
	if !slices.Equal(addresses, want) {
		t.Fatalf("private addresses = %v, want %v", addresses, want)
	}
}

func TestPrivateAddressNoticeDoesNotImplyLoopbackReachability(t *testing.T) {
	private := []netip.Addr{netip.MustParseAddr("192.168.1.50"), netip.MustParseAddr("fd00::50")}
	local := privateAddressNotice(deployment{mode: deploymentLocal}, private, nil)
	if !strings.Contains(local, "192.168.1.50") || !strings.Contains(local, "fd00::50") || !strings.Contains(local, "manager remains loopback-only") {
		t.Fatalf("local notice = %q", local)
	}
	unavailable := privateAddressNotice(deployment{mode: deploymentLocal}, nil, errors.New("unavailable"))
	if !strings.Contains(unavailable, "detection unavailable") || !strings.Contains(unavailable, "loopback-only") {
		t.Fatalf("unavailable notice = %q", unavailable)
	}
}

func TestLoopbackStartupPrintsPrivateIPStatusWithoutReachabilityClaim(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() { done <- Run(ctx, Options{Listen: "127.0.0.1:0"}, startupOutput(ready)) }()
	select {
	case output := <-ready:
		if (!strings.Contains(output, "Private IP") && !strings.Contains(output, "private IP")) || !strings.Contains(output, "manager remains loopback-only") {
			t.Fatalf("loopback startup private-IP notice = %q", output)
		}
	case err := <-done:
		t.Fatalf("loopback startup failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("loopback startup timed out")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("loopback shutdown timed out")
	}
}

func TestPrivateAddressNoticeIdentifiesExactPrivateListener(t *testing.T) {
	current := deployment{mode: deploymentTrustedLANHTTP, listen: netip.MustParseAddrPort("192.168.1.50:7331")}
	notice := privateAddressNotice(current, []netip.Addr{netip.MustParseAddr("192.168.1.99")}, nil)
	if notice != "Private listener IP: 192.168.1.50\n" {
		t.Fatalf("direct notice = %q", notice)
	}
}
