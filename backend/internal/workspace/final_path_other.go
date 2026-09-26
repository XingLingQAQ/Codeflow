//go:build !windows

package workspace

import "path/filepath"

// finalPath resolves links with filepath.EvalSymlinks. On Unix the stdlib walk
// follows every symlink component, which is exactly the OS routing behaviour
// this primitive has to report; it fails for a path that does not exist.
func finalPath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
