package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// T1.03.b wiring tests: Update appends immutable revisions inside one
// transaction, the seed path writes revision 1, Delete retains revision
// history, and disabled assets are gated out of new runs while their history
// stays readable. All entry points are the real registry methods.

func openWiringRegistry(t *testing.T, dbPath string) *InMemoryAgentRegistry {
	t.Helper()
	reg, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	return reg
}

func mustJSON(t *testing.T, a *AgentAsset) string {
	t.Helper()
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal %s: %v", a.ID, err)
	}
	return string(b)
}

func revisionsOf(t *testing.T, reg *InMemoryAgentRegistry, agentID string) []revisionRow {
	t.Helper()
	out := make([]revisionRow, 0)
	for _, r := range dumpRevisionRows(t, reg.store.db) {
		if r.agentID == agentID {
			out = append(out, r)
		}
	}
	return out
}

// Fresh databases seed builtins with revision 1 tagged "seed", closing the
// "builtin has no revision row" gap; reopening neither reseeds nor
// duplicates revisions.
func TestSeedWritesFirstRevision(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_seed_rev.db")
	reg := openWiringRegistry(t, dbPath)

	list, err := reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 5 {
		t.Fatalf("seeded agents=%d want 5", len(list))
	}
	payloadByID := make(map[string]string)
	for _, row := range dumpAgentsRows(t, reg.store.db) {
		payloadByID[row.id] = row.payload
	}
	for _, a := range list {
		head, err := reg.store.headRevision(a.ID)
		if err != nil || head != 1 {
			t.Fatalf("seed head of %s=%d err=%v want 1", a.ID, head, err)
		}
		revs := revisionsOf(t, reg, a.ID)
		if len(revs) != 1 {
			t.Fatalf("seed revisions of %s=%d want 1", a.ID, len(revs))
		}
		if revs[0].revision != 1 || revs[0].source != revisionSourceSeed {
			t.Fatalf("seed revision of %s = rev%d source %q, want rev1 %q",
				a.ID, revs[0].revision, revs[0].source, revisionSourceSeed)
		}
		// The frozen snapshot is byte-identical to the persisted document.
		if revs[0].frozen != payloadByID[a.ID] {
			t.Fatalf("seed frozen of %s differs from agents payload", a.ID)
		}
		frozen := mustUnmarshalAsset(t, revs[0].frozen)
		if frozen.ID != a.ID || frozen.Name != a.Name || !frozen.Enabled {
			t.Fatalf("seed frozen of %s = %+v", a.ID, frozen)
		}
	}
	beforeRevs := dumpRevisionRows(t, reg.store.db)
	beforeHeads := dumpHeadRows(t, reg.store.db)
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	reg2 := openWiringRegistry(t, dbPath)
	defer reg2.Close()
	if got := dumpRevisionRows(t, reg2.store.db); !reflect.DeepEqual(beforeRevs, got) {
		t.Fatalf("reopen changed revisions:\nbefore=%+v\nafter=%+v", beforeRevs, got)
	}
	if got := dumpHeadRows(t, reg2.store.db); !reflect.DeepEqual(beforeHeads, got) {
		t.Fatalf("reopen changed heads:\nbefore=%+v\nafter=%+v", beforeHeads, got)
	}
	for _, a := range list {
		if head, err := reg2.store.headRevision(a.ID); err != nil || head != 1 {
			t.Fatalf("seed head of %s after reopen=%d err=%v want 1", a.ID, head, err)
		}
	}
}

// Create writes revision 1; two Updates build a rev1/rev2/rev3 chain with
// head=3, each revision frozen at its own point in time, prompt/binding/
// mounts resolved into the snapshot, and the live row carrying the latest.
func TestUpdateAppendsRevisionChain(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_update_chain.db")
	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{
		Name: "Chain", RoleBase: RoleBaseCoder,
		SystemPrompt: "prompt v1", Version: "0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 1 {
		t.Fatalf("head after create=%d err=%v want 1", head, err)
	}

	name2, prompt2, ver2 := "Chain Renamed", "prompt v2", "0.2.0"
	updated2, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{
		Name: &name2, SystemPrompt: &prompt2, Version: &ver2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 2 {
		t.Fatalf("head after first update=%d err=%v want 2", head, err)
	}

	temp := 0.5
	updated3, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{
		Binding: &Binding{Model: "gpt-5", Channel: "default", Temperature: &temp},
		Mounts:  &Mounts{MCPTools: []string{"fs.read"}, Skills: []string{"review"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 3 {
		t.Fatalf("head after second update=%d err=%v want 3", head, err)
	}

	revs := revisionsOf(t, reg, created.ID)
	if len(revs) != 3 {
		t.Fatalf("revisions=%d want 3", len(revs))
	}
	wantFrozen := []string{mustJSON(t, created), mustJSON(t, updated2), mustJSON(t, updated3)}
	for i, want := range wantFrozen {
		rev := revs[i]
		if rev.revision != int64(i+1) {
			t.Fatalf("revision number=%d want %d", rev.revision, i+1)
		}
		if rev.source != revisionSourceUpdate {
			t.Fatalf("revision %d source=%q want %q", rev.revision, rev.source, revisionSourceUpdate)
		}
		if rev.frozen != want {
			t.Fatalf("revision %d frozen drifted:\nwant=%s\ngot=%s", rev.revision, want, rev.frozen)
		}
	}

	frozen3, err := reg.store.revisionAsset(created.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if frozen3.SystemPrompt != "prompt v2" || frozen3.Binding.Model != "gpt-5" ||
		len(frozen3.Mounts.MCPTools) != 1 || frozen3.Mounts.MCPTools[0] != "fs.read" ||
		len(frozen3.Mounts.Skills) != 1 || frozen3.Mounts.Skills[0] != "review" {
		t.Fatalf("revision 3 frozen=%+v", frozen3)
	}
	// Older revisions are not polluted by later edits.
	frozen1, err := reg.store.revisionAsset(created.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if frozen1.Name != "Chain" || frozen1.SystemPrompt != "prompt v1" || frozen1.Binding.Model != "" {
		t.Fatalf("revision 1 mutated: %+v", frozen1)
	}
	frozen2, err := reg.store.revisionAsset(created.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if frozen2.Name != "Chain Renamed" || frozen2.Version != "0.2.0" || frozen2.Binding.Model != "" {
		t.Fatalf("revision 2 mutated: %+v", frozen2)
	}

	payload := ""
	for _, row := range dumpAgentsRows(t, reg.store.db) {
		if row.id == created.ID {
			payload = row.payload
		}
	}
	if payload == "" {
		t.Fatal("live agents row missing")
	}
	live := mustUnmarshalAsset(t, payload)
	if live.Name != "Chain Renamed" || live.Version != "0.2.0" || live.Binding.Model != "gpt-5" {
		t.Fatalf("live row=%+v", live)
	}
}

// Fault injection: the revision INSERT inside Update's transaction fails on
// a primary-key conflict, so the whole transaction rolls back — agents row,
// head pointer and in-memory state all keep their pre-update facts.
func TestUpdateRevisionRollbackLeavesNoFacts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_update_rollback.db")
	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{Name: "Victim", RoleBase: RoleBaseSub})
	if err != nil {
		t.Fatal(err)
	}
	beforeAgents := dumpAgentsRows(t, reg.store.db)
	beforeRevs := dumpRevisionRows(t, reg.store.db)
	beforeHeads := dumpHeadRows(t, reg.store.db)

	// Occupy the next revision number so the append fails mid-transaction.
	if _, err := reg.store.db.Exec(
		`INSERT INTO agent_revisions (agent_id, revision, frozen_config, source, created_at)
VALUES (?, 2, '{}', 'fault_injection', 0)`, created.ID,
	); err != nil {
		t.Fatal(err)
	}

	name := "Must Not Persist"
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{Name: &name}); err == nil {
		t.Fatal("expected update to fail when the revision append conflicts")
	}

	if got := dumpAgentsRows(t, reg.store.db); !reflect.DeepEqual(beforeAgents, got) {
		t.Fatalf("agents row changed despite rollback:\nbefore=%+v\nafter=%+v", beforeAgents, got)
	}
	if got := dumpHeadRows(t, reg.store.db); !reflect.DeepEqual(beforeHeads, got) {
		t.Fatalf("head changed despite rollback:\nbefore=%+v\nafter=%+v", beforeHeads, got)
	}
	afterRevs := dumpRevisionRows(t, reg.store.db)
	if len(afterRevs) != len(beforeRevs)+1 {
		t.Fatalf("revisions=%d want %d (baseline + injected row)", len(afterRevs), len(beforeRevs)+1)
	}
	for _, r := range afterRevs {
		if r.revision == 2 && r.agentID == created.ID && r.source != "fault_injection" {
			t.Fatalf("unexpected revision row written by failed update: %+v", r)
		}
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 1 {
		t.Fatalf("head after rollback=%d err=%v want 1", head, err)
	}
	got, err := reg.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != created.Name {
		t.Fatalf("in-memory name=%q want %q (restored)", got.Name, created.Name)
	}
}

// Delete removes the agents row and the head pointer but retains every
// revision row byte-identical, and the history of a deleted asset stays
// queryable across restarts.
func TestDeleteRetainsRevisionHistory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_delete_retains.db")
	reg := openWiringRegistry(t, dbPath)
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{Name: "Doomed", RoleBase: RoleBaseCritic})
	if err != nil {
		t.Fatal(err)
	}
	name := "Doomed Edited"
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 2 {
		t.Fatalf("head before delete=%d err=%v want 2", head, err)
	}
	revsBefore := revisionsOf(t, reg, created.ID)
	if len(revsBefore) != 2 {
		t.Fatalf("revisions before delete=%d want 2", len(revsBefore))
	}

	if err := reg.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := reg.Get(ctx, created.ID); !errors.Is(err, ErrAgentAssetNotFound) {
		t.Fatalf("get after delete err=%v want ErrAgentAssetNotFound", err)
	}
	for _, row := range dumpAgentsRows(t, reg.store.db) {
		if row.id == created.ID {
			t.Fatal("agents row survived delete")
		}
	}
	for _, row := range dumpHeadRows(t, reg.store.db) {
		if row.agentID == created.ID {
			t.Fatal("head row survived delete")
		}
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 0 {
		t.Fatalf("head after delete=%d err=%v want 0 (head removed)", head, err)
	}

	if got := revisionsOf(t, reg, created.ID); !reflect.DeepEqual(revsBefore, got) {
		t.Fatalf("revisions changed by delete:\nbefore=%+v\nafter=%+v", revsBefore, got)
	}
	frozen1, err := reg.store.revisionAsset(created.ID, 1)
	if err != nil {
		t.Fatalf("revision 1 of deleted asset: %v", err)
	}
	if frozen1.Name != "Doomed" {
		t.Fatalf("revision 1 frozen=%+v", frozen1)
	}
	frozen2, err := reg.store.revisionAsset(created.ID, 2)
	if err != nil {
		t.Fatalf("revision 2 of deleted asset: %v", err)
	}
	if frozen2.Name != "Doomed Edited" {
		t.Fatalf("revision 2 frozen=%+v", frozen2)
	}
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	reg2 := openWiringRegistry(t, dbPath)
	defer reg2.Close()
	if _, err := reg2.Get(ctx, created.ID); !errors.Is(err, ErrAgentAssetNotFound) {
		t.Fatalf("get after reopen err=%v want ErrAgentAssetNotFound", err)
	}
	if got := revisionsOf(t, reg2, created.ID); !reflect.DeepEqual(revsBefore, got) {
		t.Fatalf("revisions after reopen differ:\nwant=%+v\ngot=%+v", revsBefore, got)
	}
	if _, err := reg2.store.revisionAsset(created.ID, 2); err != nil {
		t.Fatalf("revision 2 of deleted asset after reopen: %v", err)
	}
}

// AssertRunnable is the run-entry gate (S1 wires it): enabled passes,
// disabled is rejected with ErrAgentDisabled, unknown ids with
// ErrAgentAssetNotFound. A disabled asset keeps its revisions readable.
func TestAssertRunnableGatesDisabledAssets(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_disabled_gate.db")
	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{Name: "Toggle", RoleBase: RoleBaseMain})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.AssertRunnable(ctx, created.ID); err != nil {
		t.Fatalf("enabled asset rejected: %v", err)
	}
	if err := reg.AssertRunnable(ctx, "no-such-agent"); !errors.Is(err, ErrAgentAssetNotFound) {
		t.Fatalf("unknown id err=%v want ErrAgentAssetNotFound", err)
	}
	if err := reg.AssertRunnable(ctx, "builtin-flow-conductor"); err != nil {
		t.Fatalf("enabled builtin rejected: %v", err)
	}

	off := false
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if err := reg.AssertRunnable(ctx, created.ID); !errors.Is(err, ErrAgentDisabled) {
		t.Fatalf("disabled asset err=%v want ErrAgentDisabled", err)
	}

	// History stays readable while disabled; each revision froze the enabled
	// flag in effect at its own time.
	frozen1, err := reg.store.revisionAsset(created.ID, 1)
	if err != nil {
		t.Fatalf("revision 1 of disabled asset: %v", err)
	}
	if !frozen1.Enabled {
		t.Fatalf("revision 1 enabled=%v want true", frozen1.Enabled)
	}
	frozen2, err := reg.store.revisionAsset(created.ID, 2)
	if err != nil {
		t.Fatalf("revision 2 of disabled asset: %v", err)
	}
	if frozen2.Enabled {
		t.Fatalf("revision 2 enabled=%v want false", frozen2.Enabled)
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 2 {
		t.Fatalf("disabled asset head=%d err=%v want 2", head, err)
	}

	on := true
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	if err := reg.AssertRunnable(ctx, created.ID); err != nil {
		t.Fatalf("re-enabled asset rejected: %v", err)
	}

	// The gate is storage-agnostic: the memory-only registry behaves the same.
	mem := NewInMemoryAgentRegistry()
	m, err := mem.Create(ctx, &CreateAgentRequest{Name: "Mem", RoleBase: RoleBaseSub})
	if err != nil {
		t.Fatal(err)
	}
	if err := mem.AssertRunnable(ctx, m.ID); err != nil {
		t.Fatalf("memory enabled rejected: %v", err)
	}
	if _, err := mem.Update(ctx, m.ID, &UpdateAgentRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if err := mem.AssertRunnable(ctx, m.ID); !errors.Is(err, ErrAgentDisabled) {
		t.Fatalf("memory disabled err=%v want ErrAgentDisabled", err)
	}
}

// The chain built before Close persists across a restart and continues with
// new revisions, leaving the pre-restart rows untouched.
func TestRevisionChainSurvivesReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_reopen_chain.db")
	reg := openWiringRegistry(t, dbPath)
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{Name: "Durable", RoleBase: RoleBaseCoder})
	if err != nil {
		t.Fatal(err)
	}
	prompt := "prompt across restart"
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{SystemPrompt: &prompt}); err != nil {
		t.Fatal(err)
	}
	beforeRevs := revisionsOf(t, reg, created.ID)
	beforeHeads := dumpHeadRows(t, reg.store.db)
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	reg2 := openWiringRegistry(t, dbPath)
	defer reg2.Close()
	if got := revisionsOf(t, reg2, created.ID); !reflect.DeepEqual(beforeRevs, got) {
		t.Fatalf("revisions not durable:\nbefore=%+v\nafter=%+v", beforeRevs, got)
	}
	if got := dumpHeadRows(t, reg2.store.db); !reflect.DeepEqual(beforeHeads, got) {
		t.Fatalf("heads not durable:\nbefore=%+v\nafter=%+v", beforeHeads, got)
	}
	if head, err := reg2.store.headRevision(created.ID); err != nil || head != 2 {
		t.Fatalf("head after reopen=%d err=%v want 2", head, err)
	}

	name := "Durable Renamed"
	if _, err := reg2.Update(ctx, created.ID, &UpdateAgentRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if head, err := reg2.store.headRevision(created.ID); err != nil || head != 3 {
		t.Fatalf("head after reopen update=%d err=%v want 3", head, err)
	}
	revs := revisionsOf(t, reg2, created.ID)
	if len(revs) != 3 {
		t.Fatalf("revisions=%d want 3", len(revs))
	}
	if !reflect.DeepEqual(revs[:2], beforeRevs) {
		t.Fatalf("old revisions changed after reopen update:\nbefore=%+v\nafter=%+v", beforeRevs, revs[:2])
	}
	frozen3, err := reg2.store.revisionAsset(created.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if frozen3.Name != "Durable Renamed" || frozen3.SystemPrompt != prompt {
		t.Fatalf("revision 3 frozen=%+v", frozen3)
	}
}

// A registry Update on a migrated (pre-revision) asset continues the chain
// the migration started: revision 1 keeps the migration snapshot and the
// edit lands as revision 2 tagged "update".
func TestUpdateAfterMigrationAppendsUpdateRevision(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_migrate_update.db")
	assets := revisionTestAssets()[2:] // plugin-scout: not builtin, editable
	createLegacyAgentsDB(t, dbPath, assets)

	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()
	ctx := context.Background()
	scout := assets[0]

	if head, err := reg.store.headRevision(scout.ID); err != nil || head != 1 {
		t.Fatalf("head after migration=%d err=%v want 1", head, err)
	}
	name := "Scout Renamed"
	temp := 0.9
	if _, err := reg.Update(ctx, scout.ID, &UpdateAgentRequest{
		Name:    &name,
		Binding: &Binding{Model: "kimi-k2", Temperature: &temp},
	}); err != nil {
		t.Fatal(err)
	}
	if head, err := reg.store.headRevision(scout.ID); err != nil || head != 2 {
		t.Fatalf("head after update=%d err=%v want 2", head, err)
	}

	revs := revisionsOf(t, reg, scout.ID)
	if len(revs) != 2 {
		t.Fatalf("revisions=%d want 2", len(revs))
	}
	if revs[0].source != revisionSourceMigrationV1 {
		t.Fatalf("revision 1 source=%q want %q", revs[0].source, revisionSourceMigrationV1)
	}
	frozen1, err := reg.store.revisionAsset(scout.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if frozen1.Name != "Scout Plugin" || frozen1.Binding.Model != "" {
		t.Fatalf("revision 1 polluted by update: %+v", frozen1)
	}
	if revs[1].source != revisionSourceUpdate {
		t.Fatalf("revision 2 source=%q want %q", revs[1].source, revisionSourceUpdate)
	}
	frozen2, err := reg.store.revisionAsset(scout.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if frozen2.Name != "Scout Renamed" || frozen2.Binding.Model != "kimi-k2" {
		t.Fatalf("revision 2 frozen=%+v", frozen2)
	}
}

// T1.03.c stats-path narrowing: IncrementUsage/SetScore persist the counters
// but never append a revision, so consecutive stats writes leave head and
// revision history untouched; a following config Update still appends the
// next revision, proving the narrowing did not reach the main path.
func TestStatsWritesDoNotAdvanceRevisionHead(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats_narrowing.db")
	reg := openWiringRegistry(t, dbPath)
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{
		Name: "Metered", RoleBase: RoleBaseCoder, SystemPrompt: "prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 1 {
		t.Fatalf("head after create=%d err=%v want 1", head, err)
	}
	revsBefore := revisionsOf(t, reg, created.ID)
	headsBefore := dumpHeadRows(t, reg.store.db)
	if len(revsBefore) != 1 {
		t.Fatalf("revisions after create=%d want 1", len(revsBefore))
	}

	for i := 0; i < 3; i++ {
		if err := reg.IncrementUsage(ctx, created.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.SetScore(ctx, created.ID, 4.5); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetScore(ctx, created.ID, 4.75); err != nil {
		t.Fatal(err)
	}

	// Head and revision history are untouched by the stats writes.
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 1 {
		t.Fatalf("head after stats=%d err=%v want 1", head, err)
	}
	if got := revisionsOf(t, reg, created.ID); !reflect.DeepEqual(revsBefore, got) {
		t.Fatalf("stats writes changed revisions:\nbefore=%+v\nafter=%+v", revsBefore, got)
	}
	if got := dumpHeadRows(t, reg.store.db); !reflect.DeepEqual(headsBefore, got) {
		t.Fatalf("stats writes changed head rows:\nbefore=%+v\nafter=%+v", headsBefore, got)
	}

	// The counters did persist, in memory and in the agents row.
	got, err := reg.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.UsageCount != 3 || got.Stats.Score != 4.75 {
		t.Fatalf("stats=%+v want usage 3 score 4.75", got.Stats)
	}
	payload := ""
	for _, row := range dumpAgentsRows(t, reg.store.db) {
		if row.id == created.ID {
			payload = row.payload
		}
	}
	live := mustUnmarshalAsset(t, payload)
	if live.Stats.UsageCount != 3 || live.Stats.Score != 4.75 {
		t.Fatalf("persisted stats=%+v want usage 3 score 4.75", live.Stats)
	}
	// The frozen snapshot keeps the stats in effect when it was appended.
	frozen1, err := reg.store.revisionAsset(created.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if frozen1.Stats.UsageCount != 0 || frozen1.Stats.Score != 0 {
		t.Fatalf("revision 1 frozen stats polluted: %+v", frozen1.Stats)
	}
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	// Stats durability survives a restart while the head still does not move.
	reg2 := openWiringRegistry(t, dbPath)
	defer reg2.Close()
	reopened, err := reg2.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Stats.UsageCount != 3 || reopened.Stats.Score != 4.75 {
		t.Fatalf("stats after reopen=%+v want usage 3 score 4.75", reopened.Stats)
	}
	if head, err := reg2.store.headRevision(created.ID); err != nil || head != 1 {
		t.Fatalf("head after reopen=%d err=%v want 1", head, err)
	}

	// Negative control: a config Update after the stats writes still appends
	// revision 2 tagged "update" — the main path is not narrowed away.
	name := "Metered Renamed"
	if _, err := reg2.Update(ctx, created.ID, &UpdateAgentRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if head, err := reg2.store.headRevision(created.ID); err != nil || head != 2 {
		t.Fatalf("head after update=%d err=%v want 2", head, err)
	}
	revs := revisionsOf(t, reg2, created.ID)
	if len(revs) != 2 || revs[1].revision != 2 || revs[1].source != revisionSourceUpdate {
		t.Fatalf("revisions after update=%+v want rev1+rev2(update)", revs)
	}
	// Revision 2 froze the stats values in effect at its own append time.
	frozen2, err := reg2.store.revisionAsset(created.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if frozen2.Stats.UsageCount != 3 || frozen2.Stats.Score != 4.75 {
		t.Fatalf("revision 2 frozen stats=%+v want usage 3 score 4.75", frozen2.Stats)
	}
}

// §28 T1.03.c. Run records do not exist yet (S1 wires AssertRunnable and
// stores agent_revision_id); this test supplies the equivalent evidence from
// the pinned-revision consumer perspective: a consumer holding the frozen
// snapshot of revisionAsset(id, rev) sees its content field-by-field
// unchanged no matter how the asset is edited afterwards. Once S1 lands run
// records, this test extends to bind an actual run row to the pinned
// revision.
func TestAssetEditPreservesRunConfig(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_pinned_revision.db")
	reg := openWiringRegistry(t, dbPath)
	defer reg.Close()
	ctx := context.Background()

	temp := 0.4
	maxTok := 2048
	created, err := reg.Create(ctx, &CreateAgentRequest{
		Name: "Runner", RoleBase: RoleBaseCoder,
		Description: "pinned config consumer", Version: "1.0.0",
		SystemPrompt: "original prompt",
		Binding:      &Binding{Model: "gpt-5", Channel: "default", Temperature: &temp, MaxTokens: &maxTok},
		Mounts:       &Mounts{MCPTools: []string{"fs.read"}, Skills: []string{"review"}},
		StageTags:    []string{"coding"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The consumer pins revision 1: the exact configuration a run binds to.
	pinned1, err := reg.store.revisionAsset(created.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if *pinned1.Binding.Temperature != 0.4 || *pinned1.Binding.MaxTokens != 2048 ||
		pinned1.SystemPrompt != "original prompt" || pinned1.Version != "1.0.0" {
		t.Fatalf("pinned revision 1=%+v", pinned1)
	}

	// First edit round, then a second consumer pins revision 2.
	name2, prompt2, ver2 := "Runner v2", "edited prompt", "2.0.0"
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{
		Name: &name2, SystemPrompt: &prompt2, Version: &ver2,
	}); err != nil {
		t.Fatal(err)
	}
	pinned2, err := reg.store.revisionAsset(created.ID, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Later edits rewrite every configuration facet and toggle enabled.
	temp3 := 0.9
	desc3 := "rewritten"
	off := false
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{
		Description: &desc3,
		Binding:     &Binding{Model: "kimi-k2", Channel: "fast", Temperature: &temp3},
		Mounts:      &Mounts{MCPTools: []string{"shell.exec"}, Skills: []string{"lint"}},
		StageTags:   []string{"review"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 4 {
		t.Fatalf("head after edits=%d err=%v want 4 (edits really landed)", head, err)
	}

	// The pinned snapshots are unchanged: rereads equal the captured
	// snapshots, and revision 1 still matches the originally created asset.
	reread1, err := reg.store.revisionAsset(created.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pinned1, reread1) {
		t.Fatalf("pinned revision 1 drifted:\npinned=%+v\nreread=%+v", pinned1, reread1)
	}
	if mustJSON(t, pinned1) != mustJSON(t, created) {
		t.Fatalf("pinned revision 1 no longer matches the created asset:\nwant=%s\ngot=%s",
			mustJSON(t, created), mustJSON(t, pinned1))
	}
	reread2, err := reg.store.revisionAsset(created.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pinned2, reread2) {
		t.Fatalf("pinned revision 2 drifted:\npinned=%+v\nreread=%+v", pinned2, reread2)
	}

	// Field-by-field spot check on the configuration facets a run resolves.
	if reread1.Name != "Runner" || reread1.SystemPrompt != "original prompt" ||
		reread1.Binding.Model != "gpt-5" || *reread1.Binding.Temperature != 0.4 ||
		len(reread1.Mounts.MCPTools) != 1 || reread1.Mounts.MCPTools[0] != "fs.read" ||
		len(reread1.StageTags) != 1 || reread1.StageTags[0] != "coding" || !reread1.Enabled {
		t.Fatalf("pinned revision 1 fields=%+v", reread1)
	}
	if reread2.Name != "Runner v2" || reread2.SystemPrompt != "edited prompt" ||
		reread2.Version != "2.0.0" || reread2.Binding.Model != "gpt-5" || !reread2.Enabled {
		t.Fatalf("pinned revision 2 fields=%+v", reread2)
	}
}

// §28 T1.03.c, folding in the T1.03.b disabled coverage: a disabled asset is
// gated out of new runs, yet its full revision history stays readable — each
// revision keeps the enabled flag in effect at its own time — and remains
// readable after a restart while the asset is still disabled.
func TestDisabledAssetHistoryReadable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_disabled_history.db")
	reg := openWiringRegistry(t, dbPath)
	ctx := context.Background()

	created, err := reg.Create(ctx, &CreateAgentRequest{
		Name: "Gatekeeper", RoleBase: RoleBaseCritic, SystemPrompt: "prompt v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := "prompt v2"
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{SystemPrompt: &prompt}); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := reg.Update(ctx, created.ID, &UpdateAgentRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if err := reg.AssertRunnable(ctx, created.ID); !errors.Is(err, ErrAgentDisabled) {
		t.Fatalf("disabled asset err=%v want ErrAgentDisabled", err)
	}
	if head, err := reg.store.headRevision(created.ID); err != nil || head != 3 {
		t.Fatalf("head=%d err=%v want 3", head, err)
	}

	assertHistory := func(r *InMemoryAgentRegistry) {
		t.Helper()
		revs := revisionsOf(t, r, created.ID)
		if len(revs) != 3 {
			t.Fatalf("revisions=%d want 3", len(revs))
		}
		frozen1, err := r.store.revisionAsset(created.ID, 1)
		if err != nil {
			t.Fatalf("revision 1 of disabled asset: %v", err)
		}
		if !frozen1.Enabled || frozen1.SystemPrompt != "prompt v1" {
			t.Fatalf("revision 1 frozen=%+v", frozen1)
		}
		frozen2, err := r.store.revisionAsset(created.ID, 2)
		if err != nil {
			t.Fatalf("revision 2 of disabled asset: %v", err)
		}
		if !frozen2.Enabled || frozen2.SystemPrompt != "prompt v2" {
			t.Fatalf("revision 2 frozen=%+v", frozen2)
		}
		frozen3, err := r.store.revisionAsset(created.ID, 3)
		if err != nil {
			t.Fatalf("revision 3 of disabled asset: %v", err)
		}
		if frozen3.Enabled {
			t.Fatalf("revision 3 enabled=%v want false", frozen3.Enabled)
		}
	}
	assertHistory(reg)
	revsBefore := revisionsOf(t, reg, created.ID)
	if err := reg.Close(); err != nil {
		t.Fatal(err)
	}

	// Still disabled after a restart: the gate still rejects and the history
	// is byte-identical.
	reg2 := openWiringRegistry(t, dbPath)
	defer reg2.Close()
	if err := reg2.AssertRunnable(ctx, created.ID); !errors.Is(err, ErrAgentDisabled) {
		t.Fatalf("disabled asset after reopen err=%v want ErrAgentDisabled", err)
	}
	if got := revisionsOf(t, reg2, created.ID); !reflect.DeepEqual(revsBefore, got) {
		t.Fatalf("history after reopen differs:\nbefore=%+v\nafter=%+v", revsBefore, got)
	}
	assertHistory(reg2)
}
