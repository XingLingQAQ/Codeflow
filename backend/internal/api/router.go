// Package api provides HTTP API server for CodeFlow backend.
package api

import (
	"fmt"
	"net"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/api/handlers"
	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/codeflow/backend/internal/web"
)

// Config holds API server configuration.
type Config struct {
	Port            string
	AllowedOrigins  []string
	EnableDebugMode bool
}

// DefaultConfig returns default API configuration.
func DefaultConfig() *Config {
	return &Config{
		Port:            "8080",
		AllowedOrigins:  []string{"http://localhost:3000", "http://localhost:5173", "tauri://localhost", "https://tauri.localhost"},
		EnableDebugMode: false,
	}
}

// Server represents the API server.
type Server struct {
	config *Config
	router *gin.Engine
}

// NewServer creates a new API server with the given configuration.
func NewServer(config *Config) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	if !config.EnableDebugMode {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Apply middleware
	router.Use(gin.Recovery())
	router.Use(middleware.Trace())
	router.Use(middleware.Logger())
	router.Use(cors.New(cors.Config{
		AllowOriginFunc: func(origin string) bool {
			return true // Desktop app — all local origins allowed
		},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Accept",
			"Authorization",
			middleware.HeaderRequestID,
			middleware.HeaderSessionID,
			middleware.HeaderTaskID,
			middleware.HeaderAgentID,
		},
		ExposeHeaders: []string{
			"Content-Length",
			middleware.HeaderRequestID,
			middleware.HeaderSessionID,
			middleware.HeaderTaskID,
			middleware.HeaderAgentID,
		},
		AllowCredentials: true,
	}))

	server := &Server{
		config: config,
		router: router,
	}

	server.setupRoutes()

	return server
}

// setupRoutes configures all API routes.
func (s *Server) setupRoutes() {
	// Health check
	s.router.GET("/health", handlers.HealthCheck)
	s.router.GET("/ready", handlers.ReadinessCheck)
	s.router.GET("/metrics", handlers.Metrics)

	// API v1 routes
	v1 := s.router.Group("/api/v1")
	{
		// Snapshot routes
		snapshots := v1.Group("/snapshots")
		{
			snapshots.POST("", handlers.CreateSnapshot)
			snapshots.GET("", handlers.GetSnapshots)
			snapshots.GET("/:id", handlers.GetSnapshot)
			snapshots.POST("/:id/restore", handlers.RestoreSnapshot)
			snapshots.DELETE("/:id", handlers.DeleteSnapshot)
		}

		// Memory routes
		memory := v1.Group("/memory")
		{
			memory.GET("/items", handlers.GetMemoryItems)
			memory.POST("/items", handlers.CreateMemoryItem)
			memory.PATCH("/items/:id", handlers.UpdateMemoryItem)
			memory.DELETE("/items/:id", handlers.DeleteMemoryItem)
			memory.POST("/items/:id/archive", handlers.ArchiveMemoryItem)
			memory.POST("/items/:id/restore", handlers.RestoreMemoryItem)

			// Atomic Memory routes
			memory.POST("/atomic", handlers.CreateAtomicMemory)
			memory.GET("/atomic/search", handlers.SearchAtomicMemory)
			memory.GET("/atomic/session/:id", handlers.GetAtomicMemoriesBySession)
			memory.PUT("/atomic/:id", handlers.UpdateAtomicMemory)
			memory.DELETE("/atomic/:id", handlers.DeleteAtomicMemory)
			memory.POST("/atomic/decay", handlers.ApplyAtomicHeatDecay)
			memory.POST("/atomic/recompute-tiers", handlers.RecomputeAtomicTiers)
			memory.POST("/atomic/:id/boost", handlers.BoostAtomicHeat)
			memory.GET("/atomic/tier/:tier", handlers.GetAtomicMemoriesByTier)

			// Memory Preflight routes
			memory.POST("/preflight", handlers.MemoryPreflight)
			memory.GET("/suggestions", handlers.GetMemorySuggestions)
			memory.POST("/inject", handlers.InjectMemory)

			// Raw Archive routes
			memory.POST("/archive", handlers.StoreRawArchive)
			memory.GET("/archive", handlers.ListRawArchive)
			memory.GET("/archive/search", handlers.SearchRawArchive)
			memory.GET("/archive/stats", handlers.GetRawArchiveStats)
			memory.GET("/archive/:id", handlers.GetRawArchiveEntry)

			// MemoryAgent unified dispatch
			memory.POST("/agent/ingest", handlers.MemoryAgentIngest)
			memory.POST("/agent/retrieve", handlers.MemoryAgentRetrieve)
			memory.POST("/agent/context", handlers.MemoryAgentContext)
		}

		// Search routes
		search := v1.Group("/search")
		{
			search.POST("/vector", handlers.VectorSearch)
			search.POST("/fulltext", handlers.FulltextSearch)
			search.POST("/graph", handlers.GraphSearch)
			search.POST("/hybrid", handlers.HybridSearch)
		}

		// Config routes
		cfg := v1.Group("/config")
		{
			cfg.GET("/global", handlers.GetGlobalConfig)
			cfg.PUT("/global", handlers.UpdateGlobalConfig)
			cfg.GET("/sessions/:id", handlers.GetSessionConfig)
			cfg.PUT("/sessions/:id", handlers.UpdateSessionConfig)
			cfg.GET("/roles/:role", handlers.GetRoleConfig)
			cfg.PUT("/roles/:role", handlers.UpdateRoleConfig)
			cfg.GET("/resolve", handlers.ResolveConfig)

			// PAPI routes
			cfg.GET("/papi", handlers.GetPAPIVariables)
			cfg.GET("/papi/:name", handlers.GetPAPIVariable)
			cfg.POST("/papi", handlers.CreatePAPIVariable)
			cfg.PUT("/papi/:name", handlers.UpdatePAPIVariable)
			cfg.DELETE("/papi/:name", handlers.DeletePAPIVariable)
			cfg.POST("/papi/resolve", handlers.ResolvePAPIByCategory)
			cfg.POST("/papi/hotswap", handlers.HotSwapPAPI)
			cfg.GET("/papi/conflicts", handlers.DetectPAPIConflicts)
		}

		// Context routes
		context := v1.Group("/context")
		{
			context.GET("/files", handlers.GetFileTree)
			context.POST("/ast", handlers.ParseAST)
			context.POST("/tokens", handlers.CalculateTokens)
			context.GET("/presets", handlers.GetContextPresets)
			context.POST("/presets", handlers.CreateContextPreset)
			context.DELETE("/presets/:id", handlers.DeleteContextPreset)
		}

		// Agent routes
		agents := v1.Group("/agents")
		{
			agents.GET("", handlers.GetAgents)
			agents.GET("/:id/logs", handlers.GetAgentLogs)
		}

		// Conversation routes
		conversations := v1.Group("/conversations")
		{
			conversations.GET("/:sessionId/trace", handlers.GetConversationTrace)
			conversations.POST("/:sessionId/stop", handlers.StopConversation)
			conversations.POST("/:sessionId/retry", handlers.RetryConversation)
			conversations.GET("/:sessionId/stream", handlers.StreamConversation) // WebSocket
		}

		// Blackboard routes
		blackboard := v1.Group("/blackboard")
		{
			blackboard.GET("/entries", handlers.GetBlackboardEntries)
			blackboard.POST("/entries", handlers.CreateBlackboardEntry)
			blackboard.PATCH("/entries/:id", handlers.UpdateBlackboardEntry)
			blackboard.DELETE("/entries/:id", handlers.DeleteBlackboardEntry)
		}

		// Vote routes
		votes := v1.Group("/votes")
		{
			votes.POST("", handlers.CreateVote)
			votes.GET("/:id", handlers.GetVote)
			votes.POST("/:id/cast", handlers.CastVote)
		}

		// Hook routes
		hooksGroup := v1.Group("/hooks")
		{
			hooksGroup.GET("", handlers.GetHooks)
			hooksGroup.GET("/:name", handlers.GetHook)
			hooksGroup.PUT("/:name/config", handlers.UpdateHookConfig)
			hooksGroup.POST("/:name/enable", handlers.EnableHook)
			hooksGroup.POST("/:name/disable", handlers.DisableHook)
			hooksGroup.POST("/:name/trigger", handlers.TriggerHook)
			hooksGroup.GET("/events", handlers.GetHookEvents)
			hooksGroup.DELETE("/events", handlers.ClearHookEvents)
		}

		// Integration routes
		integrations := v1.Group("/integrations")
		{
			integrations.POST("", handlers.RegisterIntegration)
			integrations.GET("", handlers.ListIntegrations)
			integrations.GET("/:id", handlers.GetIntegration)
			integrations.POST("/:id/invoke", handlers.InvokeIntegration)
			integrations.POST("/:id/replay", handlers.ReplayIntegration)
		}

		// Debate routes
		debates := v1.Group("/debates")
		{
			debates.POST("", handlers.CreateDebate)
			debates.GET("/:id", handlers.GetDebate)
			debates.POST("/:id/next-round", handlers.NextDebateRound)
			debates.POST("/:id/conflicts/:cid/resolve", handlers.ResolveConflict)
			debates.POST("/:id/select-solution", handlers.SelectSolution)
			debates.GET("/:id/export", handlers.ExportDebateReport)
			debates.GET("/:id/stream", handlers.StreamDebate) // WebSocket
		}

		// Project routes
		projects := v1.Group("/projects")
		{
			projects.GET("", handlers.GetProjects)
			projects.POST("", handlers.CreateProject)
			projects.GET("/:id", handlers.GetProject)
			projects.PUT("/:id", handlers.UpdateProject)
			projects.DELETE("/:id", handlers.DeleteProject)
			projects.GET("/:id/plans", handlers.GetProjectPlans)
			projects.POST("/:id/plans", handlers.AddPlanToProject)
			projects.DELETE("/:id/plans/:planId", handlers.RemovePlanFromProject)
		}

		// Plan routes
		plans := v1.Group("/plans")
		{
			plans.GET("", handlers.GetPlans)
			plans.POST("", handlers.CreatePlan)
			plans.GET("/:id/tasks", handlers.GetPlanTasks)
			plans.POST("/:id/tasks", handlers.CreatePlanTask)
			plans.PATCH("/:id/tasks/:tid", handlers.UpdatePlanTask)
			plans.POST("/:id/tasks/:tid/reorder", handlers.ReorderPlanTask)
			plans.POST("/:id/tasks/batch-model", handlers.BatchUpdateTaskModel)
			plans.DELETE("/:id/tasks/:tid", handlers.DeletePlanTask)
		}

		// Workflow routes
		workflows := v1.Group("/workflows")
		{
			workflows.GET("/:projectId/overview", handlers.GetWorkflowOverview)
			workflows.GET("/:projectId/timeline", handlers.GetWorkflowTimeline)
			workflows.GET("/:projectId/replay", handlers.GetWorkflowReplay)
		}
		summarizeGroup := v1.Group("/summarize")
		{
			summarizeGroup.POST("/conversation", handlers.SummarizeConversation)
			summarizeGroup.POST("/context", handlers.CompressContext)
			summarizeGroup.POST("/skeleton", handlers.GetDecisionSkeleton)
		}

		// Audit routes
		auditGroup := v1.Group("/audit")
		{
			auditGroup.GET("/logs", handlers.GetAuditLogs)
			auditGroup.POST("/verify", handlers.VerifyAuditChain)
			auditGroup.GET("/statistics", handlers.GetAuditStatistics)
			auditGroup.GET("/export", handlers.ExportAuditLogs)
		}

		// Privacy routes
		privacyGroup := v1.Group("/privacy")
		{
			privacyGroup.POST("/encrypt", handlers.Encrypt)
			privacyGroup.POST("/decrypt", handlers.Decrypt)
			privacyGroup.POST("/redact", handlers.Redact)
			privacyGroup.POST("/detect", handlers.DetectPII)
			privacyGroup.GET("/keys", handlers.GetKeys)
			privacyGroup.POST("/keys", handlers.ManageKeys)
			privacyGroup.POST("/verify-chain", handlers.VerifyChain)
			privacyGroup.GET("/metrics", handlers.GetPrivacyMetrics)
			privacyGroup.POST("/metrics/reset", handlers.ResetPrivacyMetrics)
		}

		// Isolation routes
		isolationGroup := v1.Group("/isolation")
		{
			// Container management
			isolationGroup.GET("/containers", handlers.GetContainers)
			isolationGroup.POST("/containers", handlers.CreateContainer)
			isolationGroup.GET("/containers/:id", handlers.GetContainer)
			isolationGroup.DELETE("/containers/:id", handlers.DeleteContainer)
			isolationGroup.PUT("/containers/:id/quota", handlers.SetContainerQuota)

			// Access control
			isolationGroup.POST("/access/check", handlers.CheckAccess)
			isolationGroup.POST("/io/validate", handlers.ValidateIO)

			// Role management
			isolationGroup.GET("/roles", handlers.GetRoles)
			isolationGroup.POST("/roles", handlers.RegisterRole)
			isolationGroup.GET("/roles/:name", handlers.GetRole)
			isolationGroup.GET("/roles/:name/permissions", handlers.GetRolePermissions)
			isolationGroup.POST("/roles/:name/check", handlers.CheckRolePermission)
		}

		// SAMG routes
		samgGroup := v1.Group("/samg")
		{
			// Triple operations
			samgGroup.GET("/triples", handlers.GetTriples)
			samgGroup.POST("/triples", handlers.AddTriples)
			samgGroup.GET("/triples/:id", handlers.GetTriple)
			samgGroup.DELETE("/triples", handlers.DeleteTriples)
			samgGroup.GET("/triples/:id/relations", handlers.GetRelations)

			// Extraction
			samgGroup.POST("/extract", handlers.ExtractTriples)
			samgGroup.POST("/extract-with-pointers", handlers.ExtractWithPointers)

			// Query Memory (Neural Index)
			samgGroup.POST("/query-memory", handlers.QueryMemory)

			// Activation
			samgGroup.POST("/activate", handlers.Activate)
			samgGroup.POST("/paths", handlers.FindPaths)
			samgGroup.GET("/activation", handlers.GetActivationConfig)
			samgGroup.PUT("/activation", handlers.UpdateActivationConfig)

			// Decay
			samgGroup.GET("/decay", handlers.GetDecayConfig)
			samgGroup.PUT("/decay", handlers.UpdateDecayConfig)
			samgGroup.POST("/decay/apply", handlers.ApplyDecay)

			// Nodes
			samgGroup.GET("/nodes/visible", handlers.GetVisibleNodes)
			samgGroup.GET("/nodes/hidden", handlers.GetHiddenNodes)
			samgGroup.GET("/nodes/top", handlers.GetTopNodes)
			samgGroup.POST("/nodes/:id/access", handlers.RecordAccess)
			samgGroup.GET("/nodes/:id/pointers", handlers.GetNodePointers)
			samgGroup.POST("/nodes/:id/pointers", handlers.AddNodePointer)

			// Graph
			samgGroup.GET("/graph/export", handlers.ExportGraph)
			samgGroup.POST("/graph/import", handlers.ImportGraph)
			samgGroup.GET("/stats", handlers.GetSAMGStats)
		}
	}

	// Static file serving for embedded frontend (must be after API routes)
	web.SetupStaticRoutes(s.router)
}

// Run starts the API server.
// If port is "0", it binds to a random available port and prints the actual port to stdout.
func (s *Server) Run() error {
	addr := ":" + s.config.Port
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		// If the configured port is occupied, try port 0 (random)
		if s.config.Port != "0" {
			listener, err = net.Listen("tcp", ":0")
			if err != nil {
				return fmt.Errorf("failed to bind to any port: %w", err)
			}
		} else {
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
	}

	// Extract actual port and emit for Tauri sidecar protocol
	actualPort := listener.Addr().(*net.TCPAddr).Port
	fmt.Printf("CODEFLOW_PORT:%d\n", actualPort)

	return s.router.RunListener(listener)
}

// Router returns the underlying gin router for testing.
func (s *Server) Router() *gin.Engine {
	return s.router
}
