/**
 * 事务管理器实现
 * Git 事务期间禁用回滚
 */
import { randomUUID } from 'crypto';
import { RESOURCE_TYPES, } from './LockTypes.js';
const DEFAULT_CONFIG = {
    transactionTimeout: 60000, // 1 minute
    autoRollbackOnError: true,
    lockTimeout: 30000,
};
export class TransactionManager {
    constructor(lockManager, config = {}) {
        this.transactions = new Map();
        this.ownerToTransaction = new Map();
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.lockManager = lockManager;
    }
    async begin(owner) {
        // 检查 owner 是否已有活跃事务
        const existingTxId = this.ownerToTransaction.get(owner);
        if (existingTxId) {
            const existingTx = this.transactions.get(existingTxId);
            if (existingTx && existingTx.state === 'active') {
                throw new Error(`Owner ${owner} already has an active transaction: ${existingTxId}`);
            }
        }
        const transactionId = randomUUID();
        const transaction = {
            id: transactionId,
            state: 'active',
            startedAt: Date.now(),
            locks: [],
            operations: [],
        };
        this.transactions.set(transactionId, transaction);
        this.ownerToTransaction.set(owner, transactionId);
        // 设置事务超时
        setTimeout(() => {
            this.handleTransactionTimeout(transactionId);
        }, this.config.transactionTimeout);
        return transactionId;
    }
    async commit(transactionId) {
        const transaction = this.transactions.get(transactionId);
        if (!transaction) {
            throw new Error(`Transaction not found: ${transactionId}`);
        }
        if (transaction.state !== 'active') {
            throw new Error(`Transaction ${transactionId} is not active (state: ${transaction.state})`);
        }
        try {
            // 验证所有操作
            for (const op of transaction.operations) {
                if (!this.validateOperation(op)) {
                    throw new Error(`Invalid operation: ${op.action} on ${op.resourceId}`);
                }
            }
            // 提交成功
            transaction.state = 'committed';
            // 释放所有锁
            await this.releaseLocks(transactionId);
            return true;
        }
        catch (error) {
            // 提交失败，标记为失败状态
            transaction.state = 'failed';
            if (this.config.autoRollbackOnError) {
                await this.rollback(transactionId);
            }
            throw error;
        }
    }
    async rollback(transactionId) {
        const transaction = this.transactions.get(transactionId);
        if (!transaction) {
            throw new Error(`Transaction not found: ${transactionId}`);
        }
        // 检查是否有 Git 操作正在进行
        const hasActiveGitOp = transaction.operations.some(op => op.type === 'git' && !this.isOperationComplete(op));
        if (hasActiveGitOp) {
            throw new Error('Cannot rollback: Git operation in progress');
        }
        try {
            // 逆序回滚操作
            const reversedOps = [...transaction.operations].reverse();
            for (const op of reversedOps) {
                await this.rollbackOperation(op);
            }
            transaction.state = 'rolledback';
            // 释放所有锁
            await this.releaseLocks(transactionId);
            return true;
        }
        catch (error) {
            transaction.state = 'failed';
            throw error;
        }
    }
    async acquireLock(transactionId, resourceId, type) {
        const transaction = this.transactions.get(transactionId);
        if (!transaction) {
            throw new Error(`Transaction not found: ${transactionId}`);
        }
        if (transaction.state !== 'active') {
            throw new Error(`Transaction ${transactionId} is not active`);
        }
        const result = await this.lockManager.acquire(resourceId, type, transactionId, this.config.lockTimeout);
        if (result.success && result.lockId) {
            transaction.locks.push(result.lockId);
            return true;
        }
        return false;
    }
    async releaseLocks(transactionId) {
        const transaction = this.transactions.get(transactionId);
        if (!transaction)
            return;
        for (const lockId of transaction.locks) {
            await this.lockManager.release(lockId);
        }
        transaction.locks = [];
        // 清理 owner 映射
        for (const [owner, txId] of this.ownerToTransaction) {
            if (txId === transactionId) {
                this.ownerToTransaction.delete(owner);
                break;
            }
        }
    }
    getTransaction(transactionId) {
        return this.transactions.get(transactionId) || null;
    }
    getActiveTransactions() {
        return Array.from(this.transactions.values())
            .filter(t => t.state === 'active');
    }
    recordOperation(transactionId, operation) {
        const transaction = this.transactions.get(transactionId);
        if (!transaction) {
            throw new Error(`Transaction not found: ${transactionId}`);
        }
        if (transaction.state !== 'active') {
            throw new Error(`Transaction ${transactionId} is not active`);
        }
        transaction.operations.push(operation);
    }
    // ==================== Git 事务保护 ====================
    /**
     * 检查是否可以执行回滚
     * Git 事务期间禁用回滚
     */
    canRollback(transactionId) {
        const transaction = this.transactions.get(transactionId);
        if (!transaction)
            return false;
        // 检查是否有活跃的 Git 操作
        const hasActiveGitOp = transaction.operations.some(op => op.type === 'git' && !this.isOperationComplete(op));
        return !hasActiveGitOp;
    }
    /**
     * 开始 Git 事务
     * 自动获取 Git 资源的排他锁
     */
    async beginGitTransaction(transactionId) {
        const acquired = await this.acquireLock(transactionId, RESOURCE_TYPES.GIT, 'exclusive');
        if (acquired) {
            this.recordOperation(transactionId, {
                type: 'git',
                action: 'begin',
                resourceId: RESOURCE_TYPES.GIT,
                timestamp: Date.now(),
            });
        }
        return acquired;
    }
    /**
     * 结束 Git 事务
     */
    async endGitTransaction(transactionId, success) {
        this.recordOperation(transactionId, {
            type: 'git',
            action: success ? 'commit' : 'rollback',
            resourceId: RESOURCE_TYPES.GIT,
            timestamp: Date.now(),
            data: { success },
        });
    }
    // ==================== Private Methods ====================
    validateOperation(op) {
        // 基本验证
        if (!op.type || !op.action || !op.resourceId) {
            return false;
        }
        return true;
    }
    isOperationComplete(op) {
        // Git 操作：检查是否有对应的结束操作
        if (op.type === 'git' && op.action === 'begin') {
            return false; // 需要检查是否有 commit/rollback
        }
        return true;
    }
    async rollbackOperation(op) {
        // 根据操作类型执行回滚
        switch (op.type) {
            case 'git':
                // Git 回滚由 GitManager 处理
                break;
            case 'vector':
                // 向量存储回滚
                break;
            case 'graph':
                // 图谱回滚
                break;
            case 'conversation':
                // 对话回滚
                break;
        }
    }
    handleTransactionTimeout(transactionId) {
        const transaction = this.transactions.get(transactionId);
        if (!transaction || transaction.state !== 'active')
            return;
        // 超时处理：尝试回滚
        transaction.state = 'failed';
        this.rollback(transactionId).catch(() => {
            // 回滚失败，强制释放锁
            this.releaseLocks(transactionId);
        });
    }
}
//# sourceMappingURL=TransactionManager.js.map