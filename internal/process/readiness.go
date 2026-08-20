package process

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const readinessPollInterval = 100 * time.Millisecond

var errReadinessNonLoopback = errors.New("process readiness targets must be loopback-only")

func validateReadiness(req *ReadinessRequest) error {
	if req == nil {
		return nil
	}
	if req.TimeoutMS < 0 {
		return errors.New("process readiness timeout must not be negative")
	}
	switch req.Type {
	case "tcp":
		if req.Port < 1 || req.Port > 65535 {
			return errors.New("process readiness tcp port must be 1..65535")
		}
		if req.URL != "" || req.Pattern != "" {
			return errors.New("process readiness tcp accepts only host, port, and timeout_ms")
		}
	case "http":
		if req.URL == "" {
			return errors.New("process readiness http url is required")
		}
		if req.Host != "" || req.Port != 0 || req.Pattern != "" {
			return errors.New("process readiness http accepts only url and timeout_ms")
		}
	case "log":
		if req.Pattern == "" {
			return errors.New("process readiness log pattern is required")
		}
		if len(req.Pattern) > 4096 {
			return errors.New("process readiness log pattern exceeds 4096 bytes")
		}
		if _, err := regexp.Compile(req.Pattern); err != nil {
			return fmt.Errorf("process readiness log pattern: %w", err)
		}
		if req.Host != "" || req.Port != 0 || req.URL != "" {
			return errors.New("process readiness log accepts only pattern and timeout_ms")
		}
	default:
		return errors.New("process readiness type must be tcp, http, or log")
	}
	return nil
}

func waitForReadiness(ctx context.Context, process *runtimeProcess, req ReadinessRequest) error {
	timeout := DefaultReadinessTimeout
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
		if timeout > MaxReadinessTimeout {
			timeout = MaxReadinessTimeout
		}
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var check func(context.Context) (bool, error)
	switch req.Type {
	case "tcp":
		host := req.Host
		if host == "" {
			host = "127.0.0.1"
		}
		check = tcpReadinessCheck(host, req.Port)
	case "http":
		var err error
		check, err = httpReadinessCheck(req.URL)
		if err != nil {
			return err
		}
	case "log":
		pattern := regexp.MustCompile(req.Pattern)
		check = func(context.Context) (bool, error) {
			return pattern.Match(process.output.tail(process.output.max)), nil
		}
	}
	for {
		ready, err := check(readyCtx)
		if err != nil && !isRetryableReadiness(err) {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-process.done:
			return errors.New("process exited before becoming ready")
		case <-readyCtx.Done():
			if errors.Is(readyCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("timed out after %s", timeout)
			}
			return readyCtx.Err()
		case <-time.After(readinessPollInterval):
		}
	}
}

func tcpReadinessCheck(host string, port int) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		ips, err := loopbackIPs(ctx, host)
		if err != nil {
			return false, err
		}
		var lastErr error
		for _, ip := range ips {
			address := net.JoinHostPort(ip.String(), strconv.Itoa(port))
			conn, dialErr := (&net.Dialer{Timeout: readinessPollInterval}).DialContext(ctx, "tcp", address)
			if dialErr == nil {
				_ = conn.Close()
				return true, nil
			}
			lastErr = dialErr
		}
		return false, retryableReadinessError{lastErr}
	}
}

func httpReadinessCheck(rawURL string) (func(context.Context) (bool, error), error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("process readiness http url must be absolute")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("process readiness http url must use http or https")
	}
	if parsed.User != nil {
		return nil, errors.New("process readiness http url must not contain userinfo")
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host := parsed.Hostname()
			ips, err := loopbackIPs(ctx, host)
			if err != nil {
				return nil, err
			}
			port := parsed.Port()
			if port == "" {
				if parsed.Scheme == "https" {
					port = "443"
				} else {
					port = "80"
				}
			}
			var lastErr error
			for _, ip := range ips {
				conn, dialErr := (&net.Dialer{Timeout: readinessPollInterval}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return func(ctx context.Context) (bool, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return false, err
		}
		response, err := client.Do(request)
		if err != nil {
			if errors.Is(err, errReadinessNonLoopback) {
				return false, err
			}
			return false, retryableReadinessError{err}
		}
		_ = response.Body.Close()
		return response.StatusCode >= 200 && response.StatusCode <= 399, nil
	}, nil
}

func loopbackIPs(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("process readiness host is empty")
	}
	if literal := net.ParseIP(host); literal != nil {
		if !literal.IsLoopback() {
			return nil, errReadinessNonLoopback
		}
		return []net.IP{literal}, nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, retryableReadinessError{err}
	}
	if len(addresses) == 0 {
		return nil, retryableReadinessError{errors.New("host resolved to no addresses")}
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if !address.IP.IsLoopback() {
			return nil, fmt.Errorf("%w: host resolves to a non-loopback address", errReadinessNonLoopback)
		}
		ips = append(ips, address.IP)
	}
	return ips, nil
}

type retryableReadinessError struct{ error }

func isRetryableReadiness(err error) bool {
	var retry retryableReadinessError
	return errors.As(err, &retry)
}
