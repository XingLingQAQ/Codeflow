package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// UserProfile 用户画像。
type UserProfile struct {
	UserID      string             `json:"user_id"`
	LastUpdated int64              `json:"last_updated"`
	Sections    UserProfileSections `json:"sections"`
	Metadata    UserProfileMetadata `json:"metadata"`
}

// UserProfileSections 用户画像分区。
type UserProfileSections struct {
	Preferences       string   `json:"preferences"`
	Background        string   `json:"background"`
	Expertise         []string `json:"expertise"`
	CommunicationStyle string  `json:"communication_style"`
	Goals             []string `json:"goals"`
}

// UserProfileMetadata 用户画像元数据。
type UserProfileMetadata struct {
	TotalSessions int   `json:"total_sessions"`
	TotalMessages int   `json:"total_messages"`
	LastActive    int64 `json:"last_active"`
}

// CreateUserProfilesTableSQL 用户画像 SQLite schema。
// sections_json/metadata_json 使用 JSON 文本存储，便于后续扩展字段。
const CreateUserProfilesTableSQL = `
CREATE TABLE IF NOT EXISTS user_profiles (
  user_id TEXT PRIMARY KEY,
  last_updated INTEGER NOT NULL,
  sections_json TEXT NOT NULL DEFAULT '{}',
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
  updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_user_profiles_last_updated
  ON user_profiles(last_updated DESC);
`

// EnsureUserProfileSchema 初始化/迁移用户画像表结构。
func EnsureUserProfileSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("user profile schema init failed: db is nil")
	}

	if _, err := db.ExecContext(ctx, CreateUserProfilesTableSQL); err != nil {
		return fmt.Errorf("create user profile schema: %w", err)
	}

	return nil
}

// Validate 校验 UserProfile 基本字段边界。
func (p *UserProfile) Validate() error {
	if p == nil {
		return errors.New("user profile is nil")
	}
	if strings.TrimSpace(p.UserID) == "" {
		return errors.New("user_id is required")
	}
	if p.LastUpdated <= 0 {
		return errors.New("last_updated must be positive")
	}
	if p.Metadata.TotalSessions < 0 {
		return errors.New("total_sessions must be non-negative")
	}
	if p.Metadata.TotalMessages < 0 {
		return errors.New("total_messages must be non-negative")
	}
	if p.Metadata.LastActive < 0 {
		return errors.New("last_active must be non-negative")
	}

	if _, err := json.Marshal(p.Sections); err != nil {
		return fmt.Errorf("invalid sections: %w", err)
	}
	if _, err := json.Marshal(p.Metadata); err != nil {
		return fmt.Errorf("invalid metadata: %w", err)
	}

	return nil
}

// SectionsJSON 将 sections 编码为 JSON 文本。
func (p *UserProfile) SectionsJSON() (string, error) {
	if p == nil {
		return "", errors.New("user profile is nil")
	}
	b, err := json.Marshal(p.Sections)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// MetadataJSON 将 metadata 编码为 JSON 文本。
func (p *UserProfile) MetadataJSON() (string, error) {
	if p == nil {
		return "", errors.New("user profile is nil")
	}
	b, err := json.Marshal(p.Metadata)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
