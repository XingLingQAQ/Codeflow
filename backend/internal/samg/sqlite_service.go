package samg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// NewSQLiteSAMGService opens the durable graph and access-metadata store.
func NewSQLiteSAMGService(dbPath string, config *SAMGServiceConfig) (*SAMGService, error) {
	if dbPath == "" { return nil, errors.New("samg db path is required") }
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil { return nil, fmt.Errorf("create samg db dir: %w", err) }
	}
	store, err := NewSQLiteTripleStore(dbPath, func() *TripleStoreConfig { if config != nil { return config.StoreConfig }; return nil }())
	if err != nil { return nil, err }
	svc, err := NewSAMGServiceWithStore(store, config)
	if err != nil { _ = store.Close(); return nil, err }
	return svc, nil
}
