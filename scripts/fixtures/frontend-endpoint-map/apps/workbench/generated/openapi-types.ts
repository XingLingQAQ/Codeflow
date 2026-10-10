/* eslint-disable */
// Fixture generated types (the shape scripts/generate-openapi-types.mjs emits).
export type OpenApiMethod = 'GET' | 'POST' | 'DELETE';

export interface OpenApiOperationMap {
  'post_api_v1_workspace_force_sync': { method: 'POST'; path: '/api/v1/workspace/force-sync' };
  'get_api_v1_projects': { method: 'GET'; path: '/api/v1/projects' };
  'post_api_v1_projects': { method: 'POST'; path: '/api/v1/projects' };
  'get_api_v1_projects_id': { method: 'GET'; path: '/api/v1/projects/{id}' };
  'delete_api_v1_projects_id': { method: 'DELETE'; path: '/api/v1/projects/{id}' };
  'get_api_v1_workspace_list': { method: 'GET'; path: '/api/v1/workspace/list' };
  'post_api_v1_workspace_write': { method: 'POST'; path: '/api/v1/workspace/write' };
  'get_api_v1_workspace_files_id_download': { method: 'GET'; path: '/api/v1/workspace/files/{id}/download' };
  'post_api_v1_workspace_upload': { method: 'POST'; path: '/api/v1/workspace/upload' };
  'get_api_v1_agents_id_logs_stream': { method: 'GET'; path: '/api/v1/agents/{id}/logs/stream' };
  'get_api_v1_agents_id_logs_raw': { method: 'GET'; path: '/api/v1/agents/{id}/logs/raw' };
  'get_api_v1_exports_id': { method: 'GET'; path: '/api/v1/exports/{id}' };
  'get_api_v1_unregistered_only_in_contract': { method: 'GET'; path: '/api/v1/unregistered/only-in-contract' };
  'delete_api_v1_memory_atomic_id': { method: 'DELETE'; path: '/api/v1/memory/atomic/{id}' };
  'post_api_v1_memory_atomic_search': { method: 'POST'; path: '/api/v1/memory/atomic/search' };
}

export interface Project {
  id: string;
  name: 'alpha' | 'beta';
  note?: string | null;
  owner?: Project | string;
}

export interface ProjectList {
  projects: Project[];
  next_cursor?: string | null;
}

export interface ListWorkspaceQuery {
  limit?: number;
  cursor?: string;
}
