/**
 * 原子快照类型定义
 * 三位一体快照结构：Git + Conversation + Vector/Graph
 */
/**
 * 默认快照配置
 */
export const DEFAULT_SNAPSHOT_CONFIG = {
    maxSnapshots: 100,
    autoCheckpointInterval: 300000, // 5 minutes
    enableAutoCheckpoint: true,
    createBackupOnRollback: true,
};
//# sourceMappingURL=AtomicSnapshotTypes.js.map