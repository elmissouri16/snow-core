package process

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

var errCursorBeyondOutput = errors.New("process logs cursor is beyond produced output")

type outputRing struct {
	mu     sync.Mutex
	max    int
	start  int64
	end    int64
	data   []byte
	notify chan struct{}
}

func newOutputRing(maxBytes int) *outputRing {
	return &outputRing{max: maxBytes, notify: make(chan struct{})}
}

func (r *outputRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	original := len(p)
	r.end += int64(original)
	if original >= r.max {
		r.data = append(r.data[:0], p[original-r.max:]...)
		r.start = r.end - int64(len(r.data))
	} else {
		overflow := len(r.data) + original - r.max
		if overflow > 0 {
			copy(r.data, r.data[overflow:])
			r.data = r.data[:len(r.data)-overflow]
			r.start += int64(overflow)
		}
		r.data = append(r.data, p...)
	}
	close(r.notify)
	r.notify = make(chan struct{})
	return original, nil
}

type outputRead struct {
	data      []byte
	next      int64
	omitted   int64
	end       int64
	notify    <-chan struct{}
	hasOutput bool
}

func (r *outputRing) read(cursor *int64, maxBytes int, allowIncomplete bool) (outputRead, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	at := r.start
	omitted := int64(0)
	if cursor != nil {
		at = *cursor
		if at < r.start {
			omitted = r.start - at
			at = r.start
		}
	}
	if at > r.end {
		return outputRead{}, errCursorBeyondOutput
	}
	available := min(int(r.end-at), maxBytes)
	offset := int(at - r.start)
	// Never return a cursor in the middle of a retained UTF-8 rune. Invalid
	// bytes remain one-byte replacement units; only a valid continuation at the
	// cut point moves the boundary backward.
	cut := offset + available
	if cut < len(r.data) && r.data[cut]&0xc0 == 0x80 {
		boundary := cut
		for boundary > offset && r.data[boundary]&0xc0 == 0x80 {
			boundary--
		}
		if boundary > offset {
			available = boundary - offset
		}
	}
	if !allowIncomplete && available > 0 {
		candidate := r.data[offset : offset+available]
		boundary := len(candidate) - 1
		for boundary > 0 && candidate[boundary]&0xc0 == 0x80 {
			boundary--
		}
		if !utf8.FullRune(candidate[boundary:]) {
			available = boundary
		}
	}
	data := slices.Clone(r.data[offset : offset+available])
	return outputRead{
		data: data, next: at + int64(available), omitted: omitted,
		end: r.end, notify: r.notify, hasOutput: available > 0,
	}, nil
}

func (r *outputRing) tail(maxBytes int) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if maxBytes > len(r.data) {
		maxBytes = len(r.data)
	}
	return slices.Clone(r.data[len(r.data)-maxBytes:])
}

func sanitizeUTF8(data []byte, maxBytes int) string {
	if maxBytes <= 0 || len(data) == 0 {
		return ""
	}
	if len(data) > maxBytes {
		data = data[:maxBytes]
	}
	value := string(data)
	if !utf8.Valid(data) {
		value = strings.ToValidUTF8(value, string(utf8.RuneError))
	}
	for len(value) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		if size <= 0 {
			break
		}
		value = value[:len(value)-size]
	}
	return value
}
