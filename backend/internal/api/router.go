// Package api provides HTTP API server for CodeFlow backend.
package api

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/api/handlers"
	"github.com/codeflow/backend/internal/api/middleware"
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
		AllowedOrigins:  []string{"http://localhost:3000", "http://localhost:5173"},
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
	router.Use(middleware.Logger())
	router.Use(cors.New(cors.Config{
		AllowOrigins:     config.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
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

			// Memory Preflight routes
			memory.POST("/preflight", handlers.MemoryPreflight)
			memory.GET("/suggestions", handlers.GetMemorySuggestions)
			memory.POST("/inject", handlers.InjectMemory)
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

		// Summarize routes
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
	}
}

// Run starts the API server.
func (s *Server) Run() error {
	return s.router.Run(":" + s.config.Port)
}

// Router returns the underlying gin router for testing.
func (s *Server) Router() *gin.Engine {
	return s.router
}
