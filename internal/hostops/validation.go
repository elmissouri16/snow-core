package hostops

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

// ValidLeaf accepts one non-special UTF-8 component, bounded in bytes. No
// separators, control characters, or leading/trailing Unicode whitespace.
func ValidLeaf(leaf string) bool {
	if len(leaf) < 1 || len(leaf) > 128 || !utf8.ValidString(leaf) || leaf == "." || leaf == ".." || strings.TrimSpace(leaf) != leaf {
		return false
	}
	for _, r := range leaf {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validOperationID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if r < 33 || r > 126 || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func validIdentity(id protocol.HostDirectoryIdentity) bool {
	if !filepath.IsAbs(id.Path) || filepath.Clean(id.Path) != id.Path || !utf8.ValidString(id.Path) || len(id.Path) > 4096 {
		return false
	}
	for _, part := range []string{id.Device, id.Inode} {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != part {
			return false
		}
	}
	return id.Inode != "0"
}

// ValidCloneURL deliberately accepts a small anonymous HTTPS subset. It does
// not normalize the reviewed value. Redirects and additional Git transports are
// disabled independently at execution. This is not an SSRF/network sandbox.
func ValidCloneURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 4096 || !utf8.ValidString(raw) || !strings.HasPrefix(raw, "https://") {
		return false
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\\' {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, "?#") || u.Hostname() == "" || u.Path == "" || u.Path == "/" {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return false
	}
	for _, r := range u.Path {
		if unicode.IsControl(r) || r == '\\' {
			return false
		}
	}
	return true
}
