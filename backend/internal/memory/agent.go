package memory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"

	"github.com/codeflow/backend/internal/samg"
)

// MemoryAgent 统一记忆调度层（MMU）。
// 协调 Raw Archive、Atomic Memory、SAMG 三个子系统。
type MemoryAgent struct {
	archive IRawArchive
	atomic  *AtomicMemoryService
	samg    *samg.SAMGService
	mu      sync.RWMutex
}

var (
	globalMemoryAgent *MemoryAgent
	memoryAgentMu     sync.Mutex
)

// NewMemoryAgent 创建 MemoryAgent。
func NewMemoryAgent(archive IRawArchive, atomic *AtomicMemoryService, samgSvc *samg.SAMGService) *MemoryAgent {
	return &MemoryAgent{
		archive: archive,
		atomic:  atomic,
		samg:    samgSvc,
	}
}

// SetMemoryAgent 设置全局 MemoryAgent。
func SetMemoryAgent(agent *MemoryAgent) {
	memoryAgentMu.Lock()
	defer memoryAgentMu.Unlock()
	globalMemoryAgent = agent
}

// GetMemoryAgent 获取全局 MemoryAgent 单例。
func GetMemoryAgent() (*MemoryAgent, error) {
	memoryAgentMu.Lock()
	defer memoryAgentMu.Unlock()

	if globalMemoryAgent != nil {
		return globalMemoryAgent, nil
	}

	archiveImpl, err := GetRawArchive()
	if err != nil {
		return nil, fmt.Errorf("init raw archive for agent: %w", err)
	}

	dataDir := filepath.Join(".", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create agent data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "atomic_memory.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open atomic memory db for agent: %w", err)
	}

	vectorDBPath := filepath.Join(dataDir, "atomic_vectors.db")
	vectorStore, err := CreateSQLiteVectorStore(&VectorStoreConfig{
		CollectionName: "atomic_memory",
		DBPath:         vectorDBPath,
		WALMode:        true,
	}, NewSimpleEmbeddingProvider(384))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create vector store for agent: %w", err)
	}

	atomicSvc, err := NewAtomicMemoryService(context.Background(), db, vectorStore, NewSimpleEmbeddingProvider(384))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create atomic service for agent: %w", err)
	}

	globalMemoryAgent = NewMemoryAgent(archiveImpl, atomicSvc, samg.GetSAMGService())
	return globalMemoryAgent, nil
}

// SetAtomicService 注入 AtomicMemoryService。
func (a *MemoryAgent) SetAtomicService(svc *AtomicMemoryService) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.atomic = svc
}

// SetSAMGService 注入 SAMGService。
func (a *MemoryAgent) SetSAMGService(svc *samg.SAMGService) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.samg = svc
}

func (a *MemoryAgent) snapshotServices() (IRawArchive, *AtomicMemoryService, *samg.SAMGService) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.archive, a.atomic, a.samg
}

// Ingest 统一写入：同时写入 Raw Archive + Atomic Memory + SAMG Pointer。
func (a *MemoryAgent) Ingest(ctx context.Context, req IngestRequest) (*IngestResult, error) {
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("ingest: content is required")
	}

	archiveSvc, atomicSvc, samgSvc := a.snapshotServices()
	result := &IngestResult{}

	if archiveSvc != nil {
		entry := RawEntry{
			Type:      req.Type,
			Content:   req.Content,
			Metadata:  req.Metadata,
			SessionID: req.SessionID,
		}
		id, err := archiveSvc.Store(ctx, entry)
		if err != nil {
			return nil, fmt.Errorf("ingest raw archive: %w", err)
		}
		result.RawArchiveID = id
	}

	if atomicSvc != nil {
		mem := &AtomicMemory{
			ID:         uuid.New().String(),
			Timestamp:  time.Now().Unix(),
			Content:    req.Content,
			Tags:       req.Tags,
			SessionID:  req.SessionID,
			Source:     req.Source,
			Importance: 0.5,
			Tier:       MemoryTierHot,
			Heat:       1.0,
			Surprise:   0.5,
		}
		if err := atomicSvc.Add(ctx, mem); err != nil {
			return result, fmt.Errorf("ingest atomic memory: %w", err)
		}
		result.AtomicMemoryID = mem.ID
	}

	if samgSvc != nil && result.RawArchiveID != "" {
		source := samg.TripleSource{
			SessionID:        req.SessionID,
			AgentRole:        string(req.Source),
			ExtractionMethod: samg.ExtractionRule,
		}
		triples, err := samgSvc.ExtractWithPointers(ctx, req.Content, source, result.RawArchiveID)
		if err != nil {
			return result, fmt.Errorf("ingest samg pointers: %w", err)
		}
		result.SAMGTripleCount = len(triples)
	}

	return result, nil
}

// Retrieve 统一检索：聚合 Atomic Memory、SAMG Pointer 与 Raw Archive fallback。
func (a *MemoryAgent) Retrieve(ctx context.Context, req RetrieveRequest) (*RetrieveResult, error) {
	archiveSvc, atomicSvc, samgSvc := a.snapshotServices()

	limit := req.MaxResults
	if limit <= 0 {
		limit = 10
	}

	result := &RetrieveResult{
		AtomicMemories: []AtomicMemory{},
		SAMGNodes:      []MemoryAgentNode{},
		Sources:        []MemoryAgentSource{},
	}

	var rawFallbackCount int

	if atomicSvc != nil {
		var (
			memories []AtomicMemory
			err      error
		)

		if req.Tier != "" {
			memories, err = searchAtomicByTier(ctx, atomicSvc, MemoryTier(req.Tier), req.SessionID, limit)
			if err != nil {
				return nil, fmt.Errorf("retrieve by tier: %w", err)
			}
		} else {
			opts := &AtomicMemorySearchOptions{
				Limit:     limit,
				SessionID: req.SessionID,
			}
			memories, err = atomicSvc.Search(ctx, req.Query, opts)
			if err != nil {
				return nil, fmt.Errorf("retrieve search: %w", err)
			}
		}

		if req.MinHeat > 0 {
			filtered := make([]AtomicMemory, 0, len(memories))
			for _, memory := range memories {
				if memory.Heat >= req.MinHeat {
					filtered = append(filtered, memory)
				}
			}
			memories = filtered
		}

		result.AtomicMemories = memories
		result.Sources = mergeSources(result.Sources, buildAtomicSources(memories))
	}

	if samgSvc != nil && strings.TrimSpace(req.Query) != "" {
		queryResult, err := samgSvc.QueryMemory(ctx, samg.QueryMemoryRequest{
			Topic:           req.Query,
			MaxResults:      limit,
			ResolvePointers: true,
		})
		if err != nil {
			return nil, fmt.Errorf("retrieve samg memory: %w", err)
		}

		nodes, sources, err := resolveSAMGQuery(ctx, archiveSvc, queryResult)
		if err != nil {
			return nil, err
		}
		result.SAMGNodes = nodes
		result.Sources = mergeSources(result.Sources, sources)
	}

	if len(result.AtomicMemories) == 0 && len(result.SAMGNodes) == 0 {
		rawSources, err := searchRawArchiveSources(ctx, archiveSvc, req.Query, req.SessionID, limit)
		if err != nil {
			return nil, err
		}
		rawFallbackCount = len(rawSources)
		result.Sources = mergeSources(result.Sources, rawSources)
	}

	result.TotalFound = len(result.AtomicMemories) + len(result.SAMGNodes) + rawFallbackCount
	return result, nil
}

// AssembleContext 上下文组装：为 AI 请求构建统一记忆上下文块。
func (a *MemoryAgent) AssembleContext(ctx context.Context, req ContextRequest) (*ContextResult, error) {
	_, atomicSvc, _ := a.snapshotServices()

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2000
	}

	result := &ContextResult{
		AtomicMemories: []AtomicMemory{},
		SAMGNodes:      []MemoryAgentNode{},
		Sources:        []MemoryAgentSource{},
	}

	if strings.TrimSpace(req.Query) != "" {
		retrieved, err := a.Retrieve(ctx, RetrieveRequest{
			Query:      req.Query,
			MaxResults: 7,
			SessionID:  req.SessionID,
		})
		if err != nil {
			return nil, fmt.Errorf("assemble context retrieve: %w", err)
		}
		result.AtomicMemories = mergeAtomicMemories(result.AtomicMemories, retrieved.AtomicMemories)
		result.SAMGNodes = mergeSAMGNodes(result.SAMGNodes, retrieved.SAMGNodes)
		result.Sources = mergeSources(result.Sources, retrieved.Sources)
	}

	if atomicSvc != nil {
		recentHot, err := getRecentHotMemories(ctx, atomicSvc, req.SessionID, 3)
		if err != nil {
			return nil, fmt.Errorf("assemble context recent hot: %w", err)
		}

		newHot := findNewAtomicMemories(result.AtomicMemories, recentHot)
		result.AtomicMemories = mergeAtomicMemories(result.AtomicMemories, recentHot)
		result.Sources = mergeSources(result.Sources, buildAtomicSources(newHot))
	}

	result.ContextBlock = buildMemoryContextBlock(result.AtomicMemories, result.SAMGNodes, result.Sources, maxTokens)
	result.SourceCount = len(result.Sources)
	return result, nil
}

func searchAtomicByTier(ctx context.Context, atomicSvc *AtomicMemoryService, tier MemoryTier, sessionID string, limit int) ([]AtomicMemory, error) {
	if atomicSvc == nil {
		return []AtomicMemory{}, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if strings.TrimSpace(sessionID) == "" {
		return atomicSvc.SearchByTier(ctx, tier, limit)
	}

	candidates, err := atomicSvc.GetBySession(ctx, sessionID, maxInt(limit*4, 20), 0)
	if err != nil {
		return nil, err
	}

	filtered := make([]AtomicMemory, 0, len(candidates))
	for _, memory := range candidates {
		if memory.Tier == tier {
			filtered = append(filtered, memory)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Heat == filtered[j].Heat {
			return filtered[i].Timestamp > filtered[j].Timestamp
		}
		return filtered[i].Heat > filtered[j].Heat
	})

	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func getRecentHotMemories(ctx context.Context, atomicSvc *AtomicMemoryService, sessionID string, limit int) ([]AtomicMemory, error) {
	return searchAtomicByTier(ctx, atomicSvc, MemoryTierHot, sessionID, limit)
}

func resolveSAMGQuery(ctx context.Context, archiveSvc IRawArchive, queryResult *samg.QueryMemoryResponse) ([]MemoryAgentNode, []MemoryAgentSource, error) {
	if queryResult == nil {
		return []MemoryAgentNode{}, []MemoryAgentSource{}, nil
	}

	nodes := make([]MemoryAgentNode, 0, len(queryResult.ActivatedNodes))
	sources := make([]MemoryAgentSource, 0)

	for pointerIndex, node := range queryResult.ActivatedNodes {
		resolvedPointers := make([]MemoryAgentResolvedPointer, 0, len(node.Pointers))
		for idx, pointer := range node.Pointers {
			resolvedPointer := MemoryAgentResolvedPointer{
				SourceID:        pointer.SourceID,
				SourceType:      pointer.SourceType,
				Summary:         pointer.Summary,
				LineRange:       pointer.LineRange,
				FilePath:        pointer.FilePath,
				Timestamp:       pointer.Timestamp,
				Relevance:       pointer.Relevance,
				ResolvedContent: pointer.ResolvedContent,
			}

			source := MemoryAgentSource{
				Kind:       MemorySourceKindSAMGPointer,
				ID:         fmt.Sprintf("%s:%s:%d", node.ID, pointer.SourceID, pointerIndex+idx),
				Title:      node.Label,
				Summary:    pointer.Summary,
				Content:    pointer.ResolvedContent,
				NodeID:     node.ID,
				NodeLabel:  node.Label,
				SourceID:   pointer.SourceID,
				SourceType: pointer.SourceType,
				FilePath:   pointer.FilePath,
				LineRange:  pointer.LineRange,
				Timestamp:  pointer.Timestamp,
				Relevance:  pointer.Relevance,
			}

			if archiveSvc != nil && strings.TrimSpace(pointer.SourceID) != "" {
				entry, err := archiveSvc.Get(ctx, pointer.SourceID)
				if err != nil {
					return nil, nil, fmt.Errorf("resolve raw archive pointer %s: %w", pointer.SourceID, err)
				}
				if entry != nil {
					resolvedPointer.ResolvedContent = entry.Content
					resolvedPointer.SessionID = entry.SessionID
					source.Content = entry.Content
					source.SessionID = entry.SessionID
					if source.Timestamp == 0 {
						source.Timestamp = entry.Timestamp
					}
				}
			}

			if strings.TrimSpace(source.Summary) == "" {
				source.Summary = compactText(source.Content, 120)
			}
			resolvedPointers = append(resolvedPointers, resolvedPointer)
			sources = append(sources, source)
		}

		nodes = append(nodes, MemoryAgentNode{
			ID:         node.ID,
			Label:      node.Label,
			Activation: node.Activation,
			Hop:        node.Hop,
			Pointers:   resolvedPointers,
		})
	}

	return nodes, sources, nil
}

func searchRawArchiveSources(ctx context.Context, archiveSvc IRawArchive, query string, sessionID string, limit int) ([]MemoryAgentSource, error) {
	if archiveSvc == nil || strings.TrimSpace(query) == "" {
		return []MemoryAgentSource{}, nil
	}
	if limit <= 0 {
		limit = 10
	}

	var entries []RawEntry
	var err error
	if strings.TrimSpace(sessionID) == "" {
		entries, err = archiveSvc.Search(ctx, query, limit)
		if err != nil {
			return nil, fmt.Errorf("retrieve raw archive fallback: %w", err)
		}
		return buildRawArchiveSources(entries), nil
	}

	candidates, err := archiveSvc.List(ctx, &RawArchiveSearchOptions{
		SessionID: sessionID,
		Limit:     maxInt(limit*5, 20),
	})
	if err != nil {
		return nil, fmt.Errorf("retrieve raw archive fallback by session: %w", err)
	}

	queryLower := strings.ToLower(strings.TrimSpace(query))
	for _, entry := range candidates {
		if strings.Contains(strings.ToLower(entry.Content), queryLower) {
			entries = append(entries, entry)
			if len(entries) >= limit {
				break
			}
		}
	}

	return buildRawArchiveSources(entries), nil
}

func mergeAtomicMemories(existing []AtomicMemory, additions []AtomicMemory) []AtomicMemory {
	if len(additions) == 0 {
		if existing == nil {
			return []AtomicMemory{}
		}
		return existing
	}

	merged := make([]AtomicMemory, 0, len(existing)+len(additions))
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, memory := range existing {
		merged = append(merged, memory)
		seen[memory.ID] = struct{}{}
	}
	for _, memory := range additions {
		if _, ok := seen[memory.ID]; ok {
			continue
		}
		merged = append(merged, memory)
		seen[memory.ID] = struct{}{}
	}
	return merged
}

func findNewAtomicMemories(existing []AtomicMemory, additions []AtomicMemory) []AtomicMemory {
	seen := make(map[string]struct{}, len(existing))
	for _, memory := range existing {
		seen[memory.ID] = struct{}{}
	}

	newItems := make([]AtomicMemory, 0, len(additions))
	for _, memory := range additions {
		if _, ok := seen[memory.ID]; ok {
			continue
		}
		newItems = append(newItems, memory)
	}
	return newItems
}

func mergeSAMGNodes(existing []MemoryAgentNode, additions []MemoryAgentNode) []MemoryAgentNode {
	if len(additions) == 0 {
		if existing == nil {
			return []MemoryAgentNode{}
		}
		return existing
	}

	merged := make([]MemoryAgentNode, 0, len(existing)+len(additions))
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, node := range existing {
		merged = append(merged, node)
		seen[node.ID] = struct{}{}
	}
	for _, node := range additions {
		if _, ok := seen[node.ID]; ok {
			continue
		}
		merged = append(merged, node)
		seen[node.ID] = struct{}{}
	}
	return merged
}

func mergeSources(existing []MemoryAgentSource, additions []MemoryAgentSource) []MemoryAgentSource {
	if len(additions) == 0 {
		if existing == nil {
			return []MemoryAgentSource{}
		}
		return existing
	}

	merged := make([]MemoryAgentSource, 0, len(existing)+len(additions))
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, source := range existing {
		key := sourceKey(source)
		merged = append(merged, source)
		seen[key] = struct{}{}
	}
	for _, source := range additions {
		key := sourceKey(source)
		if _, ok := seen[key]; ok {
			continue
		}
		merged = append(merged, source)
		seen[key] = struct{}{}
	}
	return merged
}

func sourceKey(source MemoryAgentSource) string {
	return strings.Join([]string{string(source.Kind), source.ID, source.NodeID, source.SourceID}, "|")
}

func buildAtomicSources(memories []AtomicMemory) []MemoryAgentSource {
	sources := make([]MemoryAgentSource, 0, len(memories))
	for _, memory := range memories {
		sources = append(sources, MemoryAgentSource{
			Kind:      MemorySourceKindAtomicMemory,
			ID:        memory.ID,
			Title:     string(memory.Tier),
			Summary:   compactText(memory.Content, 120),
			Content:   memory.Content,
			SessionID: memory.SessionID,
			Timestamp: memory.Timestamp,
			Relevance: memory.Heat,
		})
	}
	return sources
}

func buildRawArchiveSources(entries []RawEntry) []MemoryAgentSource {
	sources := make([]MemoryAgentSource, 0, len(entries))
	for _, entry := range entries {
		sources = append(sources, MemoryAgentSource{
			Kind:      MemorySourceKindRawArchive,
			ID:        entry.ID,
			Title:     string(entry.Type),
			Summary:   compactText(entry.Content, 120),
			Content:   entry.Content,
			SessionID: entry.SessionID,
			Timestamp: entry.Timestamp,
		})
	}
	return sources
}

func buildMemoryContextBlock(atomicMemories []AtomicMemory, samgNodes []MemoryAgentNode, sources []MemoryAgentSource, maxTokens int) string {
	if len(atomicMemories) == 0 && len(samgNodes) == 0 && len(sources) == 0 {
		return ""
	}

	const endTag = "</memory_context>"
	charBudget := maxTokens * 4
	if charBudget <= len(endTag)+32 {
		charBudget = len(endTag) + 64
	}

	var sb strings.Builder
	sb.WriteString("<memory_context>\n")
	canAppend := func(line string) bool {
		line = strings.TrimSpace(line)
		if line == "" {
			return true
		}
		if sb.Len()+len(line)+1+len(endTag) > charBudget {
			return false
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
		return true
	}

	for _, memory := range atomicMemories {
		line := fmt.Sprintf("[atomic|%s|%s] %s", memory.Tier, memory.Source, compactText(memory.Content, 240))
		if !canAppend(line) {
			break
		}
	}

	stopSAMG := false
	for _, node := range samgNodes {
		for _, pointer := range node.Pointers {
			content := pointer.ResolvedContent
			if strings.TrimSpace(content) == "" {
				content = pointer.Summary
			}
			line := fmt.Sprintf("[samg|%s|%s] %s", node.Label, pointer.SourceType, compactText(content, 240))
			if !canAppend(line) {
				stopSAMG = true
				break
			}
		}
		if stopSAMG {
			break
		}
	}

	for _, source := range sources {
		if source.Kind != MemorySourceKindRawArchive {
			continue
		}
		line := fmt.Sprintf("[raw_archive|%s] %s", source.Title, compactText(source.Content, 240))
		if !canAppend(line) {
			break
		}
	}

	sb.WriteString(endTag)
	return sb.String()
}

func compactText(text string, limit int) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit > 0 && len(compact) > limit {
		return compact[:limit] + "..."
	}
	return compact
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
