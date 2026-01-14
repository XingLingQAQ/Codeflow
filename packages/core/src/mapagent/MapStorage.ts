/**
 * 导图存储实现
 * 基于文件系统的持久化存储
 */

import { IMapStorage, CompressionMap } from './types.js';

export class InMemoryMapStorage implements IMapStorage {
  private maps: Map<string, CompressionMap> = new Map();

  async save(map: CompressionMap): Promise<void> {
    this.maps.set(map.id, map);
  }

  async load(id: string): Promise<CompressionMap | null> {
    return this.maps.get(id) || null;
  }

  async loadBySession(sessionId: string): Promise<CompressionMap[]> {
    return Array.from(this.maps.values()).filter((m) => m.sessionId === sessionId);
  }

  async delete(id: string): Promise<void> {
    this.maps.delete(id);
  }

  async list(): Promise<Array<{ id: string; sessionId: string; createdAt: number }>> {
    return Array.from(this.maps.values()).map((m) => ({
      id: m.id,
      sessionId: m.sessionId,
      createdAt: m.createdAt,
    }));
  }

  clear(): void {
    this.maps.clear();
  }

  size(): number {
    return this.maps.size;
  }
}
