// Old client layer, download entry: the response is a server-rendered
// archive (`URL.createObjectURL` + an `a.download` attribute), not JSON, so
// the ledger lists GET /api/v1/exports/{id} under "Non-JSON entries" as
// download. The caller is ExportsPage.
import { getApiBase, authHeadersFor } from '../api';

export async function downloadExport(id: string): Promise<void> {
  const url = `${getApiBase()}/api/v1/exports/${id}`;
  const response = await fetch(url, { headers: { ...authHeadersFor(url) } });
  const blob = await response.blob();
  const href = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = href;
  anchor.download = `export-${id}.zip`;
  anchor.click();
}
