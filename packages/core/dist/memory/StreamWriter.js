/**
 * 流式写入器
 * 支持 hook_post_response → Chunking → Embedding 流程
 */
import { DEFAULT_STREAM_CONFIG, } from './types.js';
import { TextChunker } from './TextChunker.js';
export class StreamWriter {
    constructor(vectorStore, hookManager, config) {
        this.buffer = [];
        this.messageCounter = 0;
        this.vectorStore = vectorStore;
        this.hookManager = hookManager;
        this.config = { ...DEFAULT_STREAM_CONFIG, ...config };
        this.chunker = new TextChunker();
        this.startFlushTimer();
        this.registerHooks();
    }
    async write(content, metadata) {
        const fullMetadata = {
            ...metadata,
            messageIndex: this.messageCounter++,
        };
        const chunks = this.chunker.chunk(content, fullMetadata);
        this.buffer.push(...chunks);
        if (this.buffer.length >= this.config.batchSize) {
            await this.flush();
        }
    }
    async writeMessage(message, sessionId, gitCommitHash) {
        await this.write(message.content, {
            sessionId,
            agentRole: 'main',
            gitCommitHash,
            timestamp: message.timestamp || Date.now(),
            source: message.role,
        });
    }
    async writeResponse(response, sessionId, gitCommitHash) {
        await this.write(response.content, {
            sessionId,
            agentRole: 'main',
            gitCommitHash,
            timestamp: Date.now(),
            source: 'assistant',
        });
    }
    async flush() {
        if (this.buffer.length === 0)
            return;
        const chunksToWrite = [...this.buffer];
        this.buffer = [];
        try {
            await this.vectorStore.add(chunksToWrite);
            this.config.onFlush?.(chunksToWrite);
        }
        catch (error) {
            this.config.onError?.(error);
            // 失败时将块放回缓冲区
            this.buffer.unshift(...chunksToWrite);
        }
    }
    stop() {
        if (this.flushTimer) {
            globalThis.clearInterval(this.flushTimer);
            this.flushTimer = undefined;
        }
    }
    async close() {
        this.stop();
        await this.flush();
    }
    getBufferSize() {
        return this.buffer.length;
    }
    // ==================== 私有方法 ====================
    startFlushTimer() {
        this.flushTimer = globalThis.setInterval(() => {
            this.flush().catch((error) => {
                this.config.onError?.(error);
            });
        }, this.config.flushInterval);
    }
    registerHooks() {
        if (!this.hookManager)
            return;
        // 注册 post_response hook 处理器
        // 注意：这里我们不直接修改 HookManager，而是提供一个方法让外部注册
    }
    // 创建 hook 处理器（供外部使用）
    createPostResponseHandler(sessionId, getGitCommitHash) {
        return async (response) => {
            await this.writeResponse(response, sessionId, getGitCommitHash?.());
        };
    }
}
//# sourceMappingURL=StreamWriter.js.map