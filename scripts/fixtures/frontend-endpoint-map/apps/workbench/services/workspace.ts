// Old client layer:
//   GET    /api/v1/workspace/list  -> listWorkspaceFiles  (`both`)
//   DELETE /api/v1/workspace/files -> deleteWorkspaceFiles (`legacy_only`:
//          openapi.yaml declares the operation and router.go registers the
//          route, so the backend knows the path - it is NOT `orphan_client`;
//          the old client is what T12.03.b has to migrate and delete)
import { get, del, getApiBase } from '../api';

export async function listWorkspaceFiles(limit: number): Promise<string[]> {
  return get<string[]>(`${getApiBase()}/api/v1/workspace/list`, { limit });
}

export async function deleteWorkspaceFiles(): Promise<void> {
  await del<void>(`${getApiBase()}/api/v1/workspace/files`);
}
