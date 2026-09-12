// Package api provides HTTP API server for CodeFlow backend.
package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/codeflow/backend/internal/api/handlers"
	"github.com/codeflow/backend/internal/api/middleware"
	"github.com/codeflow/backend/internal/web"
	"github.com/codeflow/backend/internal/websocket"
)

// Config holds API server configuration.
type Config struct {
	Host            string
	Port            string
	AuthToken       string
	AllowedOrigins  []string
	EnableDebugMode bool
	// AllowRemote permits a non-loopback bind only when AuthToken was supplied
	// explicitly by the caller (never generated as a sidecar token).
	AllowRemote bool
	// HandshakeWriter receives the one-line startup handshake. nil means stdout.
	HandshakeWriter io.Writer
}

// DefaultConfig returns default API configuration.
func DefaultConfig() *Config {
	return &Config{
		Host:            "127.0.0.1",
		Port:            "8080",
		AllowedOrigins:  []string{"http://localhost:3000", "http://localhost:5173", "tauri://localhost", "https://tauri.localhost"},
		EnableDebugMode: false,
	}
}

// Server represents the API server.
type Server struct {
	config    *Config
	router    *gin.Engine
	configErr error
	// processStartID uniquely identifies this server process instance. The
	// desktop shell uses it to detect a backend restart (including port
	// reuse) and drop stale connections instead of re-pairing blindly.
	processStartID string
}

// NewServer creates a new API server with the given configuration.
func NewServer(config *Config) *Server {
	if config == nil {
		config = DefaultConfig()
	}
	// Copy caller-owned slices before normalizing so construction never mutates
	// configuration shared with bootstrap or tests.
	copyConfig := *config
	copyConfig.AllowedOrigins = append([]string(nil), config.AllowedOrigins...)
	if copyConfig.Host == "" {
		copyConfig.Host = "127.0.0.1"
	}
	if copyConfig.Port == "" {
		copyConfig.Port = "0"
	}
	// Generate the process-start ID once per server instance: startup
	// timestamp (UnixNano, hex) + 128 bits of crypto/rand, base64url. It
	// stays constant for the whole process lifetime and changes on every
	// restart, which is exactly the signal the sidecar consumer needs.
	processStartID, err := generateProcessStartID()
	if err != nil {
		server := &Server{config: &copyConfig, configErr: err}
		server.router = gin.New()
		return server
	}
	if copyConfig.AuthToken == "" {
		if copyConfig.AllowRemote {
			copyConfigErr := errors.New("remote mode requires an explicitly configured auth token")
			copyConfig.AuthToken = ""
			server := &Server{config: &copyConfig, configErr: copyConfigErr, processStartID: processStartID}
			server.router = gin.New()
			return server
		}
		token, err := generateAuthToken()
		if err != nil {
			server := &Server{config: &copyConfig, configErr: err, processStartID: processStartID}
			server.router = gin.New()
			return server
		}
		copyConfig.AuthToken = token
	}
	configErr := validateConfig(&copyConfig)

	if !copyConfig.EnableDebugMode {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	wsPolicy := websocket.NewAccessPolicy(copyConfig.AuthToken, copyConfig.AllowedOrigins)
	// Apply middleware
	router.Use(gin.Recovery())
	router.Use(middleware.Trace())
	router.Use(middleware.Logger())
	router.Use(func(c *gin.Context) {
		c.Set(websocket.ContextAccessPolicy, wsPolicy)
		c.Next()
	})
	router.Use(cors.New(cors.Config{
		AllowOriginFunc: wsPolicy.OriginAllowed,
		AllowMethods:    []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Accept",
			"Authorization",
			middleware.HeaderRequestID,
			middleware.HeaderSessionID,
			middleware.HeaderTaskID,
			middleware.HeaderAgentID,
			// Workspace APIs prefer this header for project root.
			"X-Codeflow-Workspace-Root",
			"X-Codeflow-Project-ID",
			"Idempotency-Key",
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
		config:         &copyConfig,
		router:         router,
		configErr:      configErr,
		processStartID: processStartID,
	}
	auth := middleware.RequireAccessToken(copyConfig.AuthToken)
	router.GET("/ready", auth, handlers.ReadinessCheck)
	router.GET("/metrics", auth, handlers.Metrics)

	server.setupRoutes()

	return server
}

// setupRoutes configures all API routes.
func (s *Server) setupRoutes() {
	// Health check
	s.router.GET("/health", handlers.HealthCheck)

	// API v1 routes
	v1 := s.router.Group("/api/v1")
	v1.Use(middleware.RequireAccessToken(s.config.AuthToken))
	v1.Use(middleware.AuditMutations())
	{
		// Snapshot routes (experimental: core capture/restore functions are placeholder implementations)
		snapshots := v1.Group("/snapshots")
		snapshots.Use(middleware.Experimental("snapshot"))
		{
			snapshots.POST("", handlers.CreateSnapshot)
			snapshots.GET("", handlers.GetSnapshots)
			snapshots.GET("/:id", handlers.GetSnapshot)
			snapshots.POST("/:id/restore", handlers.RestoreSnapshot)
			snapshots.DELETE("/:id", handlers.DeleteSnapshot)
		}

		// Flow engine routes (experimental: M2 / PR-8 minimal state machine)
		flows := v1.Group("/flows")
		flows.Use(middleware.Experimental("floweng"))
		{
			flows.GET("/templates", handlers.ListFlowTemplates)
			flows.GET("/templates/:tid", handlers.GetFlowTemplate)
			flows.GET("/templates/:tid/export", handlers.ExportFlowTemplate)
			flows.DELETE("/templates/:tid", handlers.DeleteFlowTemplate)
			flows.POST("/templates/import", handlers.ImportFlowTemplate)
			flows.POST("", handlers.CreateFlow)
			flows.GET("", handlers.ListFlows)
			flows.GET("/:id", handlers.GetFlow)
			flows.DELETE("/:id", handlers.DeleteFlow)
			flows.GET("/:id/events", handlers.ListFlowEvents)
			flows.GET("/:id/active-stage", handlers.GetActiveFlowStage)
			flows.GET("/:id/gates", handlers.ListFlowGates)
			flows.GET("/:id/stages", handlers.ListFlowStages)
			flows.GET("/:id/stages/:sid", handlers.GetFlowStage)
			flows.GET("/:id/artifacts", handlers.ListFlowArtifacts)
			flows.GET("/:id/artifacts/:aid", handlers.GetFlowArtifact)
			flows.PATCH("/:id/artifacts/:aid", handlers.UpdateFlowArtifactStatus)
			flows.POST("/:id/stages/:sid/advance", handlers.AdvanceFlowStage)
			flows.POST("/:id/stages/:sid/skip", handlers.SkipFlowStage)
			flows.POST("/:id/stages/:sid/artifacts", handlers.AttachFlowArtifact)
			flows.POST("/:id/loop", handlers.LoopFlow)
			flows.POST("/:id/abort", handlers.AbortFlow)
			flows.POST("/:id/gates/:gid/decide", handlers.DecideFlowGate)
		}

		// Workspace filesystem (experimental: M3.1; writes optional WriteGuard)
		ws := v1.Group("/workspace")
		ws.Use(middleware.Experimental("workspace"))
		{
			ws.GET("/list", handlers.ListWorkspace)
			ws.GET("/read", handlers.ReadWorkspaceFile)
			ws.GET("/stat", handlers.StatWorkspaceFile)
			ws.GET("/staged", handlers.ListWorkspaceStaged)
			ws.POST("/write", handlers.WriteWorkspaceFile)
			ws.POST("/promote", handlers.PromoteWorkspaceFile)
			ws.POST("/promote-all", handlers.PromoteAllWorkspace)
			ws.POST("/discard", handlers.DiscardWorkspaceStaged)
			ws.POST("/discard-all", handlers.DiscardAllWorkspaceStaged)
			// File watcher (experimental): static paths only, no param routes in this group.
			ws.GET("/watches", handlers.ListWorkspaceWatches)
			ws.POST("/watch", handlers.CreateWorkspaceWatch)
			ws.DELETE("/watch", handlers.DeleteWorkspaceWatch)
			ws.GET("/scripts", handlers.DetectWorkspaceScripts)
			ws.GET("/dev-servers", handlers.ListWorkspaceDevServers)
			ws.POST("/dev-servers", handlers.StartWorkspaceDevServer)
			ws.DELETE("/dev-servers", handlers.DeleteWorkspaceDevServer)
			ws.GET("/dev-servers/:id/logs", handlers.GetWorkspaceDevServerLogs)
		}

		// Skill registry (experimental: M5.0 minimal)
		skills := v1.Group("/skills")
		skills.Use(middleware.Experimental("skill"))
		{
			skills.POST("", handlers.CreateSkill)
			skills.GET("", handlers.ListSkills)
			skills.POST("/match", handlers.MatchSkills)
			skills.POST("/inject", handlers.InjectSkills)
			skills.POST("/import", handlers.ImportSkills)
			skills.GET("/export", handlers.ExportSkills)
			skills.GET("/:id", handlers.GetSkill)
			skills.PATCH("/:id", handlers.UpdateSkill)
			skills.DELETE("/:id", handlers.DeleteSkill)
			skills.GET("/:id/versions", handlers.ListSkillVersions)
			skills.POST("/:id/rollback", handlers.RollbackSkillVersion)
		}

		// Guard policy (experimental)
		guardAPI := v1.Group("/guard")
		guardAPI.Use(middleware.Experimental("guard"))
		{
			guardAPI.POST("/check", handlers.GuardCheck)
			guardAPI.GET("/config", handlers.GuardConfig)
			guardAPI.GET("/rules", handlers.GuardRules)
			guardAPI.POST("/index", handlers.GuardIndexTree)
			guardAPI.POST("/exempt", handlers.GuardExempt)
			guardAPI.GET("/exemptions", handlers.GuardListExemptions)
			guardAPI.DELETE("/exempt", handlers.GuardClearExemption)
			guardAPI.POST("/exemption-requests", handlers.CreateExemptionRequest)
			guardAPI.GET("/exemption-requests", handlers.ListExemptionRequests)
			guardAPI.POST("/exemption-requests/:id/decide", handlers.DecideExemptionRequest)
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
			agents.POST("", handlers.CreateAgent)
			// Agent registry (experimental: G20 asset store)
			agentReg := agents.Group("/registry")
			agentReg.Use(middleware.Experimental("agent-registry"))
			{
				agentReg.GET("", handlers.ListRegistryAgents)
				agentReg.POST("", handlers.CreateRegistryAgent)
				agentReg.GET("/:id", handlers.GetRegistryAgent)
				agentReg.PATCH("/:id", handlers.UpdateRegistryAgent)
				agentReg.DELETE("/:id", handlers.DeleteRegistryAgent)
				agentReg.POST("/:id/usage", handlers.IncrementRegistryAgentUsage)
				agentReg.POST("/:id/score", handlers.SetRegistryAgentScore)
			}
			agents.GET("/:id", handlers.GetAgent)
			agents.PUT("/:id", handlers.UpdateAgent)
			agents.DELETE("/:id", handlers.DeleteAgent)
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
			// Static paths before /:name so "events" is not captured as a hook name.
			hooksGroup.GET("/events", handlers.GetHookEvents)
			hooksGroup.DELETE("/events", handlers.ClearHookEvents)
			hooksGroup.GET("/:name", handlers.GetHook)
			hooksGroup.PUT("/:name/config", handlers.UpdateHookConfig)
			hooksGroup.POST("/:name/enable", handlers.EnableHook)
			hooksGroup.POST("/:name/disable", handlers.DisableHook)
			hooksGroup.POST("/:name/trigger", handlers.TriggerHook)
		}

		// Plugin routes
		plugins := v1.Group("/plugins")
		{
			plugins.GET("", handlers.ListPlugins)
			plugins.GET("/marketplace", handlers.ListMarketplacePlugins)
			plugins.GET("/:id", handlers.GetPlugin)
			plugins.POST("/:id/install", handlers.InstallPlugin)
			plugins.PATCH("/:id", handlers.TogglePlugin)
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
			debates.GET("", handlers.ListDebates)
			debates.GET("/:id", handlers.GetDebate)
			debates.POST("/:id/next-round", handlers.NextDebateRound)
			debates.POST("/:id/conflicts/:cid/resolve", handlers.ResolveConflict)
			debates.POST("/:id/solutions", handlers.ProposeSolution)
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
			projects.GET("/:id/stream", handlers.StreamProjectEvents)
			projects.PUT("/:id", handlers.UpdateProject)
			projects.DELETE("/:id", handlers.DeleteProject)
			projects.POST("/:id/restore", handlers.RestoreProject)
			projects.POST("/:id/bind-workspace", handlers.BindProjectWorkspace)
			projects.GET("/:id/plans", handlers.GetProjectPlans)
			projects.POST("/:id/plans", handlers.AddPlanToProject)
			projects.DELETE("/:id/plans/:planId", handlers.RemovePlanFromProject)
			projects.POST("/:id/plan", handlers.GenerateProjectPlan)
			projects.GET("/:id/plan", handlers.GetProjectPlan)
			projects.POST("/:id/plan/revise", handlers.ReviseProjectPlan)
			projects.POST("/:id/plan/approve", handlers.ApproveProjectPlan)
			projects.POST("/:id/plan/execute", handlers.ExecuteProjectPlan)
		}

		// Plan routes
		plans := v1.Group("/plans")
		{
			plans.GET("", handlers.GetPlans)
			plans.POST("", handlers.CreatePlan)
			plans.GET("/:id", handlers.GetPlan)
			plans.PUT("/:id", handlers.UpdatePlan)
			plans.DELETE("/:id", handlers.DeletePlan)
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
	return s.RunContext(context.Background())
}

// RunContext starts the API server and shuts it down when ctx is canceled.
func (s *Server) RunContext(ctx context.Context) error {
	if s.configErr != nil {
		return s.configErr
	}
	addr := net.JoinHostPort(s.config.Host, s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		// If the configured port is occupied, try port 0 (random)
		if s.config.Port != "0" {
			listener, err = net.Listen("tcp", net.JoinHostPort(s.config.Host, "0"))
			if err != nil {
				return fmt.Errorf("failed to bind to any port: %w", err)
			}
		} else {
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
	}

	// Extract actual port and emit for Tauri sidecar protocol
	actualPort := listener.Addr().(*net.TCPAddr).Port
	handshake := map[string]interface{}{
		"protocol_version": "1",
		"host":             s.config.Host,
		"port":             actualPort,
		"token":            s.config.AuthToken,
		"expires_at":       "process",
		"process_start_id": s.processStartID,
	}
	handshakeJSON, err := json.Marshal(handshake)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("encode sidecar handshake: %w", err)
	}
	writer := s.config.HandshakeWriter
	if writer == nil {
		writer = os.Stdout
	}
	if _, err := fmt.Fprintf(writer, "CODEFLOW_HANDSHAKE:%s\n", handshakeJSON); err != nil {
		_ = listener.Close()
		return fmt.Errorf("write sidecar handshake: %w", err)
	}

	server := &nethttp.Server{Handler: s.router}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, nethttp.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errCh
		if errors.Is(err, nethttp.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Router returns the underlying gin router for testing.
func (s *Server) Router() *gin.Engine {
	return s.router
}

// AuthToken returns the process-local token for bootstrap/tests. It is not
// exposed by any HTTP handler.
func (s *Server) AuthToken() string {
	if s == nil || s.config == nil {
		return ""
	}
	return s.config.AuthToken
}

// ProcessStartID returns the identifier generated once for this server
// process instance. It is stable for the process lifetime, changes on every
// restart, and is emitted in the sidecar handshake so the desktop shell can
// detect a restarted backend. It is not exposed by any HTTP handler.
func (s *Server) ProcessStartID() string {
	if s == nil {
		return ""
	}
	return s.processStartID
}

// Validate reports startup configuration errors before binding a listener.
func (s *Server) Validate() error {
	if s == nil {
		return errors.New("nil API server")
	}
	return s.configErr
}

func generateAuthToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate sidecar auth token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// generateProcessStartID builds the process-start identifier from the
// startup timestamp plus 128 bits of randomness: "<unixnano hex>-<base64url>".
// The timestamp keeps IDs sortable by start order; the random suffix keeps
// them unique even for two processes started within the same nanosecond
// clock tick.
func generateProcessStartID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate process start id: %w", err)
	}
	return fmt.Sprintf("%x-%s", time.Now().UnixNano(), base64.RawURLEncoding.EncodeToString(b)), nil
}

func validateConfig(config *Config) error {
	if config == nil {
		return errors.New("nil API config")
	}
	if !isLoopbackHost(config.Host) && !config.AllowRemote {
		return fmt.Errorf("sidecar host %q is not loopback; enable explicit remote mode", config.Host)
	}
	if config.AuthToken == "" {
		return errors.New("auth token must not be empty")
	}
	if !validOpaqueToken(config.AuthToken) {
		return errors.New("auth token must be at least 32 URL-safe characters")
	}
	seen := make(map[string]struct{}, len(config.AllowedOrigins))
	for _, origin := range config.AllowedOrigins {
		if err := validateOrigin(origin); err != nil {
			return err
		}
		if _, ok := seen[origin]; ok {
			return fmt.Errorf("duplicate allowed origin %q", origin)
		}
		seen[origin] = struct{}{}
	}
	return nil
}

func validateOrigin(origin string) error {
	if origin == "" || strings.TrimSpace(origin) != origin || origin == "*" {
		return fmt.Errorf("invalid allowed origin %q", origin)
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid allowed origin %q", origin)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "tauri":
	default:
		return fmt.Errorf("invalid allowed origin scheme in %q", origin)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	h := strings.Trim(host, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func validOpaqueToken(token string) bool {
	if len(token) < 32 {
		return false
	}
	for _, char := range token {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
