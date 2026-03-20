function createEventId() {
    return `change_${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}
export class InMemoryCodeChangeEventStore {
    constructor() {
        this.events = [];
    }
    appendCodeChangeEvent(event) {
        const record = {
            ...event,
            id: createEventId(),
            timestamp: Date.now(),
        };
        this.events.push(record);
        return record;
    }
    listCodeChangeEvents(filter = {}) {
        const filtered = this.events.filter((event) => {
            if (filter.type && event.type !== filter.type) {
                return false;
            }
            if (filter.sessionId && event.sessionId !== filter.sessionId) {
                return false;
            }
            if (filter.taskId && event.taskId !== filter.taskId) {
                return false;
            }
            if (filter.agentId && event.agentId !== filter.agentId) {
                return false;
            }
            if (filter.snapshotId && event.snapshotId !== filter.snapshotId) {
                return false;
            }
            return true;
        });
        const limited = filter.limit && filter.limit > 0 ? filtered.slice(-filter.limit) : filtered;
        return limited.map((event) => ({
            ...event,
            files: event.files ? [...event.files] : undefined,
            metadata: event.metadata ? { ...event.metadata } : undefined,
        }));
    }
    clearCodeChangeEvents() {
        this.events.length = 0;
    }
    countCodeChangeEvents() {
        return this.events.length;
    }
}
//# sourceMappingURL=CodeChangeEventStore.js.map