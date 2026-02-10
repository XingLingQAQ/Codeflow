import * as fs from 'fs';
import * as path from 'path';

export interface ContextLoaderConfig {
  maxTokenBudget: number;
  cacheEnabled: boolean;
  shadowRoot: string;
  projectRoot: string;
}

export interface LoadedContext {
  filePath: string;
  content: string;
  tokenCount: number;
}

export interface ContextLoadResult {
  contexts: LoadedContext[];
  totalTokens: number;
  budgetRemaining: number;
}

interface CacheEntry {
  content: string;
  tokenCount: number;
  accessedAt: number;
}

const DEFAULT_CONFIG: ContextLoaderConfig = {
  maxTokenBudget: 8000,
  cacheEnabled: true,
  shadowRoot: '.codeflow',
  projectRoot: '.',
};

const CHARS_PER_TOKEN = 4;

export class ContextLoader {
  private readonly config: ContextLoaderConfig;
  private readonly cache: Map<string, CacheEntry> = new Map();
  private readonly maxCacheSize = 100;

  constructor(config: Partial<ContextLoaderConfig> = {}) {
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  async loadContext(intent: string): Promise<ContextLoadResult> {
    const domainDir = path.resolve(this.config.projectRoot, this.config.shadowRoot, 'domain');
    const intentFiles = await this.scanIntentFiles(domainDir);

    const keywords = this.extractKeywords(intent);
    if (keywords.length === 0) {
      return { contexts: [], totalTokens: 0, budgetRemaining: this.config.maxTokenBudget };
    }

    const scored = await Promise.all(
      intentFiles.map(async (filePath) => {
        const content = await this.readWithCache(filePath);
        const fileKeywords = this.extractKeywords(content);
        const score = this.computeRelevance(keywords, fileKeywords);
        return { filePath, content, score };
      }),
    );

    scored.sort((a, b) => b.score - a.score);

    const contexts: LoadedContext[] = [];
    let totalTokens = 0;

    for (const item of scored) {
      if (item.score <= 0) break;

      const tokenCount = this.estimateTokens(item.content);
      if (totalTokens + tokenCount > this.config.maxTokenBudget) {
        const remaining = this.config.maxTokenBudget - totalTokens;
        if (remaining > 100) {
          const truncated = item.content.slice(0, remaining * CHARS_PER_TOKEN);
          const truncatedTokens = this.estimateTokens(truncated);
          contexts.push({ filePath: item.filePath, content: truncated, tokenCount: truncatedTokens });
          totalTokens += truncatedTokens;
        }
        break;
      }

      contexts.push({ filePath: item.filePath, content: item.content, tokenCount });
      totalTokens += tokenCount;
    }

    return {
      contexts,
      totalTokens,
      budgetRemaining: this.config.maxTokenBudget - totalTokens,
    };
  }

  async loadWithDependencies(filePath: string): Promise<ContextLoadResult> {
    const resolvedPath = path.resolve(filePath);
    const contexts: LoadedContext[] = [];
    let totalTokens = 0;

    const intentDocPath = this.resolveIntentDocPath(resolvedPath);
    const mainContent = await this.readFileOrEmpty(intentDocPath);

    if (mainContent) {
      const tokenCount = this.estimateTokens(mainContent);
      if (totalTokens + tokenCount <= this.config.maxTokenBudget) {
        contexts.push({ filePath: intentDocPath, content: mainContent, tokenCount });
        totalTokens += tokenCount;
      }
    }

    const sourceContent = await this.readFileOrEmpty(resolvedPath);
    if (sourceContent) {
      const imports = this.extractImportPaths(sourceContent);

      for (const importPath of imports) {
        if (totalTokens >= this.config.maxTokenBudget) break;

        const depIntentPath = this.resolveImportIntentPath(importPath, resolvedPath);
        const depContent = await this.readFileOrEmpty(depIntentPath);

        if (depContent) {
          const tokenCount = this.estimateTokens(depContent);
          if (totalTokens + tokenCount <= this.config.maxTokenBudget) {
            contexts.push({ filePath: depIntentPath, content: depContent, tokenCount });
            totalTokens += tokenCount;
          }
        }
      }
    }

    return {
      contexts,
      totalTokens,
      budgetRemaining: this.config.maxTokenBudget - totalTokens,
    };
  }

  clearCache(): void {
    this.cache.clear();
  }

  private estimateTokens(text: string): number {
    return Math.ceil(text.length / CHARS_PER_TOKEN);
  }

  private async readWithCache(filePath: string): Promise<string> {
    if (this.config.cacheEnabled) {
      const cached = this.cache.get(filePath);
      if (cached) {
        cached.accessedAt = Date.now();
        return cached.content;
      }
    }

    const content = await this.readFileOrEmpty(filePath);

    if (this.config.cacheEnabled && content) {
      this.evictIfNeeded();
      this.cache.set(filePath, {
        content,
        tokenCount: this.estimateTokens(content),
        accessedAt: Date.now(),
      });
    }

    return content;
  }

  private evictIfNeeded(): void {
    if (this.cache.size < this.maxCacheSize) return;

    let oldestKey = '';
    let oldestTime = Infinity;

    for (const [key, entry] of this.cache) {
      if (entry.accessedAt < oldestTime) {
        oldestTime = entry.accessedAt;
        oldestKey = key;
      }
    }

    if (oldestKey) {
      this.cache.delete(oldestKey);
    }
  }

  private async readFileOrEmpty(filePath: string): Promise<string> {
    try {
      return await fs.promises.readFile(filePath, 'utf-8');
    } catch {
      return '';
    }
  }

  private async scanIntentFiles(dir: string): Promise<string[]> {
    const files: string[] = [];
    try {
      const entries = await fs.promises.readdir(dir, { withFileTypes: true });
      for (const entry of entries) {
        const fullPath = path.join(dir, entry.name);
        if (entry.isDirectory()) {
          const subFiles = await this.scanIntentFiles(fullPath);
          files.push(...subFiles);
        } else if (entry.name.endsWith('.md') || entry.name.endsWith('.intent.md')) {
          files.push(fullPath);
        }
      }
    } catch {
      // directory does not exist
    }
    return files;
  }

  private extractKeywords(text: string): string[] {
    return text
      .toLowerCase()
      .replace(/[^\w\s\u4e00-\u9fff]/g, ' ')
      .split(/\s+/)
      .filter((t) => t.length > 1);
  }

  private computeRelevance(queryKeywords: string[], docKeywords: string[]): number {
    if (queryKeywords.length === 0 || docKeywords.length === 0) return 0;

    const docSet = new Set(docKeywords);
    let matches = 0;

    for (const kw of queryKeywords) {
      if (docSet.has(kw)) matches++;
    }

    return matches / queryKeywords.length;
  }

  private resolveIntentDocPath(sourceFilePath: string): string {
    const projectRoot = path.resolve(this.config.projectRoot);
    const relativePath = path.relative(projectRoot, sourceFilePath);
    const intentRelative = relativePath.replace(/\.(ts|tsx|js|jsx|go|py)$/, '.intent.md');
    return path.join(projectRoot, this.config.shadowRoot, 'domain', intentRelative);
  }

  private resolveImportIntentPath(importPath: string, fromFile: string): string {
    const projectRoot = path.resolve(this.config.projectRoot);
    let resolvedImport: string;

    if (importPath.startsWith('.')) {
      resolvedImport = path.resolve(path.dirname(fromFile), importPath);
    } else {
      resolvedImport = path.resolve(projectRoot, 'src', importPath);
    }

    const extensions = ['.ts', '.tsx', '.js', '.jsx', '.go', '.py'];
    let ext = path.extname(resolvedImport);
    if (!ext || !extensions.includes(ext)) {
      ext = '.ts';
      resolvedImport = `${resolvedImport}${ext}`;
    }

    const relativePath = path.relative(projectRoot, resolvedImport);
    const intentRelative = relativePath.replace(/\.(ts|tsx|js|jsx|go|py)$/, '.intent.md');
    return path.join(projectRoot, this.config.shadowRoot, 'domain', intentRelative);
  }

  private extractImportPaths(sourceCode: string): string[] {
    const imports: string[] = [];

    const tsImportRegex = /import\s+.*?\s+from\s+['"]([^'"]+)['"]/g;
    let match: RegExpExecArray | null;
    while ((match = tsImportRegex.exec(sourceCode)) !== null) {
      imports.push(match[1]);
    }

    const goImportRegex = /import\s+"([^"]+)"/g;
    while ((match = goImportRegex.exec(sourceCode)) !== null) {
      imports.push(match[1]);
    }

    const pyImportRegex = /(?:from\s+(\S+)\s+import|import\s+(\S+))/g;
    while ((match = pyImportRegex.exec(sourceCode)) !== null) {
      imports.push(match[1] || match[2]);
    }

    return imports.filter(Boolean);
  }
}
