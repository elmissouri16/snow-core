//go:build darwin || linux

package hostops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elmissouri16/snow-core/pkg/protocol"
)

func testService(t *testing.T) *Service {
	t.Helper()
	// Never executed by these tests: execution fixtures belong to cmd/snow.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{GitExecutable: executable, HelperExecutable: executable})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func testParent(t *testing.T) protocol.HostDirectoryIdentity {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	id, err := fileIdentity(file, path)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func testPrepare(t *testing.T) (*Prepared, protocol.RPCProjectPrepareParams) {
	t.Helper()
	params := protocol.RPCProjectPrepareParams{OperationID: "op-fixture", Parent: testParent(t), Leaf: "project"}
	p, err := testService(t).Prepare(t.Context(), params)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, params
}

func TestPrepareCreatesOnlyOnePrivateChildAndClosePreserves(t *testing.T) {
	p, params := testPrepare(t)
	result := p.Result()
	if result.OperationID != params.OperationID || result.Parent != params.Parent || result.Leaf != params.Leaf || result.Child.Path != filepath.Join(params.Parent.Path, params.Leaf) {
		t.Fatal(result)
	}
	if !matchesPath(result.Child) {
		t.Fatal("child identity does not match opened directory")
	}
	info, err := os.Stat(result.Child.Path)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal(info, err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(result.Child.Path)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestPrepareRejectsEveryExistingDestination(t *testing.T) {
	for _, kind := range []string{"directory", "file", "symlink", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			parent := testParent(t)
			path := filepath.Join(parent.Path, "project")
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0700)
			case "file":
				err = os.WriteFile(path, []byte("keep"), 0600)
			case "symlink":
				err = os.Symlink(parent.Path, path)
			case "dangling":
				err = os.Symlink(filepath.Join(parent.Path, "missing"), path)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = testService(t).Prepare(t.Context(), protocol.RPCProjectPrepareParams{OperationID: "op", Parent: parent, Leaf: "project"})
			if !errors.Is(err, ErrExists) {
				t.Fatal(err)
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("destination removed", err)
			}
		})
	}
}

func TestPrepareRejectsReplacedParentAndNoncanonicalAlias(t *testing.T) {
	parent := testParent(t)
	parent.Inode = "1"
	if _, err := testService(t).Prepare(t.Context(), protocol.RPCProjectPrepareParams{OperationID: "op", Parent: parent, Leaf: "project"}); !errors.Is(err, ErrIdentity) {
		t.Fatal(err)
	}
	actual := testParent(t)
	alias := filepath.Join(testParent(t).Path, "alias")
	if err := os.Symlink(actual.Path, alias); err != nil {
		t.Fatal(err)
	}
	actual.Path = alias
	if _, err := testService(t).Prepare(t.Context(), protocol.RPCProjectPrepareParams{OperationID: "op", Parent: actual, Leaf: "project"}); !errors.Is(err, ErrIdentity) {
		t.Fatal(err)
	}
}

func TestStartCloneRequiresExactGrantAndUnchangedBindings(t *testing.T) {
	for _, change := range []string{"operation", "identity", "url", "child", "parent", "child-symlink"} {
		t.Run(change, func(t *testing.T) {
			p, params := testPrepare(t)
			start := protocol.RPCProjectCloneStartParams{OperationID: params.OperationID, Child: p.Result().Child, URL: "https://example.invalid/repo"}
			switch change {
			case "operation":
				start.OperationID = "another-op"
			case "identity":
				start.Child.Inode = "1"
			case "url":
				start.URL = "ssh://example.invalid/repo"
			case "child", "child-symlink":
				if err := os.Rename(start.Child.Path, start.Child.Path+"-original"); err != nil {
					t.Fatal(err)
				}
				var err error
				if change == "child" {
					err = os.Mkdir(start.Child.Path, 0700)
				} else {
					err = os.Symlink(start.Child.Path+"-original", start.Child.Path)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "parent":
				if err := os.Rename(params.Parent.Path, params.Parent.Path+"-original"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(params.Parent.Path + "-original") })
				if err := os.Mkdir(params.Parent.Path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if c, err := p.StartClone(t.Context(), start); err == nil {
				c.Cancel()
				<-c.Done()
				t.Fatal("accepted changed binding")
			}
		})
	}
}

func TestCloneGateCanBeCanceledWithoutExecutionAndNeverDeletes(t *testing.T) {
	p, params := testPrepare(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	start := protocol.RPCProjectCloneStartParams{OperationID: params.OperationID, Child: p.Result().Child, URL: "https://example.invalid/repo"}
	clone, err := p.StartClone(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	if _, ready := clone.Completion(); ready {
		t.Fatal("ran before Release")
	}
	if _, err := p.StartClone(ctx, start); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	_ = p.Close()
	clone.Cancel()
	select {
	case <-clone.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel did not complete")
	}
	result, ready := clone.Completion()
	if !ready || result.Status != protocol.HostOperationCanceled {
		t.Fatal(result, ready)
	}
	clone.Release() // A late ACK cannot revive a canceled clone.
	entries, err := os.ReadDir(start.Child.Path)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

func TestCreateDoesNotRequireGitComposition(t *testing.T) {
	service, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := service.Prepare(t.Context(), protocol.RPCProjectPrepareParams{OperationID: "create-only", Parent: testParent(t), Leaf: "project"})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	result := prepared.Result()
	if _, err := prepared.StartClone(t.Context(), protocol.RPCProjectCloneStartParams{OperationID: result.OperationID, Child: result.Child, URL: "https://example.invalid/fictional"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if !matchesPath(result.Child) {
		t.Fatal("create-only child was removed")
	}
}

func TestReleaseRechecksBindingsWithoutExecution(t *testing.T) {
	p, params := testPrepare(t)
	clone, err := p.StartClone(t.Context(), protocol.RPCProjectCloneStartParams{OperationID: params.OperationID, Child: p.Result().Child, URL: "https://example.invalid/fictional"})
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Cancel()
	path := p.Result().Child.Path
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	clone.Release()
	select {
	case <-clone.Done():
	case <-time.After(time.Second):
		t.Fatal("release did not reject changed binding")
	}
	result, ready := clone.Completion()
	if !ready || result.Status != protocol.HostOperationFailed {
		t.Fatal(result, ready)
	}
}
