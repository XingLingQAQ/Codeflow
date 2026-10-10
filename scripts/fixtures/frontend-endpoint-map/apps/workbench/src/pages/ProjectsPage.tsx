// Default-object import + every value-pass shape the walker must recognise:
//   api.listProjects()                     member call (inside deleteProject's
//   action={api.listProjects}              JSX value pass          sibling)
//   queryFn: api.listProjects              object-literal value pass
// A function held only as a value is still a caller; treating it as unused is
// how T12.03.b would delete a client that a screen still uses.
import api from '../stores/api';

import { useQuery } from './react-query-stub';

export function ExportButton(props: { action: () => unknown }) {
  return (
    <button type="button" onClick={() => void props.action()}>
      export
    </button>
  );
}

export function ProjectsPage() {
  const { data } = useQuery({ queryKey: ['projects'], queryFn: api.listProjects });
  return (
    <div>
      <ExportButton action={api.listProjects} />
      <button type="button" onClick={() => void api.deleteProject('1')}>
        delete
      </button>
      <span>{data ? 'loaded' : 'loading'}</span>
    </div>
  );
}
