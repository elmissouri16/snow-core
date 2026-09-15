package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWebManagerDirectoryFreezesRelativeSnowHome(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SNOW_HOME", "private-relative-home")
	expected, err := filepath.Abs(filepath.Join("private-relative-home", "manager"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := webManagerDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if got != expected || !filepath.IsAbs(got) {
		t.Fatalf("CLI manager directory is not absolute: got %q want %q", got, expected)
	}
	if _, err := os.Stat("private-relative-home"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deriving manager directory touched user storage")
	}
}

func TestWebManagerDirectoryDoesNotResolveOrReadProjectConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, home := range []string{"relative-home", filepath.Join(root, "absolute-home")} {
		t.Setenv("SNOW_HOME", home)
		got, err := webManagerDirectory()
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.Abs(filepath.Join(home, "manager"))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatal("manager root did not follow operator SNOW_HOME")
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("manager path derivation created or read runtime storage")
	}
}
