/**
 * PassiveRAGService - MS-220 被动 RAG 自动检索
 *
 * 在发送请求前自动检索相关记忆，按时间局部性 + 语义相关性排序，
 * 格式化为上下文注入到对话中。
 */
import { AtomicMemoryService } from './AtomicMemoryService.js';
import { AtomicMemory } from './types.js';
/**
 * PassiveRAG 配置
 */
export interface PassiveRAGConfig {
    /** 返回的最大记忆条数 */
    topN: number;
    /** 最低相关性阈值 (0-1) */
    threshold: number;
    /** 时间局部性权重 (0-1) */
    timeLocalityWeight: number;
    /** 语义相关性权重 (0-1) */
    semanticWeight: number;
}
/**
 * 带评分的记忆
 */
export interface ScoredMemory {
    memory: AtomicMemory;
    score: number;
    timeScore: number;
    semanticScore: number;
}
export declare class PassiveRAGService {
    private readonly memoryService;
    private readonly config;
    constructor(memoryService: AtomicMemoryService, config?: Partial<PassiveRAGConfig>);
    /**
     * 检索与查询相关的记忆，按综合评分排序
     */
    retrieve(query: string, sessionId?: string): Promise<ScoredMemory[]>;
    /**
     * 将检索到的记忆格式化为可注入的上下文字符串
     */
    formatForInjection(memories: ScoredMemory[]): string;
    /**
     * 基于搜索结果排名计算语义评分
     * 排名越靠前，评分越高
     */
    private computeSemanticScore;
    /**
     * 基于时间衰减计算时间局部性评分
     * 越近的记忆评分越高
     */
    private computeTimeScore;
}
//# sourceMappingURL=PassiveRAG.d.ts.map