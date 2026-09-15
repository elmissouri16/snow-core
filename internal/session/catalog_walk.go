package session

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const catalogReadDirBatch = 64

var errCatalogScanLimit = errors.New("catalog: session inventory exceeds scan limit")

// catalogWalk deliberately does not use fs.WalkDir: WalkDir materializes and
// sorts an entire directory before visiting its first entry. ReadDir(n) keeps
// even one enormous directory bounded before catalog's shared budget applies.
func catalogWalk(ctx context.Context, root *os.Root, start string, remaining *int, visit func(string, fs.DirEntry) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := root.Lstat(start)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("catalog: cannot inspect session directory")
	}
	if !info.IsDir() {
		return nil
	}
	if *remaining == 0 {
		return errCatalogScanLimit
	}
	*remaining--
	pending := []string{start}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		before, err := root.Lstat(dir)
		if err != nil || !before.IsDir() {
			return errors.New("catalog: session directory changed")
		}
		file, err := root.Open(dir)
		if err != nil {
			return errors.New("catalog: cannot open session directory")
		}
		after, err := file.Stat()
		if err != nil || !after.IsDir() || !os.SameFile(before, after) {
			_ = file.Close()
			return errors.New("catalog: session directory changed")
		}
		err = catalogReadDirectory(ctx, file.ReadDir, remaining, func(entry fs.DirEntry) error {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if !strings.HasSuffix(path, ".db.agents") {
					pending = append(pending, path)
				}
				return nil
			}
			return visit(path, entry)
		})
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return errors.New("catalog: close session directory failed")
		}
	}
	return nil
}

func catalogReadDirectory(ctx context.Context, readDir func(int) ([]fs.DirEntry, error), remaining *int, visit func(fs.DirEntry) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// One extra entry detects budget exhaustion without materializing the
		// remainder. Positive n is essential: n <= 0 means read everything.
		batch, err := readDir(min(catalogReadDirBatch, *remaining+1))
		if len(batch) > *remaining {
			return errCatalogScanLimit
		}
		*remaining -= len(batch)
		for _, entry := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(entry); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("catalog: cannot inspect session directory")
		}
	}
}
