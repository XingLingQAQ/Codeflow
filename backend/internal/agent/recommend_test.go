package agent

import (
	"context"
	"testing"
)

func TestSelectForStageCodingTop(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	results, err := SelectForStage(ctx, r, "coding", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for coding stage")
	}
	if results[0].ID != "builtin-code-artisan" {
		t.Fatalf("top pick=%s want builtin-code-artisan", results[0].ID)
	}
}

func TestSelectForStageReviewTop(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	results, err := SelectForStage(ctx, r, "review", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for review stage")
	}
	if results[0].ID != "builtin-red-critic" {
		t.Fatalf("top pick=%s want builtin-red-critic", results[0].ID)
	}
}

func TestSelectForStageResearchTop(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	results, err := SelectForStage(ctx, r, "research", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for research stage")
	}
	if results[0].ID != "builtin-deep-researcher" {
		t.Fatalf("top pick=%s want builtin-deep-researcher", results[0].ID)
	}
}

func TestSelectForStagePlanningIncludes(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	results, err := SelectForStage(ctx, r, "planning", 10)
	if err != nil {
		t.Fatal(err)
	}
	conductorFound := false
	scoutFound := false
	for _, a := range results {
		if a.ID == "builtin-flow-conductor" {
			conductorFound = true
		}
		if a.ID == "builtin-scout" {
			scoutFound = true
		}
	}
	if !conductorFound {
		t.Fatal("expected flow-conductor in planning results")
	}
	if !scoutFound {
		t.Fatal("expected scout in planning results")
	}
}

func TestSelectForStageGeneralistIncluded(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	generalist, err := r.Create(ctx, &CreateAgentRequest{
		Name: "Generalist", RoleBase: RoleBaseSub,
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := SelectForStage(ctx, r, "coding", 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range results {
		if a.ID == generalist.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("generalist (no stage_tags) should appear in results")
	}
	if results[0].ID == generalist.ID {
		t.Fatal("generalist should rank below a stage-tagged specialist")
	}
}

func TestSelectForStageDisabledExcluded(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	created, err := r.Create(ctx, &CreateAgentRequest{
		Name: "Disabled", RoleBase: RoleBaseCoder, StageTags: []string{"coding"},
	})
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := r.Update(ctx, created.ID, &UpdateAgentRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}

	results, err := SelectForStage(ctx, r, "coding", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range results {
		if a.ID == created.ID {
			t.Fatal("disabled agent should not appear")
		}
	}
}

func TestSelectForStageLimitDefault(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	results, err := SelectForStage(ctx, r, "coding", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 5 {
		t.Fatalf("default limit should be 5, got %d", len(results))
	}
}

func TestSelectForStageLimitHonored(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	results, err := SelectForStage(ctx, r, "coding", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("limit=1 got %d results", len(results))
	}
}

func TestSelectForStageEmptyStageError(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	_, err := SelectForStage(context.Background(), r, "", 5)
	if err == nil {
		t.Fatal("expected error for empty stage")
	}
}

func TestSelectForStageDeterminism(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()
	r1, err := SelectForStage(ctx, r, "coding", 10)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := SelectForStage(ctx, r, "coding", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1) != len(r2) {
		t.Fatalf("non-deterministic: len %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].ID != r2[i].ID {
			t.Fatalf("non-deterministic at [%d]: %s vs %s", i, r1[i].ID, r2[i].ID)
		}
	}
}

func TestSelectForStageScoreTiebreak(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	a1, _ := r.Create(ctx, &CreateAgentRequest{
		Name: "AA Coder", RoleBase: RoleBaseCoder, StageTags: []string{"coding"},
	})
	a2, _ := r.Create(ctx, &CreateAgentRequest{
		Name: "BB Coder", RoleBase: RoleBaseCoder, StageTags: []string{"coding"},
	})
	r.SetScore(ctx, a2.ID, 5.0)
	r.SetScore(ctx, a1.ID, 1.0)

	results, err := SelectForStage(ctx, r, "coding", 100)
	if err != nil {
		t.Fatal(err)
	}
	var posA1, posA2 int
	for i, a := range results {
		if a.ID == a1.ID {
			posA1 = i
		}
		if a.ID == a2.ID {
			posA2 = i
		}
	}
	if posA2 >= posA1 {
		t.Fatalf("higher-scored agent should rank first: a2@%d a1@%d", posA2, posA1)
	}
}

func TestSelectForStageUsageCountTiebreak(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	a1, _ := r.Create(ctx, &CreateAgentRequest{
		Name: "AA Same", RoleBase: RoleBaseCoder, StageTags: []string{"coding"},
	})
	a2, _ := r.Create(ctx, &CreateAgentRequest{
		Name: "BB Same", RoleBase: RoleBaseCoder, StageTags: []string{"coding"},
	})
	for i := 0; i < 50; i++ {
		r.IncrementUsage(ctx, a2.ID)
	}

	results, err := SelectForStage(ctx, r, "coding", 100)
	if err != nil {
		t.Fatal(err)
	}
	var posA1, posA2 int
	for i, a := range results {
		if a.ID == a1.ID {
			posA1 = i
		}
		if a.ID == a2.ID {
			posA2 = i
		}
	}
	if posA2 >= posA1 {
		t.Fatalf("higher-usage agent should rank first: a2@%d a1@%d", posA2, posA1)
	}
}

func TestSelectForStageCustomStage(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	r.Create(ctx, &CreateAgentRequest{
		Name: "Deploy Bot", RoleBase: RoleBaseSub, StageTags: []string{"deploy"},
	})

	results, err := SelectForStage(ctx, r, "deploy", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for custom stage with matching agent")
	}
}

func TestSelectForStageRoleFitRanksAboveMismatch(t *testing.T) {
	r := NewInMemoryAgentRegistry()
	ctx := context.Background()

	critic, _ := r.Create(ctx, &CreateAgentRequest{
		Name: "AA Critic", RoleBase: RoleBaseCritic, StageTags: []string{"coding"},
	})
	coder, _ := r.Create(ctx, &CreateAgentRequest{
		Name: "ZZ Coder", RoleBase: RoleBaseCoder, StageTags: []string{"coding"},
	})

	results, err := SelectForStage(ctx, r, "coding", 100)
	if err != nil {
		t.Fatal(err)
	}
	var posCritic, posCoder int
	for i, a := range results {
		if a.ID == critic.ID {
			posCritic = i
		}
		if a.ID == coder.ID {
			posCoder = i
		}
	}
	if posCoder >= posCritic {
		t.Fatalf("coder role should rank above critic for coding: coder@%d critic@%d", posCoder, posCritic)
	}
}
