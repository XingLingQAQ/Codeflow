import { describe, it, expect } from 'vitest';
import { HookManager } from '../HookManager.js';
import { HookEvent, RequestPayload } from '../types.js';

describe('HookManager before_send chaining', () => {
  it('chains multiple BEFORE_SEND handlers in registration order', async () => {
    const manager = new HookManager();
    let secondSawSystemMessage = false;

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
      return {
        ...payload,
        messages: [
          { role: 'system', content: 'first-hook', timestamp: Date.now() },
          ...payload.messages,
        ],
      };
    });

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
      secondSawSystemMessage =
        Array.isArray(payload.messages) &&
        payload.messages.some((m) => m.role === 'system' && m.content === 'first-hook');
      return {
        ...payload,
        model: 'mutated-by-second',
      };
    });

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
      return {
        ...payload,
        messages: [
          ...payload.messages,
          { role: 'system', content: 'third-hook', timestamp: Date.now() },
        ],
      };
    });

    const output = await manager.hook_before_send({
      messages: [{ role: 'user', content: 'hello', timestamp: Date.now() }],
    });

    expect(secondSawSystemMessage).toBe(true);
    expect(output.model).toBe('mutated-by-second');
    expect(output.messages.some((m) => m.content === 'first-hook')).toBe(true);
    expect(output.messages.some((m) => m.content === 'third-hook')).toBe(true);
  });
  it('keeps previous payload when a handler returns undefined', async () => {
    const manager = new HookManager();

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => ({
      ...payload,
      model: 'first',
    }));
    manager.register(HookEvent.BEFORE_SEND, async () => undefined);
    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => ({
      ...payload,
      model: `${payload.model}-third`,
    }));

    const output = await manager.hook_before_send({
      messages: [{ role: 'user', content: 'hello', timestamp: Date.now() }],
    });

    expect(output.model).toBe('first-third');
  });

  it('keeps richer tool turn message parts through chained payload mutations', async () => {
    const manager = new HookManager();

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => ({
      ...payload,
      messages: [
        {
          role: 'assistant',
          content: [
            { type: 'text', text: 'calling tool' },
            { type: 'tool_call', id: 'call-1', toolName: 'search', args: { query: 'schema' } },
          ],
          timestamp: 1,
        },
        ...payload.messages,
      ],
    }));

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => ({
      ...payload,
      messages: [
        ...payload.messages,
        {
          role: 'assistant',
          content: [
            { type: 'tool_result', toolCallId: 'call-1', toolName: 'search', result: { hits: 3 } },
          ],
          timestamp: 2,
        },
      ],
    }));

    const output = await manager.hook_before_send({
      messages: [{ role: 'user', content: 'hello', timestamp: Date.now() }],
      model: 'base-model',
    });

    expect(Array.isArray(output.messages[0].content)).toBe(true);
    expect((output.messages[0].content as any[])[1]).toMatchObject({ type: 'tool_call', toolName: 'search' });
    expect(output.messages.at(-1)).toMatchObject({
      role: 'assistant',
      content: [{ type: 'tool_result', toolName: 'search', result: { hits: 3 } }],
    });
  });



  it('blocks hooks when disabled', async () => {
    const manager = new HookManager(undefined, { enabled: false });
    let called = false;

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
      called = true;
      return {
        ...payload,
        model: 'blocked',
      };
    });

    const input = {
      messages: [{ role: 'user' as const, content: 'hello', timestamp: Date.now() }],
      model: 'base-model',
    };
    const output = await manager.hook_before_send(input);

    expect(called).toBe(false);
    expect(output).toEqual(input);
  });

  it('treats empty allowlist as deny-all when explicitly provided', async () => {
    const manager = new HookManager(undefined, { allowedHooks: [] });
    let called = false;

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
      called = true;
      return {
        ...payload,
        model: 'blocked',
      };
    });

    const input = {
      messages: [{ role: 'user' as const, content: 'hello', timestamp: Date.now() }],
      model: 'base-model',
    };
    const output = await manager.hook_before_send(input);

    expect(called).toBe(false);
    expect(output).toEqual(input);
  });

  it('allows only explicitly listed hooks', async () => {
    const manager = new HookManager(undefined, { allowedHooks: [HookEvent.POST_RESPONSE] });
    let beforeSendCalled = false;
    let postResponseCalled = false;

    manager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
      beforeSendCalled = true;
      return {
        ...payload,
        model: 'mutated',
      };
    });

    manager.register(HookEvent.POST_RESPONSE, async () => {
      postResponseCalled = true;
    });

    const output = await manager.hook_before_send({
      messages: [{ role: 'user', content: 'hello', timestamp: Date.now() }],
      model: 'base-model',
    });
    await manager.hook_post_response({
      content: 'done',
      model: 'base-model',
    });

    expect(beforeSendCalled).toBe(false);
    expect(postResponseCalled).toBe(true);
    expect(output.model).toBe('base-model');
  });
});

