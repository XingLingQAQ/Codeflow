/**
 * 审计管理器实现
 * 不可篡改审计日志 + 哈希链完整性
 */
import { IAuditManager, IAuditStorage, AuditLogEntry, AuditActor, AuditResource, AuditQuery, AuditQueryResult, IntegrityVerificationResult, AuditStatistics, AuditExportOptions, AuditRetentionPolicy, AuditSeverity } from './types.js';
export declare class AuditManager implements IAuditManager {
    private storage;
    private retentionPolicy;
    private lastHash;
    constructor(storage?: IAuditStorage);
    private initializeLastHash;
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
    private generateId;
    private calculateHash;
    private exportJson;
    private exportCsv;
    private exportSyslog;
    private severityToPriority;
}
/**
 * 内存审计存储实现
 */
export declare class InMemoryAuditStorage implements IAuditStorage {
    private entries;
    private index;
    append(entry: AuditLogEntry): Promise<void>;
    get(id: string): Promise<AuditLogEntry | null>;
    query(query: AuditQuery): Promise<AuditLogEntry[]>;
    count(query?: AuditQuery): Promise<number>;
    getLastEntry(): Promise<AuditLogEntry | null>;
    delete(ids: string[]): Promise<number>;
    clear(): Promise<void>;
}
//# sourceMappingURL=AuditManager.d.ts.map