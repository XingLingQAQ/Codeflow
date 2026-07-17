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
	"github.com/codeflow/backend/internal/audit"
	"github.com/codeflow/backend/internal/bootstrap"
	"github.com/codeflow/backend/internal/commander"
	cfgsvc "github.com/codeflow/backend/internal/config"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/debate"
	"github.com/codeflow/backend/internal/floweng"
	"github.com/codeflow/backend/internal/guard"
	backendhooks "github.com/codeflow/backend/internal/hooks"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
	"github.com/codeflow/backend/internal/skill"
	"github.com/codeflow/backend/internal/snapshot"
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
	// Get port from environment or use dynamic port (0 = OS assigns)
	port := os.Getenv("PORT")
	if port == "" {
		port = "0"
	}

	// Get allowed origins from environment or use defaults
	allowedOrigins := []string{
		"http://localhost:3000",
		"http://localhost:5173",
	}
	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		// Could parse comma-separated origins here
		allowedOrigins = append(allowedOrigins, origins)
	}

	// Check if debug mode is enabled
	debugMode := os.Getenv("DEBUG") == "true"

	configureHookRuntimeControls()

	configSvc, configClose, err := initConfigService()
	if err != nil {
		return err
	}
	defer configClose()

	agentSvc := agent.NewInMemoryAgentService()

	if err := registerConfiguredAgents(configSvc, agentSvc); err != nil {
		return err
	}

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
	skillReg, err := skill.NewSQLiteRegistry(durableDBPath("skills.db"))
	if err != nil {
		return fmt.Errorf("init skill sqlite: %w", err)
	}
	defer func() { _ = skillReg.Close() }()
	// Optional bulk import from project skills directory (ignore if missing).
	if n, err := skillReg.ImportMarkdownDir(context.Background(), filepath.Join(".", ".codeflow", "skills")); err == nil && n > 0 {
		fmt.Printf("✓ Imported %d skill(s) from .codeflow/skills\n", n)
	}
	auditStore := audit.NewFileAuditStorage(&audit.FileStorageConfig{
		LogDir: durableDBPath("audit"),
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
	if roots := parseAllowedWorkspaceRoots(); len(roots) > 0 {
		wsSvc.SetAllowedRoots(roots)
		fmt.Printf("✓ Workspace roots restricted to %d path(s)\n", len(roots))
	}
	services := bootstrap.Services{
		Config:    configSvc,
		Agent:     agentSvc,
		Planner:   plannerSvc,
		Project:   projectSvc,
		Context:   contextSvc,
		Snapshot:  snapshotSvc,
		Debate:    debate.NewInMemoryDebateManager(),
		Summarize: summarize.NewSummarizerService(),
		Floweng:   flowEngine,
		Guard:     guardEng,
		Workspace: wsSvc,
		Skill:     skillReg,
	}
	if err := services.Apply(); err != nil {
		return err
	}
	defer services.Reset()

	// Create API server
	config := &api.Config{
		Port:            port,
		AllowedOrigins:  allowedOrigins,
		EnableDebugMode: debugMode,
	}

	server := api.NewServer(config)

	fmt.Printf("\n✓ API server starting on port %s\n", port)
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
	fmt.Printf("\nListening on http://localhost:%s\n", port)

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

func registerConfiguredAgents(configSvc cfgsvc.IConfigService, agentSvc agent.IAgentService) error {
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
		built, err := commander.BuildAgentFromResolved(agentRole, resolved)
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
// Empty means unrestricted (desktop default). When set, all workspace API roots
// must resolve under one of the listed paths.
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
