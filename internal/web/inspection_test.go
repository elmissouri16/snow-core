//go:build darwin || linux

package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

func inspectionTestProject(t *testing.T) Project {
	t.Helper()
	base := t.TempDir()
	registry := registryTestOpen(t, filepath.Join(base, "manager"))
	path := registryTestDirectory(t, base, "project")
	p, err := registry.Add(t.Context(), "Inspection", path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func inspectionTestWrite(t *testing.T, p Project, name, content string) {
	t.Helper()
	path := filepath.Join(p.Path, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInspectionFilesAndPreview(t *testing.T) {
	p := inspectionTestProject(t)
	inspectionTestWrite(t, p, "src/main.go", "package main\n")
	inspectionTestWrite(t, p, "README.md", "hello\n")
	page, err := InspectFiles(t.Context(), p, "", 0)
	if err != nil || len(page.Entries) != 2 || page.Path != "." || page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if page.Entries[0].Name != "README.md" || page.Entries[0].Kind != "file" || page.Entries[1].Kind != "directory" {
		t.Fatalf("entries=%+v", page.Entries)
	}
	page, err = InspectFiles(t.Context(), p, "src", 0)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Path != "src/main.go" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	file, err := InspectFile(t.Context(), p, "src/main.go")
	if err != nil || file.Text != "package main\n" || file.Size != 13 || file.Truncated {
		t.Fatalf("file=%+v err=%v", file, err)
	}
}

func TestInspectionPathAndCredentialPolicy(t *testing.T) {
	p := inspectionTestProject(t)
	for _, name := range []string{".git", ".env", ".env.example", ".ENV.local", ".ssh", ".aws", "auth.json", "access.json", "credentials", "id_rsa", "private.pem", "private.key", ".npmrc"} {
		inspectionTestWrite(t, p, name, "protected")
		for _, path := range []string{name, "nested/" + name, name + "/child"} {
			if _, err := InspectFile(t.Context(), p, path); !errors.Is(err, ErrInspectionPath) {
				t.Errorf("path=%q err=%v", path, err)
			}
			if _, err := InspectFiles(t.Context(), p, path, 0); !errors.Is(err, ErrInspectionPath) {
				t.Errorf("directory=%q err=%v", path, err)
			}
			if _, err := InspectDiff(t.Context(), p, path, "staged"); !errors.Is(err, ErrInspectionPath) {
				t.Errorf("diff=%q err=%v", path, err)
			}
		}
	}
	page, err := InspectFiles(t.Context(), p, ".", 0)
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("protected listing=%+v err=%v", page, err)
	}
	for _, path := range []string{"/etc/passwd", "../file", "x/../file", "./file", "x//file", "x/", "file\x00", "file\n", "C:/file", "x\\file", string([]byte{0xff}), strings.Repeat("x", 4097), strings.Repeat("x/", 64) + "file"} {
		if _, err := InspectFile(t.Context(), p, path); !errors.Is(err, ErrInspectionPath) {
			t.Errorf("path=%q err=%v", path, err)
		}
	}
	for _, offset := range []int{-1, InspectionScanLimit, InspectionScanLimit + 1} {
		if _, err := InspectFiles(t.Context(), p, ".", offset); !errors.Is(err, ErrInspectionPath) {
			t.Errorf("offset=%d err=%v", offset, err)
		}
	}
}

func TestInspectionFileBoundsAndEncoding(t *testing.T) {
	p := inspectionTestProject(t)
	tests := []struct {
		name, text string
		want       error
		truncated  bool
	}{
		{"empty", "", nil, false},
		{"limit", strings.Repeat("x", InspectionFileLimit), nil, true},
		{"oversize", strings.Repeat("x", InspectionFileLimit+1), ErrInspectionTooLarge, false},
		{"nul", "text\x00binary", ErrInspectionBinary, false},
		{"late-nul", strings.Repeat("x", InspectionPreviewLimit) + "\x00", ErrInspectionBinary, false},
		{"invalid", string([]byte{0xff}), ErrInspectionBinary, false},
		{"late-invalid", strings.Repeat("x", InspectionPreviewLimit) + string([]byte{0xff}), ErrInspectionBinary, false},
		{"unicode", strings.Repeat("x", InspectionPreviewLimit-1) + "€tail", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inspectionTestWrite(t, p, tt.name, tt.text)
			result, err := InspectFile(t.Context(), p, tt.name)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err=%v want=%v", err, tt.want)
			}
			if err == nil && (len(result.Text) > InspectionPreviewLimit || !utf8.ValidString(result.Text) || result.Truncated != tt.truncated || result.Size != int64(len(tt.text))) {
				t.Fatalf("invalid preview: size=%d bytes=%d truncated=%v", result.Size, len(result.Text), result.Truncated)
			}
		})
	}
}

func TestInspectionRejectsLinksSpecialFilesAndReplacement(t *testing.T) {
	p := inspectionTestProject(t)
	inspectionTestWrite(t, p, "src/file", "safe")
	inspectionTestWrite(t, p, ".env", "secret")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"link": "src/file", "alias": "src", "secret": ".env", "outside": outside} {
		if err := os.Symlink(target, filepath.Join(p.Path, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(outside, filepath.Join(p.Path, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(p.Path, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"link", "alias/file", "secret", "outside", "hardlink", "fifo", "src"} {
		if _, err := InspectFile(t.Context(), p, path); !errors.Is(err, ErrInspectionUnavailable) {
			t.Errorf("path=%s err=%v", path, err)
		}
	}
	if _, err := InspectFiles(t.Context(), p, "alias", 0); !errors.Is(err, ErrInspectionUnavailable) {
		t.Fatalf("alias err=%v", err)
	}
	page, err := InspectFiles(t.Context(), p, ".", 0)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Name != "src" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	root, err := inspectionRoot(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, info, err := inspectionOpen(t.Context(), root, "src/file", false)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Rename(filepath.Join(p.Path, "src/file"), filepath.Join(p.Path, "src/old")); err != nil {
		t.Fatal(err)
	}
	inspectionTestWrite(t, p, "src/file", "evil")
	if err := inspectionCheck(t.Context(), root, "src/file", info); !errors.Is(err, ErrInspectionUnavailable) {
		t.Fatalf("replacement err=%v", err)
	}
	if err := os.Rename(p.Path, p.Path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectFiles(t.Context(), p, ".", 0); !errors.Is(err, ErrInspectionUnavailable) {
		t.Fatalf("root replacement err=%v", err)
	}
}

func TestInspectionDirectoryScanBound(t *testing.T) {
	p := inspectionTestProject(t)
	for i := range InspectionScanLimit + 3 {
		inspectionTestWrite(t, p, fmt.Sprintf("file-%04d", i), "")
	}
	seen := 0
	for offset := 0; offset < InspectionScanLimit; offset += InspectionPageSize {
		page, err := InspectFiles(t.Context(), p, ".", offset)
		if err != nil || len(page.Entries) > InspectionPageSize || page.NextOffset != offset+InspectionPageSize {
			t.Fatalf("offset=%d page=%+v err=%v", offset, page, err)
		}
		seen += len(page.Entries)
		if offset+InspectionPageSize == InspectionScanLimit && (!page.Limited || page.HasMore) {
			t.Fatalf("last page=%+v", page)
		}
	}
	if seen != InspectionScanLimit {
		t.Fatalf("scanned %d", seen)
	}
}

func TestInspectionCancellation(t *testing.T) {
	p := inspectionTestProject(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, a := InspectFiles(ctx, p, ".", 0)
	_, b := InspectFile(ctx, p, "file")
	_, c := InspectChanges(ctx, p)
	_, d := InspectDiff(ctx, p, "file", "staged")
	for _, err := range []error{a, b, c, d} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err=%v", err)
		}
	}
}
