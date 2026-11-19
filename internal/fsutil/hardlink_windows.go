//go:build windows

package fsutil

import (
	"os"
	"syscall"
)

func hasHardLinks(path string, _ os.FileInfo) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	rc, err := f.SyscallConn()
	if err != nil {
		return false
	}
	var info syscall.ByHandleFileInformation
	var gerr error
	if err := rc.Control(func(fd uintptr) {
		gerr = syscall.GetFileInformationByHandle(syscall.Handle(fd), &info)
	}); err != nil || gerr != nil {
		return false
	}
	return info.NumberOfLinks > 1
}
