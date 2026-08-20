package mcp

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	publicmcp "github.com/elmissouri16/snow-core/pkg/mcp"
)

const (
	cacheVersion             = 2
	cacheFilename            = "mcp-v1.json"
	cacheLockFilename        = ".mcp-v1.lock"
	maxCacheBytes            = 8 << 20
	maxCachedServers         = 256
	maxCachedToolsPerServer  = 1024
	maxCachedNameBytes       = 1024
	maxCachedDescription     = 64 << 10
	maxCachedSchemaBytes     = 512 << 10
	maxCachedServerSchemas   = 4 << 20
	maxCachedCapabilities    = 64
	maxCachedCapabilityBytes = 256
	defaultCacheAge          = 7 * 24 * time.Hour
	cacheLockTimeout         = 2 * time.Second
)

type cachedTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type cachedCatalog struct {
	ServerID                 string       `json:"server_id"`
	Scope                    string       `json:"scope"`
	ProjectIdentityHash      string       `json:"project_identity_hash"`
	ConfigurationFingerprint string       `json:"configuration_fingerprint"`
	CachedAt                 time.Time    `json:"cached_at"`
	ProtocolVersion          string       `json:"protocol_version,omitempty"`
	ServerName               string       `json:"server_name,omitempty"`
	ServerVersion            string       `json:"server_version,omitempty"`
	Capabilities             []string     `json:"capabilities,omitempty"`
	Tools                    []cachedTool `json:"tools,omitempty"`
}

func (c cachedCatalog) valid() bool { return c.ServerID != "" && !c.CachedAt.IsZero() }

// requiresEagerFallback reports catalogs with no descriptor capable of
// activating a disconnected server. Resource and prompt capabilities create
// bridge descriptors; an otherwise empty tool catalog must remain connected so
// list-change notifications are observable.
func (c cachedCatalog) requiresEagerFallback() bool {
	return len(c.Tools) == 0 && !contains(c.Capabilities, "resources") && !contains(c.Capabilities, "prompts")
}

type cacheFile struct {
	Version   int                      `json:"version"`
	WrittenAt time.Time                `json:"written_at"`
	Servers   map[string]cachedCatalog `json:"servers"`
}

type catalogCache struct {
	mu       sync.Mutex
	basePath string
	root     *os.Root
	loaded   bool
	entries  map[string]cachedCatalog
	initErr  error
	putErr   error // test hook; production caches leave this nil
}

func newCatalogCache(basePath string) *catalogCache {
	return &catalogCache{basePath: basePath, entries: make(map[string]cachedCatalog)}
}

func (c *catalogCache) close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == nil {
		return nil
	}
	err := c.root.Close()
	c.root = nil
	return err
}

func (c *catalogCache) prepare() error {
	if c == nil || c.basePath == "" {
		return errors.New("MCP cache is disabled")
	}
	if c.root != nil {
		return nil
	}
	if err := os.MkdirAll(c.basePath, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(c.basePath, 0o700); err != nil {
		return err
	}
	base, err := os.OpenRoot(c.basePath)
	if err != nil {
		return err
	}
	if err := base.Mkdir("cache", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		base.Close()
		return err
	}
	info, err := base.Lstat("cache")
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		base.Close()
		return errors.New("MCP cache directory is not a regular directory")
	}
	if err := base.Chmod("cache", 0o700); err != nil {
		base.Close()
		return err
	}
	if info, err = base.Lstat("cache"); err != nil || info.Mode().Perm() != 0o700 {
		base.Close()
		return errors.New("MCP cache directory permissions are not private")
	}
	root, err := base.OpenRoot("cache")
	base.Close()
	if err != nil {
		return err
	}
	c.root = root
	return nil
}

func (c *catalogCache) prepareReadOnly() error {
	if c == nil || c.basePath == "" {
		return errors.New("MCP cache is disabled")
	}
	if c.root != nil {
		return nil
	}
	base, err := os.OpenRoot(c.basePath)
	if err != nil {
		return err
	}
	info, err := base.Lstat("cache")
	if err != nil {
		base.Close()
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		base.Close()
		return errors.New("MCP cache directory is not a regular directory")
	}
	if info.Mode().Perm() != 0o700 {
		base.Close()
		return errors.New("MCP cache directory permissions are not private")
	}
	root, err := base.OpenRoot("cache")
	base.Close()
	if err != nil {
		return err
	}
	c.root = root
	return nil
}

func (c *catalogCache) get(key string, now time.Time) (cachedCatalog, bool, error) {
	if c == nil || c.basePath == "" {
		return cachedCatalog{}, false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepare(); err != nil {
		return cachedCatalog{}, false, err
	}
	if !c.loaded {
		file, err := c.readDisk()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			c.loaded = true
			c.entries = make(map[string]cachedCatalog)
			return cachedCatalog{}, false, err
		}
		c.loaded = true
		c.entries = file.Servers
		if c.entries == nil {
			c.entries = make(map[string]cachedCatalog)
		}
	}
	entry, ok := c.entries[key]
	if !ok {
		return cachedCatalog{}, false, nil
	}
	if now.Before(entry.CachedAt.Add(-defaultClockSkew)) || now.Sub(entry.CachedAt) > defaultCacheAge {
		return cachedCatalog{}, false, nil
	}
	if err := validateCachedCatalog(entry); err != nil {
		return cachedCatalog{}, false, err
	}
	return cloneCachedCatalog(entry), true, nil
}

func (c *catalogCache) put(key string, entry cachedCatalog, now time.Time) error {
	if c == nil || c.basePath == "" {
		return nil
	}
	if c.putErr != nil {
		return c.putErr
	}
	if err := validateCachedCatalog(entry); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepare(); err != nil {
		return err
	}
	lock, err := c.root.OpenFile(cacheLockFilename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockCacheFile(lock, cacheLockTimeout); err != nil {
		return err
	}
	defer unlockCacheFile(lock)

	file, err := c.readDisk()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		file = cacheFile{Version: cacheVersion, Servers: make(map[string]cachedCatalog)}
	}
	if file.Servers == nil {
		file.Servers = make(map[string]cachedCatalog)
	}
	for existingKey, existing := range file.Servers {
		if now.Before(existing.CachedAt.Add(-defaultClockSkew)) || now.Sub(existing.CachedAt) > defaultCacheAge ||
			(existingKey != key && existing.ServerID == entry.ServerID && existing.Scope == entry.Scope && existing.ProjectIdentityHash == entry.ProjectIdentityHash) {
			delete(file.Servers, existingKey)
		}
	}
	if _, exists := file.Servers[key]; !exists && len(file.Servers) >= maxCachedServers {
		oldestKey := ""
		var oldest time.Time
		for existingKey, existing := range file.Servers {
			if oldestKey == "" || existing.CachedAt.Before(oldest) {
				oldestKey, oldest = existingKey, existing.CachedAt
			}
		}
		delete(file.Servers, oldestKey)
	}
	file.Version = cacheVersion
	file.WrittenAt = now.UTC()
	file.Servers[key] = cloneCachedCatalog(entry)
	if err := c.writeDisk(file); err != nil {
		return err
	}
	c.entries = file.Servers
	c.loaded = true
	return nil
}

func (c *catalogCache) snapshot() (map[string]cachedCatalog, error) {
	if c == nil || c.basePath == "" {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareReadOnly(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]cachedCatalog{}, nil
		}
		return nil, err
	}
	file, err := c.readDisk()
	if errors.Is(err, os.ErrNotExist) {
		return map[string]cachedCatalog{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string]cachedCatalog, len(file.Servers))
	for key, entry := range file.Servers {
		out[key] = cloneCachedCatalog(entry)
	}
	return out, nil
}

func (c *catalogCache) remove(serverID, scope, projectHash string, now time.Time) (int, error) {
	if c == nil || c.basePath == "" {
		return 0, errors.New("MCP cache is disabled")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepare(); err != nil {
		return 0, err
	}
	lock, err := c.root.OpenFile(cacheLockFilename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, err
	}
	defer lock.Close()
	if err := lockCacheFile(lock, cacheLockTimeout); err != nil {
		return 0, err
	}
	defer unlockCacheFile(lock)
	file, err := c.readDisk()
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for key, entry := range file.Servers {
		if entry.ServerID == serverID && entry.Scope == scope && entry.ProjectIdentityHash == projectHash {
			delete(file.Servers, key)
			removed++
		}
	}
	if removed == 0 {
		return 0, nil
	}
	file.Version = cacheVersion
	file.WrittenAt = now.UTC()
	if err := c.writeDisk(file); err != nil {
		return 0, err
	}
	c.entries, c.loaded = file.Servers, true
	return removed, nil
}

func (c *catalogCache) readDisk() (cacheFile, error) {
	var out cacheFile
	info, err := c.root.Lstat(cacheFilename)
	if err != nil {
		return out, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return out, errors.New("MCP cache target is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return out, errors.New("MCP cache permissions are not private")
	}
	if info.Size() > maxCacheBytes {
		return out, errors.New("MCP cache exceeds size limit")
	}
	f, err := c.root.Open(cacheFilename)
	if err != nil {
		return out, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(info, after) {
		return out, errors.New("MCP cache changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCacheBytes+1))
	if err != nil {
		return out, err
	}
	if len(data) > maxCacheBytes {
		return out, errors.New("MCP cache exceeds size limit")
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parse MCP cache: %w", err)
	}
	if out.Version != cacheVersion {
		return cacheFile{}, fmt.Errorf("unsupported MCP cache version %d", out.Version)
	}
	if len(out.Servers) > maxCachedServers {
		return cacheFile{}, errors.New("MCP cache server limit exceeded")
	}
	for key, entry := range out.Servers {
		if len(key) > 128 || !strings.HasPrefix(key, "sha256:") {
			delete(out.Servers, key)
			continue
		}
		if err := validateCachedCatalog(entry); err != nil {
			delete(out.Servers, key)
		}
	}
	return out, nil
}

func (c *catalogCache) writeDisk(file cacheFile) error {
	data, err := json.Marshal(file)
	if err != nil {
		return err
	}
	if len(data) > maxCacheBytes {
		return errors.New("MCP cache exceeds size limit")
	}
	var token [8]byte
	if _, err := rand.Read(token[:]); err != nil {
		return err
	}
	temp := ".mcp-" + hex.EncodeToString(token[:]) + ".tmp"
	f, err := c.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = c.root.Remove(temp)
		}
	}()
	if _, err := io.Copy(f, bytes.NewReader(data)); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if info, err := c.root.Lstat(cacheFilename); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("MCP cache target is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := c.root.Rename(temp, cacheFilename); err != nil {
		return err
	}
	cleanup = false
	if dir, err := c.root.Open("."); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func validateCachedCatalog(entry cachedCatalog) error {
	if entry.ServerID == "" || len(entry.ServerID) > maxCachedNameBytes || entry.CachedAt.IsZero() ||
		len(entry.Scope) > maxCachedNameBytes || len(entry.ProjectIdentityHash) > 128 || len(entry.ConfigurationFingerprint) > 128 ||
		len(entry.ProtocolVersion) > maxCachedNameBytes || len(entry.ServerName) > maxCachedNameBytes || len(entry.ServerVersion) > maxCachedNameBytes {
		return errors.New("invalid MCP cache identity")
	}
	if len(entry.Tools) > maxCachedToolsPerServer || len(entry.Capabilities) > maxCachedCapabilities {
		return errors.New("MCP cache catalog limit exceeded")
	}
	for _, capability := range entry.Capabilities {
		if capability == "" || len(capability) > maxCachedCapabilityBytes {
			return errors.New("invalid MCP cached capability")
		}
	}
	total := 0
	for _, tool := range entry.Tools {
		if tool.Name == "" || len(tool.Name) > maxCachedNameBytes || len(tool.Title) > maxCachedNameBytes || len(tool.Description) > maxCachedDescription || len(tool.InputSchema) > maxCachedSchemaBytes {
			return errors.New("invalid MCP cached tool")
		}
		var object map[string]any
		if json.Unmarshal(tool.InputSchema, &object) != nil || object == nil {
			return errors.New("invalid MCP cached schema")
		}
		total += len(tool.InputSchema)
		if total > maxCachedServerSchemas {
			return errors.New("MCP cached schema aggregate limit exceeded")
		}
	}
	return nil
}

func cloneCachedCatalog(in cachedCatalog) cachedCatalog {
	out := in
	out.Capabilities = append([]string(nil), in.Capabilities...)
	out.Tools = make([]cachedTool, len(in.Tools))
	copy(out.Tools, in.Tools)
	for i := range out.Tools {
		out.Tools[i].InputSchema = append(json.RawMessage(nil), in.Tools[i].InputSchema...)
	}
	return out
}

func cacheIdentity(decl Declaration, roots []string) (string, string, string) {
	projectHash := hashStrings("snow-mcp-project-v1", decl.ProjectIdentity)
	spec := decl.Spec
	endpoint := spec.Command
	if spec.EffectiveTransport() == publicmcp.TransportStreamableHTTP {
		if parsed, err := url.Parse(spec.URL); err == nil {
			parsed.User, parsed.RawQuery, parsed.Fragment = nil, "", ""
			endpoint = parsed.String()
		}
	}
	argShape := cacheSafeArguments(spec.Args)
	envKeys, headerKeys := sortedKeys(spec.Env), sortedKeys(spec.Headers)
	rootHashes := make([]string, 0, len(roots))
	for _, root := range roots {
		rootHashes = append(rootHashes, hashStrings("snow-mcp-root-v1", filepath.Clean(root)))
	}
	sort.Strings(rootHashes)
	fingerprint := hashStrings("snow-mcp-config-v1", spec.ID, spec.EffectiveTransport(), endpoint, spec.CWD, strings.Join(argShape, "\x00"), strings.Join(envKeys, "\x00"), strings.Join(headerKeys, "\x00"), strings.Join(rootHashes, "\x00"), decl.Scope, projectHash)
	key := hashStrings("snow-mcp-entry-v1", spec.ID, decl.Scope, projectHash, fingerprint)
	return key, projectHash, fingerprint
}

func cacheSafeArguments(values []string) []string {
	out := make([]string, 0, len(values))
	redactNext, headerNext := false, false
	for _, value := range values {
		if redactNext {
			out = append(out, "<secret>")
			redactNext = false
			continue
		}
		if headerNext {
			out = append(out, cacheSafeHeader(value))
			headerNext = false
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "-h" || lower == "--header" {
			headerNext = true
			out = append(out, lower)
			continue
		}
		if key, val, ok := strings.Cut(value, "="); ok {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if lowerKey == "-h" || lowerKey == "--header" {
				out = append(out, key+"="+cacheSafeHeader(val))
				continue
			}
			if sensitiveCacheArgument(lowerKey) {
				out = append(out, key+"=<secret>")
				continue
			}
			if strings.HasPrefix(key, "-") {
				out = append(out, key+"=<value>")
			} else {
				out = append(out, "<positional-assignment>")
			}
			continue
		}
		if strings.HasPrefix(value, "-") && sensitiveCacheArgument(lower) {
			redactNext = true
			out = append(out, lower)
			continue
		}
		if strings.HasPrefix(value, "-") {
			// Flag names affect process shape and are not secret values.
			out = append(out, lower)
		} else {
			// Positional values may be low-entropy credentials. Persist only their
			// shape so the fingerprint cannot be used as an offline verifier.
			out = append(out, "<positional>")
		}
	}
	return out
}

func sensitiveCacheArgument(value string) bool {
	value = strings.TrimLeft(strings.ToLower(value), "-/")
	for _, marker := range []string{"token", "secret", "password", "passwd", "api-key", "apikey", "auth", "credential", "cookie", "private-key", "access-key", "client-key", "key-file", "key-path"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return value == "key"
}

func cacheSafeHeader(value string) string {
	name, rest, ok := strings.Cut(value, ":")
	if !ok {
		return "<header>"
	}
	if sensitiveCacheArgument(name) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(rest)), "bearer ") {
		return name + ":<secret>"
	}
	return name + ":<value>"
}

func hashStrings(values ...string) string {
	h := sha256.New()
	for _, value := range values {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(value))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
