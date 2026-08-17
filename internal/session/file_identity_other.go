//go:build !darwin && !linux

package session

import "os"

func singleLink(os.FileInfo) bool { return true }
