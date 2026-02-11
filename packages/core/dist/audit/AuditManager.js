/**
 * 审计管理器实现
 * 不可篡改审计日志 + 哈希链完整性
 */
import * as crypto from 'crypto';
import { DEFAULT_RETENTION_POLICY, GENESIS_HASH, } from './types.js';
export class AuditManager {
    constructor(storage) {
        this.lastHash = GENESIS_HASH;
        this.storage = storage || new InMemoryAuditStorage();
        this.retentionPolicy = DEFAULT_RETENTION_POLICY;
        this.initializeLastHash();
    }
    async initializeLastHash() {
        const lastEntry = await this.storage.getLastEntry();
        if (lastEntry) {
            this.lastHash = lastEntry.hash;
        }
    }
    async log(entry) {
        const id = this.generateId();
        const timestamp = Date.now();
        const previousHash = this.lastHash;
        // 构建完整条目（hash 将在下一步计算）
        const fullEntry = {
            ...entry,
            id,
            timestamp,
            previousHash,
            hash: '', // 占位，下一步计算
        };
        // 计算哈希
        fullEntry.hash = this.calculateHash(fullEntry);
        this.lastHash = fullEntry.hash;
        // 存储
        await this.storage.append(fullEntry);
        return fullEntry;
    }
    async logAccess(actor, resource, action, outcome) {
        return this.log({
            eventType: 'access',
            severity: outcome === 'failure' ? 'warning' : 'info',
            actor,
            resource,
            action,
            outcome,
        });
    }
    async logModify(actor, resource, changes) {
        return this.log({
            eventType: 'modify',
            severity: 'info',
            actor,
            resource,
            action: 'modify',
            outcome: 'success',
            details: { changes },
        });
    }
    async logSecurity(actor, event, severity, details) {
        return this.log({
            eventType: 'security',
            severity,
            actor,
            resource: { type: 'system', id: 'security' },
            action: event,
            outcome: severity === 'critical' || severity === 'error' ? 'failure' : 'success',
            details,
        });
    }
    async query(query) {
        const entries = await this.storage.query(query);
        const total = await this.storage.count(query);
        return {
            entries,
            total,
            hasMore: (query.offset || 0) + entries.length < total,
        };
    }
    async getEntry(id) {
        return this.storage.get(id);
    }
    async getLatestEntries(count) {
        return this.storage.query({ limit: count });
    }
    async verifyIntegrity() {
        const entries = await this.storage.query({});
        const invalidEntries = [];
        let brokenChainAt;
        let expectedPreviousHash = GENESIS_HASH;
        for (const entry of entries) {
            // 验证哈希链
            if (entry.previousHash !== expectedPreviousHash && !brokenChainAt) {
                brokenChainAt = entry.id;
            }
            // 验证条目哈希
            const calculatedHash = this.calculateHash(entry);
            if (calculatedHash !== entry.hash) {
                invalidEntries.push(entry.id);
            }
            expectedPreviousHash = entry.hash;
        }
        return {
            valid: invalidEntries.length === 0 && !brokenChainAt,
            checkedEntries: entries.length,
            invalidEntries,
            brokenChainAt,
            verifiedAt: Date.now(),
        };
    }
    async verifyEntry(id) {
        const entry = await this.storage.get(id);
        if (!entry)
            return false;
        const calculatedHash = this.calculateHash(entry);
        return calculatedHash === entry.hash;
    }
    async verifyChain(startId, endId) {
        const allEntries = await this.storage.query({});
        let entries = allEntries;
        // 过滤范围
        if (startId || endId) {
            const startIndex = startId ? allEntries.findIndex(e => e.id === startId) : 0;
            const endIndex = endId ? allEntries.findIndex(e => e.id === endId) : allEntries.length - 1;
            if (startIndex >= 0 && endIndex >= 0) {
                entries = allEntries.slice(startIndex, endIndex + 1);
            }
        }
        const invalidEntries = [];
        let brokenChainAt;
        for (let i = 0; i < entries.length; i++) {
            const entry = entries[i];
            // 验证条目哈希
            const calculatedHash = this.calculateHash(entry);
            if (calculatedHash !== entry.hash) {
                invalidEntries.push(entry.id);
            }
            // 验证链接（除了第一个条目）
            if (i > 0) {
                const previousEntry = entries[i - 1];
                if (entry.previousHash !== previousEntry.hash && !brokenChainAt) {
                    brokenChainAt = entry.id;
                }
            }
        }
        return {
            valid: invalidEntries.length === 0 && !brokenChainAt,
            checkedEntries: entries.length,
            invalidEntries,
            brokenChainAt,
            verifiedAt: Date.now(),
        };
    }
    async getStatistics() {
        const entries = await this.storage.query({});
        const stats = {
            totalEntries: entries.length,
            entriesByType: {},
            entriesBySeverity: {},
            entriesByOutcome: { success: 0, failure: 0 },
            storageBytes: 0,
        };
        // 初始化计数器
        const eventTypes = [
            'access', 'modify', 'delete', 'create', 'login',
            'logout', 'permission_change', 'config_change', 'error', 'security'
        ];
        const severities = ['info', 'warning', 'error', 'critical'];
        for (const type of eventTypes) {
            stats.entriesByType[type] = 0;
        }
        for (const severity of severities) {
            stats.entriesBySeverity[severity] = 0;
        }
        // 统计
        for (const entry of entries) {
            stats.entriesByType[entry.eventType]++;
            stats.entriesBySeverity[entry.severity]++;
            stats.entriesByOutcome[entry.outcome]++;
            stats.storageBytes += JSON.stringify(entry).length;
            if (!stats.oldestEntry || entry.timestamp < stats.oldestEntry) {
                stats.oldestEntry = entry.timestamp;
            }
            if (!stats.newestEntry || entry.timestamp > stats.newestEntry) {
                stats.newestEntry = entry.timestamp;
            }
        }
        return stats;
    }
    async export(options) {
        const entries = options.query
            ? await this.storage.query(options.query)
            : await this.storage.query({});
        switch (options.format) {
            case 'json':
                return this.exportJson(entries, options.includeHashes);
            case 'csv':
                return this.exportCsv(entries, options.includeHashes);
            case 'syslog':
                return this.exportSyslog(entries);
            default:
                return this.exportJson(entries, options.includeHashes);
        }
    }
    setRetentionPolicy(policy) {
        this.retentionPolicy = policy;
    }
    async applyRetentionPolicy() {
        const cutoffTime = Date.now() - this.retentionPolicy.maxAgeDays * 24 * 60 * 60 * 1000;
        const entries = await this.storage.query({});
        const toDelete = [];
        // 按时间过滤
        for (const entry of entries) {
            if (entry.timestamp < cutoffTime) {
                toDelete.push(entry.id);
            }
        }
        // 按数量限制
        if (entries.length > this.retentionPolicy.maxEntries) {
            const excess = entries.length - this.retentionPolicy.maxEntries;
            const oldestEntries = entries
                .sort((a, b) => a.timestamp - b.timestamp)
                .slice(0, excess);
            for (const entry of oldestEntries) {
                if (!toDelete.includes(entry.id)) {
                    toDelete.push(entry.id);
                }
            }
        }
        if (toDelete.length > 0) {
            return this.storage.delete(toDelete);
        }
        return 0;
    }
    // ==================== Private Methods ====================
    generateId() {
        return `audit_${Date.now()}_${crypto.randomBytes(4).toString('hex')}`;
    }
    calculateHash(entry) {
        const data = {
            id: entry.id,
            timestamp: entry.timestamp,
            eventType: entry.eventType,
            severity: entry.severity,
            actor: entry.actor,
            resource: entry.resource,
            action: entry.action,
            outcome: entry.outcome,
            details: entry.details,
            previousHash: entry.previousHash,
        };
        return crypto
            .createHash('sha256')
            .update(JSON.stringify(data))
            .digest('hex');
    }
    exportJson(entries, includeHashes) {
        if (!includeHashes) {
            return JSON.stringify(entries.map(e => {
                const { hash, previousHash, ...rest } = e;
                return rest;
            }), null, 2);
        }
        return JSON.stringify(entries, null, 2);
    }
    exportCsv(entries, includeHashes) {
        const headers = [
            'id', 'timestamp', 'eventType', 'severity',
            'actorId', 'actorType', 'resourceType', 'resourceId',
            'action', 'outcome'
        ];
        if (includeHashes) {
            headers.push('previousHash', 'hash');
        }
        const rows = entries.map(e => {
            const row = [
                e.id,
                new Date(e.timestamp).toISOString(),
                e.eventType,
                e.severity,
                e.actor.id,
                e.actor.type,
                e.resource.type,
                e.resource.id,
                e.action,
                e.outcome,
            ];
            if (includeHashes) {
                row.push(e.previousHash, e.hash);
            }
            return row.join(',');
        });
        return [headers.join(','), ...rows].join('\n');
    }
    exportSyslog(entries) {
        return entries
            .map(e => {
            const priority = this.severityToPriority(e.severity);
            const timestamp = new Date(e.timestamp).toISOString();
            return `<${priority}>${timestamp} audit ${e.eventType}: actor=${e.actor.id} resource=${e.resource.type}:${e.resource.id} action=${e.action} outcome=${e.outcome}`;
        })
            .join('\n');
    }
    severityToPriority(severity) {
        switch (severity) {
            case 'critical': return 2;
            case 'error': return 3;
            case 'warning': return 4;
            case 'info': return 6;
            default: return 6;
        }
    }
}
/**
 * 内存审计存储实现
 */
export class InMemoryAuditStorage {
    constructor() {
        this.entries = [];
        this.index = new Map();
    }
    async append(entry) {
        this.index.set(entry.id, this.entries.length);
        this.entries.push(entry);
    }
    async get(id) {
        const index = this.index.get(id);
        if (index === undefined)
            return null;
        return this.entries[index] || null;
    }
    async query(query) {
        let results = [...this.entries];
        // 按时间排序（最新在前）
        results.sort((a, b) => b.timestamp - a.timestamp);
        // 应用过滤条件
        if (query.startTime) {
            results = results.filter(e => e.timestamp >= query.startTime);
        }
        if (query.endTime) {
            results = results.filter(e => e.timestamp <= query.endTime);
        }
        if (query.eventTypes?.length) {
            results = results.filter(e => query.eventTypes.includes(e.eventType));
        }
        if (query.severities?.length) {
            results = results.filter(e => query.severities.includes(e.severity));
        }
        if (query.actorId) {
            results = results.filter(e => e.actor.id === query.actorId);
        }
        if (query.resourceId) {
            results = results.filter(e => e.resource.id === query.resourceId);
        }
        if (query.resourceType) {
            results = results.filter(e => e.resource.type === query.resourceType);
        }
        if (query.outcome) {
            results = results.filter(e => e.outcome === query.outcome);
        }
        // 分页
        if (query.offset) {
            results = results.slice(query.offset);
        }
        if (query.limit) {
            results = results.slice(0, query.limit);
        }
        return results;
    }
    async count(query) {
        if (!query)
            return this.entries.length;
        const results = await this.query({ ...query, limit: undefined, offset: undefined });
        return results.length;
    }
    async getLastEntry() {
        if (this.entries.length === 0)
            return null;
        return this.entries[this.entries.length - 1];
    }
    async delete(ids) {
        const idSet = new Set(ids);
        const originalLength = this.entries.length;
        this.entries = this.entries.filter(e => !idSet.has(e.id));
        // 重建索引
        this.index.clear();
        this.entries.forEach((e, i) => this.index.set(e.id, i));
        return originalLength - this.entries.length;
    }
    async clear() {
        this.entries = [];
        this.index.clear();
    }
}
//# sourceMappingURL=AuditManager.js.map