//go:build windows

package builtin

func makeTestFIFO(string) (bool, error) { return false, nil }
