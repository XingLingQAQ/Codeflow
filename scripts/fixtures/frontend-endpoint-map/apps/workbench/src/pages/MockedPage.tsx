// NOT DEV-gated mock import: a production build could reach src/mocks through
// this file, which is exactly what non_dev_fallbacks must list and what the
// T12.03.c ProductionBuildHasNoMockTransport guard exists for.
import { mockWorkspaceList } from '../mocks/workspace';
import { listWorkspaceFiles } from '../../services/workspace';

export function MockedPage() {
  const rows = mockWorkspaceList(3);
  void rows;
  return (
    <button type="button" onClick={() => void listWorkspaceFiles(3)}>
      mocked
    </button>
  );
}
