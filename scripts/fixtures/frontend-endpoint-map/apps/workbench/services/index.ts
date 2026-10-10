// Barrel: the shape T12.03.b must follow before calling a client unused.
// `export * from './websocket'` is the hop a star re-export must carry, and the
// explicit re-exports are followed through `import * as ws from '../../services'`.
export * from './websocket';
export { deleteProject } from './projects';
export { listWorkspaceFiles } from './workspace';
export { deleteWorkspaceFiles } from './workspace';
