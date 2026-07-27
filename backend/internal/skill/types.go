// Package skill stores and matches agent Skill assets (prompt snippets / playbooks).
// Full market/versioning is M5; this is the minimal registry + match surface.
package skill

import (
	"context"
	"time"
)

// Source identifies where a skill came from.
type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceUser    Source = "user"
	SourcePlugin  Source = "plugin"
)

// Skill is a versioned prompt/playbook asset.
type Skill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Version     string    `json:"version"`
	Source      Source    `json:"source"`
	// Body is Markdown (or plain) content injected into prompts when matched.
	Body string `json:"body"`
	// Triggers are keywords/phrases used by Match (case-insensitive contains).
	Triggers []string `json:"triggers,omitempty"`
	// StageTags limit applicability (empty = any stage).
	StageTags []string `json:"stage_tags,omitempty"`
	// Enabled allows soft-disable without delete.
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateRequest creates a user skill.
type CreateRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Body        string   `json:"body" binding:"required"`
	Triggers    []string `json:"triggers,omitempty"`
	StageTags   []string `json:"stage_tags,omitempty"`
	Source      Source   `json:"source,omitempty"`
}

// UpdateRequest patches a skill.
type UpdateRequest struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Version     *string  `json:"version,omitempty"`
	Body        *string  `json:"body,omitempty"`
	Triggers    []string `json:"triggers,omitempty"`
	StageTags   []string `json:"stage_tags,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
}

// MatchRequest finds skills for a prompt / stage context.
type MatchRequest struct {
	Text      string `json:"text"`
	StageType string `json:"stage_type,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// MatchResult is a scored skill hit.
type MatchResult struct {
	Skill Skill    `json:"skill"`
	Score float64  `json:"score"`
	Hits  []string `json:"hits,omitempty"`
}

// SkillVersion is an archived snapshot of a skill captured before an Update.
// RowID identifies the snapshot for rollback (monotonic per registry / SQLite
// autoincrement). Skill holds the full prior payload.
type SkillVersion struct {
	RowID      int64     `json:"row_id"`
	SkillID    string    `json:"skill_id"`
	Version    string    `json:"version"`
	ArchivedAt time.Time `json:"archived_at"`
	Skill      *Skill    `json:"skill"`
}

// Registry is the skill asset service.
type Registry interface {
	Create(ctx context.Context, req *CreateRequest) (*Skill, error)
	Get(ctx context.Context, id string) (*Skill, error)
	List(ctx context.Context) ([]*Skill, error)
	// ListFiltered returns skills optionally filtered by stage tag.
	// includeDisabled controls whether disabled skills appear.
	ListFiltered(ctx context.Context, stage string, includeDisabled bool) ([]*Skill, error)
	Update(ctx context.Context, id string, req *UpdateRequest) (*Skill, error)
	Delete(ctx context.Context, id string) error
	Match(ctx context.Context, req *MatchRequest) ([]MatchResult, error)
	// RenderInjection concatenates matched skill bodies for prompt mounting.
	RenderInjection(ctx context.Context, req *MatchRequest) (string, error)
	// ImportMarkdownDir loads *.md files as skills (frontmatter optional).
	ImportMarkdownDir(ctx context.Context, dir string) (int, error)
	// ListVersions returns archived prior versions of a skill, newest first.
	ListVersions(ctx context.Context, skillID string) ([]SkillVersion, error)
	// RollbackVersion restores an archived snapshot (by row id) as a new update,
	// which in turn archives the current state. Builtins cannot be rolled back.
	RollbackVersion(ctx context.Context, skillID string, versionRowID int64) (*Skill, error)
}
