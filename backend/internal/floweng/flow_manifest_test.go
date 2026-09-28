package floweng

// Tests for FlowManifest, the checklist T3.01.b copies a legacy Flow by: the
// manifest of a document must name every id and every position, must not depend
// on how the document was built, and must be usable as the check that a copy is
// faithful.

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestFlowManifestListsEveryIdentityAndPosition builds a document whose parts
// would collide if the manifest confused them — two stages, a gate on each, two
// artifacts, two events, and two loop edges — and pins the lists.
func TestFlowManifestListsEveryIdentityAndPosition(t *testing.T) {
	f := &Flow{
		ID:        "flow-manifest",
		ProjectID: "proj-manifest",
		Stages: []Stage{
			{
				ID: "stage-a", Order: 0,
				Gates: []Gate{{ID: "gate-a1"}, {ID: "gate-a2"}},
			},
			{
				ID: "stage-b", Order: 1,
				Gates: []Gate{{ID: "gate-b1"}},
			},
		},
		Artifacts: []Artifact{
			{ID: "art-1", StageID: "stage-a"},
			{ID: "art-2", StageID: "stage-b"},
		},
		Events: []FlowEvent{
			{ID: "ev-1"},
			{ID: "ev-2"},
			{ID: "ev-3"},
		},
		Loops: []LoopEdge{
			{From: StageTypeReview, To: StageTypeCoding},
			{From: StageTypeCoding, To: StageTypeDesign},
		},
	}

	got := FlowManifest(f)
	want := FlowDocumentManifest{
		FlowID:    "flow-manifest",
		ProjectID: "proj-manifest",
		Stages: []StageManifest{
			{ID: "stage-a", Index: 0, Order: 0},
			{ID: "stage-b", Index: 1, Order: 1},
		},
		Gates: []GateManifest{
			{ID: "gate-a1", StageID: "stage-a", StageIndex: 0, Index: 0},
			{ID: "gate-a2", StageID: "stage-a", StageIndex: 0, Index: 1},
			{ID: "gate-b1", StageID: "stage-b", StageIndex: 1, Index: 0},
		},
		Artifacts: []ArtifactManifest{
			{ID: "art-1", StageID: "stage-a", Index: 0},
			{ID: "art-2", StageID: "stage-b", Index: 1},
		},
		Events: []EventManifest{
			{ID: "ev-1", Index: 0},
			{ID: "ev-2", Index: 1},
			{ID: "ev-3", Index: 2},
		},
		Loops: []LoopManifest{
			{Index: 0, From: StageTypeReview, To: StageTypeCoding},
			{Index: 1, From: StageTypeCoding, To: StageTypeDesign},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest mismatch:\n got %+v\nwant %+v", got, want)
	}

	t.Run("Stage.Order is copied, not derived from the index", func(t *testing.T) {
		// A stored document may hold an order that disagrees with the array
		// position (the engine reads Order, not the position). A copy must not
		// relaunder one into the other, so the manifest reports both as they are.
		f := &Flow{Stages: []Stage{{ID: "stage-x", Order: 9}, {ID: "stage-y", Order: 3}}}
		got := FlowManifest(f).Stages
		want := []StageManifest{{ID: "stage-x", Index: 0, Order: 9}, {ID: "stage-y", Index: 1, Order: 3}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("stages = %+v, want %+v", got, want)
		}
	})

	t.Run("an artifact may name a stage that is no longer there", func(t *testing.T) {
		f := &Flow{
			Stages:    []Stage{{ID: "stage-a"}},
			Artifacts: []Artifact{{ID: "art-orphan", StageID: "stage-gone"}},
		}
		got := FlowManifest(f).Artifacts
		if len(got) != 1 || got[0].StageID != "stage-gone" {
			t.Fatalf("the artifact's stage reference was rewritten: %+v", got)
		}
	})
}

// TestFlowManifestIsEmptyNotNil keeps the manifest usable as a JSON value and
// as a comparison subject for a document with nothing in it: the lists are
// empty, never nil, so two manifests of two empty documents are equal.
func TestFlowManifestIsEmptyNotNil(t *testing.T) {
	manifest := FlowManifest(&Flow{})
	if manifest.Stages == nil || manifest.Gates == nil || manifest.Artifacts == nil ||
		manifest.Events == nil || manifest.Loops == nil {
		t.Fatalf("empty document produced nil lists: %+v", manifest)
	}
	if len(manifest.Stages)+len(manifest.Gates)+len(manifest.Artifacts)+len(manifest.Events)+len(manifest.Loops) != 0 {
		t.Fatalf("empty document produced entries: %+v", manifest)
	}

	t.Run("nil flow", func(t *testing.T) {
		nilManifest := FlowManifest(nil)
		if !reflect.DeepEqual(nilManifest, manifest) {
			t.Fatalf("a nil Flow and an empty Flow differ: %+v vs %+v", nilManifest, manifest)
		}
	})

	t.Run("it is a JSON object", func(t *testing.T) {
		body, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("marshal manifest: %v", err)
		}
		var round map[string]any
		if err := json.Unmarshal(body, &round); err != nil {
			t.Fatalf("the manifest is not a JSON object: %v (%s)", err, body)
		}
		for _, key := range []string{"flow_id", "project_id", "stages", "gates", "artifacts", "events", "loops"} {
			if _, ok := round[key]; !ok {
				t.Fatalf("manifest has no %q member: %s", key, body)
			}
		}
	})
}

// TestFlowManifestIsDeterministicAndSourceExact states the two properties
// T3.01.b relies on: the same document always yields the same manifest, and the
// manifest of a document is the manifest of its deep copy — including a copy
// made through the JSON codec, which is the path a migration actually takes.
func TestFlowManifestIsDeterministicAndSourceExact(t *testing.T) {
	source := sampleFlowWithHistory(t)
	if got := FlowManifest(source); !reflect.DeepEqual(got, FlowManifest(source)) {
		t.Fatal("two manifests of one document differ")
	}

	t.Run("a deep copy has the same manifest", func(t *testing.T) {
		copied := CloneFlowDocument(source)
		if !reflect.DeepEqual(FlowManifest(source), FlowManifest(copied)) {
			t.Fatalf("the copy's manifest differs:\n%+v\n%+v", FlowManifest(source), FlowManifest(copied))
		}
	})

	t.Run("a manifest survives the JSON codec", func(t *testing.T) {
		body, err := EncodeFlowDocument(source)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		var decoded Flow
		if err := DecodeFlowDocument(body, &decoded); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !reflect.DeepEqual(FlowManifest(source), FlowManifest(&decoded)) {
			t.Fatal("the document's manifest changed across the codec")
		}
	})

	t.Run("an extra does not hide an identity", func(t *testing.T) {
		// Extras are preserved members, not part of the manifest: adding one
		// must not change what the manifest says.
		plain := FlowManifest(source)
		source.extras["another"] = json.RawMessage(`{"id":"not-a-part"}`)
		defer delete(source.extras, "another")
		if got := FlowManifest(source); !reflect.DeepEqual(got, plain) {
			t.Fatalf("an extra changed the manifest:\n got %+v\nwant %+v", got, plain)
		}
	})
}

// sampleFlowWithHistory returns a document with parts on several stages and
// several events, the shape a real migrated Flow has.
func sampleFlowWithHistory(t *testing.T) *Flow {
	t.Helper()
	f := sampleFlow()
	f.Stages = append(f.Stages, Stage{
		ID: "66666666-6666-4666-8666-666666666666", Type: StageTypeDesign, Name: "设计",
		Canvas: "design_board", Status: StageStatusActive,
		Gates: []Gate{{ID: "77777777-7777-4777-8777-777777777777", Phase: GatePhaseEnter, Kind: GateKindAuto}},
		Order: 1,
	})
	f.Artifacts = append(f.Artifacts, Artifact{
		ID: "88888888-8888-4888-8888-888888888888", StageID: "66666666-6666-4666-8666-666666666666",
		Type: "design_doc", Version: 1, Status: ArtifactStatusDraft, CreatedBy: ArtifactCreatorAgent,
		ContentRef: "content://design/v1", CreatedAt: f.CreatedAt,
	})
	f.Events = append(f.Events, FlowEvent{
		ID: "99999999-9999-4999-8999-999999999999", Type: "stage.done",
		StageID: "22222222-2222-4222-8222-222222222222", Message: "completed", Timestamp: f.UpdatedAt,
	})
	return f
}

// TestFlowManifestPairsWithDocumentArrays is the check T3.01.b runs, in
// miniature: every id in the document appears exactly once in the manifest, in
// the same position, and a copy walked from the manifest has those ids in those
// places. A manifest that missed, duplicated or reordered an id fails here.
func TestFlowManifestPairsWithDocumentArrays(t *testing.T) {
	f := sampleFlowWithHistory(t)
	m := FlowManifest(f)

	if len(m.Stages) != len(f.Stages) || len(m.Gates) != countGates(f) ||
		len(m.Artifacts) != len(f.Artifacts) || len(m.Events) != len(f.Events) || len(m.Loops) != len(f.Loops) {
		t.Fatalf("manifest counts do not match the document: %+v", m)
	}
	for i, s := range m.Stages {
		if s.ID != f.Stages[i].ID || s.Index != i || s.Order != f.Stages[i].Order {
			t.Fatalf("stage row %d = %+v, want id %s index %d order %d", i, s, f.Stages[i].ID, i, f.Stages[i].Order)
		}
	}
	row := 0
	for si := range f.Stages {
		for gi := range f.Stages[si].Gates {
			g := m.Gates[row]
			if g.ID != f.Stages[si].Gates[gi].ID || g.StageID != f.Stages[si].ID || g.StageIndex != si || g.Index != gi {
				t.Fatalf("gate row %d = %+v, want %s on %s", row, g, f.Stages[si].Gates[gi].ID, f.Stages[si].ID)
			}
			row++
		}
	}
	for i, a := range m.Artifacts {
		if a.ID != f.Artifacts[i].ID || a.StageID != f.Artifacts[i].StageID || a.Index != i {
			t.Fatalf("artifact row %d = %+v", i, a)
		}
	}
	for i, e := range m.Events {
		if e.ID != f.Events[i].ID || e.Index != i {
			t.Fatalf("event row %d = %+v", i, e)
		}
	}

	// The copy: rebuild the document from the manifest and compare manifests.
	copied := &Flow{ID: m.FlowID, ProjectID: m.ProjectID}
	copied.Stages = make([]Stage, len(f.Stages))
	for i, s := range m.Stages {
		copied.Stages[i] = f.Stages[s.Index]
		copied.Stages[i].Order = s.Order
		copied.Stages[i].Gates = nil
	}
	for _, g := range m.Gates {
		copied.Stages[g.StageIndex].Gates = append(copied.Stages[g.StageIndex].Gates, f.Stages[g.StageIndex].Gates[g.Index])
	}
	copied.Artifacts = make([]Artifact, len(f.Artifacts))
	for _, a := range m.Artifacts {
		if a.ID != f.Artifacts[a.Index].ID || a.StageID != f.Artifacts[a.Index].StageID {
			t.Fatalf("artifact row does not lead to the artifact it names: %+v", a)
		}
		copied.Artifacts[a.Index] = f.Artifacts[a.Index]
	}
	copied.Events = make([]FlowEvent, len(f.Events))
	for _, e := range m.Events {
		copied.Events[e.Index] = f.Events[e.Index]
	}
	for _, l := range m.Loops {
		if l.From != f.Loops[l.Index].From || l.To != f.Loops[l.Index].To {
			t.Fatalf("loop row %d leads to a different edge: %+v", l.Index, l)
		}
		copied.Loops = append(copied.Loops, f.Loops[l.Index])
	}
	if !reflect.DeepEqual(m, FlowManifest(copied)) {
		t.Fatalf("a copy walked from the manifest has a different manifest:\n%+v\n%+v", m, FlowManifest(copied))
	}
}

func countGates(f *Flow) int {
	n := 0
	for i := range f.Stages {
		n += len(f.Stages[i].Gates)
	}
	return n
}
