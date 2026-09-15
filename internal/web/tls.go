package web

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Certificate chains and private keys are small. This limit applies separately
// to each PEM input; neither filesystem paths nor parser errors reach output.
const maxTLSFileBytes = 1 << 20

var errTLSFiles = errors.New("web: TLS certificate/key unavailable or invalid; use bounded regular PEM files without symlinks")

func (o Options) validateTLSPaths() error {
	if (o.TLSCertFile == "") != (o.TLSKeyFile == "") {
		return errors.New("web: --web-tls-cert and --web-tls-key must be supplied together")
	}
	for _, path := range []string{o.TLSCertFile, o.TLSKeyFile} {
		if path != "" && (!filepath.IsAbs(path) || filepath.Clean(path) != path) {
			return errors.New("web: TLS certificate/key paths must be absolute and clean")
		}
	}
	return nil
}

func loadTLSConfig(ctx context.Context, opts Options) (*tls.Config, error) {
	if err := opts.validateTLSPaths(); err != nil {
		return nil, err
	}
	if opts.TLSCertFile == "" {
		return nil, nil
	}
	certPEM, err := readTLSFile(ctx, opts.TLSCertFile)
	if err != nil {
		return nil, err
	}
	defer clear(certPEM)
	keyPEM, err := readTLSFile(ctx, opts.TLSKeyFile)
	if err != nil {
		return nil, err
	}
	defer clear(keyPEM)
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errTLSFiles
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, nil
}

// readTLSFile pins every directory with no-follow openat operations. It rejects
// symlinks in every component, special files (without blocking on FIFOs), and
// path identity/size/mtime changes before or during the bounded read.
func readTLSFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, errTLSFiles
	}
	defer root.Close()
	relative := strings.TrimPrefix(path, string(filepath.Separator))
	file, identities, err := openTLSFile(ctx, root, relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(tlsContextReader{ctx, file}, maxTLSFileBytes+1))
	if ctx.Err() != nil {
		clear(data)
		return nil, ctx.Err()
	}
	if err != nil || len(data) > maxTLSFileBytes || !sameTLSPath(root, identities) {
		clear(data)
		return nil, errTLSFiles
	}
	after, err := file.Stat()
	expected := identities[len(identities)-1].info
	if err != nil || !sameTLSIdentity(expected, after) || int64(len(data)) != after.Size() {
		clear(data)
		return nil, errTLSFiles
	}
	return data, nil
}

type tlsPathIdentity struct {
	path string
	info os.FileInfo
}

func openTLSFile(ctx context.Context, root *os.Root, path string) (*os.File, []tlsPathIdentity, error) {
	current, err := root.OpenFile(".", os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, errTLSFiles
	}
	var identities []tlsPathIdentity
	parts := strings.Split(path, string(filepath.Separator))
	prefix := ""
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			current.Close()
			return nil, nil, err
		}
		if part == "" || part == "." || part == ".." {
			current.Close()
			return nil, nil, errTLSFiles
		}
		prefix = filepath.Join(prefix, part)
		before, err := root.Lstat(prefix)
		last := i == len(parts)-1
		if err != nil || before.Mode()&os.ModeSymlink != 0 || (!last && !before.IsDir()) || (last && (!before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maxTLSFileBytes)) {
			current.Close()
			return nil, nil, errTLSFiles
		}
		flags := os.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if !last {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(int(current.Fd()), part, flags, 0)
		current.Close()
		if err != nil {
			return nil, nil, errTLSFiles
		}
		current = os.NewFile(uintptr(fd), part)
		after, err := current.Stat()
		if err != nil || !sameTLSIdentity(before, after) {
			current.Close()
			return nil, nil, errTLSFiles
		}
		identities = append(identities, tlsPathIdentity{prefix, after})
	}
	if !sameTLSPath(root, identities) {
		current.Close()
		return nil, nil, errTLSFiles
	}
	return current, identities, nil
}

func sameTLSIdentity(a, b os.FileInfo) bool {
	if !os.SameFile(a, b) || a.Mode() != b.Mode() {
		return false
	}
	// Directory contents may legitimately change while reading a certificate.
	return a.IsDir() || (a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()))
}

func sameTLSPath(root *os.Root, identities []tlsPathIdentity) bool {
	for _, identity := range identities {
		current, err := root.Lstat(identity.path)
		if err != nil || current.Mode()&os.ModeSymlink != 0 || !sameTLSIdentity(identity.info, current) {
			return false
		}
	}
	return true
}

type tlsContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r tlsContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// canonicalLoopbackOrigin accepts direct numeric-loopback origins only. It is
// shared by HTTP and TLS shell construction; no proxy/public origin is allowed.
func canonicalLoopbackOrigin(origin string) (string, string, error) {
	invalid := errors.New("web: origin must be a direct numeric loopback HTTP or HTTPS origin")
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || strings.HasSuffix(parsed.Host, ":") {
		return "", "", invalid
	}
	address, err := netip.ParseAddr(parsed.Hostname())
	if err != nil || !address.IsLoopback() || address.Zone() != "" {
		return "", "", invalid
	}
	defaultPort := "80"
	if parsed.Scheme == "https" {
		defaultPort = "443"
	}
	port := parsed.Port()
	if port == "" {
		port = defaultPort
	}
	if _, err := netip.ParseAddrPort(net.JoinHostPort(address.String(), port)); err != nil {
		return "", "", invalid
	}
	if parsed.Port() == defaultPort {
		parsed.Host = strings.TrimSuffix(parsed.Host, ":"+defaultPort)
	}
	return parsed.String(), parsed.Host, nil
}
