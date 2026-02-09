package memory

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestUserProfileValidate(t *testing.T) {
	valid := &UserProfile{
		UserID:      "user-1",
		LastUpdated: 1739000000,
		Sections: UserProfileSections{
			Preferences:        "偏好简洁回答",
			Background:         "后端工程师",
			Expertise:          []string{"Go", "SQLite"},
			CommunicationStyle: "结构化",
			Goals:              []string{"提升代码质量", "减少回归"},
		},
		Metadata: UserProfileMetadata{
			TotalSessions: 5,
			TotalMessages: 120,
			LastActive:    1739000100,
		},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid user profile, got error: %v", err)
	}

	invalidUserID := *valid
	invalidUserID.UserID = "  "
	if err := invalidUserID.Validate(); err == nil {
		t.Fatalf("expected error when user_id is empty")
	}

	invalidUpdated := *valid
	invalidUpdated.LastUpdated = 0
	if err := invalidUpdated.Validate(); err == nil {
		t.Fatalf("expected error when last_updated <= 0")
	}

	invalidSessions := *valid
	invalidSessions.Metadata.TotalSessions = -1
	if err := invalidSessions.Validate(); err == nil {
		t.Fatalf("expected error when total_sessions is negative")
	}
}

func TestUserProfileJSONHelpers(t *testing.T) {
	profile := &UserProfile{
		UserID:      "user-2",
		LastUpdated: 1739000200,
		Sections: UserProfileSections{
			Preferences:        "中文技术术语",
			Background:         "全栈开发",
			Expertise:          []string{"TypeScript"},
			CommunicationStyle: "分步骤",
			Goals:              []string{"高效交付"},
		},
		Metadata: UserProfileMetadata{
			TotalSessions: 2,
			TotalMessages: 30,
			LastActive:    1739000300,
		},
	}

	sectionsJSON, err := profile.SectionsJSON()
	if err != nil {
		t.Fatalf("SectionsJSON failed: %v", err)
	}
	if sectionsJSON == "" {
		t.Fatalf("expected sections json not empty")
	}

	metadataJSON, err := profile.MetadataJSON()
	if err != nil {
		t.Fatalf("MetadataJSON failed: %v", err)
	}
	if metadataJSON == "" {
		t.Fatalf("expected metadata json not empty")
	}
}

func TestEnsureUserProfileSchema(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "user_profile_schema_test")
	if err != nil {
		t.Fatalf("create temp dir failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "user_profile.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := EnsureUserProfileSchema(ctx, db); err != nil {
		t.Fatalf("EnsureUserProfileSchema failed: %v", err)
	}

	if err := EnsureUserProfileSchema(ctx, db); err != nil {
		t.Fatalf("EnsureUserProfileSchema second run failed: %v", err)
	}

	var tableName string
	err = db.QueryRowContext(ctx, `
		SELECT name FROM sqlite_master WHERE type='table' AND name='user_profiles'
	`).Scan(&tableName)
	if err != nil {
		t.Fatalf("query user_profiles table failed: %v", err)
	}
	if tableName != "user_profiles" {
		t.Fatalf("unexpected table name: %s", tableName)
	}
}
