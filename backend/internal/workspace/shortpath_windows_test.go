//go:build windows

package workspace

import (
	"golang.org/x/sys/windows"
)

// shortPathName returns the 8.3 spelling of path. It is used only by the
// FinalPath short/long-name test to prove the OS normalizes both spellings to
// one final path.
func shortPathName(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", err
	}
	if n == 0 || int(n) >= len(buf) {
		return path, nil
	}
	return windows.UTF16ToString(buf[:n]), nil
}
