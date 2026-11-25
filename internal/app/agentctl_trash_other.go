//go:build !windows

package app

import "errors"

func trashWindowsHeadless(string) error {
	return errors.New("windows recycle bin unavailable")
}
