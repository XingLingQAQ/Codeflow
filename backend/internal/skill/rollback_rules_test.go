package skill

import (
	"context"
	"path/filepath"
	"testing"
)

func matchIncludes(results []MatchResult, skillID string) bool {
	for _, m := range results {
		if m.Skill.ID == skillID {
			return true
		}
	}
	return false
}

// Empty -> non-empty -> rollback restores the empty rule set, and Match
// follows the restored rules (E-08).
func TestRollbackRestoresEmptyMatchingRules(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	sk, err := r.Create(ctx, &CreateRequest{Name: "EmptyRules", Body: "v0", Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}

	// Initially there are no trigger rules, so the text cannot match.
	if got, err := r.Match(ctx, &MatchRequest{Text: "deploy now"}); err != nil || matchIncludes(got, sk.ID) {
		t.Fatalf("initial match=%+v err=%v: skill without triggers must not match", got, err)
	}

	body := "v1"
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{
		Body:      &body,
		Triggers:  []string{"deploy"},
		StageTags: []string{"coding"},
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Match(ctx, &MatchRequest{Text: "deploy now"}); err != nil || !matchIncludes(got, sk.ID) {
		t.Fatalf("match after update=%+v err=%v: trigger must hit", got, err)
	}
	// The new stage restriction excludes a non-matching stage.
	if got, err := r.Match(ctx, &MatchRequest{Text: "deploy now", StageType: "submit"}); err != nil || matchIncludes(got, sk.ID) {
		t.Fatalf("stage-filtered match=%+v err=%v: stage restriction must exclude", got, err)
	}

	vers, err := r.ListVersions(ctx, sk.ID)
	if err != nil || len(vers) != 1 {
		t.Fatalf("versions=%+v err=%v", vers, err)
	}
	restored, err := r.RollbackVersion(ctx, sk.ID, vers[0].RowID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Body != "v0" {
		t.Fatalf("rollback body=%q want v0", restored.Body)
	}
	if len(restored.Triggers) != 0 || len(restored.StageTags) != 0 {
		t.Fatalf("rollback must restore empty rules, got triggers=%v stageTags=%v", restored.Triggers, restored.StageTags)
	}

	// Match follows the restored empty rules: the text no longer hits and the
	// stage restriction is gone again.
	if got, err := r.Match(ctx, &MatchRequest{Text: "deploy now"}); err != nil || matchIncludes(got, sk.ID) {
		t.Fatalf("match after rollback=%+v err=%v: restored empty triggers must not hit", got, err)
	}
	if got, err := r.Match(ctx, &MatchRequest{StageType: "submit"}); err != nil || !matchIncludes(got, sk.ID) {
		t.Fatalf("stage match after rollback=%+v err=%v: empty stage tags apply to any stage", got, err)
	}
}

// Non-empty -> empty (explicit clear via PATCH) -> rollback restores the
// non-empty rule set.
func TestRollbackRestoresNonEmptyMatchingRules(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	sk, err := r.Create(ctx, &CreateRequest{
		Name: "NonEmpty", Body: "v0", Version: "0.1.0",
		Triggers: []string{"deploy"}, StageTags: []string{"coding"},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := "v1"
	empty := []string{}
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &body, Triggers: empty, StageTags: empty}); err != nil {
		t.Fatal(err)
	}
	// An explicit empty set clears the rules (distinct from a nil "leave unchanged").
	if got, err := r.Match(ctx, &MatchRequest{Text: "deploy now"}); err != nil || matchIncludes(got, sk.ID) {
		t.Fatalf("match after explicit clear=%+v err=%v: cleared triggers must not hit", got, err)
	}

	vers, err := r.ListVersions(ctx, sk.ID)
	if err != nil || len(vers) != 1 {
		t.Fatalf("versions=%+v err=%v", vers, err)
	}
	restored, err := r.RollbackVersion(ctx, sk.ID, vers[0].RowID)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Triggers) != 1 || restored.Triggers[0] != "deploy" {
		t.Fatalf("rollback must restore non-empty triggers, got %v", restored.Triggers)
	}
	if len(restored.StageTags) != 1 || restored.StageTags[0] != "coding" {
		t.Fatalf("rollback must restore non-empty stage tags, got %v", restored.StageTags)
	}
	if got, err := r.Match(ctx, &MatchRequest{Text: "deploy now", StageType: "coding"}); err != nil || !matchIncludes(got, sk.ID) {
		t.Fatalf("match after rollback=%+v err=%v: restored trigger must hit", got, err)
	}
}

// Rollback restores content and matching rules but keeps the current Enabled
// state; it never silently re-enables a disabled skill.
func TestRollbackKeepsCurrentEnabled(t *testing.T) {
	r := NewInMemoryRegistry()
	ctx := context.Background()
	sk, err := r.Create(ctx, &CreateRequest{Name: "Toggle", Body: "v0", Version: "0.1.0", Triggers: []string{"t"}})
	if err != nil {
		t.Fatal(err)
	}
	body := "v1"
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &body}); err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}

	vers, err := r.ListVersions(ctx, sk.ID)
	if err != nil || len(vers) != 2 {
		t.Fatalf("versions=%+v err=%v", vers, err)
	}
	// vers[1] is the oldest snapshot: the original enabled v0 body.
	restored, err := r.RollbackVersion(ctx, sk.ID, vers[1].RowID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Body != "v0" {
		t.Fatalf("rollback body=%q want v0", restored.Body)
	}
	if restored.Enabled {
		t.Fatal("rollback must keep the current disabled state")
	}
	cur, err := r.Get(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Enabled {
		t.Fatal("current skill must remain disabled after rollback")
	}
	// The pre-rollback state archived by the rollback keeps its own Enabled
	// value, so history does not pretend the disabled state was something else.
	after, err := r.ListVersions(ctx, sk.ID)
	if err != nil || len(after) != 3 {
		t.Fatalf("versions after rollback=%+v err=%v", after, err)
	}
	if after[0].Skill.Body != "v1" || after[0].Skill.Enabled {
		t.Fatalf("newest archive=%+v: pre-rollback state must be archived as-is", after[0].Skill)
	}
}

// The empty-rule rollback contract also holds through the durable store.
func TestRollbackRestoresEmptyRulesWithSQLiteStore(t *testing.T) {
	ctx := context.Background()
	r, err := NewSQLiteRegistry(filepath.Join(t.TempDir(), "skills_rollback.db"))
	if err != nil {
		t.Fatalf("open sqlite registry: %v", err)
	}
	defer r.Close()

	sk, err := r.Create(ctx, &CreateRequest{Name: "DurableEmpty", Body: "v0"})
	if err != nil {
		t.Fatal(err)
	}
	body := "v1"
	if _, err := r.Update(ctx, sk.ID, &UpdateRequest{Body: &body, Triggers: []string{"deploy"}}); err != nil {
		t.Fatal(err)
	}
	vers, err := r.ListVersions(ctx, sk.ID)
	if err != nil || len(vers) != 1 {
		t.Fatalf("versions=%+v err=%v", vers, err)
	}
	if _, err := r.RollbackVersion(ctx, sk.ID, vers[0].RowID); err != nil {
		t.Fatal(err)
	}
	cur, err := r.Get(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Body != "v0" || len(cur.Triggers) != 0 {
		t.Fatalf("after rollback body=%q triggers=%v, want v0 with no triggers", cur.Body, cur.Triggers)
	}
	if got, err := r.Match(ctx, &MatchRequest{Text: "deploy now"}); err != nil || matchIncludes(got, sk.ID) {
		t.Fatalf("match after rollback=%+v err=%v: restored empty triggers must not hit", got, err)
	}
}
