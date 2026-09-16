package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

var errListenAddress = errors.New("web: listener must be one numeric loopback or private-network address and port")

type deploymentMode uint8

const (
	deploymentLocal deploymentMode = iota
	deploymentTrustedLANHTTP
)

type deployment struct {
	listen       netip.AddrPort
	mode         deploymentMode
	publicOrigin string
	publicHost   string
}

type requestNetworkPolicy struct {
	profile string
}

func (o Options) deployment() (deployment, error) {
	address := o.Listen
	if address == "" {
		address = "127.0.0.1:7331"
	}
	listen, err := netip.ParseAddrPort(address)
	if err != nil || listen.String() != address || listen.Addr().Zone() != "" || listen.Addr().IsUnspecified() || listen.Addr().IsMulticast() || (!listen.Addr().IsLoopback() && !listen.Addr().IsPrivate()) {
		return deployment{}, errListenAddress
	}
	if listen.Addr().IsLoopback() {
		return deployment{listen: listen, mode: deploymentLocal}, nil
	}
	origin := "http://" + listen.String()
	return deployment{listen: listen, mode: deploymentTrustedLANHTTP, publicOrigin: origin, publicHost: listen.String()}, nil
}

func canonicalBrowserOrigin(origin string) (string, string, error) {
	return canonicalBrowserOriginMode(origin, false)
}

func canonicalTrustedLANOrigin(origin string) (string, string, error) {
	return canonicalBrowserOriginMode(origin, true)
}

func canonicalBrowserOriginMode(origin string, allowPrivate bool) (string, string, error) {
	invalid := errors.New("web: origin must be numeric loopback HTTP or trusted private HTTP")
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || parsed.Host == "" || strings.HasSuffix(parsed.Host, ":") {
		return "", "", invalid
	}
	address, err := netip.ParseAddr(parsed.Hostname())
	if err != nil || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() || !address.IsLoopback() && !(allowPrivate && address.IsPrivate()) {
		return "", "", invalid
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 || strconv.FormatUint(portNumber, 10) != port {
		return "", "", invalid
	}
	host := address.String()
	if port != "80" {
		host = net.JoinHostPort(address.String(), port)
	} else if address.Is6() {
		host = "[" + address.String() + "]"
	}
	return "http://" + host, host, nil
}

func (d deployment) policy() requestNetworkPolicy {
	profile := "local"
	if d.mode == deploymentTrustedLANHTTP {
		profile = "trusted-lan-http"
	}
	return requestNetworkPolicy{profile: profile}
}

type requestSourceKey struct{}

func (requestNetworkPolicy) admit(r *http.Request) (string, bool) {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || peer.Addr().Zone() != "" {
		return "unknown", true
	}
	return peer.Addr().Unmap().String(), true
}

func requestSource(ctx context.Context) string {
	source, _ := ctx.Value(requestSourceKey{}).(string)
	if source == "" {
		return "unknown"
	}
	return source
}
