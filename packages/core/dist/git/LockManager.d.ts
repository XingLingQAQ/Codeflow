/**
 * 执行锁管理器实现
 * 支持死锁检测与超时释放
 */
import { ILockManager, LockType, LockRequest, LockInfo, LockConfig, LockAcquireResult, LockReleaseResult, DeadlockDetectionResult, LockStats } from './LockTypes.js';
export declare class LockManager implements ILockManager {
    private config;
    private locks;
    private pendingRequests;
    private lockIdToResource;
    private waitGraph;
    private stats;
    private totalWaitTime;
    private deadlockDetectionTimer?;
    constructor(config?: Partial<LockConfig>);
    start(): void;
    stop(): void;
    acquire(resourceId: string, type: LockType, owner: string, timeout?: number): Promise<LockAcquireResult>;
    release(lockId: string): Promise<LockReleaseResult>;
    tryAcquire(resourceId: string, type: LockType, owner: string): Promise<LockAcquireResult>;
    isLocked(resourceId: string): boolean;
    getLockInfo(resourceId: string): LockInfo | null;
    getLocksForOwner(owner: string): LockInfo[];
    getPendingRequests(resourceId: string): LockRequest[];
    detectDeadlock(): DeadlockDetectionResult;
    resolveDeadlock(victims: string[]): Promise<void>;
    getStats(): LockStats;
    private tryAcquireInternal;
    private isCompatible;
    private addToPendingQueue;
    private removeFromPendingQueue;
    private processNextPending;
    private updateWaitGraph;
    private removeFromWaitGraph;
    private selectVictims;
    private findLockId;
}
//# sourceMappingURL=LockManager.d.ts.map