package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/runworkspace"
)

// TestHashBytesMatchesRunworkspace is the cross-check the plan asks for: the
// hash this package computes for a file's bytes is byte-for-byte the one
// runworkspace puts in a manifest entry for the same file. If the two ever
// drift, a diff could not compare a captured baseline against a stored blob.
//
// The comparison goes through runworkspace.Capture rather than a second
// hashBytes call inside runworkspace, so it exercises the value that actually
// ends up in a manifest (plain mode, so no git is needed).
func TestHashBytesMatchesRunworkspace(t *testing.T) {
	dir := t.TempDir()
	content := []byte("artifact cross-check\nsecond line\n")
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	m, err := runworkspace.Capture(context.Background(), nil, dir, runworkspace.CaptureOptions{})
	if err != nil {
		t.Fatalf("runworkspace.Capture: %v", err)
	}
	var manifestHash string
	for _, e := range m.Entries {
		if e.Path == "a.txt" {
			manifestHash = e.ContentHash
		}
	}
	if manifestHash == "" {
		t.Fatalf("manifest has no entry for a.txt: %+v", m.Entries)
	}

	got := HashBytes(content)
	if got != manifestHash {
		t.Errorf("HashBytes = %s, manifest entry = %s: the two must be identical", got, manifestHash)
	}

	// And the independent way: the raw sha256 of the bytes.
	sum := sha256.Sum256(content)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got != want {
		t.Errorf("HashBytes = %s, want %s", got, want)
	}
}

// TestHashBytesStable pins the encoding for the empty blob and a known digest,
// so a change to the prefix or the hex case cannot pass unnoticed.
func TestHashBytesStable(t *testing.T) {
	empty := HashBytes(nil)
	// sha256 of zero bytes, lowercase, with the algorithm tag.
	const wantEmpty = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if empty != wantEmpty {
		t.Errorf("HashBytes(nil) = %s, want %s", empty, wantEmpty)
	}
	if HashBytes([]byte("abc")) == HashBytes([]byte("abd")) {
		t.Error("HashBytes collided for different content")
	}
}

func TestValidHash(t *testing.T) {
	valid := HashBytes([]byte("x"))
	cases := []struct {
		name string
		hash string
		want bool
	}{
		{"freshly computed", valid, true},
		{"empty", "", false},
		{"no prefix", strings.TrimPrefix(valid, HashPrefix), false},
		{"wrong prefix", "sha512:" + valid[len(HashPrefix):], false},
		{"uppercase digest", HashPrefix + strings.ToUpper(valid[len(HashPrefix):]), false},
		{"too short", HashPrefix + valid[len(HashPrefix):len(valid)-1], false},
		{"too long", valid + "0", false},
		{"non-hex character", HashPrefix + strings.Repeat("z", hexLen), false},
		{"prefix only", HashPrefix, false},
		{"prefix repeated", HashPrefix + HashPrefix + valid[len(HashPrefix):], false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidHash(tc.hash); got != tc.want {
				t.Errorf("ValidHash(%q) = %v, want %v", tc.hash, got, tc.want)
			}
		})
	}
}

// TestHashDirNamePinsLayout stops the on-disk layout from drifting silently:
// a blob must live at sha256/<first two>/<remaining 62>.
func TestHashDirNamePinsLayout(t *testing.T) {
	hash := "sha256:" + strings.Repeat("ab", 32)
	shard, name := hashDirName(hash)
	if shard != "ab" {
		t.Errorf("shard = %q, want ab", shard)
	}
	if len(name) != 62 || name != strings.Repeat("ab", 31) {
		t.Errorf("name = %q (len %d), want the remaining 62 hex characters", name, len(name))
	}
}
