//go:build windows

package workspace

import (
	"fmt"
	"syscall"
)

// fileReadAttributes is FILE_READ_ATTRIBUTES (0x00000080). stdlib syscall does
// not export it, so it is spelled out here. Opening a directory handle needs
// this access right (plus FILE_FLAG_BACKUP_SEMANTICS); it is the minimum needed
// to read metadata and cannot modify anything.
const fileReadAttributes = 0x00000080

// captureRootIdentity opens a handle to the directory and reads its
// volume serial number and file index.
//
// The handle follows reparse points (the default, since FILE_FLAG_OPEN_REPARSE_POINT
// is not passed), so a junction yields the identity of its target - exactly the
// directory the OS routes writes to. FILE_FLAG_BACKUP_SEMANTICS is required to
// obtain a handle to a directory at all.
func captureRootIdentity(path string) (RootIdentity, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return RootIdentity{}, err
	}
	handle, err := syscall.CreateFile(
		p,
		fileReadAttributes,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return RootIdentity{}, err
	}
	defer syscall.CloseHandle(handle)

	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		return RootIdentity{}, err
	}
	return RootIdentity{
		Platform: "windows",
		Volume:   fmt.Sprintf("%08x", info.VolumeSerialNumber),
		FileID:   fmt.Sprintf("%08x:%08x", info.FileIndexHigh, info.FileIndexLow),
	}, nil
}
