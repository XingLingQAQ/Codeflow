import { ICliAdapter } from '../adapters/types.js';
import { AtomicMemorySource } from './types.js';
import { AtomicMemoryService } from './AtomicMemoryService.js';
interface ExtractorOptions {
    source?: AtomicMemorySource;
    maxMemoriesPerExtraction?: number;
    minImportance?: number;
    asyncDelayMs?: number;
}
export declare class MemoryExtractor {
    private readonly memoryService;
    private readonly llmAdapter;
    private readonly options;
    constructor(llmAdapter: Pick<ICliAdapter, 'send'>, memoryService: AtomicMemoryService, options?: ExtractorOptions);
    extractFromConversation(userMessage: string, assistantMessage: string, sessionId: string): void;
    private defer;
    private buildPrompt;
    private parseExtractionResponse;
    private normalizeCandidate;
    private normalizeTags;
    private normalizeSource;
    private extractJsonObject;
    private clamp01;
    private createMemoryId;
}
export {};
//# sourceMappingURL=MemoryExtractor.d.ts.map