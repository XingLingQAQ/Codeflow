/**
 * Token 计数器实现
 * 支持中英文混合文本的 Token 估算
 */
import { Message } from '../hooks/types.js';
import { ITokenCounter, TokenCount } from './types.js';
export declare class TokenCounter implements ITokenCounter {
    private charsPerTokenEn;
    private charsPerTokenZh;
    private overheadPerMessage;
    constructor(config?: {
        charsPerTokenEn?: number;
        charsPerTokenZh?: number;
        overheadPerMessage?: number;
    });
    count(text: string): number;
    countMessages(messages: Message[]): TokenCount;
    estimateTokens(text: string): number;
    private isChinese;
}
//# sourceMappingURL=TokenCounter.d.ts.map