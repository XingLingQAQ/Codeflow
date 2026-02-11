/**
 * Gemini Adapter 实现
 * 支持多模态输入（文本、图片）
 */
import { GoogleGenerativeAI } from '@google/generative-ai';
import { APIError, TimeoutError } from './types.js';
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
            model: config.model || 'gemini-2.0-flash-exp',
        };
        if (!this.config.apiKey) {
            throw new Error('Gemini API key is required');
        }
        this.client = new GoogleGenerativeAI(this.config.apiKey);
        this.model = this.client.getGenerativeModel({ model: this.config.model });
        this.hookManager = hookManager;
    }
    async send(prompt, options) {
        const mergedOptions = { ...this.config, ...options };
        // 构建消息
        const userMessage = {
            role: 'user',
            content: typeof prompt === 'string' ? prompt : prompt.text || '',
            timestamp: Date.now(),
        };
        this.history.push(userMessage);
        // Hook: before_send
        const payload = {
            messages: [...this.history],
            model: mergedOptions.model,
            temperature: mergedOptions.temperature,
            maxTokens: mergedOptions.maxTokens,
        };
        if (this.hookManager) {
            await this.hookManager.hook_before_send(payload);
        }
        // 转换为 Gemini 格式
        const contents = this.convertToGeminiFormat(prompt);
        // 发送请求（带重试）
        let lastError = null;
        for (let attempt = 0; attempt < (mergedOptions.maxRetries || 3); attempt++) {
            try {
                const result = await this.sendWithTimeout(contents, mergedOptions);
                const response = {
                    content: result.response.text(),
                    model: mergedOptions.model,
                    usage: {
                        promptTokens: result.response.usageMetadata?.promptTokenCount || 0,
                        completionTokens: result.response.usageMetadata?.candidatesTokenCount || 0,
                        totalTokens: result.response.usageMetadata?.totalTokenCount || 0,
                    },
                    finishReason: result.response.candidates?.[0]?.finishReason || 'stop',
                };
                // 保存助手消息
                const assistantMessage = {
                    role: 'assistant',
                    content: response.content,
                    timestamp: Date.now(),
                };
                this.history.push(assistantMessage);
                // Hook: post_response
                if (this.hookManager) {
                    await this.hookManager.hook_post_response(response);
                }
                return response;
            }
            catch (error) {
                lastError = error;
                if (error instanceof TimeoutError || this.isRetryableError(error)) {
                    if (attempt < (mergedOptions.maxRetries || 3) - 1) {
                        await this.delay(mergedOptions.retryDelay || 1000);
                        continue;
                    }
                }
                throw this.wrapError(error);
            }
        }
        throw lastError || new APIError('Request failed after retries');
    }
    async *receive() {
        if (!this.currentStream) {
            throw new Error('No active stream');
        }
        yield* this.currentStream;
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
        if (config.model) {
            this.model = this.client.getGenerativeModel({ model: config.model });
        }
    }
    getConfig() {
        return { ...this.config };
    }
    // ==================== 私有方法 ====================
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
    async sendWithTimeout(contents, options
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ) {
        const timeout = options.timeout || 60000;
        return Promise.race([
            this.model.generateContent({
                contents,
                generationConfig: {
                    temperature: options.temperature,
                    maxOutputTokens: options.maxTokens,
                },
            }),
            new Promise((_, reject) => setTimeout(() => reject(new TimeoutError()), timeout)),
        ]);
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