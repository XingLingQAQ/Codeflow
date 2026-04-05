package plugin

// PluginPermission mirrors the frontend permission contract.
type PluginPermission struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Level       string `json:"level,omitempty"`
	Granted     bool   `json:"granted"`
	Required    bool   `json:"required"`
}

// PluginConfigField mirrors the frontend config field contract.
type PluginConfigField struct {
	Key         string      `json:"key"`
	Label       string      `json:"label,omitempty"`
	Type        string      `json:"type,omitempty"`
	Value       interface{} `json:"value,omitempty"`
	Required    bool        `json:"required"`
	Masked      bool        `json:"masked"`
	Description string      `json:"description,omitempty"`
}

// PluginMetrics mirrors the frontend metrics contract.
type PluginMetrics struct {
	Installs       int     `json:"installs,omitempty"`
	Downloads      int     `json:"downloads,omitempty"`
	ActiveSessions int     `json:"active_sessions,omitempty"`
	ErrorRate      float64 `json:"error_rate,omitempty"`
	LatencyMS      float64 `json:"latency_ms,omitempty"`
}

// PluginManifest is the public plugin DTO served to the frontend.
type PluginManifest struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name,omitempty"`
	Summary     string                 `json:"summary,omitempty"`
	Description string                 `json:"description,omitempty"`
	Version     string                 `json:"version,omitempty"`
	Author      string                 `json:"author,omitempty"`
	Vendor      string                 `json:"vendor,omitempty"`
	Category    string                 `json:"category,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	Source      string                 `json:"source,omitempty"`
	Scope       string                 `json:"scope,omitempty"`
	Enabled     bool                   `json:"enabled"`
	Installed   bool                   `json:"installed"`
	Featured    bool                   `json:"featured"`
	Verified    bool                   `json:"verified"`
	Health      string                 `json:"health,omitempty"`
	Homepage    string                 `json:"homepage,omitempty"`
	Repository  string                 `json:"repository,omitempty"`
	Icon        string                 `json:"icon,omitempty"`
	UpdatedAt   int64                  `json:"updated_at,omitempty"`
	InstalledAt int64                  `json:"installed_at,omitempty"`
	Permissions []PluginPermission     `json:"permissions,omitempty"`
	Config      []PluginConfigField    `json:"config,omitempty"`
	Metrics     *PluginMetrics         `json:"metrics,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// PluginOverview summarizes installed plugin state.
type PluginOverview struct {
	Total       int            `json:"total"`
	Installed   int            `json:"installed"`
	Enabled     int            `json:"enabled"`
	Marketplace int            `json:"marketplace"`
	Unhealthy   int            `json:"unhealthy"`
	Categories  map[string]int `json:"categories,omitempty"`
}

// PluginListResponse is returned from GET /api/v1/plugins.
type PluginListResponse struct {
	Plugins []PluginManifest `json:"plugins"`
	Total   int              `json:"total"`
	HasMore bool             `json:"has_more,omitempty"`
	Summary *PluginOverview  `json:"summary,omitempty"`
}

// PluginCatalogResponse is returned from GET /api/v1/plugins/marketplace.
type PluginCatalogResponse struct {
	Plugins  []PluginManifest `json:"plugins"`
	Total    int              `json:"total"`
	Featured []PluginManifest `json:"featured,omitempty"`
}

// PluginDetailResponse is returned from GET /api/v1/plugins/:id and mutating routes.
type PluginDetailResponse struct {
	Plugin PluginManifest `json:"plugin"`
}

// ListParams controls list and catalog queries.
type ListParams struct {
	Scope  string
	Status string
	Search string
	Limit  int
	Offset int
}
