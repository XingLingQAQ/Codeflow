/**
 * 自动总结机制类型定义
 * Token 计数器 + 80/20 压缩策略
 */
import { Message, Context, DecisionSkeleton } from '../hooks/types.js';
/**
 * Token 计数结果
 */
export interface TokenCount {
    total: number;
    byRole: {
        user: number;
        assistant: number;
        system: number;
    };
    byMessage: number[];
}
/**
 * 压缩配置
 */
export interface CompressionConfig {
    maxTokens: number;
    targetRatio: number;
    preserveSystemPrompt: boolean;
    preserveRecentMessages: number;
    extractDecisionSkeleton: boolean;
}
/**
 * 压缩结果
 */
export interface CompressionResult {
    originalTokens: number;
    compressedTokens: number;
    compressionRatio: number;
    preservedMessages: Message[];
    summary?: string;
    decisionSkeleton?: DecisionSkeleton;
}
/**
 * Summary Agent 配置
 */
export interface SummaryAgentConfig {
    model?: string;
    maxSummaryTokens?: number;
    includeEntities?: boolean;
    includeDecisions?: boolean;
    includeRelations?: boolean;
}
/**
 * Token 计数器接口
 */
export interface ITokenCounter {
    count(text: string): number;
    countMessages(messages: Message[]): TokenCount;
    estimateTokens(text: string): number;
}
/**
 * 压缩器接口
 */
export interface ICompressor {
    compress(context: Context, config?: Partial<CompressionConfig>): Promise<CompressionResult>;
    extractSkeleton(messages: Message[]): Promise<DecisionSkeleton>;
    generateSummary(messages: Message[], config?: SummaryAgentConfig): Promise<string>;
}
/**
 * 默认压缩配置
 */
export declare const DEFAULT_COMPRESSION_CONFIG: CompressionConfig;
/**
 * Token 估算常量
 */
export declare const TOKEN_ESTIMATION: {
    CHARS_PER_TOKEN_EN: number;
    CHARS_PER_TOKEN_ZH: number;
    OVERHEAD_PER_MESSAGE: number;
};
//# sourceMappingURL=types.d.ts.map