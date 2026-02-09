import {
  AtomicMemory,
  AtomicMemorySearchOptions,
  AtomicMemorySource,
  IAtomicMemoryStore,
} from './types.js';

interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
}

interface AtomicMemoryWire {
  id: string;
  timestamp: number;
  content: string;
  tags?: string[];
  session_id?: string;
  sessionId?: string;
  folder_id?: string | null;
  folderId?: string;
  source: AtomicMemorySource;
  importance: number;
  embedding?: number[];
}

interface AtomicMemoryCreateInput {
  content: string;
  tags?: string[];
  sessionId: string;
  folderId?: string;
  source: AtomicMemorySource;
  importance: number;
  timestamp?: number;
  embedding?: number[];
}

interface AtomicMemoryUpdateInput {
  timestamp?: number;
  content?: string;
  tags?: string[];
  sessionId?: string;
  folderId?: string;
  source?: AtomicMemorySource;
  importance?: number;
  embedding?: number[];
  clearFolderId?: boolean;
}

interface AtomicMemoryServiceConfig {
  baseUrl?: string;
  timeoutMs?: number;
  cacheCapacity?: number;
  minLocalSearchScore?: number;
  fallbackToRemote?: boolean;
}

const DEFAULT_CONFIG: Required<AtomicMemoryServiceConfig> = {
  baseUrl: 'http://localhost:8080',
  timeoutMs: 8000,
  cacheCapacity: 500,
  minLocalSearchScore: 0.35,
  fallbackToRemote: true,
};

const ATOMIC_MEMORY_PATH = '/api/v1/memory/atomic';

type SearchContext = {
  topK: number;
  offset: number;
  tags?: string[];
  sessionId?: string;
  folderId?: string;
  startAt?: number;
  endAt?: number;
};

class AtomicMemoryError extends Error {
  public readonly statusCode?: number;

  constructor(message: string, statusCode?: number) {
    super(message);
    this.name = 'AtomicMemoryError';
    this.statusCode = statusCode;
  }
}

class LocalVectorStore {
  private readonly items = new Map<string, AtomicMemory>();
  private readonly insertionOrder: string[] = [];
  private readonly maxCapacity: number;

  constructor(maxCapacity: number) {
    this.maxCapacity = Math.max(1, maxCapacity);
  }

  add(memory: AtomicMemory): void {
    const normalized = this.normalize(memory);

    if (this.items.has(normalized.id)) {
      this.items.set(normalized.id, normalized);
      return;
    }

    this.items.set(normalized.id, normalized);
    this.insertionOrder.push(normalized.id);
    this.ensureCapacity();
  }

  addMany(memories: AtomicMemory[]): void {
    for (const memory of memories) {
      this.add(memory);
    }
  }

  search(query: string, options?: AtomicMemorySearchOptions): AtomicMemory[] {
    const normalizedQuery = query.trim().toLowerCase();
    if (!normalizedQuery) return [];

    const context = this.toSearchContext(options);
    const keywords = this.tokenize(normalizedQuery);

    const scored = Array.from(this.items.values())
      .filter((memory) => this.matchesFilter(memory, context))
      .map((memory) => {
        const score = this.computeScore(memory, normalizedQuery, keywords);
        return { memory, score };
      })
      .filter((item) => item.score >= 0)
      .sort((a, b) => b.score - a.score);

    const sliced = this.sliceWithPagination(
      scored.map((item) => item.memory),
      context.offset,
      context.topK
    );

    return sliced;
  }

  searchByTimeRange(start: number, end: number): AtomicMemory[] {
    return Array.from(this.items.values())
      .filter((memory) => memory.timestamp >= start && memory.timestamp <= end)
      .sort((a, b) => b.timestamp - a.timestamp);
  }

  searchByTags(tags: string[]): AtomicMemory[] {
    const normalizedTags = tags.map((tag) => tag.trim().toLowerCase()).filter(Boolean);
    if (!normalizedTags.length) return [];

    return Array.from(this.items.values())
      .filter((memory) => {
        const tagSet = new Set(memory.tags.map((tag) => tag.toLowerCase()));
        return normalizedTags.every((tag) => tagSet.has(tag));
      })
      .sort((a, b) => b.timestamp - a.timestamp);
  }

  update(id: string, updates: Partial<AtomicMemory>): void {
    const existing = this.items.get(id);
    if (!existing) return;

    const next: AtomicMemory = {
      ...existing,
      ...updates,
      id: existing.id,
    };

    this.items.set(id, this.normalize(next));
  }

  delete(id: string): void {
    if (!this.items.has(id)) return;

    this.items.delete(id);
    const index = this.insertionOrder.indexOf(id);
    if (index >= 0) {
      this.insertionOrder.splice(index, 1);
    }
  }

  getBySession(sessionId: string): AtomicMemory[] {
    return Array.from(this.items.values())
      .filter((memory) => memory.sessionId === sessionId)
      .sort((a, b) => b.timestamp - a.timestamp);
  }

  private normalize(memory: AtomicMemory): AtomicMemory {
    return {
      ...memory,
      tags: Array.isArray(memory.tags)
        ? memory.tags.map((tag) => tag.trim()).filter((tag) => tag.length > 0)
        : [],
      content: memory.content.trim(),
      sessionId: memory.sessionId.trim(),
      source: memory.source,
      importance: Number.isFinite(memory.importance) ? memory.importance : 0,
      embedding: Array.isArray(memory.embedding) ? [...memory.embedding] : undefined,
      folderId: memory.folderId?.trim() || undefined,
    };
  }

  private ensureCapacity(): void {
    while (this.items.size > this.maxCapacity) {
      const oldest = this.insertionOrder.shift();
      if (!oldest) break;
      this.items.delete(oldest);
    }
  }

  private tokenize(text: string): string[] {
    return text
      .split(/[^\p{L}\p{N}_]+/u)
      .map((token) => token.trim())
      .filter((token) => token.length > 1);
  }

  private computeScore(memory: AtomicMemory, query: string, keywords: string[]): number {
    const haystack = `${memory.content} ${memory.tags.join(' ')}`.toLowerCase();

    if (haystack === query) return 1;

    let score = 0;

    if (haystack.includes(query)) {
      score += 0.65;
    }

    if (keywords.length) {
      const matched = keywords.filter((keyword) => haystack.includes(keyword)).length;
      score += (matched / keywords.length) * 0.3;
    }

    score += Math.max(0, Math.min(1, memory.importance)) * 0.05;

    return Math.min(1, score);
  }

  private matchesFilter(memory: AtomicMemory, context: SearchContext): boolean {
    if (context.sessionId && memory.sessionId !== context.sessionId) return false;
    if (context.folderId && memory.folderId !== context.folderId) return false;

    if (context.startAt !== undefined && memory.timestamp < context.startAt) return false;
    if (context.endAt !== undefined && memory.timestamp > context.endAt) return false;

    if (context.tags?.length) {
      const tagSet = new Set(memory.tags.map((tag) => tag.toLowerCase()));
      const required = context.tags.map((tag) => tag.toLowerCase());
      if (!required.every((tag) => tagSet.has(tag))) return false;
    }

    return true;
  }

  private toSearchContext(options?: AtomicMemorySearchOptions): SearchContext {
    return {
      topK: options?.limit && options.limit > 0 ? options.limit : 10,
      offset: options?.offset && options.offset > 0 ? options.offset : 0,
      tags: options?.tags,
      sessionId: options?.sessionId,
      folderId: options?.folderId,
      startAt: options?.startAt,
      endAt: options?.endAt,
    };
  }

  private sliceWithPagination(items: AtomicMemory[], offset: number, limit: number): AtomicMemory[] {
    if (!items.length) return [];
    const start = Math.max(0, offset);
    if (start >= items.length) return [];
    return items.slice(start, start + Math.max(1, limit));
  }
}

export class AtomicMemoryService implements IAtomicMemoryStore {
  private readonly config: Required<AtomicMemoryServiceConfig>;
  private readonly localStore: LocalVectorStore;

  constructor(config: AtomicMemoryServiceConfig = {}) {
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.localStore = new LocalVectorStore(this.config.cacheCapacity);
  }

  async add(memory: AtomicMemory): Promise<void> {
    const payload: AtomicMemoryCreateInput = {
      content: memory.content,
      tags: memory.tags,
      sessionId: memory.sessionId,
      folderId: memory.folderId,
      source: memory.source,
      importance: memory.importance,
      timestamp: memory.timestamp,
      embedding: memory.embedding,
    };

    const created = await this.request<AtomicMemoryWire>(ATOMIC_MEMORY_PATH, {
      method: 'POST',
      body: payload,
    });

    this.localStore.add(this.fromRemote(created));
  }

  async create(input: AtomicMemoryCreateInput): Promise<AtomicMemory> {
    this.validateCreateInput(input);

    const requestPayload: AtomicMemoryCreateInput = {
      ...input,
      content: input.content.trim(),
      tags: (input.tags || []).map((tag) => tag.trim()).filter(Boolean),
      sessionId: input.sessionId.trim(),
      folderId: input.folderId?.trim() || undefined,
      timestamp: input.timestamp && input.timestamp > 0 ? input.timestamp : Math.floor(Date.now() / 1000),
    };

    const created = await this.request<AtomicMemoryWire>(ATOMIC_MEMORY_PATH, {
      method: 'POST',
      body: requestPayload,
    });

    const normalized = this.fromRemote(created);
    this.localStore.add(normalized);
    return normalized;
  }

  async search(query: string, options?: AtomicMemorySearchOptions): Promise<AtomicMemory[]> {
    const normalizedQuery = query.trim();
    if (!normalizedQuery) return [];

    const local = this.localStore.search(normalizedQuery, options);
    if (local.length && this.hasSufficientLocalScore(local, normalizedQuery)) {
      return local;
    }

    if (!this.config.fallbackToRemote) {
      return local;
    }

    const params = this.buildSearchParams(normalizedQuery, options);
    const remote = await this.request<AtomicMemoryWire[]>(`${ATOMIC_MEMORY_PATH}/search${params}`, {
      method: 'GET',
    });

    const normalized = remote.map((item) => this.fromRemote(item));
    this.localStore.addMany(normalized);

    if (local.length) {
      const merged = this.mergeUniqueById(local, normalized);
      return this.applySearchPostFilter(merged, options);
    }

    return this.applySearchPostFilter(normalized, options);
  }

  async searchByTimeRange(start: number, end: number): Promise<AtomicMemory[]> {
    if (!Number.isFinite(start) || !Number.isFinite(end) || start <= 0 || end <= 0 || start > end) {
      throw new AtomicMemoryError('invalid time range');
    }

    const normalizedStart = Math.floor(start);
    const normalizedEnd = Math.floor(end);

    const local = this.localStore.searchByTimeRange(normalizedStart, normalizedEnd);
    if (local.length) {
      return local;
    }

    return this.search('recent memory', {
      startAt: normalizedStart,
      endAt: normalizedEnd,
      limit: 200,
      offset: 0,
    });
  }

  async searchByTags(tags: string[]): Promise<AtomicMemory[]> {
    const normalizedTags = tags.map((tag) => tag.trim()).filter(Boolean);
    if (!normalizedTags.length) return [];

    const local = this.localStore.searchByTags(normalizedTags);
    if (local.length) {
      return local;
    }

    return this.search(normalizedTags.join(' '), {
      tags: normalizedTags,
      limit: 200,
      offset: 0,
    });
  }

  async update(id: string, updates: Partial<AtomicMemory>): Promise<void> {
    const memoryId = id.trim();
    if (!memoryId) {
      throw new AtomicMemoryError('id is required');
    }

    const payload: AtomicMemoryUpdateInput = this.toUpdatePayload(updates);
    const updated = await this.request<AtomicMemoryWire>(`${ATOMIC_MEMORY_PATH}/${encodeURIComponent(memoryId)}`, {
      method: 'PUT',
      body: payload,
    });

    this.localStore.update(memoryId, this.fromRemote(updated));
  }

  async delete(id: string): Promise<void> {
    const memoryId = id.trim();
    if (!memoryId) {
      throw new AtomicMemoryError('id is required');
    }

    await this.request<{ deleted: boolean; id: string }>(`${ATOMIC_MEMORY_PATH}/${encodeURIComponent(memoryId)}`, {
      method: 'DELETE',
    });

    this.localStore.delete(memoryId);
  }

  async getBySession(sessionId: string): Promise<AtomicMemory[]> {
    const normalizedSessionId = sessionId.trim();
    if (!normalizedSessionId) return [];

    const remote = await this.request<AtomicMemoryWire[]>(
      `${ATOMIC_MEMORY_PATH}/session/${encodeURIComponent(normalizedSessionId)}?limit=200&offset=0`,
      { method: 'GET' }
    );

    const normalized = remote.map((item) => this.fromRemote(item));
    this.localStore.addMany(normalized);
    return normalized;
  }

  private async request<T>(path: string, init: { method: string; body?: unknown }): Promise<T> {
    const url = this.toAbsoluteUrl(path);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), this.config.timeoutMs);

    try {
      const response = await fetch(url, {
        method: init.method,
        headers: {
          'Content-Type': 'application/json',
        },
        body: init.body === undefined ? undefined : JSON.stringify(this.toSnakeCaseBody(init.body)),
        signal: controller.signal,
      });

      const payload = (await response.json()) as ApiResponse<unknown>;

      if (!response.ok || payload.success === false) {
        const message =
          typeof payload.error === 'string' && payload.error.trim().length > 0
            ? payload.error
            : `request failed with status ${response.status}`;
        throw new AtomicMemoryError(message, response.status);
      }

      return payload.data as T;
    } catch (error) {
      if (error instanceof AtomicMemoryError) {
        throw error;
      }
      if (error instanceof Error && error.name === 'AbortError') {
        throw new AtomicMemoryError(`request timeout (${this.config.timeoutMs}ms)`);
      }
      if (error instanceof Error) {
        throw new AtomicMemoryError(error.message);
      }
      throw new AtomicMemoryError('unknown request error');
    } finally {
      clearTimeout(timeout);
    }
  }

  private toAbsoluteUrl(path: string): string {
    const base = this.config.baseUrl.endsWith('/')
      ? this.config.baseUrl.slice(0, -1)
      : this.config.baseUrl;
    const normalizedPath = path.startsWith('/') ? path : `/${path}`;
    return `${base}${normalizedPath}`;
  }

  private toSnakeCaseBody(value: unknown): unknown {
    if (Array.isArray(value)) {
      return value.map((item) => this.toSnakeCaseBody(item));
    }

    if (value && typeof value === 'object') {
      const mapped: Record<string, unknown> = {};
      for (const [key, innerValue] of Object.entries(value as Record<string, unknown>)) {
        if (innerValue === undefined) continue;
        const snake = key
          .replace(/([A-Z])/g, '_$1')
          .replace(/-/g, '_')
          .toLowerCase();
        mapped[snake] = this.toSnakeCaseBody(innerValue);
      }
      return mapped;
    }

    return value;
  }

  private fromRemote(memory: AtomicMemoryWire | AtomicMemory): AtomicMemory {
    const wire = memory as Partial<AtomicMemoryWire>;

    const sessionId =
      typeof wire.sessionId === 'string'
        ? wire.sessionId
        : typeof wire.session_id === 'string'
          ? wire.session_id
          : '';

    const folderId =
      typeof wire.folderId === 'string'
        ? wire.folderId
        : typeof wire.folder_id === 'string'
          ? wire.folder_id
          : undefined;

    return {
      id: memory.id,
      timestamp: memory.timestamp,
      content: memory.content,
      tags: Array.isArray(memory.tags) ? memory.tags : [],
      sessionId,
      folderId,
      source: memory.source,
      importance: memory.importance,
      embedding: Array.isArray(memory.embedding) ? memory.embedding : undefined,
    };
  }

  private buildSearchParams(query: string, options?: AtomicMemorySearchOptions): string {
    const params = new URLSearchParams();
    params.set('query', query);

    if (options?.limit && options.limit > 0) params.set('limit', String(options.limit));
    if (options?.offset && options.offset >= 0) params.set('offset', String(options.offset));
    if (options?.sessionId) params.set('session_id', options.sessionId);
    if (options?.folderId) params.set('folder_id', options.folderId);
    if (options?.tags?.length) params.set('tags', options.tags.join(','));
    if (options?.startAt && options.startAt > 0) params.set('start_at', String(options.startAt));
    if (options?.endAt && options.endAt > 0) params.set('end_at', String(options.endAt));

    const raw = params.toString();
    return raw ? `?${raw}` : '';
  }

  private hasSufficientLocalScore(localItems: AtomicMemory[], query: string): boolean {
    if (!localItems.length) return false;
    const top = localItems[0];
    const joined = `${top.content} ${top.tags.join(' ')}`.toLowerCase();
    const normalized = query.toLowerCase();
    const exact = joined.includes(normalized);

    if (exact) return true;

    const similarity = this.keywordSimilarity(joined, normalized);
    return similarity >= this.config.minLocalSearchScore;
  }

  private keywordSimilarity(content: string, query: string): number {
    const contentTerms = new Set(
      content
        .split(/[^\p{L}\p{N}_]+/u)
        .map((term) => term.trim())
        .filter((term) => term.length > 1)
    );

    const queryTerms = query
      .split(/[^\p{L}\p{N}_]+/u)
      .map((term) => term.trim())
      .filter((term) => term.length > 1);

    if (!queryTerms.length) return 0;

    const matches = queryTerms.filter((term) => contentTerms.has(term)).length;
    return matches / queryTerms.length;
  }

  private mergeUniqueById(local: AtomicMemory[], remote: AtomicMemory[]): AtomicMemory[] {
    const merged = new Map<string, AtomicMemory>();

    for (const item of remote) {
      merged.set(item.id, item);
    }
    for (const item of local) {
      if (!merged.has(item.id)) {
        merged.set(item.id, item);
      }
    }

    return Array.from(merged.values()).sort((a, b) => b.timestamp - a.timestamp);
  }

  private applySearchPostFilter(
    items: AtomicMemory[],
    options?: AtomicMemorySearchOptions
  ): AtomicMemory[] {
    let result = [...items];

    if (options?.sessionId) {
      result = result.filter((item) => item.sessionId === options.sessionId);
    }
    if (options?.folderId) {
      result = result.filter((item) => item.folderId === options.folderId);
    }
    if (options?.startAt !== undefined) {
      result = result.filter((item) => item.timestamp >= options.startAt!);
    }
    if (options?.endAt !== undefined) {
      result = result.filter((item) => item.timestamp <= options.endAt!);
    }
    if (options?.tags?.length) {
      const required = options.tags.map((tag) => tag.toLowerCase());
      result = result.filter((item) => {
        const tags = new Set(item.tags.map((tag) => tag.toLowerCase()));
        return required.every((tag) => tags.has(tag));
      });
    }

    const offset = options?.offset && options.offset > 0 ? options.offset : 0;
    const limit = options?.limit && options.limit > 0 ? options.limit : result.length;
    return result.slice(offset, offset + limit);
  }

  private validateCreateInput(input: AtomicMemoryCreateInput): void {
    if (!input.content || !input.content.trim()) {
      throw new AtomicMemoryError('content is required');
    }
    if (!input.sessionId || !input.sessionId.trim()) {
      throw new AtomicMemoryError('sessionId is required');
    }
    if (!['user', 'assistant', 'system'].includes(input.source)) {
      throw new AtomicMemoryError('source must be one of user/assistant/system');
    }
    if (!Number.isFinite(input.importance) || input.importance < 0 || input.importance > 1) {
      throw new AtomicMemoryError('importance must be in range [0,1]');
    }
  }

  private toUpdatePayload(updates: Partial<AtomicMemory>): AtomicMemoryUpdateInput {
    const payload: AtomicMemoryUpdateInput = {};

    if (updates.timestamp !== undefined) payload.timestamp = updates.timestamp;
    if (updates.content !== undefined) payload.content = updates.content;
    if (updates.tags !== undefined) payload.tags = updates.tags;
    if (updates.sessionId !== undefined) payload.sessionId = updates.sessionId;
    if (updates.source !== undefined) payload.source = updates.source;
    if (updates.importance !== undefined) payload.importance = updates.importance;
    if (updates.embedding !== undefined) payload.embedding = updates.embedding;

    if (updates.folderId !== undefined) {
      if (updates.folderId === '') {
        payload.clearFolderId = true;
      } else {
        payload.folderId = updates.folderId;
      }
    }

    return payload;
  }
}

export { AtomicMemoryError, LocalVectorStore };
