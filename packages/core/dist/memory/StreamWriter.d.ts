/**
 * 流式写入器
 * 支持 hook_post_response → Chunking → Embedding 流程
 */
import { AIResponse, Message } from '../hooks/types.js';
import { HookManager } from '../hooks/HookManager.js';
import { IVectorStore, ChunkMetadata, StreamWriteConfig } from './types.js';
export declare class StreamWriter {
    private vectorStore;
    private chunker;
    private config;
    private buffer;
    private flushTimer?;
    private hookManager?;
    private messageCounter;
    constructor(vectorStore: IVectorStore, hookManager?: HookManager, config?: Partial<StreamWriteConfig>);
    write(content: string, metadata: Omit<ChunkMetadata, 'chunkIndex' | 'messageIndex'>): Promise<void>;
    writeMessage(message: Message, sessionId: string, gitCommitHash?: string): Promise<void>;
    writeResponse(response: AIResponse, sessionId: string, gitCommitHash?: string): Promise<void>;
    flush(): Promise<void>;
    stop(): void;
    close(): Promise<void>;
    getBufferSize(): number;
    private startFlushTimer;
    private registerHooks;
    createPostResponseHandler(sessionId: string, getGitCommitHash?: () => string | undefined): (response: AIResponse) => Promise<void>;
}
//# sourceMappingURL=StreamWriter.d.ts.map