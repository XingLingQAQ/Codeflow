package plugin

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/integration"
)

const (
	metadataKeyPluginID         = "plugin_id"
	metadataKeyPluginDisplay    = "plugin_display_name"
	metadataKeyPluginSource     = "plugin_source"
	metadataKeyPluginScope      = "plugin_scope"
	metadataKeyPluginCategory   = "plugin_category"
	metadataKeyPluginFeatured   = "plugin_featured"
	metadataKeyPluginHomepage   = "plugin_homepage"
	metadataKeyPluginRepository = "plugin_repository"
	metadataKeyPluginIcon       = "plugin_icon"
	metadataKeyPluginAuthor     = "plugin_author"
	metadataKeyPluginVendor     = "plugin_vendor"
	metadataKeyPluginSummary    = "plugin_summary"
	metadataKeyPluginTags       = "plugin_tags"
	metadataKeyPluginDownloads  = "plugin_downloads"
	metadataKeyPluginInstalls   = "plugin_installs"
	metadataKeyPluginSessions   = "plugin_active_sessions"
	metadataKeyPluginVerified   = "plugin_verified"
)

// Service exposes a thin alias layer over integration + hooks + audit.
type Service struct {
	store *Store
}

var (
	defaultService *Service
	serviceMu      sync.RWMutex
)

// NewService creates a plugin alias service.
func NewService() *Service {
	return &Service{store: NewStore(nil)}
}

// GetPluginService returns the global plugin service.
func GetPluginService() *Service {
	serviceMu.RLock()
	if defaultService != nil {
		defer serviceMu.RUnlock()
		return defaultService
	}
	serviceMu.RUnlock()

	serviceMu.Lock()
	defer serviceMu.Unlock()
	if defaultService == nil {
		defaultService = NewService()
	}
	return defaultService
}

// SetPluginService sets the global plugin service.
func SetPluginService(svc *Service) {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	defaultService = svc
}

// ToggleRequest controls enabled state.
type ToggleRequest struct {
	Enabled bool
	Actor   audit.AuditActor
}

// ListInstalled returns installed plugins derived from plugin integrations.
func (s *Service) ListInstalled(ctx context.Context, params ListParams) (*PluginListResponse, error) {
	items, err := s.listByType(ctx, integration.IntegrationTypePlugin)
	if err != nil {
		return nil, err
	}

	filtered := make([]PluginManifest, 0, len(items))
	for _, item := range items {
		plugin, err := s.buildInstalledPlugin(item)
		if err != nil {
			return nil, err
		}
		if matchesPlugin(plugin, params) {
			filtered = append(filtered, plugin)
		}
	}

	total := len(filtered)
	paged := paginatePlugins(filtered, params)
	return &PluginListResponse{
		Plugins: paged,
		Total:   total,
		HasMore: hasMore(total, params),
		Summary: buildOverview(filtered),
	}, nil
}

// ListMarketplace returns marketplace-discoverable plugins derived from marketplace integrations.
func (s *Service) ListMarketplace(ctx context.Context, params ListParams) (*PluginCatalogResponse, error) {
	marketItems, err := s.listByType(ctx, integration.IntegrationTypeMarketplace)
	if err != nil {
		return nil, err
	}
	installedItems, err := s.listByType(ctx, integration.IntegrationTypePlugin)
	if err != nil {
		return nil, err
	}
	installedMap := map[string]integration.Integration{}
	for _, item := range installedItems {
		installedMap[publicPluginID(item)] = item
	}

	filtered := make([]PluginManifest, 0, len(marketItems))
	featured := make([]PluginManifest, 0)
	for _, item := range marketItems {
		plugin, err := s.buildMarketplacePlugin(item, installedMap[publicPluginID(item)])
		if err != nil {
			return nil, err
		}
		if matchesPlugin(plugin, ListParams{Search: params.Search}) {
			filtered = append(filtered, plugin)
			if plugin.Featured {
				featured = append(featured, plugin)
			}
		}
	}

	total := len(filtered)
	paged := paginatePlugins(filtered, params)
	return &PluginCatalogResponse{
		Plugins:  paged,
		Total:    total,
		Featured: featured,
	}, nil
}

// Get returns one installed plugin by its public plugin ID.
func (s *Service) Get(ctx context.Context, pluginID string) (*PluginDetailResponse, error) {
	item, err := s.findInstalledByPluginID(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	plugin, err := s.buildInstalledPlugin(item)
	if err != nil {
		return nil, err
	}
	return &PluginDetailResponse{Plugin: plugin}, nil
}

// Install clones a governed marketplace entry into a governed plugin entry.
func (s *Service) Install(ctx context.Context, pluginID string, actor audit.AuditActor) (*PluginDetailResponse, error) {
	if existing, err := s.findInstalledByPluginID(ctx, pluginID); err == nil {
		plugin, buildErr := s.buildInstalledPlugin(existing)
		if buildErr != nil {
			return nil, buildErr
		}
		return &PluginDetailResponse{Plugin: plugin}, nil
	}

	source, err := s.findMarketplaceByPluginID(ctx, pluginID)
	if err != nil {
		return nil, err
	}

	manifest := source.Manifest
	manifest.Type = integration.IntegrationTypePlugin
	manifest.Metadata = copyMetadata(source.Manifest.Metadata)
	if manifest.Metadata == nil {
		manifest.Metadata = map[string]interface{}{}
	}
	manifest.Metadata[metadataKeyPluginID] = pluginID
	if asString(manifest.Metadata[metadataKeyPluginSource]) == "" {
		manifest.Metadata[metadataKeyPluginSource] = "marketplace"
	}

	created, err := s.store.Register(ctx, &integration.RegisterIntegrationRequest{
		Manifest:  manifest,
		Signature: source.Signature,
		Policy:    source.Policy,
		Actor:     actor,
	})
	if err != nil {
		return nil, err
	}

	plugin, err := s.buildInstalledPlugin(*created)
	if err != nil {
		return nil, err
	}
	return &PluginDetailResponse{Plugin: plugin}, nil
}

// Toggle updates enabled state through the shared hook manager.
func (s *Service) Toggle(ctx context.Context, pluginID string, req ToggleRequest) (*PluginDetailResponse, error) {
	item, err := s.findInstalledByPluginID(ctx, pluginID)
	if err != nil {
		return nil, err
	}

	mgr := hooks.GetHookManager()
	if mgr == nil {
		return nil, fmt.Errorf("hook manager not available")
	}
	if req.Enabled {
		err = mgr.Enable(item.Manifest.HookName)
	} else {
		err = mgr.Disable(item.Manifest.HookName)
	}
	if err != nil {
		return nil, err
	}

	if _, err := audit.Record(ctx, &audit.AuditLogEntry{
		EventType: audit.EventModify,
		Severity:  audit.SeverityInfo,
		Actor:     req.Actor,
		Resource: audit.AuditResource{
			Type: "integration",
			ID:   item.ID,
			Name: item.Manifest.Name,
		},
		Action:  "plugin.toggle",
		Outcome: audit.OutcomeSuccess,
		Details: map[string]interface{}{"plugin_id": pluginID, "enabled": req.Enabled},
	}); err != nil {
		log.Printf("[WARN] plugin audit record failed: plugin_id=%s enabled=%t err=%v", pluginID, req.Enabled, err)
	}

	plugin, err := s.buildInstalledPlugin(item)
	if err != nil {
		return nil, err
	}
	plugin.Enabled = req.Enabled
	plugin.Health = healthFromEnabled(req.Enabled)
	return &PluginDetailResponse{Plugin: plugin}, nil
}

// QueryAudit returns audit entries for an installed plugin.
func (s *Service) QueryAudit(ctx context.Context, pluginID string, limit, offset int) (*audit.AuditQueryResult, error) {
	item, err := s.findInstalledByPluginID(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	auditSvc := audit.GetAuditService()
	if auditSvc == nil {
		return nil, fmt.Errorf("audit service not available")
	}
	return auditSvc.Query(ctx, &audit.AuditQuery{
		ResourceType: "integration",
		ResourceID:   item.ID,
		Limit:        limit,
		Offset:       offset,
	})
}

func (s *Service) listByType(ctx context.Context, integrationType integration.IntegrationType) ([]integration.Integration, error) {
	items, err := s.store.ListByType(ctx, integrationType)
	if err != nil {
		return nil, err
	}
	filtered := append([]integration.Integration(nil), items...)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
	})
	return filtered, nil
}

func (s *Service) findInstalledByPluginID(ctx context.Context, pluginID string) (integration.Integration, error) {
	items, err := s.listByType(ctx, integration.IntegrationTypePlugin)
	if err != nil {
		return integration.Integration{}, err
	}
	var matched integration.Integration
	found := false
	for _, item := range items {
		if publicPluginID(item) == pluginID {
			matched = item
			found = true
		}
	}
	if !found {
		return integration.Integration{}, integration.ErrIntegrationNotFound
	}
	return matched, nil
}

func (s *Service) findMarketplaceByPluginID(ctx context.Context, pluginID string) (integration.Integration, error) {
	items, err := s.listByType(ctx, integration.IntegrationTypeMarketplace)
	if err != nil {
		return integration.Integration{}, err
	}
	for _, item := range items {
		if publicPluginID(item) == pluginID {
			return item, nil
		}
	}
	return integration.Integration{}, integration.ErrIntegrationNotFound
}

func (s *Service) buildInstalledPlugin(item integration.Integration) (PluginManifest, error) {
	hook, err := hooks.GetHookManager().GetHook(item.Manifest.HookName)
	if err != nil {
		return PluginManifest{}, err
	}
	plugin := buildBasePlugin(item)
	plugin.Source = normalizeInstalledSource(plugin.Source)
	plugin.Enabled = hook.Config.Enabled
	plugin.Installed = true
	plugin.Health = healthFromEnabled(hook.Config.Enabled)
	plugin.Config = buildConfigFields(hook.Config)
	plugin.Permissions = buildPermissions(item)
	return plugin, nil
}

func (s *Service) buildMarketplacePlugin(item integration.Integration, installed integration.Integration) (PluginManifest, error) {
	plugin := buildBasePlugin(item)
	plugin.Source = normalizeMarketplaceSource(plugin.Source)
	plugin.Installed = installed.ID != ""
	plugin.Permissions = buildPermissions(item)
	plugin.Health = "healthy"
	if installed.ID != "" {
		hook, err := hooks.GetHookManager().GetHook(installed.Manifest.HookName)
		if err != nil {
			return PluginManifest{}, err
		}
		plugin.Enabled = hook.Config.Enabled
		plugin.Health = healthFromEnabled(hook.Config.Enabled)
	}
	return plugin, nil
}

func buildBasePlugin(item integration.Integration) PluginManifest {
	meta := item.Manifest.Metadata
	plugin := PluginManifest{
		ID:          publicPluginID(item),
		Name:        item.Manifest.Name,
		DisplayName: coalesce(asString(meta[metadataKeyPluginDisplay]), item.Manifest.Name),
		Summary:     coalesce(asString(meta[metadataKeyPluginSummary]), item.Manifest.Description),
		Description: item.Manifest.Description,
		Version:     item.Manifest.Version,
		Author:      asString(meta[metadataKeyPluginAuthor]),
		Vendor:      asString(meta[metadataKeyPluginVendor]),
		Category:    coalesce(asString(meta[metadataKeyPluginCategory]), "general"),
		Tags:        asStringSlice(meta[metadataKeyPluginTags], item.Manifest.Capabilities),
		Source:      asString(meta[metadataKeyPluginSource]),
		Scope:       coalesce(asString(meta[metadataKeyPluginScope]), "workspace"),
		Featured:    asBool(meta[metadataKeyPluginFeatured]),
		Verified:    item.Signature.Verified || asBool(meta[metadataKeyPluginVerified]),
		Homepage:    asString(meta[metadataKeyPluginHomepage]),
		Repository:  asString(meta[metadataKeyPluginRepository]),
		Icon:        asString(meta[metadataKeyPluginIcon]),
		UpdatedAt:   item.CreatedAt.Unix(),
		InstalledAt: item.CreatedAt.Unix(),
		Metrics: &PluginMetrics{
			Installs:       intValue(meta[metadataKeyPluginInstalls], 1),
			Downloads:      intValue(meta[metadataKeyPluginDownloads], 0),
			ActiveSessions: intValue(meta[metadataKeyPluginSessions], 0),
		},
		Metadata: copyMetadata(meta),
	}
	if plugin.DisplayName == "" {
		plugin.DisplayName = plugin.Name
	}
	if plugin.Summary == "" {
		plugin.Summary = plugin.Description
	}
	return plugin
}

func buildPermissions(item integration.Integration) []PluginPermission {
	permissions := make([]PluginPermission, 0, len(item.Policy.AllowedActorTypes)+2)
	for _, actorType := range item.Policy.AllowedActorTypes {
		permissions = append(permissions, PluginPermission{
			ID:          "actor:" + actorType,
			Name:        actorType,
			Description: "Allowed actor type",
			Level:       "write",
			Granted:     true,
			Required:    true,
		})
	}
	if item.Policy.RequireAudit {
		permissions = append(permissions, PluginPermission{
			ID:          "audit",
			Name:        "Audit logging",
			Description: "Plugin actions must be recorded to the audit chain",
			Level:       "admin",
			Granted:     true,
			Required:    true,
		})
	}
	if item.Manifest.Distribution == integration.DistributionThirdParty {
		permissions = append(permissions, PluginPermission{
			ID:          "distribution:third_party",
			Name:        "Third-party distribution",
			Description: "Plugin is distributed by a third-party source",
			Level:       "admin",
			Granted:     item.Policy.AllowThirdPartyDistribution,
			Required:    true,
		})
	}
	return permissions
}

func buildConfigFields(config hooks.HookConfig) []PluginConfigField {
	return []PluginConfigField{
		{Key: "hook_name", Label: "Hook name", Type: "string", Value: config.Name, Required: true, Description: "Bound governed hook"},
		{Key: "hook_type", Label: "Hook type", Type: "string", Value: string(config.Type), Required: true, Description: "Hook execution lane"},
		{Key: "priority", Label: "Priority", Type: "number", Value: config.Priority, Description: "Lower values run earlier"},
		{Key: "timeout_ms", Label: "Timeout", Type: "number", Value: config.Timeout.Milliseconds(), Description: "Hook timeout in milliseconds"},
		{Key: "retry_count", Label: "Retries", Type: "number", Value: config.RetryCount, Description: "Automatic retry attempts"},
	}
}

func buildOverview(plugins []PluginManifest) *PluginOverview {
	categories := make(map[string]int)
	enabled := 0
	unhealthy := 0
	marketplace := 0
	for _, plugin := range plugins {
		if plugin.Enabled {
			enabled++
		}
		if plugin.Health != "" && plugin.Health != "healthy" {
			unhealthy++
		}
		if plugin.Source == "marketplace" {
			marketplace++
		}
		category := plugin.Category
		if category == "" {
			category = "general"
		}
		categories[category]++
	}
	return &PluginOverview{
		Total:       len(plugins),
		Installed:   len(plugins),
		Enabled:     enabled,
		Marketplace: marketplace,
		Unhealthy:   unhealthy,
		Categories:  categories,
	}
}

func paginatePlugins(plugins []PluginManifest, params ListParams) []PluginManifest {
	if len(plugins) == 0 {
		return []PluginManifest{}
	}
	start := params.Offset
	if start < 0 {
		start = 0
	}
	if start >= len(plugins) {
		return []PluginManifest{}
	}
	end := len(plugins)
	if params.Limit > 0 && start+params.Limit < end {
		end = start + params.Limit
	}
	return plugins[start:end]
}

func hasMore(total int, params ListParams) bool {
	if params.Limit <= 0 {
		return false
	}
	return total > params.Offset+params.Limit
}

func matchesPlugin(plugin PluginManifest, params ListParams) bool {
	if params.Scope != "" && plugin.Scope != params.Scope {
		return false
	}
	if params.Status != "" {
		switch params.Status {
		case "enabled":
			if !plugin.Enabled {
				return false
			}
		case "disabled":
			if plugin.Enabled {
				return false
			}
		}
	}
	if params.Search == "" {
		return true
	}
	search := strings.ToLower(params.Search)
	return strings.Contains(strings.ToLower(plugin.Name), search) ||
		strings.Contains(strings.ToLower(plugin.DisplayName), search) ||
		strings.Contains(strings.ToLower(plugin.Summary), search) ||
		strings.Contains(strings.ToLower(plugin.Description), search)
}

func publicPluginID(item integration.Integration) string {
	if id := asString(item.Manifest.Metadata[metadataKeyPluginID]); id != "" {
		return id
	}
	return item.Manifest.Name
}

func normalizeInstalledSource(value string) string {
	switch value {
	case "builtin", "marketplace", "installed":
		return value
	default:
		return "installed"
	}
}

func normalizeMarketplaceSource(value string) string {
	if value == "builtin" {
		return "builtin"
	}
	return "marketplace"
}

func healthFromEnabled(enabled bool) string {
	if enabled {
		return "healthy"
	}
	return "disabled"
}

func copyMetadata(meta map[string]interface{}) map[string]interface{} {
	if len(meta) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(meta))
	for key, value := range meta {
		cloned[key] = value
	}
	return cloned
}

func asString(value interface{}) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func asStringSlice(value interface{}, fallback []string) []string {
	if value == nil {
		return append([]string(nil), fallback...)
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item == nil {
				continue
			}
			out = append(out, asString(item))
		}
		return out
	default:
		return append([]string(nil), fallback...)
	}
}

func asBool(value interface{}) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func intValue(value interface{}, fallback int) int {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func coalesce(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
