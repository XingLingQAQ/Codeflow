import { ReactNode } from 'react';

// --- UI Types (existing) ---

export enum ViewMode {
  HOME = 'home',
  PROJECTS = 'projects',
  SESSIONS = 'sessions',
  MEMORY = 'memory',
  AGENTS = 'agents',
  PLAN = 'plan',
  SETTINGS = 'settings',
}

export interface NavItem {
  id: ViewMode;
  label: string;
  icon: ReactNode;
  active?: boolean;
}

// --- API Response Types ---

export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  has_more: boolean;
}

// --- Project ---

export type ProjectStatus = 'active' | 'planning' | 'paused' | 'completed' | 'archived';

export interface Project {
  id: string;
  title: string;
  description?: string;
  status: ProjectStatus;
  progress: number;
  tags?: string[];
  git_branch?: string;
  created_at: number;
  updated_at: number;
  last_active: number;
  plan_ids?: string[];
  metadata?: Record<string, unknown>;
}

export interface ProjectListResponse {
  projects: Project[];
  total: number;
  has_more: boolean;
}

// --- Plan ---

export interface Plan {
  id: string;
  title: string;
  description?: string;
  status: string;
  task_count: number;
  completed_count: number;
  created_at: number;
  updated_at: number;
  metadata?: Record<string, unknown>;
}

export type TaskStatus = 'pending' | 'in_progress' | 'completed' | 'failed' | 'blocked';
export type TaskPriority = 'low' | 'medium' | 'high' | 'critical';

export interface PlanTask {
  id: string;
  plan_id: string;
  title: string;
  description?: string;
  status: TaskStatus;
  priority: TaskPriority;
  model?: string;
  order: number;
  dependencies?: string[];
  assignee?: string;
  estimated_ms?: number;
  actual_ms?: number;
  started_at?: number;
  completed_at?: number;
  created_at: number;
  updated_at: number;
  metadata?: Record<string, unknown>;
}

// --- Agent ---

export type AgentRole = 'orchestrator' | 'coder' | 'reviewer' | 'researcher' | 'planner' | 'tester';
export type AgentStatus = 'idle' | 'running' | 'paused' | 'stopped' | 'error';

export interface Agent {
  id: string;
  name: string;
  role: AgentRole;
  status: AgentStatus;
  model: string;
  session_id?: string;
  started_at: number;
  last_active_at: number;
  tokens_used: number;
  task_count: number;
  error_count: number;
}

// --- Conversation ---

export interface CallTrace {
  id: string;
  parent_id?: string;
  agent_id: string;
  agent_role: AgentRole;
  tool_name: string;
  input?: Record<string, unknown>;
  output?: string;
  status: string;
  start_time: number;
  end_time?: number;
  duration_ms?: number;
  children?: CallTrace[];
}

export interface Conversation {
  id: string;
  session_id: string;
  status: string;
  started_at: number;
  updated_at: number;
  agent_ids: string[];
  trace_root?: CallTrace;
  message_count: number;
}

// --- Memory ---

export type MemoryType = 'fact' | 'preference' | 'decision' | 'context' | 'summary';
export type MemoryStatus = 'active' | 'archived' | 'deleted';
export type MemorySource = 'user' | 'agent' | 'system' | 'extraction';

export interface MemoryItem {
  id: string;
  content: string;
  type: MemoryType;
  status: MemoryStatus;
  session_id: string;
  message_index: number;
  timestamp: number;
  heat: number;
  surprise: number;
  tags?: string[];
  source: MemorySource;
  is_permanent: boolean;
  archived_at?: number;
}

// --- SAMG (Semantic Graph) ---

export interface MemoryNode {
  id: string;
  x: number;
  y: number;
  label: string;
  type: string;
  color: string;
  icon: string;
}

export interface SAMGTripleNode {
  '@id': string;
  '@type'?: string[];
  label?: string;
}

export interface SAMGTriple {
  '@id': string;
  subject: SAMGTripleNode;
  predicate: string;
  object: { node?: SAMGTripleNode; literal?: { '@value': unknown; '@type'?: string } };
  confidence: number;
  timestamp: number;
  source: { session_id: string; extraction_method: string };
  metadata?: Record<string, unknown>;
}

export interface SAMGEntity {
  '@id': string;
  '@type': string[];
  label: string;
  description?: string;
  properties?: Record<string, unknown>;
  aliases?: string[];
  created_at: number;
  updated_at: number;
}

// --- Config ---

export interface APIChannel {
  id: string;
  name: string;
  provider: string;
  api_key?: string;
  base_url?: string;
  enabled: boolean;
}

export interface GlobalConfig {
  default_model: string;
  api_pool: APIChannel[];
  public_mcp: string[];
  summary_threshold?: number;
  max_retries?: number;
  timeout?: number;
}

export interface SessionConfig {
  session_id: string;
  mode: string;
  override_model?: string;
  temperature?: number;
  max_tokens?: number;
}

// --- Blackboard ---

export type BlackboardEntryType = 'proposal' | 'decision' | 'note' | 'question' | 'answer';

export interface BlackboardEntry {
  id: string;
  type: BlackboardEntryType;
  title: string;
  content: string;
  author: string;
  agent_id?: string;
  version: number;
  created_at: number;
  updated_at: number;
  metadata?: Record<string, unknown>;
  tags?: string[];
  parent_id?: string;
  is_archived: boolean;
}

// --- Debate ---

export type DebateStatus = 'pending' | 'in_progress' | 'resolved' | 'deadlocked';

export interface Debate {
  id: string;
  title: string;
  description?: string;
  status: DebateStatus;
  current_round: number;
  max_rounds: number;
  generator_id: string;
  critic_id: string;
  mediator_id?: string;
  rounds: DebateRound[];
  conflicts: Conflict[];
  solutions: Solution[];
  selected_solution?: string;
  created_at: number;
  updated_at: number;
  resolved_at?: number;
  metadata?: Record<string, unknown>;
}

export interface DebateRound {
  number: number;
  generator_input: string;
  generator_output?: string;
  critic_feedback?: string;
  conflicts_found?: string[];
  started_at: number;
  completed_at?: number;
}

export interface Conflict {
  id: string;
  debate_id: string;
  round_number: number;
  type: string;
  severity: string;
  status: string;
  description: string;
  location?: string;
  generator_view?: string;
  critic_view?: string;
  resolution?: string;
  resolved_by?: string;
  created_at: number;
  resolved_at?: number;
}

export interface Solution {
  id: string;
  debate_id: string;
  proposed_by: string;
  role: string;
  title: string;
  description: string;
  code?: string;
  pros?: string[];
  cons?: string[];
  score: number;
  created_at: number;
}

// --- Snapshot ---

export interface Snapshot {
  id: string;
  git_hash: string;
  conversation_state: string;
  vector_pointer: string;
  memory_graph_version: string;
  description: string;
  created_at: string;
  session_id?: string;
  tags?: string[];
}

// --- Hook ---

export interface HookConfig {
  name: string;
  type: string;
  enabled: boolean;
  priority: number;
  timeout: number;
  retry_count: number;
  metadata: Record<string, unknown>;
}

export interface HookEvent {
  id: string;
  hook_name: string;
  hook_type: string;
  timestamp: string;
  duration: number;
  success: boolean;
  error?: string;
  input_size: number;
  output_size: number;
  metadata?: Record<string, unknown>;
}

// --- Vote ---

export type VoteStatus = 'pending' | 'approved' | 'rejected' | 'expired';

export interface Vote {
  id: string;
  entry_id: string;
  title: string;
  description?: string;
  initiator: string;
  status: VoteStatus;
  threshold: number;
  votes: Record<string, boolean>;
  created_at: number;
  expires_at: number;
  resolved_at?: number;
}

// --- Legacy UI types (kept for backward compat) ---

export interface AgentPreset {
  title: string;
  description: string;
  count: number;
  tags: string[];
  avatars: string[];
  color: string;
}
