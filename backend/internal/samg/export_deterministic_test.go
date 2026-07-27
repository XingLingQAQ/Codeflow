package samg

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestExportGraphDeterministic verifies that ExportGraph produces byte-identical
// output (and therefore equal sha256 digests) when called twice on the same
// populated graph. This is required for snapshot consistency verification to
// work reliably.
func TestExportGraphDeterministic(t *testing.T) {
	store := NewInMemoryTripleStore(nil)
	ctx := context.Background()

	ts := time.Now().UnixMilli()
	triples := []Triple{
		{
			ID:         "triple:det-3",
			Subject:    CreateNode("entity:c", EntityTypes.Variable, "varC"),
			Predicate:  Predicates.Uses,
			Object:     CreateNodeObject(CreateNode("entity:d", EntityTypes.Function, "funcD")),
			Confidence: 0.6, Timestamp: ts,
			Source: TripleSource{SessionID: "det", ExtractionMethod: ExtractionRule},
		},
		{
			ID:         "triple:det-1",
			Subject:    CreateNode("entity:a", EntityTypes.Function, "funcA"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Function, "funcB")),
			Confidence: 0.8, Timestamp: ts,
			Source: TripleSource{SessionID: "det", ExtractionMethod: ExtractionRule},
		},
		{
			ID:         "triple:det-2",
			Subject:    CreateNode("entity:b", EntityTypes.Function, "funcB"),
			Predicate:  Predicates.RelatedTo,
			Object:     CreateNodeObject(CreateNode("entity:c", EntityTypes.Variable, "varC")),
			Confidence: 0.7, Timestamp: ts,
			Source: TripleSource{SessionID: "det", ExtractionMethod: ExtractionRule},
		},
	}

	if err := store.Add(ctx, triples); err != nil {
		t.Fatalf("Add: %v", err)
	}

	first, err := store.ExportGraph(ctx)
	if err != nil {
		t.Fatalf("ExportGraph (1): %v", err)
	}
	second, err := store.ExportGraph(ctx)
	if err != nil {
		t.Fatalf("ExportGraph (2): %v", err)
	}

	b1, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	b2, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}

	if string(b1) != string(b2) {
		t.Fatalf("ExportGraph is not deterministic:\n  first:  %s\n  second: %s", b1, b2)
	}

	if len(first.Graph) != 3 {
		t.Fatalf("expected 3 triples, got %d", len(first.Graph))
	}
	if first.Graph[0].ID != "triple:det-1" || first.Graph[1].ID != "triple:det-2" || first.Graph[2].ID != "triple:det-3" {
		t.Fatalf("triples not sorted by ID: %s, %s, %s",
			first.Graph[0].ID, first.Graph[1].ID, first.Graph[2].ID)
	}
}
