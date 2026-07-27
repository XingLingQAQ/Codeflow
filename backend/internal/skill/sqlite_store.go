package skill

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// sqliteSkillStore persists Skill JSON documents.
type sqliteSkillStore struct {
	db *sql.DB
}

func openSQLiteSkillStore(dbPath string) (*sqliteSkillStore, error) {
	conn, err := buildSkillSQLiteConnString(dbPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", conn)
	if err != nil {
		return nil, fmt.Errorf("open skill db: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &sqliteSkillStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func buildSkillSQLiteConnString(dbPath string) (string, error) {
	if dbPath == "" || dbPath == ":memory:" {
		return "file:skill_mem?mode=memory&cache=shared&_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create skill db dir: %w", err)
		}
	}
	return fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", filepath.ToSlash(dbPath)), nil
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

// archiveVersion inserts a snapshot of sk into skill_versions and returns its row id.
func (s *sqliteSkillStore) archiveVersion(sk *Skill, archivedAt time.Time) (int64, error) {
	if sk == nil || sk.ID == "" {
		return 0, fmt.Errorf("skill id required")
	}
	payload, err := json.Marshal(sk)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(
		`INSERT INTO skill_versions (skill_id, version, payload, archived_at) VALUES (?, ?, ?, ?)`,
		sk.ID, sk.Version, string(payload), archivedAt.UTC().UnixMilli(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// pruneVersions keeps only the newest keep snapshots for skillID.
func (s *sqliteSkillStore) pruneVersions(skillID string, keep int) error {
	_, err := s.db.Exec(`
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
	_, err = s.db.Exec(`
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
