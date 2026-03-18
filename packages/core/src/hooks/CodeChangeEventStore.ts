import type { CodeChangeEvent, CodeChangeEventFilter, CodeChangeEventRecorder } from './types.js';

function createEventId(): string {
  return `change_${Date.now()}_${Math.random().toString(16).slice(2, 10)}`;
}

export class InMemoryCodeChangeEventStore implements CodeChangeEventRecorder {
  private readonly events: CodeChangeEvent[] = [];

  appendCodeChangeEvent(event: Omit<CodeChangeEvent, 'id' | 'timestamp'>): CodeChangeEvent {
    const record: CodeChangeEvent = {
      ...event,
      id: createEventId(),
      timestamp: Date.now(),
    };
    this.events.push(record);
    return record;
  }

  listCodeChangeEvents(filter: CodeChangeEventFilter = {}): CodeChangeEvent[] {
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

  clearCodeChangeEvents(): void {
    this.events.length = 0;
  }

  countCodeChangeEvents(): number {
    return this.events.length;
  }
}
