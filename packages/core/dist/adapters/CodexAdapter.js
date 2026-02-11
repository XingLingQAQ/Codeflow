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
    async send(prompt, options) {
        const mergedOptions = { ...this.config, ...options };
        // 构建消息
        const userMessage = {
            role: 'user',
            content: prompt,
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
        // 转换为 OpenAI 格式
        const messages = this.history.map((msg) => ({
            role: msg.role,
            content: msg.content,
        }));
        try {
            if (mergedOptions.stream) {
                // 流式响应
                return await this.handleStream(messages, mergedOptions);
            }
            else {
                // 非流式响应
                const completion = await this.client.chat.completions.create({
                    model: mergedOptions.model,
                    messages: messages,
                    temperature: mergedOptions.temperature,
                    max_tokens: mergedOptions.maxTokens,
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
        }
        catch (error) {
            throw this.wrapError(error);
        }
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
    // ==================== 私有方法 ====================
    async handleStream(messages, options) {
        const stream = await this.client.chat.completions.create({
            model: options.model,
            messages: messages,
            temperature: options.temperature,
            max_tokens: options.maxTokens,
            stream: true,
        });
        let fullContent = '';
        let index = 0;
        this.currentStream = async function* () {
            for await (const chunk of stream) {
                const delta = chunk.choices[0]?.delta?.content || '';
                if (delta) {
                    fullContent += delta;
                    const streamChunk = {
                        delta,
                        index: index++,
                        done: false,
                    };
                    // Hook: on_stream
                    if (this.hookManager) {
                        this.hookManager.hook_on_stream(streamChunk);
                    }
                    yield streamChunk;
                }
            }
            // 最后一个 chunk
            yield {
                delta: '',
                index: index++,
                done: true,
            };
        }.bind(this)();
        // 等待流完成
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        for await (const _ of this.currentStream) {
            // 消费流
        }
        // 保存助手消息
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
                promptTokens: 0,
                completionTokens: 0,
                totalTokens: 0,
            },
            finishReason: 'stop',
        };
        // Hook: post_response
        if (this.hookManager) {
            await this.hookManager.hook_post_response(response);
        }
        return response;
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