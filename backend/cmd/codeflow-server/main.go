package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codeflow/backend/internal/agent"
	"github.com/codeflow/backend/internal/api"
	"github.com/codeflow/backend/internal/api/handlers"
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/bootstrap"
	"github.com/codeflow/backend/internal/commander"
	cfgsvc "github.com/codeflow/backend/internal/config"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/debate"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/isolation"
	"github.com/codeflow/backend/internal/memory"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/policy"
	"github.com/codeflow/backend/internal/privacy"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/skill"
	"github.com/codeflow/backend/internal/snapshot"
	"github.com/codeflow/backend/internal/samg"
	"github.com/codeflow/backend/internal/storage"
	"github.com/codeflow/backend/internal/summarize"
	"github.com/codeflow/backend/internal/workspace"
)

const version = "0.1.0"

func main() {
	fmt.Printf("CodeFlow Backend Server v%s\n", version)
	fmt.Println("Go-based backend for CodeFlow IDE")

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	trust, err := loadTrustConfig()
	if err != nil {
		return err
	}

	// Get port from environment or use dynamic port (0 = OS assigns)
	port := os.Getenv("PORT")
	if port == "" {
		port = "0"
	}

	// Get allowed origins from environment or use defaults
	allowedOrigins := []string{
		"http://localhost:3000",
		"http://localhost:5173",
		"tauri://localhost",
		"https://tauri.localhost",
	}
	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		allowedOrigins = splitCommaSeparated(origins)
	}

	// Check if debug mode is enabled
	debugMode := os.Getenv("DEBUG") == "true"

	configureHookRuntimeControls()

	// Audit must be ready before config opens so legacy credential migrations
	// and reconciliation are recorded during startup.
	auditStore := audit.NewFileAuditStorage(&audit.FileStorageConfig{
		LogDir:          durableDBPath("audit"),
		VerifyOnStartup: true,
	})
	if err := auditStore.Initialize(); err != nil {
		return fmt.Errorf("init audit storage: %w", err)
	}
	auditSvc := audit.NewAuditService(auditStore)
	audit.SetAuditService(auditSvc)
	defer func() {
		_ = auditSvc.Close()
		audit.SetAuditService(nil)
	}()

	policy.RequireEnforcement(true)
	defer policy.RequireEnforcement(false)
	policy.SetEvaluator(policy.BootstrapEvaluator())
	defer policy.SetEvaluator(nil)

	privacyMaster := strings.TrimSpace(os.Getenv("CODEFLOW_PRIVACY_MASTER_KEY"))
	if privacyMaster == "" {
		return fmt.Errorf("CODEFLOW_PRIVACY_MASTER_KEY is required")
	}
	privacySvc, err := privacy.NewPrivacyService(privacyMaster, nil)
	if err != nil {
		return fmt.Errorf("init privacy service: %w", err)
	}
	privacy.SetPrivacyService(privacySvc)
	defer privacy.SetPrivacyService(nil)
	isolation.SetIsolationService(isolation.NewIsolationService(nil))
	defer isolation.SetIsolationService(nil)

	configSvc, configClose, err := initConfigService()
	if err != nil {
		return err
	}
	defer configClose()

	plannerSvc, plannerClose, err := initPlannerService()
	if err != nil {
		return err
	}
	defer plannerClose()

	projectSvc, projectClose, err := initProjectService()
	if err != nil {
		return err
	}
	defer projectClose()
	allowedWorkspaceRoots := parseAllowedWorkspaceRoots()
	if svc, ok := projectSvc.(*project.SQLiteProjectService); ok {
		svc.SetAllowedWorkspaceRoots(allowedWorkspaceRoots)
		svc.SetAllowUnrestrictedWorkspaceRoots(os.Getenv("CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING") == "1")
	}
	sessionStore, err := storage.NewSessionStorage(durableDBPath("sessions.db"))
	if err != nil {
		return fmt.Errorf("init project session storage: %w", err)
	}
	project.SetSessionStorage(sessionStore)
	defer func() {
		project.SetSessionStorage(nil)
		_ = sessionStore.Close()
	}()
	agentSvc, err := agent.NewSQLiteAgentService(durableDBPath("sessions.db"))
	if err != nil { return fmt.Errorf("init agent runtime storage: %w", err) }
	defer closeFunc(agentSvc)()
	if err := registerConfiguredAgents(context.Background(), configSvc, agentSvc); err != nil { return err }

	rawArchive := memory.NewSQLiteRawArchive(durableDBPath("raw_archive.db"))
	if err := rawArchive.Initialize(); err != nil { return fmt.Errorf("init raw archive: %w", err) }
	defer rawArchive.Close()
	memorySvc, err := memory.NewSQLiteService(durableDBPath("memory.db"))
	if err != nil { return fmt.Errorf("init memory service: %w", err) }
	defer memorySvc.Close()
	atomicSvc, err := memory.NewSQLiteAtomicMemoryService(context.Background(), durableDBPath("atomic_memory.db"), durableDBPath("atomic_vectors.db"))
	if err != nil { return fmt.Errorf("init atomic memory service: %w", err) }
	defer atomicSvc.Close()
	samgSvc, err := samg.NewSQLiteSAMGService(durableDBPath("samg.db"), nil)
	if err != nil { return fmt.Errorf("init samg service: %w", err) }
	defer samgSvc.Close()
	memoryAgent := memory.NewMemoryAgent(rawArchive, atomicSvc, samgSvc)

	contextSvc, contextClose, err := initContextService()
	if err != nil {
		return err
	}
	defer contextClose()

	// B1 services: wired via bootstrap (floweng/skill durable sqlite).
	snapshotSvc := snapshot.NewInMemorySnapshotService()
	flowEngine, err := floweng.NewSQLiteEngine(durableDBPath("floweng.db"), floweng.NewDefaultSnapshotHook(snapshotSvc))
	if err != nil {
		return fmt.Errorf("init floweng sqlite: %w", err)
	}
	defer func() { _ = flowEngine.Close() }()
	flowEngine.SetEventNotifier(floweng.NewWSNotifier(nil))
	flowEngine.SetSnapshotRestorer(floweng.NewDefaultSnapshotRestorer(snapshotSvc))
	// Startup recovery is per-record: unrecoverable operations are reported as
	// diagnostics (also queryable via project.CreateOperationRecoveryDiagnostics)
	// and logged here without blocking startup; only an unreadable journal is fatal.
	recoveryDiagnostics, err := project.RecoverIncompleteProjectCreations(context.Background(), projectSvc, flowEngine)
	if err != nil {
		return fmt.Errorf("recover incomplete project creation: %w", err)
	}
	for _, diagnostic := range recoveryDiagnostics {
		log.Printf("[WARN] project create operation %q (project %s, state %s) was not recovered: %s", diagnostic.IdempotencyKey, diagnostic.ProjectID, diagnostic.State, diagnostic.Error)
	}
	if err := project.RecoverIncompleteProjectLifecycles(context.Background(), projectSvc, flowEngine); err != nil {
		return fmt.Errorf("recover incomplete project lifecycle: %w", err)
	}
	skillReg, err := skill.NewSQLiteRegistry(durableDBPath("skills.db"))
	if err != nil {
		return fmt.Errorf("init skill sqlite: %w", err)
	}
	defer func() { _ = skillReg.Close() }()
	// Optional bulk import from project skills directory (ignore if missing).
	if n, err := skillReg.ImportMarkdownDir(context.Background(), filepath.Join(".", ".codeflow", "skills")); err == nil && n > 0 {
		fmt.Printf("✓ Imported %d skill(s) from .codeflow/skills\n", n)
	}
	guardEng := guard.NewEngine(nil, guard.NewAuditBridge(auditSvc))
	if err := guardEng.OpenExemptionStore(durableDBPath("guard_exemptions.db")); err != nil {
		return fmt.Errorf("init guard exemption store: %w", err)
	}
	defer func() { _ = guardEng.CloseExemptionStore() }()
	// Optional project guard policy (ignore if missing).
	_ = guardEng.TryLoadConfigFile(filepath.Join(".", ".codeflow", "guard.yaml"))
	if root := strings.TrimSpace(os.Getenv("CODEFLOW_INDEX_ROOT")); root != "" {
		if n, err := guardEng.IndexTree(context.Background(), root); err == nil {
			fmt.Printf("✓ Guard indexed %d source file(s) under %s\n", n, root)
		}
	}
	wsSvc := workspace.NewFSService(guardEng)
	defer handlers.ShutdownWorkspaceWatches()
	defer handlers.ShutdownWorkspaceDevServers()
	if roots := parseAllowedWorkspaceRoots(); len(roots) > 0 {
		wsSvc.SetAllowedRoots(roots)
		if svc, ok := projectSvc.(*project.SQLiteProjectService); ok {
			svc.SetAllowedWorkspaceRoots(roots)
		}
		fmt.Printf("✓ Workspace roots restricted to %d path(s)\n", len(roots))
	}
	debateMgr, debateClose := initDebateManager()
	defer debateClose()
	agentReg, agentRegClose := initAgentRegistry()
	defer agentRegClose()
	flowEngine.SetGateEscalationHandler(floweng.GateEscalationFunc(
		func(ctx context.Context, flow *floweng.Flow, stage *floweng.Stage, gate *floweng.Gate, reason string) {
			title := fmt.Sprintf("Gate escalation: %s", stage.Name)
			input := fmt.Sprintf("gate %s rejected (on_fail=escalate_to_debate): %s", gate.ID, reason)
			d, err := debateMgr.CreateDebate(ctx, &debate.DebateCreateRequest{
				Title:        title,
				GeneratorID:  "builtin-code-artisan",
				CriticID:     "builtin-red-critic",
				InitialInput: input,
				FlowID:       flow.ID,
				StageID:      stage.ID,
			})
			if err != nil {
				log.Printf("[floweng] gate escalation debate failed: flow=%s stage=%s gate=%s err=%v", flow.ID, stage.ID, gate.ID, err)
				return
			}
			log.Printf("[floweng] gate escalation debate created: debate=%s flow=%s stage=%s gate=%s", d.ID, flow.ID, stage.ID, gate.ID)
		},
	))
	services := bootstrap.Services{
		Config:        configSvc,
		Agent:         agentSvc,
		Planner:       plannerSvc,
		Project:       projectSvc,
		Context:       contextSvc,
		Snapshot:      snapshotSvc,
		Debate:        debateMgr,
		Summarize:     summarize.NewSummarizerService(),
		Floweng:       flowEngine,
		Guard:         guardEng,
		Workspace:     wsSvc,
		Skill:         skillReg,
		AgentRegistry: agentReg,
		Session:       sessionStore,
		Memory:        memorySvc,
		RawArchive:    rawArchive,
		AtomicMemory:  atomicSvc,
		MemoryAgent:   memoryAgent,
		SAMG:          samgSvc,
		Preflight:     memory.NewMemoryPreflightService(),
		RequireDurable: true,
	}
	if err := services.Apply(); err != nil {
		return err
	}
	defer services.Reset()

	// Create API server
	config := &api.Config{
		Host:            trust.host,
		Port:            port,
		AuthToken:       trust.authToken,
		AllowedOrigins:  allowedOrigins,
		EnableDebugMode: debugMode,
		AllowRemote:     trust.remoteMode,
	}

	server := api.NewServer(config)
	if err := server.Validate(); err != nil {
		return fmt.Errorf("invalid API trust configuration: %w", err)
	}

	fmt.Printf("\n✓ API server starting on %s:%s\n", trust.host, port)
	fmt.Println("✓ CORS enabled for frontend origins")
	fmt.Println("✓ Request logging enabled")
	fmt.Printf("\nEndpoints:\n")
	fmt.Println("  GET  /health              - Health check")
	fmt.Println("  GET  /api/v1/memory/*     - Memory management")
	fmt.Println("  POST /api/v1/search/*     - Semantic search")
	fmt.Println("  GET  /api/v1/context/*    - Context builder")
	fmt.Println("  GET  /api/v1/agents/*     - Agent management")
	fmt.Println("  GET  /api/v1/blackboard/* - Blackboard collaboration")
	fmt.Println("  POST /api/v1/debates/*    - Debate validation")
	fmt.Println("  GET  /api/v1/plans/*      - Plan management")
	fmt.Printf("\nListening on http://%s:%s\n", trust.host, port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.RunContext(ctx)
}

type closer interface {
	Close() error
}

func configureHookRuntimeControls() {
	enabled := true
	backendhooks.GetHookManager().SetControls(backendhooks.HookRuntimeControls{
		Enabled: &enabled,
		AllowedHooks: []backendhooks.HookType{
			backendhooks.HookBeforeSend,
			backendhooks.HookPostResponse,
			backendhooks.HookOnStream,
			backendhooks.HookBeforeCompress,
			backendhooks.HookBeforeWrite,
			backendhooks.HookOnMessageComplete,
			backendhooks.HookAfterExec,
			backendhooks.HookRestoreState,
			backendhooks.HookOnUserInputSubmitted,
			backendhooks.HookBeforeTaskExecute,
			backendhooks.HookAfterTaskExecute,
			backendhooks.HookOnTaskFailure,
			backendhooks.HookOnTaskComplete,
		},
	})
}

func initPlannerService() (planner.IPlanner, func(), error) {
	dbPath := durableDBPath("planner.db")
	svc, err := planner.NewSQLitePlanner(dbPath)
	if err != nil {
		return nil, func() {}, fmt.Errorf("init planner sqlite service: %w", err)
	}
	return svc, closeFunc(svc), nil
}

func initConfigService() (cfgsvc.IConfigService, func(), error) {
	dbPath := durableDBPath("config.db")
	svc, err := cfgsvc.NewSQLiteConfigService(dbPath)
	if err != nil {
		return nil, func() {}, fmt.Errorf("init config sqlite service: %w", err)
	}
	return svc, closeFunc(svc), nil
}

func registerConfiguredAgents(ctx context.Context, configSvc cfgsvc.IConfigService, agentSvc agent.IAgentService) error {
	if configSvc == nil {
		return fmt.Errorf("config service is nil")
	}
	if agentSvc == nil {
		return fmt.Errorf("agent service is nil")
	}

	for _, role := range []cfgsvc.RoleType{cfgsvc.RoleMain, cfgsvc.RoleCoder, cfgsvc.RoleSub} {
		resolved := configSvc.ResolveConfig("", role)
		if resolved == nil {
			return fmt.Errorf("resolve config for role %q returned nil", role)
		}
		if resolved.APIChannel == nil {
			continue
		}
		agentRole, err := commander.RoleFromConfigRole(role)
		if err != nil {
			return err
		}
		built, err := commander.BuildAgentFromResolved(ctx, agentRole, resolved)
		if err != nil {
			return fmt.Errorf("build configured agent for role %q: %w", role, err)
		}
		agentSvc.RegisterAgent(&agent.Agent{
			Name:   fmt.Sprintf("%s-agent", role),
			Role:   agent.AgentRole(agentRole),
			Status: agent.AgentStatusIdle,
			Model:  built.Model,
		})
		if built.Adapter != nil {
			_ = built.Adapter.Close()
		}
	}
	return nil
}

func initProjectService() (project.IProjectService, func(), error) {
	dbPath := durableDBPath("project.db")
	svc, err := project.NewSQLiteProjectService(dbPath)
	if err != nil {
		return nil, func() {}, fmt.Errorf("init project sqlite service: %w", err)
	}
	return svc, closeFunc(svc), nil
}

func initContextService() (ctxsvc.IContextService, func(), error) {
	dbPath := durableDBPath("context.db")
	svc, err := ctxsvc.NewSQLiteContextService(dbPath)
	if err != nil {
		return nil, func() {}, fmt.Errorf("init context sqlite service: %w", err)
	}
	return svc, closeFunc(svc), nil
}

// initDebateManager opens the durable debate store, degrading to an in-memory
// manager (with a warning) when the database cannot be opened so the server
// still boots. Debate history is non-critical to startup.
func initDebateManager() (debate.IDebateManager, func()) {
	dbPath := durableDBPath("debates.db")
	mgr, err := debate.NewSQLiteDebateManager(dbPath)
	if err != nil {
		log.Printf("warning: debate sqlite store unavailable (%v); using in-memory debate manager", err)
		return debate.NewInMemoryDebateManager(), func() {}
	}
	return mgr, closeFunc(mgr)
}

// initAgentRegistry opens the durable agent registry, degrading to an in-memory
// registry (with a warning) when the database cannot be opened so the server
// still boots. Agent asset history is non-critical to startup.
func initAgentRegistry() (agent.AgentRegistry, func()) {
	dbPath := durableDBPath("agents.db")
	reg, err := agent.NewSQLiteAgentRegistry(dbPath)
	if err != nil {
		log.Printf("warning: agent registry sqlite store unavailable (%v); using in-memory agent registry", err)
		return agent.NewInMemoryAgentRegistry(), func() {}
	}
	return reg, closeFunc(reg)
}

func closeFunc(c closer) func() {
	return func() {
		_ = c.Close()
	}
}

func durableDBPath(filename string) string {
	if root := strings.TrimSpace(os.Getenv("CODEFLOW_DATA_DIR")); root != "" {
		return filepath.Join(root, filename)
	}
	return filepath.Join(".", "data", filename)
}

// parseAllowedWorkspaceRoots reads CODEFLOW_WORKSPACE_ROOTS (comma-separated).
// Empty rejects new Project bindings by default. Desktop migration can opt in
// to unrestricted canonical bindings with
// CODEFLOW_ALLOW_UNRESTRICTED_WORKSPACE_BINDING=1.
func parseAllowedWorkspaceRoots() []string {
	raw := strings.TrimSpace(os.Getenv("CODEFLOW_WORKSPACE_ROOTS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func splitCommaSeparated(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

type trustConfig struct {
	host       string
	authToken  string
	remoteMode bool
}

func loadTrustConfig() (trustConfig, error) {
	config := trustConfig{
		host:       strings.TrimSpace(os.Getenv("CODEFLOW_HOST")),
		authToken:  os.Getenv("CODEFLOW_SIDECAR_TOKEN"),
		remoteMode: strings.EqualFold(strings.TrimSpace(os.Getenv("CODEFLOW_REMOTE_MODE")), "true"),
	}
	if config.host == "" {
		config.host = "127.0.0.1"
	}
	if config.remoteMode {
		config.authToken = os.Getenv("CODEFLOW_REMOTE_TOKEN")
		if config.authToken == "" {
			return trustConfig{}, fmt.Errorf("CODEFLOW_REMOTE_MODE requires CODEFLOW_REMOTE_TOKEN")
		}
	}
	return config, nil
}
