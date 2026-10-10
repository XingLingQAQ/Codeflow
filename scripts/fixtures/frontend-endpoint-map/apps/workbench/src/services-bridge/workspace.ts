// Bridge layer: the same workspace endpoint, called through the new path.
import { get, getApiBase } from '../../api';

export async function listWorkspace(limit: number): Promise<string[]> {
  return get<string[]>(`${getApiBase()}/api/v1/workspace/list`, { limit });
}
