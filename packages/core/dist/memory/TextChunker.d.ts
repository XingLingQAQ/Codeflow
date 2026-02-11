/**
 * 文本分块器
 * 将长文本分割为适合向量化的块
 */
import { DocumentChunk, ChunkMetadata } from './types.js';
export interface ChunkerConfig {
    chunkSize: number;
    chunkOverlap: number;
    separator?: string;
}
export declare class TextChunker {
    private config;
    constructor(config?: Partial<ChunkerConfig>);
    chunk(text: string, baseMetadata: Omit<ChunkMetadata, 'chunkIndex'>): DocumentChunk[];
    chunkByTokens(text: string, baseMetadata: Omit<ChunkMetadata, 'chunkIndex'>, estimateTokens: (text: string) => number): DocumentChunk[];
    private createChunk;
    private splitIntoSentences;
}
//# sourceMappingURL=TextChunker.d.ts.map