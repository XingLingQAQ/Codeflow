// Old client layer, multipart entry: a FormData body cannot be migrated as
// ordinary JSON (the shared transport JSON.stringify()s its body), so the
// ledger must list POST /api/v1/workspace/upload under "Non-JSON entries" as
// multipart. The caller is WorkspacePage.
import { getApiBase, authHeadersFor } from '../api';

export async function uploadWorkspaceFile(file: Blob): Promise<void> {
  const form = new FormData();
  form.append('file', file);
  const url = `${getApiBase()}/api/v1/workspace/upload`;
  await fetch(url, {
    method: 'POST',
    headers: { ...authHeadersFor(url), 'Idempotency-Key': crypto.randomUUID() },
    body: form,
  });
}
