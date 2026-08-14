//go:build !darwin && !linux

package trust

import "errors"

func lockStoreFile(string) (func(), error) {
	return nil, errors.New("snow requires macOS or Linux")
}
