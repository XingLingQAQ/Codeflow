/**
 * 内存三元组存储实现
 * 支持 JSON-LD 格式的 S-P-O 三元组存储与查询
 */

import {
  Triple,
  TripleNode,
  LiteralValue,
  TripleQuery,
  TripleStoreConfig,
  ITripleStore,
  Entity,
  JsonLdGraph,
  GraphMetadata,
  DEFAULT_TRIPLE_STORE_CONFIG,
  generateTripleId,
  isLiteralValue,
} from './types.js';

export class InMemoryTripleStore implements ITripleStore {
  private triples: Map<string, Triple> = new Map();
  private entities: Map<string, Entity> = new Map();
  private subjectIndex: Map<string, Set<string>> = new Map();
  private predicateIndex: Map<string, Set<string>> = new Map();
  private objectIndex: Map<string, Set<string>> = new Map();
  private config: TripleStoreConfig;

  constructor(config: Partial<TripleStoreConfig> = {}) {
    this.config = { ...DEFAULT_TRIPLE_STORE_CONFIG, ...config };
  }

  async add(triples: Triple[]): Promise<void> {
    for (const triple of triples) {
      if (this.triples.size >= this.config.maxTriples) {
        throw new Error(`Max triples limit reached: ${this.config.maxTriples}`);
      }

      if (this.config.enableDeduplication) {
        const existingId = this.findDuplicate(triple);
        if (existingId) {
          const existing = this.triples.get(existingId)!;
          if (triple.confidence > existing.confidence) {
            this.removeFromIndices(existingId, existing);
            this.triples.delete(existingId);
          } else {
            continue;
          }
        }
      }

      this.triples.set(triple['@id'], triple);
      this.addToIndices(triple);
      this.updateEntitiesFromTriple(triple);
    }
  }

  async get(id: string): Promise<Triple | null> {
    return this.triples.get(id) || null;
  }

  async update(id: string, updates: Partial<Triple>): Promise<void> {
    const existing = this.triples.get(id);
    if (!existing) {
      throw new Error(`Triple not found: ${id}`);
    }

    this.removeFromIndices(id, existing);
    const updated = { ...existing, ...updates, '@id': id };
    this.triples.set(id, updated);
    this.addToIndices(updated);
  }

  async delete(ids: string[]): Promise<void> {
    for (const id of ids) {
      const triple = this.triples.get(id);
      if (triple) {
        this.removeFromIndices(id, triple);
        this.triples.delete(id);
      }
    }
  }

  async clear(): Promise<void> {
    this.triples.clear();
    this.entities.clear();
    this.subjectIndex.clear();
    this.predicateIndex.clear();
    this.objectIndex.clear();
  }

  async query(query: TripleQuery): Promise<Triple[]> {
    let candidateIds: Set<string> | null = null;

    if (query.subject) {
      const subjectId = typeof query.subject === 'string'
        ? query.subject
        : query.subject['@id'];
      if (subjectId) {
        candidateIds = this.subjectIndex.get(subjectId) || new Set();
      }
    }

    if (query.predicate) {
      const predicateIds = this.predicateIndex.get(query.predicate) || new Set();
      candidateIds = candidateIds
        ? this.intersect(candidateIds, predicateIds)
        : predicateIds;
    }

    if (query.object) {
      const objectId = typeof query.object === 'string'
        ? query.object
        : '@id' in query.object
          ? (query.object as TripleNode)['@id']
          : undefined;
      if (objectId) {
        const objectIds = this.objectIndex.get(objectId) || new Set();
        candidateIds = candidateIds
          ? this.intersect(candidateIds, objectIds)
          : objectIds;
      }
    }

    const ids = candidateIds || new Set(this.triples.keys());
    let results: Triple[] = [];

    for (const id of ids) {
      const triple = this.triples.get(id);
      if (!triple) continue;

      if (query.minConfidence && triple.confidence < query.minConfidence) continue;

      if (query.source) {
        const src = triple.source;
        if (query.source.sessionId && src.sessionId !== query.source.sessionId) continue;
        if (query.source.agentRole && src.agentRole !== query.source.agentRole) continue;
        if (query.source.extractionMethod && src.extractionMethod !== query.source.extractionMethod) continue;
      }

      results.push(triple);
    }

    results.sort((a, b) => b.confidence - a.confidence);

    const offset = query.offset || 0;
    const limit = query.limit || results.length;
    return results.slice(offset, offset + limit);
  }

  async findBySubject(subjectId: string): Promise<Triple[]> {
    return this.query({ subject: subjectId });
  }

  async findByPredicate(predicate: string): Promise<Triple[]> {
    return this.query({ predicate });
  }

  async findByObject(objectId: string): Promise<Triple[]> {
    return this.query({ object: objectId });
  }

  async getEntity(id: string): Promise<Entity | null> {
    return this.entities.get(id) || null;
  }

  async getEntities(): Promise<Entity[]> {
    return Array.from(this.entities.values());
  }

  async upsertEntity(entity: Entity): Promise<void> {
    const existing = this.entities.get(entity['@id']);
    if (existing) {
      this.entities.set(entity['@id'], {
        ...existing,
        ...entity,
        updatedAt: Date.now(),
      });
    } else {
      this.entities.set(entity['@id'], entity);
    }
  }

  async exportGraph(): Promise<JsonLdGraph> {
    const stats = await this.getStats();
    return {
      '@context': {
        '@vocab': this.config.vocabUri,
        '@base': this.config.baseUri,
        'rdf': 'http://www.w3.org/1999/02/22-rdf-syntax-ns#',
        'rdfs': 'http://www.w3.org/2000/01/rdf-schema#',
        'owl': 'http://www.w3.org/2002/07/owl#',
        'codeflow': this.config.vocabUri,
      },
      '@id': this.config.graphId,
      '@type': 'Graph',
      '@graph': Array.from(this.triples.values()),
      metadata: stats,
    };
  }

  async importGraph(graph: JsonLdGraph): Promise<void> {
    await this.clear();
    await this.add(graph['@graph']);
  }

  async getStats(): Promise<GraphMetadata> {
    const predicates = new Set<string>();
    for (const triple of this.triples.values()) {
      predicates.add(triple.predicate);
    }

    return {
      createdAt: Date.now(),
      updatedAt: Date.now(),
      tripleCount: this.triples.size,
      entityCount: this.entities.size,
      predicateCount: predicates.size,
      version: '1.0.0',
    };
  }

  async deduplicate(): Promise<number> {
    const seen = new Map<string, string>();
    const toDelete: string[] = [];

    for (const [id, triple] of this.triples) {
      const key = this.getTripleKey(triple);
      const existingId = seen.get(key);

      if (existingId) {
        const existing = this.triples.get(existingId)!;
        if (triple.confidence > existing.confidence) {
          toDelete.push(existingId);
          seen.set(key, id);
        } else {
          toDelete.push(id);
        }
      } else {
        seen.set(key, id);
      }
    }

    await this.delete(toDelete);
    return toDelete.length;
  }

  private findDuplicate(triple: Triple): string | null {
    const key = this.getTripleKey(triple);
    for (const [id, existing] of this.triples) {
      if (this.getTripleKey(existing) === key) {
        return id;
      }
    }
    return null;
  }

  private getTripleKey(triple: Triple): string {
    const subjectId = triple.subject['@id'];
    const objectId = isLiteralValue(triple.object)
      ? `literal:${triple.object['@value']}`
      : triple.object['@id'];
    return `${subjectId}|${triple.predicate}|${objectId}`;
  }

  private addToIndices(triple: Triple): void {
    const subjectId = triple.subject['@id'];
    if (!this.subjectIndex.has(subjectId)) {
      this.subjectIndex.set(subjectId, new Set());
    }
    this.subjectIndex.get(subjectId)!.add(triple['@id']);

    if (!this.predicateIndex.has(triple.predicate)) {
      this.predicateIndex.set(triple.predicate, new Set());
    }
    this.predicateIndex.get(triple.predicate)!.add(triple['@id']);

    if (!isLiteralValue(triple.object)) {
      const objectId = triple.object['@id'];
      if (!this.objectIndex.has(objectId)) {
        this.objectIndex.set(objectId, new Set());
      }
      this.objectIndex.get(objectId)!.add(triple['@id']);
    }
  }

  private removeFromIndices(id: string, triple: Triple): void {
    const subjectId = triple.subject['@id'];
    this.subjectIndex.get(subjectId)?.delete(id);

    this.predicateIndex.get(triple.predicate)?.delete(id);

    if (!isLiteralValue(triple.object)) {
      const objectId = triple.object['@id'];
      this.objectIndex.get(objectId)?.delete(id);
    }
  }

  private updateEntitiesFromTriple(triple: Triple): void {
    const subjectEntity: Entity = {
      '@id': triple.subject['@id'],
      '@type': triple.subject['@type'] || 'codeflow:Entity',
      label: triple.subject.label || triple.subject['@id'],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };

    if (!this.entities.has(subjectEntity['@id'])) {
      this.entities.set(subjectEntity['@id'], subjectEntity);
    }

    if (!isLiteralValue(triple.object)) {
      const objectEntity: Entity = {
        '@id': triple.object['@id'],
        '@type': triple.object['@type'] || 'codeflow:Entity',
        label: triple.object.label || triple.object['@id'],
        createdAt: Date.now(),
        updatedAt: Date.now(),
      };

      if (!this.entities.has(objectEntity['@id'])) {
        this.entities.set(objectEntity['@id'], objectEntity);
      }
    }
  }

  private intersect(a: Set<string>, b: Set<string>): Set<string> {
    const result = new Set<string>();
    for (const item of a) {
      if (b.has(item)) {
        result.add(item);
      }
    }
    return result;
  }
}
