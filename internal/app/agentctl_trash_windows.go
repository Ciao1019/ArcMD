//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modShell32          = syscall.NewLazyDLL("shell32.dll")
	procSHFileOperation = modShell32.NewProc("SHFileOperationW")
)

const (
	foDelete          = 0x0003
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
	fofNoErrorUI      = 0x0400
)

type shFileOpStructW64 struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

func trashWindowsHeadless(abs string) error {
	clean := filepath.Clean(abs)
	if strings.HasPrefix(clean, `\\`) {
		return errors.New("recycle bin: refusing to trash network UNC path")
	}
	vol := filepath.VolumeName(clean)
	if len(vol) != 2 || vol[1] != ':' {
		return fmt.Errorf("recycle bin: unsupported volume path %q", clean)
	}
	if filepath.Dir(clean) == clean {
		return errors.New("recycle bin: refusing to trash volume root")
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return err
	}

	if unsafe.Sizeof(uintptr(0)) == 8 {
		utf16Path, uErr := syscall.UTF16FromString(clean)
		if uErr != nil {
			return uErr
		}
		// PCZZWSTR requires double NUL termination.
		utf16Path = append(utf16Path, 0)
		op := shFileOpStructW64{
			wFunc:  foDelete,
			pFrom:  &utf16Path[0],
			fFlags: fofAllowUndo | fofNoConfirmation | fofNoErrorUI | fofSilent,
		}
		ret, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op)))
		runtime.KeepAlive(utf16Path)
		if ret == 0 && op.fAnyOperationsAborted == 0 {
			return nil
		}
		if op.fAnyOperationsAborted != 0 {
			return errors.New("recycle bin: operation aborted")
		}
		return fmt.Errorf("recycle bin: SHFileOperationW failed with code 0x%x", ret)
	}

	method := "DeleteFile"
	if info.IsDir() {
		method = "DeleteDirectory"
	}
	quoted := strings.ReplaceAll(clean, "'", "''")
	ps := fmt.Sprintf(
		"Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.FileIO.FileSystem]::%s('%s', 'NoDialogs', 'SendToRecycleBin')",
		method, quoted,
	)
	out, runErr := runHeadlessTrashCmd("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	if runErr != nil {
		return fmt.Errorf("recycle bin: %w (%s)", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}
