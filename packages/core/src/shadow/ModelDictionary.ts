import * as fs from 'fs';
import * as path from 'path';

export interface ModelField {
  name: string;
  type: string;
  required: boolean;
}

export interface ModelRelationship {
  entity: string;
  dto: string;
  type: 'map' | 'extend' | 'subset' | 'transform';
}

export interface ModelEntry {
  name: string;
  fields: ModelField[];
  relationships: ModelRelationship[];
  source: string;
  tags: string[];
  similarity?: number;
}

export interface ModelDictionaryConfig {
  dictionaryPath: string;
  similarityThreshold: number;
}

interface DuplicateCheckResult {
  isDuplicate: boolean;
  similarModels: ModelEntry[];
}

const DEFAULT_CONFIG: ModelDictionaryConfig = {
  dictionaryPath: '.codeflow/registry/models.yaml',
  similarityThreshold: 0.6,
};

export class ModelDictionary {
  private models: ModelEntry[] = [];
  private relationships: ModelRelationship[] = [];
  private readonly config: ModelDictionaryConfig;

  constructor(config: Partial<ModelDictionaryConfig> = {}) {
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  async register(entry: ModelEntry): Promise<DuplicateCheckResult> {
    const result = this.checkDuplicate(entry);
    if (result.isDuplicate) {
      return result;
    }
    this.models.push({ ...entry, similarity: undefined });
    return { isDuplicate: false, similarModels: [] };
  }

  search(query: string): ModelEntry[] {
    const queryKeywords = this.extractKeywords(query);
    if (queryKeywords.length === 0) return [];

    const scored = this.models
      .map((model) => {
        const modelKw = this.modelKeywords(model);
        const score = this.computeKeywordSimilarity(queryKeywords, modelKw);
        return { model: { ...model, similarity: score }, score };
      })
      .filter((item) => item.score > 0)
      .sort((a, b) => b.score - a.score);

    return scored.map((item) => item.model);
  }

  checkDuplicate(entry: ModelEntry): DuplicateCheckResult {
    const similar: ModelEntry[] = [];

    for (const existing of this.models) {
      if (entry.name === existing.name) {
        similar.push({ ...existing, similarity: 1.0 });
        continue;
      }

      const structuralScore = this.computeStructuralSimilarity(entry, existing);
      const keywordScore = this.computeKeywordSimilarity(
        this.modelKeywords(entry),
        this.modelKeywords(existing),
      );
      const combinedScore = structuralScore * 0.6 + keywordScore * 0.4;

      if (combinedScore >= this.config.similarityThreshold) {
        similar.push({ ...existing, similarity: combinedScore });
      }
    }

    return {
      isDuplicate: similar.length > 0,
      similarModels: similar.sort((a, b) => (b.similarity ?? 0) - (a.similarity ?? 0)),
    };
  }

  recordRelationship(entity: string, dto: string, type: ModelRelationship['type']): void {
    const existing = this.relationships.find(
      (r) => r.entity === entity && r.dto === dto && r.type === type,
    );
    if (!existing) {
      this.relationships.push({ entity, dto, type });
    }
  }

  getRelationships(): ModelRelationship[] {
    return [...this.relationships];
  }

  async loadFromYaml(): Promise<void> {
    const fullPath = path.resolve(this.config.dictionaryPath);
    let content: string;
    try {
      content = await fs.promises.readFile(fullPath, 'utf-8');
    } catch (err) {
      if ((err as NodeJS.ErrnoException).code === 'ENOENT') {
        this.models = [];
        this.relationships = [];
        return;
      }
      throw err;
    }

    const parsed = this.parseYaml(content);
    this.models = parsed.models;
    this.relationships = parsed.relationships;
  }

  async saveToYaml(): Promise<void> {
    const fullPath = path.resolve(this.config.dictionaryPath);
    await fs.promises.mkdir(path.dirname(fullPath), { recursive: true });
    const content = this.serializeYaml();
    await fs.promises.writeFile(fullPath, content, 'utf-8');
  }

  getModels(): ModelEntry[] {
    return [...this.models];
  }

  getModelCount(): number {
    return this.models.length;
  }

  private computeStructuralSimilarity(a: ModelEntry, b: ModelEntry): number {
    const fieldsA = new Set(a.fields.map((f) => `${f.name}:${f.type}`));
    const fieldsB = new Set(b.fields.map((f) => `${f.name}:${f.type}`));

    if (fieldsA.size === 0 && fieldsB.size === 0) return 0;

    let intersection = 0;
    for (const f of fieldsA) {
      if (fieldsB.has(f)) intersection++;
    }

    const union = new Set([...fieldsA, ...fieldsB]).size;
    if (union === 0) return 0;

    const fieldNameOverlap = this.computeFieldNameOverlap(a.fields, b.fields);

    return (intersection / union) * 0.7 + fieldNameOverlap * 0.3;
  }

  private computeFieldNameOverlap(fieldsA: ModelField[], fieldsB: ModelField[]): number {
    const namesA = new Set(fieldsA.map((f) => f.name.toLowerCase()));
    const namesB = new Set(fieldsB.map((f) => f.name.toLowerCase()));

    if (namesA.size === 0 && namesB.size === 0) return 0;

    let intersection = 0;
    for (const n of namesA) {
      if (namesB.has(n)) intersection++;
    }

    const union = new Set([...namesA, ...namesB]).size;
    return union === 0 ? 0 : intersection / union;
  }

  private modelKeywords(model: ModelEntry): string[] {
    const parts = [model.name, model.source, ...model.tags, ...model.fields.map((f) => f.name)];
    return this.extractKeywords(parts.join(' '));
  }

  private extractKeywords(text: string): string[] {
    return text
      .toLowerCase()
      .replace(/[^\w\s\u4e00-\u9fff]/g, ' ')
      .split(/\s+/)
      .filter((t) => t.length > 1);
  }

  private computeKeywordSimilarity(keywordsA: string[], keywordsB: string[]): number {
    if (keywordsA.length === 0 || keywordsB.length === 0) return 0;

    const setA = new Set(keywordsA);
    const setB = new Set(keywordsB);

    let intersection = 0;
    for (const kw of setA) {
      if (setB.has(kw)) intersection++;
    }

    const union = new Set([...setA, ...setB]).size;
    return union === 0 ? 0 : intersection / union;
  }

  private parseYaml(content: string): { models: ModelEntry[]; relationships: ModelRelationship[] } {
    const models: ModelEntry[] = [];
    const relationships: ModelRelationship[] = [];

    const sections = content.split(/^# /m);

    for (const section of sections) {
      const trimmed = section.trim();
      if (trimmed.startsWith('Models') || trimmed.startsWith('models')) {
        const blocks = trimmed.split(/^- name:/m).filter((b) => b.trim().length > 0);
        for (const block of blocks) {
          if (!block.includes(':')) continue;
          const model = this.parseModelBlock(`name:${block}`);
          if (model) models.push(model);
        }
      } else if (trimmed.startsWith('Relationships') || trimmed.startsWith('relationships')) {
        const relBlocks = trimmed.split(/^- /m).filter((b) => b.includes('entity:'));
        for (const block of relBlocks) {
          const rel = this.parseRelationshipBlock(block);
          if (rel) relationships.push(rel);
        }
      }
    }

    return { models, relationships };
  }

  private parseModelBlock(block: string): ModelEntry | null {
    const lines = block.split('\n');
    let name = '';
    let source = '';
    const tags: string[] = [];
    const fields: ModelField[] = [];

    for (const line of lines) {
      const trimmed = line.trim();
      if (trimmed.startsWith('name:')) name = trimmed.slice(5).trim().replace(/^["']|["']$/g, '');
      else if (trimmed.startsWith('source:')) source = trimmed.slice(7).trim().replace(/^["']|["']$/g, '');
      else if (trimmed.startsWith('tags:')) {
        const tagsStr = trimmed.slice(5).trim();
        if (tagsStr.startsWith('[')) {
          tags.push(
            ...tagsStr
              .replace(/^\[|\]$/g, '')
              .split(',')
              .map((t) => t.trim().replace(/^["']|["']$/g, ''))
              .filter(Boolean),
          );
        }
      } else if (trimmed.startsWith('- field:')) {
        const fieldParts = trimmed.slice(8).trim().split(',');
        if (fieldParts.length >= 2) {
          fields.push({
            name: fieldParts[0].trim(),
            type: fieldParts[1].trim(),
            required: fieldParts[2]?.trim() === 'required',
          });
        }
      }
    }

    if (!name) return null;
    return { name, fields, relationships: [], source, tags };
  }

  private parseRelationshipBlock(block: string): ModelRelationship | null {
    let entity = '';
    let dto = '';
    let type: ModelRelationship['type'] = 'map';

    for (const line of block.split('\n')) {
      const trimmed = line.trim();
      if (trimmed.startsWith('entity:')) entity = trimmed.slice(7).trim().replace(/^["']|["']$/g, '');
      else if (trimmed.startsWith('dto:')) dto = trimmed.slice(4).trim().replace(/^["']|["']$/g, '');
      else if (trimmed.startsWith('type:')) {
        const val = trimmed.slice(5).trim().replace(/^["']|["']$/g, '') as ModelRelationship['type'];
        if (['map', 'extend', 'subset', 'transform'].includes(val)) type = val;
      }
    }

    if (!entity || !dto) return null;
    return { entity, dto, type };
  }

  private serializeYaml(): string {
    const lines: string[] = [
      '# CodeFlow Model Dictionary',
      '# Auto-generated - do not edit manually',
      '',
      '# Models',
    ];

    for (const model of this.models) {
      lines.push(`- name: "${model.name}"`);
      lines.push(`  source: "${model.source}"`);
      lines.push(`  tags: [${model.tags.map((t) => `"${t}"`).join(', ')}]`);
      if (model.fields.length > 0) {
        lines.push('  fields:');
        for (const field of model.fields) {
          lines.push(`    - field: ${field.name}, ${field.type}, ${field.required ? 'required' : 'optional'}`);
        }
      }
      lines.push('');
    }

    if (this.relationships.length > 0) {
      lines.push('# Relationships');
      for (const rel of this.relationships) {
        lines.push(`- entity: "${rel.entity}"`);
        lines.push(`  dto: "${rel.dto}"`);
        lines.push(`  type: "${rel.type}"`);
        lines.push('');
      }
    }

    return lines.join('\n');
  }
}
