/**
 * 隐私感知 RAG 类型定义
 * AES-CBC 加密 + 隐私保护检索
 */
/**
 * 默认加密配置
 */
export const DEFAULT_ENCRYPTION_CONFIG = {
    algorithm: 'aes-256-cbc',
    keyDerivation: 'pbkdf2',
    iterations: 100000,
    saltLength: 16,
    ivLength: 16,
};
/**
 * 默认隐私策略
 */
export const DEFAULT_PRIVACY_POLICY = {
    level: 'internal',
    encryptAtRest: true,
    encryptInTransit: true,
    allowedRoles: ['admin', 'user'],
    retentionDays: 365,
    autoRedact: false,
};
/**
 * 隐私级别优先级
 */
export const PRIVACY_LEVEL_PRIORITY = {
    public: 0,
    internal: 1,
    confidential: 2,
    secret: 3,
};
//# sourceMappingURL=types.js.map