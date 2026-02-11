/**
 * 执行锁管理器实现
 * 支持死锁检测与超时释放
 */
import { randomUUID } from 'crypto';
import { DEFAULT_LOCK_CONFIG, LOCK_PRIORITIES, } from './LockTypes.js';
export class LockManager {
    constructor(config = {}) {
        this.locks = new Map();
        this.pendingRequests = new Map();
        this.lockIdToResource = new Map();
        this.waitGraph = new Map(); // owner -> waiting for owners
        this.stats = {
            totalAcquired: 0,
            totalReleased: 0,
            totalTimeout: 0,
            totalDeadlock: 0,
            currentHeld: 0,
            currentPending: 0,
            avgWaitTime: 0,
        };
        this.totalWaitTime = 0;
        this.config = { ...DEFAULT_LOCK_CONFIG, ...config };
    }
    start() {
        if (this.config.enableDeadlockDetection) {
            this.deadlockDetectionTimer = setInterval(() => {
                const result = this.detectDeadlock();
                if (result.hasDeadlock && result.victims) {
                    this.resolveDeadlock(result.victims);
                }
            }, this.config.deadlockDetectionInterval);
        }
    }
    stop() {
        if (this.deadlockDetectionTimer) {
            clearInterval(this.deadlockDetectionTimer);
            this.deadlockDetectionTimer = undefined;
        }
    }
    async acquire(resourceId, type, owner, timeout) {
        const effectiveTimeout = timeout ?? this.config.defaultTimeout;
        const startTime = Date.now();
        // 创建锁请求
        const request = {
            id: randomUUID(),
            resourceId,
            type,
            owner,
            priority: LOCK_PRIORITIES.NORMAL,
            timeout: effectiveTimeout,
            timestamp: startTime,
            state: 'pending',
        };
        // 尝试立即获取
        const immediateResult = this.tryAcquireInternal(resourceId, type, owner);
        if (immediateResult.success) {
            return immediateResult;
        }
        // 加入等待队列
        this.addToPendingQueue(request);
        this.updateWaitGraph(owner, resourceId);
        // 等待锁
        return new Promise((resolve) => {
            const checkInterval = 50;
            let elapsed = 0;
            const check = () => {
                // 检查超时
                if (elapsed >= effectiveTimeout) {
                    this.removeFromPendingQueue(request);
                    this.removeFromWaitGraph(owner);
                    this.stats.totalTimeout++;
                    resolve({
                        success: false,
                        error: `Lock acquisition timeout after ${effectiveTimeout}ms`,
                    });
                    return;
                }
                // 尝试获取
                const result = this.tryAcquireInternal(resourceId, type, owner);
                if (result.success) {
                    this.removeFromPendingQueue(request);
                    this.removeFromWaitGraph(owner);
                    const waitTime = Date.now() - startTime;
                    this.totalWaitTime += waitTime;
                    this.stats.avgWaitTime = this.totalWaitTime / this.stats.totalAcquired;
                    resolve({ ...result, waitTime });
                    return;
                }
                elapsed += checkInterval;
                setTimeout(check, checkInterval);
            };
            check();
        });
    }
    async release(lockId) {
        const resourceId = this.lockIdToResource.get(lockId);
        if (!resourceId) {
            return { success: false, error: 'Lock not found' };
        }
        const lockInfo = this.locks.get(resourceId);
        if (!lockInfo) {
            return { success: false, error: 'Lock not found' };
        }
        // 释放锁
        this.locks.delete(resourceId);
        this.lockIdToResource.delete(lockId);
        this.stats.totalReleased++;
        this.stats.currentHeld--;
        // 处理等待队列中的下一个请求
        this.processNextPending(resourceId);
        return { success: true };
    }
    tryAcquire(resourceId, type, owner) {
        return Promise.resolve(this.tryAcquireInternal(resourceId, type, owner));
    }
    isLocked(resourceId) {
        return this.locks.has(resourceId);
    }
    getLockInfo(resourceId) {
        return this.locks.get(resourceId) || null;
    }
    getLocksForOwner(owner) {
        return Array.from(this.locks.values()).filter(l => l.owner === owner);
    }
    getPendingRequests(resourceId) {
        return this.pendingRequests.get(resourceId) || [];
    }
    detectDeadlock() {
        // 使用 DFS 检测环
        const visited = new Set();
        const recursionStack = new Set();
        const cycle = [];
        const dfs = (node, path) => {
            visited.add(node);
            recursionStack.add(node);
            path.push(node);
            const neighbors = this.waitGraph.get(node);
            if (neighbors) {
                for (const neighbor of neighbors) {
                    if (!visited.has(neighbor)) {
                        if (dfs(neighbor, path)) {
                            return true;
                        }
                    }
                    else if (recursionStack.has(neighbor)) {
                        // 找到环
                        const cycleStart = path.indexOf(neighbor);
                        cycle.push(...path.slice(cycleStart), neighbor);
                        return true;
                    }
                }
            }
            path.pop();
            recursionStack.delete(node);
            return false;
        };
        for (const node of this.waitGraph.keys()) {
            if (!visited.has(node)) {
                if (dfs(node, [])) {
                    // 选择优先级最低的作为 victim
                    const victims = this.selectVictims(cycle);
                    this.stats.totalDeadlock++;
                    return { hasDeadlock: true, cycle, victims };
                }
            }
        }
        return { hasDeadlock: false };
    }
    async resolveDeadlock(victims) {
        for (const victim of victims) {
            // 取消 victim 的所有等待请求
            for (const [resourceId, requests] of this.pendingRequests) {
                const victimRequests = requests.filter(r => r.owner === victim);
                for (const request of victimRequests) {
                    request.state = 'deadlock';
                    this.removeFromPendingQueue(request);
                }
            }
            // 释放 victim 持有的所有锁
            const ownedLocks = this.getLocksForOwner(victim);
            for (const lock of ownedLocks) {
                const lockId = Array.from(this.lockIdToResource.entries())
                    .find(([, res]) => res === lock.resourceId)?.[0];
                if (lockId) {
                    await this.release(lockId);
                }
            }
            this.removeFromWaitGraph(victim);
        }
    }
    getStats() {
        return {
            ...this.stats,
            currentHeld: this.locks.size,
            currentPending: Array.from(this.pendingRequests.values())
                .reduce((sum, arr) => sum + arr.length, 0),
        };
    }
    // ==================== Private Methods ====================
    tryAcquireInternal(resourceId, type, owner) {
        const existingLock = this.locks.get(resourceId);
        if (existingLock) {
            // 检查是否是同一个 owner 的重入
            if (existingLock.owner === owner) {
                return { success: true, lockId: this.findLockId(resourceId) };
            }
            // 检查锁兼容性
            if (!this.isCompatible(existingLock.type, type)) {
                return { success: false, error: 'Resource is locked' };
            }
        }
        // 检查是否有更高优先级的等待请求
        if (this.config.enableFairness) {
            const pending = this.pendingRequests.get(resourceId) || [];
            const higherPriority = pending.some(r => r.owner !== owner && r.timestamp < Date.now() - 1000);
            if (higherPriority) {
                return { success: false, error: 'Higher priority request pending' };
            }
        }
        // 获取锁
        const lockId = randomUUID();
        const lockInfo = {
            resourceId,
            type,
            owner,
            acquiredAt: Date.now(),
            expiresAt: Date.now() + this.config.defaultTimeout,
        };
        this.locks.set(resourceId, lockInfo);
        this.lockIdToResource.set(lockId, resourceId);
        this.stats.totalAcquired++;
        this.stats.currentHeld++;
        return { success: true, lockId };
    }
    isCompatible(existing, requested) {
        // 读锁与读锁兼容
        if (existing === 'read' && requested === 'read') {
            return true;
        }
        // 其他情况不兼容
        return false;
    }
    addToPendingQueue(request) {
        if (!this.pendingRequests.has(request.resourceId)) {
            this.pendingRequests.set(request.resourceId, []);
        }
        const queue = this.pendingRequests.get(request.resourceId);
        // 按优先级和时间戳排序插入
        const insertIndex = queue.findIndex(r => r.priority < request.priority ||
            (r.priority === request.priority && r.timestamp > request.timestamp));
        if (insertIndex === -1) {
            queue.push(request);
        }
        else {
            queue.splice(insertIndex, 0, request);
        }
        this.stats.currentPending++;
    }
    removeFromPendingQueue(request) {
        const queue = this.pendingRequests.get(request.resourceId);
        if (queue) {
            const index = queue.findIndex(r => r.id === request.id);
            if (index !== -1) {
                queue.splice(index, 1);
                this.stats.currentPending--;
            }
        }
    }
    processNextPending(resourceId) {
        const queue = this.pendingRequests.get(resourceId);
        if (!queue || queue.length === 0)
            return;
        // 尝试满足队列中的请求
        const toProcess = [...queue];
        for (const request of toProcess) {
            const result = this.tryAcquireInternal(resourceId, request.type, request.owner);
            if (result.success) {
                request.state = 'acquired';
                this.removeFromPendingQueue(request);
            }
        }
    }
    updateWaitGraph(owner, resourceId) {
        const lockInfo = this.locks.get(resourceId);
        if (lockInfo && lockInfo.owner !== owner) {
            if (!this.waitGraph.has(owner)) {
                this.waitGraph.set(owner, new Set());
            }
            this.waitGraph.get(owner).add(lockInfo.owner);
        }
    }
    removeFromWaitGraph(owner) {
        this.waitGraph.delete(owner);
        for (const waiters of this.waitGraph.values()) {
            waiters.delete(owner);
        }
    }
    selectVictims(cycle) {
        if (cycle.length === 0)
            return [];
        // 选择 victim 的策略：
        // 1. 优先选择持有锁数量最少的 owner（影响最小）
        // 2. 如果相同，选择等待时间最短的（最近加入的）
        // 3. 如果还相同，选择优先级最低的
        const ownerScores = cycle.map((owner) => {
            let score = 0;
            // 计算持有的锁数量
            let heldLocks = 0;
            for (const lockInfo of this.locks.values()) {
                if (lockInfo.owner === owner) {
                    heldLocks++;
                }
            }
            score += heldLocks * 100; // 持有锁越多，分数越高（越不应该被选为 victim）
            // 计算等待时间
            const pendingForOwner = Array.from(this.pendingRequests.values())
                .flat()
                .filter((r) => r.owner === owner);
            if (pendingForOwner.length > 0) {
                const oldestRequest = pendingForOwner.reduce((oldest, r) => r.timestamp < oldest.timestamp ? r : oldest);
                const waitTime = Date.now() - oldestRequest.timestamp;
                score += waitTime / 10; // 等待时间越长，分数越高（越不应该被选为 victim）
            }
            // 考虑优先级
            const ownerPriority = pendingForOwner[0]?.priority ?? LOCK_PRIORITIES.NORMAL;
            score += ownerPriority * 50; // 优先级越高，分数越高
            return { owner, score };
        });
        // 选择分数最低的作为 victim
        ownerScores.sort((a, b) => a.score - b.score);
        // 返回分数最低的 owner 作为 victim
        return [ownerScores[0].owner];
    }
    findLockId(resourceId) {
        for (const [lockId, res] of this.lockIdToResource) {
            if (res === resourceId) {
                return lockId;
            }
        }
        return undefined;
    }
}
//# sourceMappingURL=LockManager.js.map