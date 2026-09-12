package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/codeflow/backend/internal/dbx"
)

// NewSQLiteAtomicMemoryService creates the durable atomic-memory and vector
// pair used by MemoryAgent and the atomic-memory API.
func NewSQLiteAtomicMemoryService(ctx context.Context, dbPath, vectorPath string) (*AtomicMemoryService, error) {
	if dbPath == "" || vectorPath == "" { return nil, errors.New("atomic memory paths are required") }
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil { return nil, fmt.Errorf("create atomic memory db dir: %w", err) }
	if err := os.MkdirAll(filepath.Dir(vectorPath), 0o755); err != nil { return nil, fmt.Errorf("create atomic vector db dir: %w", err) }
	db, err := dbx.Open(dbPath, dbx.WithSynchronous("NORMAL"), dbx.WithForeignKeys(false), dbx.WithMaxOpenConns(8))
	if err != nil { return nil, fmt.Errorf("open atomic memory db: %w", err) }
	vector, err := CreateSQLiteVectorStore(&VectorStoreConfig{CollectionName:"atomic_memory", DBPath:vectorPath, WALMode:true}, NewSimpleEmbeddingProvider(384))
	if err != nil { _ = db.Close(); return nil, fmt.Errorf("open atomic vector store: %w", err) }
	svc, err := NewAtomicMemoryService(ctx, db, vector, NewSimpleEmbeddingProvider(384))
	if err != nil { _ = vector.Close(); _ = db.Close(); return nil, err }
	return svc, nil
}

func (s *AtomicMemoryService) Close() error {
	// 关闭串联（T13.02.b part 2）：先停索引 worker（停收新 job、取消在途
	// ctx、有界等待退出），再关向量库与正文库。
	if s != nil && s.indexWorker != nil { s.indexWorker.stop() }
	var first error
	if s.vectorStore != nil { if err := s.vectorStore.Close(); err != nil { first = err } }
	if s.db != nil { if err := s.db.Close(); err != nil && first == nil { first = err } }
	return first
}

var (
	defaultAtomicMemoryService *AtomicMemoryService
	defaultAtomicMemoryMu sync.RWMutex
)

func GetAtomicMemoryService() *AtomicMemoryService { defaultAtomicMemoryMu.RLock(); defer defaultAtomicMemoryMu.RUnlock(); return defaultAtomicMemoryService }
func SetAtomicMemoryService(svc *AtomicMemoryService) { defaultAtomicMemoryMu.Lock(); defer defaultAtomicMemoryMu.Unlock(); defaultAtomicMemoryService=svc }
