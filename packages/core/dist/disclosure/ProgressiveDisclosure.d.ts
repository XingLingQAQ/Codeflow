/**
 * 渐进式记忆披露实现
 * 实现 hook_on_user_input_submitted 语义预检
 */
import { IProgressiveDisclosure, DisclosureConfig, DisclosureResponse, DisclosureSuggestion, ContextInjectionOptions, InjectionResult } from './types.js';
import { HookManager } from '../hooks/HookManager.js';
import { IDualTrackMemory } from '../retriever/DualTrackTypes.js';
export declare class ProgressiveDisclosure implements IProgressiveDisclosure {
    private config;
    private cache;
    private suggestionStore;
    private hookManager?;
    private dualTrackMemory?;
    constructor(config?: Partial<DisclosureConfig>);
    setHookManager(hookManager: HookManager): void;
    setDualTrackMemory(memory: IDualTrackMemory): void;
    configure(config: Partial<DisclosureConfig>): void;
    private registerHook;
    search(input: string): Promise<DisclosureResponse>;
    private performSearch;
    private createSuggestion;
    getSuggestionDetails(id: string): Promise<DisclosureSuggestion | null>;
    injectContext(suggestionIds: string[], options?: Partial<ContextInjectionOptions>): Promise<InjectionResult>;
    private formatAsXml;
    private formatAsMarkdown;
    clearCache(): void;
    private getFromCache;
    private addToCache;
    private getCacheKey;
    private evictOldestCache;
}
//# sourceMappingURL=ProgressiveDisclosure.d.ts.map