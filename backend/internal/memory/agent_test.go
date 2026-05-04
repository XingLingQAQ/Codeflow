package memory

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/codeflow/backend/internal/samg"
)

type memoryAgentTestDeps struct {
	agent   *MemoryAgent
	archive IRawArchive
	atomic  *AtomicMemoryService
	samg    *samg.SAMGService
	cleanup func()
}

func setupMemoryAgentTestDeps(t *testing.T) memoryAgentTestDeps {
	t.Helper()

	tmpDir := t.TempDir()
	archive := NewSQLiteRawArchive(filepath.Join(tmpDir, "raw_archive.db"))

	db, err := sql.Open("sqlite3", filepath.Join(tmpDir, "atomic_memory.db"))
	if err != nil {
		t.Fatalf("open atomic memory db failed: %v", err)
	}

	embeddingProvider := NewSimpleEmbeddingProvider(32)
	vectorStore, err := CreateSQLiteVectorStore(&VectorStoreConfig{
		CollectionName: "memory_agent_test",
		DBPath:         filepath.Join(tmpDir, "atomic_vectors.db"),
		WALMode:        true,
	}, embeddingProvider)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create vector store failed: %v", err)
	}

	atomicSvc, err := NewAtomicMemoryService(context.Background(), db, vectorStore, embeddingProvider)
	if err != nil {
		_ = vectorStore.Close()
		_ = db.Close()
		t.Fatalf("create atomic memory service failed: %v", err)
	}

	samgSvc := samg.NewSAMGService(nil)
	cleanup := func() {
		_ = vectorStore.Close()
		_ = db.Close()
		_ = archive.Close()
	}

	return memoryAgentTestDeps{
		agent:   NewMemoryAgent(archive, atomicSvc, samgSvc),
		archive: archive,
		atomic:  atomicSvc,
		samg:    samgSvc,
		cleanup: cleanup,
	}
}

func ingestTraceableMemory(t *testing.T, agent *MemoryAgent, sessionID string) (*IngestResult, IngestRequest) {
	t.Helper()

	req := IngestRequest{
		Content:   "class UserService extends BaseService implements IUserService",
		Type:      RawEntryConversation,
		SessionID: sessionID,
		Source:    AtomicMemorySourceAssistant,
		Tags:      []string{"architecture", "memory-agent"},
		Metadata: map[string]interface{}{
			"origin": "agent_test",
		},
	}

	result, err := agent.Ingest(context.Background(), req)
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	return result, req
}

func TestMemoryAgentIngestStoresAcrossLayers(t *testing.T) {
	deps := setupMemoryAgentTestDeps(t)
	defer deps.cleanup()

	result, req := ingestTraceableMemory(t, deps.agent, "session-ingest")

	if strings.TrimSpace(result.RawArchiveID) == "" {
		t.Fatal("expected raw archive id")
	}
	if strings.TrimSpace(result.AtomicMemoryID) == "" {
		t.Fatal("expected atomic memory id")
	}
	if result.SAMGTripleCount <= 0 {
		t.Fatalf("expected positive SAMG triple count, got %d", result.SAMGTripleCount)
	}

	entry, err := deps.archive.Get(context.Background(), result.RawArchiveID)
	if err != nil {
		t.Fatalf("archive Get failed: %v", err)
	}
	if entry == nil {
		t.Fatal("expected archived entry")
	}
	if entry.Content != req.Content {
		t.Fatalf("unexpected archived content: %s", entry.Content)
	}
	if entry.SessionID != req.SessionID {
		t.Fatalf("unexpected archived session id: %s", entry.SessionID)
	}

	memories, err := deps.atomic.GetBySession(context.Background(), req.SessionID, 10, 0)
	if err != nil {
		t.Fatalf("GetBySession failed: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 atomic memory, got %d", len(memories))
	}
	if memories[0].ID != result.AtomicMemoryID {
		t.Fatalf("unexpected atomic memory id: %s", memories[0].ID)
	}

	queryResult, err := deps.samg.QueryMemory(context.Background(), samg.QueryMemoryRequest{
		Topic:           "UserService",
		MaxResults:      10,
		ResolvePointers: true,
	})
	if err != nil {
		t.Fatalf("QueryMemory failed: %v", err)
	}
	if len(queryResult.ActivatedNodes) == 0 {
		t.Fatal("expected activated SAMG nodes")
	}

	hasPointer := false
	for _, node := range queryResult.ActivatedNodes {
		for _, pointer := range node.Pointers {
			if pointer.SourceID == result.RawArchiveID {
				hasPointer = true
				break
			}
		}
		if hasPointer {
			break
		}
	}
	if !hasPointer {
		t.Fatal("expected SAMG pointer referencing raw archive id")
	}
}

func TestMemoryAgentRetrieveReturnsTraceableSources(t *testing.T) {
	deps := setupMemoryAgentTestDeps(t)
	defer deps.cleanup()

	ingested, req := ingestTraceableMemory(t, deps.agent, "session-retrieve")

	retrieved, err := deps.agent.Retrieve(context.Background(), RetrieveRequest{
		Query:      "UserService",
		SessionID:  req.SessionID,
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(retrieved.AtomicMemories) == 0 {
		t.Fatal("expected atomic memories in retrieve result")
	}
	if len(retrieved.SAMGNodes) == 0 {
		t.Fatal("expected SAMG nodes in retrieve result")
	}
	if retrieved.TotalFound <= 0 {
		t.Fatalf("expected TotalFound > 0, got %d", retrieved.TotalFound)
	}

	hasResolvedPointer := false
	for _, node := range retrieved.SAMGNodes {
		for _, pointer := range node.Pointers {
			if pointer.SourceID == ingested.RawArchiveID && pointer.ResolvedContent == req.Content {
				hasResolvedPointer = true
				break
			}
		}
		if hasResolvedPointer {
			break
		}
	}
	if !hasResolvedPointer {
		t.Fatal("expected resolved SAMG pointer content")
	}

	hasAtomicSource := false
	hasSAMGSource := false
	for _, source := range retrieved.Sources {
		switch source.Kind {
		case MemorySourceKindAtomicMemory:
			if source.ID == ingested.AtomicMemoryID {
				hasAtomicSource = true
			}
		case MemorySourceKindSAMGPointer:
			if source.SourceID == ingested.RawArchiveID && source.Content == req.Content && source.SessionID == req.SessionID {
				hasSAMGSource = true
			}
		}
	}

	if !hasAtomicSource {
		t.Fatal("expected atomic memory source in retrieve result")
	}
	if !hasSAMGSource {
		t.Fatal("expected SAMG pointer source in retrieve result")
	}
}

func TestMemoryAgentRetrieveFallsBackToRawArchive(t *testing.T) {
	tmpDir := t.TempDir()
	archive := NewSQLiteRawArchive(filepath.Join(tmpDir, "raw_archive.db"))
	defer archive.Close()

	agent := NewMemoryAgent(archive, nil, nil)
	result, err := agent.Ingest(context.Background(), IngestRequest{
		Content:   "fallback retrieval should return raw archive source",
		Type:      RawEntryDocument,
		SessionID: "session-raw-fallback",
		Source:    AtomicMemorySourceSystem,
	})
	if err != nil {
		t.Fatalf("Ingest failed: %v", err)
	}
	if result.RawArchiveID == "" {
		t.Fatal("expected raw archive id for fallback test")
	}

	retrieved, err := agent.Retrieve(context.Background(), RetrieveRequest{
		Query:      "raw archive source",
		SessionID:  "session-raw-fallback",
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Retrieve fallback failed: %v", err)
	}

	if len(retrieved.AtomicMemories) != 0 {
		t.Fatalf("expected no atomic memories, got %d", len(retrieved.AtomicMemories))
	}
	if len(retrieved.SAMGNodes) != 0 {
		t.Fatalf("expected no SAMG nodes, got %d", len(retrieved.SAMGNodes))
	}
	if len(retrieved.Sources) != 1 {
		t.Fatalf("expected 1 raw archive source, got %d", len(retrieved.Sources))
	}
	if retrieved.TotalFound != 1 {
		t.Fatalf("expected TotalFound 1, got %d", retrieved.TotalFound)
	}

	source := retrieved.Sources[0]
	if source.Kind != MemorySourceKindRawArchive {
		t.Fatalf("expected raw archive source kind, got %s", source.Kind)
	}
	if source.ID != result.RawArchiveID {
		t.Fatalf("unexpected raw archive source id: %s", source.ID)
	}
	if source.SessionID != "session-raw-fallback" {
		t.Fatalf("unexpected raw archive session id: %s", source.SessionID)
	}
	if !strings.Contains(source.Content, "fallback retrieval") {
		t.Fatalf("unexpected raw archive content: %s", source.Content)
	}
}

func TestMemoryAgentAssembleContextIncludesRecentHotMemories(t *testing.T) {
	deps := setupMemoryAgentTestDeps(t)
	defer deps.cleanup()

	_, req := ingestTraceableMemory(t, deps.agent, "session-context")
	contextResult, err := deps.agent.AssembleContext(context.Background(), ContextRequest{
		SessionID: req.SessionID,
		MaxTokens: 200,
	})
	if err != nil {
		t.Fatalf("AssembleContext failed: %v", err)
	}

	if len(contextResult.AtomicMemories) == 0 {
		t.Fatal("expected recent hot memories in context result")
	}
	if len(contextResult.SAMGNodes) != 0 {
		t.Fatalf("expected no SAMG nodes without query, got %d", len(contextResult.SAMGNodes))
	}
	if contextResult.SourceCount != len(contextResult.Sources) {
		t.Fatalf("source count mismatch: %d vs %d", contextResult.SourceCount, len(contextResult.Sources))
	}
	if !strings.Contains(contextResult.ContextBlock, "<memory_context>") {
		t.Fatalf("expected memory context tag, got %s", contextResult.ContextBlock)
	}
	if !strings.Contains(contextResult.ContextBlock, "UserService") {
		t.Fatalf("expected ingested content in context block, got %s", contextResult.ContextBlock)
	}
}
