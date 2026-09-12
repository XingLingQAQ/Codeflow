package handlers

// HTTP end-to-end coverage for the transactional skill update path (E-03) and
// the empty-matching-rules rollback contract (E-08), per §28 T13.03.c:
// durable faults are injected as real SQLite triggers in the registry file,
// and assertions go through the actual PATCH/GET/versions/rollback/match
// handlers — including trigger matching, stage tags, the Enabled contract and
// close/reopen cycles, not just response bodies.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/dbx"
	"github.com/codeflow/backend/internal/skill"
)

// setupSkillHTTP installs a fresh SQLite-backed registry as the process-wide
// registry and returns a router wiring the real skill handlers.
func setupSkillHTTP(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dbPath := filepath.Join(t.TempDir(), "skills_http.db")
	reg, err := skill.NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Fatalf("open sqlite registry: %v", err)
	}
	prev := skill.GetRegistry()
	skill.SetRegistry(reg)
	t.Cleanup(func() {
		if cur, ok := skill.GetRegistry().(*skill.InMemoryRegistry); ok {
			_ = cur.Close()
		}
		skill.SetRegistry(prev)
	})

	r := gin.New()
	r.POST("/api/v1/skills", CreateSkill)
	r.GET("/api/v1/skills/:id", GetSkill)
	r.PATCH("/api/v1/skills/:id", UpdateSkill)
	r.GET("/api/v1/skills/:id/versions", ListSkillVersions)
	r.POST("/api/v1/skills/:id/rollback", RollbackSkillVersion)
	r.POST("/api/v1/skills/match", MatchSkills)
	return r, dbPath
}

// reopenSkillRegistry closes the live registry and reopens it from the same
// file, proving the served state is the persisted state.
func reopenSkillRegistry(t *testing.T, dbPath string) {
	t.Helper()
	if cur, ok := skill.GetRegistry().(*skill.InMemoryRegistry); ok {
		if err := cur.Close(); err != nil {
			t.Fatalf("close registry: %v", err)
		}
	}
	reg, err := skill.NewSQLiteRegistry(dbPath)
	if err != nil {
		t.Fatalf("reopen registry: %v", err)
	}
	skill.SetRegistry(reg)
}

// withSkillRawDB closes the live registry, runs fn against a raw connection to
// the same database file (trigger install/removal), then reopens the registry.
// The fault point therefore lives in the durable file the reopened registry
// serves, not in test-only memory.
func withSkillRawDB(t *testing.T, dbPath string, fn func(db *sql.DB)) {
	t.Helper()
	if cur, ok := skill.GetRegistry().(*skill.InMemoryRegistry); ok {
		if err := cur.Close(); err != nil {
			t.Fatalf("close registry: %v", err)
		}
	}
	raw, err := dbx.Open(dbPath)
	if err != nil {
		t.Fatalf("open raw skill db: %v", err)
	}
	fn(raw)
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw skill db: %v", err)
	}
	reopenSkillRegistry(t, dbPath)
}

type skillVersionItem struct {
	RowID int64       `json:"row_id"`
	Skill skill.Skill `json:"skill"`
}

type skillMatchItem struct {
	Skill skill.Skill `json:"skill"`
	Score float64     `json:"score"`
	Hits  []string    `json:"hits"`
}

func skillCall(t *testing.T, r http.Handler, method, target string, body interface{}, wantStatus int) rexEnvelope {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = rexMustJSON(t, body)
	}
	w := rexRequest(t, r, method, target, raw, nil)
	return rexData(t, w, wantStatus, wantStatus < 400, nil)
}

func skillCreate(t *testing.T, r http.Handler, req map[string]interface{}) skill.Skill {
	t.Helper()
	env := skillCall(t, r, http.MethodPost, "/api/v1/skills", req, http.StatusCreated)
	var s skill.Skill
	if err := json.Unmarshal(env.Data, &s); err != nil {
		t.Fatalf("decode created skill: %v", err)
	}
	return s
}

func skillGetHTTP(t *testing.T, r http.Handler, id string) skill.Skill {
	t.Helper()
	env := skillCall(t, r, http.MethodGet, "/api/v1/skills/"+id, nil, http.StatusOK)
	var s skill.Skill
	if err := json.Unmarshal(env.Data, &s); err != nil {
		t.Fatalf("decode skill: %v", err)
	}
	return s
}

func skillVersionsHTTP(t *testing.T, r http.Handler, id string) []skillVersionItem {
	t.Helper()
	env := skillCall(t, r, http.MethodGet, "/api/v1/skills/"+id+"/versions", nil, http.StatusOK)
	var out struct {
		Items []skillVersionItem `json:"items"`
		Total int                `json:"total"`
	}
	if err := json.Unmarshal(env.Data, &out); err != nil {
		t.Fatalf("decode versions: %v", err)
	}
	if out.Total != len(out.Items) {
		t.Fatalf("versions total=%d but items=%d", out.Total, len(out.Items))
	}
	return out.Items
}

// skillVersionRowByBody finds the archived snapshot carrying the given body.
func skillVersionRowByBody(t *testing.T, r http.Handler, id, body string) int64 {
	t.Helper()
	for _, v := range skillVersionsHTTP(t, r, id) {
		if v.Skill.Body == body {
			return v.RowID
		}
	}
	t.Fatalf("no archived version with body %q for skill %s", body, id)
	return 0
}

func skillMatchHTTP(t *testing.T, r http.Handler, req map[string]interface{}) []skillMatchItem {
	t.Helper()
	env := skillCall(t, r, http.MethodPost, "/api/v1/skills/match", req, http.StatusOK)
	var out struct {
		Items []skillMatchItem `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(env.Data, &out); err != nil {
		t.Fatalf("decode match: %v", err)
	}
	return out.Items
}

func skillMatchByID(items []skillMatchItem, id string) *skillMatchItem {
	for i := range items {
		if items[i].Skill.ID == id {
			return &items[i]
		}
	}
	return nil
}

// TestSkillUpdateArchivePruneAtomic drives real SQLite fault injection at two
// of the three commit points of UpdateWithHistory (current write, archive)
// through the HTTP PATCH handler: the failed update is a 5xx, and the visible
// skill, the entire version history, trigger matching, stage tags and the
// Enabled flag all stay exactly as they were — in memory and across a reopen
// (E-03). The third commit point (prune at capacity) is covered through HTTP
// by TestSkillHistoryAtCapacityRollbackOnFailure.
func TestSkillUpdateArchivePruneAtomic(t *testing.T) {
	router, dbPath := setupSkillHTTP(t)

	for _, tc := range []struct {
		name    string
		keyword string
		trigger string
		ddl     string
	}{
		{
			name:    "current write failure",
			keyword: "atomic-current-trigger",
			trigger: "fail_skill_current_http",
			ddl: `CREATE TRIGGER fail_skill_current_http
BEFORE INSERT ON skills
BEGIN SELECT RAISE(ABORT, 'injected current write failure'); END;`,
		},
		{
			name:    "archive failure",
			keyword: "atomic-archive-trigger",
			trigger: "fail_skill_archive_http",
			ddl: `CREATE TRIGGER fail_skill_archive_http
BEFORE INSERT ON skill_versions
BEGIN SELECT RAISE(ABORT, 'injected archive failure'); END;`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created := skillCreate(t, router, map[string]interface{}{
				"name":       "AtomicSkill-" + tc.keyword,
				"body":       "atomic-v1",
				"triggers":   []string{tc.keyword},
				"stage_tags": []string{"coding"},
			})
			skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
				map[string]interface{}{"body": "atomic-v2"}, http.StatusOK)

			assertPreserved := func() {
				t.Helper()
				cur := skillGetHTTP(t, router, created.ID)
				if cur.Body != "atomic-v2" || !cur.Enabled {
					t.Fatalf("current = body %q enabled %v, want body atomic-v2 enabled true", cur.Body, cur.Enabled)
				}
				if len(cur.Triggers) != 1 || cur.Triggers[0] != tc.keyword {
					t.Fatalf("triggers = %v, want [%s]", cur.Triggers, tc.keyword)
				}
				if len(cur.StageTags) != 1 || cur.StageTags[0] != "coding" {
					t.Fatalf("stage tags = %v, want [coding]", cur.StageTags)
				}
				versions := skillVersionsHTTP(t, router, created.ID)
				if len(versions) != 1 || versions[0].Skill.Body != "atomic-v1" {
					t.Fatalf("versions = %+v, want exactly one archived atomic-v1", versions)
				}
				// Trigger matching still resolves the preserved rules.
				if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
					"text": "please run the " + tc.keyword + " now",
				}), created.ID); m == nil {
					t.Fatal("match lost the skill after the failed update")
				}
				// Stage tags still scope the skill out of a foreign stage.
				if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
					"text":       "please run the " + tc.keyword + " now",
					"stage_type": "review",
				}), created.ID); m != nil {
					t.Fatal("stage filter leaked a coding-only skill into review")
				}
			}

			withSkillRawDB(t, dbPath, func(db *sql.DB) {
				if _, err := db.Exec(tc.ddl); err != nil {
					t.Fatalf("install trigger: %v", err)
				}
			})

			// The failed update is a server-side persistence failure: 5xx, and
			// no fact changes — not a client 4xx.
			env := skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
				map[string]interface{}{"body": "should-not-persist"}, http.StatusInternalServerError)
			if env.Error == "" {
				t.Fatal("expected an error message in the 5xx envelope")
			}
			assertPreserved()

			// A close/reopen cycle serves exactly the pre-failure state.
			reopenSkillRegistry(t, dbPath)
			assertPreserved()

			// Clearing the fault lets the same update commit through the same path.
			withSkillRawDB(t, dbPath, func(db *sql.DB) {
				if _, err := db.Exec("DROP TRIGGER IF EXISTS " + tc.trigger); err != nil {
					t.Fatalf("drop trigger: %v", err)
				}
			})
			skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
				map[string]interface{}{"body": "should-not-persist"}, http.StatusOK)
			cur := skillGetHTTP(t, router, created.ID)
			if cur.Body != "should-not-persist" {
				t.Fatalf("post-recovery body = %q", cur.Body)
			}
			if versions := skillVersionsHTTP(t, router, created.ID); len(versions) != 2 {
				t.Fatalf("post-recovery versions = %d, want 2", len(versions))
			}
		})
	}
}

// TestSkillRollbackRestoresEmptyMatchingRules covers the E-08 contract through
// the HTTP rollback and match handlers: an empty rule set and a non-empty rule
// set are distinct archived states, rollback restores each exactly (nil does
// not leak "keep current" semantics into an archived empty set), and rollback
// keeps the current Enabled value instead of silently re-enabling the skill.
func TestSkillRollbackRestoresEmptyMatchingRules(t *testing.T) {
	router, dbPath := setupSkillHTTP(t)

	t.Run("empty to non-empty and back", func(t *testing.T) {
		created := skillCreate(t, router, map[string]interface{}{
			"name": "CaseA",
			"body": "a-v1-empty",
		})
		skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID, map[string]interface{}{
			"body":       "a-v2",
			"triggers":   []string{"case-a-trigger"},
			"stage_tags": []string{"coding"},
		}, http.StatusOK)
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text": "case-a-trigger",
		}), created.ID); m == nil {
			t.Fatal("non-empty rules did not match before rollback")
		}

		// Roll back to the archived empty-rules version.
		rowID := skillVersionRowByBody(t, router, created.ID, "a-v1-empty")
		skillCall(t, router, http.MethodPost, "/api/v1/skills/"+created.ID+"/rollback",
			map[string]interface{}{"version_row_id": rowID}, http.StatusOK)
		cur := skillGetHTTP(t, router, created.ID)
		if cur.Body != "a-v1-empty" || len(cur.Triggers) != 0 || len(cur.StageTags) != 0 {
			t.Fatalf("rollback to empty rules = body %q triggers %v stages %v", cur.Body, cur.Triggers, cur.StageTags)
		}
		if !cur.Enabled {
			t.Fatal("rollback unexpectedly changed Enabled")
		}
		// The restored empty trigger set no longer matches the old keyword,
		// and the restored empty stage set applies to every stage again.
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text": "case-a-trigger",
		}), created.ID); m != nil {
			t.Fatal("empty triggers still matched after rollback")
		}
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"stage_type": "review",
		}), created.ID); m == nil {
			t.Fatal("empty stage tags should apply to any stage")
		}

		// Enabled contract: disable, then roll back to the non-empty version;
		// the rollback restores content and rules but keeps Enabled=false.
		skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
			map[string]interface{}{"enabled": false}, http.StatusOK)
		rowID = skillVersionRowByBody(t, router, created.ID, "a-v2")
		skillCall(t, router, http.MethodPost, "/api/v1/skills/"+created.ID+"/rollback",
			map[string]interface{}{"version_row_id": rowID}, http.StatusOK)
		cur = skillGetHTTP(t, router, created.ID)
		if cur.Body != "a-v2" || len(cur.Triggers) != 1 || cur.Triggers[0] != "case-a-trigger" ||
			len(cur.StageTags) != 1 || cur.StageTags[0] != "coding" {
			t.Fatalf("rollback to non-empty rules = body %q triggers %v stages %v", cur.Body, cur.Triggers, cur.StageTags)
		}
		if cur.Enabled {
			t.Fatal("rollback silently re-enabled a disabled skill")
		}
		// The match path proves the disabled state: restored triggers cannot hit.
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text": "case-a-trigger",
		}), created.ID); m != nil {
			t.Fatal("disabled skill matched after rollback")
		}

		// Re-enable, then close/reopen: the rollback result is durable.
		skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
			map[string]interface{}{"enabled": true}, http.StatusOK)
		reopenSkillRegistry(t, dbPath)
		cur = skillGetHTTP(t, router, created.ID)
		if cur.Body != "a-v2" || !cur.Enabled || len(cur.Triggers) != 1 || len(cur.StageTags) != 1 {
			t.Fatalf("reopened state = %+v", cur)
		}
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text": "case-a-trigger",
		}), created.ID); m == nil {
			t.Fatal("match lost the restored rules after reopen")
		}
	})

	t.Run("non-empty to empty and back", func(t *testing.T) {
		created := skillCreate(t, router, map[string]interface{}{
			"name":       "CaseB",
			"body":       "b-v1",
			"triggers":   []string{"case-b-trigger"},
			"stage_tags": []string{"submit"},
		})
		// Clearing the rules is an explicit empty set, not an omitted field.
		skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID, map[string]interface{}{
			"body":       "b-v2-empty",
			"triggers":   []string{},
			"stage_tags": []string{},
		}, http.StatusOK)
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text": "case-b-trigger",
		}), created.ID); m != nil {
			t.Fatal("cleared triggers still matched")
		}

		rowID := skillVersionRowByBody(t, router, created.ID, "b-v1")
		skillCall(t, router, http.MethodPost, "/api/v1/skills/"+created.ID+"/rollback",
			map[string]interface{}{"version_row_id": rowID}, http.StatusOK)
		cur := skillGetHTTP(t, router, created.ID)
		if cur.Body != "b-v1" || len(cur.Triggers) != 1 || cur.Triggers[0] != "case-b-trigger" ||
			len(cur.StageTags) != 1 || cur.StageTags[0] != "submit" || !cur.Enabled {
			t.Fatalf("rollback to non-empty rules = %+v", cur)
		}
		m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text":       "case-b-trigger",
			"stage_type": "submit",
		}), created.ID)
		if m == nil {
			t.Fatal("restored rules did not match in their stage")
		}
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text":       "case-b-trigger",
			"stage_type": "review",
		}), created.ID); m != nil {
			t.Fatal("restored stage tag leaked into a foreign stage")
		}

		reopenSkillRegistry(t, dbPath)
		cur = skillGetHTTP(t, router, created.ID)
		if cur.Body != "b-v1" || len(cur.Triggers) != 1 || len(cur.StageTags) != 1 || !cur.Enabled {
			t.Fatalf("reopened state = %+v", cur)
		}
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text": "case-b-trigger",
		}), created.ID); m == nil {
			t.Fatal("match lost the restored rules after reopen")
		}
	})
}

// TestSkillHistoryAtCapacityRollbackOnFailure fills the archive to its
// 20-version cap, then injects a real SQLite fault at the prune commit point:
// the HTTP update fails 5xx and all 20 archived snapshots plus the current
// skill survive, in memory and across a reopen. Once the fault is cleared the
// same update commits, evicting exactly the oldest snapshot, and a rollback
// from the retained history restores content while keeping the current
// Enabled value (E-03 retention guarantee).
func TestSkillHistoryAtCapacityRollbackOnFailure(t *testing.T) {
	router, dbPath := setupSkillHTTP(t)

	created := skillCreate(t, router, map[string]interface{}{
		"name":       "CapacitySkill",
		"body":       "cap-body-0",
		"triggers":   []string{"capacity-trigger"},
		"stage_tags": []string{"coding"},
	})
	for i := 1; i <= 21; i++ {
		skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
			map[string]interface{}{"body": fmt.Sprintf("cap-body-%d", i)}, http.StatusOK)
	}
	versions := skillVersionsHTTP(t, router, created.ID)
	if len(versions) != 20 {
		t.Fatalf("seed history = %d versions, want 20", len(versions))
	}
	// Newest-first list; 21 archives pruned to 20 evicted the create snapshot.
	if versions[len(versions)-1].Skill.Body != "cap-body-1" {
		t.Fatalf("oldest retained body = %q, want cap-body-1", versions[len(versions)-1].Skill.Body)
	}

	// Prune fault point: the retention DELETE aborts mid-transaction.
	withSkillRawDB(t, dbPath, func(db *sql.DB) {
		if _, err := db.Exec(`
CREATE TRIGGER fail_skill_prune_http
BEFORE DELETE ON skill_versions
BEGIN SELECT RAISE(ABORT, 'injected prune failure'); END;`); err != nil {
			t.Fatalf("install prune trigger: %v", err)
		}
	})

	skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
		map[string]interface{}{"body": "should-not-persist"}, http.StatusInternalServerError)
	assertAtCapacity := func(wantBody string) {
		t.Helper()
		cur := skillGetHTTP(t, router, created.ID)
		if cur.Body != wantBody || !cur.Enabled {
			t.Fatalf("current = body %q enabled %v, want body %q enabled true", cur.Body, cur.Enabled, wantBody)
		}
		versions := skillVersionsHTTP(t, router, created.ID)
		if len(versions) != 20 || versions[len(versions)-1].Skill.Body != "cap-body-1" {
			t.Fatalf("history after failed update = %d versions (oldest %q), want 20 with oldest cap-body-1",
				len(versions), versions[len(versions)-1].Skill.Body)
		}
		if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
			"text": "capacity-trigger",
		}), created.ID); m == nil {
			t.Fatal("match lost the skill after the failed update")
		}
	}
	assertAtCapacity("cap-body-21")
	reopenSkillRegistry(t, dbPath)
	assertAtCapacity("cap-body-21")

	// Clear the fault: the same update commits and evicts exactly the oldest
	// snapshot, keeping the archive at capacity.
	withSkillRawDB(t, dbPath, func(db *sql.DB) {
		if _, err := db.Exec("DROP TRIGGER IF EXISTS fail_skill_prune_http"); err != nil {
			t.Fatalf("drop prune trigger: %v", err)
		}
	})
	skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
		map[string]interface{}{"body": "cap-body-22"}, http.StatusOK)
	versions = skillVersionsHTTP(t, router, created.ID)
	if len(versions) != 20 {
		t.Fatalf("post-recovery history = %d, want 20", len(versions))
	}
	if versions[len(versions)-1].Skill.Body != "cap-body-2" {
		t.Fatalf("oldest retained after recovery = %q, want cap-body-2 (cap-body-1 evicted)",
			versions[len(versions)-1].Skill.Body)
	}

	// Rollback from the retained history works at capacity, restoring content
	// and matching rules while keeping the current Enabled value. The
	// enabled=false PATCH itself archives and prunes, so the rollback target
	// is the snapshot the PATCH just archived.
	skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
		map[string]interface{}{"enabled": false}, http.StatusOK)
	versions = skillVersionsHTTP(t, router, created.ID)
	if len(versions) != 20 || versions[len(versions)-1].Skill.Body != "cap-body-3" {
		t.Fatalf("history after disable = %d versions (oldest %q), want 20 with oldest cap-body-3",
			len(versions), versions[len(versions)-1].Skill.Body)
	}
	rowID := skillVersionRowByBody(t, router, created.ID, "cap-body-22")
	skillCall(t, router, http.MethodPost, "/api/v1/skills/"+created.ID+"/rollback",
		map[string]interface{}{"version_row_id": rowID}, http.StatusOK)
	cur := skillGetHTTP(t, router, created.ID)
	if cur.Body != "cap-body-22" || cur.Enabled {
		t.Fatalf("rollback at capacity = body %q enabled %v, want cap-body-22 enabled false", cur.Body, cur.Enabled)
	}
	if len(cur.Triggers) != 1 || cur.Triggers[0] != "capacity-trigger" ||
		len(cur.StageTags) != 1 || cur.StageTags[0] != "coding" {
		t.Fatalf("rollback at capacity lost rules: triggers %v stages %v", cur.Triggers, cur.StageTags)
	}
	if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
		"text": "capacity-trigger",
	}), created.ID); m != nil {
		t.Fatal("disabled skill matched after rollback")
	}
	if versions := skillVersionsHTTP(t, router, created.ID); len(versions) != 20 {
		t.Fatalf("history after rollback = %d, want 20", len(versions))
	}

	// Re-enable and prove durability across a final close/reopen.
	skillCall(t, router, http.MethodPatch, "/api/v1/skills/"+created.ID,
		map[string]interface{}{"enabled": true}, http.StatusOK)
	reopenSkillRegistry(t, dbPath)
	cur = skillGetHTTP(t, router, created.ID)
	if cur.Body != "cap-body-22" || !cur.Enabled {
		t.Fatalf("reopened current = body %q enabled %v", cur.Body, cur.Enabled)
	}
	if versions := skillVersionsHTTP(t, router, created.ID); len(versions) != 20 {
		t.Fatalf("reopened history = %d, want 20", len(versions))
	}
	if m := skillMatchByID(skillMatchHTTP(t, router, map[string]interface{}{
		"text": "capacity-trigger",
	}), created.ID); m == nil {
		t.Fatal("match lost the skill after reopen")
	}
}
