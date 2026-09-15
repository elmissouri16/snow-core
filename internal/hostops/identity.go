package hostops

import (
	"github.com/elmissouri16/snow-core/pkg/protocol"
	"os"
)

func fileIdentity(file *os.File, path string) (protocol.HostDirectoryIdentity, error) {
	info, err := file.Stat()
	if err != nil {
		return protocol.HostDirectoryIdentity{}, ErrIdentity
	}
	return statIdentity(info, path)
}
