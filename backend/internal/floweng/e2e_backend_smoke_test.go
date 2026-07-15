package floweng_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeflow/backend/internal/debate"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	"github.com/codeflow/backend/internal/skill"
	"github.com/codeflow/backend/internal/workspace"
)

// Smoke: flow + debate FK + guarded workspace write + skill match (no HTTP).
func TestBackendCoreSmoke(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// floweng
	eng := floweng.NewInMemoryEngine(nil)
	flow, err := eng.Create(ctx, &floweng.CreateFlowRequest{ProjectID: "smoke", TemplateID: floweng.TemplateNewProject})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Advance(ctx, flow.ID, &floweng.AdvanceRequest{}); err != nil {
		t.Fatal(err)
	}
	flow, _ = eng.Get(ctx, flow.ID)
	design := flow.Stages[1]
	if design.Type != floweng.StageTypeDesign || design.Status != floweng.StageStatusActive {
		t.Fatalf("expected design active, got %+v", design)
	}
	if _, err := eng.AttachArtifact(ctx, flow.ID, design.ID, "design.md", "file:design.md"); err != nil {
		t.Fatal(err)
	}

	// debate bound to stage
	dm := debate.NewInMemoryDebateManager()
	d, err := dm.CreateDebate(ctx, &debate.DebateCreateRequest{
		Title: "smoke", GeneratorID: "g", CriticID: "c", InitialInput: "x",
		FlowID: flow.ID, StageID: design.ID,
	})
	if err != nil || d.StageID != design.ID {
		t.Fatalf("debate: %+v err=%v", d, err)
	}

	// workspace + guard
	g := guard.NewEngine(nil, nil)
	ws := workspace.NewFSService(g)
	if _, err := ws.Write(ctx, &workspace.WriteRequest{
		Root: root, Path: "src/ok.go", Content: []byte("package src\n\nfunc Ok() {}\n"), CreateParents: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Write(ctx, &workspace.WriteRequest{
		Root: root, Path: "src/bad_v2.go", Content: []byte("package src\n"),
	}); err == nil {
		t.Fatal("expected stacked naming block")
	}

	// skill
	reg := skill.NewInMemoryRegistry()
	matches, err := reg.Match(ctx, &skill.MatchRequest{Text: "please write a unit test", StageType: "coding"})
	if err != nil || len(matches) == 0 {
		t.Fatalf("skill match: %v %d", err, len(matches))
	}

	// ensure files on disk
	if _, err := os.Stat(filepath.Join(root, "src", "ok.go")); err != nil {
		t.Fatal(err)
	}
}
