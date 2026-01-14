/**
 * 语义检索器实现
 * 支持向量搜索、关键词搜索和混合搜索
 */

import { MemoryMatch } from '../hooks/types.js';
import { IVectorStore, VectorSearchResult, ChunkMetadata } from '../memory/types.js';
import {
  ISemanticRetriever,
  HybridSearchConfig,
  KeywordSearchResult,
  HybridSearchResult,
  SearchHistoricalContextParams,
  SearchHistoricalContextResult,
  DEFAULT_HYBRID_CONFIG,
} from './types.js';

export class SemanticRetriever implements ISemanticRetriever {
  private vectorStore: IVectorStore;
  private config: HybridSearchConfig;
  private keywordIndex: Map<string, Set<string>> = new Map();
  private contentCache: Map<string, { content: string; metadata: ChunkMetadata }> = new Map();

  constructor(vectorStore: IVectorStore, config?: Partial<HybridSearchConfig>) {
    this.vectorStore = vectorStore;
    this.config = { ...DEFAULT_HYBRID_CONFIG, ...config };
  }

  async searchHistoricalContext(
    params: SearchHistoricalContextParams
  ): Promise<SearchHistoricalContextResult> {
    const startTime = Date.now();
    const searchType = params.searchType || 'hybrid';
    const limit = params.limit || this.config.topK;

    let results: HybridSearchResult[];

    switch (searchType) {
      case 'vector':
        results = (await this.vectorSearch(params.query, { topK: limit })).map((r) => ({
          content: r.chunk.content,
          score: r.score,
          vectorScore: r.score,
          source: 'vector' as const,
          metadata: r.chunk.metadata,
        }));
        break;
      case 'keyword':
        results = (await this.keywordSearch(params.query, { topK: limit })).map((r) => ({
          content: r.content,
          score: r.score,
          keywordScore: r.score,
          source: 'keyword' as const,
          metadata: r.metadata,
          highlights: r.highlights,
        }));
        break;
      case 'hybrid':
      default:
        results = await this.hybridSearch(params.query, { topK: limit });
    }

    // 应用过滤器
    results = this.applyFilters(results, params);

    // 转换为 MemoryMatch 格式
    const matches: MemoryMatch[] = results.map((r) => ({
      content: r.content,
      similarity: r.score,
      source: r.source === 'vector' ? 'vector' : r.source === 'keyword' ? 'rules' : 'vector',
      metadata: r.metadata as unknown as Record<string, unknown>,
    }));

    return {
      matches,
      totalCount: matches.length,
      searchType,
      queryTime: Date.now() - startTime,
    };
  }

  async vectorSearch(
    query: string,
    options?: Partial<HybridSearchConfig>
  ): Promise<VectorSearchResult[]> {
    const topK = options?.topK ?? this.config.topK;
    const minScore = options?.minScore ?? this.config.minScore;

    return this.vectorStore.search(query, {
      topK,
      minScore,
    });
  }

  async keywordSearch(
    query: string,
    options?: Partial<HybridSearchConfig>
  ): Promise<KeywordSearchResult[]> {
    const topK = options?.topK ?? this.config.topK;
    const minScore = options?.minScore ?? this.config.minScore;

    const queryTokens = this.tokenize(query);
    const scores: Map<string, { score: number; highlights: string[] }> = new Map();

    // 计算 BM25 风格的分数
    for (const token of queryTokens) {
      const matchingIds = this.keywordIndex.get(token.toLowerCase());
      if (!matchingIds) continue;

      const idf = Math.log((this.contentCache.size + 1) / (matchingIds.size + 1)) + 1;

      for (const id of matchingIds) {
        const cached = this.contentCache.get(id);
        if (!cached) continue;

        const tf = this.calculateTF(cached.content, token);
        const score = tf * idf;

        const existing = scores.get(id) || { score: 0, highlights: [] };
        existing.score += score;
        existing.highlights.push(this.createHighlight(cached.content, token));
        scores.set(id, existing);
      }
    }

    // 归一化分数
    const maxScore = Math.max(...Array.from(scores.values()).map((s) => s.score), 1);

    const results: KeywordSearchResult[] = [];
    for (const [id, { score, highlights }] of scores) {
      const normalizedScore = score / maxScore;
      if (normalizedScore < minScore) continue;

      const cached = this.contentCache.get(id);
      if (!cached) continue;

      results.push({
        content: cached.content,
        score: normalizedScore,
        metadata: cached.metadata,
        highlights: highlights.filter((h) => h.length > 0),
      });
    }

    return results.sort((a, b) => b.score - a.score).slice(0, topK);
  }

  async hybridSearch(
    query: string,
    options?: Partial<HybridSearchConfig>
  ): Promise<HybridSearchResult[]> {
    const vectorWeight = options?.vectorWeight ?? this.config.vectorWeight;
    const keywordWeight = options?.keywordWeight ?? this.config.keywordWeight;
    const topK = options?.topK ?? this.config.topK;

    // 并行执行两种搜索
    const [vectorResults, keywordResults] = await Promise.all([
      this.vectorSearch(query, { topK: topK * 2 }),
      this.keywordSearch(query, { topK: topK * 2 }),
    ]);

    // 合并结果
    const merged: Map<string, HybridSearchResult> = new Map();

    for (const vr of vectorResults) {
      const id = vr.chunk.id;
      merged.set(id, {
        content: vr.chunk.content,
        score: vr.score * vectorWeight,
        vectorScore: vr.score,
        source: 'vector',
        metadata: vr.chunk.metadata,
      });
    }

    for (const kr of keywordResults) {
      const id = `${kr.metadata.sessionId}_${kr.metadata.messageIndex}_${kr.metadata.chunkIndex}`;
      const existing = merged.get(id);

      if (existing) {
        existing.score += kr.score * keywordWeight;
        existing.keywordScore = kr.score;
        existing.source = 'hybrid';
        existing.highlights = kr.highlights;
      } else {
        merged.set(id, {
          content: kr.content,
          score: kr.score * keywordWeight,
          keywordScore: kr.score,
          source: 'keyword',
          metadata: kr.metadata,
          highlights: kr.highlights,
        });
      }
    }

    // 排序并返回
    const results = Array.from(merged.values())
      .sort((a, b) => b.score - a.score)
      .slice(0, topK);

    // 可选：重排序
    if (options?.reranking ?? this.config.reranking) {
      return this.rerank(results, query);
    }

    return results;
  }

  // 索引内容（用于关键词搜索）
  indexContent(id: string, content: string, metadata: ChunkMetadata): void {
    this.contentCache.set(id, { content, metadata });

    const tokens = this.tokenize(content);
    for (const token of tokens) {
      const lowerToken = token.toLowerCase();
      if (!this.keywordIndex.has(lowerToken)) {
        this.keywordIndex.set(lowerToken, new Set());
      }
      this.keywordIndex.get(lowerToken)!.add(id);
    }
  }

  // 清除索引
  clearIndex(): void {
    this.keywordIndex.clear();
    this.contentCache.clear();
  }

  // ==================== 私有方法 ====================

  private tokenize(text: string): string[] {
    return text
      .toLowerCase()
      .replace(/[^\w\s\u4e00-\u9fff]/g, ' ')
      .split(/\s+/)
      .filter((t) => t.length > 1);
  }

  private calculateTF(content: string, term: string): number {
    const regex = new RegExp(term, 'gi');
    const matches = content.match(regex);
    return matches ? matches.length / content.split(/\s+/).length : 0;
  }

  private createHighlight(content: string, term: string): string {
    const regex = new RegExp(`(.{0,30})(${term})(.{0,30})`, 'gi');
    const match = regex.exec(content);
    if (!match) return '';
    return `...${match[1]}**${match[2]}**${match[3]}...`;
  }

  private applyFilters(
    results: HybridSearchResult[],
    params: SearchHistoricalContextParams
  ): HybridSearchResult[] {
    return results.filter((r) => {
      if (params.sessionId && r.metadata.sessionId !== params.sessionId) {
        return false;
      }
      if (params.agentRole && r.metadata.agentRole !== params.agentRole) {
        return false;
      }
      if (params.gitCommitHash && r.metadata.gitCommitHash !== params.gitCommitHash) {
        return false;
      }
      if (params.timeRange) {
        const timestamp = r.metadata.timestamp;
        if (timestamp < params.timeRange.start || timestamp > params.timeRange.end) {
          return false;
        }
      }
      return true;
    });
  }

  private rerank(results: HybridSearchResult[], query: string): HybridSearchResult[] {
    // 简单的重排序：考虑查询词覆盖率
    const queryTokens = new Set(this.tokenize(query));

    return results
      .map((r) => {
        const contentTokens = new Set(this.tokenize(r.content));
        const overlap = [...queryTokens].filter((t) => contentTokens.has(t)).length;
        const coverage = overlap / queryTokens.size;

        return {
          ...r,
          score: r.score * (1 + coverage * 0.2),
        };
      })
      .sort((a, b) => b.score - a.score);
  }
}
