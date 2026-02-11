/**
 * Memory 1 向量存储类型定义
 * 支持 Chroma 向量数据库集成
 */
/**
 * 默认配置
 */
export const DEFAULT_VECTOR_CONFIG = {
    collectionName: 'codeflow_memory',
    chunkSize: 500,
    chunkOverlap: 50,
    host: 'localhost',
    port: 8000,
};
export const DEFAULT_STREAM_CONFIG = {
    batchSize: 10,
    flushInterval: 5000,
};
//# sourceMappingURL=types.js.map