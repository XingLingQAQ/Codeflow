export interface ContextLoaderConfig {
    maxTokenBudget: number;
    cacheEnabled: boolean;
    shadowRoot: string;
    projectRoot: string;
}
export interface LoadedContext {
    filePath: string;
    content: string;
    tokenCount: number;
}
export interface ContextLoadResult {
    contexts: LoadedContext[];
    totalTokens: number;
    budgetRemaining: number;
}
export declare class ContextLoader {
    private readonly config;
    private readonly cache;
    private readonly maxCacheSize;
    constructor(config?: Partial<ContextLoaderConfig>);
    loadContext(intent: string): Promise<ContextLoadResult>;
    loadWithDependencies(filePath: string): Promise<ContextLoadResult>;
    clearCache(): void;
    private estimateTokens;
    private readWithCache;
    private evictIfNeeded;
    private readFileOrEmpty;
    private scanIntentFiles;
    private extractKeywords;
    private computeRelevance;
    private resolveIntentDocPath;
    private resolveImportIntentPath;
    private extractImportPaths;
}
//# sourceMappingURL=ContextLoader.d.ts.map