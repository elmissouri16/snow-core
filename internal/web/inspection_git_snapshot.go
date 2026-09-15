//go:build darwin || linux

package web

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const inspectionGitUnsupported = "Git inspection is unavailable for this repository layout, index format, or changed metadata."

// The private control directory contains no project config, hooks, executable
// drivers, remote URLs or includes. Only validated HEAD and index are copied.
// Objects remain read-only in the original repository, after a bounded no-link
// walk; this is not containment of concurrent host processes changing that tree.
type inspectionGitSnapshot struct {
	root       *os.Root
	project    Project
	temp       string
	gitInfo    os.FileInfo
	objectInfo os.FileInfo
	indexInfo  os.FileInfo
	headInfo   os.FileInfo
	head       string
}

func (s *inspectionGitSnapshot) close() {
	if s.temp != "" {
		_ = os.RemoveAll(s.temp)
	}
	if s.root != nil {
		_ = s.root.Close()
	}
}

func newInspectionGitSnapshot(ctx context.Context, p Project) (*inspectionGitSnapshot, error) {
	root, err := inspectionRoot(ctx, p)
	if err != nil {
		return nil, err
	}
	s := &inspectionGitSnapshot{root: root, project: p}
	fail := func() (*inspectionGitSnapshot, error) { s.close(); return nil, ErrInspectionUnavailable }
	dir, info, err := inspectionOpen(ctx, root, ".git", true)
	if err != nil {
		return fail()
	}
	_ = dir.Close()
	s.gitInfo = info
	for _, name := range []string{".git/commondir", ".git/shallow", ".git/info/grafts", ".git/objects/info/alternates", ".git/objects/info/http-alternates"} {
		if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return fail()
		}
	}
	objects, info, err := inspectionOpen(ctx, root, ".git/objects", true)
	if err != nil {
		return fail()
	}
	_ = objects.Close()
	s.objectInfo = info
	if err := s.checkObjects(ctx); err != nil {
		return fail()
	}
	head, headInfo, err := inspectionGitRead(ctx, root, ".git/HEAD", 1024)
	if err != nil {
		return fail()
	}
	s.headInfo = headInfo
	value := strings.TrimSpace(string(head))
	unborn := false
	if ref, ok := strings.CutPrefix(value, "ref: "); ok {
		if !strings.HasPrefix(ref, "refs/heads/") || len(ref) > 256 {
			return fail()
		}
		if _, err := inspectionPath(ref, false); err != nil {
			return fail()
		}
		hash, _, err := inspectionGitRead(ctx, root, ".git/"+ref, 1024)
		if err == nil {
			value = strings.TrimSpace(string(hash))
		} else if _, statErr := root.Lstat(".git/" + ref); errors.Is(statErr, os.ErrNotExist) {
			value = ""
			unborn = true
			packed, _, readErr := inspectionGitRead(ctx, root, ".git/packed-refs", 1<<20)
			if readErr != nil {
				if _, statErr := root.Lstat(".git/packed-refs"); !errors.Is(statErr, os.ErrNotExist) {
					return fail()
				}
			} else {
				for line := range strings.SplitSeq(string(packed), "\n") {
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					if peeled, ok := strings.CutPrefix(line, "^"); ok {
						if !inspectionGitHex(peeled, 40) {
							return fail()
						}
						continue
					}
					hash, name, ok := strings.Cut(line, " ")
					if !ok || !inspectionGitHex(hash, 40) || !strings.HasPrefix(name, "refs/") {
						return fail()
					}
					if _, err := inspectionPath(name, false); err != nil {
						return fail()
					}
					if name == ref {
						value = hash
						unborn = false
					}
				}
			}
		} else {
			return fail()
		}
	}
	if (value == "" && !unborn) || (value != "" && !inspectionGitHex(value, 40)) {
		return fail()
	}
	s.head = value
	var index []byte
	if _, err := root.Lstat(".git/index"); err == nil {
		index, s.indexInfo, err = inspectionGitRead(ctx, root, ".git/index", 8<<20)
		if err != nil || !inspectionGitIndex(index) {
			return fail()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail()
	}
	// Never place temporary control files inside the inspected project, including
	// when an operator has configured TMPDIR to point there.
	canonical, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return fail()
	}
	relative, err := filepath.Rel(p.Path, canonical)
	if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return fail()
	}
	temp, err := os.MkdirTemp(canonical, "snow-inspection-git-")
	if err != nil {
		return fail()
	}
	s.temp = temp
	for _, name := range []string{"objects", "refs", "home", "xdg"} {
		if err := os.Mkdir(filepath.Join(temp, name), 0o700); err != nil {
			return fail()
		}
	}
	headValue := value + "\n"
	if value == "" {
		headValue = "ref: refs/heads/snow-unborn\n"
	}
	for name, data := range map[string][]byte{"HEAD": []byte(headValue), "config": []byte("[core]\n repositoryformatversion = 0\n bare = false\n logallrefupdates = false\n")} {
		if err := os.WriteFile(filepath.Join(temp, name), data, 0o600); err != nil {
			return fail()
		}
	}
	if index != nil {
		if err := os.WriteFile(filepath.Join(temp, "index"), index, 0o600); err != nil {
			return fail()
		}
	}
	if err := s.check(ctx); err != nil {
		return fail()
	}
	return s, nil
}

func inspectionGitRead(ctx context.Context, root *os.Root, path string, limit int64) ([]byte, os.FileInfo, error) {
	file, info, err := inspectionOpen(ctx, root, path, false)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	if info.Size() > limit {
		return nil, nil, ErrInspectionUnavailable
	}
	var result bytes.Buffer
	buffer := make([]byte, 8192)
	for int64(result.Len()) <= limit {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		n, readErr := file.Read(buffer[:min(int64(len(buffer)), limit+1-int64(result.Len()))])
		_, _ = result.Write(buffer[:n])
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || n == 0 {
			return nil, nil, ErrInspectionUnavailable
		}
	}
	if int64(result.Len()) > limit || int64(result.Len()) != info.Size() {
		return nil, nil, ErrInspectionUnavailable
	}
	if err := inspectionCheck(ctx, root, path, info); err != nil {
		return nil, nil, err
	}
	return result.Bytes(), info, nil
}

func inspectionGitHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Only SHA-1 full indexes v2/v3 are supported. Split/sparse indexes, conflicts,
// extended flags and unknown extensions fail closed rather than silently show a
// clean tree. TREE is merely a cache; all other extensions are refused.
func inspectionGitIndex(data []byte) bool {
	if len(data) < 32 || string(data[:4]) != "DIRC" {
		return false
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != 2 && version != 3 {
		return false
	}
	sum := sha1.Sum(data[:len(data)-20])
	if !bytes.Equal(sum[:], data[len(data)-20:]) {
		return false
	}
	count := binary.BigEndian.Uint32(data[8:12])
	if count > 65536 {
		return false
	}
	end := len(data) - 20
	offset := 12
	for range count {
		start := offset
		if offset+62 > end {
			return false
		}
		mode := binary.BigEndian.Uint32(data[offset+24 : offset+28])
		flags := binary.BigEndian.Uint16(data[offset+60 : offset+62])
		if flags&0x7000 != 0 || mode&0o170000 == 0o040000 {
			return false
		}
		offset += 62
		nul := bytes.IndexByte(data[offset:end], 0)
		if nul < 0 || nul > 4096 {
			return false
		}
		name := string(data[offset : offset+nul])
		// Sensitive names are not inspected, but may legitimately be tracked.
		if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
			return false
		}
		for part := range strings.SplitSeq(name, "/") {
			if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
				return false
			}
		}
		offset += nul + 1
		offset = start + ((offset-start+7)/8)*8
		if offset > end {
			return false
		}
	}
	for offset < end {
		if offset+8 > end || string(data[offset:offset+4]) != "TREE" {
			return false
		}
		size := int(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if size > end-offset {
			return false
		}
		offset += size
	}
	return offset == end
}

func (s *inspectionGitSnapshot) check(ctx context.Context) error {
	for path, info := range map[string]os.FileInfo{".git": s.gitInfo, ".git/objects": s.objectInfo, ".git/HEAD": s.headInfo} {
		if err := inspectionCheck(ctx, s.root, path, info); err != nil {
			return err
		}
	}
	if s.indexInfo != nil {
		if err := inspectionCheck(ctx, s.root, ".git/index", s.indexInfo); err != nil {
			return err
		}
	}
	for _, path := range []string{".git/objects/info/alternates", ".git/objects/info/http-alternates", ".git/shallow", ".git/commondir", ".git/info/grafts"} {
		if _, err := s.root.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return ErrInspectionUnavailable
		}
	}
	p := s.project
	p.checkIdentity()
	if !p.Available {
		return ErrInspectionUnavailable
	}
	return ctx.Err()
}

// Git's object lookup must not traverse symlinks or alternate stores. Bound
// enumeration independently of command output/time. Unsupported metadata is
// refused, including promisor packs that could request lazy network fetching.
func (s *inspectionGitSnapshot) checkObjects(ctx context.Context) error {
	queue := []string{".git/objects"}
	scanned := 0
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		dir, info, err := inspectionOpen(ctx, s.root, path, true)
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				_ = dir.Close()
				return err
			}
			entries, readErr := dir.ReadDir(64)
			for _, entry := range entries {
				scanned++
				if scanned > 16384 {
					_ = dir.Close()
					return ErrInspectionUnavailable
				}
				child := path + "/" + entry.Name()
				stat, err := s.root.Lstat(child)
				if err != nil || stat.Mode()&os.ModeSymlink != 0 {
					_ = dir.Close()
					return ErrInspectionUnavailable
				}
				if stat.IsDir() {
					if path != ".git/objects" || (entry.Name() != "info" && entry.Name() != "pack" && !inspectionGitHex(entry.Name(), 2)) {
						_ = dir.Close()
						return ErrInspectionUnavailable
					}
					queue = append(queue, child)
				} else {
					if !inspectionRegular(stat) || path == ".git/objects" {
						_ = dir.Close()
						return ErrInspectionUnavailable
					}
					if path == ".git/objects/info" {
						// Acceleration/server inventory files contain no alternate
						// object-store configuration or executable commands.
						if entry.Name() != "packs" && entry.Name() != "commit-graph" {
							_ = dir.Close()
							return ErrInspectionUnavailable
						}
					} else if path == ".git/objects/pack" {
						if entry.Name() == "multi-pack-index" {
							continue
						}
						base, ext, ok := strings.CutLast(entry.Name(), ".")
						hash, hasPrefix := strings.CutPrefix(base, "pack-")
						if !ok || !hasPrefix || !inspectionGitHex(hash, 40) || (ext != "pack" && ext != "idx" && ext != "rev" && ext != "bitmap" && ext != "keep") {
							_ = dir.Close()
							return ErrInspectionUnavailable
						}
					} else if !inspectionGitHex(entry.Name(), 38) {
						_ = dir.Close()
						return ErrInspectionUnavailable
					}
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					_ = dir.Close()
					return ErrInspectionUnavailable
				}
				break
			}
		}
		_ = dir.Close()
		if err := inspectionCheck(ctx, s.root, path, info); err != nil {
			return err
		}
	}
	return nil
}
