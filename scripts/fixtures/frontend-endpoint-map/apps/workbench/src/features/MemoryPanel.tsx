// Raw fetch through the shared transport helpers: no client module holds this
// path (status legacy_direct), the write carries an Idempotency-Key and the
// response is read as the {success, data} envelope.
import { getApiBase, authHeadersFor } from '../../api';

export async function searchMemory(query: string): Promise<unknown> {
  const response = await fetch(`${getApiBase()}/api/v1/memory/atomic/search`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...authHeadersFor(`${getApiBase()}/api/v1/memory/atomic/search`),
      'Idempotency-Key': crypto.randomUUID(),
    },
    body: JSON.stringify({ query }),
  });
  const body = (await response.json()) as { success: boolean; data?: unknown };
  if (!body.success) throw new Error('search failed');
  return body.data;
}

export const MemoryPanel = ({ query }: { query: string }) => (
  <button type="button" onClick={() => void searchMemory(query)}>
    search
  </button>
);
