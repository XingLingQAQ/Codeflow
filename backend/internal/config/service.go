// Package config - Config service layer
package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/dbx"
)

// IConfigService is an alias for IConfigManager for consistency with other modules.
type IConfigService interface {
	IConfigManager
}

// SQLiteConfigService implements IConfigService with SQLite persistence.
type SQLiteConfigService struct {
	*ConfigManager
	papiManager *PAPIManager
	db          *sql.DB
	mu          sync.RWMutex
	// papiMu serializes every PAPI mutation pipeline: snapshot -> validate ->
	// SQL transaction -> publish. papiDiagnostics records category conflicts
	// detected when loading persisted PAPI data (E-10); it is written under
	// papiMu and read via PAPIDiagnostics.
	//
	// Fixed lock order: papiMu -> PAPIManager.mu. Code holding PAPIManager.mu
	// never acquires papiMu, and s.mu (the config-row write lock used by the
	// global/session/role save helpers) is never acquired while holding
	// either PAPI lock, so the three locks cannot cycle. No network I/O
	// happens under any of them.
	papiMu          sync.Mutex
	papiDiagnostics []string
}

// NewSQLiteConfigService creates a new SQLite-backed config service.
func NewSQLiteConfigService(dbPath string) (*SQLiteConfigService, error) {
	var secretStore SecretStore
	var err error
	if dbPath == "" || dbPath == ":memory:" {
		secretStore = NewMemorySecretStore()
	} else {
		secretStore, err = NewEncryptedFileSecretStore(dbPath + ".secrets.json")
		if err != nil {
			return nil, err
		}
	}
	return NewSQLiteConfigServiceWithSecretStore(dbPath, secretStore)
}

// NewSQLiteConfigServiceWithSecretStore allows tests and platform bootstrap to
// inject an explicit credential backend.
func NewSQLiteConfigServiceWithSecretStore(dbPath string, secretStore SecretStore) (*SQLiteConfigService, error) {
	if secretStore == nil {
		return nil, errors.New("secret store is required")
	}
	if err := prepareConfigDBDir(dbPath); err != nil {
		return nil, err
	}

	db, err := dbx.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Create tables
	if err := createConfigTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	svc := &SQLiteConfigService{
		ConfigManager: NewConfigManagerWithSecretStore(nil, secretStore),
		papiManager:   NewPAPIManager(),
		db:            db,
	}
	if err := svc.migrateLegacyAPIKeys(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate legacy API credentials: %w", err)
	}

	// Load existing config from database
	if err := svc.loadFromDB(); err != nil {
		db.Close()
		return nil, fmt.Errorf("load from database: %w", err)
	}
	deleted, err := svc.ConfigManager.reconcileSecrets(context.Background(), svc.ConfigManager.LoadGlobalConfig())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("reconcile API credentials: %w", err)
	}
	for _, ref := range deleted {
		svc.auditSecret(context.Background(), "secret.delete", ref, "", 0)
	}

	// Load PAPI variables from database
	if err := svc.loadPAPIFromDB(); err != nil {
		db.Close()
		return nil, fmt.Errorf("load PAPI from database: %w", err)
	}

	return svc, nil
}

func prepareConfigDBDir(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create config db dir: %w", err)
		}
	}
	return nil
}

// Close closes the database connection.
func (s *SQLiteConfigService) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// SaveGlobalConfig overrides to persist to SQLite.
func (s *SQLiteConfigService) SaveGlobalConfig(config *GlobalConfig) error {
	if config == nil {
		return errors.New("config cannot be nil")
	}
	current := s.ConfigManager.LoadGlobalConfig()
	ctx := context.Background()
	if _, err := s.ConfigManager.reconcileSecrets(ctx, current); err != nil {
		return err
	}
	sanitized, created, err := s.ConfigManager.prepareGlobalConfig(ctx, config, current)
	if err != nil {
		return err
	}
	if err := s.saveGlobalConfigToDB(sanitized); err != nil {
		return errors.Join(err, s.ConfigManager.deleteSecretRefs(ctx, created))
	}
	s.ConfigManager.mu.Lock()
	s.ConfigManager.globalConfig = cloneGlobalConfig(sanitized)
	s.ConfigManager.mu.Unlock()
	s.ConfigManager.notifyChange()
	s.auditSecretChanges(ctx, current, sanitized)
	deleted, err := s.ConfigManager.reconcileSecrets(ctx, sanitized)
	for _, ref := range deleted {
		s.auditSecret(ctx, "secret.delete", ref, "", 0)
	}
	return err
}

// SaveSessionConfig overrides to persist to SQLite.
func (s *SQLiteConfigService) SaveSessionConfig(config *SessionConfig) error {
	if config == nil || config.SessionID == "" {
		return errors.New("session config and session ID are required")
	}
	merged := mergeSessionConfig(s.ConfigManager.LoadSessionConfig(config.SessionID), config)
	if err := s.saveSessionConfigToDB(merged); err != nil {
		return err
	}
	return s.ConfigManager.SaveSessionConfig(config)
}

// SaveRoleConfig overrides to persist to SQLite.
func (s *SQLiteConfigService) SaveRoleConfig(role RoleType, config *RoleConfig) error {
	if config == nil {
		return errors.New("role config cannot be nil")
	}
	if err := s.saveRoleConfigToDB(role, config); err != nil {
		return err
	}
	return s.ConfigManager.SaveRoleConfig(role, config)
}

// Database operations

func createConfigTables(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS global_config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		config_json TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS session_config (
		session_id TEXT PRIMARY KEY,
		config_json TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS role_config (
		role TEXT PRIMARY KEY,
		config_json TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS papi_variables (
		name TEXT PRIMARY KEY,
		variable_json TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := db.Exec(schema)
	return err
}

func (s *SQLiteConfigService) loadFromDB() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load global config
	var globalJSON string
	err := s.db.QueryRow("SELECT config_json FROM global_config WHERE id = 1").Scan(&globalJSON)
	if err == nil {
		var global GlobalConfig
		if err := json.Unmarshal([]byte(globalJSON), &global); err != nil {
			return fmt.Errorf("decode global config: %w", err)
		}
		if err := s.ConfigManager.validateSecretReferences(context.Background(), &global); err != nil {
			return err
		}
		s.ConfigManager.globalConfig = cloneGlobalConfig(&global)
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("load global config: %w", err)
	}

	// Load session configs
	rows, err := s.db.Query("SELECT session_id, config_json FROM session_config")
	if err != nil {
		return fmt.Errorf("load session configs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sessionID, configJSON string
		if err := rows.Scan(&sessionID, &configJSON); err != nil {
			return fmt.Errorf("scan session config: %w", err)
		}
		var session SessionConfig
		if err := json.Unmarshal([]byte(configJSON), &session); err != nil {
			return fmt.Errorf("decode session config %q: %w", sessionID, err)
		}
		s.ConfigManager.sessionConfigs[sessionID] = &session
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate session configs: %w", err)
	}

	// Load role configs
	roleRows, err := s.db.Query("SELECT role, config_json FROM role_config")
	if err != nil {
		return fmt.Errorf("load role configs: %w", err)
	}
	defer roleRows.Close()

	for roleRows.Next() {
		var roleStr, configJSON string
		if err := roleRows.Scan(&roleStr, &configJSON); err != nil {
			return fmt.Errorf("scan role config: %w", err)
		}
		var role RoleConfig
		if err := json.Unmarshal([]byte(configJSON), &role); err != nil {
			return fmt.Errorf("decode role config %q: %w", roleStr, err)
		}
		s.ConfigManager.roleConfigs[RoleType(roleStr)] = &role
	}
	if err := roleRows.Err(); err != nil {
		return fmt.Errorf("iterate role configs: %w", err)
	}

	return nil
}

// AddAPIChannel persists through the same secret-aware global update path.
func (s *SQLiteConfigService) AddAPIChannel(channel *APIChannel) error {
	if channel == nil {
		return errors.New("channel cannot be nil")
	}
	current := s.LoadGlobalConfig()
	found := false
	for i := range current.APIPool {
		if current.APIPool[i].ID == channel.ID {
			current.APIPool[i] = *channel
			found = true
			break
		}
	}
	if !found {
		current.APIPool = append(current.APIPool, *channel)
	}
	return s.SaveGlobalConfig(current)
}

// RemoveAPIChannel removes the referenced secret after the metadata update
// commits. A missing channel is reported explicitly so callers cannot mistake
// a configuration mismatch for a successful deletion.
func (s *SQLiteConfigService) RemoveAPIChannel(channelID string) error {
	if strings.TrimSpace(channelID) == "" {
		return errors.New("channel ID cannot be empty")
	}
	current := s.LoadGlobalConfig()
	for i := range current.APIPool {
		if current.APIPool[i].ID == channelID {
			current.APIPool = append(current.APIPool[:i], current.APIPool[i+1:]...)
			return s.SaveGlobalConfig(current)
		}
	}
	return fmt.Errorf("%w: %s", ErrAPIChannelNotFound, channelID)
}

func (s *SQLiteConfigService) migrateLegacyAPIKeys(ctx context.Context) error {
	var raw string
	err := s.db.QueryRow("SELECT config_json FROM global_config WHERE id = 1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var document struct {
		APIPool []map[string]json.RawMessage `json:"api_pool"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return fmt.Errorf("decode legacy global config: %w", err)
	}
	hasLegacyField := false
	legacyKeys := make(map[int]string)
	for i, channel := range document.APIPool {
		value, ok := channel["api_key"]
		if !ok {
			continue
		}
		hasLegacyField = true
		var key string
		if err := json.Unmarshal(value, &key); err != nil {
			return errors.New("legacy API credential has an invalid representation")
		}
		if key != "" {
			legacyKeys[i] = key
		}
	}
	if !hasLegacyField {
		return nil
	}
	var global GlobalConfig
	if err := json.Unmarshal([]byte(raw), &global); err != nil {
		return fmt.Errorf("decode legacy global config: %w", err)
	}
	for i, key := range legacyKeys {
		if i >= len(global.APIPool) {
			return errors.New("legacy API credential channel index is invalid")
		}
		global.APIPool[i].APIKey = key
	}
	sanitized, created, err := s.ConfigManager.prepareGlobalConfig(ctx, &global, &global)
	if err != nil {
		return err
	}
	if err := s.saveGlobalConfigToDB(sanitized); err != nil {
		return errors.Join(err, s.ConfigManager.deleteSecretRefs(ctx, created))
	}
	createdSet := make(map[string]struct{}, len(created))
	for _, ref := range created {
		createdSet[ref] = struct{}{}
	}
	for _, channel := range sanitized.APIPool {
		if _, ok := createdSet[channel.SecretRef]; ok {
			s.auditSecret(ctx, "secret.migrate", channel.SecretRef, channel.ID, channel.SecretVersion)
		}
	}
	return nil
}

func (s *SQLiteConfigService) auditSecretChanges(ctx context.Context, before, after *GlobalConfig) {
	oldByID := make(map[string]APIChannel)
	if before != nil {
		for _, channel := range before.APIPool {
			oldByID[channel.ID] = channel
		}
	}
	for _, channel := range after.APIPool {
		old := oldByID[channel.ID]
		switch {
		case old.SecretRef == "" && channel.SecretRef != "":
			s.auditSecret(ctx, "secret.create", channel.SecretRef, channel.ID, channel.SecretVersion)
		case old.SecretRef != "" && old.SecretRef != channel.SecretRef && channel.SecretRef != "":
			s.auditSecret(ctx, "secret.rotate", channel.SecretRef, channel.ID, channel.SecretVersion)
		}
	}
}

func (s *SQLiteConfigService) auditSecret(ctx context.Context, action, ref, channelID string, version int) {
	if !audit.HasAuditService() {
		return
	}
	if err := audit.GetAuditService().Log(ctx, &audit.AuditLogEntry{
		EventType: audit.EventConfigChange,
		Severity:  audit.SeverityInfo,
		Actor:     audit.AuditActor{ID: "codeflow-server", Type: "service"},
		Resource:  audit.AuditResource{Type: "api_credential", ID: ref},
		Action:    action,
		Outcome:   audit.OutcomeSuccess,
		Details: map[string]interface{}{
			"channel_id": channelID,
			"version":    version,
		},
	}); err != nil {
		log.Printf("[WARN] credential audit write failed: action=%s err=%v", action, err)
	}
}

func (s *SQLiteConfigService) saveGlobalConfigToDB(config *GlobalConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO global_config (id, config_json) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET config_json = ?, updated_at = CURRENT_TIMESTAMP
	`, string(configJSON), string(configJSON))

	return err
}

func (s *SQLiteConfigService) saveSessionConfigToDB(config *SessionConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO session_config (session_id, config_json) VALUES (?, ?)
		ON CONFLICT(session_id) DO UPDATE SET config_json = ?, updated_at = CURRENT_TIMESTAMP
	`, config.SessionID, string(configJSON), string(configJSON))

	return err
}

func (s *SQLiteConfigService) saveRoleConfigToDB(role RoleType, config *RoleConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	configJSON, err := json.Marshal(config)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO role_config (role, config_json) VALUES (?, ?)
		ON CONFLICT(role) DO UPDATE SET config_json = ?, updated_at = CURRENT_TIMESTAMP
	`, string(role), string(configJSON), string(configJSON))

	return err
}

// Global service instance
var defaultConfigService IConfigService

// GetConfigService returns the global config service instance.
func GetConfigService() IConfigService {
	if defaultConfigService == nil {
		defaultConfigService = NewConfigManager(nil)
	}
	return defaultConfigService
}

// SetConfigService sets the global config service instance (for testing).
func SetConfigService(svc IConfigService) {
	defaultConfigService = svc
}

const defaultConfigContextTimeout = 5 * time.Second

// WithContext wraps the service with context support while preserving IConfigService compatibility.
type ConfigServiceWithContext struct {
	IConfigService
	timeout time.Duration
}

// NewConfigServiceWithContext creates a context-aware config service.
func NewConfigServiceWithContext(svc IConfigService) *ConfigServiceWithContext {
	return NewConfigServiceWithContextTimeout(svc, defaultConfigContextTimeout)
}

// NewConfigServiceWithContextTimeout creates a context-aware config service with a custom timeout.
func NewConfigServiceWithContextTimeout(svc IConfigService, timeout time.Duration) *ConfigServiceWithContext {
	if timeout <= 0 {
		timeout = defaultConfigContextTimeout
	}
	return &ConfigServiceWithContext{IConfigService: svc, timeout: timeout}
}

// LoadGlobalConfigWithContext loads global config with cancellation and timeout support.
func (s *ConfigServiceWithContext) LoadGlobalConfigWithContext(ctx context.Context) (*GlobalConfig, error) {
	ctx, cancel, err := s.contextWithTimeout(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.LoadGlobalConfig(), nil
}

// LoadSessionConfigWithContext loads session config with cancellation and timeout support.
func (s *ConfigServiceWithContext) LoadSessionConfigWithContext(ctx context.Context, sessionID string) (*SessionConfig, error) {
	ctx, cancel, err := s.contextWithTimeout(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.LoadSessionConfig(sessionID), nil
}

// LoadRoleConfigWithContext loads role config with cancellation and timeout support.
func (s *ConfigServiceWithContext) LoadRoleConfigWithContext(ctx context.Context, role RoleType) (*RoleConfig, error) {
	ctx, cancel, err := s.contextWithTimeout(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.LoadRoleConfig(role), nil
}

// ResolveConfigWithContext resolves config with cancellation and timeout support.
func (s *ConfigServiceWithContext) ResolveConfigWithContext(ctx context.Context, sessionID string, role RoleType) (*ResolvedConfig, error) {
	ctx, cancel, err := s.contextWithTimeout(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.ResolveConfig(sessionID, role), nil
}

func (s *ConfigServiceWithContext) contextWithTimeout(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if s == nil || s.IConfigService == nil {
		return nil, nil, fmt.Errorf("config service is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ctx, func() {}, err
	}
	timedCtx, cancel := context.WithTimeout(ctx, s.timeout)
	return timedCtx, cancel, nil
}

// PAPI methods for SQLiteConfigService

// GetPAPIManager returns the PAPI manager instance.
func (s *SQLiteConfigService) GetPAPIManager() *PAPIManager {
	return s.papiManager
}

// PAPIDiagnostics returns the recorded PAPI diagnostics, currently the
// category conflicts detected while loading persisted data (E-10). Legacy
// conflicting data stays loadable; the conflict is surfaced here and by
// DetectConflicts instead of being silently resolved or deleted.
func (s *SQLiteConfigService) PAPIDiagnostics() []string {
	s.papiMu.Lock()
	defer s.papiMu.Unlock()
	return append([]string(nil), s.papiDiagnostics...)
}

// mutatePAPIMapping is the single PAPI write pipeline (E-09): under the
// service-level mutation lock it builds and validates a candidate snapshot
// from the live mapping, persists that snapshot in one SQL transaction, and
// only then publishes it to memory. A validation or persistence failure
// leaves the visible snapshot and the stored rows exactly as they were, and
// concurrent mutations serialize, so memory and the database cannot diverge.
// Lock order: papiMu is taken first; BuildCandidateMapping/publishMapping and
// DetectConflicts acquire PAPIManager.mu underneath it (see SQLiteConfigService).
func (s *SQLiteConfigService) mutatePAPIMapping(mutate func(*PAPIMapping) error) error {
	s.papiMu.Lock()
	defer s.papiMu.Unlock()
	candidate, err := s.papiManager.BuildCandidateMapping(mutate)
	if err != nil {
		return err
	}
	if err := s.replacePAPIVariablesInDB(candidate); err != nil {
		return err
	}
	s.papiManager.publishMapping(candidate)
	// A successfully published candidate is conflict-free by construction;
	// recompute so post-mutation diagnostics always describe the live mapping.
	s.papiDiagnostics = s.papiManager.DetectConflicts()
	return nil
}

// replacePAPIVariablesInDB rewrites the papi_variables table to exactly the
// candidate snapshot inside one transaction: all effects commit together or
// none do, so the stored mapping always equals the last published snapshot.
// It runs under papiMu and deliberately does not take s.mu (see lock order on
// SQLiteConfigService); papiMu alone serializes every writer of this table.
func (s *SQLiteConfigService) replacePAPIVariablesInDB(mapping *PAPIMapping) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin papi variables tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once Commit succeeds
	if _, err := tx.Exec("DELETE FROM papi_variables"); err != nil {
		return fmt.Errorf("clear papi variables: %w", err)
	}
	names := make([]string, 0, len(mapping.Variables))
	for name := range mapping.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		variableJSON, err := json.Marshal(mapping.Variables[name])
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			"INSERT INTO papi_variables (name, variable_json) VALUES (?, ?)",
			name, string(variableJSON),
		); err != nil {
			return fmt.Errorf("write papi variable %q: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit papi variables tx: %w", err)
	}
	return nil
}

// DefinePAPIVariable defines (or replaces) a PAPI variable and persists the
// resulting snapshot. A category that another variable already claims
// produces a *CategoryConflictError and writes nothing (E-10).
func (s *SQLiteConfigService) DefinePAPIVariable(variable *PAPIVariable) error {
	if variable == nil {
		return errors.New("variable cannot be nil")
	}
	if variable.Name == "" {
		return errors.New("variable name cannot be empty")
	}
	v := clonePAPIVariable(variable)
	return s.mutatePAPIMapping(func(m *PAPIMapping) error {
		m.Variables[v.Name] = v
		return nil
	})
}

// DeletePAPIVariable deletes a PAPI variable and persists the resulting
// snapshot. A missing name is an explicit error and changes nothing.
func (s *SQLiteConfigService) DeletePAPIVariable(name string) error {
	return s.mutatePAPIMapping(func(m *PAPIMapping) error {
		if _, ok := m.Variables[name]; !ok {
			return fmt.Errorf("variable %s not found", name)
		}
		delete(m.Variables, name)
		return nil
	})
}

// HotSwapPAPI performs hot swap and persists the resulting snapshot. The
// variable keeps its existing name; a swap that would claim another
// variable's category produces a *CategoryConflictError and changes nothing.
func (s *SQLiteConfigService) HotSwapPAPI(varName string, newVariable *PAPIVariable) error {
	if newVariable == nil {
		return errors.New("new variable cannot be nil")
	}
	replacement := clonePAPIVariable(newVariable)
	replacement.Name = varName
	return s.mutatePAPIMapping(func(m *PAPIMapping) error {
		if _, ok := m.Variables[varName]; !ok {
			return fmt.Errorf("variable %s not found", varName)
		}
		m.Variables[varName] = replacement
		return nil
	})
}

// loadPAPIFromDB loads PAPI variables from database. The load path applies
// the same normalization rule as writes (LoadMapping normalizes categories),
// and persisted category conflicts are allowed to load: they are recorded in
// papiDiagnostics and stay resolvable for non-conflicted categories (E-10).
// Called once during construction; it takes papiMu only to keep the
// documented lock order uniform.
func (s *SQLiteConfigService) loadPAPIFromDB() error {
	rows, err := s.db.Query("SELECT name, variable_json FROM papi_variables")
	if err != nil {
		return fmt.Errorf("load PAPI variables: %w", err)
	}
	defer rows.Close()

	mapping := &PAPIMapping{Variables: make(map[string]*PAPIVariable)}
	for rows.Next() {
		var name, variableJSON string
		if err := rows.Scan(&name, &variableJSON); err != nil {
			continue
		}
		var variable PAPIVariable
		if err := json.Unmarshal([]byte(variableJSON), &variable); err == nil {
			mapping.Variables[name] = &variable
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate PAPI variables: %w", err)
	}

	s.papiMu.Lock()
	defer s.papiMu.Unlock()
	if err := s.papiManager.LoadMapping(mapping); err != nil {
		return fmt.Errorf("load PAPI mapping: %w", err)
	}
	s.papiDiagnostics = s.papiManager.DetectConflicts()
	return nil
}
