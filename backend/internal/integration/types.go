// Package integration provides a governed open integration surface.
package integration

import (
	"context"
	"errors"
	"time"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/snapshot"
)

// IntegrationType represents the supported integration class.
type IntegrationType string

const (
	IntegrationTypeWebhook     IntegrationType = "webhook"
	IntegrationTypePlugin      IntegrationType = "plugin"
	IntegrationTypeVCS         IntegrationType = "vcs"
	IntegrationTypeMarketplace IntegrationType = "marketplace"
)

// DistributionMode represents how an integration is distributed.
type DistributionMode string

const (
	DistributionInternal   DistributionMode = "internal"
	DistributionThirdParty DistributionMode = "third_party"
)

var (
	// ErrInvalidManifest indicates registration metadata is incomplete or unsafe.
	ErrInvalidManifest = errors.New("invalid integration manifest")
	// ErrIntegrationNotFound indicates the integration ID does not exist.
	ErrIntegrationNotFound = errors.New("integration not found")
	// ErrInvocationNotFound indicates the invocation ID does not exist.
	ErrInvocationNotFound = errors.New("integration invocation not found")
	// ErrPermissionDenied indicates the actor is not allowed by policy.
	ErrPermissionDenied = errors.New("integration permission denied")
)

// Manifest defines the governed integration boundary.
type Manifest struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description,omitempty"`
	Type         IntegrationType        `json:"type"`
	HookName     string                 `json:"hook_name"`
	Distribution DistributionMode       `json:"distribution"`
	Capabilities []string               `json:"capabilities,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Signature records the manifest signature requirement.
type Signature struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
	Verified  bool   `json:"verified"`
}

// Policy defines permission and audit requirements for invocation.
type Policy struct {
	AllowedActorTypes           []string `json:"allowed_actor_types"`
	RequireAudit                bool     `json:"require_audit"`
	AllowThirdPartyDistribution bool     `json:"allow_third_party_distribution"`
}

// Integration represents a registered governed integration.
type Integration struct {
	ID        string           `json:"id"`
	Manifest  Manifest         `json:"manifest"`
	Signature Signature        `json:"signature"`
	Policy    Policy           `json:"policy"`
	CreatedAt time.Time        `json:"created_at"`
	CreatedBy audit.AuditActor `json:"created_by"`
}

// RegisterIntegrationRequest describes integration registration input.
type RegisterIntegrationRequest struct {
	Manifest  Manifest         `json:"manifest"`
	Signature Signature        `json:"signature"`
	Policy    Policy           `json:"policy"`
	Actor     audit.AuditActor `json:"actor"`
}

// InvokeIntegrationRequest describes an integration invocation request.
type InvokeIntegrationRequest struct {
	Actor       audit.AuditActor `json:"actor"`
	Payload     interface{}      `json:"payload"`
	SessionID   string           `json:"session_id,omitempty"`
	Description string           `json:"description,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
}

// IntegrationInvocation stores a completed invocation for replay.
type IntegrationInvocation struct {
	ID            string           `json:"id"`
	IntegrationID string           `json:"integration_id"`
	SnapshotID    string           `json:"snapshot_id"`
	AuditEntryID  string           `json:"audit_entry_id"`
	ReplayOf      string           `json:"replay_of,omitempty"`
	SessionID     string           `json:"session_id,omitempty"`
	Payload       interface{}      `json:"payload"`
	Output        interface{}      `json:"output"`
	Actor         audit.AuditActor `json:"actor"`
	InvokedAt     time.Time        `json:"invoked_at"`
}

// InvocationResult is returned to callers after a successful invoke or replay.
type InvocationResult struct {
	InvocationID  string      `json:"invocation_id"`
	IntegrationID string      `json:"integration_id"`
	SnapshotID    string      `json:"snapshot_id"`
	AuditEntryID  string      `json:"audit_entry_id"`
	ReplayOf      string      `json:"replay_of,omitempty"`
	Replayed      bool        `json:"replayed"`
	Output        interface{} `json:"output"`
	InvokedAt     time.Time   `json:"invoked_at"`
}

// ReplayIntegrationRequest describes a replay request.
type ReplayIntegrationRequest struct {
	Actor        audit.AuditActor `json:"actor"`
	InvocationID string           `json:"invocation_id"`
}

// ReplayResult returns restore evidence plus the new replay invocation.
type ReplayResult struct {
	RestoredSnapshotID string                  `json:"restored_snapshot_id"`
	Restore            *snapshot.RestoreResult `json:"restore"`
	Invocation         *InvocationResult       `json:"invocation"`
}

// IIntegrationService defines governed integration registration and execution.
type IIntegrationService interface {
	Register(ctx context.Context, req *RegisterIntegrationRequest) (*Integration, error)
	List(ctx context.Context) ([]Integration, error)
	Get(ctx context.Context, id string) (*Integration, error)
	Invoke(ctx context.Context, id string, req *InvokeIntegrationRequest) (*InvocationResult, error)
	Replay(ctx context.Context, id string, req *ReplayIntegrationRequest) (*ReplayResult, error)
}
