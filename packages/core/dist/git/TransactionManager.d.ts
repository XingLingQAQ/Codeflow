/**
 * 事务管理器实现
 * Git 事务期间禁用回滚
 */
import { ITransactionManager, ILockManager, TransactionInfo, TransactionOperation, LockType } from './LockTypes.js';
export interface TransactionManagerConfig {
    transactionTimeout: number;
    autoRollbackOnError: boolean;
    lockTimeout: number;
}
export declare class TransactionManager implements ITransactionManager {
    private config;
    private lockManager;
    private transactions;
    private ownerToTransaction;
    constructor(lockManager: ILockManager, config?: Partial<TransactionManagerConfig>);
    begin(owner: string): Promise<string>;
    commit(transactionId: string): Promise<boolean>;
    rollback(transactionId: string): Promise<boolean>;
    acquireLock(transactionId: string, resourceId: string, type: LockType): Promise<boolean>;
    releaseLocks(transactionId: string): Promise<void>;
    getTransaction(transactionId: string): TransactionInfo | null;
    getActiveTransactions(): TransactionInfo[];
    recordOperation(transactionId: string, operation: TransactionOperation): void;
    /**
     * 检查是否可以执行回滚
     * Git 事务期间禁用回滚
     */
    canRollback(transactionId: string): boolean;
    /**
     * 开始 Git 事务
     * 自动获取 Git 资源的排他锁
     */
    beginGitTransaction(transactionId: string): Promise<boolean>;
    /**
     * 结束 Git 事务
     */
    endGitTransaction(transactionId: string, success: boolean): Promise<void>;
    private validateOperation;
    private isOperationComplete;
    private rollbackOperation;
    private handleTransactionTimeout;
}
//# sourceMappingURL=TransactionManager.d.ts.map