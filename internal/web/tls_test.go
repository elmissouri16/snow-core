package web

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// All material is generated solely for this test in private temporary storage.
// No developer certificates, user configuration, or system trust store is used.
func tlsFixture(t *testing.T) (Options, *x509.CertPool) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Snow isolated TLS test"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Listen: "127.0.0.1:0", TLSCertFile: filepath.Join(root, "cert.pem"), TLSKeyFile: filepath.Join(root, "key.pem")}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(opts.TLSCertFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opts.TLSKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("test certificate not loaded")
	}
	return opts, pool
}

func TestTLSOptionsStayStrictlyLoopback(t *testing.T) {
	for _, opts := range []Options{
		{TLSCertFile: "/private/cert"}, {TLSKeyFile: "/private/key"},
		{TLSCertFile: "relative", TLSKeyFile: "/private/key"},
		{TLSCertFile: "/private/cert", TLSKeyFile: "relative"},
		{TLSCertFile: "/private/../cert", TLSKeyFile: "/private/key"},
		{Listen: "localhost:7331", TLSCertFile: "/private/cert", TLSKeyFile: "/private/key"},
		{Listen: "0.0.0.0:7331", TLSCertFile: "/private/cert", TLSKeyFile: "/private/key"},
		{Listen: "[::]:7331", TLSCertFile: "/private/cert", TLSKeyFile: "/private/key"},
		{Listen: "192.168.1.2:7331", TLSCertFile: "/private/cert", TLSKeyFile: "/private/key"},
	} {
		if err := opts.Validate(); err == nil {
			t.Error("invalid TLS/listen configuration accepted")
		}
	}
	for _, address := range []string{"", "127.0.0.1:0", "[::1]:0"} {
		if err := (Options{Listen: address, TLSCertFile: "/private/cert", TLSKeyFile: "/private/key"}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTLSLoadBoundedPrivateMaterial(t *testing.T) {
	opts, _ := tlsFixture(t)
	config, err := loadTLSConfig(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if config.MinVersion != tls.VersionTLS12 || len(config.Certificates) != 1 || config.GetCertificate != nil {
		t.Fatal("unexpected TLS policy or reload callback")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := loadTLSConfig(ctx, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	for _, mutate := range []struct {
		name  string
		apply func(*testing.T, *Options)
	}{
		{"missing", func(t *testing.T, o *Options) { o.TLSKeyFile += ".missing" }},
		{"malformed key", func(t *testing.T, o *Options) {
			if err := os.WriteFile(o.TLSKeyFile, []byte("private sentinel invalid PEM"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"oversized", func(t *testing.T, o *Options) {
			if err := os.Truncate(o.TLSKeyFile, maxTLSFileBytes+1); err != nil {
				t.Fatal(err)
			}
		}},
		{"empty", func(t *testing.T, o *Options) {
			if err := os.Truncate(o.TLSKeyFile, 0); err != nil {
				t.Fatal(err)
			}
		}},
		{"directory", func(t *testing.T, o *Options) { o.TLSKeyFile = filepath.Dir(o.TLSKeyFile) }},
		{"symlink file", func(t *testing.T, o *Options) {
			path := o.TLSKeyFile + ".link"
			if err := os.Symlink(o.TLSKeyFile, path); err != nil {
				t.Fatal(err)
			}
			o.TLSKeyFile = path
		}},
		{"symlink directory", func(t *testing.T, o *Options) {
			dir := filepath.Dir(o.TLSKeyFile)
			path := dir + "-link"
			if err := os.Symlink(dir, path); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Remove(path) })
			o.TLSKeyFile = filepath.Join(path, "key.pem")
		}},
		{"FIFO", func(t *testing.T, o *Options) {
			o.TLSKeyFile += ".fifo"
			if err := unix.Mkfifo(o.TLSKeyFile, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"mismatched", func(t *testing.T, o *Options) { other, _ := tlsFixture(t); o.TLSKeyFile = other.TLSKeyFile }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			opts, _ := tlsFixture(t)
			mutate.apply(t, &opts)
			opts.ManagerDir = filepath.Join(t.TempDir(), "must-not-create")
			var output bytes.Buffer
			err := Run(t.Context(), opts, &output)
			if !errors.Is(err, errTLSFiles) || err.Error() != errTLSFiles.Error() {
				t.Fatalf("expected fixed TLS error, got %v", err)
			}
			if output.Len() != 0 {
				t.Fatal("pairing output preceded TLS validation")
			}
			if _, err := os.Stat(opts.ManagerDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("TLS failure initialized manager storage")
			}
		})
	}
}

func TestTLSFileIdentityChangesRejected(t *testing.T) {
	for _, change := range []string{"replacement", "symlink", "growth", "directory swap"} {
		t.Run(change, func(t *testing.T) {
			opts, _ := tlsFixture(t)
			root, err := os.OpenRoot(string(filepath.Separator))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			file, identities, err := openTLSFile(t.Context(), root, strings.TrimPrefix(opts.TLSKeyFile, "/"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			switch change {
			case "replacement":
				if err := os.Rename(opts.TLSKeyFile, opts.TLSKeyFile+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(opts.TLSKeyFile, []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(opts.TLSKeyFile, opts.TLSKeyFile+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(opts.TLSKeyFile+".old", opts.TLSKeyFile); err != nil {
					t.Fatal(err)
				}
			case "growth":
				if err := os.Truncate(opts.TLSKeyFile, maxTLSFileBytes+1); err != nil {
					t.Fatal(err)
				}
			case "directory swap":
				dir := filepath.Dir(opts.TLSKeyFile)
				if err := os.Rename(dir, dir+".old"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(dir + ".old") })
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(opts.TLSKeyFile, []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if sameTLSPath(root, identities) {
				t.Fatal("accepted changed certificate identity")
			}
		})
	}
}

func TestTLSCanonicalOrigin(t *testing.T) {
	for _, origin := range []string{"https://127.0.0.1:443", "https://[::1]:443", "http://127.0.0.1:80", "https://127.0.0.1:7443"} {
		canonical, host, err := canonicalLoopbackOrigin(origin)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(canonical, ":443") || strings.HasSuffix(canonical, ":80") || host == "" {
			t.Fatal("default port not canonicalized")
		}
	}
	for _, origin := range []string{"https://localhost:7331", "https://0.0.0.0:7331", "https://192.168.1.2:7331", "https://127.0.0.1:99999", "https://127.0.0.1:", "https://user@127.0.0.1:7331", "https://127.0.0.1/path", "https://127.0.0.1?x=y", "https://127.0.0.1#fragment", "ftp://127.0.0.1"} {
		if _, _, err := canonicalLoopbackOrigin(origin); err == nil {
			t.Error("non-loopback or malformed origin accepted")
		}
	}
}
