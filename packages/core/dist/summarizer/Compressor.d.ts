/**
 * 压缩器实现
 * 80/20 压缩策略 + 决策骨架提取
 */
import { Message, Context, DecisionSkeleton } from '../hooks/types.js';
import { ICliAdapter } from '../adapters/types.js';
import { HookManager } from '../hooks/HookManager.js';
import { ICompressor, CompressionConfig, CompressionResult, SummaryAgentConfig } from './types.js';
export declare class Compressor implements ICompressor {
    private tokenCounter;
    private summaryAdapter?;
    private hookManager?;
    private config;
    constructor(summaryAdapter?: ICliAdapter, hookManager?: HookManager, config?: Partial<CompressionConfig>);
    compress(context: Context, config?: Partial<CompressionConfig>): Promise<CompressionResult>;
    extractSkeleton(messages: Message[]): Promise<DecisionSkeleton>;
    generateSummary(messages: Message[], config?: SummaryAgentConfig): Promise<string>;
    private applyCompressionStrategy;
    private calculateImportance;
    private generateLocalSummary;
    private buildSummaryPrompt;
}
//# sourceMappingURL=Compressor.d.ts.map