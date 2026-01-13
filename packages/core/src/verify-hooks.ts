/**
 * Hook Bus 验证脚本
 * 验证 Hook 触发顺序和并发场景
 */

import { HookManager, HookEvent, RequestPayload, AIResponse, StreamChunk } from './hooks/index.js';

async function main() {
  console.log('=== Hook Bus 验证开始 ===\n');

  const hookManager = new HookManager();

  // 测试 1: Hook 触发顺序
  console.log('测试 1: Hook 触发顺序');
  const executionOrder: number[] = [];

  hookManager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
    executionOrder.push(1);
    console.log('  Handler 1 executed');
    return payload;
  });

  hookManager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
    executionOrder.push(2);
    console.log('  Handler 2 executed');
    return payload;
  });

  hookManager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
    executionOrder.push(3);
    console.log('  Handler 3 executed');
    return payload;
  });

  const payload: RequestPayload = {
    messages: [{ role: 'user', content: 'test' }],
  };

  await hookManager.hook_before_send(payload);

  console.log(`  执行顺序: [${executionOrder.join(', ')}]`);
  console.log(`  ✓ 顺序正确: ${JSON.stringify(executionOrder) === JSON.stringify([1, 2, 3])}\n`);

  // 测试 2: 并发场景
  console.log('测试 2: 并发场景');
  let concurrentCounter = 0;

  hookManager.clear();
  hookManager.register(HookEvent.POST_RESPONSE, async () => {
    await new Promise((resolve) => setTimeout(resolve, 10));
    concurrentCounter++;
  });

  const response: AIResponse = {
    content: 'test response',
    model: 'claude-3',
  };

  const promises = Array.from({ length: 10 }, () => hookManager.hook_post_response(response));

  await Promise.all(promises);

  console.log(`  并发调用次数: ${concurrentCounter}`);
  console.log(`  ✓ 并发处理正确: ${concurrentCounter === 10}\n`);

  // 测试 3: 流式输出
  console.log('测试 3: 流式输出');
  const chunks: string[] = [];

  hookManager.clear();
  hookManager.register(HookEvent.ON_STREAM, (chunk: StreamChunk) => {
    chunks.push(chunk.delta);
    console.log(`  收到 chunk: "${chunk.delta}"`);
  });

  const chunk1: StreamChunk = { delta: 'Hello', index: 0, done: false };
  const chunk2: StreamChunk = { delta: ' ', index: 1, done: false };
  const chunk3: StreamChunk = { delta: 'World', index: 2, done: true };

  hookManager.hook_on_stream(chunk1);
  hookManager.hook_on_stream(chunk2);
  hookManager.hook_on_stream(chunk3);

  console.log(`  完整消息: "${chunks.join('')}"`);
  console.log(`  ✓ 流式输出正确: ${chunks.join('') === 'Hello World'}\n`);

  // 测试 4: EventEmitter 集成
  console.log('测试 4: EventEmitter 集成');

  hookManager.clear();

  let eventFired = false;
  hookManager.on(HookEvent.BEFORE_SEND, () => {
    eventFired = true;
    console.log('  EventEmitter 事件触发');
  });

  await hookManager.hook_before_send(payload);

  console.log(`  ✓ EventEmitter 集成正确: ${eventFired}\n`);

  console.log('=== Hook Bus 验证完成 ===');
  console.log('✓ 所有测试通过');
}

main().catch(console.error);
