// Package dbx is the backend's single SQLite connection factory.
//
// It exists so that driver registration, DSN construction and the pragma
// baseline live in exactly one place (ADR 0009). The driver is
// modernc.org/sqlite, a pure Go translation of SQLite, so databases work
// with CGO_ENABLED=0; the on-disk format is unchanged.
//
// Baseline applied to every physical connection of the returned pool (the
// driver applies DSN parameters on each connection open, so connections
// created later by database/sql get the same settings):
//
//   - foreign_keys = on (file and in-memory databases)
//   - journal_mode = WAL (file databases only; meaningless for in-memory)
//   - busy_timeout = 5000 ms
//
// WithTxLock additionally pins the BEGIN mode of every transaction
// (deferred/immediate/exclusive); see its doc comment for why a
// read-then-write transaction needs "immediate" under WAL.
//
// Path contract: pass a plain filesystem path for a file database (the
// parent directory must already exist), or ""/":memory:" for a private
// in-memory database. "file:" URIs are rejected: shared-cache memory URIs
// would break the per-Open isolation guarantee.
package dbx

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	// Registers the "sqlite" driver; this is the only file in the backend
	// allowed to name or blank-import a SQLite driver.
	_ "modernc.org/sqlite"
)

// DriverName is the database/sql driver name registered by
// modernc.org/sqlite. Callers use Open and should never need this; it is
// exported so tests can assert which driver backs a handle.
const DriverName = "sqlite"

// DefaultBusyTimeout is applied when WithBusyTimeout is not used.
const DefaultBusyTimeout = 5 * time.Second

type config struct {
	busyTimeoutMS int
	foreignKeys   bool
	wal           bool
	synchronous   string
	maxOpenConns  int
	pragmas       []string
	txLock        string
}

// Option configures Open.
type Option func(*config)

// WithTxLock sets the SQLite transaction mode every Begin/BeginTx on the
// returned handle uses: "deferred" (SQLite's own default), "immediate" or
// "exclusive". The value is case-insensitive; any other value makes Open
// return an error.
//
// Why this option exists: under WAL, a deferred transaction that reads first
// and writes second can be refused with SQLITE_BUSY_SNAPSHOT (extended code
// 517) when another connection commits between the two steps. SQLite does not
// invoke the busy handler for that upgrade failure, so busy_timeout cannot
// help and the transaction fails immediately. A read-then-write transaction
// (an expected_revision CAS is the canonical case) must therefore take the
// write lock at BEGIN, which is what "immediate" does.
//
// Not passing this option leaves the DSN byte-for-byte unchanged (no _txlock
// parameter is emitted), so existing callers keep SQLite's deferred default.
func WithTxLock(mode string) Option {
	return func(c *config) { c.txLock = mode }
}

// WithBusyTimeout sets the per-connection busy timeout. d <= 0 means no
// waiting: lock conflicts fail immediately with SQLITE_BUSY.
func WithBusyTimeout(d time.Duration) Option {
	return func(c *config) {
		ms := d.Milliseconds()
		if ms < 0 {
			ms = 0
		}
		c.busyTimeoutMS = int(ms)
	}
}

// WithForeignKeys toggles PRAGMA foreign_keys on every pooled connection.
// Default is on.
func WithForeignKeys(on bool) Option {
	return func(c *config) { c.foreignKeys = on }
}

// WithWAL toggles journal_mode=WAL for file databases. Default is on.
// Ignored for in-memory databases.
func WithWAL(on bool) Option {
	return func(c *config) { c.wal = on }
}

// WithSynchronous sets PRAGMA synchronous (e.g. "NORMAL", "FULL"). The
// empty default keeps the SQLite default (FULL under WAL: NORMAL is the
// WAL default; pass explicitly to pin).
func WithSynchronous(mode string) Option {
	return func(c *config) { c.synchronous = mode }
}

// WithMaxOpenConns applies (*sql.DB).SetMaxOpenConns. Ignored for
// in-memory databases, which are always pinned to a single connection:
// each pooled ":memory:" connection would otherwise be a distinct
// database and the handle would be incoherent.
func WithMaxOpenConns(n int) Option {
	return func(c *config) { c.maxOpenConns = n }
}

// WithPragma appends a verbatim PRAGMA body (e.g. "cache_size(-20000)")
// applied to every pooled connection via the driver's _pragma parameter.
// Values are executed, not validated by the driver; a malformed pragma
// makes Open fail.
func WithPragma(stmt string) Option {
	return func(c *config) { c.pragmas = append(c.pragmas, stmt) }
}

// Open opens a SQLite database at path and verifies the connection by
// pinging it, so DSN/pragma errors surface here rather than at first use.
// WithTxLock, when passed, must name a valid transaction mode or Open
// returns an error before any handle is created.
//
// The returned *sql.DB is owned by the caller, who must Close it. If Open
// fails it has already closed the half-open handle. For an in-memory
// database the handle is the database: closing it discards all content,
// and each Open call returns a database invisible to every other handle.
func Open(path string, opts ...Option) (*sql.DB, error) {
	cfg := config{
		busyTimeoutMS: int(DefaultBusyTimeout.Milliseconds()),
		foreignKeys:   true,
		wal:           true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	// Validate the transaction mode here rather than leaving it to the driver,
	// so the error names the option the caller passed.
	if cfg.txLock != "" {
		lower := strings.ToLower(cfg.txLock)
		switch lower {
		case "deferred", "immediate", "exclusive":
			cfg.txLock = lower
		default:
			return nil, fmt.Errorf("dbx: unknown transaction mode %q: want deferred, immediate or exclusive", cfg.txLock)
		}
	}

	memory := path == "" || path == ":memory:"
	var dsn string
	switch {
	case memory:
		dsn = ":memory:?" + cfg.query(false).Encode()
	case strings.HasPrefix(path, "file:"):
		return nil, fmt.Errorf("dbx: pass a filesystem path or \":memory:\", not a %q URI", "file:")
	case strings.ContainsRune(path, '?'):
		return nil, fmt.Errorf("dbx: path %q must not contain '?'", path)
	default:
		dsn = path + "?" + cfg.query(true).Encode()
	}

	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("dbx: open: %w", err)
	}
	if memory {
		db.SetMaxOpenConns(1)
	} else if cfg.maxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.maxOpenConns)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("dbx: connect: %w", err)
	}
	return db, nil
}

// query builds the DSN query applied to every physical connection. The
// shorthand keys are the same ones github.com/mattn/go-sqlite3 accepts;
// modernc.org/sqlite validates them (invalid values fail the connection)
// and applies busy_timeout first.
func (c *config) query(file bool) url.Values {
	q := url.Values{}
	q.Set("_busy_timeout", strconv.Itoa(c.busyTimeoutMS))
	q.Set("_foreign_keys", boolOnOff(c.foreignKeys))
	if file && c.wal {
		q.Set("_journal_mode", "WAL")
	}
	if c.synchronous != "" {
		q.Set("_synchronous", c.synchronous)
	}
	// Only emitted when the caller asked for it: without WithTxLock the DSN
	// must stay byte-for-byte what it was before the option existed.
	if c.txLock != "" {
		q.Set("_txlock", c.txLock)
	}
	for _, p := range c.pragmas {
		q.Add("_pragma", p)
	}
	return q
}

func boolOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
