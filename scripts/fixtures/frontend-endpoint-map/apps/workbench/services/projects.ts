// Old client layer, fixture-sized. Endpoints called from here:
//   GET    /api/v1/projects            -> listProjects   (dead: only a test
//          imports it, so it must read legacy_unused, not be kept alive by
//          the test)
//   DELETE /api/v1/projects/{id}       -> deleteProject  (live: ProjectsPage
//          passes it as a JSX handler -> legacy_only)
import { get, del, API_ENDPOINTS } from '../api';
import type { Project, ProjectList } from '../generated/openapi-types';

export async function listProjects(signal?: AbortSignal): Promise<Project[]> {
  const body = await get<ProjectList>(API_ENDPOINTS.projects, { limit: 50 }, signal);
  return body.projects;
}

export async function deleteProject(id: string): Promise<void> {
  await del<void>(`${API_ENDPOINTS.projects}/${id}`);
}
