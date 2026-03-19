// Package integration - Governed integration service implementation.
package integration

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/snapshot"
)

// InMemoryIntegrationService is an in-memory governed integration service.
type InMemoryIntegrationService struct {
	mu           sync.RWMutex
	integrations map[string]*Integration
	invocations  map[string]*IntegrationInvocation
}

// NewInMemoryIntegrationService creates a new integration service.
func NewInMemoryIntegrationService() *InMemoryIntegrationService {
	return &InMemoryIntegrationService{
		integrations: make(map[string]*Integration),
		invocations:  make(map[string]*IntegrationInvocation),
	}
}

// Register registers a governed integration.
func (s *InMemoryIntegrationService) Register(ctx context.Context, req *RegisterIntegrationRequest) (*Integration, error) {
	if req == nil {
		return nil, ErrInvalidManifest
	}
	if err := validateRegistration(req); err != nil {
		return nil, err
	}

	integration := &Integration{
		ID:        uuid.NewString(),
		Manifest:  req.Manifest,
		Signature: req.Signature,
		Policy:    req.Policy,
		CreatedAt: time.Now(),
		CreatedBy: req.Actor,
	}

	if err := logAudit(ctx, audit.EventCreate, audit.SeverityInfo, req.Actor, audit.OutcomeSuccess, audit.AuditResource{
		Type: "integration",
		ID:   integration.ID,
		Name: integration.Manifest.Name,
	}, "integration.register", map[string]interface{}{
		"type":         integration.Manifest.Type,
		"distribution": integration.Manifest.Distribution,
		"hook_name":    integration.Manifest.HookName,
	}); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.integrations[integration.ID] = integration
	s.mu.Unlock()

	copyValue := *integration
	return &copyValue, nil
}

// List lists registered integrations.
func (s *InMemoryIntegrationService) List(ctx context.Context) ([]Integration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Integration, 0, len(s.integrations))
	for _, item := range s.integrations {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

// Get gets one integration by ID.
func (s *InMemoryIntegrationService) Get(ctx context.Context, id string) (*Integration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, ok := s.integrations[id]
	if !ok {
		return nil, ErrIntegrationNotFound
	}
	copyValue := *item
	return &copyValue, nil
}

// Invoke invokes one integration through the shared executor substrate.
func (s *InMemoryIntegrationService) Invoke(ctx context.Context, id string, req *InvokeIntegrationRequest) (*InvocationResult, error) {
	if req == nil {
		return nil, ErrPermissionDenied
	}

	integration, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := authorize(integration.Policy, req.Actor); err != nil {
		_ = logAudit(ctx, audit.EventSecurity, audit.SeverityWarning, req.Actor, audit.OutcomeFailure, audit.AuditResource{
			Type: "integration",
			ID:   integration.ID,
			Name: integration.Manifest.Name,
		}, "integration.invoke.denied", map[string]interface{}{
			"reason": err.Error(),
		})
		return nil, err
	}

	snapshotSvc := snapshot.GetSnapshotService()
	if snapshotSvc == nil {
		return nil, fmt.Errorf("snapshot service not available")
	}

	hookMgr := hooks.GetHookManager()
	if hookMgr == nil {
		return nil, fmt.Errorf("hook manager not available")
	}

	snap, err := snapshotSvc.Create(ctx, &snapshot.SnapshotCreateRequest{
		Description: req.Description,
		SessionID:   req.SessionID,
		Tags:        append([]string{"integration", string(integration.Manifest.Type), integration.Manifest.Name}, req.Tags...),
	})
	if err != nil {
		return nil, err
	}

	output, err := hookMgr.TriggerHook(ctx, integration.Manifest.HookName, req.Payload)
	outcome := audit.OutcomeSuccess
	severity := audit.SeverityInfo
	if err != nil {
		outcome = audit.OutcomeFailure
		severity = audit.SeverityError
	}

	auditEntryID, logErr := logAuditWithID(ctx, audit.EventAccess, severity, req.Actor, outcome, audit.AuditResource{
		Type: "integration",
		ID:   integration.ID,
		Name: integration.Manifest.Name,
	}, "integration.invoke", map[string]interface{}{
		"hook_name":   integration.Manifest.HookName,
		"snapshot_id": snap.ID,
		"session_id":  req.SessionID,
		"type":        integration.Manifest.Type,
	})
	if logErr != nil {
		return nil, logErr
	}
	if err != nil {
		return nil, err
	}

	invocation := &IntegrationInvocation{
		ID:            uuid.NewString(),
		IntegrationID: integration.ID,
		SnapshotID:    snap.ID,
		AuditEntryID:  auditEntryID,
		SessionID:     req.SessionID,
		Payload:       req.Payload,
		Output:        output,
		Actor:         req.Actor,
		InvokedAt:     time.Now(),
	}

	s.mu.Lock()
	s.invocations[invocation.ID] = invocation
	s.mu.Unlock()

	return &InvocationResult{
		InvocationID:  invocation.ID,
		IntegrationID: invocation.IntegrationID,
		SnapshotID:    invocation.SnapshotID,
		AuditEntryID:  invocation.AuditEntryID,
		Output:        invocation.Output,
		InvokedAt:     invocation.InvokedAt,
	}, nil
}

// Replay restores the original snapshot and re-invokes the integration.
func (s *InMemoryIntegrationService) Replay(ctx context.Context, id string, req *ReplayIntegrationRequest) (*ReplayResult, error) {
	if req == nil || req.InvocationID == "" {
		return nil, ErrInvocationNotFound
	}

	integration, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := authorize(integration.Policy, req.Actor); err != nil {
		return nil, err
	}

	s.mu.RLock()
	previous, ok := s.invocations[req.InvocationID]
	s.mu.RUnlock()
	if !ok || previous.IntegrationID != id {
		return nil, ErrInvocationNotFound
	}

	snapshotSvc := snapshot.GetSnapshotService()
	if snapshotSvc == nil {
		return nil, fmt.Errorf("snapshot service not available")
	}

	restoreResult, err := snapshotSvc.Restore(ctx, previous.SnapshotID)
	if err != nil {
		return nil, err
	}

	invocation, err := s.Invoke(ctx, id, &InvokeIntegrationRequest{
		Actor:       req.Actor,
		Payload:     previous.Payload,
		SessionID:   previous.SessionID,
		Description: fmt.Sprintf("replay integration %s from invocation %s", integration.Manifest.Name, previous.ID),
		Tags:        []string{"replay", previous.ID},
	})
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if created, ok := s.invocations[invocation.InvocationID]; ok {
		created.ReplayOf = previous.ID
	}
	s.mu.Unlock()
	invocation.ReplayOf = previous.ID
	invocation.Replayed = true

	return &ReplayResult{
		RestoredSnapshotID: previous.SnapshotID,
		Restore:            restoreResult,
		Invocation:         invocation,
	}, nil
}

func validateRegistration(req *RegisterIntegrationRequest) error {
	if req.Manifest.Name == "" || req.Manifest.Version == "" || req.Manifest.Type == "" || req.Manifest.HookName == "" {
		return ErrInvalidManifest
	}
	if req.Signature.Algorithm == "" || req.Signature.Value == "" || !req.Signature.Verified {
		return ErrInvalidManifest
	}
	if req.Policy.RequireAudit && audit.GetAuditService() == nil {
		return fmt.Errorf("audit service not available")
	}
	if len(req.Policy.AllowedActorTypes) == 0 {
		return ErrInvalidManifest
	}
	if req.Manifest.Distribution == DistributionThirdParty && !req.Policy.AllowThirdPartyDistribution {
		return ErrPermissionDenied
	}
	if hooks.GetHookManager() == nil {
		return fmt.Errorf("hook manager not available")
	}
	if _, err := hooks.GetHookManager().GetHook(req.Manifest.HookName); err != nil {
		return err
	}
	return nil
}

func authorize(policy Policy, actor audit.AuditActor) error {
	if len(policy.AllowedActorTypes) == 0 {
		return nil
	}
	for _, allowed := range policy.AllowedActorTypes {
		if allowed == actor.Type {
			return nil
		}
	}
	return ErrPermissionDenied
}

func logAudit(ctx context.Context, eventType audit.AuditEventType, severity audit.AuditSeverity, actor audit.AuditActor, outcome audit.AuditOutcome, resource audit.AuditResource, action string, details map[string]interface{}) error {
	_, err := logAuditWithID(ctx, eventType, severity, actor, outcome, resource, action, details)
	return err
}

func logAuditWithID(ctx context.Context, eventType audit.AuditEventType, severity audit.AuditSeverity, actor audit.AuditActor, outcome audit.AuditOutcome, resource audit.AuditResource, action string, details map[string]interface{}) (string, error) {
	auditSvc := audit.GetAuditService()
	if auditSvc == nil {
		return "", fmt.Errorf("audit service not available")
	}
	entry := &audit.AuditLogEntry{
		EventType: eventType,
		Severity:  severity,
		Actor:     actor,
		Resource:  resource,
		Action:    action,
		Outcome:   outcome,
		Details:   details,
	}
	if err := auditSvc.Log(ctx, entry); err != nil {
		return "", err
	}
	return entry.ID, nil
}

var defaultIntegrationService IIntegrationService
var integrationMu sync.RWMutex

// GetIntegrationService returns the global integration service.
func GetIntegrationService() IIntegrationService {
	integrationMu.RLock()
	if defaultIntegrationService != nil {
		defer integrationMu.RUnlock()
		return defaultIntegrationService
	}
	integrationMu.RUnlock()

	integrationMu.Lock()
	defer integrationMu.Unlock()
	if defaultIntegrationService == nil {
		defaultIntegrationService = NewInMemoryIntegrationService()
	}
	return defaultIntegrationService
}

// SetIntegrationService sets the global integration service.
func SetIntegrationService(svc IIntegrationService) {
	integrationMu.Lock()
	defer integrationMu.Unlock()
	defaultIntegrationService = svc
}
