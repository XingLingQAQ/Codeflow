import { describe, it, expect, beforeEach } from 'vitest';
import { TokenCounter } from '../TokenCounter.js';
import { Message } from '../../hooks/types.js';
import { TOKEN_ESTIMATION } from '../types.js';

describe('TokenCounter', () => {
  let counter: TokenCounter;

  beforeEach(() => {
    counter = new TokenCounter();
  });

  describe('constructor', () => {
    it('should create counter with default config', () => {
      const c = new TokenCounter();
      expect(c).toBeDefined();
    });

    it('should create counter with custom config', () => {
      const c = new TokenCounter({
        charsPerTokenEn: 3,
        charsPerTokenZh: 2,
        overheadPerMessage: 5,
      });
      expect(c).toBeDefined();
    });

    it('should use default values from TOKEN_ESTIMATION', () => {
      const c = new TokenCounter();
      // Test by counting known text
      const enText = 'a'.repeat(TOKEN_ESTIMATION.CHARS_PER_TOKEN_EN);
      expect(c.count(enText)).toBe(1);
    });
  });

  describe('count', () => {
    it('should count tokens for English text', () => {
      const text = 'Hello world';
      const tokens = counter.count(text);

      expect(tokens).toBeGreaterThan(0);
      // 11 chars / 4 chars per token = ~3 tokens
      expect(tokens).toBe(Math.ceil(11 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_EN));
    });

    it('should count tokens for Chinese text', () => {
      const text = '你好世界';
      const tokens = counter.count(text);

      expect(tokens).toBeGreaterThan(0);
      // 4 chars / 1.5 chars per token = ~3 tokens
      expect(tokens).toBe(Math.ceil(4 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_ZH));
    });

    it('should count tokens for mixed text', () => {
      const text = 'Hello 你好';
      const tokens = counter.count(text);

      // 6 English chars + 2 Chinese chars
      const expectedEn = Math.ceil(6 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_EN);
      const expectedZh = Math.ceil(2 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_ZH);

      expect(tokens).toBe(expectedEn + expectedZh);
    });

    it('should return 0 for empty string', () => {
      expect(counter.count('')).toBe(0);
    });

    it('should handle whitespace', () => {
      const text = '   ';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should handle special characters', () => {
      const text = '!@#$%^&*()';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should handle numbers', () => {
      const text = '12345';
      const tokens = counter.count(text);
      expect(tokens).toBe(Math.ceil(5 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_EN));
    });

    it('should handle code snippets', () => {
      const text = 'function hello() { return "world"; }';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should handle newlines', () => {
      const text = 'line1\nline2\nline3';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });
  });

  describe('countMessages', () => {
    it('should count tokens for array of messages', () => {
      const messages: Message[] = [
        { role: 'user', content: 'Hello', timestamp: Date.now() },
        { role: 'assistant', content: 'Hi there', timestamp: Date.now() },
      ];

      const result = counter.countMessages(messages);

      expect(result.total).toBeGreaterThan(0);
      expect(result.byMessage).toHaveLength(2);
      expect(result.byRole.user).toBeGreaterThan(0);
      expect(result.byRole.assistant).toBeGreaterThan(0);
    });

    it('should include overhead per message', () => {
      const messages: Message[] = [
        { role: 'user', content: 'Hi', timestamp: Date.now() },
      ];

      const result = counter.countMessages(messages);
      const contentTokens = counter.count('Hi');

      expect(result.total).toBe(contentTokens + TOKEN_ESTIMATION.OVERHEAD_PER_MESSAGE);
    });

    it('should track tokens by role', () => {
      const messages: Message[] = [
        { role: 'user', content: 'User message 1', timestamp: Date.now() },
        { role: 'user', content: 'User message 2', timestamp: Date.now() },
        { role: 'assistant', content: 'Assistant response', timestamp: Date.now() },
        { role: 'system', content: 'System prompt', timestamp: Date.now() },
      ];

      const result = counter.countMessages(messages);

      expect(result.byRole.user).toBeGreaterThan(result.byRole.assistant);
      expect(result.byRole.system).toBeGreaterThan(0);
    });

    it('should track tokens by message index', () => {
      const messages: Message[] = [
        { role: 'user', content: 'Short', timestamp: Date.now() },
        { role: 'assistant', content: 'This is a much longer message', timestamp: Date.now() },
      ];

      const result = counter.countMessages(messages);

      expect(result.byMessage[1]).toBeGreaterThan(result.byMessage[0]);
    });

    it('should handle empty messages array', () => {
      const result = counter.countMessages([]);

      expect(result.total).toBe(0);
      expect(result.byMessage).toEqual([]);
      expect(result.byRole.user).toBe(0);
      expect(result.byRole.assistant).toBe(0);
      expect(result.byRole.system).toBe(0);
    });

    it('should handle messages with empty content', () => {
      const messages: Message[] = [
        { role: 'user', content: '', timestamp: Date.now() },
      ];

      const result = counter.countMessages(messages);

      // Should only have overhead
      expect(result.total).toBe(TOKEN_ESTIMATION.OVERHEAD_PER_MESSAGE);
    });
  });

  describe('estimateTokens', () => {
    it('should be equivalent to count()', () => {
      const text = 'Hello world 你好世界';

      expect(counter.estimateTokens(text)).toBe(counter.count(text));
    });

    it('should return 0 for null-like values', () => {
      expect(counter.estimateTokens('')).toBe(0);
    });
  });

  describe('Chinese character detection', () => {
    it('should detect CJK Unified Ideographs', () => {
      const text = '中文测试';
      const tokens = counter.count(text);
      // 4 Chinese chars / 1.5 = ~3 tokens
      expect(tokens).toBe(Math.ceil(4 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_ZH));
    });

    it('should detect CJK Extension A', () => {
      // Characters from CJK Extension A (U+3400-U+4DBF)
      const text = '\u3400\u3401';
      const tokens = counter.count(text);
      expect(tokens).toBe(Math.ceil(2 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_ZH));
    });

    it('should detect CJK Compatibility Ideographs', () => {
      // Characters from CJK Compatibility (U+F900-U+FAFF)
      const text = '\uF900\uF901';
      const tokens = counter.count(text);
      expect(tokens).toBe(Math.ceil(2 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_ZH));
    });

    it('should not count Japanese Hiragana as Chinese', () => {
      // Hiragana is not in the Chinese ranges
      const text = 'あいう';
      const tokens = counter.count(text);
      // Should be counted as non-Chinese
      expect(tokens).toBe(Math.ceil(3 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_EN));
    });

    it('should not count Korean as Chinese', () => {
      const text = '한글';
      const tokens = counter.count(text);
      // Should be counted as non-Chinese
      expect(tokens).toBe(Math.ceil(2 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_EN));
    });
  });

  describe('custom configuration', () => {
    it('should use custom charsPerTokenEn', () => {
      const customCounter = new TokenCounter({ charsPerTokenEn: 2 });
      const text = 'Hello'; // 5 chars

      expect(customCounter.count(text)).toBe(Math.ceil(5 / 2));
    });

    it('should use custom charsPerTokenZh', () => {
      const customCounter = new TokenCounter({ charsPerTokenZh: 1 });
      const text = '你好'; // 2 chars

      expect(customCounter.count(text)).toBe(2);
    });

    it('should use custom overheadPerMessage', () => {
      const customCounter = new TokenCounter({ overheadPerMessage: 10 });
      const messages: Message[] = [
        { role: 'user', content: '', timestamp: Date.now() },
      ];

      const result = customCounter.countMessages(messages);
      expect(result.total).toBe(10);
    });
  });

  describe('edge cases', () => {
    it('should handle very long text', () => {
      const text = 'a'.repeat(10000);
      const tokens = counter.count(text);

      expect(tokens).toBe(Math.ceil(10000 / TOKEN_ESTIMATION.CHARS_PER_TOKEN_EN));
    });

    it('should handle emoji', () => {
      const text = '😀😁😂';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should handle mixed emoji and text', () => {
      const text = 'Hello 😀 世界';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should handle markdown', () => {
      const text = '# Heading\n\n**Bold** and *italic*\n\n- List item';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should handle JSON', () => {
      const text = '{"key": "value", "number": 123}';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });

    it('should handle URLs', () => {
      const text = 'https://example.com/path?query=value';
      const tokens = counter.count(text);
      expect(tokens).toBeGreaterThan(0);
    });
  });
});
