/**
 * 原子快照管理器实现
 * 三位一体快照：Git + Conversation + Vector/Graph
 */
import { AtomicSnapshot, SnapshotTrigger, RollbackOptions, RollbackResult, SnapshotValidation, IAtomicSnapshotManager, ISnapshotStorage } from './AtomicSnapshotTypes.js';
import { IGitManager } from './types.js';
import { Message } from '../hooks/types.js';
import { IVectorStore } from '../memory/types.js';
import { ITripleStore } from '../samg/types.js';
export interface AtomicSnapshotManagerConfig {
    maxSnapshots: number;
    autoCheckpointInterval: number;
    enableAutoCheckpoint: boolean;
    createBackupOnRollback: boolean;
}
export declare class AtomicSnapshotManager implements IAtomicSnapshotManager {
    private config;
    private storage;
    private gitManager;
    private vectorStore?;
    private tripleStore?;
    private conversationProvider?;
    private checkpointTimer?;
    constructor(storage: ISnapshotStorage, gitManager: IGitManager, config?: Partial<AtomicSnapshotManagerConfig>);
    setVectorStore(store: IVectorStore): void;
    setTripleStore(store: ITripleStore): void;
    setConversationProvider(provider: () => {
        sessionId: string;
        messages: Message[];
    }): void;
    createSnapshot(description?: string, trigger?: SnapshotTrigger): Promise<AtomicSnapshot>;
    getSnapshot(id: string): Promise<AtomicSnapshot | null>;
    listSnapshots(limit?: number): Promise<AtomicSnapshot[]>;
    findSnapshotByGitHash(gitHash: string): Promise<AtomicSnapshot | null>;
    findSnapshotsBySession(sessionId: string): Promise<AtomicSnapshot[]>;
    rollback(options: RollbackOptions): Promise<RollbackResult>;
    canRollback(snapshotId: string): Promise<boolean>;
    validateSnapshot(id: string): Promise<SnapshotValidation>;
    validateConsistency(): Promise<SnapshotValidation>;
    pruneSnapshots(keepCount: number): Promise<number>;
    deleteSnapshot(id: string): Promise<boolean>;
    destroy(): void;
    private captureConversation;
    private captureVector;
    private captureGraph;
    private rollbackGitDirect;
    private rollbackVector;
    private rollbackGraph;
    private pruneIfNeeded;
    private startAutoCheckpoint;
    private generateChecksum;
}
/**
 * 内存快照存储实现
 */
export declare class InMemorySnapshotStorage implements ISnapshotStorage {
    private snapshots;
    save(snapshot: AtomicSnapshot): Promise<void>;
    load(id: string): Promise<AtomicSnapshot | null>;
    list(limit?: number, offset?: number): Promise<AtomicSnapshot[]>;
    delete(id: string): Promise<boolean>;
    clear(): Promise<void>;
    count(): Promise<number>;
}
//# sourceMappingURL=AtomicSnapshotManager.d.ts.map