/**
 * 导图存储实现
 * 基于文件系统的持久化存储
 */
export class InMemoryMapStorage {
    constructor() {
        this.maps = new Map();
    }
    async save(map) {
        this.maps.set(map.id, map);
    }
    async load(id) {
        return this.maps.get(id) || null;
    }
    async loadBySession(sessionId) {
        return Array.from(this.maps.values()).filter((m) => m.sessionId === sessionId);
    }
    async delete(id) {
        this.maps.delete(id);
    }
    async list() {
        return Array.from(this.maps.values()).map((m) => ({
            id: m.id,
            sessionId: m.sessionId,
            createdAt: m.createdAt,
        }));
    }
    clear() {
        this.maps.clear();
    }
    size() {
        return this.maps.size;
    }
}
//# sourceMappingURL=MapStorage.js.map