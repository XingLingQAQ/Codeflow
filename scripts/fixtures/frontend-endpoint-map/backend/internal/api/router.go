package api

// Fixture router fragment: exercises group prefixes, a nested group, a dotted
// receiver, middleware in front of the handler and a wildcard path.
//
// Three route/contract combinations on purpose:
//   - /api/v1/workspace/force-sync is in openapi.yaml but no route here
//     -> status backend_unregistered
//   - GET /api/v1/debug/state is registered here, in no contract, and only a
//     test file calls it -> status route_only (the test caller never keeps it
//     alive; the route is what the row is left with)
//   - GET /api/v1/realtime/socket is registered here and in no contract; its
//     old client app/workbench/services/realtime.ts is not imported anywhere
//     -> status legacy_unused (deleting the client leaves route_only)
//
// DELETE /api/v1/workspace/files is registered here AND declared in
// openapi.yaml, with an old client that pages still call: that is
// legacy_only, not orphan_client - the router knows the path, so the row is
// not "a call to a path the backend never registered".
func (s *Server) setupRoutes() {
	router.GET("/health", handlers.HealthCheck)
	router.GET("/api/v1/unregistered/only-in-contract", handlers.ContractOnlyRoute)

	v1 := s.router.Group("/api/v1")
	{
		projects := v1.Group("/projects")
		{
			projects.GET("", handlers.ListProjects)
			projects.POST("", handlers.CreateProject)
			projects.GET("/:id", handlers.GetProject)
			projects.DELETE("/:id", handlers.DeleteProject)
		}

		ws := v1.Group("/workspace")
		{
			ws.GET("/list", handlers.ListWorkspace)
			ws.POST("/write", handlers.WriteWorkspaceFile)
			ws.POST("/upload", handlers.UploadWorkspaceFile)
			ws.DELETE("/files", handlers.DeleteWorkspaceFiles)
		}

		agents := v1.Group("/agents")
		{
			agents.GET("/:id/logs/stream", handlers.AgentLogStream)
			agents.GET("/:id/logs/raw", handlers.AgentLogChunks)
		}

		memory := v1.Group("/memory")
		{
			memory.DELETE("/atomic/:id", handlers.DeleteAtomicMemory)
		}

		v1.GET("/debug/state", handlers.DebugState)
		v1.GET("/realtime/socket", handlers.RealtimeSocket)
		v1.GET("/exports/:id", handlers.DownloadExport)
	}
}
