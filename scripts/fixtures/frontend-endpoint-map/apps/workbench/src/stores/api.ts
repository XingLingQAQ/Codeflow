// Default-object re-export: `export default { listProjects, listWorkspaceFiles,
// deleteProject }` gathers the named imports and the pages call them as
// `api.listProjects()` / `api.deleteProject(...)` / `api.listWorkspaceFiles` is
// passed to useQuery as a value. Every hop (named import -> default object ->
// member call / value pass) must be followed back to the declaring client.
import { listProjects, deleteProject } from '../../services/projects';
import { listWorkspaceFiles } from '../../services/workspace';

export default {
  listProjects,
  deleteProject,
  listWorkspaceFiles,
};
