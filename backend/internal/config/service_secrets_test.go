package config

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeflow/backend/internal/dbx"
)

type failingPutSecretStore struct {
	*MemorySecretStore
}

func (s *failingPutSecretStore) Put(context.Context, string, string) (SecretPutResult, error) {
	return SecretPutResult{}, errors.New("secret backend unavailable")
}

func seedGlobalConfigJSON(t *testing.T, dbPath, configJSON string) {
	t.Helper()
	db, err := dbx.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := createConfigTables(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO global_config (id, config_json) VALUES (1, ?)`, configJSON); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteConfigServiceMigratesLegacyAPIKeyAndRestarts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	const secret = "sk-legacy-codeflow-secret"
	seedGlobalConfigJSON(t, dbPath, `{"default_model":"model","api_pool":[{"id":"primary","name":"Primary","provider":"openai","api_key":"`+secret+`","enabled":true}],"public_mcp":[]}`)
	store := NewMemorySecretStore()
	svc, err := NewSQLiteConfigServiceWithSecretStore(dbPath, store)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	var persisted string
	if err := svc.db.QueryRow(`SELECT config_json FROM global_config WHERE id = 1`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, secret) || strings.Contains(persisted, "api_key") {
		t.Fatalf("config_json still contains legacy credential: %s", persisted)
	}
	resolved := svc.ResolveConfig("", "")
	got, err := resolved.ResolveAPIKey(context.Background())
	if err != nil || got != secret {
		t.Fatalf("resolved migrated key = %q, %v", got, err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewSQLiteConfigServiceWithSecretStore(dbPath, store)
	if err != nil {
		t.Fatalf("restart failed: %v", err)
	}
	defer restarted.Close()
	got, err = restarted.ResolveConfig("", "").ResolveAPIKey(context.Background())
	if err != nil || got != secret {
		t.Fatalf("resolved key after restart = %q, %v", got, err)
	}
}

func TestSQLiteConfigServiceLegacyMigrationFailsClosed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	const secret = "sk-migration-must-not-load"
	seedGlobalConfigJSON(t, dbPath, `{"api_pool":[{"id":"primary","provider":"openai","api_key":"`+secret+`","enabled":true}]}`)
	store := &failingPutSecretStore{MemorySecretStore: NewMemorySecretStore()}
	if _, err := NewSQLiteConfigServiceWithSecretStore(dbPath, store); err == nil {
		t.Fatal("expected startup to fail when legacy credential cannot migrate")
	} else if strings.Contains(err.Error(), secret) {
		t.Fatalf("migration error leaked secret: %v", err)
	}
}

func TestSQLiteConfigServiceSaveFailureRemovesNewSecret(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	store := NewMemorySecretStore()
	svc, err := NewSQLiteConfigServiceWithSecretStore(dbPath, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.db.Close(); err != nil {
		t.Fatal(err)
	}
	global := svc.LoadGlobalConfig()
	global.APIPool = []APIChannel{{ID: "primary", Provider: ProviderOpenAI, APIKey: "sk-rollback-secret", Enabled: true}}
	if err := svc.SaveGlobalConfig(global); err == nil {
		t.Fatal("expected database save failure")
	}
	metadata, err := store.ListMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 0 {
		t.Fatalf("new secret was not rolled back: %#v", metadata)
	}
	if len(svc.LoadGlobalConfig().APIPool) != 0 {
		t.Fatal("in-memory config changed after failed database save")
	}
}

func TestSQLiteConfigServiceRejectsMissingReferencedSecret(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	global := GlobalConfig{APIPool: []APIChannel{{
		ID:           "primary",
		Provider:     ProviderOpenAI,
		SecretRef:    "api-channel/primary/missing",
		SecretStatus: SecretStatusConfigured,
		Enabled:      true,
	}}}
	data, err := json.Marshal(global)
	if err != nil {
		t.Fatal(err)
	}
	seedGlobalConfigJSON(t, dbPath, string(data))
	if _, err := NewSQLiteConfigServiceWithSecretStore(dbPath, NewMemorySecretStore()); err == nil {
		t.Fatal("expected startup failure for missing referenced secret")
	}
}

func TestSQLiteConfigServiceReconcilesOrphanSecretsOnStartup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	store := NewMemorySecretStore()
	if _, err := store.Put(context.Background(), "api-channel/orphan", "orphan-secret"); err != nil {
		t.Fatal(err)
	}
	svc, err := NewSQLiteConfigServiceWithSecretStore(dbPath, store)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	metadata, err := store.ListMetadata(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 0 {
		t.Fatalf("orphan secret was not reconciled: %#v", metadata)
	}
}

func TestSQLiteConfigServiceRejectsMismatchedSecretMetadata(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	store := NewMemorySecretStore()
	result, err := store.Put(context.Background(), "api-channel/primary", "persisted-secret")
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	record := store.secrets[result.Metadata.Ref]
	record.metadata.MaskedValue = "****wrong"
	store.secrets[result.Metadata.Ref] = record
	store.mu.Unlock()
	data, err := json.Marshal(GlobalConfig{APIPool: []APIChannel{{
		ID: "primary", Provider: ProviderOpenAI, SecretRef: result.Metadata.Ref,
		SecretStatus: SecretStatusConfigured, Enabled: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	seedGlobalConfigJSON(t, dbPath, string(data))
	if _, err := NewSQLiteConfigServiceWithSecretStore(dbPath, store); err == nil {
		t.Fatal("expected startup to reject mismatched secret metadata")
	}
}
