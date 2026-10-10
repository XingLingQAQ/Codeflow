// Caller of the download client: GET /api/v1/exports/{id} is a
// non-JSON (download) entry, and this page is the reason the old client
// cannot be called unused.
import { downloadExport } from '../../services/exports';

export function ExportsPage() {
  return (
    <button type="button" onClick={() => void downloadExport('latest')}>
      download
    </button>
  );
}
