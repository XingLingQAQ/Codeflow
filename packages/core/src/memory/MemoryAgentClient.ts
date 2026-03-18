/**
 * MemoryAgentClient - 统一记忆调度层客户端
 *
 * 对接后端 MemoryAgent API，提供 Ingest/Retrieve/AssembleContext 三个核心操作。
 */

export interface IngestRequest {
  content: string;
  type?: 'conversation' | 'code_diff' | 'document';
  session_id?: string;
  source?: 'user' | 'assistant' | 'system';
  tags?: string[];
  metadata?: Record<string, unknown>;
}

export interface CodeChangeMemoryIngestRequest {
  summary: string;
  session_id?: string;
  task_id?: string;
  agent_id?: string;
  snapshot_id?: string;
  files?: string[];
  event_type?: string;
  metadata?: Record<string, unknown>;
}

export type MemorySourceKind = 'atomic_memory' | 'samg_pointer' | 'raw_archive';


export interface MemoryAgentAtomicMemory {
  id: string;
  timestamp: number;
  content: string;
  tags: string[];
  session_id: string;
  source: string;
  importance: number;
  tier: string;
  heat: number;
  surprise: number;
}

export interface MemoryAgentResolvedPointer {
  source_id: string;
  source_type: string;
  summary: string;
  line_range?: string;
  file_path?: string;
  timestamp: number;
  relevance: number;
  resolved_content?: string;
  session_id?: string;
}

export interface MemoryAgentNode {
  id: string;
  label: string;
  activation: number;
  hop: number;
  pointers?: MemoryAgentResolvedPointer[];
}

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

export interface IngestResult {
  raw_archive_id: string;
  atomic_memory_id: string;
  samg_triples_count?: number;
}

export interface RetrieveRequest {
  query: string;
  max_results?: number;
  min_heat?: number;
  tier?: 'hot' | 'warm' | 'cold';
  session_id?: string;
}

export interface RetrieveResult {
  atomic_memories: MemoryAgentAtomicMemory[];
  samg_nodes?: MemoryAgentNode[];
  sources?: MemoryAgentSource[];
  total_found: number;
}

export interface ContextRequest {
  session_id: string;
  query?: string;
  max_tokens?: number;
}

export interface ContextResult {
  context_block: string;
  source_count: number;
  atomic_memories?: MemoryAgentAtomicMemory[];
  samg_nodes?: MemoryAgentNode[];
  sources?: MemoryAgentSource[];
}

interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
}

export class MemoryAgentClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;

  constructor(baseUrl = 'http://localhost:8080', timeoutMs = 8000) {
    this.baseUrl = baseUrl;
    this.timeoutMs = timeoutMs;
  }

  /**
   * 统一写入：同时归档到 Raw Archive + Atomic Memory
   */
  async ingest(req: IngestRequest): Promise<IngestResult> {
    return this.post<IngestResult>('/api/v1/memory/agent/ingest', req);
  }

  /**
   * 统一检索：从 Atomic Memory 语义搜索
   */
  async retrieve(req: RetrieveRequest): Promise<RetrieveResult> {
    return this.post<RetrieveResult>('/api/v1/memory/agent/retrieve', req);
  }

  /**
   * 上下文组装：为 AI 请求构建记忆上下文块
   */
  async assembleContext(req: ContextRequest): Promise<ContextResult> {
    return this.post<ContextResult>('/api/v1/memory/agent/context', req);
  }

  /**
   * 代码变更记忆写入：将 CodeChangeEvent 摘要沉淀到统一记忆主线。
   */
  async ingestCodeChange(req: CodeChangeMemoryIngestRequest): Promise<IngestResult> {
    return this.ingest({
      content: req.summary,
      type: 'code_diff',
      session_id: req.session_id,
      source: 'system',
      tags: ['code-change-event', req.event_type ?? 'code_diff'].filter(Boolean),
      metadata: {
        task_id: req.task_id,
        agent_id: req.agent_id,
        snapshot_id: req.snapshot_id,
        files: req.files,
        event_type: req.event_type,
        ...req.metadata,
      },
    });
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);

    try {
      const res = await fetch(`${this.baseUrl}${path}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal: controller.signal,
      });

      const json = (await res.json()) as ApiResponse<T>;
      if (!json.success || !json.data) {
        throw new Error(json.error || 'MemoryAgent request failed');
      }
      return json.data;
    } finally {
      clearTimeout(timer);
    }
  }
}
