package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/codeflow/backend/internal/api"
	ctxsvc "github.com/codeflow/backend/internal/context"
	"github.com/codeflow/backend/internal/planner"
	"github.com/codeflow/backend/internal/project"
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

	plannerSvc, plannerClose, err := initPlannerService()
	if err != nil {
		return err
	}
	defer plannerClose()
	planner.SetPlanner(plannerSvc)

	projectSvc, projectClose, err := initProjectService()
	if err != nil {
		return err
	}
	defer projectClose()
	project.SetProjectService(projectSvc)

	contextSvc, contextClose, err := initContextService()
	if err != nil {
		return err
	}
	defer contextClose()
	ctxsvc.SetContextService(contextSvc)

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

	return server.Run()
}

type closer interface {
	Close() error
}

func initPlannerService() (planner.IPlanner, func(), error) {
	dbPath := durableDBPath("planner.db")
	svc, err := planner.NewSQLitePlanner(dbPath)
	if err != nil {
		return nil, func() {}, fmt.Errorf("init planner sqlite service: %w", err)
	}
	return svc, closeFunc(svc), nil
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
