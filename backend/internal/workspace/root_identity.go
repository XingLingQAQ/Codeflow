package workspace

import (
	"fmt"
	"os"
	"strings"
)

// RootIdentity is an operating-system level identity for a directory.
//
// It is the identity of "the directory the OS actually writes into": capture
// follows every link, symlink and junction on the way, so a binding that was
// captured through a junction keeps the identity of the real target. Two paths
// that resolve to the same directory share one identity; the same path string
// can have two different identities over time (directory deleted and recreated,
// renamed away and recreated, or a junction re-pointed at another target).
//
// It deliberately contains no path: it is safe to put in logs, audit records
// and persisted binding revisions.
type RootIdentity struct {
	// Platform is "windows" or "unix" (the build tag family that produced it).
	Platform string
	// Volume identifies the volume the directory lives on
	// (Windows volume serial number, Unix st_dev).
	Volume string
	// FileID identifies the directory within its volume
	// (Windows file index, Unix st_ino).
	FileID string
}

// Equal reports whether both identities describe the same directory.
func (r RootIdentity) Equal(other RootIdentity) bool {
	return r.Platform == other.Platform && r.Volume == other.Volume && r.FileID == other.FileID
}

// IsZero reports whether the identity was never captured.
func (r RootIdentity) IsZero() bool {
	return r.Platform == "" && r.Volume == "" && r.FileID == ""
}

// String renders the identity for logs. It never contains a filesystem path.
func (r RootIdentity) String() string {
	if r.IsZero() {
		return "root-identity:none"
	}
	return fmt.Sprintf("root-identity:%s:%s:%s", r.Platform, r.Volume, r.FileID)
}

// CaptureRootIdentity returns the OS identity of an existing directory.
//
// The path may be a link, symlink or junction; the identity captured is the one
// of the directory the OS actually writes into after following the path. A
// missing path, a non-directory, or a platform where the identity cannot be
// read fails closed with an error.
func CaptureRootIdentity(path string) (RootIdentity, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return RootIdentity{}, fmt.Errorf("root identity: path is required")
	}
	// Stat follows links/junctions, matching what the OS will write through.
	info, err := os.Stat(path)
	if err != nil {
		return RootIdentity{}, fmt.Errorf("root identity: %w", err)
	}
	if !info.IsDir() {
		return RootIdentity{}, fmt.Errorf("root identity: not a directory: %s", path)
	}
	id, err := captureRootIdentity(path)
	if err != nil {
		return RootIdentity{}, fmt.Errorf("root identity: %w", err)
	}
	if id.IsZero() {
		return RootIdentity{}, fmt.Errorf("root identity: platform returned an empty identity for %s", path)
	}
	return id, nil
}
