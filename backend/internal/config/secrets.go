package config

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrSecretNotFound = errors.New("secret not found")

type SecretStatus string

const (
	SecretStatusMissing    SecretStatus = "missing"
	SecretStatusConfigured SecretStatus = "configured"
)

type SecretMetadata struct {
	Ref         string       `json:"ref"`
	Status      SecretStatus `json:"status"`
	MaskedValue string       `json:"masked_value,omitempty"`
	Version     int          `json:"version"`
	UpdatedAt   int64        `json:"updated_at"`
}

type SecretPutResult struct {
	Metadata SecretMetadata
	Changed  bool
}

// SecretStore is the only persistence boundary allowed to hold API secrets.
// Implementations must never include plaintext values in errors or metadata.
type SecretStore interface {
	Put(ctx context.Context, ref, value string) (SecretPutResult, error)
	Get(ctx context.Context, ref string) (string, error)
	Metadata(ctx context.Context, ref string) (SecretMetadata, error)
	ListMetadata(ctx context.Context) ([]SecretMetadata, error)
	Delete(ctx context.Context, ref string) (bool, error)
}

type memorySecret struct {
	value    string
	metadata SecretMetadata
}

type MemorySecretStore struct {
	mu      sync.RWMutex
	secrets map[string]memorySecret
}

func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{secrets: make(map[string]memorySecret)}
}

func (s *MemorySecretStore) Put(_ context.Context, ref, value string) (SecretPutResult, error) {
	if ref == "" || value == "" {
		return SecretPutResult{}, errors.New("secret ref and value are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.secrets[ref]; ok && current.value == value {
		return SecretPutResult{Metadata: current.metadata}, nil
	}
	version := 1
	if current, ok := s.secrets[ref]; ok {
		version = current.metadata.Version + 1
	}
	metadata := SecretMetadata{
		Ref:         ref,
		Status:      SecretStatusConfigured,
		MaskedValue: maskSecret(value),
		Version:     version,
		UpdatedAt:   time.Now().UnixMilli(),
	}
	s.secrets[ref] = memorySecret{value: value, metadata: metadata}
	return SecretPutResult{Metadata: metadata, Changed: true}, nil
}

func (s *MemorySecretStore) Get(_ context.Context, ref string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	secret, ok := s.secrets[ref]
	if !ok {
		return "", ErrSecretNotFound
	}
	return secret.value, nil
}

func (s *MemorySecretStore) Metadata(_ context.Context, ref string) (SecretMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	secret, ok := s.secrets[ref]
	if !ok {
		return SecretMetadata{}, ErrSecretNotFound
	}
	return secret.metadata, nil
}

func (s *MemorySecretStore) ListMetadata(_ context.Context) ([]SecretMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metadata := make([]SecretMetadata, 0, len(s.secrets))
	for _, secret := range s.secrets {
		metadata = append(metadata, secret.metadata)
	}
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].Ref < metadata[j].Ref })
	return metadata, nil
}

func (s *MemorySecretStore) Delete(_ context.Context, ref string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.secrets[ref]; !ok {
		return false, nil
	}
	delete(s.secrets, ref)
	return true, nil
}

type encryptedSecretRecord struct {
	Ciphertext  string `json:"ciphertext"`
	MaskedValue string `json:"masked_value"`
	Version     int    `json:"version"`
	UpdatedAt   int64  `json:"updated_at"`
}

type encryptedSecretDocument struct {
	SchemaVersion int                              `json:"schema_version"`
	Secrets       map[string]encryptedSecretRecord `json:"secrets"`
}

// EncryptedFileSecretStore keeps only OS/user-bound ciphertext in a file
// separate from config.db. Platform protectSecret/unprotectSecret functions
// provide the key boundary.
type EncryptedFileSecretStore struct {
	path      string
	protector secretProtector
	mu        sync.RWMutex
	records   map[string]encryptedSecretRecord
}

type secretProtector interface {
	Protect([]byte) ([]byte, error)
	Unprotect([]byte) ([]byte, error)
}

type platformSecretProtector struct{}

func (platformSecretProtector) Protect(value []byte) ([]byte, error)   { return protectSecret(value) }
func (platformSecretProtector) Unprotect(value []byte) ([]byte, error) { return unprotectSecret(value) }

func NewEncryptedFileSecretStore(path string) (*EncryptedFileSecretStore, error) {
	if path == "" {
		return nil, errors.New("secret store path is required")
	}
	if err := secretProtectorAvailable(); err != nil {
		return nil, fmt.Errorf("secret protection unavailable: %w", err)
	}
	return newEncryptedFileSecretStore(path, platformSecretProtector{})
}

// NewEncryptedFileSecretStoreWithMasterKey supports explicit platform
// bootstrap and deterministic tests. The key must originate outside the
// ciphertext store.
func NewEncryptedFileSecretStoreWithMasterKey(path string, key []byte) (*EncryptedFileSecretStore, error) {
	protector, err := newAESSecretProtector(key)
	if err != nil {
		return nil, err
	}
	return newEncryptedFileSecretStore(path, protector)
}

func newEncryptedFileSecretStore(path string, protector secretProtector) (*EncryptedFileSecretStore, error) {
	if path == "" || protector == nil {
		return nil, errors.New("secret store path and protector are required")
	}
	store := &EncryptedFileSecretStore{path: path, protector: protector, records: make(map[string]encryptedSecretRecord)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read encrypted secret store: %w", err)
	}
	var document encryptedSecretDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, errors.New("encrypted secret store is invalid")
	}
	if document.SchemaVersion != 1 || document.Secrets == nil {
		return nil, errors.New("encrypted secret store schema is unsupported")
	}
	store.records = document.Secrets
	return store, nil
}

func (s *EncryptedFileSecretStore) Put(_ context.Context, ref, value string) (SecretPutResult, error) {
	if ref == "" || value == "" {
		return SecretPutResult{}, errors.New("secret ref and value are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, hadPrevious := s.records[ref]
	if hadPrevious {
		ciphertext, err := decodeCiphertext(previous.Ciphertext)
		if err != nil {
			return SecretPutResult{}, errors.New("existing secret ciphertext is invalid")
		}
		plaintext, err := s.protector.Unprotect(ciphertext)
		if err != nil {
			return SecretPutResult{}, errors.New("decrypt existing secret failed")
		}
		if string(plaintext) == value {
			return SecretPutResult{Metadata: recordMetadata(ref, previous)}, nil
		}
	}
	protected, err := s.protector.Protect([]byte(value))
	if err != nil {
		return SecretPutResult{}, errors.New("encrypt secret failed")
	}
	version := 1
	if hadPrevious {
		version = previous.Version + 1
	}
	record := encryptedSecretRecord{
		Ciphertext:  base64.RawStdEncoding.EncodeToString(protected),
		MaskedValue: maskSecret(value),
		Version:     version,
		UpdatedAt:   time.Now().UnixMilli(),
	}
	s.records[ref] = record
	if err := s.persistLocked(); err != nil {
		if hadPrevious {
			s.records[ref] = previous
		} else {
			delete(s.records, ref)
		}
		return SecretPutResult{}, err
	}
	return SecretPutResult{Metadata: recordMetadata(ref, record), Changed: true}, nil
}

func (s *EncryptedFileSecretStore) Get(_ context.Context, ref string) (string, error) {
	s.mu.RLock()
	record, ok := s.records[ref]
	s.mu.RUnlock()
	if !ok {
		return "", ErrSecretNotFound
	}
	ciphertext, err := decodeCiphertext(record.Ciphertext)
	if err != nil {
		return "", errors.New("secret ciphertext is invalid")
	}
	plaintext, err := s.protector.Unprotect(ciphertext)
	if err != nil {
		return "", errors.New("decrypt secret failed")
	}
	return string(plaintext), nil
}

func (s *EncryptedFileSecretStore) Metadata(_ context.Context, ref string) (SecretMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.records[ref]
	if !ok {
		return SecretMetadata{}, ErrSecretNotFound
	}
	return recordMetadata(ref, record), nil
}

func (s *EncryptedFileSecretStore) ListMetadata(_ context.Context) ([]SecretMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metadata := make([]SecretMetadata, 0, len(s.records))
	for ref, record := range s.records {
		metadata = append(metadata, recordMetadata(ref, record))
	}
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].Ref < metadata[j].Ref })
	return metadata, nil
}

func (s *EncryptedFileSecretStore) Delete(_ context.Context, ref string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[ref]
	if !ok {
		return false, nil
	}
	delete(s.records, ref)
	if err := s.persistLocked(); err != nil {
		s.records[ref] = record
		return false, err
	}
	return true, nil
}

func (s *EncryptedFileSecretStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create secret store directory: %w", err)
	}
	document := encryptedSecretDocument{SchemaVersion: 1, Secrets: s.records}
	data, err := json.Marshal(document)
	if err != nil {
		return errors.New("encode encrypted secret store failed")
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".codeflow-secrets-*")
	if err != nil {
		return fmt.Errorf("create encrypted secret store temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceSecretStoreFile(tempPath, s.path); err != nil {
		return fmt.Errorf("replace encrypted secret store: %w", err)
	}
	return nil
}

func decodeCiphertext(value string) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func recordMetadata(ref string, record encryptedSecretRecord) SecretMetadata {
	return SecretMetadata{
		Ref:         ref,
		Status:      SecretStatusConfigured,
		MaskedValue: record.MaskedValue,
		Version:     record.Version,
		UpdatedAt:   record.UpdatedAt,
	}
}

func maskSecret(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return "****" + value[len(value)-4:]
}

type aesSecretProtector struct {
	gcm cipher.AEAD
}

func newAESSecretProtector(key []byte) (*aesSecretProtector, error) {
	if len(key) != 32 {
		return nil, errors.New("secret master key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &aesSecretProtector{gcm: gcm}, nil
}

func (p *aesSecretProtector) Protect(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, p.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return p.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (p *aesSecretProtector) Unprotect(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < p.gcm.NonceSize() {
		return nil, errors.New("ciphertext is invalid")
	}
	return p.gcm.Open(nil, ciphertext[:p.gcm.NonceSize()], ciphertext[p.gcm.NonceSize():], nil)
}
