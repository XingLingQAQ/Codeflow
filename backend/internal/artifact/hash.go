package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// HashPrefix is the algorithm tag on every content hash this package writes or
// accepts. It is the same tag runworkspace uses for manifest entries, which is
// what lets a diff compare a captured file hash with a stored blob hash
// directly (§28 T1.09.b: "manifest、diff、备份引用同一套 hash").
const HashPrefix = "sha256:"

// hexLen is the number of hex characters a sha256 digest has.
const hexLen = 64

// HashBytes returns the content-addressed form of data: HashPrefix followed by
// 64 lowercase hex characters. It is the in-memory counterpart of BlobStore.Put
// and produces the identical string for the identical bytes.
//
// There is deliberately no streaming variant here; Put hashes while it writes,
// and a caller that only has a byte slice is writing a small object anyway.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return HashPrefix + hex.EncodeToString(sum[:])
}

// ValidHash reports whether hash is exactly HashPrefix followed by 64 lowercase
// hex characters.
//
// Callers use it as a precondition, not as a repair: a hash that does not match
// is rejected (ErrInvalidHash) rather than normalized, because upper-casing,
// trimming or re-prefixing a hash would mean this package silently accepted an
// identity that no other component would recognize.
func ValidHash(hash string) bool {
	rest, ok := strings.CutPrefix(hash, HashPrefix)
	if !ok || len(rest) != hexLen {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			// Uppercase hex is not accepted: the same content has exactly one
			// spelling, otherwise two rows could name one blob under two
			// identities and a comparison would miss it.
			return false
		}
	}
	return true
}

// hashDirName returns the two-character shard directory a hash lives in, and
// the remaining file name. It panics on an invalid hash: every caller has
// already validated with ValidHash, and a bad shard would silently write a blob
// where no reader looks.
func hashDirName(hash string) (shard, name string) {
	if !ValidHash(hash) {
		panic("artifact: hashDirName called with an invalid hash: " + hash)
	}
	hex := hash[len(HashPrefix):]
	return hex[:2], hex[2:]
}
