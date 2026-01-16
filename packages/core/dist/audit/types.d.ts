/**
 * 审计与合规类型定义
 * 不可篡改审计日志 + 哈希链
 */
/**
 * 审计事件类型
 */
export type AuditEventType = 'access' | 'modify' | 'delete' | 'create' | 'login' | 'logout' | 'permission_change' | 'config_change' | 'error' | 'security';
/**
 * 审计严重级别
 */
export type AuditSeverity = 'info' | 'warning' | 'error' | 'critical';
/**
 * 审计日志条目
 */
export interface AuditLogEntry {
    id: string;
    timestamp: number;
    eventType: AuditEventType;
    severity: AuditSeverity;
    actor: AuditActor;
    resource: AuditResource;
    action: string;
    outcome: 'success' | 'failure';
    details?: Record<string, unknown>;
    previousHash: string;
    hash: string;
}
/**
 * 审计参与者
 */
export interface AuditActor {
    id: string;
    type: 'user' | 'system' | 'agent' | 'service';
    name?: string;
    ip?: string;
    sessionId?: string;
}
/**
 * 审计资源
 */
export interface AuditResource {
    type: string;
    id: string;
    name?: string;
    path?: string;
}
/**
 * 哈希链块
 */
export interface HashChainBlock {
    index: number;
    timestamp: number;
    entries: AuditLogEntry[];
    previousBlockHash: string;
    blockHash: string;
    merkleRoot: string;
}
/**
 * 审计查询条件
 */
export interface AuditQuery {
    startTime?: number;
    endTime?: number;
    eventTypes?: AuditEventType[];
    severities?: AuditSeverity[];
    actorId?: string;
    resourceId?: string;
    resourceType?: string;
    outcome?: 'success' | 'failure';
    limit?: number;
    offset?: number;
}
/**
 * 审计查询结果
 */
export interface AuditQueryResult {
    entries: AuditLogEntry[];
    total: number;
    hasMore: boolean;
}
/**
 * 完整性验证结果
 */
export interface IntegrityVerificationResult {
    valid: boolean;
    checkedEntries: number;
    invalidEntries: string[];
    brokenChainAt?: string;
    verifiedAt: number;
}
/**
 * 审计统计
 */
export interface AuditStatistics {
    totalEntries: number;
    entriesByType: Record<AuditEventType, number>;
    entriesBySeverity: Record<AuditSeverity, number>;
    entriesByOutcome: {
        success: number;
        failure: number;
    };
    oldestEntry?: number;
    newestEntry?: number;
    storageBytes: number;
}
/**
 * 审计导出格式
 */
export type AuditExportFormat = 'json' | 'csv' | 'syslog';
/**
 * 审计导出选项
 */
export interface AuditExportOptions {
    format: AuditExportFormat;
    query?: AuditQuery;
    includeHashes?: boolean;
    compress?: boolean;
}
/**
 * 审计保留策略
 */
export interface AuditRetentionPolicy {
    maxAgeDays: number;
    maxEntries: number;
    archiveBeforeDelete: boolean;
    archivePath?: string;
}
/**
 * 审计管理器接口
 */
export interface IAuditManager {
    log(entry: Omit<AuditLogEntry, 'id' | 'timestamp' | 'previousHash' | 'hash'>): Promise<AuditLogEntry>;
    logAccess(actor: AuditActor, resource: AuditResource, action: string, outcome: 'success' | 'failure'): Promise<AuditLogEntry>;
    logModify(actor: AuditActor, resource: AuditResource, changes: Record<string, unknown>): Promise<AuditLogEntry>;
    logSecurity(actor: AuditActor, event: string, severity: AuditSeverity, details?: Record<string, unknown>): Promise<AuditLogEntry>;
    query(query: AuditQuery): Promise<AuditQueryResult>;
    getEntry(id: string): Promise<AuditLogEntry | null>;
    getLatestEntries(count: number): Promise<AuditLogEntry[]>;
    verifyIntegrity(): Promise<IntegrityVerificationResult>;
    verifyEntry(id: string): Promise<boolean>;
    verifyChain(startId?: string, endId?: string): Promise<IntegrityVerificationResult>;
    getStatistics(): Promise<AuditStatistics>;
    export(options: AuditExportOptions): Promise<string>;
    setRetentionPolicy(policy: AuditRetentionPolicy): void;
    applyRetentionPolicy(): Promise<number>;
}
/**
 * 审计存储接口
 */
export interface IAuditStorage {
    append(entry: AuditLogEntry): Promise<void>;
    get(id: string): Promise<AuditLogEntry | null>;
    query(query: AuditQuery): Promise<AuditLogEntry[]>;
    count(query?: AuditQuery): Promise<number>;
    getLastEntry(): Promise<AuditLogEntry | null>;
    delete(ids: string[]): Promise<number>;
    clear(): Promise<void>;
}
/**
 * 默认保留策略
 */
export declare const DEFAULT_RETENTION_POLICY: AuditRetentionPolicy;
/**
 * 创世块哈希（链的起点）
 */
export declare const GENESIS_HASH = "0000000000000000000000000000000000000000000000000000000000000000";
//# sourceMappingURL=types.d.ts.map