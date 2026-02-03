/**
 * SessionJournal - 会话日志持久化和上下文恢复
 * 实现会话摘要生成、存储和自动加载
 */

import { EventEmitter } from 'events';
import * as fs from 'fs/promises';
import * as path from 'path';

/**
 * 会话条目
 */
export interface SessionEntry {
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp: number;
  metadata?: Record<string, unknown>;
}

/**
 * 会话摘要
 */
export interface SessionSummary {
  id: string;
  sessionId: string;
  userId: string;
  title: string;
  summary: string;
  keyPoints: string[];
  decisions: string[];
  codeChanges: CodeChange[];
  tags: string[];
  startTime: number;
  endTime: number;
  entryCount: number;
  createdAt: number;
}

/**
 * 代码变更
 */
export interface CodeChange {
  file: string;
  type: 'created' | 'modified' | 'deleted';
  description: string;
}

/**
 * 日志索引条目
 */
export interface JournalIndexEntry {
  id: string;
  sessionId: string;
  title: string;
  summary: string;
  tags: string[];
  date: string;
  path: string;
}

/**
 * 日志索引
 */
export interface JournalIndex {
  version: string;
  userId: string;
  lastUpdated: number;
  entries: JournalIndexEntry[];
}

/**
 * 上下文恢复结果
 */
export interface ContextRestoreResult {
  summaries: SessionSummary[];
  relevantContext: string;
  suggestions: string[];
}

/**
 * Session Journal 配置
 */
export interface SessionJournalConfig {
  workspaceDir: string;
  maxJournals: number;
  maxContextLength: number;
  autoSave: boolean;
  summaryModel?: string;
}

const DEFAULT_CONFIG: SessionJournalConfig = {
  workspaceDir: '.codeflow/workspace',
  maxJournals: 100,
  maxContextLength: 8000,
  autoSave: true,
};

/**
 * 摘要生成回调
 */
export type SummaryGeneratorCallback = (entries: SessionEntry[]) => Promise<SessionSummary>;

/**
 * JournalWriter - 日志写入器
 */
export class JournalWriter extends EventEmitter {
  private config: SessionJournalConfig;

  constructor(config: Partial<SessionJournalConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 写入会话摘要
   */
  async write(summary: SessionSummary): Promise<string> {
    const userDir = path.join(this.config.workspaceDir, summary.userId);
    await this.ensureDir(userDir);

    const filename = this.generateFilename(summary);
    const filePath = path.join(userDir, filename);

    const content = this.formatSummary(summary);
    await fs.writeFile(filePath, content, 'utf-8');

    this.emit('journal:written', { path: filePath, summary });

    return filePath;
  }

  /**
   * 生成文件名
   */
  private generateFilename(summary: SessionSummary): string {
    const date = new Date(summary.startTime);
    const dateStr = date.toISOString().split('T')[0];
    const timeStr = date.toTimeString().split(' ')[0].replace(/:/g, '-');
    return `journal-${dateStr}_${timeStr}.md`;
  }

  /**
   * 格式化摘要为 Markdown
   */
  private formatSummary(summary: SessionSummary): string {
    const lines: string[] = [
      `# ${summary.title}`,
      '',
      `> Session: ${summary.sessionId}`,
      `> Date: ${new Date(summary.startTime).toISOString()}`,
      `> Duration: ${this.formatDuration(summary.endTime - summary.startTime)}`,
      `> Entries: ${summary.entryCount}`,
      '',
      '## Summary',
      '',
      summary.summary,
      '',
    ];

    if (summary.keyPoints.length > 0) {
      lines.push('## Key Points', '');
      for (const point of summary.keyPoints) {
        lines.push(`- ${point}`);
      }
      lines.push('');
    }

    if (summary.decisions.length > 0) {
      lines.push('## Decisions', '');
      for (const decision of summary.decisions) {
        lines.push(`- ${decision}`);
      }
      lines.push('');
    }

    if (summary.codeChanges.length > 0) {
      lines.push('## Code Changes', '');
      for (const change of summary.codeChanges) {
        lines.push(`- **${change.type}**: \`${change.file}\` - ${change.description}`);
      }
      lines.push('');
    }

    if (summary.tags.length > 0) {
      lines.push('## Tags', '');
      lines.push(summary.tags.map(t => `\`${t}\``).join(' '));
      lines.push('');
    }

    lines.push('---');
    lines.push(`*Generated at ${new Date(summary.createdAt).toISOString()}*`);

    return lines.join('\n');
  }

  /**
   * 格式化持续时间
   */
  private formatDuration(ms: number): string {
    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);

    if (hours > 0) {
      return `${hours}h ${minutes % 60}m`;
    } else if (minutes > 0) {
      return `${minutes}m ${seconds % 60}s`;
    } else {
      return `${seconds}s`;
    }
  }

  /**
   * 确保目录存在
   */
  private async ensureDir(dir: string): Promise<void> {
    try {
      await fs.access(dir);
    } catch {
      await fs.mkdir(dir, { recursive: true });
    }
  }
}

/**
 * JournalIndex - 日志索引管理
 */
export class JournalIndexManager extends EventEmitter {
  private config: SessionJournalConfig;
  private indices: Map<string, JournalIndex> = new Map();

  constructor(config: Partial<SessionJournalConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 加载用户索引
   */
  async loadIndex(userId: string): Promise<JournalIndex> {
    const cached = this.indices.get(userId);
    if (cached) return cached;

    const indexPath = this.getIndexPath(userId);

    try {
      const content = await fs.readFile(indexPath, 'utf-8');
      const index = this.parseIndex(content);
      this.indices.set(userId, index);
      return index;
    } catch {
      // 创建新索引
      const newIndex: JournalIndex = {
        version: '1.0',
        userId,
        lastUpdated: Date.now(),
        entries: [],
      };
      this.indices.set(userId, newIndex);
      return newIndex;
    }
  }

  /**
   * 添加条目到索引
   */
  async addEntry(userId: string, entry: JournalIndexEntry): Promise<void> {
    const index = await this.loadIndex(userId);

    // 检查是否已存在
    const existingIndex = index.entries.findIndex(e => e.id === entry.id);
    if (existingIndex >= 0) {
      index.entries[existingIndex] = entry;
    } else {
      index.entries.unshift(entry);
    }

    // 限制条目数量
    if (index.entries.length > this.config.maxJournals) {
      index.entries = index.entries.slice(0, this.config.maxJournals);
    }

    index.lastUpdated = Date.now();
    await this.saveIndex(userId, index);

    this.emit('index:updated', { userId, entry });
  }

  /**
   * 保存索引
   */
  async saveIndex(userId: string, index: JournalIndex): Promise<void> {
    const indexPath = this.getIndexPath(userId);
    const userDir = path.dirname(indexPath);

    try {
      await fs.access(userDir);
    } catch {
      await fs.mkdir(userDir, { recursive: true });
    }

    const content = this.formatIndex(index);
    await fs.writeFile(indexPath, content, 'utf-8');

    this.indices.set(userId, index);
    this.emit('index:saved', { userId, path: indexPath });
  }

  /**
   * 搜索索引
   */
  async search(userId: string, query: string): Promise<JournalIndexEntry[]> {
    const index = await this.loadIndex(userId);
    const lowerQuery = query.toLowerCase();

    return index.entries.filter(entry =>
      entry.title.toLowerCase().includes(lowerQuery) ||
      entry.summary.toLowerCase().includes(lowerQuery) ||
      entry.tags.some(t => t.toLowerCase().includes(lowerQuery))
    );
  }

  /**
   * 获取最近条目
   */
  async getRecent(userId: string, count: number = 5): Promise<JournalIndexEntry[]> {
    const index = await this.loadIndex(userId);
    return index.entries.slice(0, count);
  }

  /**
   * 获取索引路径
   */
  private getIndexPath(userId: string): string {
    return path.join(this.config.workspaceDir, userId, 'index.md');
  }

  /**
   * 解析索引文件
   */
  private parseIndex(content: string): JournalIndex {
    const lines = content.split('\n');
    const entries: JournalIndexEntry[] = [];

    let userId = 'unknown';
    let version = '1.0';
    let lastUpdated = Date.now();

    // 解析头部
    for (const line of lines) {
      if (line.startsWith('> User:')) {
        userId = line.replace('> User:', '').trim();
      } else if (line.startsWith('> Version:')) {
        version = line.replace('> Version:', '').trim();
      } else if (line.startsWith('> Updated:')) {
        lastUpdated = new Date(line.replace('> Updated:', '').trim()).getTime();
      }
    }

    // 解析条目（表格格式）
    const tableStart = lines.findIndex(l => l.startsWith('| Date'));
    if (tableStart >= 0) {
      for (let i = tableStart + 2; i < lines.length; i++) {
        const line = lines[i];
        if (!line.startsWith('|')) continue;

        const parts = line.split('|').map(p => p.trim()).filter(p => p);
        if (parts.length >= 4) {
          entries.push({
            id: parts[3] || `entry_${i}`,
            sessionId: parts[3] || '',
            title: parts[1] || '',
            summary: parts[2] || '',
            tags: [],
            date: parts[0] || '',
            path: parts[4] || '',
          });
        }
      }
    }

    return { version, userId, lastUpdated, entries };
  }

  /**
   * 格式化索引为 Markdown
   */
  private formatIndex(index: JournalIndex): string {
    const lines: string[] = [
      '# Session Journal Index',
      '',
      `> User: ${index.userId}`,
      `> Version: ${index.version}`,
      `> Updated: ${new Date(index.lastUpdated).toISOString()}`,
      `> Total: ${index.entries.length} sessions`,
      '',
      '## Sessions',
      '',
      '| Date | Title | Summary | Session ID | Path |',
      '|------|-------|---------|------------|------|',
    ];

    for (const entry of index.entries) {
      const summary = entry.summary.length > 50
        ? entry.summary.substring(0, 47) + '...'
        : entry.summary;
      lines.push(`| ${entry.date} | ${entry.title} | ${summary} | ${entry.sessionId} | ${entry.path} |`);
    }

    return lines.join('\n');
  }
}

/**
 * ContextRestorer - 上下文恢复器
 */
export class ContextRestorer extends EventEmitter {
  private config: SessionJournalConfig;
  private indexManager: JournalIndexManager;

  constructor(config: Partial<SessionJournalConfig> = {}, indexManager?: JournalIndexManager) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.indexManager = indexManager || new JournalIndexManager(config);
  }

  /**
   * 恢复上下文
   */
  async restore(userId: string, query?: string): Promise<ContextRestoreResult> {
    this.emit('restore:start', { userId, query });

    // 获取相关日志
    let entries: JournalIndexEntry[];
    if (query) {
      entries = await this.indexManager.search(userId, query);
    } else {
      entries = await this.indexManager.getRecent(userId, 5);
    }

    // 加载摘要
    const summaries: SessionSummary[] = [];
    for (const entry of entries.slice(0, 3)) {
      try {
        const summary = await this.loadSummary(userId, entry.path);
        if (summary) {
          summaries.push(summary);
        }
      } catch {
        // 忽略加载失败的摘要
      }
    }

    // 构建相关上下文
    const relevantContext = this.buildContext(summaries);

    // 生成建议
    const suggestions = this.generateSuggestions(summaries);

    const result: ContextRestoreResult = {
      summaries,
      relevantContext,
      suggestions,
    };

    this.emit('restore:complete', result);

    return result;
  }

  /**
   * 加载摘要
   */
  private async loadSummary(userId: string, relativePath: string): Promise<SessionSummary | null> {
    const fullPath = path.join(this.config.workspaceDir, userId, relativePath);

    try {
      const content = await fs.readFile(fullPath, 'utf-8');
      return this.parseSummary(content);
    } catch {
      return null;
    }
  }

  /**
   * 解析摘要文件
   */
  private parseSummary(content: string): SessionSummary {
    const lines = content.split('\n');

    let title = '';
    let sessionId = '';
    let summary = '';
    const keyPoints: string[] = [];
    const decisions: string[] = [];
    const codeChanges: CodeChange[] = [];
    const tags: string[] = [];
    let startTime = Date.now();
    let endTime = Date.now();
    let entryCount = 0;

    let currentSection = '';

    for (const line of lines) {
      if (line.startsWith('# ')) {
        title = line.substring(2).trim();
      } else if (line.startsWith('> Session:')) {
        sessionId = line.replace('> Session:', '').trim();
      } else if (line.startsWith('> Date:')) {
        startTime = new Date(line.replace('> Date:', '').trim()).getTime();
      } else if (line.startsWith('> Entries:')) {
        entryCount = parseInt(line.replace('> Entries:', '').trim()) || 0;
      } else if (line.startsWith('## ')) {
        currentSection = line.substring(3).trim().toLowerCase();
      } else if (line.startsWith('- ') && currentSection) {
        const item = line.substring(2).trim();
        switch (currentSection) {
          case 'key points':
            keyPoints.push(item);
            break;
          case 'decisions':
            decisions.push(item);
            break;
          case 'code changes':
            const match = item.match(/\*\*(\w+)\*\*: `([^`]+)` - (.+)/);
            if (match) {
              codeChanges.push({
                type: match[1] as CodeChange['type'],
                file: match[2],
                description: match[3],
              });
            }
            break;
        }
      } else if (currentSection === 'summary' && line.trim() && !line.startsWith('#')) {
        summary += (summary ? ' ' : '') + line.trim();
      } else if (currentSection === 'tags' && line.includes('`')) {
        const tagMatches = line.match(/`([^`]+)`/g);
        if (tagMatches) {
          tags.push(...tagMatches.map(t => t.replace(/`/g, '')));
        }
      }
    }

    return {
      id: `summary_${sessionId}`,
      sessionId,
      userId: '',
      title,
      summary,
      keyPoints,
      decisions,
      codeChanges,
      tags,
      startTime,
      endTime,
      entryCount,
      createdAt: Date.now(),
    };
  }

  /**
   * 构建上下文
   */
  private buildContext(summaries: SessionSummary[]): string {
    if (summaries.length === 0) {
      return '';
    }

    const lines: string[] = [
      '## Recent Session Context',
      '',
    ];

    let totalLength = 0;

    for (const summary of summaries) {
      const section = [
        `### ${summary.title}`,
        `*${new Date(summary.startTime).toLocaleDateString()}*`,
        '',
        summary.summary,
        '',
      ];

      if (summary.keyPoints.length > 0) {
        section.push('Key points:');
        for (const point of summary.keyPoints.slice(0, 3)) {
          section.push(`- ${point}`);
        }
        section.push('');
      }

      const sectionText = section.join('\n');

      if (totalLength + sectionText.length > this.config.maxContextLength) {
        break;
      }

      lines.push(sectionText);
      totalLength += sectionText.length;
    }

    return lines.join('\n');
  }

  /**
   * 生成建议
   */
  private generateSuggestions(summaries: SessionSummary[]): string[] {
    const suggestions: string[] = [];

    if (summaries.length === 0) {
      suggestions.push('No previous sessions found. Start a new conversation!');
      return suggestions;
    }

    // 基于最近会话生成建议
    const recentSummary = summaries[0];

    if (recentSummary.decisions.length > 0) {
      suggestions.push(`Continue from last decision: ${recentSummary.decisions[0]}`);
    }

    if (recentSummary.codeChanges.length > 0) {
      const lastChange = recentSummary.codeChanges[0];
      suggestions.push(`Review recent change: ${lastChange.file}`);
    }

    // 基于标签建议
    const allTags = summaries.flatMap(s => s.tags);
    const tagCounts = new Map<string, number>();
    for (const tag of allTags) {
      tagCounts.set(tag, (tagCounts.get(tag) || 0) + 1);
    }

    const topTags = Array.from(tagCounts.entries())
      .sort((a, b) => b[1] - a[1])
      .slice(0, 3)
      .map(([tag]) => tag);

    if (topTags.length > 0) {
      suggestions.push(`Frequent topics: ${topTags.join(', ')}`);
    }

    return suggestions;
  }
}

/**
 * SessionJournal - 会话日志管理器
 */
export class SessionJournal extends EventEmitter {
  private config: SessionJournalConfig;
  private writer: JournalWriter;
  private indexManager: JournalIndexManager;
  private restorer: ContextRestorer;
  private currentSession: SessionEntry[] = [];
  private currentSessionId: string = '';
  private currentUserId: string = '';
  private sessionStartTime: number = 0;
  private summaryGenerator?: SummaryGeneratorCallback;

  constructor(config: Partial<SessionJournalConfig> = {}, summaryGenerator?: SummaryGeneratorCallback) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.writer = new JournalWriter(this.config);
    this.indexManager = new JournalIndexManager(this.config);
    this.restorer = new ContextRestorer(this.config, this.indexManager);
    this.summaryGenerator = summaryGenerator;

    // 转发事件
    this.writer.on('journal:written', (data) => this.emit('journal:written', data));
    this.indexManager.on('index:updated', (data) => this.emit('index:updated', data));
    this.restorer.on('restore:complete', (data) => this.emit('restore:complete', data));
  }

  /**
   * 开始新会话
   */
  startSession(sessionId: string, userId: string): void {
    this.currentSessionId = sessionId;
    this.currentUserId = userId;
    this.currentSession = [];
    this.sessionStartTime = Date.now();

    this.emit('session:started', { sessionId, userId });
  }

  /**
   * 添加会话条目
   */
  addEntry(entry: Omit<SessionEntry, 'timestamp'>): void {
    const fullEntry: SessionEntry = {
      ...entry,
      timestamp: Date.now(),
    };

    this.currentSession.push(fullEntry);
    this.emit('entry:added', fullEntry);
  }

  /**
   * 结束会话并生成摘要
   */
  async endSession(): Promise<SessionSummary | null> {
    if (this.currentSession.length === 0) {
      this.emit('session:ended', { sessionId: this.currentSessionId, summary: null });
      return null;
    }

    const summary = await this.generateSummary();

    // 写入日志
    const journalPath = await this.writer.write(summary);

    // 更新索引
    const indexEntry: JournalIndexEntry = {
      id: summary.id,
      sessionId: summary.sessionId,
      title: summary.title,
      summary: summary.summary.substring(0, 100),
      tags: summary.tags,
      date: new Date(summary.startTime).toISOString().split('T')[0],
      path: path.basename(journalPath),
    };

    await this.indexManager.addEntry(this.currentUserId, indexEntry);

    // 清理当前会话
    this.currentSession = [];

    this.emit('session:ended', { sessionId: this.currentSessionId, summary });

    return summary;
  }

  /**
   * 生成摘要
   */
  private async generateSummary(): Promise<SessionSummary> {
    if (this.summaryGenerator) {
      return this.summaryGenerator(this.currentSession);
    }

    // 默认摘要生成
    return this.generateDefaultSummary();
  }

  /**
   * 默认摘要生成
   */
  private generateDefaultSummary(): SessionSummary {
    const userEntries = this.currentSession.filter(e => e.role === 'user');
    const assistantEntries = this.currentSession.filter(e => e.role === 'assistant');

    // 提取标题（从第一个用户消息）
    const title = userEntries[0]?.content.substring(0, 50) || 'Untitled Session';

    // 生成摘要
    const summary = `Session with ${userEntries.length} user messages and ${assistantEntries.length} assistant responses.`;

    // 提取关键点（简单实现：取用户消息的前几个）
    const keyPoints = userEntries.slice(0, 3).map(e =>
      e.content.length > 100 ? e.content.substring(0, 97) + '...' : e.content
    );

    // 提取代码变更（从助手消息中查找文件引用）
    const codeChanges: CodeChange[] = [];
    for (const entry of assistantEntries) {
      const fileMatches = entry.content.match(/`([^`]+\.(ts|js|tsx|jsx|py|go|rs))`/g);
      if (fileMatches) {
        for (const match of fileMatches.slice(0, 5)) {
          const file = match.replace(/`/g, '');
          if (!codeChanges.some(c => c.file === file)) {
            codeChanges.push({
              file,
              type: 'modified',
              description: 'Referenced in conversation',
            });
          }
        }
      }
    }

    // 提取标签
    const tags = this.extractTags(this.currentSession);

    return {
      id: `summary_${this.currentSessionId}`,
      sessionId: this.currentSessionId,
      userId: this.currentUserId,
      title,
      summary,
      keyPoints,
      decisions: [],
      codeChanges,
      tags,
      startTime: this.sessionStartTime,
      endTime: Date.now(),
      entryCount: this.currentSession.length,
      createdAt: Date.now(),
    };
  }

  /**
   * 提取标签
   */
  private extractTags(entries: SessionEntry[]): string[] {
    const tags = new Set<string>();
    const keywords = [
      'react', 'vue', 'angular', 'typescript', 'javascript', 'python',
      'api', 'database', 'test', 'bug', 'feature', 'refactor',
      'performance', 'security', 'documentation',
    ];

    for (const entry of entries) {
      const lowerContent = entry.content.toLowerCase();
      for (const keyword of keywords) {
        if (lowerContent.includes(keyword)) {
          tags.add(keyword);
        }
      }
    }

    return Array.from(tags);
  }

  /**
   * 恢复上下文
   */
  async restoreContext(userId: string, query?: string): Promise<ContextRestoreResult> {
    return this.restorer.restore(userId, query);
  }

  /**
   * 获取最近日志
   */
  async getRecentJournals(userId: string, count: number = 5): Promise<JournalIndexEntry[]> {
    return this.indexManager.getRecent(userId, count);
  }

  /**
   * 搜索日志
   */
  async searchJournals(userId: string, query: string): Promise<JournalIndexEntry[]> {
    return this.indexManager.search(userId, query);
  }

  /**
   * 获取当前会话条目
   */
  getCurrentSession(): SessionEntry[] {
    return [...this.currentSession];
  }

  /**
   * 获取当前会话 ID
   */
  getCurrentSessionId(): string {
    return this.currentSessionId;
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<SessionJournalConfig>): void {
    this.config = { ...this.config, ...config };
    this.emit('config:updated', this.config);
  }

  /**
   * 获取配置
   */
  getConfig(): SessionJournalConfig {
    return { ...this.config };
  }
}
