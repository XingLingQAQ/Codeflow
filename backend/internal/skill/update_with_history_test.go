package skill

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTxTestStore(t *testing.T) *sqliteSkillStore {
	t.Helper()
	store, err := openSQLiteSkillStore(filepath.Join(t.TempDir(), "skills_tx.db"))
	if err != nil {
		t.Fatalf("open sqlite skill store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func loadSkillByID(t *testing.T, store *sqliteSkillStore, id string) *Skill {
	t.Helper()
	all, err := store.loadAll()
	if err != nil {
		t.Fatalf("loadAll: %v", err)
	}
	for _, sk := range all {
		if sk.ID == id {
			return sk
		}
	}
	return nil
}

func loadVersionsFor(t *testing.T, store *sqliteSkillStore, skillID string) []SkillVersion {
	t.Helper()
	vers, err := store.loadAllVersions()
	if err != nil {
		t.Fatalf("loadAllVersions: %v", err)
	}
	out := make([]SkillVersion, 0, len(vers))
	for _, v := range vers {
		if v.SkillID == skillID {
			out = append(out, v)
		}
	}
	return out
}

// Current write, archive, and prune commit as one durable unit (E-03 contract).
func TestUpdateWithHistoryCommitsCurrentArchiveAndPruneTogether(t *testing.T) {
	store := openTxTestStore(t)
	base := &Skill{ID: "sk1", Name: "One", Version: "1", Body: "b1", Source: SourceUser, Enabled: true}
	if err := store.put(base); err != nil {
		t.Fatal(err)
	}
	// Seed two archived snapshots so the prune step has something to cut.
	for _, body := range []string{"a0", "a1"} {
		prev := *base
		prev.Body = body
		if _, err := store.archiveVersion(&prev, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}

	next := *base
	next.Body = "b2"
	next.Version = "2"
	prevSnap := *base // the state being replaced
	rowID, archivedAt, err := store.UpdateWithHistory(&next, &prevSnap, 2)
	if err != nil {
		t.Fatalf("UpdateWithHistory: %v", err)
	}
	if rowID <= 0 {
		t.Fatalf("expected positive archive row id, got %d", rowID)
	}

	cur := loadSkillByID(t, store, "sk1")
	if cur == nil || cur.Body != "b2" || cur.Version != "2" {
		t.Fatalf("current skill not updated: %+v", cur)
	}

	// keep=2 retains the newest two snapshots: seeded "a1" plus the archived
	// previous "b1"; the oldest seeded "a0" is pruned in the same transaction.
	vers := loadVersionsFor(t, store, "sk1")
	if len(vers) != 2 {
		t.Fatalf("expected 2 retained versions, got %d (%+v)", len(vers), vers)
	}
	if vers[0].Skill.Body != "a1" || vers[1].Skill.Body != "b1" {
		t.Fatalf("retained versions wrong: %q, %q", vers[0].Skill.Body, vers[1].Skill.Body)
	}
	if vers[1].RowID != rowID {
		t.Fatalf("archived row id=%d, UpdateWithHistory returned %d", vers[1].RowID, rowID)
	}
	if vers[1].ArchivedAt.UnixMilli() != archivedAt.UnixMilli() {
		t.Fatalf("archived_at=%v, UpdateWithHistory returned %v", vers[1].ArchivedAt, archivedAt)
	}
}

// A failure in the archive step rolls the current write back: no fact changes.
func TestUpdateWithHistoryRollsBackWhenArchiveFails(t *testing.T) {
	store := openTxTestStore(t)
	base := &Skill{ID: "sk1", Name: "One", Version: "1", Body: "b1", Source: SourceUser, Enabled: true}
	if err := store.put(base); err != nil {
		t.Fatal(err)
	}
	// Deterministic mid-transaction failure: the archive INSERT aborts after
	// the current-row upsert has already executed inside the same tx.
	if _, err := store.db.Exec(`CREATE TRIGGER fail_skill_archive
BEFORE INSERT ON skill_versions
BEGIN SELECT RAISE(ABORT, 'injected archive failure'); END;`); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	next := *base
	next.Body = "should-not-persist"
	if _, _, err := store.UpdateWithHistory(&next, base, 20); err == nil {
		t.Fatal("expected injected archive failure")
	} else if !strings.Contains(err.Error(), "archive previous skill") {
		t.Fatalf("expected archive step error, got %v", err)
	}

	if cur := loadSkillByID(t, store, "sk1"); cur == nil || cur.Body != "b1" {
		t.Fatalf("current write must roll back with the tx, got %+v", cur)
	}
	if vers := loadVersionsFor(t, store, "sk1"); len(vers) != 0 {
		t.Fatalf("no version may survive the rolled-back tx, got %+v", vers)
	}

	// The store stays usable after the injected failure is removed.
	if _, err := store.db.Exec(`DROP TRIGGER fail_skill_archive`); err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	if _, _, err := store.UpdateWithHistory(&next, base, 20); err != nil {
		t.Fatalf("UpdateWithHistory after dropping trigger: %v", err)
	}
	if cur := loadSkillByID(t, store, "sk1"); cur == nil || cur.Body != "should-not-persist" {
		t.Fatalf("expected committed update after trigger removal, got %+v", cur)
	}
	if vers := loadVersionsFor(t, store, "sk1"); len(vers) != 1 || vers[0].Skill.Body != "b1" {
		t.Fatalf("expected exactly one archived previous, got %+v", vers)
	}
}

// Invalid inputs fail before any write; keep=0 archives then prunes everything.
func TestUpdateWithHistoryValidationAndKeepZero(t *testing.T) {
	store := openTxTestStore(t)
	base := &Skill{ID: "sk1", Name: "One", Version: "1", Body: "b1", Source: SourceUser, Enabled: true}
	if err := store.put(base); err != nil {
		t.Fatal(err)
	}
	next := *base
	next.Body = "b2"
	other := *base
	other.ID = "sk2"

	cases := []struct {
		name           string
		current, prior *Skill
		keep           int
	}{
		{name: "nil current", current: nil, prior: base, keep: 20},
		{name: "nil previous", current: &next, prior: nil, keep: 20},
		{name: "mismatched ids", current: &next, prior: &other, keep: 20},
		{name: "negative keep", current: &next, prior: base, keep: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := store.UpdateWithHistory(tc.current, tc.prior, tc.keep); err == nil {
				t.Fatal("expected validation error")
			}
			if cur := loadSkillByID(t, store, "sk1"); cur == nil || cur.Body != "b1" {
				t.Fatalf("validation failure must not write, got %+v", cur)
			}
			if vers := loadVersionsFor(t, store, "sk1"); len(vers) != 0 {
				t.Fatalf("validation failure must not archive, got %+v", vers)
			}
		})
	}

	if _, _, err := store.UpdateWithHistory(&next, base, 0); err != nil {
		t.Fatalf("UpdateWithHistory keep=0: %v", err)
	}
	if cur := loadSkillByID(t, store, "sk1"); cur == nil || cur.Body != "b2" {
		t.Fatalf("current write missing after keep=0, got %+v", cur)
	}
	if vers := loadVersionsFor(t, store, "sk1"); len(vers) != 0 {
		t.Fatalf("keep=0 must retain nothing, got %+v", vers)
	}
}
