// Package artifact owns the content-addressed blob store and the version rows
// of the runtime library (T1.09.b, plan §27.8 "成果内容", §19.1
// artifact_versions).
//
// Two stores, one hash family:
//
//   - BlobStore is the filesystem half. It writes immutable files under
//     root/sha256/aa/bbbb... and returns "sha256:<64 lowercase hex>" — exactly
//     the form runworkspace.Entry.ContentHash already uses for captured files
//     and symlink targets, so a manifest entry, a manifest diff and a stored
//     blob can be compared without translating between formats.
//   - The version rows are the SQL half (CreateVersionTx and friends over
//     runstore.Tx/Querier). A row is the visible fact: it says "artifact A had
//     this content at version N".
//
// Ordering rule, and the reason this package is one package rather than two:
// a blob is written first and a version row second. Between the two steps a
// crash can only leave a blob that no row references — invisible, harmless, and
// collectable by the retention job that T12.02 will add (which is why this
// package exposes no delete: it cannot know what a pin holds). The reverse
// order is never used, because a committed version whose blob is missing would
// be a visible promise the database cannot keep, and CreateVersionTx refuses to
// write one even when a caller gets the order wrong: it stats the blob and
// returns ErrBlobMissing rather than inserting a row.
//
// Dependency direction: this package imports runstore (for Tx/Querier) and the
// Go standard library only. runstore must not import artifact — it may import
// run and dbx and nothing else — so the version table's SQL lives in
// runstore/migrations and its accessors live here.
package artifact
