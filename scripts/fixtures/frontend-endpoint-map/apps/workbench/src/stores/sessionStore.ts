// Namespace import through a barrel: `ws.connect` must resolve through
// services/index.ts's star re-export down to services/websocket.ts#connect,
// which is what keeps `GET /ws` out of the caller-free bucket.
import * as ws from '../../services';

export function warmSocket(): void {
  ws.connect('/ws');
}
