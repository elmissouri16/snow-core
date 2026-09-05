package shellanalysis

import (
	"net/url"
	"path/filepath"
	"strings"
)

func urlEndpoint(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
func remoteEndpoint(value string) string {
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host + parsed.Path
	}
	if filepath.IsAbs(value) {
		return ""
	}
	host, path, ok := strings.Cut(value, ":")
	if !ok || host == "" {
		return ""
	}
	return "ssh://" + host + "/" + strings.TrimPrefix(path, "/")
}
