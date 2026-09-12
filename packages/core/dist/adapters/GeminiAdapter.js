/**
 * Gemini Adapter 实现
 * 支持多模态输入（文本、图片）
 */
import { GoogleGenerativeAI } from '@google/generative-ai';
import { APIError, TimeoutError, toHookPayload, applyHookPayload, rewindHistoryByTurns, compactHistoryWithSummary, splitHistoryForGovernance, } from './types.js';
import { getMessageText } from '../hooks/types.js';
export class GeminiAdapter {
    constructor(config, hookManager) {
        this.history = [];
        this.config = {
            temperature: 0.7,
            maxTokens: 8192,
            timeout: 60000,
            maxRetries: 3,
            retryDelay: 1000,
            ...config,
            model: config.model ?? 'gemini-2.0-flash-exp',
        };
        if (!this.config.apiKey) {
            throw new Error('Gemini API key is required');
        }
        this.client = this.createClient(this.config);
        this.model = this.createModel(this.config.model);
        this.hookManager = hookManager;
    }
    setHookManager(hookManager) {
        this.hookManager = hookManager;
    }
    getHookManager() {
        return this.hookManager;
    }
    createClient(config) {
        return new GoogleGenerativeAI(config.apiKey);
    }
    createModel(modelName) {
        if (this.config.baseURL) {
            return this.client.getGenerativeModel({ model: modelName }, { baseUrl: this.config.baseURL });
        }
        return this.client.getGenerativeModel({ model: modelName });
    }
    mergeConfig(config) {
        const nextConfig = { ...this.config };
        for (const [key, value] of Object.entries(config)) {
            if (value !== undefined) {
                nextConfig[key] = value;
            }
        }
        return nextConfig;
    }
    mergeRuntimeOptions(options) {
        const mergedOptions = { ...this.config };
        for (const [key, value] of Object.entries(options ?? {})) {
            if (value !== undefined) {
                mergedOptions[key] = value;
            }
        }
        return mergedOptions;
    }
    resolveAttemptCount(maxRetries) {
        const configuredRetries = maxRetries ?? 3;
        return configuredRetries <= 0 ? 1 : configuredRetries;
    }
    buildPayloadContext(options) {
        return {
            messages: [...this.history],
            model: options?.model ?? this.config.model,
            temperature: options?.temperature ?? this.config.temperature,
            maxTokens: options?.maxTokens ?? this.config.maxTokens,
        };
    }
    async applyBeforeSendHooks(context) {
        if (!this.hookManager) {
            return context;
        }
        const processedPayload = await this.hookManager.hook_before_send(toHookPayload(context));
        return applyHookPayload(context, processedPayload);
    }
    async send(prompt, options) {
        if (options?.stream) {
            throw new Error('Use stream() for streaming responses');
        }
        const mergedOptions = this.mergeRuntimeOptions(options);
        const userMessage = {
            role: 'user',
            content: typeof prompt === 'string' ? prompt : prompt.text || '',
            timestamp: Date.now(),
        };
        this.history.push(userMessage);
        const payload = await this.applyBeforeSendHooks(this.buildPayloadContext(options));
        const request = this.buildGeminiRequest(prompt, payload.messages);
        const model = this.getModel(payload.model);
        const maxAttempts = this.resolveAttemptCount(mergedOptions.maxRetries);
        let lastError = null;
        for (let attempt = 0; attempt < maxAttempts; attempt++) {
            try {
                const result = await this.sendWithTimeout(model, request, {
                    ...mergedOptions,
                    model: payload.model,
                    temperature: payload.temperature,
                    maxTokens: payload.maxTokens,
                });
                const response = {
                    content: result.response.text(),
                    model: payload.model,
                    usage: {
                        promptTokens: result.response.usageMetadata?.promptTokenCount || 0,
                        completionTokens: result.response.usageMetadata?.candidatesTokenCount || 0,
                        totalTokens: result.response.usageMetadata?.totalTokenCount || 0,
                    },
                    finishReason: result.response.candidates?.[0]?.finishReason || 'stop',
                };
                const assistantMessage = {
                    role: 'assistant',
                    content: response.content,
                    timestamp: Date.now(),
                };
                this.history.push(assistantMessage);
                if (this.hookManager) {
                    await this.hookManager.hook_post_response(response);
                }
                return response;
            }
            catch (error) {
                lastError = error;
                if (error instanceof TimeoutError || this.isRetryableError(error)) {
                    if (attempt < maxAttempts - 1) {
                        await this.delay(mergedOptions.retryDelay ?? 1000);
                        continue;
                    }
                }
                throw this.wrapError(error);
            }
        }
        throw lastError || new APIError('Request failed after retries');
    }
    async *stream(prompt, options) {
        const mergedOptions = this.mergeRuntimeOptions(options);
        const userMessage = {
            role: 'user',
            content: typeof prompt === 'string' ? prompt : prompt.text || '',
            timestamp: Date.now(),
        };
        this.history.push(userMessage);
        const payload = await this.applyBeforeSendHooks(this.buildPayloadContext(options));
        const request = this.buildGeminiRequest(prompt, payload.messages);
        const model = this.getModel(payload.model);
        const streamGenerator = this.createStreamGenerator(model, request, {
            ...mergedOptions,
            model: payload.model,
            temperature: payload.temperature,
            maxTokens: payload.maxTokens,
        });
        this.currentStream = streamGenerator;
        try {
            yield* streamGenerator;
        }
        finally {
            this.currentStream = undefined;
        }
    }
    async *receive() {
        if (!this.currentStream) {
            throw new Error('No active stream');
        }
        try {
            yield* this.currentStream;
        }
        finally {
            this.currentStream = undefined;
        }
    }
    getHistory() {
        return [...this.history];
    }
    setHistory(messages) {
        this.history = [...messages];
    }
    async rewind(steps) {
        this.history = rewindHistoryByTurns(this.history, steps);
    }
    async compact() {
        this.history = await compactHistoryWithSummary(this.history, {
            buildSkeleton: this.hookManager
                ? async (messages, tokenCount) => this.hookManager.hook_before_compress({
                    messages,
                    tokenCount,
                })
                : undefined,
        });
    }
    configure(config) {
        this.config = this.mergeConfig(config);
        const shouldRecreateClient = config.apiKey !== undefined;
        const shouldRecreateModel = shouldRecreateClient ||
            config.model !== undefined ||
            config.baseURL !== undefined;
        if (shouldRecreateClient) {
            this.client = this.createClient(this.config);
        }
        if (shouldRecreateModel) {
            this.model = this.createModel(this.config.model);
        }
    }
    getConfig() {
        return { ...this.config };
    }
    resolvePromptFromPayload(originalPrompt, messages) {
        const latestUserMessage = [...messages].reverse().find((message) => message.role === 'user');
        const text = latestUserMessage ? getMessageText(latestUserMessage.content) : (typeof originalPrompt === 'string' ? originalPrompt : originalPrompt.text || '');
        if (typeof originalPrompt === 'string') {
            return text;
        }
        return {
            ...originalPrompt,
            text,
        };
    }
    convertToGeminiFormat(prompt) {
        if (typeof prompt === 'string') {
            return [{ role: 'user', parts: [{ text: prompt }] }];
        }
        const parts = [];
        if (prompt.text) {
            parts.push({ text: prompt.text });
        }
        if (prompt.images) {
            for (const image of prompt.images) {
                parts.push({
                    inlineData: {
                        data: image.data,
                        mimeType: image.mimeType,
                    },
                });
            }
        }
        return [{ role: 'user', parts }];
    }
    getModel(modelName) {
        if (modelName && modelName !== this.config.model) {
            return this.createModel(modelName);
        }
        return this.model;
    }
    buildGeminiRequest(originalPrompt, messages) {
        const { systemMessages, dialogueMessages } = splitHistoryForGovernance(messages);
        const latestUserIndex = [...dialogueMessages].map((message, index) => ({ message, index })).reverse().find(({ message }) => message.role === 'user')?.index;
        const latestPrompt = this.resolvePromptFromPayload(originalPrompt, messages);
        const contents = [];
        for (const [index, message] of dialogueMessages.entries()) {
            if (message.role === 'assistant') {
                contents.push({ role: 'model', parts: [{ text: getMessageText(message.content) }] });
                continue;
            }
            if (index === latestUserIndex) {
                const prompt = typeof latestPrompt === 'string' ? latestPrompt : { ...latestPrompt, text: getMessageText(message.content) };
                contents.push(...this.convertToGeminiFormat(prompt));
                continue;
            }
            contents.push({ role: 'user', parts: [{ text: getMessageText(message.content) }] });
        }
        const systemInstruction = systemMessages.length > 0
            ? systemMessages.map((message) => getMessageText(message.content)).join('\n\n')
            : undefined;
        return { contents, systemInstruction };
    }
    async sendWithTimeout(model, request, options) {
        const timeout = options.timeout ?? 60000;
        return Promise.race([
            model.generateContent({
                contents: request.contents,
                systemInstruction: request.systemInstruction,
                generationConfig: {
                    temperature: options.temperature,
                    maxOutputTokens: options.maxTokens,
                },
            }),
            new Promise((_, reject) => setTimeout(() => reject(new TimeoutError()), timeout)),
        ]);
    }
    async *createStreamGenerator(model, request, options) {
        const timeout = options.timeout ?? 60000;
        try {
            const streamResult = await Promise.race([
                model.generateContentStream({
                    contents: request.contents,
                    systemInstruction: request.systemInstruction,
                    generationConfig: {
                        temperature: options.temperature,
                        maxOutputTokens: options.maxTokens,
                    },
                }),
                new Promise((_, reject) => setTimeout(() => reject(new TimeoutError()), timeout)),
            ]);
            let fullContent = '';
            let index = 0;
            for await (const chunkResponse of streamResult.stream) {
                const delta = chunkResponse.text();
                if (!delta) {
                    continue;
                }
                fullContent += delta;
                const chunk = {
                    delta,
                    index: index++,
                    done: false,
                };
                if (this.hookManager) {
                    this.hookManager.hook_on_stream(chunk);
                }
                yield chunk;
            }
            const finalResponse = await Promise.race([
                streamResult.response,
                new Promise((_, reject) => setTimeout(() => reject(new TimeoutError()), timeout)),
            ]);
            const finalChunk = {
                delta: '',
                index,
                done: true,
            };
            if (this.hookManager) {
                this.hookManager.hook_on_stream(finalChunk);
            }
            yield finalChunk;
            const assistantMessage = {
                role: 'assistant',
                content: fullContent,
                timestamp: Date.now(),
            };
            this.history.push(assistantMessage);
            const response = {
                content: fullContent,
                model: options.model,
                usage: {
                    promptTokens: finalResponse.usageMetadata?.promptTokenCount || 0,
                    completionTokens: finalResponse.usageMetadata?.candidatesTokenCount || 0,
                    totalTokens: finalResponse.usageMetadata?.totalTokenCount || 0,
                },
                finishReason: finalResponse.candidates?.[0]?.finishReason || 'stop',
            };
            if (this.hookManager) {
                await this.hookManager.hook_post_response(response);
            }
        }
        catch (error) {
            throw this.wrapError(error);
        }
    }
    isRetryableError(error) {
        if (error instanceof APIError) {
            return error.retryable;
        }
        const message = error.message?.toLowerCase() || '';
        return (message.includes('rate limit') ||
            message.includes('timeout') ||
            message.includes('503') ||
            message.includes('429'));
    }
    wrapError(error) {
        if (error instanceof TimeoutError || error instanceof APIError) {
            return error;
        }
        const err = error;
        const message = err.message || 'Unknown error';
        const statusCode = err.status || err.statusCode;
        return new APIError(message, statusCode, err.code, this.isRetryableError(error));
    }
    delay(ms) {
        return new Promise((resolve) => setTimeout(resolve, ms));
    }
}
//# sourceMappingURL=GeminiAdapter.js.map