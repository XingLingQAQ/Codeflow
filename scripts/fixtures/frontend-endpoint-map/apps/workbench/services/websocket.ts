// Old client layer, WebSocket entry: the socket URL comes from
// WS_ENDPOINTS.events, a getter on the api.ts table. Neither the path nor the
// import can be resolved by the ledger, so the endpoint must read `unknown`
// (unresolved_imports + the `{WS_ENDPOINTS.events}` row) - never `orphan_client`.
export class WebSocketService {
  private url: string;

  constructor(url: string) {
    this.url = url;
  }

  connect(): void {
    const socket = new WebSocket(this.url);
    void socket;
  }
}
