/**
 * 简单 Embedding 提供者
 * 基于 TF-IDF 的本地向量化（无需外部服务）
 */
import { IEmbeddingProvider } from './types.js';
export declare class SimpleEmbeddingProvider implements IEmbeddingProvider {
    private dimension;
    private vocabulary;
    private idf;
    private documentCount;
    constructor(dimension?: number);
    embed(text: string): Promise<number[]>;
    embedBatch(texts: string[]): Promise<number[][]>;
    getDimension(): number;
    train(documents: string[]): void;
    private tokenize;
    private calculateTF;
    private vectorize;
    private hashToken;
    private getIndices;
    private normalize;
}
//# sourceMappingURL=SimpleEmbeddingProvider.d.ts.map