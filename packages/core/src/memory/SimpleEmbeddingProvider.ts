/**
 * 简单 Embedding 提供者
 * 基于 TF-IDF 的本地向量化（无需外部服务）
 */

import { IEmbeddingProvider } from './types.js';

export class SimpleEmbeddingProvider implements IEmbeddingProvider {
  private dimension: number;
  private vocabulary: Map<string, number> = new Map();
  private idf: Map<string, number> = new Map();
  private documentCount = 0;

  constructor(dimension = 384) {
    this.dimension = dimension;
  }

  async embed(text: string): Promise<number[]> {
    const tokens = this.tokenize(text);
    const tf = this.calculateTF(tokens);
    return this.vectorize(tf);
  }

  async embedBatch(texts: string[]): Promise<number[][]> {
    return Promise.all(texts.map((text) => this.embed(text)));
  }

  getDimension(): number {
    return this.dimension;
  }

  // 训练词汇表（可选，用于提高质量）
  train(documents: string[]): void {
    this.documentCount = documents.length;
    const docFreq: Map<string, number> = new Map();

    for (const doc of documents) {
      const tokens = new Set(this.tokenize(doc));
      for (const token of tokens) {
        docFreq.set(token, (docFreq.get(token) || 0) + 1);
        if (!this.vocabulary.has(token)) {
          this.vocabulary.set(token, this.vocabulary.size);
        }
      }
    }

    // 计算 IDF
    for (const [token, freq] of docFreq) {
      this.idf.set(token, Math.log(this.documentCount / (freq + 1)) + 1);
    }
  }

  private tokenize(text: string): string[] {
    return text
      .toLowerCase()
      .replace(/[^\w\s\u4e00-\u9fff]/g, ' ')
      .split(/\s+/)
      .filter((t) => t.length > 1);
  }

  private calculateTF(tokens: string[]): Map<string, number> {
    const tf: Map<string, number> = new Map();
    for (const token of tokens) {
      tf.set(token, (tf.get(token) || 0) + 1);
    }
    // 归一化
    const maxFreq = Math.max(...tf.values());
    for (const [token, freq] of tf) {
      tf.set(token, freq / maxFreq);
    }
    return tf;
  }

  private vectorize(tf: Map<string, number>): number[] {
    const vector = new Array(this.dimension).fill(0);

    for (const [token, tfValue] of tf) {
      const idfValue = this.idf.get(token) || 1;
      const tfidf = tfValue * idfValue;

      // 使用哈希将 token 映射到向量维度
      const hash = this.hashToken(token);
      const indices = this.getIndices(hash, 3);

      for (const idx of indices) {
        vector[idx] += tfidf / indices.length;
      }
    }

    // L2 归一化
    return this.normalize(vector);
  }

  private hashToken(token: string): number {
    let hash = 0;
    for (let i = 0; i < token.length; i++) {
      const char = token.charCodeAt(i);
      hash = (hash << 5) - hash + char;
      hash = hash & hash;
    }
    return Math.abs(hash);
  }

  private getIndices(hash: number, count: number): number[] {
    const indices: number[] = [];
    for (let i = 0; i < count; i++) {
      indices.push((hash + i * 7919) % this.dimension);
    }
    return indices;
  }

  private normalize(vector: number[]): number[] {
    const magnitude = Math.sqrt(vector.reduce((sum, v) => sum + v * v, 0));
    if (magnitude === 0) return vector;
    return vector.map((v) => v / magnitude);
  }
}
