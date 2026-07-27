package debate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteDebateManagerPersistenceRoundTrip(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "debates.db")

	m, err := NewSQLiteDebateManager(dbPath)
	require.NoError(t, err)

	created, err := m.CreateDebate(ctx, &DebateCreateRequest{
		Title: "durable", GeneratorID: "g", CriticID: "c", InitialInput: "start",
		FlowID: "flow-1", StageID: "stage-1", MaxRounds: 5,
		Parties: []PartyConfig{
			{AgentID: "g", Role: RoleGenerator, Model: "gpt-4"},
			{AgentID: "c", Role: RoleCritic, Model: "claude"},
			{AgentID: "r", Role: "reviewer", Channel: "ws"},
		},
	})
	require.NoError(t, err)

	advanced, err := m.NextRound(ctx, created.ID, &NextRoundRequest{
		GeneratorOutput: "v1",
		CriticFeedback:  "the logic is wrong and there is a security vulnerability",
	})
	require.NoError(t, err)
	require.Len(t, advanced.Conflicts, 2)
	conflictID := advanced.Conflicts[0].ID

	_, err = m.ResolveConflict(ctx, created.ID, conflictID, &ResolveConflictRequest{
		Resolution: "patched", ResolvedBy: "mediator",
	})
	require.NoError(t, err)

	sol, err := m.ProposeSolution(ctx, created.ID, &ProposeSolutionRequest{
		ProposedBy: "g", Role: RoleGenerator, Title: "fix", Description: "apply patch",
		Pros: []string{"safe", "clean"}, Cons: []string{"slower"},
	})
	require.NoError(t, err)

	_, err = m.SelectSolution(ctx, created.ID, &SelectSolutionRequest{SolutionID: sol.ID})
	require.NoError(t, err)

	require.NoError(t, m.Close())

	// Reopen from the same file: state must survive.
	m2, err := NewSQLiteDebateManager(dbPath)
	require.NoError(t, err)
	defer m2.Close()

	got, err := m2.GetDebate(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "durable", got.Title)
	assert.Equal(t, "flow-1", got.FlowID)
	assert.Equal(t, "stage-1", got.StageID)
	assert.Equal(t, DebateStatusResolved, got.Status)
	assert.Equal(t, sol.ID, got.SelectedSolution)

	require.Len(t, got.Rounds, 2)
	assert.Equal(t, "v1", got.Rounds[0].GeneratorOutput)
	assert.Equal(t, "the logic is wrong and there is a security vulnerability", got.Rounds[0].CriticFeedback)

	require.Len(t, got.Conflicts, 2)
	var resolved *Conflict
	for _, c := range got.Conflicts {
		if c.ID == conflictID {
			resolved = c
		}
	}
	require.NotNil(t, resolved)
	assert.Equal(t, ConflictStatusResolved, resolved.Status)
	assert.Equal(t, "mediator", resolved.ResolvedBy)

	require.Len(t, got.Solutions, 1)
	assert.Equal(t, sol.ID, got.Solutions[0].ID)
	assert.Equal(t, 2.0/3.0, got.Solutions[0].Score)

	// Multi-party fields survive the round-trip.
	require.Len(t, got.Parties, 3)
	assert.Equal(t, "gpt-4", got.Parties[0].Model)
	assert.Equal(t, AgentRole("reviewer"), got.Parties[2].Role)
	assert.Equal(t, "ws", got.Parties[2].Channel)
	require.NotEmpty(t, got.Rounds[0].Contributions)
	assert.Equal(t, "v1", got.Rounds[0].Contributions[0].Content)

	list, err := m2.ListDebates(ctx, &DebateListRequest{FlowID: "flow-1"})
	require.NoError(t, err)
	assert.Equal(t, 1, list.Total)
}

func TestSQLiteDebateStoreCRUD(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "crud.db")
	store, err := openSQLiteDebateStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	d := &Debate{
		ID: "d1", Title: "one", Status: DebateStatusInProgress,
		FlowID: "f1", StageID: "s1", CreatedAt: 100, UpdatedAt: 100,
	}
	require.NoError(t, store.put(d))

	loaded, err := store.loadAll()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "one", loaded[0].Title)
	assert.Equal(t, DebateStatusInProgress, loaded[0].Status)

	// Upsert updates the existing row rather than inserting a duplicate.
	d.Status = DebateStatusResolved
	d.UpdatedAt = 200
	require.NoError(t, store.put(d))
	loaded, err = store.loadAll()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, DebateStatusResolved, loaded[0].Status)

	require.NoError(t, store.delete("d1"))
	loaded, err = store.loadAll()
	require.NoError(t, err)
	assert.Empty(t, loaded)

	// An id-less document is rejected.
	assert.Error(t, store.put(&Debate{}))
}

func TestSQLiteDebateStoreSkipsCorruptedPayload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "corrupt.db")
	store, err := openSQLiteDebateStore(dbPath)
	require.NoError(t, err)
	defer store.Close()

	require.NoError(t, store.put(&Debate{
		ID: "good", Title: "ok", Status: DebateStatusInProgress, CreatedAt: 1, UpdatedAt: 1,
	}))

	// Inject a row whose payload is not valid JSON.
	_, err = store.db.Exec(
		`INSERT INTO debates (id, status, flow_id, stage_id, created_at, updated_at, payload) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"bad", "in_progress", "", "", 0, 0, "{not valid json",
	)
	require.NoError(t, err)

	loaded, err := store.loadAll()
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "good", loaded[0].ID)
}

func TestNewSQLiteDebateManagerCloseNilSafe(t *testing.T) {
	var m *SQLiteDebateManager
	assert.NoError(t, m.Close())
}

func TestSQLiteDebateManagerAppendContributionPersists(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "contrib.db")

	m, err := NewSQLiteDebateManager(dbPath)
	require.NoError(t, err)

	d, err := m.CreateDebate(ctx, &DebateCreateRequest{
		Title: "mp", GeneratorID: "g", CriticID: "c", InitialInput: "go",
		Parties: []PartyConfig{
			{AgentID: "g", Role: RoleGenerator},
			{AgentID: "c", Role: RoleCritic},
			{AgentID: "r", Role: "reviewer"},
		},
	})
	require.NoError(t, err)

	_, err = m.AppendContribution(ctx, d.ID, PartyContribution{
		AgentID: "r", Role: "reviewer", Content: "LGTM",
	})
	require.NoError(t, err)
	require.NoError(t, m.Close())

	m2, err := NewSQLiteDebateManager(dbPath)
	require.NoError(t, err)
	defer m2.Close()

	got, err := m2.GetDebate(ctx, d.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Rounds[0].Contributions, 1)
	assert.Equal(t, "LGTM", got.Rounds[0].Contributions[0].Content)
	assert.Equal(t, "r", got.Rounds[0].Contributions[0].AgentID)
}
