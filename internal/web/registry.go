//go:build darwin || linux

package web

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
	"uuid"

	_ "modernc.org/sqlite"
)

const (
	MaxProjects           = 100
	registryTimeout       = 5 * time.Second
	registryApplicationID = 1397641047 // SNOW; separate from session storage.
)

var (
	ErrProjectNotFound  = errors.New("web: project not found")
	ErrProjectDuplicate = errors.New("web: project is already registered")
	ErrProjectLimit     = errors.New("web: project registration limit reached")
	ErrProjectInvalid   = errors.New("web: invalid project name or directory")
	ErrRegistryUnsafe   = errors.New("web: unsafe manager storage")
	ErrRegistryBusy     = errors.New("web: manager storage is already in use")
	ErrRegistryClosed   = errors.New("web: project registry is closed")
	ErrRegistryStorage  = errors.New("web: project registry storage failed")
)

// Project is manager metadata, not authority to execute in a directory. Callers
// must Lookup immediately before use and require Available; later filesystem
// operations must still pin their own root. State is available, missing, or
// changed. Missing also covers inaccessible directories. Paths are sensitive
// presentation data and must not be included in public diagnostics.
type Project struct {
	ID              string
	Trusted         bool // Remembered activation consent only; never tool or extension permission.
	SkillsEnabled   bool // Saved startup preference; never activates skills or grants project trust.
	TrustRemembered bool // Saved consent exists; permits revocation even when unavailable.
	Pinned          bool // Manager-only organization; never execution authority.
	Archived        bool // removed_at soft-removal; exact-ID restore is explicit.
	Name            string
	Path            string
	Available       bool
	State           string
	Issue           string // Fixed safe explanation; never an OS or database diagnostic.
	device          string
	inode           string
}

// Registry owns one private, exclusively locked manager database. It never
// opens sessions or changes project files. Methods are safe for concurrent use.
// The lock is advisory: other programs running as this OS user are not contained.
type Registry struct {
	db        *sql.DB
	root      *os.Root
	path      string
	dirInfo   os.FileInfo
	dbInfo    os.FileInfo
	lease     *os.File
	leaseInfo os.FileInfo
	gate      chan struct{}
	closed    bool
}

// OpenRegistry creates only managerDir, never project directories. Existing
// storage must already be private and owned by this user; it is not repaired by
// chmod. One instance owns managerDir until Close or process exit.
func OpenRegistry(ctx context.Context, managerDir string) (*Registry, error) {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := registryDirectory(managerDir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrRegistryUnsafe
	}
	r := &Registry{root: root, path: path, gate: make(chan struct{}, 1)}
	fail := func(err error) (*Registry, error) { _ = r.Close(); return nil, err }
	r.dirInfo, err = root.Stat(".")
	if err != nil || !privateRegistryDirectory(r.dirInfo) {
		return fail(ErrRegistryUnsafe)
	}
	if err := r.checkDirectory(); err != nil {
		return fail(err)
	}
	r.lease, err = r.openPrivateFile("manager.lock")
	if err != nil {
		return fail(err)
	}
	r.leaseInfo, err = r.lease.Stat()
	if err != nil {
		return fail(ErrRegistryUnsafe)
	}
	if err := syscall.Flock(int(r.lease.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fail(ErrRegistryBusy)
		}
		return fail(ErrRegistryUnsafe)
	}
	file, err := r.openPrivateFile("manager.db")
	if err != nil {
		return fail(err)
	}
	r.dbInfo, err = file.Stat()
	_ = file.Close()
	if err != nil {
		return fail(ErrRegistryUnsafe)
	}
	if err := r.checkStorage(); err != nil {
		return fail(err)
	}
	u := url.URL{Scheme: "file", Path: filepath.Join(path, "manager.db")}
	q := u.Query()
	q.Set("mode", "rw") // Creation was already done with O_EXCL and mode 0600.
	q.Add("_pragma", "busy_timeout(1000)")
	u.RawQuery = q.Encode()
	r.db, err = sql.Open("sqlite", u.String())
	if err != nil {
		return fail(ErrRegistryStorage)
	}
	r.db.SetMaxOpenConns(1)
	r.db.SetMaxIdleConns(1)
	if err := r.db.PingContext(ctx); err != nil {
		return fail(registryError(ctx))
	}
	if err := r.checkStorage(); err != nil {
		return fail(err)
	}
	if err := r.migrate(ctx); err != nil {
		return fail(err)
	}
	if err := r.checkStorage(); err != nil {
		return fail(err)
	}
	return r, nil
}

// Close releases the database before its lifetime lock; repeated calls are safe.
func (r *Registry) Close() error {
	r.gate <- struct{}{}
	defer func() { <-r.gate }()
	if r.closed {
		return nil
	}
	r.closed = true
	var failed bool
	if r.db != nil {
		failed = r.db.Close() != nil
	}
	if r.lease != nil {
		// Closing releases flock, including on partially initialized instances.
		failed = r.lease.Close() != nil || failed
	}
	if r.root != nil {
		failed = r.root.Close() != nil || failed
	}
	if failed {
		return ErrRegistryStorage
	}
	return nil
}

func (r *Registry) enter(ctx context.Context) (func(), error) {
	select {
	case r.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	leave := func() { <-r.gate }
	if err := ctx.Err(); err != nil {
		leave()
		return nil, err
	}
	if r.closed {
		leave()
		return nil, ErrRegistryClosed
	}
	if err := r.checkStorage(); err != nil {
		leave()
		return nil, err
	}
	return leave, nil
}

// List includes missing/changed registrations, but excludes removed metadata.
func (r *Registry) List(ctx context.Context) ([]Project, error) {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer leave()
	trusted, err := r.loadProjectTrust(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,path,device,inode,skills_enabled FROM projects WHERE removed_at IS NULL ORDER BY created_at,id LIMIT ?`, MaxProjects+1)
	if err != nil {
		return nil, registryError(ctx)
	}
	defer rows.Close()
	projects := make([]Project, 0)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.device, &p.inode, &p.SkillsEnabled); err != nil {
			return nil, registryError(ctx)
		}
		if !validStoredProject(p) || len(projects) == MaxProjects {
			return nil, ErrRegistryStorage
		}
		p.TrustRemembered = trusted[p.ID]
		p.Trusted = p.TrustRemembered
		p.checkIdentity()
		projects = append(projects, p)
	}
	if rows.Err() != nil {
		return nil, registryError(ctx)
	}
	return projects, nil
}

// Lookup refreshes identity from the filesystem. An unavailable registration is
// returned without an error so the UI can display it, never as an empty project.
func (r *Registry) Lookup(ctx context.Context, id string) (Project, error) {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return Project{}, err
	}
	defer leave()
	trusted, err := r.loadProjectTrust(ctx)
	if err != nil {
		return Project{}, err
	}
	p, err := r.lookupRegistration(ctx, id)
	if err != nil {
		return Project{}, err
	}
	p.TrustRemembered = trusted[p.ID]
	p.Trusted = p.TrustRemembered && p.Available
	return p, nil
}

// lookupRegistration requires the registry gate and refreshes filesystem identity.
func (r *Registry) lookupRegistration(ctx context.Context, id string) (Project, error) {
	if !validProjectID(id) {
		return Project{}, ErrProjectNotFound
	}
	var p Project
	err := r.db.QueryRowContext(ctx, `SELECT id,name,path,device,inode,skills_enabled FROM projects WHERE id=? AND removed_at IS NULL`, id).Scan(&p.ID, &p.Name, &p.Path, &p.device, &p.inode, &p.SkillsEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrProjectNotFound
	}
	if err != nil {
		return Project{}, registryError(ctx)
	}
	if !validStoredProject(p) {
		return Project{}, ErrRegistryStorage
	}
	p.checkIdentity()
	if err := ctx.Err(); err != nil {
		return Project{}, err
	}
	return p, nil
}

// Add registers an existing canonical directory. Symlink aliases are resolved
// before duplicate checks. Removal followed by Add intentionally makes a new ID
// with newly validated identity, rather than resurrecting the old registration.
func (r *Registry) Add(ctx context.Context, name, path string) (Project, error) {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return Project{}, err
	}
	defer leave()
	if len(name) > 128 || path == "" || len(path) > 4096 || !utf8.ValidString(path) || strings.ContainsRune(path, 0) {
		return Project{}, ErrProjectInvalid
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Project{}, ErrProjectInvalid
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil || len(path) > 4096 || !utf8.ValidString(path) {
		return Project{}, ErrProjectInvalid
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(path)
	}
	if !validProjectName(name) {
		return Project{}, ErrProjectInvalid
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return Project{}, ErrProjectInvalid
	}
	dev, ino, ok := registryIdentity(info)
	if !ok {
		return Project{}, ErrProjectInvalid
	}
	p := Project{ID: uuid.New().String(), Name: name, Path: path, device: dev, inode: ino}
	p.checkIdentity()
	if !p.Available {
		return Project{}, ErrProjectInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, registryError(ctx)
	}
	defer tx.Rollback()
	var duplicate, count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE removed_at IS NULL AND (path=? OR (device=? AND inode=?))`, path, dev, ino).Scan(&duplicate); err != nil {
		return Project{}, registryError(ctx)
	}
	if duplicate != 0 {
		return Project{}, ErrProjectDuplicate
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE removed_at IS NULL`).Scan(&count); err != nil {
		return Project{}, registryError(ctx)
	}
	if count >= MaxProjects {
		return Project{}, ErrProjectLimit
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,path,device,inode,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, p.ID, p.Name, p.Path, dev, ino, time.Now().UnixMilli(), time.Now().UnixMilli())
	if err != nil {
		return Project{}, registryError(ctx)
	}
	p.checkIdentity()
	if !p.Available {
		return Project{}, ErrProjectInvalid
	}
	if err := tx.Commit(); err != nil {
		return Project{}, registryError(ctx)
	}
	return p, nil
}

// Remove soft-deletes only manager metadata. No project or session file is
// opened, renamed, removed, or otherwise modified.
func (r *Registry) Remove(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	leave, err := r.enter(ctx)
	if err != nil {
		return err
	}
	defer leave()
	if !validProjectID(id) {
		return ErrProjectNotFound
	}
	now := time.Now().UnixMilli()
	result, err := r.db.ExecContext(ctx, `UPDATE projects SET removed_at=?,updated_at=? WHERE id=? AND removed_at IS NULL`, now, now, id)
	if err != nil {
		return registryError(ctx)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return registryError(ctx)
	}
	if count == 0 {
		return ErrProjectNotFound
	}
	return nil
}

func (r *Registry) migrate(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return registryError(ctx)
	}
	defer tx.Rollback()
	var application, version int
	if tx.QueryRowContext(ctx, `PRAGMA application_id`).Scan(&application) != nil || tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version) != nil {
		return registryError(ctx)
	}
	if application == registryApplicationID && version == 1 {
		if err := migrateOrganization(ctx, tx); err != nil {
			return err
		}
		if err := migrateRecovery(ctx, tx); err != nil {
			return err
		}
		if err := migrateProjectOperations(ctx, tx); err != nil {
			return err
		}
		if err := migrateProjectTrust(ctx, tx); err != nil {
			return err
		}
		if err := migrateProjectSkills(ctx, tx); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return registryError(ctx)
		}
		return nil
	}
	if application != 0 || version != 0 {
		return ErrRegistryStorage
	}
	var tables int
	if tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'`).Scan(&tables) != nil || tables != 0 {
		return ErrRegistryStorage
	}
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE projects (
			id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=36),
			name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 128),
			path TEXT NOT NULL CHECK(length(path) BETWEEN 1 AND 4096),
			device TEXT NOT NULL, inode TEXT NOT NULL,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, removed_at INTEGER
		);
		CREATE UNIQUE INDEX projects_active_path ON projects(path) WHERE removed_at IS NULL;
		CREATE UNIQUE INDEX projects_active_identity ON projects(device,inode) WHERE removed_at IS NULL;
		CREATE TRIGGER projects_active_limit BEFORE INSERT ON projects
		WHEN NEW.removed_at IS NULL AND (SELECT count(*) FROM projects WHERE removed_at IS NULL) >= 100
		BEGIN SELECT RAISE(ABORT, 'project limit'); END;
		PRAGMA application_id=1397641047;
		PRAGMA user_version=1;
	`)
	if err != nil {
		return registryError(ctx)
	}
	if err := migrateOrganization(ctx, tx); err != nil {
		return err
	}
	if err := migrateRecovery(ctx, tx); err != nil {
		return err
	}
	if err := migrateProjectOperations(ctx, tx); err != nil {
		return err
	}
	if err := migrateProjectTrust(ctx, tx); err != nil {
		return err
	}
	if err := migrateProjectSkills(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return registryError(ctx)
	}
	return nil
}

func registryError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrRegistryStorage // Never expose driver errors, SQL, or host paths.
}

func validProjectID(id string) bool {
	if len(id) != 36 {
		return false
	}
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func validProjectName(name string) bool {
	if name == "" || len(name) > 128 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validStoredProject(p Project) bool {
	return validProjectID(p.ID) && validProjectName(p.Name) && len(p.Path) <= 4096 && utf8.ValidString(p.Path) && filepath.IsAbs(p.Path) && filepath.Clean(p.Path) == p.Path && !strings.ContainsRune(p.Path, 0) && p.device != "" && p.inode != ""
}

func registryIdentity(info os.FileInfo) (string, string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", false
	}
	return strconv.FormatUint(uint64(stat.Dev), 10), strconv.FormatUint(uint64(stat.Ino), 10), true
}

func (p *Project) checkIdentity() {
	defer func() { p.Trusted = p.Trusted && p.Available && !p.Archived }()
	p.Available, p.State = false, "missing"
	p.Issue = "Project directory is missing or inaccessible."
	canonical, err := filepath.EvalSymlinks(p.Path)
	if err != nil {
		return
	}
	if canonical != p.Path {
		p.State = "changed"
		p.Issue = "Project directory identity changed; remove and register it again."
		return
	}
	info, err := os.Lstat(p.Path)
	if err != nil {
		return
	}
	p.State = "changed"
	p.Issue = "Project directory identity changed; remove and register it again."
	if !info.IsDir() {
		return
	}
	dev, ino, ok := registryIdentity(info)
	if ok && dev == p.device && ino == p.inode {
		p.Available, p.State, p.Issue = true, "available", ""
	}
}

func privateRegistryDirectory(info os.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func privateRegistryFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1 && info.Size() <= 64<<20
}

func registryDirectory(path string) (string, error) {
	if path == "" || len(path) > 4096 {
		return "", ErrRegistryUnsafe
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", ErrRegistryUnsafe
	}
	// Canonicalize ancestors (e.g. macOS /var), but never a manager-dir link.
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		parent, err = registryDirectory(filepath.Dir(path))
	}
	if err != nil {
		return "", ErrRegistryUnsafe
	}
	path = filepath.Join(parent, filepath.Base(path))
	for dir := parent; ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			return "", ErrRegistryUnsafe
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) {
			return "", ErrRegistryUnsafe
		}
		// Root-owned sticky temporary directories are safe against other users
		// replacing this user's child; writable non-sticky parents are not.
		if info.Mode().Perm()&0o022 != 0 && !(stat.Uid == 0 && info.Mode()&os.ModeSticky != 0) {
			return "", ErrRegistryUnsafe
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", ErrRegistryUnsafe
	}
	info, err := os.Lstat(path)
	if err != nil || !privateRegistryDirectory(info) {
		return "", ErrRegistryUnsafe
	}
	return path, nil
}

func (r *Registry) openPrivateFile(name string) (*os.File, error) {
	before, err := r.root.Lstat(name)
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if errors.Is(err, os.ErrNotExist) {
		flags |= os.O_CREATE | os.O_EXCL
	} else if err != nil || !privateRegistryFile(before) {
		return nil, ErrRegistryUnsafe
	}
	file, err := r.root.OpenFile(name, flags, 0o600)
	if err != nil {
		return nil, ErrRegistryUnsafe
	}
	after, err := file.Stat()
	current, currentErr := r.root.Lstat(name)
	if err != nil || currentErr != nil || !privateRegistryFile(after) || !privateRegistryFile(current) || !os.SameFile(after, current) || (before != nil && !os.SameFile(before, after)) {
		_ = file.Close()
		return nil, ErrRegistryUnsafe
	}
	return file, nil
}

func (r *Registry) checkDirectory() error {
	info, err := os.Lstat(r.path)
	if err != nil || !privateRegistryDirectory(info) || !os.SameFile(r.dirInfo, info) {
		return ErrRegistryUnsafe
	}
	canonical, err := filepath.EvalSymlinks(r.path)
	if err != nil || canonical != r.path {
		return ErrRegistryUnsafe
	}
	return nil
}

func (r *Registry) checkStorage() error {
	if err := r.checkDirectory(); err != nil {
		return err
	}
	for _, entry := range []struct {
		name     string
		identity os.FileInfo
	}{{"manager.db", r.dbInfo}, {"manager.lock", r.leaseInfo}, {"manager.db-journal", nil}, {"manager.db-wal", nil}, {"manager.db-shm", nil}} {
		info, err := r.root.Lstat(entry.name)
		if errors.Is(err, os.ErrNotExist) && entry.identity == nil {
			continue
		}
		if err != nil || !privateRegistryFile(info) || (entry.identity != nil && !os.SameFile(entry.identity, info)) {
			return ErrRegistryUnsafe
		}
	}
	return nil
}
