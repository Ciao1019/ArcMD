//go:build unix

package fsutil

import (
	"os"
	"syscall"
)

func hasHardLinks(_ string, fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Nlink > 1
}
