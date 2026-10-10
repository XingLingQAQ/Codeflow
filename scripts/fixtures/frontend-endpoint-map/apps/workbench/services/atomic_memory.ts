// Old client layer, dead code: DELETE /api/v1/memory/atomic/{id} is called by
// deleteAtomicMemory and by nothing else - no import anywhere in the fixture.
// No `post` is used here on purpose: an unused api.ts import makes the fixture
// emit a "imported but no call/value use found" note, which is noise the
// ledger does not need in order to be correct.
import { del, getApiBase } from '../api';

export async function deleteAtomicMemory(id: string): Promise<void> {
  await del<void>(`${getApiBase()}/api/v1/memory/atomic/${id}`);
}
