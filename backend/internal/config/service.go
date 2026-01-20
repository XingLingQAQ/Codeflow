// Package config - Config service layer
package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "github.com/mattn/go-sqlite3"
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
}

// NewSQLiteConfigService creates a new SQLite-backed config service.
func NewSQLiteConfigService(dbPath string) (*SQLiteConfigService, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Create tables
	if err := createConfigTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	svc := &SQLiteConfigService{
		ConfigManager: NewConfigManager(nil),
		papiManager:   NewPAPIManager(),
		db:            db,
	}

	// Load existing config from database
	if err := svc.loadFromDB(); err != nil {
		db.Close()
		return nil, fmt.Errorf("load from database: %w", err)
	}

	// Load PAPI variables from database
	if err := svc.loadPAPIFromDB(); err != nil {
		db.Close()
		return nil, fmt.Errorf("load PAPI from database: %w", err)
	}

	// Register change callback to persist to database
	svc.OnConfigChange(func(config *ConfigHierarchy) {
		_ = svc.saveToDB(config)
	})

	return svc, nil
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
	if err := s.ConfigManager.SaveGlobalConfig(config); err != nil {
		return err
	}
	return s.saveGlobalConfigToDB(config)
}

// SaveSessionConfig overrides to persist to SQLite.
func (s *SQLiteConfigService) SaveSessionConfig(config *SessionConfig) error {
	if err := s.ConfigManager.SaveSessionConfig(config); err != nil {
		return err
	}
	return s.saveSessionConfigToDB(config)
}

// SaveRoleConfig overrides to persist to SQLite.
func (s *SQLiteConfigService) SaveRoleConfig(role RoleType, config *RoleConfig) error {
	if err := s.ConfigManager.SaveRoleConfig(role, config); err != nil {
		return err
	}
	return s.saveRoleConfigToDB(role, config)
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
		if err := json.Unmarshal([]byte(globalJSON), &global); err == nil {
			s.ConfigManager.globalConfig = global
		}
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
			continue
		}
		var session SessionConfig
		if err := json.Unmarshal([]byte(configJSON), &session); err == nil {
			s.ConfigManager.sessionConfigs[sessionID] = &session
		}
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
			continue
		}
		var role RoleConfig
		if err := json.Unmarshal([]byte(configJSON), &role); err == nil {
			s.ConfigManager.roleConfigs[RoleType(roleStr)] = &role
		}
	}

	return nil
}

func (s *SQLiteConfigService) saveToDB(config *ConfigHierarchy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Save global config
	globalJSON, err := json.Marshal(config.Global)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO global_config (id, config_json) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET config_json = ?, updated_at = CURRENT_TIMESTAMP
	`, string(globalJSON), string(globalJSON))
	if err != nil {
		return err
	}

	// Save session config
	if config.Session != nil {
		sessionJSON, err := json.Marshal(config.Session)
		if err != nil {
			return err
		}

		_, err = tx.Exec(`
			INSERT INTO session_config (session_id, config_json) VALUES (?, ?)
			ON CONFLICT(session_id) DO UPDATE SET config_json = ?, updated_at = CURRENT_TIMESTAMP
		`, config.Session.SessionID, string(sessionJSON), string(sessionJSON))
		if err != nil {
			return err
		}
	}

	// Save role configs
	if config.Role != nil {
		for role, roleCfg := range config.Role {
			roleJSON, err := json.Marshal(roleCfg)
			if err != nil {
				continue
			}

			_, err = tx.Exec(`
				INSERT INTO role_config (role, config_json) VALUES (?, ?)
				ON CONFLICT(role) DO UPDATE SET config_json = ?, updated_at = CURRENT_TIMESTAMP
			`, string(role), string(roleJSON), string(roleJSON))
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
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

// WithContext wraps the service with context support (for future use).
type ConfigServiceWithContext struct {
	IConfigService
}

// NewConfigServiceWithContext creates a context-aware config service.
func NewConfigServiceWithContext(svc IConfigService) *ConfigServiceWithContext {
	return &ConfigServiceWithContext{IConfigService: svc}
}

// ResolveConfigWithContext resolves config with context (placeholder for future enhancements).
func (s *ConfigServiceWithContext) ResolveConfigWithContext(ctx context.Context, sessionID string, role RoleType) *ResolvedConfig {
	// TODO: Add context-aware features (timeout, cancellation, etc.)
	return s.ResolveConfig(sessionID, role)
}

// PAPI methods for SQLiteConfigService

// GetPAPIManager returns the PAPI manager instance.
func (s *SQLiteConfigService) GetPAPIManager() *PAPIManager {
	return s.papiManager
}

// DefinePAPIVariable defines a PAPI variable and persists it.
func (s *SQLiteConfigService) DefinePAPIVariable(variable *PAPIVariable) error {
	if err := s.papiManager.DefineVariable(variable); err != nil {
		return err
	}
	return s.savePAPIVariableToDB(variable)
}

// DeletePAPIVariable deletes a PAPI variable and removes it from database.
func (s *SQLiteConfigService) DeletePAPIVariable(name string) error {
	if !s.papiManager.DeleteVariable(name) {
		return fmt.Errorf("variable %s not found", name)
	}
	return s.deletePAPIVariableFromDB(name)
}

// HotSwapPAPI performs hot swap and persists the change.
func (s *SQLiteConfigService) HotSwapPAPI(varName string, newVariable *PAPIVariable) error {
	if err := s.papiManager.HotSwap(varName, newVariable); err != nil {
		return err
	}
	return s.savePAPIVariableToDB(newVariable)
}

// loadPAPIFromDB loads PAPI variables from database.
func (s *SQLiteConfigService) loadPAPIFromDB() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query("SELECT name, variable_json FROM papi_variables")
	if err != nil {
		return fmt.Errorf("load PAPI variables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name, variableJSON string
		if err := rows.Scan(&name, &variableJSON); err != nil {
			continue
		}
		var variable PAPIVariable
		if err := json.Unmarshal([]byte(variableJSON), &variable); err == nil {
			s.papiManager.DefineVariable(&variable)
		}
	}

	return nil
}

// savePAPIVariableToDB saves a PAPI variable to database.
func (s *SQLiteConfigService) savePAPIVariableToDB(variable *PAPIVariable) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	variableJSON, err := json.Marshal(variable)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO papi_variables (name, variable_json) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET variable_json = ?, updated_at = CURRENT_TIMESTAMP
	`, variable.Name, string(variableJSON), string(variableJSON))

	return err
}

// deletePAPIVariableFromDB deletes a PAPI variable from database.
func (s *SQLiteConfigService) deletePAPIVariableFromDB(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM papi_variables WHERE name = ?", name)
	return err
}
