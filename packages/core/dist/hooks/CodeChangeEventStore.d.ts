import type { CodeChangeEvent, CodeChangeEventFilter, CodeChangeEventRecorder } from './types.js';
export declare class InMemoryCodeChangeEventStore implements CodeChangeEventRecorder {
    private readonly events;
    appendCodeChangeEvent(event: Omit<CodeChangeEvent, 'id' | 'timestamp'>): CodeChangeEvent;
    listCodeChangeEvents(filter?: CodeChangeEventFilter): CodeChangeEvent[];
    clearCodeChangeEvents(): void;
    countCodeChangeEvents(): number;
}
//# sourceMappingURL=CodeChangeEventStore.d.ts.map