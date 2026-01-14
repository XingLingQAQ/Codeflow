/**
 * 压缩器实现
 * 80/20 压缩策略 + 决策骨架提取
 */

import { Message, Context, DecisionSkeleton } from '../hooks/types.js';
import { ICliAdapter } from '../adapters/types.js';
import { HookManager } from '../hooks/HookManager.js';
import {
  ICompressor,
  CompressionConfig,
  CompressionResult,
  SummaryAgentConfig,
  DEFAULT_COMPRESSION_CONFIG,
} from './types.js';
import { TokenCounter } from './TokenCounter.js';

export class Compressor implements ICompressor {
  private tokenCounter: TokenCounter;
  private summaryAdapter?: ICliAdapter;
  private hookManager?: HookManager;
  private config: CompressionConfig;

  constructor(
    summaryAdapter?: ICliAdapter,
    hookManager?: HookManager,
    config?: Partial<CompressionConfig>
  ) {
    this.tokenCounter = new TokenCounter();
    this.summaryAdapter = summaryAdapter;
    this.hookManager = hookManager;
    this.config = { ...DEFAULT_COMPRESSION_CONFIG, ...config };
  }

  async compress(
    context: Context,
    config?: Partial<CompressionConfig>
  ): Promise<CompressionResult> {
    const mergedConfig = { ...this.config, ...config };
    const { messages, tokenCount: originalTokens } = context;

    // 触发 hook_before_compress
    let decisionSkeleton: DecisionSkeleton | undefined;
    if (this.hookManager && mergedConfig.extractDecisionSkeleton) {
      decisionSkeleton = await this.hookManager.hook_before_compress(context);
    } else if (mergedConfig.extractDecisionSkeleton) {
      decisionSkeleton = await this.extractSkeleton(messages);
    }

    // 80/20 压缩策略
    const preservedMessages = this.applyCompressionStrategy(messages, mergedConfig);

    // 生成摘要（如果有 Summary Agent）
    let summary: string | undefined;
    if (this.summaryAdapter && messages.length > preservedMessages.length) {
      const messagesToSummarize = messages.slice(
        0,
        messages.length - mergedConfig.preserveRecentMessages
      );
      summary = await this.generateSummary(messagesToSummarize);
    }

    // 计算压缩后的 token 数
    const compressedTokens = this.tokenCounter.countMessages(preservedMessages).total;

    return {
      originalTokens,
      compressedTokens,
      compressionRatio: compressedTokens / originalTokens,
      preservedMessages,
      summary,
      decisionSkeleton,
    };
  }

  async extractSkeleton(messages: Message[]): Promise<DecisionSkeleton> {
    const entities: string[] = [];
    const decisions: string[] = [];
    const relations: Array<{ from: string; to: string; type: string }> = [];

    // 简单的实体和决策提取
    for (const msg of messages) {
      const content = msg.content;

      // 提取实体（简化：提取大写开头的词）
      const entityMatches = content.match(/\b[A-Z][a-zA-Z]+\b/g);
      if (entityMatches) {
        for (const entity of entityMatches) {
          if (!entities.includes(entity)) {
            entities.push(entity);
          }
        }
      }

      // 提取决策（简化：提取包含决策关键词的句子）
      const decisionKeywords = [
        'decide',
        'choose',
        'select',
        'implement',
        'use',
        'adopt',
        '决定',
        '选择',
        '采用',
        '实现',
      ];
      const sentences = content.split(/[.。!！?？]/);
      for (const sentence of sentences) {
        const lowerSentence = sentence.toLowerCase();
        if (decisionKeywords.some((kw) => lowerSentence.includes(kw))) {
          const trimmed = sentence.trim();
          if (trimmed && !decisions.includes(trimmed)) {
            decisions.push(trimmed);
          }
        }
      }
    }

    // 提取关系（简化：基于实体共现）
    for (let i = 0; i < entities.length - 1; i++) {
      for (let j = i + 1; j < entities.length; j++) {
        const entity1 = entities[i];
        const entity2 = entities[j];

        // 检查是否在同一消息中出现
        for (const msg of messages) {
          if (msg.content.includes(entity1) && msg.content.includes(entity2)) {
            relations.push({
              from: entity1,
              to: entity2,
              type: 'related_to',
            });
            break;
          }
        }
      }
    }

    return {
      entities: entities.slice(0, 20),
      decisions: decisions.slice(0, 10),
      relations: relations.slice(0, 15),
    };
  }

  async generateSummary(messages: Message[], config?: SummaryAgentConfig): Promise<string> {
    if (!this.summaryAdapter) {
      return this.generateLocalSummary(messages);
    }

    const prompt = this.buildSummaryPrompt(messages, config);

    try {
      const response = await this.summaryAdapter.send(prompt, {
        maxTokens: config?.maxSummaryTokens ?? 500,
      });
      return response.content;
    } catch {
      return this.generateLocalSummary(messages);
    }
  }

  // ==================== 私有方法 ====================

  private applyCompressionStrategy(messages: Message[], config: CompressionConfig): Message[] {
    const preserved: Message[] = [];
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    const tokenCount = this.tokenCounter.countMessages(messages);

    // 1. 保留系统提示
    if (config.preserveSystemPrompt) {
      const systemMessages = messages.filter((m) => m.role === 'system');
      preserved.push(...systemMessages);
    }

    // 2. 保留最近的消息
    const recentMessages = messages
      .filter((m) => m.role !== 'system')
      .slice(-config.preserveRecentMessages);
    preserved.push(...recentMessages);

    // 3. 80/20 策略：保留 20% 最重要的历史消息
    const historicalMessages = messages
      .filter((m) => m.role !== 'system')
      .slice(0, -config.preserveRecentMessages);

    if (historicalMessages.length > 0) {
      const targetHistoricalTokens = Math.floor(
        config.maxTokens * config.targetRatio - this.tokenCounter.countMessages(preserved).total
      );

      if (targetHistoricalTokens > 0) {
        // 按重要性排序（简化：优先保留较长的消息和 assistant 消息）
        const scored = historicalMessages.map((msg, idx) => ({
          msg,
          idx,
          score: this.calculateImportance(msg, idx, historicalMessages.length),
        }));

        scored.sort((a, b) => b.score - a.score);

        let currentTokens = 0;
        const selectedHistorical: Message[] = [];

        for (const { msg } of scored) {
          const msgTokens = this.tokenCounter.count(msg.content);
          if (currentTokens + msgTokens <= targetHistoricalTokens) {
            selectedHistorical.push(msg);
            currentTokens += msgTokens;
          }
        }

        // 按原始顺序排列
        selectedHistorical.sort(
          (a, b) => historicalMessages.indexOf(a) - historicalMessages.indexOf(b)
        );

        // 插入到系统消息之后、最近消息之前
        const systemCount = preserved.filter((m) => m.role === 'system').length;
        preserved.splice(systemCount, 0, ...selectedHistorical);
      }
    }

    return preserved;
  }

  private calculateImportance(msg: Message, index: number, totalMessages: number): number {
    let score = 0;

    // 长度因素（较长的消息可能更重要）
    score += Math.min(msg.content.length / 100, 10);

    // 角色因素（assistant 消息通常包含更多信息）
    if (msg.role === 'assistant') {
      score += 5;
    }

    // 位置因素（较新的消息更重要）
    score += (index / totalMessages) * 3;

    // 关键词因素
    const importantKeywords = [
      'important',
      'critical',
      'must',
      'should',
      'error',
      'bug',
      'fix',
      '重要',
      '关键',
      '必须',
      '错误',
      '修复',
    ];
    for (const kw of importantKeywords) {
      if (msg.content.toLowerCase().includes(kw)) {
        score += 2;
      }
    }

    return score;
  }

  private generateLocalSummary(messages: Message[]): string {
    const userMessages = messages.filter((m) => m.role === 'user');
    const assistantMessages = messages.filter((m) => m.role === 'assistant');

    const topics: string[] = [];

    // 提取主题（简化：取每条消息的前 50 个字符）
    for (const msg of userMessages.slice(-5)) {
      const topic = msg.content.slice(0, 50).replace(/\n/g, ' ');
      topics.push(topic);
    }

    return `Previous conversation covered ${messages.length} messages. User topics: ${topics.join('; ')}. Assistant provided ${assistantMessages.length} responses.`;
  }

  private buildSummaryPrompt(messages: Message[], config?: SummaryAgentConfig): string {
    const content = messages.map((m) => `[${m.role}]: ${m.content.slice(0, 500)}`).join('\n\n');

    let prompt = `Summarize the following conversation concisely:\n\n${content}\n\n`;

    if (config?.includeEntities) {
      prompt += 'Include key entities mentioned.\n';
    }
    if (config?.includeDecisions) {
      prompt += 'Include important decisions made.\n';
    }
    if (config?.includeRelations) {
      prompt += 'Include relationships between concepts.\n';
    }

    prompt += '\nProvide a concise summary:';

    return prompt;
  }
}
