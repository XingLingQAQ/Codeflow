/**
 * Codex Adapter 实现
 * 基于 OpenAI API（兼容 Codex 端点）
 */
import OpenAI from 'openai';
import { APIError, TimeoutError } from './types.js';
export class CodexAdapter {
    constructor(config, hookManager) {
        this.history = [];
        this.config = {
            temperature: 0.7,
            maxTokens: 4096,
            timeout: 60000,
            maxRetries: 3,
            retryDelay: 1000,
            ...config,
            model: config.model || 'gpt-4',
        };
        if (!this.config.apiKey) {
            throw new Error('OpenAI API key is required');
        }
        this.client = new OpenAI({
            apiKey: this.config.apiKey,
            baseURL: this.config.baseURL,
            timeout: this.config.timeout,
            maxRetries: this.config.maxRetries,
        });
        this.hookManager = hookManager;
    }
    setHookManager(hookManager) {
        this.hookManager = hookManager;
    }
    getHookManager() {
        return this.hookManager;
    }
    async send(prompt, options) {
        if (options?.stream) {
            throw new Error('Use stream() for streaming responses');
        }
        const mergedOptions = { ...this.config, ...options };
        const userMessage = {
            role: 'user',
            content: prompt,
            timestamp: Date.now(),
        };
        this.history.push(userMessage);
        let payload = {
            messages: [...this.history],
            model: mergedOptions.model,
            temperature: mergedOptions.temperature,
            maxTokens: mergedOptions.maxTokens,
        };
        if (this.hookManager) {
            const processedPayload = await this.hookManager.hook_before_send(payload);
            payload = {
                messages: [...processedPayload.messages],
                model: processedPayload.model || payload.model,
                temperature: processedPayload.temperature ?? payload.temperature,
                maxTokens: processedPayload.maxTokens || payload.maxTokens,
            };
        }
        const messages = payload.messages.map((msg) => ({
            role: msg.role,
            content: msg.content,
        }));
        try {
            const completion = await this.client.chat.completions.create({
                model: payload.model,
                messages: messages,
                temperature: payload.temperature,
                max_tokens: payload.maxTokens,
            });
            const response = {
                content: completion.choices[0]?.message?.content || '',
                model: completion.model,
                usage: {
                    promptTokens: completion.usage?.prompt_tokens || 0,
                    completionTokens: completion.usage?.completion_tokens || 0,
                    totalTokens: completion.usage?.total_tokens || 0,
                },
                finishReason: completion.choices[0]?.finish_reason || 'stop',
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
            throw this.wrapError(error);
        }
    }
    async *stream(prompt, options) {
        const mergedOptions = { ...this.config, ...options };
        const userMessage = {
            role: 'user',
            content: prompt,
            timestamp: Date.now(),
        };
        this.history.push(userMessage);
        let payload = {
            messages: [...this.history],
            model: mergedOptions.model,
            temperature: mergedOptions.temperature,
            maxTokens: mergedOptions.maxTokens,
        };
        if (this.hookManager) {
            const processedPayload = await this.hookManager.hook_before_send(payload);
            payload = {
                messages: [...processedPayload.messages],
                model: processedPayload.model || payload.model,
                temperature: processedPayload.temperature ?? payload.temperature,
                maxTokens: processedPayload.maxTokens || payload.maxTokens,
            };
        }
        const messages = payload.messages.map((msg) => ({
            role: msg.role,
            content: msg.content,
        }));
        const streamGenerator = this.createStreamGenerator({
            messages,
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
        if (steps <= 0 || steps > this.history.length) {
            throw new Error('Invalid rewind steps');
        }
        this.history = this.history.slice(0, -steps);
    }
    async compact() {
        // 保留最近 10 条消息
        if (this.history.length > 10) {
            this.history = this.history.slice(-10);
        }
    }
    configure(config) {
        this.config = { ...this.config, ...config };
        // 重新创建客户端（如果 API key 或 baseURL 变更）
        if (config.apiKey || config.baseURL) {
            this.client = new OpenAI({
                apiKey: this.config.apiKey,
                baseURL: this.config.baseURL,
                timeout: this.config.timeout,
                maxRetries: this.config.maxRetries,
            });
        }
    }
    getConfig() {
        return { ...this.config };
    }
    async *createStreamGenerator(payload) {
        try {
            const stream = await this.client.chat.completions.create({
                model: payload.model,
                messages: payload.messages,
                temperature: payload.temperature,
                max_tokens: payload.maxTokens,
                stream: true,
            });
            let fullContent = '';
            let index = 0;
            for await (const chunk of stream) {
                const delta = chunk.choices[0]?.delta?.content || '';
                if (!delta) {
                    continue;
                }
                fullContent += delta;
                const streamChunk = {
                    delta,
                    index: index++,
                    done: false,
                };
                if (this.hookManager) {
                    this.hookManager.hook_on_stream(streamChunk);
                }
                yield streamChunk;
            }
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
                model: payload.model,
                usage: {
                    promptTokens: 0,
                    completionTokens: 0,
                    totalTokens: 0,
                },
                finishReason: 'stop',
            };
            if (this.hookManager) {
                await this.hookManager.hook_post_response(response);
            }
        }
        catch (error) {
            throw this.wrapError(error);
        }
    }
    wrapError(error) {
        if (error instanceof TimeoutError || error instanceof APIError) {
            return error;
        }
        const err = error;
        const message = err.message || 'Unknown error';
        const statusCode = err.status || err.statusCode;
        const retryable = statusCode === 429 || statusCode === 503 || message.includes('timeout');
        return new APIError(message, statusCode, err.code, retryable);
    }
}
//# sourceMappingURL=CodexAdapter.js.map