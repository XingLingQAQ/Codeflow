//go:build windows

package workspace

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// GetFinalPathNameByHandleW flags. golang.org/x/sys/windows wraps the call but
// does not export the flag constants, so they are spelled out here. Both are
// zero, which is why they cannot be omitted from the call without changing the
// documented meaning:
//
//   - fileNameNormalized (FILE_NAME_NORMALIZED, 0) asks for the fully normalized
//     name; the alternative (FILE_NAME_OPENED, 0x8) would return the path as it
//     was opened, including 8.3 short components.
//   - volumeNameDos (VOLUME_NAME_DOS, 0) asks for a drive-letter path; the
//     alternative (VOLUME_NAME_NT, 0x2) would return a \Device\... path.
const (
	fileNameNormalized = 0x0
	volumeNameDos      = 0x0
)

// finalPath asks the OS for the final path of the path.
//
// GetFinalPathNameByHandleW returns the path the kernel resolves the handle to:
// it follows reparse points (junctions, mount points, symlinks), so a junction
// yields its target. FILE_NAME_NORMALIZED asks for the fully normalized name,
// which expands the 8.3 short components *of the path that was opened* to their
// long form.
//
// The handle is opened without FILE_FLAG_OPEN_REPARSE_POINT, so a junction is
// followed; FILE_FLAG_BACKUP_SEMANTICS is required to open a directory handle
// at all. FILE_READ_ATTRIBUTES is the minimum access right that can read a name
// and cannot modify anything. The handle is always closed.
//
// The result is normalized from the \\?\ prefixed form the API returns:
// "\\?\C:\dir" becomes "C:\dir" and "\\?\UNC\server\share" becomes
// "\\server\share". The caller has already made the path absolute.
//
// A junction target is returned in the spelling the OS has stored for it, which
// can itself contain 8.3 components. GetLongPathNameW then expands them, so the
// same directory always yields one byte-identical final path however it was
// reached; that is what makes the result comparable and usable as a map key or
// as a stored root. Both calls only read, and both handles are closed.
func finalPath(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		p,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	// The handle must be closed on every path out of this function.
	defer windows.CloseHandle(handle)

	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), fileNameNormalized|volumeNameDos)
	if err != nil {
		return "", err
	}
	// A return value >= the buffer size means the name was truncated.
	if n == 0 || int(n) >= len(buf) {
		return "", fmt.Errorf("final path: could not resolve %s (buffer %d, need %d)", path, len(buf), n)
	}
	final := stripExtendedPrefix(windows.UTF16ToString(buf[:n]))
	return longPathName(final), nil
}

// longPathName expands 8.3 components of an existing path to their long form.
// It is best effort: when the OS cannot report a long name the path is returned
// unchanged, because the path itself was already resolved successfully.
func longPathName(path string) string {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(p, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) >= len(buf) {
		return path
	}
	return windows.UTF16ToString(buf[:n])
}

// stripExtendedPrefix removes the \\?\ prefix the final-path API returns and
// converts the UNC spelling back to its ordinary form.
func stripExtendedPrefix(p string) string {
	switch {
	case strings.HasPrefix(p, `\\?\UNC\`):
		return `\\` + p[len(`\\?\UNC\`):]
	case strings.HasPrefix(p, `\\?\`):
		return p[len(`\\?\`):]
	default:
		return p
	}
}
