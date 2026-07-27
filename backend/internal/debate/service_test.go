package debate

import (
	"context"
	"errors"
	"testing"
)

func mustCreate(t *testing.T, m IDebateManager, mod func(*DebateCreateRequest)) *Debate {
	t.Helper()
	req := &DebateCreateRequest{
		Title:        "t",
		GeneratorID:  "g",
		CriticID:     "c",
		InitialInput: "start",
	}
	if mod != nil {
		mod(req)
	}
	d, err := m.CreateDebate(context.Background(), req)
	if err != nil {
		t.Fatalf("create debate: %v", err)
	}
	return d
}

func TestNextRoundAdvancesAndRecords(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)

	got, err := m.NextRound(ctx, d.ID, &NextRoundRequest{
		GeneratorOutput: "impl v1",
		CriticFeedback:  "please continue to the next step",
	})
	if err != nil {
		t.Fatalf("next round: %v", err)
	}
	if got.CurrentRound != 2 {
		t.Fatalf("current round = %d, want 2", got.CurrentRound)
	}
	if got.Status != DebateStatusInProgress {
		t.Fatalf("status = %s, want in_progress", got.Status)
	}
	if len(got.Rounds) != 2 {
		t.Fatalf("rounds = %d, want 2", len(got.Rounds))
	}
	if got.Rounds[0].GeneratorOutput != "impl v1" || got.Rounds[0].CriticFeedback != "please continue to the next step" {
		t.Fatalf("round 0 not recorded: %+v", got.Rounds[0])
	}
	if got.Rounds[0].CompletedAt == 0 {
		t.Fatal("round 0 CompletedAt not set")
	}
	// Critic feedback seeds the next round's generator input.
	if got.Rounds[1].GeneratorInput != "please continue to the next step" {
		t.Fatalf("round 1 input = %q", got.Rounds[1].GeneratorInput)
	}
}

func TestNextRoundDetectsConflictKeywords(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)

	got, err := m.NextRound(ctx, d.ID, &NextRoundRequest{
		GeneratorOutput: "code",
		CriticFeedback:  "this has a security vulnerability and the logic is wrong",
	})
	if err != nil {
		t.Fatalf("next round: %v", err)
	}
	if len(got.Conflicts) != 2 {
		t.Fatalf("conflicts = %d, want 2 (%+v)", len(got.Conflicts), got.Conflicts)
	}
	byType := map[string]*Conflict{}
	for _, c := range got.Conflicts {
		byType[c.Type] = c
	}
	sec, ok := byType["security"]
	if !ok {
		t.Fatal("missing security conflict")
	}
	if sec.Severity != SeverityCritical {
		t.Fatalf("security severity = %s, want critical", sec.Severity)
	}
	if _, ok := byType["logic"]; !ok {
		t.Fatal("missing logic conflict")
	}
	// Conflicts are linked back onto the completed round.
	if len(got.Rounds[0].ConflictsFound) != 2 {
		t.Fatalf("round conflicts_found = %d, want 2", len(got.Rounds[0].ConflictsFound))
	}
	for _, c := range got.Conflicts {
		if c.ID == "" || c.DebateID != d.ID || c.CreatedAt == 0 {
			t.Fatalf("conflict not fully populated: %+v", c)
		}
	}
}

func TestNextRoundPausesAtMaxRounds(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, func(r *DebateCreateRequest) { r.MaxRounds = 2 })

	// Round 1 -> 2, still in progress.
	if _, err := m.NextRound(ctx, d.ID, &NextRoundRequest{GeneratorOutput: "a", CriticFeedback: "b"}); err != nil {
		t.Fatalf("next round 1: %v", err)
	}
	// Round 2 hits the cap -> paused.
	paused, err := m.NextRound(ctx, d.ID, &NextRoundRequest{GeneratorOutput: "c", CriticFeedback: "d"})
	if err != nil {
		t.Fatalf("next round 2: %v", err)
	}
	if paused.Status != DebateStatusPaused {
		t.Fatalf("status = %s, want paused", paused.Status)
	}
	if len(paused.Rounds) != 2 {
		t.Fatalf("rounds = %d, want 2 (no new round after pause)", len(paused.Rounds))
	}
	// A paused debate rejects further rounds.
	if _, err := m.NextRound(ctx, d.ID, &NextRoundRequest{GeneratorOutput: "e", CriticFeedback: "f"}); err == nil {
		t.Fatal("expected error advancing a paused debate")
	}
}

func TestResolveConflictHappyPath(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)
	adv, err := m.NextRound(ctx, d.ID, &NextRoundRequest{GeneratorOutput: "x", CriticFeedback: "the logic is wrong"})
	if err != nil {
		t.Fatalf("next round: %v", err)
	}
	if len(adv.Conflicts) == 0 {
		t.Fatal("expected a conflict to resolve")
	}
	cid := adv.Conflicts[0].ID

	resolved, err := m.ResolveConflict(ctx, d.ID, cid, &ResolveConflictRequest{Resolution: "patched", ResolvedBy: "mediator"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Status != ConflictStatusResolved || resolved.ResolvedBy != "mediator" || resolved.Resolution != "patched" {
		t.Fatalf("conflict not resolved: %+v", resolved)
	}
	if resolved.ResolvedAt == 0 {
		t.Fatal("ResolvedAt not set")
	}
	// Reflected in the stored debate.
	after, _ := m.GetDebate(ctx, d.ID)
	if after.Conflicts[0].Status != ConflictStatusResolved {
		t.Fatal("resolution not persisted on debate")
	}
}

func TestResolveConflictNotFound(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)

	if _, err := m.ResolveConflict(ctx, d.ID, "nope", &ResolveConflictRequest{Resolution: "r", ResolvedBy: "x"}); err == nil {
		t.Fatal("expected error for unknown conflict")
	}
	_, err := m.ResolveConflict(ctx, "missing-debate", "c", &ResolveConflictRequest{Resolution: "r", ResolvedBy: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for missing debate, got %v", err)
	}
}

func TestProposeSolutionScore(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)

	withPros, err := m.ProposeSolution(ctx, d.ID, &ProposeSolutionRequest{
		ProposedBy: "g", Role: RoleGenerator, Title: "opt", Description: "desc",
		Pros: []string{"fast", "clean", "safe"}, Cons: []string{"complex"},
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if withPros.Score != 0.75 {
		t.Fatalf("score = %v, want 0.75", withPros.Score)
	}
	neutral, err := m.ProposeSolution(ctx, d.ID, &ProposeSolutionRequest{
		ProposedBy: "c", Role: RoleCritic, Title: "plain", Description: "desc",
	})
	if err != nil {
		t.Fatalf("propose neutral: %v", err)
	}
	if neutral.Score != 0.5 {
		t.Fatalf("neutral score = %v, want 0.5", neutral.Score)
	}
}

func TestSelectSolutionValidAndReturnsClone(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)
	sol, err := m.ProposeSolution(ctx, d.ID, &ProposeSolutionRequest{
		ProposedBy: "g", Role: RoleGenerator, Title: "opt", Description: "desc",
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}

	selected, err := m.SelectSolution(ctx, d.ID, &SelectSolutionRequest{SolutionID: sol.ID})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if selected.SelectedSolution != sol.ID {
		t.Fatalf("selected = %q, want %q", selected.SelectedSolution, sol.ID)
	}
	if selected.Status != DebateStatusResolved || selected.ResolvedAt == 0 {
		t.Fatalf("debate not resolved: %+v", selected)
	}

	// Mutating the returned debate must not corrupt internal state.
	selected.Title = "mutated"
	selected.SelectedSolution = "tampered"
	again, _ := m.GetDebate(ctx, d.ID)
	if again.Title == "mutated" || again.SelectedSolution == "tampered" {
		t.Fatal("SelectSolution must return a clone")
	}
}

func TestSelectSolutionMissing(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)

	_, err := m.SelectSolution(ctx, d.ID, &SelectSolutionRequest{SolutionID: "ghost"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for missing solution, got %v", err)
	}
	_, err = m.SelectSolution(ctx, "missing-debate", &SelectSolutionRequest{SolutionID: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for missing debate, got %v", err)
	}
}

func TestExportReportTimelineAndStats(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, func(r *DebateCreateRequest) { r.Title = "audit me" })
	adv, err := m.NextRound(ctx, d.ID, &NextRoundRequest{GeneratorOutput: "x", CriticFeedback: "the logic is wrong"})
	if err != nil {
		t.Fatalf("next round: %v", err)
	}
	if _, err := m.ResolveConflict(ctx, d.ID, adv.Conflicts[0].ID, &ResolveConflictRequest{Resolution: "r", ResolvedBy: "mediator"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := m.ProposeSolution(ctx, d.ID, &ProposeSolutionRequest{ProposedBy: "g", Role: RoleGenerator, Title: "s", Description: "d"}); err != nil {
		t.Fatalf("propose: %v", err)
	}

	rep, err := m.ExportReport(ctx, d.ID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if rep.TotalRounds != 2 {
		t.Fatalf("total rounds = %d, want 2", rep.TotalRounds)
	}
	if rep.TotalConflicts != 1 || rep.ResolvedConflicts != 1 || rep.OpenConflicts != 0 {
		t.Fatalf("conflict stats wrong: total=%d resolved=%d open=%d", rep.TotalConflicts, rep.ResolvedConflicts, rep.OpenConflicts)
	}
	if len(rep.Solutions) != 1 {
		t.Fatalf("solutions = %d, want 1", len(rep.Solutions))
	}
	if len(rep.Timeline) == 0 {
		t.Fatal("timeline empty")
	}
	for i := 1; i < len(rep.Timeline); i++ {
		if rep.Timeline[i].Timestamp < rep.Timeline[i-1].Timestamp {
			t.Fatalf("timeline not sorted ascending at %d", i)
		}
	}
	var created bool
	for _, ev := range rep.Timeline {
		if ev.Type == "debate_created" {
			created = true
		}
	}
	if !created {
		t.Fatal("timeline missing debate_created event")
	}
}

func TestListDebatesPagination(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		mustCreate(t, m, nil)
	}

	page1, err := m.ListDebates(ctx, &DebateListRequest{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page1.Total != 3 || len(page1.Debates) != 2 || !page1.HasMore {
		t.Fatalf("page1 total=%d len=%d hasMore=%v", page1.Total, len(page1.Debates), page1.HasMore)
	}
	page2, _ := m.ListDebates(ctx, &DebateListRequest{Limit: 2, Offset: 2})
	if page2.Total != 3 || len(page2.Debates) != 1 || page2.HasMore {
		t.Fatalf("page2 total=%d len=%d hasMore=%v", page2.Total, len(page2.Debates), page2.HasMore)
	}
	// Offset beyond total yields an empty page, not an error.
	beyond, _ := m.ListDebates(ctx, &DebateListRequest{Limit: 2, Offset: 10})
	if beyond.Total != 3 || len(beyond.Debates) != 0 || beyond.HasMore {
		t.Fatalf("beyond total=%d len=%d hasMore=%v", beyond.Total, len(beyond.Debates), beyond.HasMore)
	}
	// Limit 0 falls back to the default page size.
	def, _ := m.ListDebates(ctx, &DebateListRequest{Limit: 0, Offset: 0})
	if len(def.Debates) != 3 || def.HasMore {
		t.Fatalf("default limit len=%d hasMore=%v", len(def.Debates), def.HasMore)
	}
}

func TestListDebatesFilters(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	mustCreate(t, m, func(r *DebateCreateRequest) { r.FlowID = "f1"; r.StageID = "s1" })
	mustCreate(t, m, func(r *DebateCreateRequest) { r.FlowID = "f1"; r.StageID = "s2" })
	d3 := mustCreate(t, m, func(r *DebateCreateRequest) { r.FlowID = "f2"; r.StageID = "s3" })

	if got, _ := m.ListDebates(ctx, &DebateListRequest{FlowID: "f1"}); got.Total != 2 {
		t.Fatalf("flow f1 total = %d, want 2", got.Total)
	}
	if got, _ := m.ListDebates(ctx, &DebateListRequest{FlowID: "f1", StageID: "s1"}); got.Total != 1 {
		t.Fatalf("flow f1 stage s1 total = %d, want 1", got.Total)
	}
	if got, _ := m.ListDebates(ctx, &DebateListRequest{StageID: "s2"}); got.Total != 1 {
		t.Fatalf("stage s2 total = %d, want 1", got.Total)
	}

	// Resolve d3 so a status filter has something to select.
	sol, err := m.ProposeSolution(ctx, d3.ID, &ProposeSolutionRequest{ProposedBy: "g", Role: RoleGenerator, Title: "s", Description: "d"})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if _, err := m.SelectSolution(ctx, d3.ID, &SelectSolutionRequest{SolutionID: sol.ID}); err != nil {
		t.Fatalf("select: %v", err)
	}
	inProgress, _ := m.ListDebates(ctx, &DebateListRequest{Status: string(DebateStatusInProgress)})
	if inProgress.Total != 2 {
		t.Fatalf("in_progress total = %d, want 2", inProgress.Total)
	}
	resolved, _ := m.ListDebates(ctx, &DebateListRequest{Status: string(DebateStatusResolved)})
	if resolved.Total != 1 || resolved.Debates[0].ID != d3.ID {
		t.Fatalf("resolved total = %d (want 1) id match=%v", resolved.Total, resolved.Total == 1 && resolved.Debates[0].ID == d3.ID)
	}
}

func TestResolveConflictReturnsClone(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)
	adv, err := m.NextRound(ctx, d.ID, &NextRoundRequest{
		GeneratorOutput: "x", CriticFeedback: "the logic is wrong",
	})
	if err != nil {
		t.Fatalf("next round: %v", err)
	}
	cid := adv.Conflicts[0].ID
	c, err := m.ResolveConflict(ctx, d.ID, cid, &ResolveConflictRequest{
		Resolution: "fixed", ResolvedBy: "med",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	c.Resolution = "tampered"
	c.ResolvedBy = "evil"
	got, _ := m.GetDebate(ctx, d.ID)
	for _, ic := range got.Conflicts {
		if ic.ID == cid {
			if ic.Resolution == "tampered" || ic.ResolvedBy == "evil" {
				t.Fatal("ResolveConflict must return a clone")
			}
			return
		}
	}
	t.Fatal("conflict not found after resolve")
}

func TestProposeSolutionReturnsClone(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)
	sol, err := m.ProposeSolution(ctx, d.ID, &ProposeSolutionRequest{
		ProposedBy: "g", Role: RoleGenerator, Title: "opt", Description: "desc",
		Pros: []string{"a"}, Cons: []string{"b"},
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	sol.Title = "tampered"
	sol.Pros[0] = "tampered"
	got, _ := m.GetDebate(ctx, d.ID)
	if got.Solutions[0].Title == "tampered" || got.Solutions[0].Pros[0] == "tampered" {
		t.Fatal("ProposeSolution must return a clone")
	}
}

func TestCreateDebateExplicitParties(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	parties := []PartyConfig{
		{AgentID: "a1", Role: RoleGenerator, Model: "gpt-4", Channel: "ws"},
		{AgentID: "a2", Role: RoleCritic, Model: "claude-sonnet-5", Channel: "rest"},
		{AgentID: "a3", Role: "reviewer", Model: "gemini", Channel: "grpc"},
		{AgentID: "a4", Role: "tester"},
	}
	d, err := m.CreateDebate(ctx, &DebateCreateRequest{
		Title: "multi", GeneratorID: "a1", CriticID: "a2", InitialInput: "start",
		Parties: parties,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(d.Parties) != 4 {
		t.Fatalf("parties = %d, want 4", len(d.Parties))
	}
	for i, p := range d.Parties {
		if p.AgentID != parties[i].AgentID || p.Role != parties[i].Role || p.Model != parties[i].Model {
			t.Fatalf("party %d mismatch: got %+v", i, p)
		}
	}
}

func TestCreateDebateLegacySynthesizesParties(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()

	// Without mediator: 2 parties.
	d, err := m.CreateDebate(ctx, &DebateCreateRequest{
		Title: "2p", GeneratorID: "gen", CriticID: "crit", InitialInput: "go",
	})
	if err != nil {
		t.Fatalf("create 2p: %v", err)
	}
	if len(d.Parties) != 2 {
		t.Fatalf("2p parties = %d, want 2", len(d.Parties))
	}
	if d.Parties[0].AgentID != "gen" || d.Parties[0].Role != RoleGenerator {
		t.Fatalf("party 0 = %+v", d.Parties[0])
	}
	if d.Parties[1].AgentID != "crit" || d.Parties[1].Role != RoleCritic {
		t.Fatalf("party 1 = %+v", d.Parties[1])
	}

	// With mediator: 3 parties.
	d2, err := m.CreateDebate(ctx, &DebateCreateRequest{
		Title: "3p", GeneratorID: "g", CriticID: "c", MediatorID: "m", InitialInput: "go",
	})
	if err != nil {
		t.Fatalf("create 3p: %v", err)
	}
	if len(d2.Parties) != 3 {
		t.Fatalf("3p parties = %d, want 3", len(d2.Parties))
	}
	if d2.Parties[2].AgentID != "m" || d2.Parties[2].Role != RoleMediator {
		t.Fatalf("party 2 = %+v", d2.Parties[2])
	}
}

func TestNextRoundMirrorsContributions(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)
	got, err := m.NextRound(ctx, d.ID, &NextRoundRequest{
		GeneratorOutput: "code v1", CriticFeedback: "needs tests",
	})
	if err != nil {
		t.Fatalf("next round: %v", err)
	}
	r := got.Rounds[0]
	if len(r.Contributions) != 2 {
		t.Fatalf("contributions = %d, want 2", len(r.Contributions))
	}
	gen := r.Contributions[0]
	if gen.AgentID != "g" || gen.Role != RoleGenerator || gen.Content != "code v1" || gen.Timestamp == 0 {
		t.Fatalf("gen contribution = %+v", gen)
	}
	crit := r.Contributions[1]
	if crit.AgentID != "c" || crit.Role != RoleCritic || crit.Content != "needs tests" || crit.Timestamp == 0 {
		t.Fatalf("crit contribution = %+v", crit)
	}
}

func TestCloneIsolatesPartiesAndContributions(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, func(r *DebateCreateRequest) {
		r.Parties = []PartyConfig{
			{AgentID: "a", Role: RoleGenerator},
			{AgentID: "b", Role: RoleCritic},
		}
	})
	got, _ := m.GetDebate(ctx, d.ID)
	got.Parties[0].AgentID = "tampered"
	again, _ := m.GetDebate(ctx, d.ID)
	if again.Parties[0].AgentID == "tampered" {
		t.Fatal("clone must isolate Parties")
	}

	// Advance to produce contributions and verify isolation.
	adv, err := m.NextRound(ctx, d.ID, &NextRoundRequest{
		GeneratorOutput: "x", CriticFeedback: "y",
	})
	if err != nil {
		t.Fatalf("next round: %v", err)
	}
	adv.Rounds[0].Contributions[0].Content = "tampered"
	again2, _ := m.GetDebate(ctx, d.ID)
	if again2.Rounds[0].Contributions[0].Content == "tampered" {
		t.Fatal("clone must isolate Contributions")
	}
}

func TestCreateDebateRejectsOneParty(t *testing.T) {
	m := NewInMemoryDebateManager()
	_, err := m.CreateDebate(context.Background(), &DebateCreateRequest{
		Title: "bad", GeneratorID: "g", CriticID: "c", InitialInput: "go",
		Parties: []PartyConfig{{AgentID: "solo", Role: RoleGenerator}},
	})
	if err == nil {
		t.Fatal("expected error for single party")
	}
}

func TestCreateDebateRejectsDuplicateAgentIDs(t *testing.T) {
	m := NewInMemoryDebateManager()
	_, err := m.CreateDebate(context.Background(), &DebateCreateRequest{
		Title: "dup", GeneratorID: "g", CriticID: "c", InitialInput: "go",
		Parties: []PartyConfig{
			{AgentID: "same", Role: RoleGenerator},
			{AgentID: "same", Role: RoleCritic},
		},
	})
	if err == nil {
		t.Fatal("expected error for duplicate agent_id")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestCreateDebateRejectsEmptyAgentID(t *testing.T) {
	m := NewInMemoryDebateManager()
	_, err := m.CreateDebate(context.Background(), &DebateCreateRequest{
		Title: "empty", GeneratorID: "g", CriticID: "c", InitialInput: "go",
		Parties: []PartyConfig{
			{AgentID: "ok", Role: RoleGenerator},
			{AgentID: "", Role: RoleCritic},
		},
	})
	if err == nil {
		t.Fatal("expected error for empty agent_id")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestAppendContributionHappyPath(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, func(r *DebateCreateRequest) {
		r.Parties = []PartyConfig{
			{AgentID: "a1", Role: RoleGenerator},
			{AgentID: "a2", Role: RoleCritic},
			{AgentID: "a3", Role: "reviewer"},
		}
	})
	got, err := m.AppendContribution(ctx, d.ID, PartyContribution{
		AgentID: "a3", Role: "reviewer", Content: "looks good",
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	r := got.Rounds[0]
	if len(r.Contributions) != 1 {
		t.Fatalf("contributions = %d, want 1", len(r.Contributions))
	}
	if r.Contributions[0].Content != "looks good" || r.Contributions[0].AgentID != "a3" {
		t.Fatalf("contribution mismatch: %+v", r.Contributions[0])
	}
	if r.Contributions[0].Timestamp == 0 {
		t.Fatal("timestamp not set")
	}
}

func TestAppendContributionRejectsUnknownParty(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)
	_, err := m.AppendContribution(ctx, d.ID, PartyContribution{
		AgentID: "stranger", Role: "hacker", Content: "hi",
	})
	if err == nil {
		t.Fatal("expected error for unknown party")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}

func TestAppendContributionRejectsNotInProgress(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, func(r *DebateCreateRequest) { r.MaxRounds = 1 })
	// Exhaust rounds to pause the debate.
	if _, err := m.NextRound(ctx, d.ID, &NextRoundRequest{
		GeneratorOutput: "x", CriticFeedback: "y",
	}); err != nil {
		t.Fatalf("next round: %v", err)
	}
	_, err := m.AppendContribution(ctx, d.ID, PartyContribution{
		AgentID: "g", Role: RoleGenerator, Content: "late",
	})
	if err == nil {
		t.Fatal("expected error for paused debate")
	}
}

func TestAppendContributionRejectsEmptyContent(t *testing.T) {
	m := NewInMemoryDebateManager()
	ctx := context.Background()
	d := mustCreate(t, m, nil)
	_, err := m.AppendContribution(ctx, d.ID, PartyContribution{
		AgentID: "g", Role: RoleGenerator, Content: "",
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("want ErrInvalidRequest, got %v", err)
	}
}
