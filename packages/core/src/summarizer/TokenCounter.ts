/**
 * Token 计数器实现
 * 支持中英文混合文本的 Token 估算
 */

import { Message, getMessageText } from '../hooks/types.js';
import { ITokenCounter, TokenCount, TOKEN_ESTIMATION } from './types.js';

export class TokenCounter implements ITokenCounter {
  private charsPerTokenEn: number;
  private charsPerTokenZh: number;
  private overheadPerMessage: number;

  constructor(config?: {
    charsPerTokenEn?: number;
    charsPerTokenZh?: number;
    overheadPerMessage?: number;
  }) {
    this.charsPerTokenEn = config?.charsPerTokenEn ?? TOKEN_ESTIMATION.CHARS_PER_TOKEN_EN;
    this.charsPerTokenZh = config?.charsPerTokenZh ?? TOKEN_ESTIMATION.CHARS_PER_TOKEN_ZH;
    this.overheadPerMessage = config?.overheadPerMessage ?? TOKEN_ESTIMATION.OVERHEAD_PER_MESSAGE;
  }

  count(text: string): number {
    return this.estimateTokens(text);
  }

  countMessages(messages: Message[]): TokenCount {
    const byMessage: number[] = [];
    const byRole = { user: 0, assistant: 0, system: 0 };
    let total = 0;

    for (const msg of messages) {
      const tokens = this.count(getMessageText(msg.content)) + this.overheadPerMessage;
      byMessage.push(tokens);
      byRole[msg.role] += tokens;
      total += tokens;
    }

    return { total, byRole, byMessage };
  }

  estimateTokens(text: string): number {
    if (!text) return 0;

    let enChars = 0;
    let zhChars = 0;

    for (const char of text) {
      if (this.isChinese(char)) {
        zhChars++;
      } else {
        enChars++;
      }
    }

    const enTokens = Math.ceil(enChars / this.charsPerTokenEn);
    const zhTokens = Math.ceil(zhChars / this.charsPerTokenZh);

    return enTokens + zhTokens;
  }

  private isChinese(char: string): boolean {
    const code = char.charCodeAt(0);
    return (
      (code >= 0x4e00 && code <= 0x9fff) ||
      (code >= 0x3400 && code <= 0x4dbf) ||
      (code >= 0x20000 && code <= 0x2a6df) ||
      (code >= 0x2a700 && code <= 0x2b73f) ||
      (code >= 0x2b740 && code <= 0x2b81f) ||
      (code >= 0x2b820 && code <= 0x2ceaf) ||
      (code >= 0xf900 && code <= 0xfaff) ||
      (code >= 0x2f800 && code <= 0x2fa1f)
    );
  }
}
