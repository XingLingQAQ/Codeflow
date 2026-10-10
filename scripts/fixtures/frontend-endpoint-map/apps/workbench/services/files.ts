// Old client layer: POST /api/v1/workspace/write -> writeWorkspaceFile.
// Imported from outside apps/workbench/services (the page below) but never
// called in a value position there: the endpoint must read `legacy_unused`
// (no caller) but with `importers` on record - the distinction the ledger
// keeps between "dead" and "nobody imports it at all".
import { post, getApiBase } from '../api';

export async function writeWorkspaceFile(path: string, body: string): Promise<void> {
  await post<void>(`${getApiBase()}/api/v1/workspace/write`, { path, body });
}
