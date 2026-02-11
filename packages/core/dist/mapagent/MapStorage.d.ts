/**
 * 导图存储实现
 * 基于文件系统的持久化存储
 */
import { IMapStorage, CompressionMap } from './types.js';
export declare class InMemoryMapStorage implements IMapStorage {
    private maps;
    save(map: CompressionMap): Promise<void>;
    load(id: string): Promise<CompressionMap | null>;
    loadBySession(sessionId: string): Promise<CompressionMap[]>;
    delete(id: string): Promise<void>;
    list(): Promise<Array<{
        id: string;
        sessionId: string;
        createdAt: number;
    }>>;
    clear(): void;
    size(): number;
}
//# sourceMappingURL=MapStorage.d.ts.map