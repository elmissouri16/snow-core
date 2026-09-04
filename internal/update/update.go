// Package update securely checks and installs official Snow GitHub releases.
package update

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultAPIURL      = "https://api.github.com/repos/elmissouri16/snow-core/releases?per_page=20"
	defaultDownloadURL = "https://github.com/elmissouri16/snow-core/releases/download"
	metadataLimit      = 1 << 20
	checksumLimit      = 64 << 10
	archiveLimit       = 128 << 20
	progressStep       = 256 << 10
)

type Release struct {
	Version string
	Tag     string
}

type Status struct {
	CurrentVersion string
	LatestVersion  string
	Available      bool
	Eligible       bool
	Reason         string
	Release        Release
}

type Result struct {
	PreviousVersion  string
	InstalledVersion string
}

// ProgressPhase identifies the visible phase of an explicitly approved update.
type ProgressPhase uint8

const (
	ProgressPreparing ProgressPhase = iota + 1
	ProgressDownloading
	ProgressVerifying
	ProgressInstalling
)

// Progress is a bounded installation progress snapshot. TotalBytes is zero when
// the server does not provide a usable content length.
type Progress struct {
	Phase           ProgressPhase
	DownloadedBytes int64
	TotalBytes      int64
}

// ProgressFunc receives synchronous progress snapshots during installation.
type ProgressFunc func(Progress)

type Options struct {
	CurrentVersion string
	Executable     string
	GOOS           string
	GOARCH         string
	HTTPClient     *http.Client
	APIURL         string
	DownloadURL    string
	CommandTimeout time.Duration
}

type Service struct {
	currentVersion string
	executable     string
	goos           string
	goarch         string
	client         *http.Client
	apiURL         string
	downloadURL    string
	commandTimeout time.Duration
}

func New(currentVersion string) *Service {
	return NewWithOptions(Options{CurrentVersion: currentVersion})
}

// NewWithOptions constructs an updater with injectable dependencies for tests.
func NewWithOptions(opts Options) *Service {
	executable := opts.Executable
	if executable == "" {
		executable, _ = os.Executable()
	}
	goos, goarch := opts.GOOS, opts.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute, CheckRedirect: safeRedirect}
	}
	apiURL := opts.APIURL
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	downloadURL := opts.DownloadURL
	if downloadURL == "" {
		downloadURL = defaultDownloadURL
	}
	commandTimeout := opts.CommandTimeout
	if commandTimeout <= 0 {
		commandTimeout = 10 * time.Second
	}
	return &Service{currentVersion: opts.CurrentVersion, executable: executable, goos: goos, goarch: goarch, client: client, apiURL: apiURL, downloadURL: strings.TrimRight(downloadURL, "/"), commandTimeout: commandTimeout}
}

func safeRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("update: too many redirects")
	}
	if req.URL.Scheme != "https" || req.URL.User != nil {
		return errors.New("update: unsafe redirect")
	}
	switch strings.ToLower(req.URL.Hostname()) {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return nil
	default:
		return errors.New("update: redirect to unapproved host")
	}
}

func (s *Service) Check(ctx context.Context) (Status, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var releases []struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
	}
	if err := s.getJSON(ctx, s.apiURL, metadataLimit, &releases); err != nil {
		return Status{}, err
	}
	var latest Version
	latestTag := ""
	for _, release := range releases {
		if release.Draft {
			continue
		}
		candidate, err := ParseVersion(release.TagName)
		if err != nil || release.TagName != "v"+candidate.String() {
			continue
		}
		if latestTag == "" || Compare(candidate, latest) > 0 {
			latest = candidate
			latestTag = release.TagName
		}
	}
	if latestTag == "" {
		return Status{}, errors.New("update: no published release has a valid canonical tag")
	}
	status := Status{CurrentVersion: s.currentVersion, LatestVersion: latest.String(), Release: Release{Version: latest.String(), Tag: latestTag}}
	current, err := ParseVersion(s.currentVersion)
	if err != nil {
		status.Available = true
		status.Reason = "development builds cannot self-update"
		return status, nil
	}
	status.Available = Compare(latest, current) > 0
	status.Eligible, status.Reason = s.eligibility()
	return status, nil
}

func (s *Service) Eligibility() (bool, string) { return s.eligibility() }

func (s *Service) eligibility() (bool, string) {
	if _, err := ParseVersion(s.currentVersion); err != nil {
		return false, "development builds cannot self-update"
	}
	if (s.goos != "linux" && s.goos != "darwin") || (s.goarch != "amd64" && s.goarch != "arm64") {
		return false, "self-update is unavailable on this platform"
	}
	path, err := filepath.Abs(s.executable)
	if err != nil || path == "" {
		return false, "cannot resolve the running executable"
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, "the running executable is not a regular non-symlink file"
	}
	probe, err := os.CreateTemp(filepath.Dir(path), ".snow-update-probe-*")
	if err != nil {
		return false, "the executable directory is not writable"
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true, ""
}

func (s *Service) getJSON(ctx context.Context, target string, limit int64, dst any) error {
	data, err := s.download(ctx, target, limit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst, json.RejectUnknownMembers(false)); err != nil {
		return errors.New("update: GitHub returned malformed release metadata")
	}
	return nil
}

func (s *Service) download(ctx context.Context, target string, limit int64) ([]byte, error) {
	return s.downloadWithProgress(ctx, target, limit, nil)
}

func (s *Service) downloadWithProgress(ctx context.Context, target string, limit int64, report func(downloaded, total int64)) ([]byte, error) {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return nil, errors.New("update: invalid HTTPS download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("update: create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "snow-core-updater/1")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("update: download failed with HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("update: download exceeds %d-byte limit", limit)
	}
	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	body := io.Reader(resp.Body)
	var progress *downloadProgressReader
	if report != nil {
		report(0, total)
		progress = &downloadProgressReader{reader: body, total: total, report: report}
		body = progress
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("update: read download: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("update: download exceeds %d-byte limit", limit)
	}
	if progress != nil {
		progress.finish()
	}
	return data, nil
}

type downloadProgressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	reported   int64
	report     func(downloaded, total int64)
}

func (r *downloadProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.downloaded += int64(n)
	if r.downloaded-r.reported >= progressStep {
		r.reported = r.downloaded
		r.report(r.downloaded, r.total)
	}
	return n, err
}

func (r *downloadProgressReader) finish() {
	if r.downloaded != r.reported {
		r.reported = r.downloaded
		r.report(r.downloaded, r.total)
	}
}
