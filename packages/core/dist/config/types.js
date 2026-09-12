/**
 * 将运行时 provider family 归一到声明侧 canonical provider。
 */
export function toCanonicalProvider(provider) {
    switch (provider) {
        case 'claude':
            return 'anthropic';
        case 'gemini':
            return 'google';
        case 'codex':
            return 'openai';
        default:
            return provider;
    }
}
/**
 * 将声明侧 canonical provider 映射到运行时 provider family。
 */
export function toRuntimeProviderFamily(provider) {
    switch (provider) {
        case 'anthropic':
            return 'claude';
        case 'google':
            return 'gemini';
        default:
            return provider;
    }
}
export function toAdapterConfigPatch(config) {
    const patch = {
        model: config.model,
        temperature: config.temperature,
        maxTokens: config.maxTokens,
        timeout: config.timeout,
        maxRetries: config.maxRetries,
    };
    if (config.apiChannel) {
        patch.apiKey = config.apiChannel.apiKey;
        patch.baseURL = config.apiChannel.baseURL;
    }
    return Object.fromEntries(Object.entries(patch).filter(([, value]) => value !== undefined));
}
//# sourceMappingURL=types.js.map