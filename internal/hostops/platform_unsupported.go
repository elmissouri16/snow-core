//go:build !darwin && !linux

package hostops

import (
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"os"
)

func supported() bool                              { return false }
func openChild(*os.Root, string) (*os.File, error) { return nil, ErrUnavailable }
func statIdentity(os.FileInfo, string) (protocol.HostDirectoryIdentity, error) {
	return protocol.HostDirectoryIdentity{}, ErrUnavailable
}
func enterHelperDirectory(*os.File, *os.File) error { return ErrUnavailable }

func validHelperDescriptors(uintptr, uintptr) bool { return false }
