/**
 * JSONL 文件存储层
 * 支持 memory_graph.jsonl 读写
 */

import { Triple, JsonLdGraph, GraphMetadata, DEFAULT_TRIPLE_STORE_CONFIG } from './types.js';
import * as fs from 'fs';
import * as path from 'path';
import * as readline from 'readline';

export interface JsonlStorageConfig {
  filePath: string;
  autoFlush: boolean;
  flushInterval: number;
  maxBufferSize: number;
}

const DEFAULT_JSONL_CONFIG: JsonlStorageConfig = {
  filePath: './memory_graph.jsonl',
  autoFlush: true,
  flushInterval: 5000,
  maxBufferSize: 100,
};

export class JsonlStorage {
  private config: JsonlStorageConfig;
  private writeBuffer: Triple[] = [];
  private flushTimer: NodeJS.Timeout | null = null;

  constructor(config: Partial<JsonlStorageConfig> = {}) {
    this.config = { ...DEFAULT_JSONL_CONFIG, ...config };

    if (this.config.autoFlush) {
      this.startAutoFlush();
    }
  }

  async append(triples: Triple[]): Promise<void> {
    this.writeBuffer.push(...triples);

    if (this.writeBuffer.length >= this.config.maxBufferSize) {
      await this.flush();
    }
  }

  async flush(): Promise<void> {
    if (this.writeBuffer.length === 0) return;

    const lines = this.writeBuffer.map(t => JSON.stringify(t)).join('\n') + '\n';
    await this.appendToFile(lines);
    this.writeBuffer = [];
  }

  async readAll(): Promise<Triple[]> {
    const triples: Triple[] = [];

    if (!fs.existsSync(this.config.filePath)) {
      return triples;
    }

    const fileStream = fs.createReadStream(this.config.filePath, { encoding: 'utf-8' });
    const rl = readline.createInterface({
      input: fileStream,
      crlfDelay: Infinity,
    });

    for await (const line of rl) {
      if (line.trim()) {
        try {
          const triple = JSON.parse(line) as Triple;
          triples.push(triple);
        } catch {
          // Skip malformed lines
        }
      }
    }

    return triples;
  }

  async readStream(callback: (triple: Triple) => void | Promise<void>): Promise<number> {
    let count = 0;

    if (!fs.existsSync(this.config.filePath)) {
      return count;
    }

    const fileStream = fs.createReadStream(this.config.filePath, { encoding: 'utf-8' });
    const rl = readline.createInterface({
      input: fileStream,
      crlfDelay: Infinity,
    });

    for await (const line of rl) {
      if (line.trim()) {
        try {
          const triple = JSON.parse(line) as Triple;
          await callback(triple);
          count++;
        } catch {
          // Skip malformed lines
        }
      }
    }

    return count;
  }

  async exportToJsonLd(): Promise<JsonLdGraph> {
    const triples = await this.readAll();
    const predicates = new Set<string>();
    const entities = new Set<string>();

    for (const triple of triples) {
      predicates.add(triple.predicate);
      entities.add(triple.subject['@id']);
      if ('@id' in triple.object) {
        entities.add(triple.object['@id']);
      }
    }

    const metadata: GraphMetadata = {
      createdAt: Date.now(),
      updatedAt: Date.now(),
      tripleCount: triples.length,
      entityCount: entities.size,
      predicateCount: predicates.size,
      version: '1.0.0',
    };

    return {
      '@context': {
        '@vocab': DEFAULT_TRIPLE_STORE_CONFIG.vocabUri,
        '@base': DEFAULT_TRIPLE_STORE_CONFIG.baseUri,
        'rdf': 'http://www.w3.org/1999/02/22-rdf-syntax-ns#',
        'rdfs': 'http://www.w3.org/2000/01/rdf-schema#',
        'owl': 'http://www.w3.org/2002/07/owl#',
        'codeflow': DEFAULT_TRIPLE_STORE_CONFIG.vocabUri,
      },
      '@id': DEFAULT_TRIPLE_STORE_CONFIG.graphId,
      '@type': 'Graph',
      '@graph': triples,
      metadata,
    };
  }

  async importFromJsonLd(graph: JsonLdGraph): Promise<void> {
    await this.clear();
    await this.append(graph['@graph']);
    await this.flush();
  }

  async clear(): Promise<void> {
    this.writeBuffer = [];
    if (fs.existsSync(this.config.filePath)) {
      await fs.promises.writeFile(this.config.filePath, '', 'utf-8');
    }
  }

  async getStats(): Promise<{ lineCount: number; fileSize: number }> {
    if (!fs.existsSync(this.config.filePath)) {
      return { lineCount: 0, fileSize: 0 };
    }

    const stats = await fs.promises.stat(this.config.filePath);
    let lineCount = 0;

    const fileStream = fs.createReadStream(this.config.filePath, { encoding: 'utf-8' });
    const rl = readline.createInterface({
      input: fileStream,
      crlfDelay: Infinity,
    });

    for await (const line of rl) {
      if (line.trim()) lineCount++;
    }

    return { lineCount, fileSize: stats.size };
  }

  async deleteByTimestamp(beforeTimestamp: number): Promise<number> {
    const triples = await this.readAll();
    const remaining = triples.filter(t => t.timestamp >= beforeTimestamp);
    const deleted = triples.length - remaining.length;

    if (deleted > 0) {
      await this.clear();
      await this.append(remaining);
      await this.flush();
    }

    return deleted;
  }

  async deleteBySessionId(sessionId: string): Promise<number> {
    const triples = await this.readAll();
    const remaining = triples.filter(t => t.source.sessionId !== sessionId);
    const deleted = triples.length - remaining.length;

    if (deleted > 0) {
      await this.clear();
      await this.append(remaining);
      await this.flush();
    }

    return deleted;
  }

  close(): void {
    if (this.flushTimer) {
      clearInterval(this.flushTimer);
      this.flushTimer = null;
    }
    this.flush().catch(() => {});
  }

  private async appendToFile(content: string): Promise<void> {
    const dir = path.dirname(this.config.filePath);
    if (!fs.existsSync(dir)) {
      await fs.promises.mkdir(dir, { recursive: true });
    }
    await fs.promises.appendFile(this.config.filePath, content, 'utf-8');
  }

  private startAutoFlush(): void {
    this.flushTimer = setInterval(() => {
      this.flush().catch(() => {});
    }, this.config.flushInterval);
  }
}
