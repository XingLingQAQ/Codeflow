/**
 * 流式写入器
 * 支持 hook_post_response → Chunking → Embedding 流程
 */

import { AIResponse, Message, getMessageText } from '../hooks/types.js';
import { HookManager } from '../hooks/HookManager.js';
import {
  IVectorStore,
  DocumentChunk,
  ChunkMetadata,
  StreamWriteConfig,
  DEFAULT_STREAM_CONFIG,
} from './types.js';
import { TextChunker } from './TextChunker.js';

export class StreamWriter {
  private vectorStore: IVectorStore;
  private chunker: TextChunker;
  private config: StreamWriteConfig;
  private buffer: DocumentChunk[] = [];
  private flushTimer?: ReturnType<typeof globalThis.setInterval>;
  private hookManager?: HookManager;
  private messageCounter = 0;

  constructor(
    vectorStore: IVectorStore,
    hookManager?: HookManager,
    config?: Partial<StreamWriteConfig>
  ) {
    this.vectorStore = vectorStore;
    this.hookManager = hookManager;
    this.config = { ...DEFAULT_STREAM_CONFIG, ...config };
    this.chunker = new TextChunker();

    this.startFlushTimer();
    this.registerHooks();
  }

  async write(
    content: string,
    metadata: Omit<ChunkMetadata, 'chunkIndex' | 'messageIndex'>
  ): Promise<void> {
    const fullMetadata: Omit<ChunkMetadata, 'chunkIndex'> = {
      ...metadata,
      messageIndex: this.messageCounter++,
    };

    const chunks = this.chunker.chunk(content, fullMetadata);
    this.buffer.push(...chunks);

    if (this.buffer.length >= this.config.batchSize) {
      await this.flush();
    }
  }

  async writeMessage(message: Message, sessionId: string, gitCommitHash?: string): Promise<void> {
    await this.write(getMessageText(message.content), {
      sessionId,
      agentRole: 'main',
      gitCommitHash,
      timestamp: message.timestamp || Date.now(),
      source: message.role,
    });
  }

  async writeResponse(
    response: AIResponse,
    sessionId: string,
    gitCommitHash?: string
  ): Promise<void> {
    await this.write(response.content, {
      sessionId,
      agentRole: 'main',
      gitCommitHash,
      timestamp: Date.now(),
      source: 'assistant',
    });
  }

  async flush(): Promise<void> {
    if (this.buffer.length === 0) return;

    const chunksToWrite = [...this.buffer];
    this.buffer = [];

    try {
      await this.vectorStore.add(chunksToWrite);
      this.config.onFlush?.(chunksToWrite);
    } catch (error) {
      this.config.onError?.(error as Error);
      // 失败时将块放回缓冲区
      this.buffer.unshift(...chunksToWrite);
    }
  }

  stop(): void {
    if (this.flushTimer) {
      globalThis.clearInterval(this.flushTimer);
      this.flushTimer = undefined;
    }
  }

  async close(): Promise<void> {
    this.stop();
    await this.flush();
  }

  getBufferSize(): number {
    return this.buffer.length;
  }

  // ==================== 私有方法 ====================

  private startFlushTimer(): void {
    this.flushTimer = globalThis.setInterval(() => {
      this.flush().catch((error) => {
        this.config.onError?.(error);
      });
    }, this.config.flushInterval);
  }

  private registerHooks(): void {
    if (!this.hookManager) return;

    // 注册 post_response hook 处理器
    // 注意：这里我们不直接修改 HookManager，而是提供一个方法让外部注册
  }

  // 创建 hook 处理器（供外部使用）
  createPostResponseHandler(
    sessionId: string,
    getGitCommitHash?: () => string | undefined
  ): (response: AIResponse) => Promise<void> {
    return async (response: AIResponse) => {
      await this.writeResponse(response, sessionId, getGitCommitHash?.());
    };
  }
}
