import * as fs from 'fs';
import * as path from 'path';

export interface APIRegistryEntry {
  path: string;
  method: string;
  description: string;
  handler: string;
  tags: string[];
  similarity?: number;
}

export interface APIRegistryConfig {
  registryPath: string;
  similarityThreshold: number;
}

interface DuplicateCheckResult {
  isDuplicate: boolean;
  similarEntries: APIRegistryEntry[];
}

const DEFAULT_CONFIG: APIRegistryConfig = {
  registryPath: '.codeflow/registry/apis.yaml',
  similarityThreshold: 0.7,
};

export class APIRegistry {
  private entries: APIRegistryEntry[] = [];
  private readonly config: APIRegistryConfig;

  constructor(config: Partial<APIRegistryConfig> = {}) {
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  async register(entry: APIRegistryEntry): Promise<DuplicateCheckResult> {
    const result = this.checkDuplicate(entry);
    if (result.isDuplicate) {
      return result;
    }
    this.entries.push({ ...entry, similarity: undefined });
    return { isDuplicate: false, similarEntries: [] };
  }

  search(query: string): APIRegistryEntry[] {
    const queryKeywords = this.extractKeywords(query);
    if (queryKeywords.length === 0) return [];

    const scored = this.entries
      .map((entry) => {
        const score = this.computeSimilarity(queryKeywords, this.entryKeywords(entry));
        return { entry: { ...entry, similarity: score }, score };
      })
      .filter((item) => item.score > 0)
      .sort((a, b) => b.score - a.score);

    return scored.map((item) => item.entry);
  }

  checkDuplicate(entry: APIRegistryEntry): DuplicateCheckResult {
    const entryKw = this.entryKeywords(entry);
    const similar: APIRegistryEntry[] = [];

    for (const existing of this.entries) {
      const existingKw = this.entryKeywords(existing);
      const score = this.computeSimilarity(entryKw, existingKw);

      if (entry.path === existing.path && entry.method === existing.method) {
        similar.push({ ...existing, similarity: 1.0 });
        continue;
      }

      if (score >= this.config.similarityThreshold) {
        similar.push({ ...existing, similarity: score });
      }
    }

    return {
      isDuplicate: similar.length > 0,
      similarEntries: similar.sort((a, b) => (b.similarity ?? 0) - (a.similarity ?? 0)),
    };
  }

  async loadFromYaml(): Promise<void> {
    const fullPath = path.resolve(this.config.registryPath);
    let content: string;
    try {
      content = await fs.promises.readFile(fullPath, 'utf-8');
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === 'ENOENT') {
        this.entries = [];
        return;
      }
      throw err;
    }

    this.entries = this.parseYaml(content);
  }

  async saveToYaml(): Promise<void> {
    const fullPath = path.resolve(this.config.registryPath);
    await fs.promises.mkdir(path.dirname(fullPath), { recursive: true });
    const content = this.serializeYaml(this.entries);
    await fs.promises.writeFile(fullPath, content, 'utf-8');
  }

  getEntries(): APIRegistryEntry[] {
    return [...this.entries];
  }

  getEntryCount(): number {
    return this.entries.length;
  }

  private entryKeywords(entry: APIRegistryEntry): string[] {
    const parts = [
      entry.path,
      entry.method,
      entry.description,
      entry.handler,
      ...entry.tags,
    ];
    return this.extractKeywords(parts.join(' '));
  }

  private extractKeywords(text: string): string[] {
    return text
      .toLowerCase()
      .replace(/[^\w\s\u4e00-\u9fff/-]/g, ' ')
      .split(/[\s/]+/)
      .filter((t) => t.length > 1);
  }

  private computeSimilarity(keywordsA: string[], keywordsB: string[]): number {
    if (keywordsA.length === 0 || keywordsB.length === 0) return 0;

    const setA = new Set(keywordsA);
    const setB = new Set(keywordsB);

    let intersection = 0;
    for (const kw of setA) {
      if (setB.has(kw)) intersection++;
    }

    const union = new Set([...setA, ...setB]).size;
    if (union === 0) return 0;

    return intersection / union;
  }

  private parseYaml(content: string): APIRegistryEntry[] {
    const entries: APIRegistryEntry[] = [];
    const blocks = content.split(/^- /m).filter((b) => b.trim().length > 0);

    for (const block of blocks) {
      const entry: Partial<APIRegistryEntry> = {};
      const lines = block.split('\n');

      for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed.startsWith('path:')) entry.path = trimmed.slice(5).trim().replace(/^["']|["']$/g, '');
        else if (trimmed.startsWith('method:')) entry.method = trimmed.slice(7).trim().replace(/^["']|["']$/g, '');
        else if (trimmed.startsWith('description:')) entry.description = trimmed.slice(12).trim().replace(/^["']|["']$/g, '');
        else if (trimmed.startsWith('handler:')) entry.handler = trimmed.slice(8).trim().replace(/^["']|["']$/g, '');
        else if (trimmed.startsWith('tags:')) {
          const tagsMatch = trimmed.slice(5).trim();
          if (tagsMatch.startsWith('[')) {
            entry.tags = tagsMatch
              .replace(/^\[|\]$/g, '')
              .split(',')
              .map((t) => t.trim().replace(/^["']|["']$/g, ''))
              .filter(Boolean);
          }
        } else if (trimmed.startsWith('- ') && entry.tags === undefined) {
          // skip sub-list items not related to tags
        }
      }

      if (entry.path && entry.method) {
        entries.push({
          path: entry.path,
          method: entry.method,
          description: entry.description ?? '',
          handler: entry.handler ?? '',
          tags: entry.tags ?? [],
        });
      }
    }

    return entries;
  }

  private serializeYaml(entries: APIRegistryEntry[]): string {
    const lines: string[] = ['# CodeFlow API Registry', '# Auto-generated - do not edit manually', ''];

    for (const entry of entries) {
      lines.push(`- path: "${entry.path}"`);
      lines.push(`  method: "${entry.method}"`);
      lines.push(`  description: "${entry.description}"`);
      lines.push(`  handler: "${entry.handler}"`);
      lines.push(`  tags: [${entry.tags.map((t) => `"${t}"`).join(', ')}]`);
      lines.push('');
    }

    return lines.join('\n');
  }
}
