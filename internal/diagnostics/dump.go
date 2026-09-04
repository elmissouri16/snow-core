package diagnostics

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	// FormatVersion identifies the diagnostic JSON schema.
	FormatVersion = "snow-diagnostic-v1"
	// MaxDumpBytes bounds one encoded dump, including full session content.
	MaxDumpBytes = 256 << 20
	// SharingWarning is deliberately embedded in every dump and shown by the UI.
	SharingWarning = "Sensitive diagnostic data: this file contains full prompts, responses, thinking, tool arguments/results, errors, paths, and session state. Known credentials and provider-private continuity data are excluded, but review the file before sharing."
	redactedValue  = "[REDACTED CREDENTIAL]"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)((?:proxy-)?authorization["']?\s*[:=]\s*["']?(?:bearer\s+)?)([^\s,;"']+)`),
	regexp.MustCompile(`(?i)((?:"|')?(?:api[_-]?key|x-api-key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password|passwd|private[_-]?key|credential|token|secret|cookie|set-cookie|key|access|refresh)(?:"|')?\s*[:=]\s*["']?)([^\s,;"']+)`),
	regexp.MustCompile(`\b(?:sk|rk|pk)-[A-Za-z0-9_-]{16,}\b`),
	regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{16,}|xox[baprs]-[A-Za-z0-9-]{10,}|AKIA[A-Z0-9]{16}|AIza[A-Za-z0-9_-]{30,})\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`(?s)-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----.*?-----END (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----`),
}

// RedactText removes common explicit credential forms from user-controlled
// diagnostic strings. It is defense in depth, not a substitute for reviewing
// the sensitive dump before sharing it.
func RedactText(value string) string {
	for index, pattern := range secretPatterns {
		if index < 2 {
			value = pattern.ReplaceAllString(value, `${1}`+redactedValue)
		} else {
			value = pattern.ReplaceAllString(value, redactedValue)
		}
	}
	return value
}

// SanitizeJSONWithSecrets additionally replaces exact runtime-known secret
// values. Values shorter than four bytes are ignored to avoid destroying
// ordinary diagnostic prose.
func SanitizeJSONWithSecrets(data json.RawMessage, secrets []string) (json.RawMessage, int, error) {
	if len(data) == 0 {
		return nil, 0, nil
	}
	secrets = normalizedSecrets(secrets)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, 0, err
	}
	removed := 0
	value = sanitizeValue(value, secrets, &removed)
	encoded, err := json.Marshal(value)
	return encoded, removed, err
}

func sanitizeValue(value any, secrets []string, removed *int) any {
	switch typed := value.(type) {
	case string:
		typed = RedactText(typed)
		for _, secret := range secrets {
			typed = strings.ReplaceAll(typed, secret, redactedValue)
		}
		return typed
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok && object["type"] == "provider_data" {
				*removed++
				continue
			}
			out = append(out, sanitizeValue(item, secrets, removed))
		}
		return out
	case map[string]any:
		if typed["type"] == "provider_data" {
			*removed++
			return nil
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			lower := normalizeSensitiveKey(key)
			if lower == "providerdata" {
				*removed++
				continue
			}
			if sensitiveJSONKey(lower) {
				out[key] = redactedValue
				continue
			}
			if object, ok := item.(map[string]any); ok && object["type"] == "provider_data" {
				*removed++
				continue
			}
			out[key] = sanitizeValue(item, secrets, removed)
		}
		return out
	default:
		return value
	}
}

func normalizedSecrets(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len(value) < 4 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.SortFunc(out, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return out
}

func normalizeSensitiveKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(key)
}

func sensitiveJSONKey(key string) bool {
	switch key {
	case "authorization", "proxyauthorization", "apikey", "xapikey",
		"accesstoken", "refreshtoken", "clientsecret", "password", "passwd",
		"privatekey", "credential", "credentials", "secret", "token",
		"auth", "authstore", "cookie", "setcookie", "headers", "httpheaders",
		"transportheaders", "environment", "environmentvariables", "env":
		return true
	default:
		return false
	}
}

// WriteJSON atomically creates or replaces path with mode 0600. It rejects
// symlink destinations and bounds the complete encoded output.
func WriteJSON(path string, value any) error {
	return WriteJSONContext(context.Background(), path, value)
}

// WriteJSONContext is WriteJSON with cancellation checks around encoding,
// writing, synchronization, and commit. Cancellation removes the temporary
// file and leaves an existing destination unchanged.
func WriteJSONContext(ctx context.Context, path string, value any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("diagnostics: empty dump path")
	}
	path = filepath.Clean(path)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("diagnostics: destination is not a regular file: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("diagnostics: inspect destination: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("diagnostics: create dump directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".snow-diagnostic-*.tmp")
	if err != nil {
		return fmt.Errorf("diagnostics: create temporary dump: %w", err)
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("diagnostics: secure temporary dump: %w", err)
	}
	limited := &limitWriter{ctx: ctx, writer: temp, remaining: MaxDumpBytes}
	encoder := json.NewEncoder(limited)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("diagnostics: encode dump: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("diagnostics: sync dump: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("diagnostics: close dump: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("diagnostics: replace dump: %w", err)
	}
	committed = true
	return nil
}

type limitWriter struct {
	ctx       context.Context
	writer    io.Writer
	remaining int64
}

func (w *limitWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(data)) > w.remaining {
		return 0, fmt.Errorf("dump exceeds %d byte limit", MaxDumpBytes)
	}
	n, err := w.writer.Write(data)
	w.remaining -= int64(n)
	return n, err
}
