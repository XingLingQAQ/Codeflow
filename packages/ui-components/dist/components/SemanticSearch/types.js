/**
 * 语义检索引擎UI类型定义
 * 三位一体搜索：向量搜索 + 全文搜索 + 图谱关联
 */
/**
 * 搜索类型配置
 */
export const SEARCH_TYPE_CONFIG = {
    vector: { label: 'Vector Search', icon: '🔮', color: '#9C27B0' },
    fulltext: { label: 'Full-text Search', icon: '📝', color: '#2196F3' },
    graph: { label: 'Graph Relations', icon: '🕸️', color: '#4CAF50' },
};
/**
 * 来源颜色映射
 */
export const SOURCE_COLORS = {
    memory: '#9C27B0',
    document: '#2196F3',
    code: '#4CAF50',
    graph: '#FF9800',
};
/**
 * 排序选项
 */
export const SEARCH_SORT_OPTIONS = [
    { field: 'score', label: 'Relevance' },
    { field: 'timestamp', label: 'Time' },
    { field: 'heat', label: 'Heat' },
];
/**
 * 高亮文本中的关键词
 */
export function highlightText(text, query) {
    if (!query.trim())
        return text;
    const words = query.trim().split(/\s+/).filter(Boolean);
    let result = text;
    words.forEach((word) => {
        const regex = new RegExp(`(${word})`, 'gi');
        result = result.replace(regex, '<mark>$1</mark>');
    });
    return result;
}
/**
 * 格式化时间戳
 */
export function formatTimestamp(ts) {
    const date = new Date(ts);
    const now = new Date();
    const diff = now.getTime() - date.getTime();
    if (diff < 60000)
        return 'Just now';
    if (diff < 3600000)
        return `${Math.floor(diff / 60000)}m ago`;
    if (diff < 86400000)
        return `${Math.floor(diff / 3600000)}h ago`;
    if (diff < 604800000)
        return `${Math.floor(diff / 86400000)}d ago`;
    return date.toLocaleDateString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
    });
}
//# sourceMappingURL=types.js.map