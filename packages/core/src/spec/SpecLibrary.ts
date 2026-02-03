/**
 * SpecLibrary - 分层规范库
 * 实现 Trellis 风格的分层规范库，支持 .codeflow/spec/ 目录结构
 */

import { EventEmitter } from 'events';
import * as fs from 'fs';
import * as path from 'path';

/**
 * 规范领域
 */
export type SpecDomain = 'frontend' | 'backend' | 'guides' | 'common' | 'custom';

/**
 * 规范类型
 */
export type SpecType = 'rule' | 'pattern' | 'template' | 'checklist' | 'reference';

/**
 * 规范优先级
 */
export type SpecPriority = 'critical' | 'high' | 'medium' | 'low';

/**
 * 规范元数据
 */
export interface SpecMetadata {
  id: string;
  name: string;
  domain: SpecDomain;
  type: SpecType;
  priority: SpecPriority;
  tags: string[];
  version: string;
  author?: string;
  description?: string;
  createdAt: number;
  updatedAt: number;
}

/**
 * 规范文档
 */
export interface SpecDocument {
  metadata: SpecMetadata;
  content: string;
  path: string;
  checksum?: string;
}

/**
 * 规范验证结果
 */
export interface SpecValidationResult {
  valid: boolean;
  errors: SpecValidationError[];
  warnings: SpecValidationWarning[];
}

/**
 * 验证错误
 */
export interface SpecValidationError {
  field: string;
  message: string;
  line?: number;
}

/**
 * 验证警告
 */
export interface SpecValidationWarning {
  field: string;
  message: string;
  suggestion?: string;
}

/**
 * 规范库配置
 */
export interface SpecLibraryConfig {
  rootDir: string;
  domains: SpecDomain[];
  autoReload: boolean;
  validateOnLoad: boolean;
  cacheSpecs: boolean;
}

const DEFAULT_CONFIG: SpecLibraryConfig = {
  rootDir: '.codeflow/spec',
  domains: ['frontend', 'backend', 'guides', 'common'],
  autoReload: true,
  validateOnLoad: true,
  cacheSpecs: true,
};

/**
 * 默认目录结构
 */
const DEFAULT_STRUCTURE: Record<SpecDomain, string[]> = {
  frontend: ['components', 'styles', 'state', 'testing'],
  backend: ['api', 'database', 'security', 'performance'],
  guides: ['coding-style', 'git-workflow', 'review-checklist'],
  common: ['naming', 'documentation', 'error-handling'],
  custom: [],
};

/**
 * SpecLoader - 规范加载器
 */
export class SpecLoader extends EventEmitter {
  private config: SpecLibraryConfig;
  private cache: Map<string, SpecDocument> = new Map();
  private watchers: Map<string, fs.FSWatcher> = new Map();

  constructor(config: Partial<SpecLibraryConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 加载单个规范
   */
  async load(specPath: string): Promise<SpecDocument | null> {
    const fullPath = path.isAbsolute(specPath)
      ? specPath
      : path.join(this.config.rootDir, specPath);

    // 检查缓存
    if (this.config.cacheSpecs && this.cache.has(fullPath)) {
      return this.cache.get(fullPath)!;
    }

    if (!fs.existsSync(fullPath)) {
      this.emit('load:error', { path: fullPath, error: 'File not found' });
      return null;
    }

    try {
      const content = fs.readFileSync(fullPath, 'utf-8');
      const metadata = this.parseMetadata(content, fullPath);
      const checksum = this.calculateChecksum(content);

      const doc: SpecDocument = {
        metadata,
        content: this.extractContent(content),
        path: fullPath,
        checksum,
      };

      if (this.config.cacheSpecs) {
        this.cache.set(fullPath, doc);
      }

      this.emit('load:success', doc);
      return doc;
    } catch (error) {
      this.emit('load:error', { path: fullPath, error: (error as Error).message });
      return null;
    }
  }

  /**
   * 加载目录下所有规范
   */
  async loadDirectory(dirPath: string): Promise<SpecDocument[]> {
    const fullPath = path.isAbsolute(dirPath)
      ? dirPath
      : path.join(this.config.rootDir, dirPath);

    if (!fs.existsSync(fullPath)) {
      return [];
    }

    const docs: SpecDocument[] = [];
    const files = this.findSpecFiles(fullPath);

    for (const file of files) {
      const doc = await this.load(file);
      if (doc) {
        docs.push(doc);
      }
    }

    return docs;
  }

  /**
   * 加载指定领域的所有规范
   */
  async loadDomain(domain: SpecDomain): Promise<SpecDocument[]> {
    const domainPath = path.join(this.config.rootDir, domain);
    return this.loadDirectory(domainPath);
  }

  /**
   * 加载所有规范
   */
  async loadAll(): Promise<SpecDocument[]> {
    const allDocs: SpecDocument[] = [];

    for (const domain of this.config.domains) {
      const docs = await this.loadDomain(domain);
      allDocs.push(...docs);
    }

    return allDocs;
  }

  /**
   * 查找规范文件
   */
  private findSpecFiles(dir: string): string[] {
    const files: string[] = [];

    if (!fs.existsSync(dir)) return files;

    const entries = fs.readdirSync(dir, { withFileTypes: true });

    for (const entry of entries) {
      const fullPath = path.join(dir, entry.name);

      if (entry.isDirectory()) {
        files.push(...this.findSpecFiles(fullPath));
      } else if (entry.isFile() && entry.name.endsWith('.md')) {
        files.push(fullPath);
      }
    }

    return files;
  }

  /**
   * 解析元数据
   */
  private parseMetadata(content: string, filePath: string): SpecMetadata {
    const frontmatterMatch = content.match(/^---\n([\s\S]*?)\n---/);
    const fileName = path.basename(filePath, '.md');
    const dirName = path.basename(path.dirname(filePath));

    const defaultMetadata: SpecMetadata = {
      id: `spec-${fileName}-${Date.now()}`,
      name: fileName,
      domain: this.inferDomain(filePath),
      type: 'rule',
      priority: 'medium',
      tags: [],
      version: '1.0.0',
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };

    if (!frontmatterMatch) {
      return defaultMetadata;
    }

    const frontmatter = frontmatterMatch[1];
    const parsed: Partial<SpecMetadata> = {};

    // 解析 YAML-like frontmatter
    const lines = frontmatter.split('\n');
    for (const line of lines) {
      const match = line.match(/^(\w+):\s*(.+)$/);
      if (match) {
        const [, key, value] = match;
        switch (key) {
          case 'id':
            parsed.id = value;
            break;
          case 'name':
            parsed.name = value;
            break;
          case 'domain':
            parsed.domain = value as SpecDomain;
            break;
          case 'type':
            parsed.type = value as SpecType;
            break;
          case 'priority':
            parsed.priority = value as SpecPriority;
            break;
          case 'tags':
            parsed.tags = value.split(',').map(t => t.trim());
            break;
          case 'version':
            parsed.version = value;
            break;
          case 'author':
            parsed.author = value;
            break;
          case 'description':
            parsed.description = value;
            break;
        }
      }
    }

    return { ...defaultMetadata, ...parsed };
  }

  /**
   * 提取内容（去除 frontmatter）
   */
  private extractContent(content: string): string {
    return content.replace(/^---\n[\s\S]*?\n---\n*/, '').trim();
  }

  /**
   * 推断领域
   */
  private inferDomain(filePath: string): SpecDomain {
    const relativePath = path.relative(this.config.rootDir, filePath);
    const parts = relativePath.split(path.sep);

    if (parts.length > 0) {
      const domain = parts[0] as SpecDomain;
      if (this.config.domains.includes(domain)) {
        return domain;
      }
    }

    return 'common';
  }

  /**
   * 计算校验和
   */
  private calculateChecksum(content: string): string {
    let hash = 0;
    for (let i = 0; i < content.length; i++) {
      const char = content.charCodeAt(i);
      hash = ((hash << 5) - hash) + char;
      hash = hash & hash;
    }
    return Math.abs(hash).toString(16);
  }

  /**
   * 启用热加载
   */
  enableHotReload(): void {
    if (!this.config.autoReload) return;

    for (const domain of this.config.domains) {
      const domainPath = path.join(this.config.rootDir, domain);
      if (fs.existsSync(domainPath)) {
        this.watchDirectory(domainPath);
      }
    }
  }

  /**
   * 监听目录变化
   */
  private watchDirectory(dir: string): void {
    if (this.watchers.has(dir)) return;

    try {
      const watcher = fs.watch(dir, { recursive: true }, async (eventType, filename) => {
        if (filename && filename.endsWith('.md')) {
          const fullPath = path.join(dir, filename);
          this.cache.delete(fullPath);
          const doc = await this.load(fullPath);
          if (doc) {
            this.emit('spec:changed', doc);
          }
        }
      });

      this.watchers.set(dir, watcher);
    } catch {
      // Watch not supported on this platform
    }
  }

  /**
   * 停止热加载
   */
  disableHotReload(): void {
    for (const watcher of this.watchers.values()) {
      watcher.close();
    }
    this.watchers.clear();
  }

  /**
   * 清除缓存
   */
  clearCache(): void {
    this.cache.clear();
    this.emit('cache:cleared');
  }

  /**
   * 获取缓存的规范
   */
  getCachedSpecs(): SpecDocument[] {
    return Array.from(this.cache.values());
  }
}

/**
 * SpecValidator - 规范验证器
 */
export class SpecValidator extends EventEmitter {
  /**
   * 验证规范文档
   */
  validate(doc: SpecDocument): SpecValidationResult {
    const errors: SpecValidationError[] = [];
    const warnings: SpecValidationWarning[] = [];

    // 验证元数据
    this.validateMetadata(doc.metadata, errors, warnings);

    // 验证内容
    this.validateContent(doc.content, errors, warnings);

    const result: SpecValidationResult = {
      valid: errors.length === 0,
      errors,
      warnings,
    };

    this.emit('validation:complete', { doc, result });
    return result;
  }

  /**
   * 批量验证
   */
  validateBatch(docs: SpecDocument[]): Map<string, SpecValidationResult> {
    const results = new Map<string, SpecValidationResult>();

    for (const doc of docs) {
      results.set(doc.metadata.id, this.validate(doc));
    }

    return results;
  }

  private validateMetadata(
    metadata: SpecMetadata,
    errors: SpecValidationError[],
    warnings: SpecValidationWarning[]
  ): void {
    // 必填字段
    if (!metadata.id) {
      errors.push({ field: 'id', message: 'ID is required' });
    }

    if (!metadata.name) {
      errors.push({ field: 'name', message: 'Name is required' });
    }

    // 版本格式
    if (metadata.version && !/^\d+\.\d+\.\d+$/.test(metadata.version)) {
      warnings.push({
        field: 'version',
        message: 'Version should follow semver format',
        suggestion: 'Use format: X.Y.Z',
      });
    }

    // 标签建议
    if (metadata.tags.length === 0) {
      warnings.push({
        field: 'tags',
        message: 'No tags specified',
        suggestion: 'Add tags for better discoverability',
      });
    }
  }

  private validateContent(
    content: string,
    errors: SpecValidationError[],
    warnings: SpecValidationWarning[]
  ): void {
    // 内容不能为空
    if (!content.trim()) {
      errors.push({ field: 'content', message: 'Content cannot be empty' });
      return;
    }

    // 检查标题
    if (!content.startsWith('#')) {
      warnings.push({
        field: 'content',
        message: 'Content should start with a heading',
        suggestion: 'Add a # heading at the beginning',
      });
    }

    // 检查最小长度
    if (content.length < 50) {
      warnings.push({
        field: 'content',
        message: 'Content is very short',
        suggestion: 'Consider adding more details',
      });
    }
  }
}

/**
 * SpecLibrary - 规范库管理器
 */
export class SpecLibrary extends EventEmitter {
  private config: SpecLibraryConfig;
  private loader: SpecLoader;
  private validator: SpecValidator;
  private specs: Map<string, SpecDocument> = new Map();
  private initialized: boolean = false;

  constructor(config: Partial<SpecLibraryConfig> = {}) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.loader = new SpecLoader(this.config);
    this.validator = new SpecValidator();

    // 转发事件
    this.loader.on('load:success', (doc) => this.emit('spec:loaded', doc));
    this.loader.on('load:error', (err) => this.emit('spec:error', err));
    this.loader.on('spec:changed', (doc) => {
      this.specs.set(doc.metadata.id, doc);
      this.emit('spec:changed', doc);
    });
  }

  /**
   * 初始化规范库
   */
  async initialize(): Promise<void> {
    if (this.initialized) return;

    // 确保目录结构存在
    await this.ensureDirectoryStructure();

    // 加载所有规范
    const docs = await this.loader.loadAll();

    // 验证并存储
    for (const doc of docs) {
      if (this.config.validateOnLoad) {
        const result = this.validator.validate(doc);
        if (!result.valid) {
          this.emit('spec:invalid', { doc, result });
          continue;
        }
      }
      this.specs.set(doc.metadata.id, doc);
    }

    // 启用热加载
    if (this.config.autoReload) {
      this.loader.enableHotReload();
    }

    this.initialized = true;
    this.emit('library:initialized', { count: this.specs.size });
  }

  /**
   * 确保目录结构存在
   */
  private async ensureDirectoryStructure(): Promise<void> {
    if (!fs.existsSync(this.config.rootDir)) {
      fs.mkdirSync(this.config.rootDir, { recursive: true });
    }

    for (const domain of this.config.domains) {
      const domainPath = path.join(this.config.rootDir, domain);
      if (!fs.existsSync(domainPath)) {
        fs.mkdirSync(domainPath, { recursive: true });
      }

      // 创建子目录
      const subDirs = DEFAULT_STRUCTURE[domain] || [];
      for (const subDir of subDirs) {
        const subPath = path.join(domainPath, subDir);
        if (!fs.existsSync(subPath)) {
          fs.mkdirSync(subPath, { recursive: true });
        }
      }
    }
  }

  /**
   * 获取规范
   */
  getSpec(id: string): SpecDocument | undefined {
    return this.specs.get(id);
  }

  /**
   * 获取所有规范
   */
  getAllSpecs(): SpecDocument[] {
    return Array.from(this.specs.values());
  }

  /**
   * 按领域获取规范
   */
  getSpecsByDomain(domain: SpecDomain): SpecDocument[] {
    return this.getAllSpecs().filter(s => s.metadata.domain === domain);
  }

  /**
   * 按类型获取规范
   */
  getSpecsByType(type: SpecType): SpecDocument[] {
    return this.getAllSpecs().filter(s => s.metadata.type === type);
  }

  /**
   * 按标签获取规范
   */
  getSpecsByTag(tag: string): SpecDocument[] {
    return this.getAllSpecs().filter(s => s.metadata.tags.includes(tag));
  }

  /**
   * 按优先级获取规范
   */
  getSpecsByPriority(priority: SpecPriority): SpecDocument[] {
    return this.getAllSpecs().filter(s => s.metadata.priority === priority);
  }

  /**
   * 搜索规范
   */
  searchSpecs(query: string): SpecDocument[] {
    const lowerQuery = query.toLowerCase();
    return this.getAllSpecs().filter(s =>
      s.metadata.name.toLowerCase().includes(lowerQuery) ||
      s.metadata.description?.toLowerCase().includes(lowerQuery) ||
      s.content.toLowerCase().includes(lowerQuery) ||
      s.metadata.tags.some(t => t.toLowerCase().includes(lowerQuery))
    );
  }

  /**
   * 添加规范
   */
  async addSpec(
    domain: SpecDomain,
    name: string,
    content: string,
    metadata?: Partial<SpecMetadata>
  ): Promise<SpecDocument> {
    const specPath = path.join(this.config.rootDir, domain, `${name}.md`);

    // 构建完整内容
    const fullMetadata: SpecMetadata = {
      id: `spec-${name}-${Date.now()}`,
      name,
      domain,
      type: 'rule',
      priority: 'medium',
      tags: [],
      version: '1.0.0',
      createdAt: Date.now(),
      updatedAt: Date.now(),
      ...metadata,
    };

    const frontmatter = this.generateFrontmatter(fullMetadata);
    const fullContent = `${frontmatter}\n\n${content}`;

    // 写入文件
    fs.writeFileSync(specPath, fullContent, 'utf-8');

    // 加载并返回
    const doc = await this.loader.load(specPath);
    if (doc) {
      this.specs.set(doc.metadata.id, doc);
      this.emit('spec:added', doc);
    }

    return doc!;
  }

  /**
   * 更新规范
   */
  async updateSpec(id: string, content: string, metadata?: Partial<SpecMetadata>): Promise<SpecDocument | null> {
    const existing = this.specs.get(id);
    if (!existing) return null;

    const updatedMetadata: SpecMetadata = {
      ...existing.metadata,
      ...metadata,
      updatedAt: Date.now(),
    };

    const frontmatter = this.generateFrontmatter(updatedMetadata);
    const fullContent = `${frontmatter}\n\n${content}`;

    fs.writeFileSync(existing.path, fullContent, 'utf-8');

    // 清除缓存以确保重新加载
    this.loader.clearCache();

    const doc = await this.loader.load(existing.path);
    if (doc) {
      this.specs.set(doc.metadata.id, doc);
      this.emit('spec:updated', doc);
    }

    return doc;
  }

  /**
   * 删除规范
   */
  deleteSpec(id: string): boolean {
    const spec = this.specs.get(id);
    if (!spec) return false;

    try {
      fs.unlinkSync(spec.path);
      this.specs.delete(id);
      this.emit('spec:deleted', id);
      return true;
    } catch {
      return false;
    }
  }

  /**
   * 生成 frontmatter
   */
  private generateFrontmatter(metadata: SpecMetadata): string {
    const lines = [
      '---',
      `id: ${metadata.id}`,
      `name: ${metadata.name}`,
      `domain: ${metadata.domain}`,
      `type: ${metadata.type}`,
      `priority: ${metadata.priority}`,
      `tags: ${metadata.tags.join(', ')}`,
      `version: ${metadata.version}`,
    ];

    if (metadata.author) lines.push(`author: ${metadata.author}`);
    if (metadata.description) lines.push(`description: ${metadata.description}`);

    lines.push('---');
    return lines.join('\n');
  }

  /**
   * 重新加载所有规范
   */
  async reload(): Promise<void> {
    this.specs.clear();
    this.loader.clearCache();
    this.initialized = false;
    await this.initialize();
    this.emit('library:reloaded');
  }

  /**
   * 获取统计信息
   */
  getStats(): {
    total: number;
    byDomain: Record<SpecDomain, number>;
    byType: Record<SpecType, number>;
    byPriority: Record<SpecPriority, number>;
  } {
    const specs = this.getAllSpecs();

    const byDomain: Record<SpecDomain, number> = {
      frontend: 0,
      backend: 0,
      guides: 0,
      common: 0,
      custom: 0,
    };

    const byType: Record<SpecType, number> = {
      rule: 0,
      pattern: 0,
      template: 0,
      checklist: 0,
      reference: 0,
    };

    const byPriority: Record<SpecPriority, number> = {
      critical: 0,
      high: 0,
      medium: 0,
      low: 0,
    };

    for (const spec of specs) {
      byDomain[spec.metadata.domain]++;
      byType[spec.metadata.type]++;
      byPriority[spec.metadata.priority]++;
    }

    return {
      total: specs.length,
      byDomain,
      byType,
      byPriority,
    };
  }

  /**
   * 关闭规范库
   */
  close(): void {
    this.loader.disableHotReload();
    this.specs.clear();
    this.initialized = false;
    this.emit('library:closed');
  }
}
