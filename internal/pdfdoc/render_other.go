//go:build (!darwin || ios) && !linux && !windows

package pdfdoc

import "errors"

func openPlatformFile(path string) (platformDoc, error) {
	return nil, errors.New("no native pdf backend")
}

func openPlatformBytes(data []byte) (platformDoc, error) {
	return nil, errors.New("no native pdf backend")
}
