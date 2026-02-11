import { AtomicMemory, AtomicMemorySearchOptions, AtomicMemorySource, IAtomicMemoryStore } from './types.js';
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
interface AtomicMemoryServiceConfig {
    baseUrl?: string;
    timeoutMs?: number;
    cacheCapacity?: number;
    minLocalSearchScore?: number;
    fallbackToRemote?: boolean;
}
declare class AtomicMemoryError extends Error {
    readonly statusCode?: number;
    constructor(message: string, statusCode?: number);
}
declare class LocalVectorStore {
    private readonly items;
    private readonly insertionOrder;
    private readonly maxCapacity;
    constructor(maxCapacity: number);
    add(memory: AtomicMemory): void;
    addMany(memories: AtomicMemory[]): void;
    search(query: string, options?: AtomicMemorySearchOptions): AtomicMemory[];
    searchByTimeRange(start: number, end: number): AtomicMemory[];
    searchByTags(tags: string[]): AtomicMemory[];
    update(id: string, updates: Partial<AtomicMemory>): void;
    delete(id: string): void;
    getBySession(sessionId: string): AtomicMemory[];
    private normalize;
    private ensureCapacity;
    private tokenize;
    private computeScore;
    private matchesFilter;
    private toSearchContext;
    private sliceWithPagination;
}
export declare class AtomicMemoryService implements IAtomicMemoryStore {
    private readonly config;
    private readonly localStore;
    constructor(config?: AtomicMemoryServiceConfig);
    add(memory: AtomicMemory): Promise<void>;
    create(input: AtomicMemoryCreateInput): Promise<AtomicMemory>;
    search(query: string, options?: AtomicMemorySearchOptions): Promise<AtomicMemory[]>;
    searchByTimeRange(start: number, end: number): Promise<AtomicMemory[]>;
    searchByTags(tags: string[]): Promise<AtomicMemory[]>;
    update(id: string, updates: Partial<AtomicMemory>): Promise<void>;
    delete(id: string): Promise<void>;
    getBySession(sessionId: string): Promise<AtomicMemory[]>;
    private request;
    private toAbsoluteUrl;
    private toSnakeCaseBody;
    private fromRemote;
    private buildSearchParams;
    private hasSufficientLocalScore;
    private keywordSimilarity;
    private mergeUniqueById;
    private applySearchPostFilter;
    private validateCreateInput;
    private toUpdatePayload;
}
export { AtomicMemoryError, LocalVectorStore };
//# sourceMappingURL=AtomicMemoryService.d.ts.map