/**
 * 渐进式记忆披露实现
 * 实现 hook_on_user_input_submitted 语义预检
 */
import { DEFAULT_DISCLOSURE_CONFIG, } from './types.js';
import { HookEvent } from '../hooks/types.js';
export class ProgressiveDisclosure {
    constructor(config = {}) {
        this.cache = new Map();
        this.suggestionStore = new Map();
        this.config = { ...DEFAULT_DISCLOSURE_CONFIG, ...config };
    }
    setHookManager(hookManager) {
        this.hookManager = hookManager;
        this.registerHook();
    }
    setDualTrackMemory(memory) {
        this.dualTrackMemory = memory;
    }
    configure(config) {
        this.config = { ...this.config, ...config };
    }
    registerHook() {
        if (!this.hookManager)
            return;
        this.hookManager.register(HookEvent.USER_INPUT_SUBMITTED, async (input) => {
            const response = await this.search(input);
            return response.suggestions.map(s => ({
                content: s.fullContent,
                similarity: s.relevanceScore,
                source: s.source,
                metadata: s.metadata,
            }));
        });
    }
    async search(input) {
        const startTime = Date.now();
        // 检查缓存
        if (this.config.enableCache) {
            const cached = this.getFromCache(input);
            if (cached) {
                return {
                    ...cached,
                    queryTime: Date.now() - startTime,
                };
            }
        }
        // 创建超时 Promise
        const timeoutPromise = new Promise((resolve) => {
            setTimeout(() => {
                resolve({
                    suggestions: [],
                    queryTime: this.config.timeoutMs,
                    totalMatches: 0,
                    hasMore: false,
                });
            }, this.config.timeoutMs);
        });
        // 执行搜索
        const searchPromise = this.performSearch(input, startTime);
        // 竞争：搜索 vs 超时
        const response = await Promise.race([searchPromise, timeoutPromise]);
        // 缓存结果
        if (this.config.enableCache && response.suggestions.length > 0) {
            this.addToCache(input, response);
        }
        return response;
    }
    async performSearch(input, startTime) {
        const suggestions = [];
        if (this.dualTrackMemory) {
            try {
                const searchResult = await this.dualTrackMemory.hybridSearch({
                    query: input,
                    limit: this.config.maxSuggestions * 2,
                    searchMode: 'hybrid',
                });
                for (const result of searchResult.results) {
                    if (result.score < this.config.minRelevanceScore)
                        continue;
                    const suggestion = this.createSuggestion(result);
                    suggestions.push(suggestion);
                    this.suggestionStore.set(suggestion.id, suggestion);
                    if (suggestions.length >= this.config.maxSuggestions)
                        break;
                }
            }
            catch {
                // 搜索失败时返回空结果
            }
        }
        return {
            suggestions,
            queryTime: Date.now() - startTime,
            totalMatches: suggestions.length,
            hasMore: suggestions.length >= this.config.maxSuggestions,
        };
    }
    createSuggestion(result) {
        const id = `suggestion_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
        const content = result.content;
        const preview = content.length > this.config.previewMaxLength
            ? content.slice(0, this.config.previewMaxLength) + '...'
            : content;
        let title = 'Memory Match';
        if (result.entity?.label) {
            title = result.entity.label;
        }
        else if (result.metadata?.sessionId) {
            title = `Session: ${result.metadata.sessionId}`;
        }
        return {
            id,
            type: result.source === 'graph' ? 'context' : 'memory',
            title,
            preview,
            fullContent: content,
            relevanceScore: result.score,
            source: (result.source === 'hybrid' || result.source === 'keyword') ? 'vector' : result.source,
            metadata: result.metadata,
            timestamp: Date.now(),
        };
    }
    async getSuggestionDetails(id) {
        return this.suggestionStore.get(id) || null;
    }
    async injectContext(suggestionIds, options = {}) {
        const opts = {
            position: 'prepend',
            format: 'markdown',
            deduplicate: true,
            ...options,
        };
        const suggestions = suggestionIds
            .map(id => this.suggestionStore.get(id))
            .filter((s) => s !== undefined);
        if (suggestions.length === 0) {
            return {
                injectedContent: '',
                tokenCount: 0,
                sourceSuggestions: [],
            };
        }
        // 去重
        const uniqueContents = opts.deduplicate
            ? [...new Set(suggestions.map(s => s.fullContent))]
            : suggestions.map(s => s.fullContent);
        // 格式化
        let injectedContent;
        switch (opts.format) {
            case 'xml':
                injectedContent = this.formatAsXml(uniqueContents);
                break;
            case 'markdown':
                injectedContent = this.formatAsMarkdown(uniqueContents);
                break;
            default:
                injectedContent = uniqueContents.join('\n\n');
        }
        // 截断（如果指定了 maxTokens）
        if (opts.maxTokens) {
            const estimatedTokens = Math.ceil(injectedContent.length / 4);
            if (estimatedTokens > opts.maxTokens) {
                const maxChars = opts.maxTokens * 4;
                injectedContent = injectedContent.slice(0, maxChars) + '\n[truncated]';
            }
        }
        return {
            injectedContent,
            tokenCount: Math.ceil(injectedContent.length / 4),
            sourceSuggestions: suggestionIds,
        };
    }
    formatAsXml(contents) {
        const items = contents.map((c, i) => `<context_item index="${i + 1}">\n${c}\n</context_item>`);
        return `<injected_context>\n${items.join('\n')}\n</injected_context>`;
    }
    formatAsMarkdown(contents) {
        const items = contents.map((c, i) => `### Context ${i + 1}\n\n${c}`);
        return `## Relevant Context\n\n${items.join('\n\n---\n\n')}`;
    }
    clearCache() {
        this.cache.clear();
    }
    getFromCache(input) {
        const key = this.getCacheKey(input);
        const entry = this.cache.get(key);
        if (!entry)
            return null;
        if (Date.now() - entry.timestamp > this.config.cacheTtlMs) {
            this.cache.delete(key);
            return null;
        }
        return entry.response;
    }
    addToCache(input, response) {
        const key = this.getCacheKey(input);
        // 清理过期缓存
        if (this.cache.size >= this.config.cacheMaxSize) {
            this.evictOldestCache();
        }
        this.cache.set(key, {
            response,
            timestamp: Date.now(),
        });
    }
    getCacheKey(input) {
        return input.toLowerCase().trim();
    }
    evictOldestCache() {
        let oldestKey = null;
        let oldestTime = Infinity;
        for (const [key, entry] of this.cache) {
            if (entry.timestamp < oldestTime) {
                oldestTime = entry.timestamp;
                oldestKey = key;
            }
        }
        if (oldestKey) {
            this.cache.delete(oldestKey);
        }
    }
}
//# sourceMappingURL=ProgressiveDisclosure.js.map