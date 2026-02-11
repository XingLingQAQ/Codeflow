/**
 * 渐进式记忆披露类型定义
 */
import { MemoryMatch } from '../hooks/types.js';
/**
 * 披露建议
 */
export interface DisclosureSuggestion {
    id: string;
    type: 'memory' | 'context' | 'rule';
    title: string;
    preview: string;
    fullContent: string;
    relevanceScore: number;
    source: MemoryMatch['source'];
    metadata?: Record<string, unknown>;
    timestamp: number;
}
/**
 * 披露响应
 */
export interface DisclosureResponse {
    suggestions: DisclosureSuggestion[];
    queryTime: number;
    totalMatches: number;
    hasMore: boolean;
}
/**
 * 披露配置
 */
export interface DisclosureConfig {
    maxSuggestions: number;
    minRelevanceScore: number;
    timeoutMs: number;
    enableCache: boolean;
    cacheMaxSize: number;
    cacheTtlMs: number;
    debounceMs: number;
    previewMaxLength: number;
}
/**
 * 上下文注入选项
 */
export interface ContextInjectionOptions {
    position: 'prepend' | 'append' | 'system';
    format: 'raw' | 'markdown' | 'xml';
    maxTokens?: number;
    deduplicate: boolean;
}
/**
 * 注入结果
 */
export interface InjectionResult {
    injectedContent: string;
    tokenCount: number;
    sourceSuggestions: string[];
}
/**
 * 渐进式披露管理器接口
 */
export interface IProgressiveDisclosure {
    search(input: string): Promise<DisclosureResponse>;
    getSuggestionDetails(id: string): Promise<DisclosureSuggestion | null>;
    injectContext(suggestionIds: string[], options?: Partial<ContextInjectionOptions>): Promise<InjectionResult>;
    clearCache(): void;
    configure(config: Partial<DisclosureConfig>): void;
}
/**
 * 默认配置
 */
export declare const DEFAULT_DISCLOSURE_CONFIG: DisclosureConfig;
//# sourceMappingURL=types.d.ts.map