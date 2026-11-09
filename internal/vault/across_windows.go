//go:build windows

package vault

import (
	"errors"
	"syscall"
)

// ERROR_NOT_SAME_DEVICE: the file can't be moved to a different disk drive.
const errNotSameDevice = syscall.Errno(17)

func isCrossDevice(err error) bool { return errors.Is(err, errNotSameDevice) }
