//go:build !windows

package vault

import (
	"errors"
	"syscall"
)

func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
