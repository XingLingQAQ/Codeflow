import { describe, it, expect } from 'vitest';
import { CodexAdapter } from '../CodexAdapter.js';
import { HookManager } from '../../hooks/HookManager.js';
import { HookEvent, RequestPayload, AIResponse } from '../../hooks/types.js';

const providerBase = process.env.REAL_PROVIDER_BASE_URL;
const providerKey = process.env.REAL_PROVIDER_API_KEY;
const providerModel = process.env.REAL_PROVIDER_MODEL || 'gpt-5.1-codex';

const runOrSkip = providerBase && providerKey ? describe : describe.skip;

// 该文件只验证 Codex API adapter 的 real provider hook 路径，不作为 cowork CLI adapter 完整性证据。
runOrSkip('CodexAdapter real provider hook e2e (API path only)', () => {
  it(
    'applies before_send mutation on real request and triggers post_response',
    async () => {
      const hookManager = new HookManager();
      let beforeSendCalled = false;
      let postResponseCalled = false;

      hookManager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
        beforeSendCalled = true;
        return {
          ...payload,
          model: providerModel,
          temperature: 0,
          maxTokens: 32,
          messages: [
            {
              role: 'system',
              content: '你必须只回复 HOOK_OK',
              timestamp: Date.now(),
            },
            ...payload.messages,
          ],
        };
      });

      hookManager.register(HookEvent.POST_RESPONSE, async (_response: AIResponse) => {
        postResponseCalled = true;
      });

      const adapter = new CodexAdapter(
        {
          apiKey: providerKey!,
          baseURL: providerBase!,
          // 故意给无效模型，确保必须依赖 hook_before_send 的模型改写才会成功
          model: 'invalid-model-for-hook-verification',
          temperature: 0.7,
          maxTokens: 256,
          timeout: 60000,
          maxRetries: 1,
        },
        hookManager
      );

      const response = await adapter.send('请按系统指令回复');

      expect(beforeSendCalled).toBe(true);
      expect(postResponseCalled).toBe(true);
      expect(response.content.length).toBeGreaterThan(0);
      expect(response.model).not.toBe('invalid-model-for-hook-verification');
      expect(response.model).toContain('gpt-5.1-codex');
      expect(response.model).toBeTruthy();
    },
    120000
  );

  it(
    'triggers before_send but not post_response when provider request fails after hook mutation',
    async () => {
      const hookManager = new HookManager();
      let beforeSendCalled = false;
      let postResponseCalled = false;

      hookManager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
        beforeSendCalled = true;
        return {
          ...payload,
          // 强制改写为非法模型，验证失败路径下 hook 行为
          model: 'invalid-model-for-real-provider-failure-case',
          temperature: 0,
          maxTokens: 16,
        };
      });

      hookManager.register(HookEvent.POST_RESPONSE, async (_response: AIResponse) => {
        postResponseCalled = true;
      });

      const adapter = new CodexAdapter(
        {
          apiKey: providerKey!,
          baseURL: providerBase!,
          model: providerModel,
          temperature: 0.7,
          maxTokens: 256,
          timeout: 60000,
          maxRetries: 1,
        },
        hookManager
      );

      await expect(adapter.send('请按系统指令回复')).rejects.toThrow();
      expect(beforeSendCalled).toBe(true);
      expect(postResponseCalled).toBe(false);
    },
    120000
  );

  it(
    'triggers streaming hooks on real provider stream response',
    async () => {
      const hookManager = new HookManager();
      let beforeSendCalled = false;
      let streamChunkCount = 0;
      let postResponseCalled = false;

      hookManager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
        beforeSendCalled = true;
        return {
          ...payload,
          model: providerModel,
          temperature: 0,
          maxTokens: 64,
        };
      });

      hookManager.register(HookEvent.ON_STREAM, (_chunk) => {
        streamChunkCount += 1;
      });

      hookManager.register(HookEvent.POST_RESPONSE, async (_response: AIResponse) => {
        postResponseCalled = true;
      });

      const adapter = new CodexAdapter(
        {
          apiKey: providerKey!,
          baseURL: providerBase!,
          model: providerModel,
          temperature: 0.7,
          maxTokens: 256,
          timeout: 60000,
          maxRetries: 1,
        },
        hookManager
      );

      let content = '';
      for await (const chunk of adapter.stream('请用一句话确认 stream hook 已触发')) {
        if (!chunk.done) {
          content += chunk.delta;
        }
      }

      expect(beforeSendCalled).toBe(true);
      expect(streamChunkCount).toBeGreaterThan(0);
      expect(postResponseCalled).toBe(true);
      expect(content.length).toBeGreaterThan(0);
      expect(adapter.getHistory().at(-1)?.content.length).toBeGreaterThan(0);
    },
    120000
  );
});
