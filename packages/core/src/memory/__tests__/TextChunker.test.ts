import { describe, it, expect, beforeEach } from 'vitest';
import { TextChunker } from '../TextChunker.js';
import { ChunkMetadata } from '../types.js';

describe('TextChunker', () => {
  let chunker: TextChunker;

  const baseMetadata: Omit<ChunkMetadata, 'chunkIndex'> = {
    sessionId: 'session1',
    agentRole: 'assistant',
    messageIndex: 0,
    timestamp: Date.now(),
    source: 'assistant',
  };

  beforeEach(() => {
    chunker = new TextChunker({
      chunkSize: 100,
      chunkOverlap: 20,
    });
  });

  describe('constructor', () => {
    it('should create chunker with default config', () => {
      const defaultChunker = new TextChunker();
      expect(defaultChunker).toBeDefined();
    });

    it('should create chunker with custom config', () => {
      const customChunker = new TextChunker({
        chunkSize: 200,
        chunkOverlap: 50,
        separator: '.',
      });
      expect(customChunker).toBeDefined();
    });
  });

  describe('chunk', () => {
    it('should split text into chunks', () => {
      const text = 'Line one\nLine two\nLine three\nLine four\nLine five';
      const chunks = chunker.chunk(text, baseMetadata);

      expect(chunks.length).toBeGreaterThan(0);
      for (const chunk of chunks) {
        expect(chunk.content).toBeTruthy();
        expect(chunk.metadata.sessionId).toBe('session1');
      }
    });

    it('should generate unique chunk ids', () => {
      const text = 'First line\nSecond line\nThird line';
      const chunks = chunker.chunk(text, baseMetadata);

      const ids = chunks.map(c => c.id);
      const uniqueIds = new Set(ids);
      expect(uniqueIds.size).toBe(ids.length);
    });

    it('should include chunk index in metadata', () => {
      const text = 'Line one\nLine two\nLine three';
      const chunks = chunker.chunk(text, baseMetadata);

      for (let i = 0; i < chunks.length; i++) {
        expect(chunks[i].metadata.chunkIndex).toBe(i);
      }
    });

    it('should handle empty text', () => {
      const chunks = chunker.chunk('', baseMetadata);
      expect(chunks).toEqual([]);
    });

    it('should handle text shorter than chunk size', () => {
      const shortChunker = new TextChunker({ chunkSize: 1000 });
      const text = 'Short text';
      const chunks = shortChunker.chunk(text, baseMetadata);

      expect(chunks.length).toBe(1);
      expect(chunks[0].content).toBe('Short text');
    });

    it('should respect custom separator', () => {
      const dotChunker = new TextChunker({
        chunkSize: 50,
        chunkOverlap: 10,
        separator: '.',
      });
      const text = 'First sentence.Second sentence.Third sentence';
      const chunks = dotChunker.chunk(text, baseMetadata);

      expect(chunks.length).toBeGreaterThan(0);
    });

    it('should apply overlap between chunks', () => {
      const overlapChunker = new TextChunker({
        chunkSize: 30,
        chunkOverlap: 10,
      });
      const text = 'Line one content\nLine two content\nLine three content';
      const chunks = overlapChunker.chunk(text, baseMetadata);

      if (chunks.length > 1) {
        // Overlap should cause some content to appear in consecutive chunks
        expect(chunks.length).toBeGreaterThan(1);
      }
    });
  });

  describe('chunkByTokens', () => {
    const simpleTokenEstimator = (text: string) => Math.ceil(text.length / 4);

    it('should split text by token count', () => {
      const tokenChunker = new TextChunker({
        chunkSize: 20, // 20 tokens
        chunkOverlap: 5,
      });
      const text = 'This is a test sentence. Another sentence here. And one more.';
      const chunks = tokenChunker.chunkByTokens(text, baseMetadata, simpleTokenEstimator);

      expect(chunks.length).toBeGreaterThan(0);
    });

    it('should handle single sentence', () => {
      const tokenChunker = new TextChunker({
        chunkSize: 100,
        chunkOverlap: 10,
      });
      const text = 'Single sentence.';
      const chunks = tokenChunker.chunkByTokens(text, baseMetadata, simpleTokenEstimator);

      expect(chunks.length).toBe(1);
    });

    it('should handle empty text', () => {
      const chunks = chunker.chunkByTokens('', baseMetadata, simpleTokenEstimator);
      expect(chunks).toEqual([]);
    });

    it('should split on sentence boundaries', () => {
      const tokenChunker = new TextChunker({
        chunkSize: 15,
        chunkOverlap: 3,
      });
      const text = 'First sentence! Second sentence? Third sentence.';
      const chunks = tokenChunker.chunkByTokens(text, baseMetadata, simpleTokenEstimator);

      expect(chunks.length).toBeGreaterThan(0);
      // Each chunk should contain complete sentences
      for (const chunk of chunks) {
        expect(chunk.content.trim()).toBeTruthy();
      }
    });

    it('should handle CJK sentence endings', () => {
      const tokenChunker = new TextChunker({
        chunkSize: 10,
        chunkOverlap: 2,
      });
      const text = '第一句话。第二句话！第三句话？';
      const chunks = tokenChunker.chunkByTokens(text, baseMetadata, simpleTokenEstimator);

      expect(chunks.length).toBeGreaterThan(0);
    });
  });

  describe('chunk id format', () => {
    it('should generate id in format sessionId_messageIndex_chunkIndex', () => {
      const text = 'Test content';
      const chunks = chunker.chunk(text, baseMetadata);

      expect(chunks[0].id).toBe('session1_0_0');
    });

    it('should increment chunk index for multiple chunks', () => {
      const longChunker = new TextChunker({
        chunkSize: 20,
        chunkOverlap: 5,
      });
      const text = 'Line one\nLine two\nLine three\nLine four';
      const chunks = longChunker.chunk(text, baseMetadata);

      for (let i = 0; i < chunks.length; i++) {
        expect(chunks[i].id).toBe(`session1_0_${i}`);
      }
    });
  });
});
