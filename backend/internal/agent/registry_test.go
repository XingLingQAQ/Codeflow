package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCRUDRoundTrip(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	created, err := r.Create(ctx, &CreateAgentRequest{
		Name:     "My Agent",
		RoleBase: RoleBaseCoder,
		StageTags: []string{"coding"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "My Agent" || created.RoleBase != RoleBaseCoder {
		t.Fatalf("unexpected create result: %+v", created)
	}
	if created.Source != SourceUser {
		t.Fatalf("source=%s want user", created.Source)
	}
	if created.Version != "0.1.0" {
		t.Fatalf("version=%s want 0.1.0", created.Version)
	}

	got, err := r.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "My Agent" {
		t.Fatalf("get name=%s want My Agent", got.Name)
	}

	name := "Renamed"
	ver := "0.2.0"
	updated, err := r.Update(ctx, created.ID, &UpdateAgentRequest{Name: &name, Version: &ver})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Renamed" || updated.Version != "0.2.0" {
		t.Fatalf("update result: %+v", updated)
	}

	if err := r.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, created.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestBuiltinProtectionUpdate(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	name := "hacked"
	_, err := r.Update(context.Background(), "builtin-flow-conductor", &UpdateAgentRequest{Name: &name})
	if err == nil {
		t.Fatal("expected error updating builtin")
	}
	if !errors.Is(err, ErrBuiltinProtected) {
		t.Fatalf("error=%v want ErrBuiltinProtected", err)
	}
}

func TestBuiltinProtectionDelete(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	for _, id := range []string{
		"builtin-flow-conductor", "builtin-code-artisan",
		"builtin-scout", "builtin-red-critic", "builtin-deep-researcher",
	} {
		if err := r.Delete(context.Background(), id); err == nil {
			t.Fatalf("expected error deleting builtin %s", id)
		}
	}
}

func TestCreateCannotMintBuiltinSource(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	a, err := r.Create(context.Background(), &CreateAgentRequest{
		Name: "Fake Builtin", RoleBase: RoleBaseMain, Source: SourceBuiltin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Source != SourceUser {
		t.Fatalf("source=%s want user", a.Source)
	}
	if err := r.Delete(context.Background(), a.ID); err != nil {
		t.Fatalf("user agent should be deletable: %v", err)
	}
}

func TestListFilteredByStage(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	reviewAgents, err := r.ListFiltered(ctx, "review", "", "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range reviewAgents {
		if a.ID == "builtin-red-critic" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected red-critic in review stage filter")
	}

	researchAgents, err := r.ListFiltered(ctx, "research", "", "")
	if err != nil {
		t.Fatal(err)
	}
	foundResearcher := false
	for _, a := range researchAgents {
		if a.ID == "builtin-deep-researcher" {
			foundResearcher = true
		}
	}
	if !foundResearcher {
		t.Fatal("expected deep-researcher in research stage filter")
	}
}

func TestListFilteredByRole(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	coders, err := r.ListFiltered(ctx, "", RoleBaseCoder, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(coders) == 0 {
		t.Fatal("expected at least one coder")
	}
	for _, a := range coders {
		if a.RoleBase != RoleBaseCoder {
			t.Fatalf("non-coder in role filter: %s %s", a.ID, a.RoleBase)
		}
	}
}

func TestListFilteredBySource(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	builtins, err := r.ListFiltered(ctx, "", "", SourceBuiltin)
	if err != nil {
		t.Fatal(err)
	}
	if len(builtins) != 5 {
		t.Fatalf("builtin count=%d want 5", len(builtins))
	}

	userAgents, err := r.ListFiltered(ctx, "", "", SourceUser)
	if err != nil {
		t.Fatal(err)
	}
	if len(userAgents) != 0 {
		t.Fatalf("user count=%d want 0", len(userAgents))
	}
}

func TestListFilteredCombined(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	result, err := r.ListFiltered(ctx, "coding", RoleBaseCoder, SourceBuiltin)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "builtin-code-artisan" {
		t.Fatalf("combined filter: got %d agents", len(result))
	}
}

func TestIncrementUsageConcurrency(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	id := "builtin-flow-conductor"

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := r.IncrementUsage(ctx, id); err != nil {
				t.Errorf("increment: %v", err)
			}
		}()
	}
	wg.Wait()

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.UsageCount != n {
		t.Fatalf("usage=%d want %d", got.Stats.UsageCount, n)
	}
}

func TestSetScoreConcurrency(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	id := "builtin-scout"

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		score := float64(i) / float64(n)
		go func(s float64) {
			defer wg.Done()
			if err := r.SetScore(ctx, id, s); err != nil {
				t.Errorf("set score: %v", err)
			}
		}(score)
	}
	wg.Wait()

	got, err := r.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Score < 0 || got.Stats.Score >= 1.0 {
		t.Fatalf("score=%f out of expected range", got.Stats.Score)
	}
}

func TestCloneOnRead(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	created, err := r.Create(ctx, &CreateAgentRequest{
		Name:      "Clone Test",
		RoleBase:  RoleBaseSub,
		StageTags: []string{"coding"},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := r.Get(ctx, created.ID)
	got.Name = "Mutated"
	got.StageTags[0] = "hacked"
	temp := 0.99
	got.Binding.Temperature = &temp

	internal, _ := r.Get(ctx, created.ID)
	if internal.Name != "Clone Test" {
		t.Fatalf("internal name mutated to %s", internal.Name)
	}
	if internal.StageTags[0] != "coding" {
		t.Fatalf("internal stage_tags mutated to %v", internal.StageTags)
	}
	if internal.Binding.Temperature != nil {
		t.Fatal("internal temperature mutated")
	}
}

func TestValidationRejectsInvalidRoleBase(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	_, err := r.Create(context.Background(), &CreateAgentRequest{
		Name: "Bad Role", RoleBase: "wizard",
	})
	if err == nil {
		t.Fatal("expected error for invalid role_base")
	}
	if !strings.Contains(err.Error(), "role_base") {
		t.Fatalf("error should mention role_base: %v", err)
	}
}

func TestValidationRejectsEmptyName(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	_, err := r.Create(context.Background(), &CreateAgentRequest{
		Name: "   ", RoleBase: RoleBaseMain,
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidationRejectsInvalidSource(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	_, err := r.Create(context.Background(), &CreateAgentRequest{
		Name: "Bad Source", RoleBase: RoleBaseMain, Source: "github",
	})
	if err == nil {
		t.Fatal("expected error for invalid source")
	}
}

func TestPromptSanity(t *testing.T) {
	agents := builtinAgents()
	checks := map[string][]string{
		"builtin-flow-conductor":  {"gate", "handoff"},
		"builtin-code-artisan":    {"duplicate", "guard"},
		"builtin-scout":           {"read-only", "file"},
		"builtin-red-critic":      {"security", "severity"},
		"builtin-deep-researcher": {"citation", "evidence"},
	}
	for _, a := range agents {
		t.Run(a.Name, func(t *testing.T) {
			if a.SystemPrompt == "" {
				t.Fatal("empty system prompt")
			}
			if len(a.SystemPrompt) < 100 {
				t.Fatalf("prompt too short: %d chars", len(a.SystemPrompt))
			}
			keywords, ok := checks[a.ID]
			if !ok {
				t.Fatalf("no keyword checks defined for %s", a.ID)
			}
			lower := strings.ToLower(a.SystemPrompt)
			for _, kw := range keywords {
				if !strings.Contains(lower, kw) {
					t.Errorf("prompt for %s missing keyword %q", a.Name, kw)
				}
			}
		})
	}
}

func TestGetSetAgentRegistry(t *testing.T) {
	prev := GetAgentRegistry()
	t.Cleanup(func() { SetAgentRegistry(prev) })
	custom := NewInMemoryAgentRegistry()
	SetAgentRegistry(custom)
	if GetAgentRegistry() != custom {
		t.Fatal("expected custom registry")
	}
	if !HasAgentRegistry() {
		t.Fatal("expected HasAgentRegistry true")
	}
}

func TestBuiltinsAllEnabled(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	list, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 5 {
		t.Fatalf("builtin count=%d want >=5", len(list))
	}
	for _, a := range list {
		if a.Source == SourceBuiltin && !a.Enabled {
			t.Fatalf("builtin %s is disabled", a.ID)
		}
	}
}

func TestNotFoundError(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	_, err := r.Get(ctx, "nonexistent")
	if !errors.Is(err, ErrAgentAssetNotFound) {
		t.Fatalf("get error=%v want ErrAgentAssetNotFound", err)
	}

	_, err = r.Update(ctx, "nonexistent", &UpdateAgentRequest{})
	if !errors.Is(err, ErrAgentAssetNotFound) {
		t.Fatalf("update error=%v want ErrAgentAssetNotFound", err)
	}

	err = r.Delete(ctx, "nonexistent")
	if !errors.Is(err, ErrAgentAssetNotFound) {
		t.Fatalf("delete error=%v want ErrAgentAssetNotFound", err)
	}
}

// --- SQLite tests ---

func TestSQLitePersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents.db")
	reg, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	defer reg.Close()

	list, err := reg.List(context.Background())
	if err != nil || len(list) < 5 {
		t.Fatalf("builtins list=%d err=%v", len(list), err)
	}

	created, err := reg.Create(context.Background(), &CreateAgentRequest{
		Name: "Persist Me", RoleBase: RoleBaseSub,
	})
	if err != nil {
		t.Fatal(err)
	}

	reg.Close()

	reg2, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg2.Close()

	got, err := reg2.Get(context.Background(), created.ID)
	if err != nil || got.Name != "Persist Me" {
		t.Fatalf("reload got=%+v err=%v", got, err)
	}

	list2, _ := reg2.List(context.Background())
	if len(list2) != len(list)+1 {
		t.Fatalf("list size=%d want %d (no builtin reseed)", len(list2), len(list)+1)
	}
}

func TestSQLiteStatsSurvival(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_stats.db")
	reg, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := reg.IncrementUsage(ctx, "builtin-flow-conductor"); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.SetScore(ctx, "builtin-flow-conductor", 4.5); err != nil {
		t.Fatal(err)
	}
	reg.Close()

	reg2, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg2.Close()

	got, err := reg2.Get(ctx, "builtin-flow-conductor")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.UsageCount != 5 {
		t.Fatalf("usage=%d want 5", got.Stats.UsageCount)
	}
	if got.Stats.Score != 4.5 {
		t.Fatalf("score=%f want 4.5", got.Stats.Score)
	}
}

func TestSQLiteBuiltinSeedOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_seed.db")

	reg1, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	list1, _ := reg1.List(context.Background())
	reg1.Close()

	reg2, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg2.Close()
	list2, _ := reg2.List(context.Background())

	if len(list1) != len(list2) {
		t.Fatalf("seed duplication: first=%d second=%d", len(list1), len(list2))
	}
}

func TestSQLiteDeletePersists(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agents_del.db")
	reg, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}

	ctx := context.Background()
	created, err := reg.Create(ctx, &CreateAgentRequest{
		Name: "Delete Me", RoleBase: RoleBaseCritic,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	reg.Close()

	reg2, err := NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reg2.Close()

	if _, err := reg2.Get(ctx, created.ID); err == nil {
		t.Fatal("deleted agent reappeared after reopen")
	}
}

func TestUpdateRoleBaseValidation(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	created, err := r.Create(ctx, &CreateAgentRequest{
		Name: "Role Test", RoleBase: RoleBaseSub,
	})
	if err != nil {
		t.Fatal(err)
	}
	badRole := RoleBase("wizard")
	_, err = r.Update(ctx, created.ID, &UpdateAgentRequest{RoleBase: &badRole})
	if err == nil {
		t.Fatal("expected error for invalid role_base in update")
	}
}

func TestEnableDisable(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	created, err := r.Create(ctx, &CreateAgentRequest{
		Name: "Toggle", RoleBase: RoleBaseMain,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Enabled {
		t.Fatal("new agent should be enabled")
	}
	off := false
	updated, err := r.Update(ctx, created.ID, &UpdateAgentRequest{Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("agent should be disabled")
	}
}
