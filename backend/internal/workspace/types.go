// Package workspace provides project-root-scoped filesystem access.
// Writes must go through an optional WriteGuard (internal/guard will implement it).
package workspace

import (
	"context"
	"time"
)

// Entry is a directory listing item.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"` // relative to project root, slash-separated
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size,omitempty"`
	ModTime time.Time `json:"mod_time"`
}

// FileContent is a file read result.
type FileContent struct {
	Path    string    `json:"path"`
	Content []byte    `json:"content"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// WriteMode controls whether content hits the real tree or shadow staging.
type WriteMode string

const (
	// WriteModeDirect writes to the project tree (default).
	WriteModeDirect WriteMode = "direct"
	// WriteModeStage writes under .codeflow/staging/ then requires Promote.
	WriteModeStage WriteMode = "stage"
)

// WriteRequest is a guarded write.
type WriteRequest struct {
	// Root is the absolute project root directory.
	Root string `json:"-"`
	// Path is relative to Root (slash or OS separators accepted).
	Path string `json:"path" binding:"required"`
	// Content is the full file body to write.
	Content []byte `json:"content"`
	// CreateParents creates intermediate directories when true.
	CreateParents bool `json:"create_parents,omitempty"`
	// Mode is direct (default) or stage (shadow staging under .codeflow/staging).
	Mode WriteMode `json:"mode,omitempty"`
}

// ListRequest lists a relative directory under Root.
type ListRequest struct {
	Root string `json:"-"`
	Path string `json:"path"` // empty = root
}

// ReadRequest reads a relative file under Root.
type ReadRequest struct {
	Root string `json:"-"`
	Path string `json:"path" binding:"required"`
}

// WriteGuard is the M3 write-intercept surface (implemented by internal/guard).
// When nil, Service still writes but logs that guard is absent (tests only).
type WriteGuard interface {
	BeforeWrite(ctx context.Context, absPath string, content []byte) error
}

// Service is the workspace filesystem API.
type Service interface {
	List(ctx context.Context, req *ListRequest) ([]Entry, error)
	Read(ctx context.Context, req *ReadRequest) (*FileContent, error)
	Write(ctx context.Context, req *WriteRequest) (*Entry, error)
	Stat(ctx context.Context, root, rel string) (*Entry, error)
	// Resolve returns the absolute path if and only if it stays within root.
	Resolve(root, rel string) (abs string, err error)
	// Promote moves a staged file from .codeflow/staging into the real tree (re-runs guard).
	Promote(ctx context.Context, root, rel string) (*Entry, error)
}
