package main

import (
	"errors"
	"net"
	"testing"
)

func TestAutomaticWebOptionsUsesPrivateIPv4WithoutSetup(t *testing.T) {
	opts, err := automaticWebOptions(func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("fd00::50"), Mask: net.CIDRMask(64, 128)},
			&net.IPNet{IP: net.ParseIP("192.168.1.50"), Mask: net.CIDRMask(24, 32)},
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Listen != "192.168.1.50:7331" {
		t.Fatalf("automatic web options = %+v", opts)
	}
	if err := opts.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticWebOptionsUsesULAAndOfflineFallback(t *testing.T) {
	ula, err := automaticWebOptions(func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("fd00::50"), Mask: net.CIDRMask(64, 128)}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ula.Listen != "[fd00::50]:7331" {
		t.Fatalf("ULA web options = %+v", ula)
	}

	loopback, err := automaticWebOptions(func() ([]net.Addr, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if loopback.Listen != "127.0.0.1:7331" {
		t.Fatalf("offline web options = %+v", loopback)
	}

	if _, err := automaticWebOptions(func() ([]net.Addr, error) { return nil, errors.New("interfaces unavailable") }); err == nil {
		t.Fatal("automatic web options ignored interface failure")
	}
}

func TestFirstAutomaticWebAddressRejectsUnsafeAddresses(t *testing.T) {
	address, ok := firstAutomaticWebAddress([]net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("203.0.113.10"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("224.0.0.1"), Mask: net.CIDRMask(4, 32)},
		&net.IPNet{IP: net.ParseIP("fd00::50"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("10.0.0.20"), Mask: net.CIDRMask(24, 32)},
	})
	if !ok || address.String() != "10.0.0.20" {
		t.Fatalf("first automatic address = %v, %v", address, ok)
	}
}
