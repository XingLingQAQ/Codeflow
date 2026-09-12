/**
 * 模型热切换类型定义
 */
export const CLI_PROVIDER_MODEL_IDS = {
    'gemini-cli': ['gemini-2.0-flash-exp', 'gemini-2.5-pro'],
    'codex-cli': ['gpt-5.4', 'gpt-5-codex'],
};
const PREDEFINED_CLI_MODELS = [
    {
        id: 'gemini-cli',
        name: 'Gemini CLI',
        provider: 'gemini',
        capabilities: {
            streaming: true,
            vision: false,
            functionCalling: false,
            codeExecution: false,
            multimodal: false,
        },
        contextWindow: 128000,
        maxOutputTokens: 8192,
        available: false,
        status: 'offline',
        adapterKind: 'cli',
        adapterId: 'gemini-cli',
        supportedModelIds: CLI_PROVIDER_MODEL_IDS['gemini-cli'],
    },
    {
        id: 'codex-cli',
        name: 'Codex CLI',
        provider: 'codex',
        capabilities: {
            streaming: true,
            vision: false,
            functionCalling: false,
            codeExecution: false,
            multimodal: false,
        },
        contextWindow: 200000,
        maxOutputTokens: 4096,
        available: false,
        status: 'offline',
        adapterKind: 'cli',
        adapterId: 'codex-cli',
        supportedModelIds: CLI_PROVIDER_MODEL_IDS['codex-cli'],
    },
];
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
    ...PREDEFINED_CLI_MODELS,
];
//# sourceMappingURL=types.js.map