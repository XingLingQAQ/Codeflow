/**
 * 压缩器实现
 * 80/20 压缩策略 + 决策骨架提取
 */

import { Message, Context, DecisionSkeleton, getMessageText } from '../hooks/types.js';
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

    // 实体提取模式
    const entityPatterns = [
      // 大写开头的词（类名、专有名词）
      /\b[A-Z][a-zA-Z]{2,}\b/g,
      // 驼峰命名（变量名、函数名）
      /\b[a-z]+(?:[A-Z][a-z]+)+\b/g,
      // 下划线命名
      /\b[a-z]+(?:_[a-z]+)+\b/g,
      // 文件路径
      /(?:[\w-]+\/)+[\w-]+\.\w+/g,
      // 技术术语（带连字符）
      /\b[A-Za-z]+-[A-Za-z]+(?:-[A-Za-z]+)*\b/g,
    ];

    // 决策关键词（扩展）
    const decisionKeywords = [
      // 英文
      'decide', 'decided', 'decision',
      'choose', 'chose', 'chosen', 'choice',
      'select', 'selected', 'selection',
      'implement', 'implemented', 'implementation',
      'use', 'using', 'used',
      'adopt', 'adopted', 'adoption',
      'prefer', 'preferred',
      'recommend', 'recommended',
      'should', 'must', 'will',
      'going to', 'plan to',
      // 中文
      '决定', '选择', '采用', '实现', '使用',
      '推荐', '建议', '应该', '必须', '计划',
    ];

    // 关系关键词
    const relationPatterns = [
      { pattern: /(\w+)\s+(?:extends|inherits from|is a)\s+(\w+)/gi, type: 'extends' },
      { pattern: /(\w+)\s+(?:implements|realizes)\s+(\w+)/gi, type: 'implements' },
      { pattern: /(\w+)\s+(?:uses|depends on|requires)\s+(\w+)/gi, type: 'uses' },
      { pattern: /(\w+)\s+(?:contains|has|includes)\s+(\w+)/gi, type: 'contains' },
      { pattern: /(\w+)\s+(?:calls|invokes)\s+(\w+)/gi, type: 'calls' },
      { pattern: /(\w+)\s+(?:returns|produces)\s+(\w+)/gi, type: 'returns' },
    ];

    // 排除的常见词
    const excludeWords = new Set([
      'The', 'This', 'That', 'These', 'Those', 'What', 'When', 'Where', 'Which', 'Who',
      'How', 'Why', 'Can', 'Could', 'Would', 'Should', 'Will', 'May', 'Might', 'Must',
      'Have', 'Has', 'Had', 'Been', 'Being', 'Are', 'Were', 'Was', 'Is', 'Do', 'Does',
      'Did', 'Done', 'Get', 'Got', 'Set', 'Let', 'Put', 'Make', 'Made', 'Take', 'Took',
      'Give', 'Gave', 'Come', 'Came', 'Go', 'Went', 'See', 'Saw', 'Know', 'Knew',
      'Think', 'Thought', 'Say', 'Said', 'Tell', 'Told', 'Ask', 'Asked', 'Use', 'Used',
      'Find', 'Found', 'Want', 'Need', 'Try', 'Tried', 'Keep', 'Kept', 'Start', 'Stop',
      'True', 'False', 'Null', 'Undefined', 'None', 'Yes', 'No', 'Not', 'And', 'Or',
      'But', 'For', 'With', 'From', 'Into', 'About', 'After', 'Before', 'Between',
    ]);

    for (const msg of messages) {
      const content = getMessageText(msg.content);

      // 提取实体
      for (const pattern of entityPatterns) {
        const matches = content.match(pattern);
        if (matches) {
          for (const entity of matches) {
            if (
              entity.length >= 3 &&
              !excludeWords.has(entity) &&
              !entities.includes(entity)
            ) {
              entities.push(entity);
            }
          }
        }
      }

      // 提取决策
      const sentences = content.split(/[.。!！?？\n]/);
      for (const sentence of sentences) {
        const trimmed = sentence.trim();
        if (trimmed.length < 10) continue;

        const lowerSentence = trimmed.toLowerCase();
        const hasDecisionKeyword = decisionKeywords.some((kw) => lowerSentence.includes(kw));

        if (hasDecisionKeyword && !decisions.includes(trimmed)) {
          // 提取决策的核心部分（限制长度）
          const decision = trimmed.length > 200 ? trimmed.slice(0, 200) + '...' : trimmed;
          decisions.push(decision);
        }
      }

      // 提取关系
      for (const { pattern, type } of relationPatterns) {
        let match;
        const regex = new RegExp(pattern.source, pattern.flags);
        while ((match = regex.exec(content)) !== null) {
          const from = match[1];
          const to = match[2];
          if (
            from !== to &&
            !relations.some((r) => r.from === from && r.to === to && r.type === type)
          ) {
            relations.push({ from, to, type });
          }
        }
      }
    }

    // 基于实体共现补充关系
    const entitySet = new Set(entities.slice(0, 30));
    for (const msg of messages) {
      const content = getMessageText(msg.content);
      const foundEntities = entities.filter((e) => content.includes(e));
      if (foundEntities.length >= 2) {
        for (let i = 0; i < foundEntities.length - 1; i++) {
          for (let j = i + 1; j < foundEntities.length; j++) {
            const from = foundEntities[i];
            const to = foundEntities[j];
            if (!relations.some((r) => r.from === from && r.to === to)) {
              relations.push({ from, to, type: 'related_to' });
            }
          }
        }
      }
    }

    return {
      entities: entities.slice(0, 30),
      decisions: decisions.slice(0, 15),
      relations: relations.slice(0, 25),
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
        // 按重要性排序历史消息
        const scored = historicalMessages.map((msg, idx) => ({
          msg,
          idx,
          score: this.calculateImportance(msg, idx, historicalMessages.length),
        }));

        scored.sort((a, b) => b.score - a.score);

        let currentTokens = 0;
        const selectedHistorical: Message[] = [];

        for (const { msg } of scored) {
          const msgTokens = this.tokenCounter.count(getMessageText(msg.content));
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
    const content = getMessageText(msg.content);
    const lowerContent = content.toLowerCase();

    // 1. 长度因素（较长的消息可能更重要，但有上限）
    const lengthScore = Math.min(content.length / 100, 10);
    score += lengthScore;

    // 2. 角色因素
    if (msg.role === 'assistant') {
      score += 5; // assistant 消息通常包含更多信息
    } else if (msg.role === 'system') {
      score += 10; // 系统消息最重要
    }

    // 3. 位置因素（较新的消息更重要）
    const positionScore = (index / totalMessages) * 5;
    score += positionScore;

    // 4. 关键词因素（扩展）
    const keywordScores: Record<string, number> = {
      // 高优先级关键词
      'critical': 5, 'important': 4, 'must': 4, 'required': 4,
      'error': 4, 'bug': 4, 'fix': 3, 'issue': 3,
      'security': 5, 'vulnerability': 5,
      'breaking': 5, 'deprecated': 4,
      // 中优先级关键词
      'should': 2, 'recommend': 2, 'suggest': 2,
      'warning': 3, 'caution': 3,
      'implement': 2, 'feature': 2, 'enhancement': 2,
      // 中文关键词
      '重要': 4, '关键': 4, '必须': 4, '错误': 4, '修复': 3,
      '安全': 5, '漏洞': 5, '问题': 3, '建议': 2,
    };

    for (const [keyword, keywordScore] of Object.entries(keywordScores)) {
      if (lowerContent.includes(keyword)) {
        score += keywordScore;
      }
    }

    // 5. 代码块因素（包含代码的消息可能更重要）
    const codeBlockCount = (content.match(/```/g) || []).length / 2;
    score += Math.min(codeBlockCount * 2, 6);

    // 6. 结构化内容因素（列表、标题等）
    const hasStructure =
      content.includes('- ') ||
      content.includes('* ') ||
      content.includes('1. ') ||
      content.match(/^#+\s/m);
    if (hasStructure) {
      score += 2;
    }

    // 7. 问答因素（问题和回答通常成对重要）
    if (content.includes('?') || content.includes('？')) {
      score += 1;
    }

    return score;
  }

  private generateLocalSummary(messages: Message[]): string {
    const userMessages = messages.filter((m) => m.role === 'user');
    const assistantMessages = messages.filter((m) => m.role === 'assistant');

    // 提取主题（改进：提取更有意义的内容）
    const topics: string[] = [];
    const keyPhrases: string[] = [];

    for (const msg of userMessages.slice(-5)) {
      const content = getMessageText(msg.content);
      const firstSentence = content.split(/[.。!！?？\n]/)[0]?.trim();
      if (firstSentence && firstSentence.length >= 5 && firstSentence.length <= 100) {
        topics.push(firstSentence);
      } else if (content.length > 0) {
        const topic = content.slice(0, 80).replace(/\n/g, ' ').trim();
        if (topic) topics.push(topic + (content.length > 80 ? '...' : ''));
      }
    }

    // 从 assistant 消息中提取关键短语
    for (const msg of assistantMessages.slice(-3)) {
      // 提取包含关键动作的短语
      const actionPatterns = [
        /(?:I |I'll |I've |We |We'll |We've )([^.。!！?？]{10,60})/gi,
        /(?:created|implemented|fixed|added|updated|modified|removed)\s+([^.。!！?？]{5,50})/gi,
      ];

      for (const pattern of actionPatterns) {
        const matches = getMessageText(msg.content).match(pattern);
        if (matches) {
          for (const match of matches.slice(0, 2)) {
            const phrase = match.trim();
            if (phrase.length >= 10 && !keyPhrases.includes(phrase)) {
              keyPhrases.push(phrase);
            }
          }
        }
      }
    }

    // 构建摘要
    const parts: string[] = [];

    parts.push(`Conversation summary (${messages.length} messages):`);

    if (topics.length > 0) {
      parts.push(`Topics discussed: ${topics.slice(0, 3).join('; ')}`);
    }

    if (keyPhrases.length > 0) {
      parts.push(`Key actions: ${keyPhrases.slice(0, 3).join('; ')}`);
    }

    parts.push(
      `Statistics: ${userMessages.length} user messages, ${assistantMessages.length} assistant responses.`
    );

    return parts.join(' ');
  }

  private buildSummaryPrompt(messages: Message[], config?: SummaryAgentConfig): string {
    const content = messages.map((m) => `[${m.role}]: ${getMessageText(m.content).slice(0, 500)}`).join('\n\n');

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
