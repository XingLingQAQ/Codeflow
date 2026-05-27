/**
 * Git 时间轴类型定义
 */
/**
 * 默认配置
 */
export const DEFAULT_TIMELINE_CONFIG = {
    maxEntries: 50,
    showDiffPreview: true,
    showFileList: true,
    compactMode: false,
    groupByDate: true,
};
/**
 * 触发类型颜色映射
 */
export const TRIGGER_COLORS = {
    hook_after_exec: '#4CAF50',
    manual: '#2196F3',
    auto_checkpoint: '#FF9800',
    before_rollback: '#F44336',
    session_end: '#9C27B0',
};
/**
 * 触发类型标签映射
 */
export const TRIGGER_LABELS = {
    hook_after_exec: 'Exec',
    manual: 'Manual',
    auto_checkpoint: 'Auto',
    before_rollback: 'Backup',
    session_end: 'Session',
};
/**
 * 从 AtomicSnapshot 转换为 TimelineEntry
 */
export function snapshotToTimelineEntry(snapshot) {
    return {
        id: snapshot.id,
        timestamp: snapshot.timestamp,
        gitHash: snapshot.git.hash,
        shortHash: snapshot.git.shortHash,
        message: snapshot.git.message || snapshot.description || 'No message',
        trigger: snapshot.metadata.trigger,
        filesChanged: snapshot.git.files.length,
        messageCount: snapshot.conversation.messageCount,
        tags: snapshot.metadata.tags,
    };
}
/**
 * 按日期分组条目
 */
export function groupEntriesByDate(entries) {
    const groups = new Map();
    for (const entry of entries) {
        const date = new Date(entry.timestamp).toLocaleDateString();
        const existing = groups.get(date) || [];
        existing.push(entry);
        groups.set(date, existing);
    }
    return groups;
}
/**
 * 格式化相对时间
 */
export function formatRelativeTime(timestamp) {
    const now = Date.now();
    const diff = now - timestamp;
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    if (days > 0)
        return `${days}d ago`;
    if (hours > 0)
        return `${hours}h ago`;
    if (minutes > 0)
        return `${minutes}m ago`;
    return 'just now';
}
//# sourceMappingURL=types.js.map