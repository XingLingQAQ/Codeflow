package floweng

// Tests for the engine half of T3.01.a group 2: one active project flow per
// project, task flows without a limit, the parent link of a task flow, and
// Resume. Every store-backed case runs twice, once on memoryStore and once on
// SQLiteFlowStore, because the rule is enforced in three places (the engine, the
// memory store's map, and the database's partial unique index) and all three
// have to agree.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// storePair runs a subtest per store implementation.
func storePair(t *testing.T, run func(t *testing.T, store FlowStore)) {
	t.Helper()
	for name, store := range map[string]FlowStore{
		"memory": newMemoryStore(),
		"sqlite": func() FlowStore {
			s, _ := newUpgradeTestStore(t)
			return s
		}(),
	} {
		t.Run(name, func(t *testing.T) { run(t, store) })
	}
}

// countStoredFlows counts the rows/documents a store holds for a project.
func countStoredFlows(t *testing.T, store FlowStore, projectID string) int {
	t.Helper()
	items, err := store.List(projectID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return len(items)
}

// TestCreateKeepsOneActiveProjectFlow pins the rule: the first project flow of a
// project is created, the second is refused with ErrActiveProjectFlowExists and
// writes nothing, task flows are not limited, another project is unaffected, and
// a project whose flow was aborted can have a new one.
func TestCreateKeepsOneActiveProjectFlow(t *testing.T) {
	storePair(t, func(t *testing.T, store FlowStore) {
		eng := NewEngineWithStore(store, nil)
		ctx := context.Background()

		first, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "p1"})
		if err != nil {
			t.Fatalf("create the first project flow: %v", err)
		}
		if first.Kind != FlowKindProject {
			t.Fatalf("the first flow is kind %q, want project", first.Kind)
		}

		_, err = eng.Create(ctx, &CreateFlowRequest{ProjectID: "p1"})
		if !errors.Is(err, ErrActiveProjectFlowExists) {
			t.Fatalf("the second project flow was not refused by the rule: %v", err)
		}
		if !strings.Contains(err.Error(), first.ID) {
			t.Fatalf("the refusal does not name the flow holding the slot: %v", err)
		}
		if got := countStoredFlows(t, store, "p1"); got != 1 {
			t.Fatalf("p1 holds %d flows after the refused create, want 1", got)
		}

		// Task flows are not limited, and they carry their kind.
		for i := 0; i < 3; i++ {
			task, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "p1", Kind: FlowKindTask})
			if err != nil {
				t.Fatalf("create task flow %d: %v", i, err)
			}
			if task.Kind != FlowKindTask {
				t.Fatalf("task flow %d is kind %q", i, task.Kind)
			}
		}
		if got := countStoredFlows(t, store, "p1"); got != 4 {
			t.Fatalf("p1 holds %d flows, want 4 (1 project + 3 task)", got)
		}

		// Another project has its own slot.
		if _, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "p2"}); err != nil {
			t.Fatalf("another project was refused its first project flow: %v", err)
		}

		// Aborting the flow of record frees the slot.
		if _, err := eng.Abort(ctx, first.ID, "start over"); err != nil {
			t.Fatalf("abort: %v", err)
		}
		replacement, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "p1"})
		if err != nil {
			t.Fatalf("create after the slot was freed: %v", err)
		}
		if replacement.ID == first.ID {
			t.Fatal("the replacement reuses the aborted flow")
		}
	})
}

// TestStoreRefusesASecondActiveProjectFlow is the backstop on its own: a write
// that goes straight to the store, with no engine in front of it, still cannot
// leave a project with two active project flows.
func TestStoreRefusesASecondActiveProjectFlow(t *testing.T) {
	storePair(t, func(t *testing.T, store FlowStore) {
		first := testLegacyFlow("store-first", "proj-store", testLegacyEvent("store-first-ev", "flow.created", "", "created"))
		if err := store.Put(first); err != nil {
			t.Fatalf("put the first active project flow: %v", err)
		}
		second := testLegacyFlow("store-second", "proj-store", testLegacyEvent("store-second-ev", "flow.created", "", "created"))
		err := store.Put(second)
		if !errors.Is(err, ErrActiveProjectFlowExists) {
			t.Fatalf("the store accepted a second active project flow: %v", err)
		}
		if !strings.Contains(err.Error(), "store-first") {
			t.Fatalf("the refusal does not name the flow holding the slot: %v", err)
		}
		if got := countStoredFlows(t, store, "proj-store"); got != 1 {
			t.Fatalf("the store holds %d flows, want only the first", got)
		}

		// The shapes the rule does not constrain.
		allowed := []*Flow{
			testLegacyFlow("store-task-a", "proj-store", testLegacyEvent("store-task-a-ev", "flow.created", "", "created")),
			testLegacyFlow("store-task-b", "proj-store", testLegacyEvent("store-task-b-ev", "flow.created", "", "created")),
		}
		for _, f := range allowed {
			f.Kind = FlowKindTask
			if err := store.Put(f); err != nil {
				t.Fatalf("put task flow %s: %v", f.ID, err)
			}
		}
		suspended := testLegacyFlow("store-suspended", "proj-store", testLegacyEvent("store-suspended-ev", "flow.created", "", "created"))
		suspended.Status = FlowStatusSuspended
		if err := store.Put(suspended); err != nil {
			t.Fatalf("put a suspended project flow: %v", err)
		}
		terminal := testLegacyFlow("store-completed", "proj-store", testLegacyEvent("store-completed-ev", "flow.created", "", "created"))
		terminal.Status = FlowStatusCompleted
		if err := store.Put(terminal); err != nil {
			t.Fatalf("put a completed project flow: %v", err)
		}
		otherProject := testLegacyFlow("store-other", "proj-other", testLegacyEvent("store-other-ev", "flow.created", "", "created"))
		if err := store.Put(otherProject); err != nil {
			t.Fatalf("put an active project flow of another project: %v", err)
		}

		// A flow that is already stored stays writable: the rule is about a
		// second active project flow, not about updating the one there is.
		first.UpdatedAt = first.UpdatedAt.Add(time.Second)
		if err := store.Put(first); err != nil {
			t.Fatalf("updating the flow of record was refused: %v", err)
		}
	})
}

// TestCreateValidatesParentProjectFlowID pins the parent link: a task flow may
// name a project flow of its own project, and any other parent is refused before
// anything is written.
func TestCreateValidatesParentProjectFlowID(t *testing.T) {
	eng := NewInMemoryEngine(nil)
	ctx := context.Background()

	projectFlow, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	otherFlow, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "p2"})
	if err != nil {
		t.Fatal(err)
	}

	task, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "p1", Kind: FlowKindTask, ParentProjectFlowID: projectFlow.ID})
	if err != nil {
		t.Fatalf("a task flow with its project's flow as parent was refused: %v", err)
	}
	if task.ParentProjectFlowID != projectFlow.ID {
		t.Fatalf("the parent link was not kept: %q", task.ParentProjectFlowID)
	}

	before := countStoredFlows(t, eng.store, "p1")
	for name, req := range map[string]*CreateFlowRequest{
		"an unknown parent":            {ProjectID: "p1", Kind: FlowKindTask, ParentProjectFlowID: "no-such-flow"},
		"a task flow as parent":        {ProjectID: "p1", Kind: FlowKindTask, ParentProjectFlowID: task.ID},
		"another project's flow":       {ProjectID: "p1", Kind: FlowKindTask, ParentProjectFlowID: otherFlow.ID},
		"a task flow without a parent": {ProjectID: "p1", Kind: FlowKindTask, ParentProjectFlowID: ""},
	} {
		if name == "a task flow without a parent" {
			// No parent is not an error: a task flow without one is allowed
			// here (the runtime database's foreign key is T3.01.b).
			if _, err := eng.Create(ctx, req); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			before++
			continue
		}
		_, err := eng.Create(ctx, req)
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if !strings.Contains(err.Error(), "parent_project_flow_id") {
			t.Fatalf("%s: the error does not name the field: %v", name, err)
		}
	}
	if got := countStoredFlows(t, eng.store, "p1"); got != before {
		t.Fatalf("p1 holds %d flows, want %d: a refused create wrote one", got, before)
	}
}

// TestConcurrentCreateOfProjectFlowsKeepsOne is the race the rule has to win:
// two callers ask for the same project's flow of record at the same time, and
// exactly one of them gets it.
func TestConcurrentCreateOfProjectFlowsKeepsOne(t *testing.T) {
	storePair(t, func(t *testing.T, store FlowStore) {
		eng := NewEngineWithStore(store, nil)
		ctx := context.Background()

		const n = 8
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			created []*Flow
			refused []error
			other   []error
		)
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				flow, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "proj-race"})
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					created = append(created, flow)
				case errors.Is(err, ErrActiveProjectFlowExists):
					refused = append(refused, err)
				default:
					other = append(other, err)
				}
			}()
		}
		close(start)
		wg.Wait()

		if len(other) != 0 {
			t.Fatalf("concurrent creates failed for another reason: %v", other)
		}
		if len(created) != 1 || len(refused) != n-1 {
			t.Fatalf("created=%d refused=%d, want exactly 1 created", len(created), len(refused))
		}
		if got := countStoredFlows(t, store, "proj-race"); got != 1 {
			t.Fatalf("the store holds %d flows, want 1", got)
		}
		active, err := eng.ListByStatus(ctx, "proj-race", FlowStatusActive)
		if err != nil {
			t.Fatal(err)
		}
		if len(active) != 1 || active[0].ID != created[0].ID {
			t.Fatalf("active project flows = %+v, want only %s", active, created[0].ID)
		}
		if !strings.Contains(refused[0].Error(), created[0].ID) {
			t.Fatalf("the refusals do not name the winner: %v", refused[0])
		}
	})
}

// newSuspendedProjectFlow stores a suspended project flow directly, the way a
// database written before this build holds one: it is a complete document with
// stages, a gate and an artifact, and no flow.suspended event (the migration
// adds that; a test fixture does not need it).
func newSuspendedProjectFlow(t *testing.T, store FlowStore, projectID string) *Flow {
	t.Helper()
	now := time.Now().UTC()
	flow := &Flow{
		ID:         uuid.NewString(),
		ProjectID:  projectID,
		TemplateID: TemplateNewProject,
		Status:     FlowStatusSuspended,
		Kind:       FlowKindProject,
		Stages: []Stage{
			{ID: uuid.NewString(), Type: StageTypeIdea, Name: "想法", Canvas: "intent", Status: StageStatusActive, Order: 0,
				Gates: []Gate{{ID: "suspended-gate", Phase: GatePhaseExit, Kind: GateKindHumanApproval}}},
			{ID: uuid.NewString(), Type: StageTypeDesign, Name: "设计", Canvas: "design_board", Status: StageStatusPending, Order: 1},
		},
		Artifacts: []Artifact{{ID: uuid.NewString(), StageID: "stage-none", Type: "intent_doc", Version: 1,
			Status: ArtifactStatusDraft, CreatedAt: now}},
		Events:    []FlowEvent{{ID: uuid.NewString(), Type: "flow.created", Message: "created", Timestamp: now}},
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	if err := store.Put(flow); err != nil {
		t.Fatalf("store the suspended flow: %v", err)
	}
	return flow
}

// TestResumeTakesTheProjectSlotBack is the Resume rule: only a suspended flow
// can be resumed, and only while the project has no active project flow.
func TestResumeTakesTheProjectSlotBack(t *testing.T) {
	storePair(t, func(t *testing.T, store FlowStore) {
		eng := NewEngineWithStore(store, nil)
		ctx := context.Background()

		primary, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "proj-r"})
		if err != nil {
			t.Fatal(err)
		}
		parked := newSuspendedProjectFlow(t, store, "proj-r")

		t.Run("while the slot is taken the refusal names the flow that holds it", func(t *testing.T) {
			_, err := eng.Resume(ctx, parked.ID)
			if !errors.Is(err, ErrActiveProjectFlowExists) {
				t.Fatalf("resume was not refused by the rule: %v", err)
			}
			if !strings.Contains(err.Error(), primary.ID) {
				t.Fatalf("the refusal does not name the holder: %v", err)
			}
			loaded, err := eng.Get(ctx, parked.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Status != FlowStatusSuspended {
				t.Fatalf("the refused resume changed the status to %s", loaded.Status)
			}
		})

		t.Run("a flow that is not suspended cannot be resumed", func(t *testing.T) {
			_, err := eng.Resume(ctx, primary.ID)
			if !errors.Is(err, ErrFlowNotSuspended) {
				t.Fatalf("resuming an active flow: %v", err)
			}
			if _, err := eng.Resume(ctx, "no-such-flow"); err == nil {
				t.Fatal("resuming a flow that does not exist was accepted")
			}
			if _, err := eng.Abort(ctx, primary.ID, "free the slot"); err != nil {
				t.Fatal(err)
			}
			if _, err := eng.Resume(ctx, primary.ID); !errors.Is(err, ErrFlowNotSuspended) {
				t.Fatalf("resuming an aborted flow: %v", err)
			}
		})

		t.Run("resuming after the slot was freed activates the flow and says so", func(t *testing.T) {
			before, err := eng.Get(ctx, parked.ID)
			if err != nil {
				t.Fatal(err)
			}
			resumed, err := eng.Resume(ctx, parked.ID)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if resumed.Status != FlowStatusActive {
				t.Fatalf("resumed status = %s", resumed.Status)
			}
			if resumed.Revision != before.Revision+1 {
				t.Fatalf("revision = %d, want %d", resumed.Revision, before.Revision+1)
			}
			if countEventType(resumed.Events, "flow.resumed") != 1 {
				t.Fatalf("events = %v, want one flow.resumed", flowEventTypes(resumed.Events))
			}
			// The stage it was parked on is the stage it resumes on: resume
			// moves the status, nothing else.
			if len(resumed.Stages) != len(before.Stages) ||
				resumed.Stages[0].Status != before.Stages[0].Status ||
				resumed.Stages[0].ID != before.Stages[0].ID {
				t.Fatalf("resume changed the stages: %+v", resumed.Stages)
			}
			if len(resumed.Artifacts) != len(before.Artifacts) {
				t.Fatalf("resume changed the artifacts: %+v", resumed.Artifacts)
			}

			// The project now has exactly one active project flow again, and it
			// is this one: a new project flow would be refused.
			if _, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "proj-r"}); !errors.Is(err, ErrActiveProjectFlowExists) {
				t.Fatalf("a new project flow was accepted while one was resumed: %v", err)
			}
			// And a second resume of the same flow is refused.
			if _, err := eng.Resume(ctx, parked.ID); !errors.Is(err, ErrFlowNotSuspended) {
				t.Fatalf("resuming an active flow twice: %v", err)
			}
		})
	})
}

// TestResumeRefusalDoesNotPersistWaitOnDocNotifier is the guard's difference
// between memoryStore and SQLiteFlowStore, made observable: the engine makes the
// resume decision *before* it appends the flow.resumed event, so an unguarded
// Resume would announce a resume the store then refuses. SQLiteFlowStore hides
// that (its Put refuses the write and the engine returns the error, so the
// announcement is the only symptom), while memoryStore accepts the write and
// leaves the project with two active project flows. The notifier records the
// announcement, and the final state records whether the rule survived.
func TestResumeRefusalDoesNotPersistWaitOnDocNotifier(t *testing.T) {
	storePair(t, func(t *testing.T, store FlowStore) {
		eng := NewEngineWithStore(store, nil)
		ctx := context.Background()
		primary, err := eng.Create(ctx, &CreateFlowRequest{ProjectID: "proj-notify"})
		if err != nil {
			t.Fatal(err)
		}
		parked := newSuspendedProjectFlow(t, store, "proj-notify")

		cap := &captureNotifier{}
		eng.SetEventNotifier(cap)
		if _, err := eng.Resume(ctx, parked.ID); !errors.Is(err, ErrActiveProjectFlowExists) {
			t.Fatalf("resume while the slot was taken: %v", err)
		}
		for _, ev := range cap.events {
			if ev.Type == "flow.resumed" {
				t.Fatalf("the refused resume was announced as an event: %+v", ev)
			}
		}
		active, err := eng.ListByStatus(ctx, "proj-notify", FlowStatusActive)
		if err != nil {
			t.Fatal(err)
		}
		if len(active) != 1 || active[0].ID != primary.ID {
			t.Fatalf("active flows after the refused resume = %+v, want only %s", active, primary.ID)
		}
	})
}

// TestSuspendedFlowRefusesTransitionsAndAcceptsAbort is the stage machine's half
// of the status: a parked flow drives nothing, and the one operation it does
// accept is the one that discards it.
func TestSuspendedFlowRefusesTransitionsAndAcceptsAbort(t *testing.T) {
	storePair(t, func(t *testing.T, store FlowStore) {
		eng := NewEngineWithStore(store, nil)
		ctx := context.Background()
		parked := newSuspendedProjectFlow(t, store, "proj-t")

		approved := true
		stageID := parked.Stages[0].ID
		for name, call := range map[string]func() error{
			"Advance": func() error {
				_, err := eng.Advance(ctx, parked.ID, &AdvanceRequest{})
				return err
			},
			"Skip": func() error {
				_, err := eng.Skip(ctx, parked.ID, &SkipRequest{StageID: stageID})
				return err
			},
			"Loop": func() error {
				_, err := eng.Loop(ctx, parked.ID, &LoopRequest{FromStageID: parked.Stages[1].ID, ToStageID: stageID})
				return err
			},
			"DecideGate": func() error {
				_, err := eng.DecideGate(ctx, parked.ID, "suspended-gate", &GateDecisionRequest{Approved: &approved})
				return err
			},
			"AttachArtifact": func() error {
				_, err := eng.AttachArtifact(ctx, parked.ID, stageID, "design_doc", "ref://x")
				return err
			},
			"SetArtifactStatus": func() error {
				_, err := eng.SetArtifactStatus(ctx, parked.ID, parked.Artifacts[0].ID, ArtifactStatusApproved)
				return err
			},
		} {
			err := call()
			if err == nil {
				t.Fatalf("%s was accepted on a suspended flow", name)
			}
			if !strings.Contains(err.Error(), string(FlowStatusSuspended)) {
				t.Fatalf("%s on a suspended flow says %q, want it to name the status", name, err)
			}
		}
		// The content-ref idempotence path must not slip past the guard: a
		// retried attach is refused just like the first attempt, and it adds
		// nothing to the frozen document.
		for attempt := 1; attempt <= 2; attempt++ {
			if _, err := eng.AttachArtifact(ctx, parked.ID, stageID, "intent_doc", "ref://retry"); err == nil {
				t.Fatalf("attach attempt %d was accepted on a suspended flow", attempt)
			}
		}
		unchanged, err := eng.Get(ctx, parked.ID)
		if err != nil {
			t.Fatal(err)
		}
		if unchanged.Revision != parked.Revision {
			t.Fatalf("a refused transition wrote the document: revision %d -> %d", parked.Revision, unchanged.Revision)
		}

		aborted, err := eng.Abort(ctx, parked.ID, "no longer wanted")
		if err != nil {
			t.Fatalf("abort a suspended flow: %v", err)
		}
		if aborted.Status != FlowStatusAborted || !flowHasEvent(aborted.Events, "flow.aborted") {
			t.Fatalf("aborted flow = status %s events %v", aborted.Status, flowEventTypes(aborted.Events))
		}
	})
}
