// Bridge layer: a server-rendered blob download, not ordinary JSON.
import { getApiBase, authHeadersFor } from '../../api';

export async function downloadWorkspaceFile(id: string): Promise<Blob> {
  const url = `${getApiBase()}/api/v1/workspace/files/${id}/download`;
  const response = await fetch(url, {
    headers: { Authorization: `Bearer ${authHeadersFor(url)['Authorization']}` },
  });
  return response.blob();
}
