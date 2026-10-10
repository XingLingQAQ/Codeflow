// LIVE DEV mock entry point: the workspace list is answered from src/mocks
// behind import.meta.env.DEV, so this injection site is guarded and must NOT
// appear in non_dev_fallbacks. T12.03.c guards that a production build never
// carries this branch, so the fixture shows the flagged shape.
import { listWorkspace } from '../services-bridge/workspace';
import { writeWorkspaceFile } from '../../services/files';
import { uploadWorkspaceFile } from '../../services/uploads';

export async function loadWorkspace(limit: number): Promise<string[]> {
  if (import.meta.env.DEV) {
    const { mockWorkspaceList } = await import('../mocks/workspace');
    return mockWorkspaceList(limit);
  }
  return listWorkspace(limit);
}

export async function pushWorkspaceFile(file: Blob): Promise<void> {
  await uploadWorkspaceFile(file);
}

// Held as a value, never called here: holding an imported function as a value
// is still a caller - deleting services/files.ts#writeWorkspaceFile would
// break this export - so POST /api/v1/workspace/write reads legacy_only,
// NOT legacy_unused. "Not called in this file" is not "no caller".
export const workspaceWriterRef = writeWorkspaceFile;
