import { describe, it, expect, beforeEach, vi } from 'vitest';
import { Compressor } from '../Compressor.js';
import { Message, Context } from '../../hooks/types.js';

describe('Compressor', () => {
  let compressor: Compressor;

  beforeEach(() => {
    compressor = new Compressor();
  });

  describe('constructor', () => {
    it('should create compressor with default config', () => {
      const c = new Compressor();
      expect(c).toBeDefined();
    });

    it('should create compressor with custom config', () => {
      const c = new Compressor(undefined, undefined, {
        targetRatio: 0.3,
        preserveRecentMessages: 5,
      });
      expect(c).toBeDefined();
    });
  });

  describe('compress', () => {
    it('should compress context and return result', async () => {
      const messages: Message[] = [
        { role: 'system', content: 'You are a helpful assistant.', timestamp: 1000 },
        { role: 'user', content: 'Hello, how are you?', timestamp: 2000 },
        { role: 'assistant', content: 'I am doing well, thank you!', timestamp: 3000 },
        { role: 'user', content: 'Can you help me with coding?', timestamp: 4000 },
        { role: 'assistant', content: 'Of course! What do you need help with?', timestamp: 5000 },
      ];

      const context: Context = {
        messages,
        tokenCount: 500,
      };

      const result = await compressor.compress(context);

      expect(result.originalTokens).toBe(500);
      expect(result.compressedTokens).toBeLessThanOrEqual(result.originalTokens);
      expect(result.compressionRatio).toBeLessThanOrEqual(1);
      expect(result.preservedMessages.length).toBeGreaterThan(0);
    });

    it('should preserve system messages', async () => {
      const messages: Message[] = [
        { role: 'system', content: 'Important system prompt', timestamp: 1000 },
        { role: 'user', content: 'User message 1', timestamp: 2000 },
        { role: 'assistant', content: 'Assistant response 1', timestamp: 3000 },
      ];

      const context: Context = { messages, tokenCount: 200 };
      const result = await compressor.compress(context, { preserveSystemPrompt: true });

      const systemMessages = result.preservedMessages.filter(m => m.role === 'system');
      expect(systemMessages.length).toBeGreaterThan(0);
    });

    it('should preserve recent messages', async () => {
      const messages: Message[] = Array.from({ length: 20 }, (_, i) => ({
        role: i % 2 === 0 ? 'user' : 'assistant',
        content: `Message ${i}`,
        timestamp: i * 1000,
      })) as Message[];

      const context: Context = { messages, tokenCount: 1000 };
      const result = await compressor.compress(context, { preserveRecentMessages: 4 });

      // Should include at least the recent messages
      expect(result.preservedMessages.length).toBeGreaterThanOrEqual(4);
    });
  });

  describe('extractSkeleton', () => {
    it('should extract entities from messages', async () => {
      const messages: Message[] = [
        { role: 'user', content: 'I want to implement a UserService class', timestamp: 1000 },
        { role: 'assistant', content: 'The UserService should extend BaseService', timestamp: 2000 },
      ];

      const skeleton = await compressor.extractSkeleton(messages);

      expect(skeleton.entities).toBeDefined();
      expect(skeleton.entities.length).toBeGreaterThan(0);
      expect(skeleton.entities).toContain('UserService');
    });

    it('should extract decisions from messages', async () => {
      const messages: Message[] = [
        { role: 'user', content: 'Should we use React or Vue?', timestamp: 1000 },
        { role: 'assistant', content: 'I recommend using React for this project because of its ecosystem.', timestamp: 2000 },
      ];

      const skeleton = await compressor.extractSkeleton(messages);

      expect(skeleton.decisions).toBeDefined();
      expect(skeleton.decisions.length).toBeGreaterThan(0);
    });

    it('should extract relations from messages', async () => {
      const messages: Message[] = [
        { role: 'assistant', content: 'UserController extends BaseController and uses UserService', timestamp: 1000 },
      ];

      const skeleton = await compressor.extractSkeleton(messages);

      expect(skeleton.relations).toBeDefined();
      expect(skeleton.relations.length).toBeGreaterThan(0);
    });

    it('should handle empty messages', async () => {
      const skeleton = await compressor.extractSkeleton([]);

      expect(skeleton.entities).toEqual([]);
      expect(skeleton.decisions).toEqual([]);
      expect(skeleton.relations).toEqual([]);
    });

    it('should extract camelCase identifiers', async () => {
      const messages: Message[] = [
        { role: 'assistant', content: 'The getUserById function returns a userProfile object', timestamp: 1000 },
      ];

      const skeleton = await compressor.extractSkeleton(messages);

      expect(skeleton.entities.some(e => e === 'getUserById' || e === 'userProfile')).toBe(true);
    });

    it('should extract snake_case identifiers', async () => {
      const messages: Message[] = [
        { role: 'assistant', content: 'Use the get_user_by_id function with user_profile', timestamp: 1000 },
      ];

      const skeleton = await compressor.extractSkeleton(messages);

      expect(skeleton.entities.some(e => e === 'get_user_by_id' || e === 'user_profile')).toBe(true);
    });

    it('should extract file paths', async () => {
      const messages: Message[] = [
        { role: 'assistant', content: 'Check the file at src/services/UserService.ts', timestamp: 1000 },
      ];

      const skeleton = await compressor.extractSkeleton(messages);

      expect(skeleton.entities.some(e => e.includes('/'))).toBe(true);
    });

    it('should limit entities to 30', async () => {
      const messages: Message[] = [
        { role: 'assistant', content: Array.from({ length: 50 }, (_, i) => `Entity${i}`).join(' '), timestamp: 1000 },
      ];

      const skeleton = await compressor.extractSkeleton(messages);

      expect(skeleton.entities.length).toBeLessThanOrEqual(30);
    });

    it('should limit decisions to 15', async () => {
      const messages: Message[] = [
        { role: 'assistant', content: Array.from({ length: 20 }, (_, i) => `We decided to use option ${i}.`).join(' '), timestamp: 1000 },
      ];

      const skeleton = await compressor.extractSkeleton(messages);

      expect(skeleton.decisions.length).toBeLessThanOrEqual(15);
    });
  });

  describe('generateSummary', () => {
    it('should generate local summary without adapter', async () => {
      const messages: Message[] = [
        { role: 'user', content: 'How do I implement authentication?', timestamp: 1000 },
        { role: 'assistant', content: 'I implemented JWT authentication for the API.', timestamp: 2000 },
      ];

      const summary = await compressor.generateSummary(messages);

      expect(summary).toBeDefined();
      expect(summary.length).toBeGreaterThan(0);
      expect(summary).toContain('Conversation summary');
    });

    it('should include message statistics', async () => {
      const messages: Message[] = [
        { role: 'user', content: 'Question 1', timestamp: 1000 },
        { role: 'assistant', content: 'Answer 1', timestamp: 2000 },
        { role: 'user', content: 'Question 2', timestamp: 3000 },
        { role: 'assistant', content: 'Answer 2', timestamp: 4000 },
      ];

      const summary = await compressor.generateSummary(messages);

      expect(summary).toContain('2 user messages');
      expect(summary).toContain('2 assistant responses');
    });
  });
});
