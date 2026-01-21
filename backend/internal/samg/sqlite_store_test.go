// Package samg - SQLite triple store tests
package samg

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestSQLiteStore(t *testing.T) (*SQLiteTripleStore, func()) {
	tmpFile, err := os.CreateTemp("", "samg_test_*.db")
	require.NoError(t, err)
	tmpFile.Close()

	store, err := NewSQLiteTripleStore(tmpFile.Name(), nil)
	require.NoError(t, err)

	cleanup := func() {
		store.Close()
		os.Remove(tmpFile.Name())
	}

	return store, cleanup
}

func TestSQLiteTripleStore_AddAndGet(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triple := Triple{
		ID:         "test-triple-1",
		Subject:    CreateNode("entity:a", EntityTypes.Class, "ClassA"),
		Predicate:  Predicates.Extends,
		Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "ClassB")),
		Confidence: 0.9,
		Timestamp:  time.Now().UnixMilli(),
		Source: TripleSource{
			SessionID:        "session-1",
			ExtractionMethod: ExtractionRule,
		},
	}

	err := store.Add(ctx, []Triple{triple})
	assert.NoError(t, err)

	retrieved, err := store.Get(ctx, "test-triple-1")
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "test-triple-1", retrieved.ID)
	assert.Equal(t, "entity:a", retrieved.Subject.ID)
	assert.Equal(t, Predicates.Extends, retrieved.Predicate)
	assert.Equal(t, "entity:b", retrieved.Object.Node.ID)
	assert.Equal(t, 0.9, retrieved.Confidence)
}

func TestSQLiteTripleStore_Query(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triples := []Triple{
		{
			ID:         "triple-1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "triple-2",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:c", EntityTypes.Function, "C")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "triple-3",
			Subject:    CreateNode("entity:b", EntityTypes.Class, "B"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:d", EntityTypes.Class, "D")),
			Confidence: 0.7,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	// Query by subject
	results, err := store.Query(ctx, TripleQuery{Subject: "entity:a"})
	assert.NoError(t, err)
	assert.Len(t, results, 2)

	// Query by predicate
	results, err = store.Query(ctx, TripleQuery{Predicate: Predicates.Extends})
	assert.NoError(t, err)
	assert.Len(t, results, 2)

	// Query by object
	results, err = store.Query(ctx, TripleQuery{Object: "entity:b"})
	assert.NoError(t, err)
	assert.Len(t, results, 1)

	// Query with min confidence
	results, err = store.Query(ctx, TripleQuery{MinConfidence: 0.85})
	assert.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestSQLiteTripleStore_Delete(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triple := Triple{
		ID:         "delete-test",
		Subject:    CreateNode("entity:x", EntityTypes.Class, "X"),
		Predicate:  Predicates.Extends,
		Object:     CreateNodeObject(CreateNode("entity:y", EntityTypes.Class, "Y")),
		Confidence: 0.9,
		Timestamp:  time.Now().UnixMilli(),
	}

	err := store.Add(ctx, []Triple{triple})
	assert.NoError(t, err)

	// Verify it exists
	retrieved, err := store.Get(ctx, "delete-test")
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)

	// Delete
	err = store.Delete(ctx, []string{"delete-test"})
	assert.NoError(t, err)

	// Verify it's gone
	retrieved, err = store.Get(ctx, "delete-test")
	assert.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestSQLiteTripleStore_Update(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triple := Triple{
		ID:         "update-test",
		Subject:    CreateNode("entity:x", EntityTypes.Class, "X"),
		Predicate:  Predicates.Extends,
		Object:     CreateNodeObject(CreateNode("entity:y", EntityTypes.Class, "Y")),
		Confidence: 0.5,
		Timestamp:  time.Now().UnixMilli(),
	}

	err := store.Add(ctx, []Triple{triple})
	assert.NoError(t, err)

	// Update confidence
	err = store.Update(ctx, "update-test", map[string]interface{}{
		"confidence": 0.95,
	})
	assert.NoError(t, err)

	// Verify update
	retrieved, err := store.Get(ctx, "update-test")
	assert.NoError(t, err)
	assert.Equal(t, 0.95, retrieved.Confidence)
}

func TestSQLiteTripleStore_Clear(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triples := []Triple{
		{
			ID:         "clear-1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "clear-2",
			Subject:    CreateNode("entity:c", EntityTypes.Class, "C"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:d", EntityTypes.Class, "D")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	// Verify triples exist
	results, err := store.Query(ctx, TripleQuery{})
	assert.NoError(t, err)
	assert.Len(t, results, 2)

	// Clear
	err = store.Clear(ctx)
	assert.NoError(t, err)

	// Verify empty
	results, err = store.Query(ctx, TripleQuery{})
	assert.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestSQLiteTripleStore_Entity(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add triple (should auto-create entities)
	triple := Triple{
		ID:         "entity-test",
		Subject:    CreateNode("entity:user", EntityTypes.Class, "User"),
		Predicate:  Predicates.Extends,
		Object:     CreateNodeObject(CreateNode("entity:base", EntityTypes.Class, "BaseModel")),
		Confidence: 0.9,
		Timestamp:  time.Now().UnixMilli(),
	}

	err := store.Add(ctx, []Triple{triple})
	assert.NoError(t, err)

	// Get entity
	entity, err := store.GetEntity(ctx, "entity:user")
	assert.NoError(t, err)
	assert.NotNil(t, entity)
	assert.Equal(t, "entity:user", entity.ID)
	assert.Equal(t, "User", entity.Label)

	// Get all entities
	entities, err := store.GetEntities(ctx)
	assert.NoError(t, err)
	assert.Len(t, entities, 2) // user and base

	// Upsert entity
	newEntity := Entity{
		ID:          "entity:new",
		Type:        []string{EntityTypes.Function},
		Label:       "NewFunction",
		Description: "A new function",
	}
	err = store.UpsertEntity(ctx, newEntity)
	assert.NoError(t, err)

	retrieved, err := store.GetEntity(ctx, "entity:new")
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "NewFunction", retrieved.Label)
	assert.Equal(t, "A new function", retrieved.Description)
}

func TestSQLiteTripleStore_ExportImport(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triples := []Triple{
		{
			ID:         "export-1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	// Export
	graph, err := store.ExportGraph(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, graph)
	assert.Len(t, graph.Graph, 1)
	assert.Equal(t, 1, graph.Metadata.TripleCount)

	// Clear and import
	err = store.Clear(ctx)
	assert.NoError(t, err)

	err = store.ImportGraph(ctx, graph)
	assert.NoError(t, err)

	// Verify
	results, err := store.Query(ctx, TripleQuery{})
	assert.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestSQLiteTripleStore_Stats(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triples := []Triple{
		{
			ID:         "stats-1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "stats-2",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:c", EntityTypes.Function, "C")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	stats, err := store.GetStats(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 2, stats.TripleCount)
	assert.Equal(t, 3, stats.EntityCount) // a, b, c
	assert.Equal(t, 2, stats.PredicateCount) // extends, calls
}

func TestSQLiteTripleStore_LiteralObject(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triple := Triple{
		ID:        "literal-test",
		Subject:   CreateNode("entity:config", EntityTypes.Variable, "Config"),
		Predicate: "codeflow:hasValue",
		Object:    CreateLiteralObject(CreateLiteral("test-value", "xsd:string", "")),
		Confidence: 1.0,
		Timestamp:  time.Now().UnixMilli(),
	}

	err := store.Add(ctx, []Triple{triple})
	assert.NoError(t, err)

	retrieved, err := store.Get(ctx, "literal-test")
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.True(t, retrieved.Object.IsLiteral())
	assert.Equal(t, "test-value", retrieved.Object.Literal.Value)
	assert.Equal(t, "xsd:string", retrieved.Object.Literal.Type)
}

func TestSQLiteTripleStore_Deduplicate(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add duplicate triples with different confidence
	triples := []Triple{
		{
			ID:         "dup-1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.5,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "dup-2",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	// Deduplicate
	removed, err := store.Deduplicate(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 1, removed)

	// Verify only higher confidence remains
	results, err := store.Query(ctx, TripleQuery{})
	assert.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 0.9, results[0].Confidence)
}

func TestSQLiteTripleStore_FindByMethods(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	triples := []Triple{
		{
			ID:         "find-1",
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.9,
			Timestamp:  time.Now().UnixMilli(),
		},
		{
			ID:         "find-2",
			Subject:    CreateNode("entity:c", EntityTypes.Class, "C"),
			Predicate:  Predicates.Extends,
			Object:     CreateNodeObject(CreateNode("entity:b", EntityTypes.Class, "B")),
			Confidence: 0.8,
			Timestamp:  time.Now().UnixMilli(),
		},
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	// FindBySubject
	results, err := store.FindBySubject(ctx, "entity:a")
	assert.NoError(t, err)
	assert.Len(t, results, 1)

	// FindByPredicate
	results, err = store.FindByPredicate(ctx, Predicates.Extends)
	assert.NoError(t, err)
	assert.Len(t, results, 2)

	// FindByObject
	results, err = store.FindByObject(ctx, "entity:b")
	assert.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestSQLiteTripleStore_Pagination(t *testing.T) {
	store, cleanup := setupTestSQLiteStore(t)
	defer cleanup()

	ctx := context.Background()

	// Add 10 triples
	triples := make([]Triple, 10)
	for i := 0; i < 10; i++ {
		triples[i] = Triple{
			ID:         GenerateTripleID("entity:a", Predicates.Calls, "entity:"+string(rune('a'+i))),
			Subject:    CreateNode("entity:a", EntityTypes.Class, "A"),
			Predicate:  Predicates.Calls,
			Object:     CreateNodeObject(CreateNode("entity:"+string(rune('a'+i)), EntityTypes.Function, string(rune('A'+i)))),
			Confidence: float64(10-i) / 10.0,
			Timestamp:  time.Now().UnixMilli(),
		}
	}

	err := store.Add(ctx, triples)
	assert.NoError(t, err)

	// Query with limit
	results, err := store.Query(ctx, TripleQuery{Limit: 5})
	assert.NoError(t, err)
	assert.Len(t, results, 5)

	// Query with offset
	results, err = store.Query(ctx, TripleQuery{Limit: 5, Offset: 5})
	assert.NoError(t, err)
	assert.Len(t, results, 5)

	// Query with offset beyond data
	results, err = store.Query(ctx, TripleQuery{Offset: 20})
	assert.NoError(t, err)
	assert.Len(t, results, 0)
}
