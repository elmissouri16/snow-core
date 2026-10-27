package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// On-device dictation runs Whistle (Cactus Compute, Apache-2.0) inside the
// browser with WebAssembly. The 17 MB model is fetched once by the host, only
// on an explicit request, verified against a pinned digest, and then served to
// every paired browser from the manager directory, so audio never leaves the
// device and no browser contacts Hugging Face itself.
const (
	speechModelFile   = "whistle.cact"
	speechModelSHA256 = "b6e02f048568ac5d01a2042556c658061e699acbc0aa2a1439f52f3d461dffeb"
	speechModelSize   = 16919407
	speechModelSource = "https://huggingface.co/Cactus-Compute/whistle/resolve/b358ddadd89b7a713b5aa131f23032d3cca1b251/whistle.cact"
)

type speechStore struct {
	dir    string
	source string
	sha256 string
	size   int64
	client *http.Client

	mu          sync.Mutex
	downloading bool
}

func newSpeechStore(dir string) *speechStore {
	return &speechStore{dir: dir, source: speechModelSource, sha256: speechModelSHA256, size: speechModelSize, client: &http.Client{Timeout: 5 * time.Minute}}
}

type SpeechStatus struct {
	Model       string `json:"model"`
	License     string `json:"license"`
	Source      string `json:"source"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Available   bool   `json:"available"`
	Downloading bool   `json:"downloading"`
}

func (s *speechStore) path() string { return filepath.Join(s.dir, speechModelFile) }

func (s *speechStore) status() SpeechStatus {
	s.mu.Lock()
	downloading := s.downloading
	s.mu.Unlock()
	status := SpeechStatus{Model: "Whistle", License: "Apache-2.0", Source: s.source, Size: s.size, SHA256: s.sha256, Downloading: downloading}
	if info, err := os.Stat(s.path()); err == nil && info.Mode().IsRegular() && info.Size() == s.size {
		status.Available = true
	}
	return status
}

var errSpeechBusy = errors.New("speech model download already running")

// download fetches the pinned model into place atomically. A size or digest
// mismatch leaves nothing behind.
func (s *speechStore) download(ctx context.Context) error {
	s.mu.Lock()
	if s.downloading {
		s.mu.Unlock()
		return errSpeechBusy
	}
	s.downloading = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.downloading = false; s.mu.Unlock() }()
	if s.status().Available {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.source, nil)
	if err != nil {
		return err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("speech model source returned " + strconv.Itoa(response.StatusCode))
	}
	temp, err := os.CreateTemp(s.dir, speechModelFile+".*.part")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, digest), io.LimitReader(response.Body, s.size+1))
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if written != s.size || hex.EncodeToString(digest.Sum(nil)) != s.sha256 {
		return errors.New("speech model did not match its pinned size and digest")
	}
	if err := os.Chmod(temp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(temp.Name(), s.path())
}

func (s *shell) speechStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	if s.speech == nil {
		http.Error(w, "On-device dictation needs a manager directory", http.StatusNotFound)
		return
	}
	s.runtimeJSON(w, s.speech.status())
}

func (s *shell) speechDownload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeForm(w, r); !ok {
		return
	}
	if s.speech == nil {
		http.Error(w, "On-device dictation needs a manager directory", http.StatusNotFound)
		return
	}
	if r.PostForm.Get("confirm") != "download" {
		http.Error(w, "Confirm the model download", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	switch err := s.speech.download(ctx); {
	case errors.Is(err, errSpeechBusy):
		http.Error(w, "The speech model is already downloading", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "The speech model could not be downloaded and verified; nothing was kept", http.StatusBadGateway)
		return
	}
	s.runtimeJSON(w, s.speech.status())
}

func (s *shell) speechModel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.browser(r); !ok {
		http.Error(w, "Pair this browser to continue", http.StatusUnauthorized)
		return
	}
	if s.speech == nil || !s.speech.status().Available {
		http.Error(w, "Speech model not downloaded", http.StatusNotFound)
		return
	}
	file, err := os.Open(s.speech.path())
	if err != nil {
		http.Error(w, "Speech model unavailable", http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != s.speech.size {
		http.Error(w, "Speech model unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, speechModelFile, info.ModTime(), file)
}
