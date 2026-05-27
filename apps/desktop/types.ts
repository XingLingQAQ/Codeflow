import { ReactNode } from 'react';

// --- UI Types (existing) ---

export enum ViewMode {
  HOME = 'home',
  PROJECTS = 'projects',
  SESSIONS = 'sessions',
  MEMORY = 'memory',
  AGENTS = 'agents',
  PLAN = 'plan',
  PLUGINS = 'plugins',
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

export type WorkflowLane = 'project' | 'plan' | 'task' | 'trace' | 'archive' | 'audit';

export interface WorkflowApproval {
  stage: string;
  status: 'pending' | 'in_review' | 'approved' | 'rejected' | string;
  owner?: string;
  note?: string;
  updated_at?: number;
}

export interface WorkflowDecision {
  id: string;
  summary: string;
  owner?: string;
  reason?: string;
  timestamp?: number;
}

export interface WorkflowReplayItem {
  id: string;
  lane: WorkflowLane;
  title: string;
  message: string;
  timestamp?: number;
  status?: string;
  actor?: string;
  evidence?: string;
  sourceId?: string;
}

export interface WorkflowMetadata {
  workflow_id?: string;
  workflow_title?: string;
  blueprint?: string;
  template?: string;
  replay_session_id?: string;
  approval?: WorkflowApproval[];
  decisions?: WorkflowDecision[];
  timeline?: WorkflowReplayItem[];
}

export interface WorkflowReplayData {
  title: string;
  subtitle?: string;
  status?: string;
  summary?: string;
  items: WorkflowReplayItem[];
}

export interface WorkflowOverviewSummary {
  project_count: number;
  plan_count: number;
  task_count: number;
  completed_tasks: number;
  in_progress_tasks: number;
  blocked_tasks: number;
  pending_tasks: number;
  agent_count: number;
  session_count: number;
  audit_count: number;
  progress: number;
}

export interface WorkflowOverview {
  project: Project;
  plans: Plan[];
  tasks: PlanTask[];
  agents: Agent[];
  session_ids: string[];
  latest_audit?: AuditLogEntry;
  summary: WorkflowOverviewSummary;
}

export interface WorkflowTimelineEvent {
  id: string;
  type: string;
  lane: string;
  title: string;
  detail?: string;
  status?: string;
  source?: string;
  timestamp: number;
  project_id?: string;
  plan_id?: string;
  task_id?: string;
  session_id?: string;
  agent_id?: string;
  audit_id?: string;
  trace_id?: string;
}

export interface WorkflowTimelineResponse {
  project_id: string;
  session_ids: string[];
  events: WorkflowTimelineEvent[];
  summary: WorkflowOverviewSummary;
}

export interface WorkflowReplayEvent {
  id: string;
  type: string;
  lane: string;
  speaker: string;
  message: string;
  evidence?: string;
  status?: string;
  timestamp: number;
  project_id?: string;
  plan_id?: string;
  task_id?: string;
  session_id?: string;
  agent_id?: string;
  audit_id?: string;
  trace_id?: string;
}

export interface WorkflowReplaySummary {
  event_count: number;
  trace_count: number;
  audit_count: number;
}

export interface WorkflowReplayResponse {
  project_id: string;
  session_id?: string;
  events: WorkflowReplayEvent[];
  trace?: CallTrace;
  agents?: Agent[];
  audit_entries?: AuditLogEntry[];
  summary: WorkflowReplaySummary;
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

export interface PlanListResponse {
  plans: Plan[];
  total: number;
  has_more: boolean;
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

export interface PlanTaskListResponse {
  tasks: PlanTask[];
  total: number;
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

export type PluginHealth = 'healthy' | 'degraded' | 'disabled' | 'unknown';
export type PluginScope = 'workspace' | 'session' | 'project' | 'global' | string;
export type PluginSource = 'installed' | 'marketplace' | 'builtin' | string;

export interface PluginPermission {
  id: string;
  name: string;
  description?: string;
  level?: 'read' | 'write' | 'admin' | string;
  granted?: boolean;
  required?: boolean;
}

export interface PluginConfigField {
  key: string;
  label?: string;
  type?: string;
  value?: unknown;
  required?: boolean;
  masked?: boolean;
  description?: string;
}

export interface PluginMetrics {
  installs?: number;
  downloads?: number;
  active_sessions?: number;
  error_rate?: number;
  latency_ms?: number;
}

export interface PluginManifest {
  id: string;
  name: string;
  display_name?: string;
  summary?: string;
  description?: string;
  version?: string;
  author?: string;
  vendor?: string;
  category?: string;
  tags?: string[];
  source?: PluginSource;
  scope?: PluginScope;
  enabled?: boolean;
  installed?: boolean;
  featured?: boolean;
  verified?: boolean;
  health?: PluginHealth;
  homepage?: string;
  repository?: string;
  icon?: string;
  updated_at?: number;
  installed_at?: number;
  permissions?: PluginPermission[];
  config?: PluginConfigField[];
  metrics?: PluginMetrics;
  metadata?: Record<string, unknown>;
}

export interface PluginOverview {
  total: number;
  installed: number;
  enabled: number;
  marketplace: number;
  unhealthy: number;
  categories?: Record<string, number>;
}

export interface PluginListResponse {
  plugins: PluginManifest[];
  total: number;
  has_more?: boolean;
  summary?: PluginOverview;
}

export interface PluginCatalogResponse {
  plugins: PluginManifest[];
  total: number;
  featured?: PluginManifest[];
}

export interface PluginDetailResponse {
  plugin: PluginManifest;
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

export interface ConversationTraceResponse {
  session_id: string;
  trace?: CallTrace;
  agents?: Agent[];
  duration_ms?: number;
}

// --- Memory ---

export type MemoryType = 'stm' | 'ltm';
export type MemoryStatus = 'active' | 'archived' | 'pending_archive' | 'pending_delete';
export type MemorySource = 'user' | 'assistant' | 'system';

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

export interface MemoryListResponse {
  items: MemoryItem[];
  total: number;
  has_more: boolean;
  next_offset?: number;
}

// --- Atomic Memory (Unified STM/LTM) ---

export type MemoryTier = 'hot' | 'warm' | 'cold';

export interface AtomicMemory {
  id: string;
  timestamp: number;
  content: string;
  tags: string[];
  session_id: string;
  folder_id?: string;
  source: 'user' | 'assistant' | 'system';
  importance: number;
  tier: MemoryTier;
  heat: number;
  surprise: number;
}

// --- Raw Archive ---

export type RawEntryType = 'conversation' | 'code_diff' | 'document' | 'workflow_event';

export interface RawEntry {
  id: string;
  type: RawEntryType;
  content: string;
  metadata?: Record<string, unknown>;
  timestamp: number;
  session_id: string;
}

export interface RawArchiveListResponse {
  entries: RawEntry[];
  count: number;
  total: number;
}

export interface AuditActor {
  id: string;
  type: string;
  name?: string;
  session_id?: string;
}

export interface AuditResource {
  type: string;
  id: string;
  name?: string;
  path?: string;
}

export interface AuditTrace {
  request_id?: string;
  session_id?: string;
  task_id?: string;
  agent_id?: string;
  method?: string;
  path?: string;
  route?: string;
  status_code?: number;
  latency_ms?: number;
}

export interface AuditLogEntry {
  id: string;
  timestamp: number;
  event_type: string;
  severity: string;
  actor: AuditActor;
  resource: AuditResource;
  action: string;
  outcome: string;
  trace?: AuditTrace;
  details?: Record<string, unknown>;
  previous_hash: string;
  hash: string;
}

export interface AuditLogListResponse {
  entries: AuditLogEntry[];
  total: number;
  has_more: boolean;
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
  pointers?: SAMGPointer[];
  activation?: number;
  access_count?: number;
  hidden?: boolean;
  created_at: number;
  updated_at: number;
}

// --- SAMG Pointer System ---

export interface SAMGPointer {
  source_id: string;
  source_type: string;
  summary: string;
  line_range?: string;
  file_path?: string;
  timestamp: number;
  relevance: number;
}

export interface ResolvedPointer extends SAMGPointer {
  resolved_content?: string;
  session_id?: string;
}

export interface QueryMemoryNode {
  id: string;
  '@type'?: string[];
  label: string;
  description?: string;
  properties?: Record<string, unknown>;
  aliases?: string[];
  activation: number;
  hop: number;
  pointers?: ResolvedPointer[];
}

export interface QueryMemoryResponse {
  activated_nodes: QueryMemoryNode[];
  context_block: string;
}

export interface SAMGPathResponse {
  source_id: string;
  target_id: string;
  paths: string[][];
  count: number;
}

export interface SAMGGraphContext {
  '@vocab'?: string;
  '@base'?: string;
}

export interface SAMGGraph {
  '@context': SAMGGraphContext;
  '@id': string;
  '@type': string;
  '@graph': SAMGTriple[];
  metadata: SAMGGraphStats;
}

export interface SAMGGraphImportResult {
  message: string;
  triple_count: number;
  deduplicated_count: number;
  total_triples: number;
}

export type MemorySourceKind = 'atomic_memory' | 'samg_pointer' | 'raw_archive';

export interface MemoryAgentSource {
  kind: MemorySourceKind;
  id: string;
  title?: string;
  summary?: string;
  content?: string;
  session_id?: string;
  timestamp?: number;
  node_id?: string;
  node_label?: string;
  source_id?: string;
  source_type?: string;
  file_path?: string;
  line_range?: string;
  relevance?: number;
}

export interface MemoryAgentRetrieveResult {
  atomic_memories: AtomicMemory[];
  samg_nodes?: QueryMemoryNode[];
  sources?: MemoryAgentSource[];
  total_found: number;
}

export interface MemoryAgentContextResult {
  context_block: string;
  source_count: number;
  atomic_memories?: AtomicMemory[];
  samg_nodes?: QueryMemoryNode[];
  sources?: MemoryAgentSource[];
}

export interface SAMGGraphStats {
  created_at: number;
  updated_at: number;
  triple_count: number;
  entity_count: number;
  predicate_count: number;
  version: string;
}

export interface SAMGStats {
  graph_stats: SAMGGraphStats;
  decay_stats: Record<string, unknown>;
  extractor_info: Record<string, unknown>;
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
  mode: 'development' | 'research' | 'creative';
  override_model?: string;
  temperature?: number;
  max_tokens?: number;
}

export interface ResolvedConfig {
  model: string;
  temperature: number;
  top_p?: number;
  max_tokens?: number;
  api_channel?: APIChannel;
  mcp_tools: string[];
  system_prompt?: string;
  timeout?: number;
  max_retries?: number;
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
