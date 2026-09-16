package web

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
)

const maxDisplayedPrivateAddresses = 8

func hostPrivateAddresses() ([]netip.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var values []net.Addr
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := networkInterface.Addrs()
		if err != nil {
			continue
		}
		values = append(values, addresses...)
	}
	return canonicalPrivateAddresses(values), nil
}

func canonicalPrivateAddresses(values []net.Addr) []netip.Addr {
	unique := make(map[netip.Addr]struct{}, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		text, _, _ := strings.Cut(value.String(), "/")
		text, _, _ = strings.Cut(text, "%")
		address, err := netip.ParseAddr(text)
		if err != nil {
			continue
		}
		address = address.Unmap()
		if !address.IsPrivate() || address.IsLoopback() {
			continue
		}
		unique[address] = struct{}{}
	}
	addresses := make([]netip.Addr, 0, len(unique))
	for address := range unique {
		addresses = append(addresses, address)
	}
	slices.SortFunc(addresses, func(a, b netip.Addr) int { return a.Compare(b) })
	return addresses
}

func privateAddressNotice(current deployment, addresses []netip.Addr, detectionErr error) string {
	if current.mode == deploymentTrustedLANHTTP {
		return "Private listener IP: " + current.listen.Addr().Unmap().String() + "\n"
	}
	listener := "manager remains loopback-only"
	if detectionErr != nil {
		return "Private IP detection unavailable; " + listener + ".\n"
	}
	if len(addresses) == 0 {
		return "Private IP: none detected; " + listener + ".\n"
	}
	displayed := addresses
	suffix := ""
	if len(displayed) > maxDisplayedPrivateAddresses {
		suffix = fmt.Sprintf(" (+%d more)", len(displayed)-maxDisplayedPrivateAddresses)
		displayed = displayed[:maxDisplayedPrivateAddresses]
	}
	values := make([]string, len(displayed))
	for i, address := range displayed {
		values[i] = address.String()
	}
	return "Detected private IPs (" + listener + "): " + strings.Join(values, ", ") + suffix + "\n"
}
