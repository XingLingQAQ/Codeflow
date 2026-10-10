// Dynamic import + re-export chain: `listProjects` is pulled through the
// `./barrel` star re-export with `await import()` and called on the resulting
// namespace object, while `listWorkspaceFiles` arrives as a named re-export.
// Neither may read caller-free.
export async function refresh(): Promise<void> {
  const api = await import('./barrel');
  const rows = await api.listProjects();
  const names = await api.listWorkspaceFiles(5);
  void rows;
  void names;
}
