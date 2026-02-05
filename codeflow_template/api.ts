export const API_BASE = 'http://localhost:8080';
export const WS_BASE = 'ws://localhost:8080';

export const API_ENDPOINTS = {
  health: `${API_BASE}/health`,
  memory: `${API_BASE}/api/v1/memory`,
  search: `${API_BASE}/api/v1/search`,
  context: `${API_BASE}/api/v1/context`,
  agents: `${API_BASE}/api/v1/agents`,
  blackboard: `${API_BASE}/api/v1/blackboard`,
  debates: `${API_BASE}/api/v1/debates`,
  plans: `${API_BASE}/api/v1/plans`,
} as const;

export const WS_ENDPOINTS = {
  events: `${WS_BASE}/ws`,
} as const;
