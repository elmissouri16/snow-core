package javascript

import (
	"context"
	"path/filepath"
	"testing"
)

func TestScaffoldInitializesWithoutIO(t *testing.T) {
	for _, ts := range []bool{false, true} {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "demo")
		if err := Scaffold(path, "demo", ts); err != nil {
			t.Fatal(err)
		}
		p, err := ReadPackage(t.Context(), path, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		r := New(p, Options{})
		if err := r.Register(t.Context(), newRegistrar()); err != nil {
			t.Fatal(err)
		}
		if len(r.ExtensionInfo().Commands) != 1 {
			t.Fatal("missing command")
		}
		if err := r.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := Scaffold(path, "demo", ts); err == nil {
			t.Fatal("overwrote existing plugin")
		}
	}
}
