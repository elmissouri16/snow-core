package session

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DeleteWithIDs binds confirmation to expectedID, exclusively leases the root
// and every child database, stages the complete deletion set in a uniquely
// created quarantine directory, and revalidates the quarantined root before
// irreversible cleanup.
func (f *FileIndex) DeleteWithIDs(cwd, path, expectedID string) ([]string, error) {
	if strings.TrimSpace(expectedID) == "" {
		return nil, ErrNotFound
	}
	infos, err := f.List(cwd)
	if err != nil {
		return nil, err
	}
	requested, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrNotFound
	}
	target := ""
	for _, info := range infos {
		listed, listedErr := filepath.Abs(info.Path)
		if listedErr == nil && filepath.Clean(requested) == filepath.Clean(listed) && info.ID == expectedID {
			target = listed
			break
		}
	}
	if target == "" {
		return nil, ErrNotFound
	}
	rootPath, err := filepath.Abs(f.Root)
	if err != nil {
		return nil, fmt.Errorf("session: resolve sessions root: %w", err)
	}
	rel, err := filepath.Rel(rootPath, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !strings.HasSuffix(rel, ".db") {
		return nil, ErrNotFound
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("session: open sessions root: %w", err)
	}
	defer root.Close()

	var leases []*os.File
	databaseInfos := make(map[string]os.FileInfo)
	closeLeases := func() {
		for i := len(leases) - 1; i >= 0; i-- {
			unlockSessionFile(leases[i])
			_ = leases[i].Close()
		}
	}
	defer closeLeases()
	lockDatabase := func(database string) error {
		info, err := root.Lstat(database)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || !singleLink(info) {
			return errors.New("session: database must be a regular, non-aliased file")
		}
		lease, err := root.OpenFile(database+".lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		if err := tryLockSessionExclusive(lease); err != nil {
			_ = lease.Close()
			return err
		}
		leases = append(leases, lease)
		databaseInfos[database] = info
		return nil
	}
	if err := lockDatabase(rel); err != nil {
		if errors.Is(err, errSessionInUse) {
			return nil, err
		}
		return nil, fmt.Errorf("session: lock root deletion lease: %w", err)
	}
	recordedChildPaths, err := subagentChildSessionPaths(target)
	if err != nil {
		return nil, fmt.Errorf("session: list owned child histories: %w", err)
	}

	ownedIDs := []string{expectedID}
	agentsRel := rel + ".agents"
	if agentsInfo, err := root.Lstat(agentsRel); err == nil && agentsInfo.IsDir() {
		var childPaths []string
		walkErr := fs.WalkDir(root.FS(), agentsRel, func(childPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(childPath, ".db") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || !singleLink(info) {
				return errors.New("session: child database must be a regular, non-aliased file")
			}
			childPaths = append(childPaths, childPath)
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("session: inspect child histories: %w", walkErr)
		}
		sort.Strings(childPaths)
		for _, childPath := range childPaths {
			if err := lockDatabase(childPath); err != nil {
				if errors.Is(err, errSessionInUse) {
					return nil, err
				}
				return nil, fmt.Errorf("session: lock child deletion lease: %w", err)
			}
			childInfo, err := root.Stat(childPath)
			if err != nil {
				return nil, err
			}
			absoluteChild := filepath.Clean(filepath.Join(rootPath, childPath))
			child, include, inspectErr := inspectSQLiteSession(absoluteChild, cwd, childInfo.ModTime().UnixMilli())
			if inspectErr == nil && include && recordedChildPaths[absoluteChild] {
				ownedIDs = append(ownedIDs, child.ID)
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	parent := filepath.Dir(rel)
	var quarantine string
	for attempts := 0; attempts < 16; attempts++ {
		candidate := filepath.Join(parent, ".delete-"+randomSuffix())
		if err := root.Mkdir(candidate, 0o700); err == nil {
			quarantine = candidate
			break
		} else if !os.IsExist(err) {
			return nil, fmt.Errorf("session: create deletion quarantine: %w", err)
		}
	}
	if quarantine == "" {
		return nil, errors.New("session: could not allocate deletion quarantine")
	}
	removeQuarantine := true
	defer func() {
		if removeQuarantine {
			_ = root.RemoveAll(quarantine)
		}
	}()
	type stagedPath struct{ source, target string }
	staged := make([]stagedPath, 0, 5)
	rollback := func() error {
		var rollbackErrs []error
		for i := len(staged) - 1; i >= 0; i-- {
			if _, err := root.Lstat(staged[i].source); err == nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("restore destination already exists: %s", staged[i].source))
				continue
			} else if !os.IsNotExist(err) {
				rollbackErrs = append(rollbackErrs, err)
				continue
			}
			if err := root.Rename(staged[i].target, staged[i].source); err != nil && !os.IsNotExist(err) {
				rollbackErrs = append(rollbackErrs, err)
			}
		}
		return errors.Join(rollbackErrs...)
	}
	failAndRollback := func(failure error) error {
		rollbackErr := rollback()
		if rollbackErr != nil {
			// A failed restore leaves the only safe copy in quarantine.
			removeQuarantine = false
		}
		return errors.Join(failure, rollbackErr)
	}
	stage := func(source, name string) error {
		if _, err := root.Lstat(source); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		destination := filepath.Join(quarantine, name)
		if err := root.Rename(source, destination); err != nil {
			return err
		}
		staged = append(staged, stagedPath{source: source, target: destination})
		return nil
	}
	for _, item := range []struct{ source, name string }{
		{rel + "-wal", "session.db-wal"}, {rel + "-shm", "session.db-shm"},
		{rel + "-journal", "session.db-journal"}, {agentsRel, "agents"}, {rel, "session.db"},
	} {
		if err := stage(item.source, item.name); err != nil {
			return nil, failAndRollback(fmt.Errorf("session: quarantine deletion set: %w", err))
		}
	}
	quarantinedRel := filepath.Join(quarantine, "session.db")
	quarantinedInfo, err := root.Lstat(quarantinedRel)
	if err != nil || !quarantinedInfo.Mode().IsRegular() || !singleLink(quarantinedInfo) || !os.SameFile(databaseInfos[rel], quarantinedInfo) {
		if err == nil {
			err = errors.New("session root identity changed while staging")
		}
		return nil, failAndRollback(fmt.Errorf("session: inspect quarantined database: %w", err))
	}
	for childPath, originalInfo := range databaseInfos {
		if childPath == rel {
			continue
		}
		childRel, relErr := filepath.Rel(agentsRel, childPath)
		stagedChild := filepath.Join(quarantine, "agents", childRel)
		stagedInfo, statErr := root.Lstat(stagedChild)
		if relErr != nil || statErr != nil || !stagedInfo.Mode().IsRegular() || !singleLink(stagedInfo) || !os.SameFile(originalInfo, stagedInfo) {
			if statErr == nil {
				statErr = errors.New("session child identity changed while staging")
			}
			return nil, failAndRollback(fmt.Errorf("session: inspect quarantined child database: %w", statErr))
		}
	}
	quarantinedPath := filepath.Join(rootPath, quarantinedRel)
	info, include, err := inspectSQLiteSession(quarantinedPath, cwd, quarantinedInfo.ModTime().UnixMilli())
	if err != nil || !include || info.ID != expectedID {
		if err == nil {
			err = errors.New("session identity changed before deletion")
		}
		return nil, failAndRollback(fmt.Errorf("session: validate quarantined database: %w", err))
	}
	if err := root.RemoveAll(quarantine); err != nil {
		removeQuarantine = false
		return ownedIDs, fmt.Errorf("session: remove deletion quarantine: %w", err)
	}
	removeQuarantine = false
	if err := root.Remove(rel + ".lock"); err != nil && !os.IsNotExist(err) {
		return ownedIDs, fmt.Errorf("session: remove lifetime lease: %w", err)
	}
	_ = root.Remove(parent)
	return ownedIDs, nil
}

// List implements Index. Returns sessions sorted by most recently updated.
// It searches the collision-resistant directory and the legacy flattened
// directory, filtering the latter by its stored CWD so colliding projects can
// never see each other's sessions.
func (f *FileIndex) List(cwd string) ([]SessionInfo, error) {
	dirNames := []string{EncodeCWD(cwd), legacyEncodeCWD(cwd)}
	seenDirs := make(map[string]bool, len(dirNames))
	seenPaths := make(map[string]bool)
	var out []SessionInfo
	for _, dirName := range dirNames {
		dir := filepath.Join(f.Root, dirName)
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if info.IsDir() {
				if strings.HasSuffix(path, ".db.agents") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".db") || seenPaths[path] || !info.Mode().IsRegular() || !singleLink(info) {
				return nil
			}
			// Listing is read-only discovery: validate and inspect through a
			// query-only connection without journal changes or schema migration.
			sessionInfo, include, inspectErr := inspectSQLiteSession(path, cwd, info.ModTime().UnixMilli())
			if inspectErr != nil || !include {
				return nil // skip corrupt, partial, foreign, and root-only files
			}
			out = append(out, sessionInfo)
			seenPaths[path] = true
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// EncodeCWD returns a fixed-size, collision-resistant directory name for an
// absolute project path. The v2 prefix distinguishes it from legacy slugs.
func EncodeCWD(cwd string) string {
	cleaned := normalizeCWD(cwd)
	sum := sha256.Sum256([]byte(cleaned))
	return fmt.Sprintf("cwd-v2-%x", sum[:])
}

func legacyEncodeCWD(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	cleaned := filepath.Clean(abs)
	if cleaned == "." || cleaned == "" {
		cleaned, _ = os.Getwd()
	}
	return legacyEncodeCleanedCWD(cleaned)
}

// legacyEncodeCleanedCWD intentionally reproduces the original encoder byte
// for byte. In particular it removes only one leading hyphen and preserves
// trailing hyphens.
func legacyEncodeCleanedCWD(cleaned string) string {
	if cleaned == "/" {
		return "root"
	}
	enc := strings.ReplaceAll(cleaned, "/", "-")
	enc = strings.ReplaceAll(enc, ":", "-")
	enc = strings.TrimPrefix(enc, "-")
	if enc == "" {
		enc = "root"
	}
	return enc
}

func normalizeCWD(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	cleaned := filepath.Clean(abs)
	if cleaned == "." || cleaned == "" {
		if current, getwdErr := os.Getwd(); getwdErr == nil {
			cleaned = filepath.Clean(current)
		}
	}
	return cleaned
}

func sameCWD(left, right string) bool {
	return normalizeCWD(left) == normalizeCWD(right)
}
