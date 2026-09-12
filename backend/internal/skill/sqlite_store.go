package skill

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codeflow/backend/internal/dbx"
)

// sqliteSkillStore persists Skill JSON documents.
type sqliteSkillStore struct {
	db *sql.DB
}

func openSQLiteSkillStore(dbPath string) (*sqliteSkillStore, error) {
	if err := prepareSkillDBDir(dbPath); err != nil {
		return nil, err
	}
	db, err := dbx.Open(dbPath, dbx.WithMaxOpenConns(1))
	if err != nil {
		return nil, fmt.Errorf("open skill db: %w", err)
	}
	s := &sqliteSkillStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func prepareSkillDBDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create skill db dir: %w", err)
		}
	}
	return nil
}

func (s *sqliteSkillStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS skills (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  source TEXT NOT NULL,
  enabled INTEGER NOT NULL,
  payload TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_skills_name ON skills(name);
CREATE TABLE IF NOT EXISTS skill_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  skill_id TEXT NOT NULL,
  version TEXT NOT NULL,
  payload TEXT NOT NULL,
  archived_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_skill_versions_skill ON skill_versions(skill_id);
`)
	if err != nil {
		return fmt.Errorf("init skill schema: %w", err)
	}
	return nil
}

// sqlExecer is satisfied by *sql.DB and *sql.Tx, letting the statement
// helpers serve one-shot writes and the transactional UpdateWithHistory.
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// archiveVersion inserts a snapshot of sk into skill_versions and returns its row id.
func (s *sqliteSkillStore) archiveVersion(sk *Skill, archivedAt time.Time) (int64, error) {
	return archiveVersionExec(s.db, sk, archivedAt)
}

func archiveVersionExec(ex sqlExecer, sk *Skill, archivedAt time.Time) (int64, error) {
	if sk == nil || sk.ID == "" {
		return 0, fmt.Errorf("skill id required")
	}
	payload, err := json.Marshal(sk)
	if err != nil {
		return 0, err
	}
	res, err := ex.Exec(
		`INSERT INTO skill_versions (skill_id, version, payload, archived_at) VALUES (?, ?, ?, ?)`,
		sk.ID, sk.Version, string(payload), archivedAt.UTC().UnixMilli(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateWithHistory writes the new current skill payload, archives previous
// into skill_versions, and prunes the per-skill archive to keep snapshots in
// a single SQLite transaction: all three effects commit together or none do
// (E-03 contract; defined in T13.03.a, the registry is wired to it in
// T13.03.b). current and previous must be non-nil and share the same skill
// ID. keep must be >= 0 and bounds how many archived snapshots the skill
// retains after archiving; keep == 0 retains none, including the
// just-archived previous. On success it returns the row id and archive
// timestamp of the archived previous snapshot, so the caller can publish its
// in-memory history only after durability is known. A non-nil error means no
// fact changed.
func (s *sqliteSkillStore) UpdateWithHistory(current, previous *Skill, keep int) (archiveRowID int64, archivedAt time.Time, err error) {
	if current == nil || current.ID == "" {
		return 0, time.Time{}, fmt.Errorf("skill id required")
	}
	if previous == nil || previous.ID == "" {
		return 0, time.Time{}, fmt.Errorf("previous skill snapshot required")
	}
	if previous.ID != current.ID {
		return 0, time.Time{}, fmt.Errorf("current and previous must reference the same skill id")
	}
	if keep < 0 {
		return 0, time.Time{}, fmt.Errorf("keep must be >= 0")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("begin skill update tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once Commit succeeds
	if err := putSkillExec(tx, current); err != nil {
		return 0, time.Time{}, fmt.Errorf("write current skill: %w", err)
	}
	archivedAt = time.Now().UTC()
	archiveRowID, err = archiveVersionExec(tx, previous, archivedAt)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("archive previous skill: %w", err)
	}
	if err := pruneVersionsExec(tx, current.ID, keep); err != nil {
		return 0, time.Time{}, fmt.Errorf("prune skill versions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, time.Time{}, fmt.Errorf("commit skill update tx: %w", err)
	}
	return archiveRowID, archivedAt, nil
}

// pruneVersions keeps only the newest keep snapshots for skillID.
func (s *sqliteSkillStore) pruneVersions(skillID string, keep int) error {
	return pruneVersionsExec(s.db, skillID, keep)
}

func pruneVersionsExec(ex sqlExecer, skillID string, keep int) error {
	_, err := ex.Exec(`
DELETE FROM skill_versions
WHERE skill_id = ?
  AND id NOT IN (
    SELECT id FROM skill_versions WHERE skill_id = ? ORDER BY id DESC LIMIT ?
  )`, skillID, skillID, keep)
	return err
}

// loadAllVersions returns every archived snapshot ordered oldest-first per skill.
func (s *sqliteSkillStore) loadAllVersions() ([]SkillVersion, error) {
	rows, err := s.db.Query(`SELECT id, skill_id, version, payload, archived_at FROM skill_versions ORDER BY skill_id ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SkillVersion, 0)
	for rows.Next() {
		var (
			rowID          int64
			skillID        string
			version        string
			payload        string
			archivedMillis int64
		)
		if err := rows.Scan(&rowID, &skillID, &version, &payload, &archivedMillis); err != nil {
			return nil, err
		}
		var sk Skill
		if err := json.Unmarshal([]byte(payload), &sk); err != nil {
			return nil, err
		}
		out = append(out, SkillVersion{
			RowID:      rowID,
			SkillID:    skillID,
			Version:    version,
			ArchivedAt: time.UnixMilli(archivedMillis).UTC(),
			Skill:      cloneSkill(&sk),
		})
	}
	return out, rows.Err()
}

func (s *sqliteSkillStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *sqliteSkillStore) put(sk *Skill) error {
	return putSkillExec(s.db, sk)
}

func putSkillExec(ex sqlExecer, sk *Skill) error {
	if sk == nil || sk.ID == "" {
		return fmt.Errorf("skill id required")
	}
	payload, err := json.Marshal(sk)
	if err != nil {
		return err
	}
	enabled := 0
	if sk.Enabled {
		enabled = 1
	}
	_, err = ex.Exec(`
INSERT INTO skills (id, name, source, enabled, payload, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name=excluded.name,
  source=excluded.source,
  enabled=excluded.enabled,
  payload=excluded.payload,
  updated_at=excluded.updated_at
`, sk.ID, sk.Name, string(sk.Source), enabled, string(payload), sk.UpdatedAt.UTC().UnixMilli())
	return err
}

func (s *sqliteSkillStore) delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM skills WHERE id = ?`, id)
	return err
}

func (s *sqliteSkillStore) loadAll() ([]*Skill, error) {
	rows, err := s.db.Query(`SELECT payload FROM skills`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Skill, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var sk Skill
		if err := json.Unmarshal([]byte(payload), &sk); err != nil {
			return nil, err
		}
		out = append(out, cloneSkill(&sk))
	}
	return out, rows.Err()
}
