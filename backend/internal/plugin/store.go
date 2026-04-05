package plugin

import (
	"context"

	"github.com/codeflow/backend/internal/integration"
)

// Store keeps plugin reads rooted in the governed integration registry.
type Store struct {
	integration integration.IIntegrationService
}

// NewStore creates a plugin store backed by the provided governed integration service.
func NewStore(integrationSvc integration.IIntegrationService) *Store {
	if integrationSvc == nil {
		integrationSvc = integration.GetIntegrationService()
	}
	return &Store{integration: integrationSvc}
}

// ListByType lists governed integrations for a specific plugin-facing type.
func (s *Store) ListByType(ctx context.Context, integrationType integration.IntegrationType) ([]integration.Integration, error) {
	items, err := s.integration.List(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]integration.Integration, 0, len(items))
	for _, item := range items {
		if item.Manifest.Type == integrationType {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

// Register clones or persists a governed plugin integration record.
func (s *Store) Register(ctx context.Context, req *integration.RegisterIntegrationRequest) (*integration.Integration, error) {
	return s.integration.Register(ctx, req)
}
