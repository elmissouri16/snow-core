//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	json "encoding/json/v2"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/internal/config"
	"github.com/elmissouri16/snow-core/internal/hostcontrol"
	"github.com/elmissouri16/snow-core/internal/hostops"
	"github.com/elmissouri16/snow-core/internal/rpc"
	"github.com/elmissouri16/snow-core/internal/web"
)

const hostControlsBrowserEnv = "SNOW_HOST_CONTROLS_BROWSER_DIR"

func init() {
	if len(os.Args) < 3 || os.Args[1] != "--fixture-host-controls-worker" {
		return
	}
	root := os.Args[2]
	info, err := os.Stat(root)
	marker, markerErr := os.ReadFile(filepath.Join(root, "fixture-only"))
	if !filepath.IsAbs(root) || err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || markerErr != nil || string(marker) != "PRIVATE_PROJECT_OPERATIONS_FIXTURE" {
		os.Exit(90)
	}
	args := slices.Clone(os.Args[3:])
	if slices.Equal(args, []string{"--mode", "rpc", "--rpc-startup", "control"}) {
		os.Exit(hostControlsBrowserControl(root))
	}
	// Reuse the existing actual-app fake-provider fixture for explicitly admitted
	// live workers only; CONTROL never constructs app/provider/session state.
	// Host defaults must use the actual public provider grammar ("fake" is a
	// reserved test provider, not a valid host profile). Substitute only the
	// explicitly activated runtime's provider to keep this fixture network-free.
	if slices.Contains(args, "eager") {
		if slices.Contains(args, "--provider") || slices.Contains(args, "--model") {
			os.Exit(95)
		}
		args = append(args, "--provider", "fake", "--model", "fake-1")
	}
	os.Args = append([]string{"snow"}, args...)
	code := runtimeControlsBrowserWorker(root)
	_ = os.WriteFile(filepath.Join(root, "host-runtime-exit"), []byte(strconv.Itoa(code)), 0600)
	os.Exit(code)
}

func hostControlsBrowserControl(root string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return 91
	}
	service, err := hostcontrol.New(hostcontrol.Options{})
	if err != nil {
		return 92
	}
	self, err := os.Executable()
	if err != nil {
		return 93
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	output := &realProjectRPCOutput{root: root, out: &permissionFixtureOutput{out: os.Stdout}}
	if rpc.ServeControl(ctx, permissionFixtureInput{ReadCloser: os.Stdin}, output, cwd, "host-browser-fixture", service, rpc.ControlOptions{
		HostOperations: rpc.NewControlHostOperations(hostops.Options{GitExecutable: filepath.Join(root, "git"), HelperExecutable: self}), APIKeys: service,
	}) != nil {
		return 94
	}
	return 0
}

// The certificate is never installed in the OS trust store. Chrome receives
// only this ephemeral key's SPKI digest, scoped to its private fixture profile.
func hostControlsBrowserCertificate(root string) (certPath, keyPath, spki string, err error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Private Snow fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}}
	public := &private.PublicKey
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		return "", "", "", err
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return "", "", "", err
	}
	encoded, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return "", "", "", err
	}
	digest := sha256.Sum256(encoded)
	certPath, keyPath = filepath.Join(root, "fixture-cert.pem"), filepath.Join(root, "fixture-key.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		return
	}
	return certPath, keyPath, base64.StdEncoding.EncodeToString(digest[:]), nil
}

func TestHostControlsBrowserCertificate(t *testing.T) {
	root := t.TempDir()
	cert, key, digest, err := hostControlsBrowserCertificate(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cert)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("fixture certificate encoding")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	if digest != base64.StdEncoding.EncodeToString(sum[:]) || parsed.VerifyHostname("127.0.0.1") != nil || parsed.VerifyHostname("example.com") == nil {
		t.Fatal("fixture certificate scope")
	}
	for _, path := range []string{cert, key} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("fixture certificate permissions")
		}
	}
}

// Opt-in private IPC. Both real HTTP and HTTPS listeners use ephemeral numeric
// loopback ports, independent private registries and no injected browser cookie.
func TestWebHostControlsBrowserFixture(t *testing.T) {
	root := os.Getenv(hostControlsBrowserEnv)
	if root == "" {
		t.Skip("opt-in native host controls fixture")
	}
	info, err := os.Stat(root)
	if !filepath.IsAbs(root) || err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("private fixture root required")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal("fixture root unavailable")
	}
	for _, dir := range []string{"home", "home/.snow", "project-a", "parent", "http-manager"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("SNOW_HOME", filepath.Join(root, "home", ".snow"))
	t.Setenv("SNOW_SESSIONS_DIR", filepath.Join(root, "sessions"))
	cfg := config.Default()
	cfg.DefaultProvider, cfg.DefaultModel = "opencode-zen", "fake-1"
	if err := config.Save(filepath.Join(root, "home", ".snow", "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixture-only"), []byte("PRIVATE_PROJECT_OPERATIONS_FIXTURE"), 0600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	for name, mode := range map[string]string{"worker": "--fixture-host-controls-worker", "git": "--fixture-real-project-git"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("#!/bin/sh\nexec "+quote(self)+" "+mode+" "+quote(root)+" \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := web.OpenRegistry(t.Context(), filepath.Join(root, "manager"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.Add(t.Context(), "Fictional existing worker", filepath.Join(root, "project-a"))
	if err != nil {
		_ = registry.Close()
		t.Fatal(err)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}
	cert, key, spki, err := hostControlsBrowserCertificate(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 220*time.Second)
	defer cancel()
	start := func(options web.Options) (string, string) {
		output := permissionFixtureStartup{output: make(chan string, 1)}
		done := make(chan error, 1)
		go func() { done <- web.Run(ctx, options, output) }()
		t.Cleanup(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error("fixture manager shutdown failed")
				}
			case <-time.After(12 * time.Second):
				t.Error("fixture manager shutdown bound")
			}
		})
		var startup string
		select {
		case startup = <-output.output:
		case <-ctx.Done():
			t.Fatal("fixture manager startup bound")
		}
		lines := strings.Split(startup, "\n")
		if len(lines) < 3 {
			t.Fatal("private fixture startup frame")
		}
		_, code, ok := strings.Cut(lines[2], "): ")
		if !ok {
			t.Fatal("private pairing credential unavailable")
		}
		return lines[1], code
	}
	origin, code := start(web.Options{Listen: "127.0.0.1:0", Version: "host-controls-browser", ManagerDir: filepath.Join(root, "manager"), Executable: filepath.Join(root, "worker"), SessionsRoot: filepath.Join(root, "sessions"), TLSCertFile: cert, TLSKeyFile: key})
	httpOrigin, httpCode := start(web.Options{Listen: "127.0.0.1:0", Version: "host-controls-http-disabled", ManagerDir: filepath.Join(root, "http-manager"), Executable: filepath.Join(root, "worker-must-not-start"), SessionsRoot: filepath.Join(root, "http-sessions")})
	ready, err := json.Marshal(map[string]string{"origin": origin, "code": code, "httpOrigin": httpOrigin, "httpCode": httpCode, "spki": spki, "project": project.ID, "directory": root})
	if err != nil {
		t.Fatal(err)
	}
	// Credential-bearing IPC is consumed privately; never report or screenshot it.
	fmt.Fprintln(os.Stdout, "SNOW_HOST_CONTROLS_READY "+string(ready))
	input := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 64), 1024)
		scanner.Scan()
		close(input)
	}()
	select {
	case <-input:
	case <-ctx.Done():
		t.Fatal("native host fixture lifetime exceeded")
	}
}

// Normal, browser-free smoke: starts the complete opt-in fixture in a private
// subprocess and verifies its actual TLS handshake against only its own cert.
func TestHostControlsBrowserPrivateStartup(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, self, "-test.run=^TestWebHostControlsBrowserFixture$", "-test.count=1")
	child.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(root, "home"), hostControlsBrowserEnv + "=" + root, "GOMAXPROCS=2"}
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = input.Close()
		if err := child.Wait(); err != nil {
			t.Error("private host fixture failed")
		}
	}()
	ready := make(chan map[string]string, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 65536)
		for scanner.Scan() {
			if value, ok := strings.CutPrefix(scanner.Text(), "SNOW_HOST_CONTROLS_READY "); ok {
				var frame map[string]string
				if json.Unmarshal([]byte(value), &frame) == nil {
					ready <- frame
				}
				return
			}
		}
	}()
	var frame map[string]string
	select {
	case frame = <-ready:
	case <-ctx.Done():
		t.Fatal("private fixture startup bound")
	}
	data, err := os.ReadFile(filepath.Join(root, "fixture-cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		t.Fatal("private fixture certificate unavailable")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for _, key := range []string{"origin", "httpOrigin"} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, frame[key]+"/login", nil)
		if err != nil {
			t.Fatal("private listener URL")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("private fixture listener unavailable")
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatal("private fixture pairing page unavailable")
		}
		if key == "origin" && (response.TLS == nil || len(response.TLS.VerifiedChains) == 0) {
			t.Fatal("private TLS handshake did not verify")
		}
	}
}
