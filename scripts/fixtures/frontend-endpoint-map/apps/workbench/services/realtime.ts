// Old client layer, WebSocket entry with a *resolvable* path: the socket URL
// goes through getWsBase(), so the ledger attributes it to
// GET /api/v1/realtime/socket and lists it in "Non-JSON entries" as
// websocket (a frame stream cannot be migrated as ordinary JSON). The route
// exists but no contract declares it and nothing imports this module; the
// row must read legacy_unused and deleting the function leaves route_only.
import { getWsBase } from '../api';

export function openRealtimeSocket(): WebSocket {
  return new WebSocket(`${getWsBase()}/api/v1/realtime/socket`);
}
