package main

import (
	"net"
	"net/netip"
	"strings"
)

const automaticWebPort = 7331

func availableInterfaceAddrs() ([]net.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var addresses []net.Addr
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
			continue
		}
		values, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		addresses = append(addresses, values...)
	}
	return addresses, nil
}

func firstAutomaticWebAddress(values []net.Addr) (netip.Addr, bool) {
	for _, ipv4 := range []bool{true, false} {
		for _, value := range values {
			address, ok := privateWebAddress(value)
			if ok && address.Is4() == ipv4 {
				return address, true
			}
		}
	}
	return netip.Addr{}, false
}

func privateWebAddress(value net.Addr) (netip.Addr, bool) {
	if value == nil {
		return netip.Addr{}, false
	}
	text, _, _ := strings.Cut(value.String(), "/")
	text, _, _ = strings.Cut(text, "%")
	address, err := netip.ParseAddr(text)
	if err != nil {
		return netip.Addr{}, false
	}
	address = address.Unmap()
	return address, address.IsPrivate() && !address.IsLoopback()
}
