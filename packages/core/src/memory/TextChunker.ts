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

export class TextChunker {
  private config: ChunkerConfig;

  constructor(config?: Partial<ChunkerConfig>) {
    this.config = {
      chunkSize: 500,
      chunkOverlap: 50,
      separator: '\n',
      ...config,
    };
  }

  chunk(text: string, baseMetadata: Omit<ChunkMetadata, 'chunkIndex'>): DocumentChunk[] {
    const chunks: DocumentChunk[] = [];
    const { chunkSize, chunkOverlap } = this.config;
    const separator = this.config.separator || '\n';

    // 按分隔符分割
    const segments = text.split(separator);
    let currentChunk = '';
    let chunkIndex = 0;

    for (const segment of segments) {
      const potentialChunk = currentChunk ? `${currentChunk}${separator}${segment}` : segment;

      if (potentialChunk.length > chunkSize && currentChunk) {
        // 保存当前块
        chunks.push(this.createChunk(currentChunk, chunkIndex, baseMetadata));
        chunkIndex++;

        // 计算重叠部分
        if (chunkOverlap > 0) {
          const overlapStart = Math.max(0, currentChunk.length - chunkOverlap);
          currentChunk = currentChunk.slice(overlapStart) + separator + segment;
        } else {
          currentChunk = segment;
        }
      } else {
        currentChunk = potentialChunk;
      }
    }

    // 保存最后一个块
    if (currentChunk.trim()) {
      chunks.push(this.createChunk(currentChunk, chunkIndex, baseMetadata));
    }

    return chunks;
  }

  chunkByTokens(
    text: string,
    baseMetadata: Omit<ChunkMetadata, 'chunkIndex'>,
    estimateTokens: (text: string) => number
  ): DocumentChunk[] {
    const chunks: DocumentChunk[] = [];
    const { chunkSize, chunkOverlap } = this.config;

    const sentences = this.splitIntoSentences(text);
    let currentChunk = '';
    let currentTokens = 0;
    let chunkIndex = 0;

    for (const sentence of sentences) {
      const sentenceTokens = estimateTokens(sentence);

      if (currentTokens + sentenceTokens > chunkSize && currentChunk) {
        chunks.push(this.createChunk(currentChunk, chunkIndex, baseMetadata));
        chunkIndex++;

        // 重叠处理
        if (chunkOverlap > 0) {
          const words = currentChunk.split(' ');
          const overlapWords = words.slice(-Math.ceil(chunkOverlap / 4));
          currentChunk = overlapWords.join(' ') + ' ' + sentence;
          currentTokens = estimateTokens(currentChunk);
        } else {
          currentChunk = sentence;
          currentTokens = sentenceTokens;
        }
      } else {
        currentChunk = currentChunk ? `${currentChunk} ${sentence}` : sentence;
        currentTokens += sentenceTokens;
      }
    }

    if (currentChunk.trim()) {
      chunks.push(this.createChunk(currentChunk, chunkIndex, baseMetadata));
    }

    return chunks;
  }

  private createChunk(
    content: string,
    chunkIndex: number,
    baseMetadata: Omit<ChunkMetadata, 'chunkIndex'>
  ): DocumentChunk {
    return {
      id: `${baseMetadata.sessionId}_${baseMetadata.messageIndex}_${chunkIndex}`,
      content: content.trim(),
      metadata: {
        ...baseMetadata,
        chunkIndex,
      },
    };
  }

  private splitIntoSentences(text: string): string[] {
    return text.split(/(?<=[.!?。！？])\s+/).filter((s) => s.trim().length > 0);
  }
}
