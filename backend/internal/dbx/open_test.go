package dbx_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeflow/backend/internal/dbx"
	sqlite "modernc.org/sqlite"
)

// sqlitePrimaryCode extracts the primary SQLite result code (extended code
// masked to its low byte), or -1 if err is not a *sqlite.Error.
func sqlitePrimaryCode(err error) int {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		return serr.Code() & 0xff
	}
	return -1
}

const (
	sqliteError      = 1  // SQLITE_ERROR
	sqliteBusy       = 5  // SQLITE_BUSY
	sqliteConstraint = 19 // SQLITE_CONSTRAINT
)

func pragmaInt(t *testing.T, db *sql.DB, name string) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatalf("read PRAGMA %s: %v", name, err)
	}
	return v
}

func pragmaString(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var v string
	if err := db.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatalf("read PRAGMA %s: %v", name, err)
	}
	return v
}

// TestOpenForeignKeys proves the factory's foreign_keys=on baseline is
// applied to every physical connection of the pool, not just the one that
// happened to run a statement-level PRAGMA.
func TestOpenForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fk.db")
	db, err := dbx.Open(path, dbx.WithMaxOpenConns(4))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	for _, stmt := range []string{
		`CREATE TABLE parent (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent(id))`,
		`INSERT INTO parent (id) VALUES (1)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	// Positive: a valid parent/child pair is accepted.
	if _, err := db.Exec(`INSERT INTO child (id, parent_id) VALUES (10, 1)`); err != nil {
		t.Fatalf("valid child insert rejected: %v", err)
	}

	// Pool-wide negative: hold every pooled connection open at once, then
	// run a bad foreign-key insert on each one. If any connection had FK
	// off, its insert would succeed.
	ctx := context.Background()
	conns := make([]*sql.Conn, 0, 4)
	for i := 0; i < 4; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire pooled conn %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	for i, c := range conns {
		_, err := c.ExecContext(ctx, `INSERT INTO child (id, parent_id) VALUES (?, ?)`, 100+i, 424242)
		if err == nil {
			t.Fatalf("conn %d accepted a row referencing a missing parent; foreign_keys is off", i)
		}
		if code := sqlitePrimaryCode(err); code != sqliteConstraint {
			t.Fatalf("conn %d: expected SQLITE_CONSTRAINT, got code=%d err=%v", i, code, err)
		}
	}
	// Release the pool before using db again: all 4 slots are checked out.
	for _, c := range conns {
		if err := c.Close(); err != nil {
			t.Fatalf("release conn: %v", err)
		}
	}
	var orphans int
	if err := db.QueryRow(`SELECT COUNT(*) FROM child WHERE parent_id = 424242`).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("%d orphan rows committed despite FK errors", orphans)
	}

	// Control: the same violation must succeed when FK is explicitly off,
	// proving the failures above come from the pragma, not the schema.
	dbOff, err := dbx.Open(path, dbx.WithForeignKeys(false))
	if err != nil {
		t.Fatalf("open with FK off: %v", err)
	}
	defer dbOff.Close()
	if _, err := dbOff.Exec(`INSERT INTO child (id, parent_id) VALUES (200, 424242)`); err != nil {
		t.Fatalf("control insert with foreign_keys=off must succeed, got: %v", err)
	}
	var got int
	if err := dbOff.QueryRow(`SELECT COUNT(*) FROM child WHERE parent_id = 424242`).Scan(&got); err != nil {
		t.Fatalf("count control rows: %v", err)
	}
	if got != 1 {
		t.Fatalf("control: expected 1 orphan row with FK off, got %d", got)
	}
}

// TestBusyTimeout proves a contending writer waits up to the configured
// busy timeout instead of failing immediately with SQLITE_BUSY, and that a
// short configured timeout really caps the wait.
func TestBusyTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")
	dbA, err := dbx.Open(path) // lock holder; default 5s budget
	if err != nil {
		t.Fatalf("open A: %v", err)
	}
	defer dbA.Close()
	if _, err := dbA.Exec(`CREATE TABLE t (v INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := dbA.Exec(`INSERT INTO t VALUES (1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// holdWriteLock leaves a committed-write transaction open on dbA for
	// holdFor; the returned func blocks until the commit has finished.
	holdWriteLock := func(holdFor time.Duration) func() {
		tx, err := dbA.Begin()
		if err != nil {
			t.Fatalf("begin lock tx: %v", err)
		}
		if _, err := tx.Exec(`UPDATE t SET v = v + 1`); err != nil {
			t.Fatalf("acquire write lock: %v", err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(holdFor)
			if err := tx.Commit(); err != nil {
				t.Errorf("commit lock tx: %v", err)
			}
		}()
		return func() { <-done }
	}

	t.Run("waits_then_succeeds", func(t *testing.T) {
		dbB, err := dbx.Open(path) // default busy timeout 5000ms
		if err != nil {
			t.Fatalf("open B: %v", err)
		}
		defer dbB.Close()

		unlock := holdWriteLock(500 * time.Millisecond)
		start := time.Now()
		if _, err := dbB.Exec(`UPDATE t SET v = v + 1`); err != nil {
			unlock()
			t.Fatalf("second writer got %v instead of waiting for the lock", err)
		}
		waited := time.Since(start)
		unlock()
		if waited < 300*time.Millisecond {
			t.Fatalf("second writer returned after %s; expected it to wait ~500ms for the lock", waited)
		}
		t.Logf("second writer waited %s (lock held 500ms, budget 5000ms) and succeeded", waited)
	})

	t.Run("short_timeout_fails_fast", func(t *testing.T) {
		dbC, err := dbx.Open(path, dbx.WithBusyTimeout(100*time.Millisecond))
		if err != nil {
			t.Fatalf("open C: %v", err)
		}
		defer dbC.Close()

		unlock := holdWriteLock(2 * time.Second)
		start := time.Now()
		_, err = dbC.Exec(`UPDATE t SET v = v + 1`)
		waited := time.Since(start)
		unlock()
		if err == nil {
			t.Fatal("expected SQLITE_BUSY with a 100ms budget against a 2s lock")
		}
		if code := sqlitePrimaryCode(err); code != sqliteBusy {
			t.Fatalf("expected SQLITE_BUSY, got code=%d err=%v", code, err)
		}
		if waited > 1500*time.Millisecond {
			t.Fatalf("waited %s: the 100ms busy budget did not cap the wait (lock released at 2s)", waited)
		}
		t.Logf("100ms budget produced SQLITE_BUSY after %s: %v", waited, err)
	})

	t.Run("zero_timeout_fails_immediately", func(t *testing.T) {
		dbD, err := dbx.Open(path, dbx.WithBusyTimeout(0))
		if err != nil {
			t.Fatalf("open D: %v", err)
		}
		defer dbD.Close()

		unlock := holdWriteLock(1500 * time.Millisecond)
		start := time.Now()
		_, err = dbD.Exec(`UPDATE t SET v = v + 1`)
		waited := time.Since(start)
		unlock()
		if err == nil {
			t.Fatal("expected immediate SQLITE_BUSY with a zero busy timeout")
		}
		if code := sqlitePrimaryCode(err); code != sqliteBusy {
			t.Fatalf("expected SQLITE_BUSY, got code=%d err=%v", code, err)
		}
		if waited > time.Second {
			t.Fatalf("zero busy timeout still waited %s", waited)
		}
		t.Logf("zero budget produced SQLITE_BUSY after %s", waited)
	})
}

// TestMemoryConnectionsIsolation proves each Open of an in-memory database
// is a private database: nothing written through one handle is visible
// through another.
func TestMemoryConnectionsIsolation(t *testing.T) {
	dbA, err := dbx.Open(":memory:")
	if err != nil {
		t.Fatalf("open A: %v", err)
	}
	dbB, err := dbx.Open(":memory:")
	if err != nil {
		t.Fatalf("open B: %v", err)
	}
	defer dbB.Close()

	if _, err := dbA.Exec(`CREATE TABLE marker (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("A create: %v", err)
	}
	if _, err := dbA.Exec(`INSERT INTO marker VALUES (1)`); err != nil {
		t.Fatalf("A insert: %v", err)
	}

	var n int
	if err := dbA.QueryRow(`SELECT COUNT(*) FROM marker`).Scan(&n); err != nil {
		t.Fatalf("A read back: %v", err)
	}
	if n != 1 {
		t.Fatalf("A: expected 1 row, got %d", n)
	}

	// Negative: B must not see A's schema or data.
	_, err = dbB.Exec(`INSERT INTO marker VALUES (2)`)
	if err == nil {
		t.Fatal("B inserted into A's in-memory table; the databases are not isolated")
	}
	if !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("B: expected a 'no such table' error, got: %v", err)
	}
	if code := sqlitePrimaryCode(err); code != sqliteError {
		t.Fatalf("B: expected SQLITE_ERROR, got code=%d err=%v", code, err)
	}

	// Close semantics: a closed handle rejects further use.
	if err := dbA.Close(); err != nil {
		t.Fatalf("close A: %v", err)
	}
	if err := dbA.Ping(); err == nil || !strings.Contains(err.Error(), "database is closed") {
		t.Fatalf("ping after Close: expected 'database is closed', got %v", err)
	}
}

// TestOpenAppliesPragmaBaseline pins the factory defaults of ADR 0009
// decision 5 and the option overrides.
func TestOpenAppliesPragmaBaseline(t *testing.T) {
	dir := t.TempDir()

	db, err := dbx.Open(filepath.Join(dir, "baseline.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if got := pragmaInt(t, db, "foreign_keys"); got != 1 {
		t.Fatalf("foreign_keys: want 1, got %d", got)
	}
	if got := pragmaString(t, db, "journal_mode"); got != "wal" {
		t.Fatalf("journal_mode: want wal, got %q", got)
	}
	if got := pragmaInt(t, db, "busy_timeout"); got != 5000 {
		t.Fatalf("busy_timeout: want 5000, got %d", got)
	}

	mem, err := dbx.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer mem.Close()
	if got := pragmaInt(t, mem, "foreign_keys"); got != 1 {
		t.Fatalf("memory foreign_keys: want 1, got %d", got)
	}
	if got := pragmaInt(t, mem, "busy_timeout"); got != 5000 {
		t.Fatalf("memory busy_timeout: want 5000, got %d", got)
	}

	custom, err := dbx.Open(
		filepath.Join(dir, "custom.db"),
		dbx.WithBusyTimeout(1234*time.Millisecond),
		dbx.WithForeignKeys(false),
		dbx.WithWAL(false),
		dbx.WithSynchronous("NORMAL"),
		dbx.WithMaxOpenConns(8),
	)
	if err != nil {
		t.Fatalf("open custom: %v", err)
	}
	defer custom.Close()
	if got := pragmaInt(t, custom, "busy_timeout"); got != 1234 {
		t.Fatalf("custom busy_timeout: want 1234, got %d", got)
	}
	if got := pragmaInt(t, custom, "foreign_keys"); got != 0 {
		t.Fatalf("custom foreign_keys: want 0, got %d", got)
	}
	if got := pragmaString(t, custom, "journal_mode"); got == "wal" {
		t.Fatalf("custom journal_mode: WAL must be off, got %q", got)
	}
	if got := pragmaInt(t, custom, "synchronous"); got != 1 { // 1 = NORMAL
		t.Fatalf("custom synchronous: want 1 (NORMAL), got %d", got)
	}
}

// TestShorthandAndPragmaCompatibility is ADR 0009 verification item 1:
// the pinned modernc.org/sqlite v1.57.0 accepts the go-sqlite3 shorthand
// keys used by the existing 16 production DSNs, the canonical _pragma=
// form, and rejects invalid values at connect time.
func TestShorthandAndPragmaCompatibility(t *testing.T) {
	dir := t.TempDir()

	check := func(t *testing.T, dsn string) {
		t.Helper()
		db, err := sql.Open(dbx.DriverName, dsn)
		if err != nil {
			t.Fatalf("sql.Open %q: %v", dsn, err)
		}
		defer db.Close()
		if err := db.Ping(); err != nil {
			t.Fatalf("ping %q: %v", dsn, err)
		}
		if got := pragmaInt(t, db, "foreign_keys"); got != 1 {
			t.Fatalf("%q: foreign_keys want 1, got %d", dsn, got)
		}
		if got := pragmaString(t, db, "journal_mode"); got != "wal" {
			t.Fatalf("%q: journal_mode want wal, got %q", dsn, got)
		}
		if got := pragmaInt(t, db, "busy_timeout"); got != 5000 {
			t.Fatalf("%q: busy_timeout want 5000, got %d", dsn, got)
		}
	}

	t.Run("mattn_shorthand_dsn_works_verbatim", func(t *testing.T) {
		// The exact F1-style DSN text the existing stores build.
		check(t, filepath.Join(dir, "shorthand.db")+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	})

	t.Run("canonical_pragma_form", func(t *testing.T) {
		check(t, filepath.Join(dir, "pragma.db")+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	})

	t.Run("fk_alias_and_numeric_value", func(t *testing.T) {
		check(t, filepath.Join(dir, "alias.db")+"?_fk=1&_journal=WAL&_timeout=5000")
	})

	t.Run("invalid_shorthand_value_rejected", func(t *testing.T) {
		for _, dsn := range []string{
			filepath.Join(dir, "bad1.db") + "?_foreign_keys=bogus",
			filepath.Join(dir, "bad2.db") + "?_journal_mode=BOGUS",
			filepath.Join(dir, "bad3.db") + "?_busy_timeout=soon",
		} {
			db, err := sql.Open(dbx.DriverName, dsn)
			if err != nil {
				t.Fatalf("sql.Open %q: %v", dsn, err)
			}
			err = db.Ping()
			db.Close()
			if err == nil {
				t.Fatalf("%q: invalid shorthand value must fail at connect time", dsn)
			}
			t.Logf("%q rejected as documented: %v", dsn, err)
		}
	})

	t.Run("factory_surfaces_bad_pragma_at_open", func(t *testing.T) {
		_, err := dbx.Open(filepath.Join(dir, "badp.db"), dbx.WithPragma("journal_mode(WAL"))
		if err == nil {
			t.Fatal("malformed WithPragma must make Open fail")
		}
		t.Logf("malformed pragma rejected: %v", err)
	})
}

// TestSharedMemoryDSNSemantics is ADR 0009 verification item 2: how the
// shared-cache memory DSNs used by the existing stores behave under the
// pinned driver, and why the factory pins ":memory:" to one connection.
func TestSharedMemoryDSNSemantics(t *testing.T) {
	probe := func(t *testing.T, dsn string) (shared bool) {
		t.Helper()
		a, err := sql.Open(dbx.DriverName, dsn)
		if err != nil {
			t.Fatalf("open A %q: %v", dsn, err)
		}
		defer a.Close()
		b, err := sql.Open(dbx.DriverName, dsn)
		if err != nil {
			t.Fatalf("open B %q: %v", dsn, err)
		}
		defer b.Close()
		if _, err := a.Exec(`CREATE TABLE shared_marker (id INTEGER PRIMARY KEY)`); err != nil {
			t.Fatalf("A create on %q: %v", dsn, err)
		}
		if _, err := a.Exec(`INSERT INTO shared_marker VALUES (1)`); err != nil {
			t.Fatalf("A insert on %q: %v", dsn, err)
		}
		var n int
		err = b.QueryRow(`SELECT COUNT(*) FROM shared_marker`).Scan(&n)
		switch {
		case err == nil:
			t.Logf("%q: B sees A's row (n=%d) -> shared database", dsn, n)
			return true
		case strings.Contains(err.Error(), "no such table"):
			t.Logf("%q: B cannot see A's table -> isolated database", dsn)
			return false
		default:
			t.Fatalf("B probe on %q: %v", dsn, err)
			return false
		}
	}

	unique := fmt.Sprintf("dbxtest_%d", time.Now().UnixNano())
	t.Run("named_shared_cache_memory_dsn_shares", func(t *testing.T) {
		if !probe(t, "file:"+unique+"?mode=memory&cache=shared") {
			t.Fatal("F2-style named shared-cache DSN must be visible across handles")
		}
	})
	t.Run("anonymous_shared_cache_memory_dsn_shares", func(t *testing.T) {
		if !probe(t, "file::memory:?cache=shared") {
			t.Fatal("F1-style anonymous shared-cache DSN must be visible across handles")
		}
	})

	t.Run("pooled_bare_memory_dsn_is_incoherent", func(t *testing.T) {
		// A bare ":memory:" handle with more than one pooled connection is a
		// set of distinct databases; this is why dbx.Open pins the pool to 1.
		db, err := sql.Open(dbx.DriverName, ":memory:")
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer db.Close()
		db.SetMaxOpenConns(2)

		ctx := context.Background()
		c1, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("conn1: %v", err)
		}
		defer c1.Close()
		if _, err := c1.ExecContext(ctx, `CREATE TABLE only_on_conn1 (id INTEGER PRIMARY KEY)`); err != nil {
			t.Fatalf("conn1 create: %v", err)
		}
		// c1 is still checked out, so this must run on a second connection.
		if _, err := db.ExecContext(ctx, `INSERT INTO only_on_conn1 VALUES (1)`); err == nil {
			t.Fatal("second pooled connection saw conn1's table; ':memory:' would be shared")
		} else if !strings.Contains(err.Error(), "no such table") {
			t.Fatalf("expected 'no such table' on the second connection, got: %v", err)
		} else {
			t.Logf("second pooled connection correctly blind to conn1's schema: %v", err)
		}
	})
}

// TestDatetimeRoundTrip is ADR 0009 verification item 5. Measured result
// on the pinned v1.57.0: a TEXT value in a column declared DATETIME scans
// as time.Time (rows.go parses declared DATE/DATETIME/TIMESTAMP columns by
// default; _texttotime only affects undeclared columns, _inttotime only
// INTEGER storage). This matches go-sqlite3's decltype-based parsing, so
// the ADR comparison row "modernc scans TEXT DATETIME as string" does not
// hold for the pinned version. The repository never scans the one
// DATETIME column it declares (config updated_at is write-only); this test
// pins the observed behavior so a future driver bump cannot change it
// silently.
func TestDatetimeRoundTrip(t *testing.T) {
	db, err := dbx.Open(filepath.Join(t.TempDir(), "dt.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE dt (id INTEGER PRIMARY KEY, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO dt DEFAULT VALUES`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var raw any
	if err := db.QueryRow(`SELECT updated_at FROM dt WHERE id = 1`).Scan(&raw); err != nil {
		t.Fatalf("scan: %v", err)
	}
	t.Logf("modernc v1.57.0 scans DATETIME DEFAULT CURRENT_TIMESTAMP as %T (%v)", raw, raw)
	ts, ok := raw.(time.Time)
	if !ok {
		t.Fatalf("pinned behavior changed: expected time.Time for a declared DATETIME column, got %T (%v)", raw, raw)
	}
	if ts.IsZero() {
		t.Fatal("DEFAULT CURRENT_TIMESTAMP produced a zero time")
	}
}

// TestOpenRejectsUnusableTarget covers the factory's own failure paths.
func TestOpenRejectsUnusableTarget(t *testing.T) {
	dir := t.TempDir()
	if _, err := dbx.Open("file::memory:?cache=shared"); err == nil {
		t.Fatal("a file: URI must be rejected; shared-cache DSNs break per-Open isolation")
	}
	if _, err := dbx.Open(filepath.Join(dir, "missing", "x.db")); err == nil {
		t.Fatal("a path whose parent directory does not exist must fail at Open")
	} else {
		t.Logf("missing parent rejected: %v", err)
	}
	if _, err := dbx.Open("bad?name.db"); err == nil {
		t.Fatal("a path containing '?' must be rejected")
	}
}
