package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedFileSecretStorePersistsWithoutPlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	masterKey := []byte("0123456789abcdef0123456789abcdef")
	store, err := NewEncryptedFileSecretStoreWithMasterKey(path, masterKey)
	if err != nil {
		t.Fatalf("NewEncryptedFileSecretStore() error = %v", err)
	}
	const secret = "sk-codeflow-persisted-secret"
	result, err := store.Put(context.Background(), "api-channel/test/1", secret)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if !result.Changed || result.Metadata.Status != SecretStatusConfigured {
		t.Fatalf("unexpected put result: %#v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatal("encrypted store contains plaintext secret")
	}

	reopened, err := NewEncryptedFileSecretStoreWithMasterKey(path, masterKey)
	if err != nil {
		t.Fatalf("reopen secret store: %v", err)
	}
	got, err := reopened.Get(context.Background(), result.Metadata.Ref)
	if err != nil {
		t.Fatalf("Get() after reopen error = %v", err)
	}
	if got != secret {
		t.Fatalf("secret after reopen = %q", got)
	}
}

func TestConfigManagerStoresOnlySecretReference(t *testing.T) {
	mgr := NewConfigManagerWithSecretStore(nil, NewMemorySecretStore())
	global := mgr.LoadGlobalConfig()
	global.APIPool = []APIChannel{{
		ID:       "primary",
		Name:     "Primary",
		Provider: ProviderOpenAI,
		APIKey:   "sk-codeflow-runtime-secret",
		Enabled:  true,
	}}
	if err := mgr.SaveGlobalConfig(global); err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	stored := mgr.LoadGlobalConfig()
	if stored.APIPool[0].APIKey != "" {
		t.Fatal("plaintext key retained in config manager")
	}
	if stored.APIPool[0].SecretRef == "" || stored.APIPool[0].SecretStatus != SecretStatusConfigured {
		t.Fatalf("credential metadata missing: %#v", stored.APIPool[0])
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "sk-codeflow-runtime-secret") || strings.Contains(string(encoded), "api_key") {
		t.Fatalf("serialized config contains credential material: %s", encoded)
	}

	resolved := mgr.ResolveConfig("", "")
	key, err := resolved.ResolveAPIKey(context.Background())
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}
	if key != "sk-codeflow-runtime-secret" {
		t.Fatalf("resolved key = %q", key)
	}
}

func TestSecretUpdateAndDeleteAreIdempotent(t *testing.T) {
	store := NewMemorySecretStore()
	ctx := context.Background()
	first, err := store.Put(ctx, "ref", "first-secret")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := store.Put(ctx, "ref", "first-secret")
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Changed || repeated.Metadata.Version != first.Metadata.Version {
		t.Fatalf("repeated put was not idempotent: %#v", repeated)
	}
	rotated, err := store.Put(ctx, "ref", "second-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !rotated.Changed || rotated.Metadata.Version != first.Metadata.Version+1 {
		t.Fatalf("rotation did not advance version: %#v", rotated)
	}
	if removed, err := store.Delete(ctx, "ref"); err != nil || !removed {
		t.Fatalf("first delete = %v, %v", removed, err)
	}
	if removed, err := store.Delete(ctx, "ref"); err != nil || removed {
		t.Fatalf("second delete = %v, %v", removed, err)
	}
}

func TestEncryptedFileSecretStoreRollsBackWhenPersistenceFails(t *testing.T) {
	// Point the store at an existing directory. The in-memory record must be
	// restored when the atomic replacement cannot replace that directory.
	path := filepath.Join(t.TempDir(), "secret.json")
	store, err := NewEncryptedFileSecretStoreWithMasterKey(path, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewEncryptedFileSecretStoreWithMasterKey() error = %v", err)
	}
	store.path = t.TempDir()
	if _, err := store.Put(context.Background(), "ref", "secret"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if _, err := store.Get(context.Background(), "ref"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("failed write left a secret behind: %v", err)
	}
}

func TestConfigManagerRejectsInconsistentSecretMetadata(t *testing.T) {
	store := NewMemorySecretStore()
	result, err := store.Put(context.Background(), "api-channel/test", "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	record := store.secrets[result.Metadata.Ref]
	record.metadata.MaskedValue = "****wrong"
	store.secrets[result.Metadata.Ref] = record
	store.mu.Unlock()

	mgr := NewConfigManagerWithSecretStore(nil, store)
	global := &GlobalConfig{APIPool: []APIChannel{{
		ID: "test", Provider: ProviderOpenAI, SecretRef: result.Metadata.Ref,
		SecretStatus: SecretStatusConfigured, Enabled: true,
	}}}
	if err := mgr.SaveGlobalConfig(global); err == nil {
		t.Fatal("expected inconsistent secret metadata to be rejected")
	}
}

func TestConfigManagerDeepCopiesResolvedPointers(t *testing.T) {
	mgr := NewConfigManager(nil)
	maxTokens := 2048
	topP := 0.8
	if err := mgr.SaveSessionConfig(&SessionConfig{SessionID: "session", MaxTokens: &maxTokens}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SaveRoleConfig(RoleMain, &RoleConfig{Model: "model", APIChannel: "default", TopP: &topP}); err != nil {
		t.Fatal(err)
	}
	resolved := mgr.ResolveConfig("session", RoleMain)
	*resolved.MaxTokens = 1
	*resolved.TopP = 0.1
	again := mgr.ResolveConfig("session", RoleMain)
	if again.MaxTokens == nil || *again.MaxTokens != maxTokens {
		t.Fatalf("resolved max tokens was aliased: %#v", again.MaxTokens)
	}
	if again.TopP == nil || *again.TopP != topP {
		t.Fatalf("resolved top_p was aliased: %#v", again.TopP)
	}
}

func TestConfigManagerRemoveMissingAPIChannelReturnsError(t *testing.T) {
	mgr := NewConfigManager(nil)
	if err := mgr.RemoveAPIChannel("missing"); !errors.Is(err, ErrAPIChannelNotFound) {
		t.Fatalf("expected ErrAPIChannelNotFound, got %v", err)
	}
}
