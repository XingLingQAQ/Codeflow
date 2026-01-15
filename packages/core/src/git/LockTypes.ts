/**
 * 执行锁机制类型定义
 * 一致性保障与并发控制
 */

/**
 * 锁类型
 */
export type LockType = 'read' | 'write' | 'exclusive';

/**
 * 锁状态
 */
export type LockState = 'pending' | 'acquired' | 'released' | 'timeout' | 'deadlock';

/**
 * 锁请求
 */
export interface LockRequest {
  id: string;
  resourceId: string;
  type: LockType;
  owner: string;
  priority: number;
  timeout: number;
  timestamp: number;
  state: LockState;
}

/**
 * 锁信息
 */
export interface LockInfo {
  resourceId: string;
  type: LockType;
  owner: string;
  acquiredAt: number;
  expiresAt?: number;
  metadata?: Record<string, unknown>;
}

/**
 * 锁配置
 */
export interface LockConfig {
  defaultTimeout: number;
  maxRetries: number;
  retryDelay: number;
  deadlockDetectionInterval: number;
  enableDeadlockDetection: boolean;
  enableFairness: boolean;
}

/**
 * 锁获取结果
 */
export interface LockAcquireResult {
  success: boolean;
  lockId?: string;
  waitTime?: number;
  error?: string;
}

/**
 * 锁释放结果
 */
export interface LockReleaseResult {
  success: boolean;
  error?: string;
}

/**
 * 死锁检测结果
 */
export interface DeadlockDetectionResult {
  hasDeadlock: boolean;
  cycle?: string[];
  victims?: string[];
}

/**
 * 锁统计
 */
export interface LockStats {
  totalAcquired: number;
  totalReleased: number;
  totalTimeout: number;
  totalDeadlock: number;
  currentHeld: number;
  currentPending: number;
  avgWaitTime: number;
}

/**
 * 执行锁管理器接口
 */
export interface ILockManager {
  // 锁操作
  acquire(resourceId: string, type: LockType, owner: string, timeout?: number): Promise<LockAcquireResult>;
  release(lockId: string): Promise<LockReleaseResult>;
  tryAcquire(resourceId: string, type: LockType, owner: string): Promise<LockAcquireResult>;

  // 查询
  isLocked(resourceId: string): boolean;
  getLockInfo(resourceId: string): LockInfo | null;
  getLocksForOwner(owner: string): LockInfo[];
  getPendingRequests(resourceId: string): LockRequest[];

  // 死锁检测
  detectDeadlock(): DeadlockDetectionResult;
  resolveDeadlock(victims: string[]): Promise<void>;

  // 统计
  getStats(): LockStats;

  // 生命周期
  start(): void;
  stop(): void;
}

/**
 * 事务状态
 */
export type TransactionState = 'pending' | 'active' | 'committed' | 'rolledback' | 'failed';

/**
 * 事务信息
 */
export interface TransactionInfo {
  id: string;
  state: TransactionState;
  startedAt: number;
  locks: string[];
  operations: TransactionOperation[];
  metadata?: Record<string, unknown>;
}

/**
 * 事务操作
 */
export interface TransactionOperation {
  type: 'git' | 'vector' | 'graph' | 'conversation';
  action: string;
  resourceId: string;
  timestamp: number;
  data?: unknown;
}

/**
 * 事务管理器接口
 */
export interface ITransactionManager {
  // 事务操作
  begin(owner: string): Promise<string>;
  commit(transactionId: string): Promise<boolean>;
  rollback(transactionId: string): Promise<boolean>;

  // 锁集成
  acquireLock(transactionId: string, resourceId: string, type: LockType): Promise<boolean>;
  releaseLocks(transactionId: string): Promise<void>;

  // 查询
  getTransaction(transactionId: string): TransactionInfo | null;
  getActiveTransactions(): TransactionInfo[];

  // 操作记录
  recordOperation(transactionId: string, operation: TransactionOperation): void;
}

/**
 * 默认锁配置
 */
export const DEFAULT_LOCK_CONFIG: LockConfig = {
  defaultTimeout: 30000, // 30 seconds
  maxRetries: 3,
  retryDelay: 100,
  deadlockDetectionInterval: 5000, // 5 seconds
  enableDeadlockDetection: true,
  enableFairness: true,
};

/**
 * 资源类型常量
 */
export const RESOURCE_TYPES = {
  GIT: 'git',
  VECTOR_STORE: 'vector_store',
  TRIPLE_STORE: 'triple_store',
  CONVERSATION: 'conversation',
  SNAPSHOT: 'snapshot',
} as const;

/**
 * 锁优先级
 */
export const LOCK_PRIORITIES = {
  LOW: 1,
  NORMAL: 5,
  HIGH: 10,
  CRITICAL: 100,
} as const;
