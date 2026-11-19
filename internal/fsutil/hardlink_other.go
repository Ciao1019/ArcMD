//go:build !unix && !windows

package fsutil

import "os"

func hasHardLinks(string, os.FileInfo) bool { return false }
