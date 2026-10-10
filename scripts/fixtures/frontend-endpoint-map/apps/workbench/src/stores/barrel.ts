// Re-export chain behind the dynamic import in projectStore.ts: the star
// re-export must carry `listProjects`, the named re-export must carry
// `listWorkspaceFiles`, and the chain must be followed all the way to the
// declaring client so deletion cannot call either one unused.
export * from '../../services/projects';
export { listWorkspaceFiles } from '../../services/workspace';
