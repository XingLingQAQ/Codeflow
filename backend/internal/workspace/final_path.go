package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FinalPath returns the real path the operating system routes operations to.
//
// It is the "final path" of the path, not the path as spelled:
//
//   - every link, symlink and junction on the way is followed, so a junction
//     yields the directory it points at rather than the junction path itself;
//   - the name is the OS-normalized long name, so 8.3 short names ("PROGRA~1")
//     and their long form yield the same result.
//
// The result is *the path of the directory*, not an identity: on Windows it is
// the spelling the OS uses for the file object the handle was opened on, and
// the same directory can be reachable by several such paths (a hardlinked
// directory entry, a volume mounted at a second drive letter, a symlinked file
// whose target is a directory). Path comparison is therefore a necessary but
// not a sufficient uniqueness check; pair it with RootIdentity
// (CaptureRootIdentity / RootIdentity.Equal), which compares the file object
// itself, whenever "is this still the same directory" is the question. The
// credential check for a root lives in RootIdentity and is not weakened by this
// helper.
//
// It fails closed for a path that does not exist: there is nothing to resolve
// and no way to know where a future create would land, so callers must not
// treat an error as "allow". The path must exist; a plain file is accepted
// (the primitive is used for directories but is not restricted to them).
//
// FinalPath only reads: on Windows it opens a handle with FILE_READ_ATTRIBUTES
// and closes it before returning.
func FinalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("final path: path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("final path: %w", err)
	}
	final, err := finalPath(abs)
	if err != nil {
		return "", fmt.Errorf("final path: %w", err)
	}
	if strings.TrimSpace(final) == "" {
		return "", fmt.Errorf("final path: platform returned an empty path for %s", path)
	}
	return filepath.Clean(final), nil
}
