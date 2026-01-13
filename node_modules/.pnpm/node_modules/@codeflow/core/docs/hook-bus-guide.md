# Hook Bus 事件系统使用指南

## 概述

Hook Bus 是 CodeFlow 的核心事件系统，提供统一的生命周期管理和事件订阅/发布机制。

## 核心接口

### IHookManager

```typescript
interface IHookManager {
  // 生命周期 Hooks
  hook_before_send(payload: RequestPayload): Promise<RequestPayload>;
  hook_post_response(response: AIResponse): Promise<void>;
  hook_on_stream(chunk: StreamChunk): void;

  // 上下文治理 Hooks
  hook_before_compress(context: Context): Promise<DecisionSkeleton>;
  hook_on_message_complete(message: Message): Promise<void>;

  // 状态管理 Hooks
  hook_after_exec(result: ExecResult): Promise<SnapshotID>;
  hook_restore_state(snapshotId: SnapshotID): Promise<void>;

  // 记忆检索 Hooks
  hook_on_user_input_submitted(input: string): Promise<MemoryMatch[]>;
}
```

## 使用示例

### 1. 创建 HookManager 实例

```typescript
import { HookManager } from '@codeflow/core';

const hookManager = new HookManager();
```

### 2. 注册 Hook 处理器

#### 发送前拦截（Token 计数）

```typescript
import { HookEvent, RequestPayload } from '@codeflow/core';

hookManager.register(HookEvent.BEFORE_SEND, async (payload: RequestPayload) => {
  // 计算 Token 数量
  const tokenCount = calculateTokens(payload.messages);

  if (tokenCount > 20000) {
    // 触发自动总结
    const summarized = await summarizeContext(payload.messages);
    return { ...payload, messages: summarized };
  }

  return payload;
});
```

#### 响应后处理（向量化存储）

```typescript
hookManager.register(HookEvent.POST_RESPONSE, async (response: AIResponse) => {
  // 将响应存入向量数据库
  await vectorDB.store({
    content: response.content,
    model: response.model,
    timestamp: Date.now(),
  });
});
```

#### 流式输出处理（实时渲染）

```typescript
hookManager.register(HookEvent.ON_STREAM, (chunk: StreamChunk) => {
  // 更新 UI
  updateChatUI(chunk.delta);

  // 检测特殊标记
  if (chunk.delta.includes('```')) {
    enableCodeHighlight();
  }
});
```

#### 执行后快照生成

```typescript
hookManager.register(HookEvent.AFTER_EXEC, async (result: ExecResult) => {
  // 生成 Git 快照
  const gitHash = await git.commit(`Auto snapshot: ${result.command}`);

  // 保存对话状态
  const dialogState = await saveDialogState();

  // 返回快照 ID
  return `snapshot_${gitHash}_${dialogState}`;
});
```

### 3. 触发 Hook

```typescript
// 发送消息前
const payload = {
  messages: [{ role: 'user', content: 'Hello' }],
  model: 'claude-3',
};

const processedPayload = await hookManager.hook_before_send(payload);

// 接收响应后
const response = {
  content: 'Hi there!',
  model: 'claude-3',
};

await hookManager.hook_post_response(response);

// 流式输出
const chunk = {
  delta: 'Hello',
  index: 0,
  done: false,
};

hookManager.hook_on_stream(chunk);
```

### 4. 注销处理器

```typescript
const handler = async (payload: RequestPayload) => {
  // ...
  return payload;
};

// 注册
hookManager.register(HookEvent.BEFORE_SEND, handler);

// 注销
hookManager.unregister(HookEvent.BEFORE_SEND, handler);
```

### 5. 清理所有处理器

```typescript
hookManager.clear();
```

## 高级用法

### 多处理器链式执行

```typescript
// 处理器 1：Token 计数
hookManager.register(HookEvent.BEFORE_SEND, async (payload) => {
  console.log('Token count:', calculateTokens(payload.messages));
  return payload;
});

// 处理器 2：注入 System Prompt
hookManager.register(HookEvent.BEFORE_SEND, async (payload) => {
  return {
    ...payload,
    messages: [
      { role: 'system', content: 'You are a helpful assistant.' },
      ...payload.messages,
    ],
  };
});

// 处理器 3：日志记录
hookManager.register(HookEvent.BEFORE_SEND, async (payload) => {
  await logger.log('Sending request', payload);
  return payload;
});
```

### EventEmitter 集成

```typescript
// 监听 Hook 事件
hookManager.on(HookEvent.BEFORE_SEND, (data, result) => {
  console.log('Before send event:', data, result);
});

hookManager.on(HookEvent.POST_RESPONSE, (data) => {
  console.log('Post response event:', data);
});
```

## 最佳实践

1. **错误处理**：Hook 处理器应捕获并处理异常，避免中断主流程
2. **性能优化**：避免在 Hook 中执行耗时操作，考虑异步处理
3. **顺序依赖**：如果处理器之间有依赖关系，按正确顺序注册
4. **资源清理**：使用完毕后调用 `clear()` 清理处理器
5. **并发安全**：Hook 系统支持并发调用，但处理器内部需自行保证线程安全

## 架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                         GUI Layer (Electron/React)              │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────────┐   │
│  │ Chat UI  │ │ Plan View│ │ Memory   │ │ Context Builder  │   │
│  │          │ │          │ │ Dashboard│ │ (AST Tree)       │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│                      Hook Bus (Event Emitter)                   │
│  hook_before_send | hook_post_response | hook_on_stream | ...   │
├─────────────────────────────────────────────────────────────────┤
│                    Adapter Layer (ICliAdapter)                  │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐           │
│  │ Claude       │ │ Gemini       │ │ Codex        │           │
│  │ Adapter      │ │ Adapter      │ │ Adapter      │           │
│  └──────────────┘ └──────────────┘ └──────────────┘           │
└─────────────────────────────────────────────────────────────────┘
```
