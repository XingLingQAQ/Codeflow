//go:build !windows

package workspace

import (
	"fmt"
	"os"
	"syscall"
)

// captureRootIdentity reads the device and inode of the directory.
//
// os.Stat follows symlinks, so a symlinked root yields the identity of its
// target - exactly the directory the OS routes writes to. Stat_t.Dev/Ino are
// stable across renames within a filesystem, which is what makes "the same
// directory under a new path" detectable as the same identity.
func captureRootIdentity(path string) (RootIdentity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return RootIdentity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return RootIdentity{}, fmt.Errorf("stat does not expose device/inode information for %s", path)
	}
	return RootIdentity{
		Platform: "unix",
		Volume:   fmt.Sprintf("%08x", uint64(stat.Dev)),
		FileID:   fmt.Sprintf("%08x", uint64(stat.Ino)),
	}, nil
}
