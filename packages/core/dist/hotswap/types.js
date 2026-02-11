/**
 * 模型热切换类型定义
 */
/**
 * 默认配置
 */
export const DEFAULT_HOTSWAP_CONFIG = {
    defaultModel: 'claude-3-opus',
    autoRetry: true,
    retryStrategy: {
        maxRetries: 3,
        baseDelay: 1000,
        maxDelay: 10000,
        backoffMultiplier: 2,
        retryableErrors: ['rate_limit', 'timeout', 'server_error'],
    },
    relayConfig: {
        enabled: true,
        fallbackChain: ['claude-3-opus', 'gemini-pro', 'gpt-4'],
        autoSwitch: false,
        switchThreshold: 3,
    },
    contextMigrationEnabled: true,
    maxContextTokens: 100000,
};
/**
 * 预定义模型列表
 */
export const PREDEFINED_MODELS = [
    {
        id: 'claude-3-opus',
        name: 'Claude 3 Opus',
        provider: 'claude',
        capabilities: {
            streaming: true,
            vision: true,
            functionCalling: true,
            codeExecution: false,
            multimodal: true,
        },
        contextWindow: 200000,
        maxOutputTokens: 4096,
        available: true,
        status: 'online',
    },
    {
        id: 'claude-3-sonnet',
        name: 'Claude 3 Sonnet',
        provider: 'claude',
        capabilities: {
            streaming: true,
            vision: true,
            functionCalling: true,
            codeExecution: false,
            multimodal: true,
        },
        contextWindow: 200000,
        maxOutputTokens: 4096,
        available: true,
        status: 'online',
    },
    {
        id: 'gemini-pro',
        name: 'Gemini Pro',
        provider: 'gemini',
        capabilities: {
            streaming: true,
            vision: true,
            functionCalling: true,
            codeExecution: false,
            multimodal: true,
        },
        contextWindow: 1000000,
        maxOutputTokens: 8192,
        available: true,
        status: 'online',
    },
    {
        id: 'codex-cli',
        name: 'Codex CLI',
        provider: 'codex',
        capabilities: {
            streaming: true,
            vision: false,
            functionCalling: true,
            codeExecution: true,
            multimodal: false,
        },
        contextWindow: 128000,
        maxOutputTokens: 4096,
        available: true,
        status: 'online',
    },
];
//# sourceMappingURL=types.js.map