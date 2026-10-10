// Namespace import + pure value pass: `workspace.deleteWorkspaceFiles` is
// passed as a handler (`onClick={...}`) and never called with parentheses in
// this file. A function held as a value is still a caller; missing this shape
// is how T12.03.b would delete a client a screen still uses.
import * as workspace from '../../services/workspace';

export function ShellPage() {
  return (
    <button type="button" onClick={workspace.deleteWorkspaceFiles}>
      purge
    </button>
  );
}
