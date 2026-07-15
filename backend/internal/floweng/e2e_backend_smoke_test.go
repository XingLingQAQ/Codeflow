package floweng_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	// floweng + template describe
	if info, ok := floweng.DescribeTemplate(floweng.TemplateNewProject); !ok || len(info.Stages) == 0 {
		t.Fatalf("describe template: ok=%v info=%+v", ok, info)
	}
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

	// debate bound to stage + list by flow_id
	dm := debate.NewInMemoryDebateManager()
	d, err := dm.CreateDebate(ctx, &debate.DebateCreateRequest{
		Title: "smoke", GeneratorID: "g", CriticID: "c", InitialInput: "x",
		FlowID: flow.ID, StageID: design.ID,
	})
	if err != nil || d.StageID != design.ID {
		t.Fatalf("debate: %+v err=%v", d, err)
	}
	listed, err := dm.ListDebates(ctx, &debate.DebateListRequest{FlowID: flow.ID})
	if err != nil || listed.Total != 1 {
		t.Fatalf("list debates by flow: %+v err=%v", listed, err)
	}

	// workspace + guard + staging promote + exemption
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
	// stage a clean file, list, promote, confirm staging empty
	if _, err := ws.Write(ctx, &workspace.WriteRequest{
		Root: root, Path: "src/staged.go", Content: []byte("package src\n\nfunc Staged() {}\n"),
		CreateParents: true, Mode: workspace.WriteModeStage,
	}); err != nil {
		t.Fatal(err)
	}
	staged, err := ws.ListStaged(ctx, root)
	if err != nil || len(staged) != 1 {
		t.Fatalf("list staged: %+v err=%v", staged, err)
	}
	if _, err := ws.Promote(ctx, root, "src/staged.go"); err != nil {
		t.Fatal(err)
	}
	if remaining, err := ws.ListStaged(ctx, root); err != nil || len(remaining) != 0 {
		t.Fatalf("staging should be empty after promote: %+v err=%v", remaining, err)
	}
	// promote-all / discard-all bulk path
	if _, err := ws.Write(ctx, &workspace.WriteRequest{
		Root: root, Path: "bulk/a.txt", Content: []byte("a"), CreateParents: true, Mode: workspace.WriteModeStage,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Write(ctx, &workspace.WriteRequest{
		Root: root, Path: "bulk/b.txt", Content: []byte("b"), CreateParents: true, Mode: workspace.WriteModeStage,
	}); err != nil {
		t.Fatal(err)
	}
	if items, err := ws.PromoteAll(ctx, root); err != nil || len(items) != 2 {
		t.Fatalf("promote all: %d %v", len(items), err)
	}
	if _, err := ws.Write(ctx, &workspace.WriteRequest{
		Root: root, Path: "bulk/c.txt", Content: []byte("c"), CreateParents: true, Mode: workspace.WriteModeStage,
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := ws.DiscardAllStaged(ctx, root); err != nil || n != 1 {
		t.Fatalf("discard all: n=%d err=%v", n, err)
	}
	// temporary exemption for stacked name
	blocked := filepath.Join(root, "src", "utils2.go")
	g.GrantExemption(guard.Exemption{
		Path: blocked, Rules: []guard.RuleID{guard.RuleStackedNaming},
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if _, err := ws.Write(ctx, &workspace.WriteRequest{
		Root: root, Path: "src/utils2.go", Content: []byte("package src\n"),
	}); err != nil {
		t.Fatalf("exempt write should pass: %v", err)
	}
	if len(g.ListExemptions()) == 0 {
		t.Fatal("expected active exemption")
	}

	// skill
	reg := skill.NewInMemoryRegistry()
	matches, err := reg.Match(ctx, &skill.MatchRequest{Text: "please write a unit test", StageType: "coding"})
	if err != nil || len(matches) == 0 {
		t.Fatalf("skill match: %v %d", err, len(matches))
	}
	filtered, err := reg.ListFiltered(ctx, "coding", true)
	if err != nil || len(filtered) == 0 {
		t.Fatalf("skill list filtered: %v %d", err, len(filtered))
	}

	// ensure files on disk
	if _, err := os.Stat(filepath.Join(root, "src", "ok.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "staged.go")); err != nil {
		t.Fatal(err)
	}
}
