/**
 * JSONL 文件存储层
 * 支持 memory_graph.jsonl 读写
 */
import { Triple, JsonLdGraph } from './types.js';
export interface JsonlStorageConfig {
    filePath: string;
    autoFlush: boolean;
    flushInterval: number;
    maxBufferSize: number;
}
export declare class JsonlStorage {
    private config;
    private writeBuffer;
    private flushTimer;
    constructor(config?: Partial<JsonlStorageConfig>);
    append(triples: Triple[]): Promise<void>;
    flush(): Promise<void>;
    readAll(): Promise<Triple[]>;
    readStream(callback: (triple: Triple) => void | Promise<void>): Promise<number>;
    exportToJsonLd(): Promise<JsonLdGraph>;
    importFromJsonLd(graph: JsonLdGraph): Promise<void>;
    clear(): Promise<void>;
    getStats(): Promise<{
        lineCount: number;
        fileSize: number;
    }>;
    deleteByTimestamp(beforeTimestamp: number): Promise<number>;
    deleteBySessionId(sessionId: string): Promise<number>;
    close(): void;
    private appendToFile;
    private startAutoFlush;
}
//# sourceMappingURL=JsonlStorage.d.ts.map