/**
 * 双轨记忆协同类型定义
 * 向量存储 + 图谱存储混合检索
 */
/**
 * 默认双轨搜索配置
 */
export const DEFAULT_DUAL_TRACK_CONFIG = {
    vectorWeight: 0.5,
    graphWeight: 0.3,
    keywordWeight: 0.2,
    topK: 10,
    minScore: 0.3,
    enableSpreadingActivation: true,
    spreadingDepth: 2,
    spreadingDecay: 0.5,
};
//# sourceMappingURL=DualTrackTypes.js.map