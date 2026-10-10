// An unattributable shape on purpose: `const { deleteWorkspaceFiles } = workspace`
// destructures a namespace import, and the local it binds cannot be tied back
// to the module member by name. The fail-closed rule is that *every* export
// of services/workspace.ts is then treated as possibly imported, so nothing
// in that module can ever read caller-free because of this file - the row
// keeps a `unresolved` importer even though no `<name>` call was seen.
import * as workspace from '../../services/workspace';

export function ShortcutBar() {
  const { deleteWorkspaceFiles } = workspace;
  return (
    <button type="button" onClick={deleteWorkspaceFiles}>
      purge
    </button>
  );
}
